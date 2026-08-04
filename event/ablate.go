package event

import (
	"fmt"
	"sort"
	"strings"
)

// What the two risky choices in this layer buy, measured by taking them out.
//
// The obvious ablation, removing the event layer and counting the competency
// question answers that change, answers itself: before this layer there were no
// act nodes, so every answer changes and the number means nothing. The two
// ablations here take out a decision instead of a milestone, and each one is a
// decision this layer could plausibly have made the other way.

// Scoped rewrites sightings so an act named in two instruments is two nodes.
//
// This is the layer as it would have been built if identity stopped at the
// document, which is the conservative choice: it cannot merge two unrelated acts
// that share a phrase, and it cannot join a law to the decree that guides it.
func Scoped(in []Occurrence, chains []Chain) ([]Occurrence, []Chain) {
	out := make([]Occurrence, 0, len(in))
	for _, o := range in {
		o.EventID = scope(o.EventID, o.Evidence.DocID)
		out = append(out, o)
	}
	cs := make([]Chain, 0, len(chains))
	for _, c := range chains {
		if len(c.Evidence) == 0 {
			continue
		}
		doc := c.Evidence[0].DocID
		c.FromID, c.ToID = scope(c.FromID, doc), scope(c.ToID, doc)
		cs = append(cs, c)
	}
	return out, cs
}

func scope(id, docID string) string {
	if docID == "" {
		return id
	}
	return id + "@" + docID
}

func unscope(id string) string {
	if i := strings.LastIndex(id, "@"); i > 0 {
		return id[:i]
	}
	return id
}

// IdentityAblation is what corpus wide act identity is worth, and what it risks.
type IdentityAblation struct {
	Depth int `json:"depth"`

	Events    int `json:"events"`
	PerDoc    int `json:"per_doc_events"`
	Chains    int `json:"chains"`
	PerDocOut int `json:"per_doc_chains"`

	// Acts is how many acts question 24 was asked about, and Changed how many
	// of those answers lose something when identity stops at the document.
	Acts    int `json:"acts"`
	Changed int `json:"changed"`
	// Lost is the total number of consequences that disappear. It cannot be
	// negative: splitting a node can only break a path, never make one.
	Lost int `json:"lost"`
	// Merged is how many acts the corpus wide identifier joined across
	// instruments. Every one of them is either the layer working or two acts
	// merged under one phrase, and this number is the size of that bet.
	Merged int `json:"merged"`
}

// AblateIdentity folds the same sightings both ways and compares the answers.
func AblateIdentity(occurrences []Occurrence, chains []Chain, r *Registry, th Thresholds, depth int) IdentityAblation {
	if depth <= 0 {
		depth = 3
	}
	a := IdentityAblation{Depth: depth}

	events := Fold(occurrences, r, th)
	folded := FoldChains(chains, events, r, th)
	wide := NewGraph(events, folded, nil)
	a.Events, a.Chains = len(events), len(folded)

	so, sc := Scoped(occurrences, chains)
	perDocEvents := Fold(so, r, th)
	perDocChains := FoldChains(sc, perDocEvents, r, th)
	narrow := NewGraph(perDocEvents, perDocChains, nil)
	a.PerDoc, a.PerDocOut = len(perDocEvents), len(perDocChains)

	// The per document nodes that stand for one corpus wide act.
	parts := map[string][]string{}
	for _, e := range perDocEvents {
		base := unscope(e.ID)
		parts[base] = append(parts[base], e.ID)
	}
	for _, e := range events {
		if len(e.Evidence) > 0 && len(eventDocs(e)) > 1 {
			a.Merged++
		}
		a.Acts++
		want := reach(wide, e.ID, depth)
		got := map[string]bool{}
		for _, part := range parts[e.ID] {
			for id := range reach(narrow, part, depth) {
				got[unscope(id)] = true
			}
		}
		lost := 0
		for id := range want {
			if !got[id] {
				lost++
			}
		}
		if lost > 0 {
			a.Changed++
			a.Lost += lost
		}
	}
	return a
}

// eventDocs is the instruments one folded act was seen in.
func eventDocs(e Event) []string {
	seen := map[string]bool{}
	for _, ev := range e.Evidence {
		if ev.DocID != "" {
			seen[ev.DocID] = true
		}
	}
	return sortedKeys(seen)
}

