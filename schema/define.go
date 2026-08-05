package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/eval"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/retrieve"
)

// Proposal is one thing open extraction proposed, folded across the corpus.
//
// Folding first is what makes the define pass affordable. A thousand bootstrap
// candidates are a few hundred distinct labels, and defining a label once with
// all of its quotes in front of the model is both cheaper and better evidence
// than defining it once per provision.
type Proposal struct {
	Slug       string   `json:"slug"`
	Label      string   `json:"label"`
	Count      int      `json:"count"`
	Docs       int      `json:"docs"`
	Provisions []string `json:"provisions,omitempty"`
	Quotes     []string `json:"quotes,omitempty"`
	Definition string   `json:"definition,omitempty"`
}

// FoldProposals groups candidates by the slug of their label.
//
// The slug is the same fold the linker uses, so "Người lao động" and "người lao
// động" are one proposal here exactly as they would be one mention there.
func FoldProposals(cs []ontology.Candidate, docOf func(provisionID string) string) []Proposal {
	by := map[string]*Proposal{}
	docs := map[string]map[string]bool{}
	for _, c := range cs {
		if c.Kind != "class" {
			continue
		}
		slug := law.Slug(c.Label)
		if slug == "" {
			continue
		}
		p := by[slug]
		if p == nil {
			p = &Proposal{Slug: slug, Label: strings.TrimSpace(c.Label)}
			by[slug] = p
			docs[slug] = map[string]bool{}
		}
		p.Count++
		if c.Provision != "" {
			if len(p.Provisions) < MaxExamples {
				p.Provisions = append(p.Provisions, c.Provision)
			}
			if docOf != nil {
				if id := docOf(c.Provision); id != "" {
					docs[slug][id] = true
				}
			}
		}
		if q := strings.TrimSpace(c.Quote); q != "" && len(p.Quotes) < MaxExamples {
			p.Quotes = append(p.Quotes, q)
		}
	}
	out := make([]Proposal, 0, len(by))
	for slug, p := range by {
		p.Docs = len(docs[slug])
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

// Definer runs the define and canonicalize halves of the EDC loop.
//
// The order is the whole point of the method. Deciding whether a proposal is
// already in the registry by comparing labels asks a question about words;
// deciding it by comparing definitions asks a question about meaning, and the
// two disagree exactly where a registry goes wrong: two words for one class,
// and one word for two.
type Definer struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
}

const defineInstructions = `Bạn viết định nghĩa cho một khái niệm pháp lý xuất hiện trong văn bản pháp luật Việt Nam.
Bạn được cho nhãn của khái niệm và một vài đoạn trích nguyên văn nơi nó xuất hiện.

Quy tắc bắt buộc:
1. Định nghĩa dài đúng một câu, viết bằng tiếng Việt.
2. Định nghĩa nói khái niệm đó là loại gì và phân biệt nó với những khái niệm gần nó, không nhắc lại nhãn.
3. Định nghĩa nói về loại khái niệm, không nói về một văn bản hay một điều khoản cụ thể.

Trả về đúng một đối tượng JSON, không giải thích, theo dạng:
{"definition":"..."}`

type wireDefinition struct {
	Definition string `json:"definition"`
}

// DefinePrompt renders one define question.
func DefinePrompt(label string, quotes []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Khái niệm: %s\n", label)
	if len(quotes) > 0 {
		b.WriteString("\nCác đoạn trích:\n")
		for _, q := range quotes {
			fmt.Fprintf(&b, "  - %s\n", q)
		}
	}
	return b.String()
}

// Define writes one definition from a label and its quotes.
func (d *Definer) Define(ctx context.Context, label string, quotes []string) (string, api.Usage, error) {
	var usage api.Usage
	input := DefinePrompt(label, quotes)
	for attempt := 0; attempt <= corrections(d.MaxCorrections); attempt++ {
		resp, err := d.Completer.Complete(ctx, api.Request{
			Model: d.Model, Instructions: defineInstructions, Input: input,
		})
		if err != nil {
			return "", usage, err
		}
		usage = addUsage(usage, resp.Usage)
		var wire wireDefinition
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			input = DefinePrompt(label, quotes) + "\nLần trả lời trước không phải một đối tượng JSON hợp lệ: " + err.Error() + "\nTrả lời lại.\n"
			continue
		}
		if def := strings.TrimSpace(wire.Definition); def != "" {
			return def, usage, nil
		}
		input = DefinePrompt(label, quotes) + "\nLần trả lời trước để trống định nghĩa.\nTrả lời lại.\n"
	}
	return "", usage, nil
}

