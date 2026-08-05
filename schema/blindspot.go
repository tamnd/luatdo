package schema

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
)

// Ask is one place the corpus asked the registry for a class and did not get
// one: a party or an object the extractor wrote down and could not place.
type Ask struct {
	Text        string
	Role        string
	ProvisionID string
	DocID       string
}

// Asked collects the asks for one surface form.
type Asked struct {
	Slug     string   `json:"slug"`
	Text     string   `json:"text"`
	Roles    []string `json:"roles,omitempty"`
	Count    int      `json:"count"`
	Docs     int      `json:"docs"`
	Examples []string `json:"examples,omitempty"`
}

// Blindspots is what the corpus wanted and the registry did not have, beside
// what the registry has and the corpus never wanted.
//
// Both halves are the report. A registry can be wrong by missing a class the
// drafters keep using, and it can be wrong by carrying a class nobody ever
// needs, and a report that only shows the first will grow the registry forever.
type Blindspots struct {
	Refs     int      `json:"refs"`
	Placed   int      `json:"placed"`
	Unplaced int      `json:"unplaced"`
	Wanted   []Asked  `json:"wanted"`
	Unused   []string `json:"unused_classes"`
	Used     []string `json:"used_classes"`

	// ByRole is the unplaced count per role, and it is here because the roles
	// are not comparable. The registry is a registry of entity classes, so an
	// action reference cannot be placed in it whatever the corpus does, and an
	// unplaced action is a fact about the schema rather than a gap in the class
	// list. Rolling the two together makes the verbs drown the parties.
	ByRole map[string]int `json:"by_role,omitempty"`

	// Predicates is the number the registry declares. It is reported without a
	// usage count on purpose, because nothing in the pipeline emits one, so a
	// zero here would read as a corpus that never needed them rather than as a
	// part of the registry no pass can reach.
	Predicates int `json:"predicates"`
}

// MinAsks is how often a surface form has to appear before the report names it.
// A party mentioned once is a drafting particular and a party mentioned in
// twenty provisions across five instruments is a class the registry is missing,
// and the only thing that separates them is counting.
const MinAsks = 3

// FindBlindspots walks stored statements for references the registry could not
// place, and folds them by surface form.
//
// A reference with no class is the honest signal here. The extractor is told
// not to guess, so a bearer carrying text and no class identifier is the model
// saying it saw a party the registry has no name for.
func FindBlindspots(reg *ontology.Registry, items []Item, extra []Ask) Blindspots {
	b := Blindspots{Predicates: len(reg.Predicates), ByRole: map[string]int{}}
	used := map[string]bool{}
	asks := append([]Ask{}, extra...)
	for _, it := range items {
		s := it.Statement
		for _, r := range []struct {
			ref  *norm.Ref
			role string
		}{
			{s.Bearer, "bearer"}, {s.Counterparty, "counterparty"},
			{&s.Action, "action"}, {s.Object, "object"},
		} {
			if r.ref == nil || strings.TrimSpace(r.ref.Text) == "" {
				continue
			}
			b.Refs++
			if r.ref.ClassID != "" && reg.Class(r.ref.ClassID) != nil {
				b.Placed++
				used[r.ref.ClassID] = true
				continue
			}
			b.Unplaced++
			b.ByRole[r.role]++
			asks = append(asks, Ask{Text: r.ref.Text, Role: r.role, ProvisionID: it.ProvisionID, DocID: it.DocID})
		}
	}
	b.Wanted = fold(asks)
	for _, c := range reg.Classes {
		if used[c.ID] {
			b.Used = append(b.Used, c.ID)
		} else {
			b.Unused = append(b.Unused, c.ID)
		}
	}
	sort.Strings(b.Used)
	sort.Strings(b.Unused)
	return b
}

// fold groups asks by the slug of their text, which is the same folding the
// linker uses, so a surface form that would have resolved had the class existed
// is counted as one thing rather than as its case and accent variants.
func fold(asks []Ask) []Asked {
	by := map[string]*Asked{}
	docs := map[string]map[string]bool{}
	roles := map[string]map[string]bool{}
	for _, a := range asks {
		slug := law.Slug(a.Text)
		if slug == "" {
			continue
		}
		got := by[slug]
		if got == nil {
			got = &Asked{Slug: slug, Text: strings.TrimSpace(a.Text)}
			by[slug] = got
			docs[slug] = map[string]bool{}
			roles[slug] = map[string]bool{}
		}
		got.Count++
		if a.DocID != "" {
			docs[slug][a.DocID] = true
		}
		if a.Role != "" {
			roles[slug][a.Role] = true
		}
		if len(got.Examples) < MaxExamples && a.ProvisionID != "" {
			got.Examples = append(got.Examples, a.ProvisionID)
		}
	}
	out := make([]Asked, 0, len(by))
	for slug, a := range by {
		a.Docs = len(docs[slug])
		for r := range roles[slug] {
			a.Roles = append(a.Roles, r)
		}
		sort.Strings(a.Roles)
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

// RoleAction is the reference role the registry cannot serve.
const RoleAction = "action"

// Recurring returns the surface forms asked for often enough to be worth a
// person's time.
//
// A form only ever seen as an action is left out. Not because it does not
// matter, the verbs are half the meaning of a norm, but because no decision a
// reviewer can make about the class list would place it. Putting "bao gồm" at
// the top of a list of missing classes forty nine times over would train
// whoever reads it to stop reading it.
func (b Blindspots) Recurring() []Asked {
	var out []Asked
	for _, a := range b.Wanted {
		if a.Count < MinAsks || onlyAction(a.Roles) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func onlyAction(roles []string) bool {
	return len(roles) == 1 && roles[0] == RoleAction
}

func (b Blindspots) String() string {
	var s strings.Builder
	fmt.Fprintf(&s, "registry blind spots over %d references, %d placed in a class and %d not\n",
		b.Refs, b.Placed, b.Unplaced)
	rec := b.Recurring()
	fmt.Fprintf(&s, "  %d distinct surface forms went unplaced, %d of them at least %d times in a role the registry could serve\n",
		len(b.Wanted), len(rec), MinAsks)
	if n := b.ByRole[RoleAction]; n > 0 {
		fmt.Fprintf(&s, "  note: %d of the unplaced references are actions, which no class in an entity registry can take, so they are counted and not recommended\n", n)
	}
	for i, a := range rec {
		if i >= 15 {
			fmt.Fprintf(&s, "  and %d more in the JSON\n", len(rec)-i)
			break
		}
		fmt.Fprintf(&s, "  %4d in %2d docs  %-12s %s\n", a.Count, a.Docs, strings.Join(a.Roles, "/"), a.Text)
	}
	fmt.Fprintf(&s, "  note: %d of the registry's classes were used and %d never were\n", len(b.Used), len(b.Unused))
	if len(b.Unused) > 0 {
		fmt.Fprintf(&s, "  note: unused: %s\n", strings.Join(b.Unused, ", "))
	}
	fmt.Fprintf(&s, "  note: the registry declares %d predicates and no pass emits one, so their usage is not measured rather than zero\n",
		b.Predicates)
	return s.String()
}