// reach is the set of acts question 24 walks to, forward only.
func reach(g *Graph, from string, depth int) map[string]bool {
	out := map[string]bool{}
	for _, s := range g.walk(from, depth, true) {
		out[s.ToID] = true
	}
	return out
}

func (a IdentityAblation) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ablation       corpus wide act identity, against the same sightings folded per document\n")
	fmt.Fprintf(&b, "               %d acts and %d chains, against %d acts and %d chains\n",
		a.Events, a.Chains, a.PerDoc, a.PerDocOut)
	fmt.Fprintf(&b, "               %d of %d question 24 answers lose something without it, and the number of consequences lost is %d\n",
		a.Changed, a.Acts, a.Lost)
	fmt.Fprintf(&b, "               %d acts are named in more than one instrument, which is how many merges this bet rests on\n", a.Merged)
	if a.Changed == 0 {
		b.WriteString("               nothing was lost, so on this corpus the identifier is carrying its risk for no answer\n")
	}
	return b.String()
}

// SanctionAblation is what joining a penalty through the norm buys over joining
// it by the act's label.
type SanctionAblation struct {
	ThroughNorm int `json:"through_norm"`
	ByLabel     int `json:"by_label"`
	// Invented is the rows label matching adds: a duty in one provision handed a
	// penalty that provision does not state.
	Invented int `json:"invented"`
	// CrossDoc is how many of those cross an instrument boundary, which is the
	// shape of the error, a fine in a sanctions decree attached to a duty in an
	// unrelated law that happens to use the same verb.
	CrossDoc int      `json:"cross_doc"`
	Examples []string `json:"examples,omitempty"`
}

// AblateSanctionJoin runs the label matching baseline this package refuses.
//
// The baseline is the natural implementation and it is what this milestone would
// have shipped without the norm slot links: take the act a sanctioning provision
// names, and attach its penalty to every provision that names the same act.
func AblateSanctionJoin(g *Graph) SanctionAblation {
	var a SanctionAblation
	actions := map[string]Link{}
	sanctions := map[string]Link{}
	for _, l := range g.links {
		switch l.Kind {
		case LinkAction:
			actions[l.StatementID] = l
		case LinkSanction:
			sanctions[l.StatementID] = l
		}
	}
	// What each act is penalised with, anywhere in the corpus.
	penalties := map[string]map[string]Link{}
	for id, action := range actions {
		p, ok := sanctions[id]
		if !ok {
			continue
		}
		a.ThroughNorm++
		if penalties[action.EventID] == nil {
			penalties[action.EventID] = map[string]Link{}
		}
		penalties[action.EventID][p.EventID] = p
	}
	// In statement order, because the examples printed under the count have to be
	// the same examples on two machines.
	statements := make([]string, 0, len(actions))
	for id := range actions {
		statements = append(statements, id)
	}
	sort.Strings(statements)
	for _, id := range statements {
		action := actions[id]
		for _, p := range sortedLinks(penalties[action.EventID]) {
			a.ByLabel++
			if s, ok := sanctions[id]; ok && s.EventID == p.EventID {
				continue
			}
			a.Invented++
			if p.DocID != action.DocID {
				a.CrossDoc++
				if len(a.Examples) < MaxExamples {
					a.Examples = append(a.Examples, fmt.Sprintf("%s in %s would carry %s from %s",
						g.Label(action.EventID), action.ProvisionID, g.Label(p.EventID), p.ProvisionID))
				}
			}
		}
	}
	return a
}

func sortedLinks(in map[string]Link) []Link {
	out := make([]Link, 0, len(in))
	for _, l := range in {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EventID < out[j].EventID })
	return out
}

func (a SanctionAblation) String() string {
	var b strings.Builder
	b.WriteString("ablation       penalties joined through the norm, against joined by the act's label\n")
	fmt.Fprintf(&b, "               %d rows through the norm, %d by label, %d of which no provision states\n",
		a.ThroughNorm, a.ByLabel, a.Invented)
	fmt.Fprintf(&b, "               %d of the invented rows attach a penalty from another instrument\n", a.CrossDoc)
	for _, e := range a.Examples {
		fmt.Fprintf(&b, "               %s\n", e)
	}
	if a.Invented == 0 {
		b.WriteString("               label matching would have produced the same table here, so on this corpus the join costs nothing and buys nothing\n")
	}
	return b.String()
}