// DefineRegistry writes a definition for every registry class that has none.
//
// Ontology v1 shipped forty three classes and not one definition, which means
// the canonicalize step has nothing on its own side to compare against. This
// runs first for that reason. It returns the definitions rather than writing
// them, because the version is frozen and only a person bumps it.
func (d *Definer) DefineRegistry(ctx context.Context, reg *ontology.Registry, onEach func(id, definition string)) (map[string]string, api.Usage, error) {
	out := map[string]string{}
	var usage api.Usage
	for _, c := range reg.Classes {
		if strings.TrimSpace(c.DefinitionVI) != "" {
			out[c.ID] = c.DefinitionVI
			continue
		}
		var quotes []string
		if c.Parent != "" {
			if p := reg.Class(c.Parent); p != nil {
				quotes = append(quotes, "Khái niệm này là một loại "+p.LabelVI+".")
			}
		}
		for _, a := range c.Aliases {
			quotes = append(quotes, "Còn được gọi là "+a+".")
		}
		def, u, err := d.Define(ctx, c.LabelVI, quotes)
		usage = addUsage(usage, u)
		if err != nil {
			return out, usage, err
		}
		out[c.ID] = def
		if onEach != nil {
			onEach(c.ID, def)
		}
	}
	return out, usage, nil
}

// Shortlist ranks registry classes against a text by shared tokens.
//
// The shortlist exists to keep the decision affordable, not to make it. Forty
// three classes would fit in one prompt today and four hundred will not, and a
// pass whose cost grows with the registry is a pass that gets switched off. The
// score is lexical because there is no embedding route in this repository, and
// that is a real limit: it will shortlist "người sử dụng lao động" for "người
// lao động" on four shared syllables and miss a synonym with none.
func Shortlist(reg *ontology.Registry, defs map[string]string, text string, n int) []ontology.Class {
	want := map[string]bool{}
	for _, t := range retrieve.Tokens(text) {
		want[t] = true
	}
	type scored struct {
		class ontology.Class
		score float64
	}
	var all []scored
	for _, c := range reg.Classes {
		var b strings.Builder
		b.WriteString(c.LabelVI)
		for _, a := range c.Aliases {
			b.WriteString(" " + a)
		}
		if def := defs[c.ID]; def != "" {
			b.WriteString(" " + def)
		} else if c.DefinitionVI != "" {
			b.WriteString(" " + c.DefinitionVI)
		}
		tokens := retrieve.Tokens(b.String())
		if len(tokens) == 0 {
			continue
		}
		hit := 0
		seen := map[string]bool{}
		for _, t := range tokens {
			if want[t] && !seen[t] {
				seen[t] = true
				hit++
			}
		}
		if hit == 0 {
			continue
		}
		// Dividing by the class side length keeps a long definition from
		// winning on breadth alone.
		all = append(all, scored{class: c, score: float64(hit) / float64(len(seen)+len(tokens))})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].class.ID < all[j].class.ID
	})
	if n > 0 && len(all) > n {
		all = all[:n]
	}
	out := make([]ontology.Class, 0, len(all))
	for _, s := range all {
		out = append(out, s.class)
	}
	return out
}

// ShortlistSize is how many registry classes the model is asked to choose
// between. Small enough that the prompt stays about one decision, large enough
// that a lexical miss on the best match is usually caught by the second.
const ShortlistSize = 8

const canonicalizeInstructions = `Bạn quyết định một khái niệm mới có trùng với một lớp đã có trong danh mục hay không.
Bạn được cho định nghĩa của khái niệm mới và định nghĩa của các lớp đã có.

Quy tắc bắt buộc:
1. Chỉ trả lời trùng khi hai định nghĩa nói về cùng một loại đối tượng, không phải khi hai nhãn giống chữ.
2. Một lớp rộng hơn hoặc hẹp hơn thì không trùng. Ví dụ người lao động và người sử dụng lao động là hai lớp khác nhau dù chữ gần giống.
3. Nếu không lớp nào trùng, trả về "` + NoMatch + `" và nêu lớp gần nhất cùng lý do bị loại.

Trả về đúng một đối tượng JSON, không giải thích, theo dạng:
{"match":"vn-legal:Employer","nearest":"vn-legal:Employer","reason":"..."}`

// NoMatch is the answer that sends a proposal to the queue instead of merging
// it into an existing class.
const NoMatch = "none"

// CanonicalizePrompt renders one canonicalize question.
func CanonicalizePrompt(p Proposal, shortlist []ontology.Class, defs map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Khái niệm mới: %s\n", p.Label)
	fmt.Fprintf(&b, "Định nghĩa: %s\n\n", p.Definition)
	b.WriteString("Các lớp đã có:\n")
	for _, c := range shortlist {
		def := defs[c.ID]
		if def == "" {
			def = c.DefinitionVI
		}
		if def == "" {
			def = "chưa có định nghĩa"
		}
		fmt.Fprintf(&b, "  [%s] %s: %s\n", c.ID, c.LabelVI, def)
	}
	return b.String()
}

type wireMatch struct {
	Match   string `json:"match"`
	Nearest string `json:"nearest"`
	Reason  string `json:"reason"`
}

// Match is one canonicalize decision.
//
// Nearest and Reason are carried even when the answer is a match, because the
// queue entry for a rejected proposal is worthless without them: "new class"
// tells a reviewer nothing, "not the same as vn-legal:Employer because this one
// is the party that pays and that one is the party that hires" tells them
// whether the pass was right.
type Match struct {
	Slug    string `json:"slug"`
	ClassID string `json:"class_id,omitempty"`
	Nearest string `json:"nearest,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Canonicalize decides whether a defined proposal is one of the shortlisted
// classes.
func (d *Definer) Canonicalize(ctx context.Context, p Proposal, shortlist []ontology.Class, defs map[string]string) (Match, api.Usage, error) {
	var usage api.Usage
	m := Match{Slug: p.Slug}
	if len(shortlist) == 0 {
		// Nothing lexically overlapped, so there is no decision to pay for.
		// This is recorded as a shortlist miss rather than as a model saying
		// the proposal is new.
		m.Reason = "no registry class shared a token with the definition"
		return m, usage, nil
	}
	allowed := map[string]bool{}
	for _, c := range shortlist {
		allowed[c.ID] = true
	}
	input := CanonicalizePrompt(p, shortlist, defs)
	for attempt := 0; attempt <= corrections(d.MaxCorrections); attempt++ {
		resp, err := d.Completer.Complete(ctx, api.Request{
			Model: d.Model, Instructions: canonicalizeInstructions, Input: input,
		})
		if err != nil {
			return m, usage, err
		}
		usage = addUsage(usage, resp.Usage)
		var wire wireMatch
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			input = CanonicalizePrompt(p, shortlist, defs) + "\nLần trả lời trước không phải một đối tượng JSON hợp lệ: " + err.Error() + "\nTrả lời lại.\n"
			continue
		}
		m.Reason = strings.TrimSpace(wire.Reason)
		if allowed[wire.Nearest] {
			m.Nearest = wire.Nearest
		}
		if wire.Match == "" || strings.EqualFold(wire.Match, NoMatch) {
			return m, usage, nil
		}
		if !allowed[wire.Match] {
			input = CanonicalizePrompt(p, shortlist, defs) +
				fmt.Sprintf("\nLần trả lời trước bị từ chối: %q không nằm trong danh sách.\nTrả lời lại.\n", wire.Match)
			continue
		}
		m.ClassID = wire.Match
		if m.Nearest == "" {
			m.Nearest = wire.Match
		}
		return m, usage, nil
	}
	m.Reason = "the model did not answer with a class from the shortlist"
	return m, usage, nil
}

// RoundTripScore is canonicalization measured against the only gold that
// exists for it: the registry itself.
//
// Held is the half where the class is in the shortlist and the pass should find
// it. Withheld is the half where the class was taken out and the pass should
// answer none. They are reported apart because a pass that answers a class for
// everything scores perfectly on the first half and nothing on the second, and
// one number hides that.
type RoundTripScore struct {
	Held     eval.Accuracy `json:"held"`
	Withheld eval.Accuracy `json:"withheld"`
	InShort  eval.Accuracy `json:"in_shortlist"`
	Confused []Mistake     `json:"confused,omitempty"`
	Sibling  int           `json:"withheld_answered_with_a_relative"`
	Calls    int           `json:"calls"`
	Usage    api.Usage     `json:"usage"`
}

// RoundTrip feeds each class definition back through canonicalization.
//
// It is the one part of this milestone with real gold, so it is worth its
// calls: a pass that cannot match a class definition to its own class has no
// business deciding that a proposal is new.
func (d *Definer) RoundTrip(ctx context.Context, reg *ontology.Registry, defs map[string]string, onEach func(id string, got Match, held bool)) (RoundTripScore, error) {
	var s RoundTripScore
	for _, c := range reg.Classes {
		def := defs[c.ID]
		if def == "" {
			def = c.DefinitionVI
		}
		if strings.TrimSpace(def) == "" {
			continue
		}
		// The label is deliberately not part of the probe. Sending it would
		// measure string matching, which is the thing the define pass exists to
		// replace.
		p := Proposal{Slug: c.ID, Label: "khái niệm cần xếp lớp", Definition: def}

		full := Shortlist(reg, defs, def, ShortlistSize)
		s.InShort.Observe(containsClass(full, c.ID))
		got, usage, err := d.Canonicalize(ctx, p, full, defs)
		s.Usage = addUsage(s.Usage, usage)
		s.Calls++
		if err != nil {
			return s, err
		}
		s.Held.Observe(got.ClassID == c.ID)
		if got.ClassID != c.ID {
			s.Confused = append(s.Confused, Mistake{ChildID: c.ID, Got: got.ClassID, Want: c.ID})
		}
		if onEach != nil {
			onEach(c.ID, got, true)
		}

		short := withoutClass(full, c.ID)
		out, usage, err := d.Canonicalize(ctx, p, short, defs)
		s.Usage = addUsage(s.Usage, usage)
		s.Calls++
		if err != nil {
			return s, err
		}
		s.Withheld.Observe(out.ClassID == "")
		if out.ClassID != "" && related(reg, c.ID, out.ClassID) {
			// A withheld class answered with its own parent or a sibling is not
			// the same error as one answered with an unrelated class, and the
			// count is kept so the rate can be read with that in mind.
			s.Sibling++
		}
		if onEach != nil {
			onEach(c.ID, out, false)
		}
	}
	return s, nil
}

func containsClass(cs []ontology.Class, id string) bool {
	for _, c := range cs {
		if c.ID == id {
			return true
		}
	}
	return false
}

func withoutClass(cs []ontology.Class, id string) []ontology.Class {
	out := make([]ontology.Class, 0, len(cs))
	for _, c := range cs {
		if c.ID != id {
			out = append(out, c)
		}
	}
	return out
}

// related reports whether two classes share a parent or one descends from the
// other.
func related(reg *ontology.Registry, a, b string) bool {
	if reg.IsA(a, b) || reg.IsA(b, a) {
		return true
	}
	ca, cb := reg.Class(a), reg.Class(b)
	return ca != nil && cb != nil && ca.Parent != "" && ca.Parent == cb.Parent
}

func (s RoundTripScore) String() string {
	t := eval.NewTable("canonicalize round trip", fmt.Sprintf("%d registry classes", s.Held.Of))
	t.Rate("class in its own lexical shortlist", s.InShort)
	t.Rate("class matched back to itself", s.Held)
	t.Rate("withheld class answered none", s.Withheld)
	t.Note("%d calls, %d tokens", s.Calls, s.Usage.TotalTokens)
	t.Note("%d of the withheld misses were a parent, child or sibling rather than an unrelated class", s.Sibling)
	return t.String()
}

// Queue turns unmatched proposals into candidate rows.
//
// Everything the pass could not place goes in with its definition, its counts,
// the documents it came from, its example quotes and the nearest registry entry
// with the reason that entry was rejected. A reviewer should be able to decide
// from the row alone, without rerunning anything.
func Queue(ps []Proposal, matches map[string]Match, now string) []ontology.Candidate {
	var out []ontology.Candidate
	for _, p := range ps {
		m := matches[p.Slug]
		if m.ClassID != "" {
			continue
		}
		out = append(out, ontology.Candidate{
			Kind:       "class",
			Label:      p.Label,
			Provision:  first(p.Provisions),
			Quote:      first(p.Quotes),
			Quotes:     p.Quotes,
			Definition: p.Definition,
			Count:      p.Count,
			Docs:       p.Docs,
			Nearest:    m.Nearest,
			Rejected:   m.Reason,
			Source:     "define",
			Status:     "proposed",
			At:         now,
		})
	}
	return out
}

func first(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
}

// DefineScore is what the define pass did over a corpus.
type DefineScore struct {
	Proposals int           `json:"proposals"`
	Defined   eval.Accuracy `json:"defined"`
	Matched   eval.Accuracy `json:"matched"`
	NoShort   int           `json:"no_shortlist"`
	Queued    int           `json:"queued"`
	Calls     int           `json:"calls"`
	Usage     api.Usage     `json:"usage"`
}

func (s DefineScore) String() string {
	t := eval.NewTable("define and canonicalize", fmt.Sprintf("%d folded proposals", s.Proposals))
	t.Rate("proposals the model defined", s.Defined)
	t.Rate("defined proposals matched to a registry class", s.Matched)
	t.Note("%d proposals queued for review", s.Queued)
	t.Note("%d proposals shared no token with any class, so no decision was paid for", s.NoShort)
	t.Note("%d calls, %d tokens", s.Calls, s.Usage.TotalTokens)
	return t.String()
}
