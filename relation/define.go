package relation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/api"
)

// Proposal is one relation type the model invented, folded across every
// provision that produced it.
//
// Folding before defining is the point. A label proposed once is a guess and a
// label proposed in ninety provisions across forty documents is the registry
// being incomplete, and the two get told apart by counting rather than by
// reading the label and forming an impression.
type Proposal struct {
	Label string `json:"label"`
	// Definition is what the Define step wrote. Canonicalization matches on
	// this and never on the label, because can co truoc and la dieu kien de
	// duoc cap are different phrases for one relation, and a model asked
	// whether two verb phrases mean the same thing in the abstract is being
	// asked the wrong question.
	Definition string `json:"definition"`
	// AsWritten collects the model's per provision wordings, deduplicated and
	// sorted. They are the material the Define step generalises over, and they
	// are kept afterwards so a reviewer can see the spread.
	AsWritten []string   `json:"as_written,omitempty"`
	Instances int        `json:"instances"`
	Docs      int        `json:"docs"`
	Examples  []Evidence `json:"examples,omitempty"`
	// MatchedTo is the registry type canonicalization landed on, empty when
	// nothing matched.
	MatchedTo string `json:"matched_to,omitempty"`
	Rationale string `json:"rationale,omitempty"`
}

// MaxExamples is how many supporting quotes a proposal carries into the review
// queue. A reviewer reads a handful and a file holding all ninety is a file
// nobody opens.
const MaxExamples = 5

// Propose folds the provisional edges into one proposal per invented label.
func Propose(edges []Edge) []Proposal {
	byLabel := map[string]*Proposal{}
	docs := map[string]map[string]bool{}
	written := map[string]map[string]bool{}
	for _, e := range edges {
		if e.Status != StatusProvisional {
			continue
		}
		p := byLabel[e.Type]
		if p == nil {
			p = &Proposal{Label: e.Type, Definition: e.Definition}
			byLabel[e.Type] = p
			docs[e.Type] = map[string]bool{}
			written[e.Type] = map[string]bool{}
		}
		for _, ev := range e.Evidence {
			p.Instances++
			if ev.DocID != "" {
				docs[e.Type][ev.DocID] = true
			}
			if w := strings.TrimSpace(ev.AsWritten); w != "" {
				written[e.Type][w] = true
			}
			if len(p.Examples) < MaxExamples {
				p.Examples = append(p.Examples, ev)
			}
		}
	}
	out := make([]Proposal, 0, len(byLabel))
	for label, p := range byLabel {
		p.Docs = len(docs[label])
		for w := range written[label] {
			p.AsWritten = append(p.AsWritten, w)
		}
		sort.Strings(p.AsWritten)
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Instances != out[j].Instances {
			return out[i].Instances > out[j].Instances
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// Definer is the Define step of extract, define, canonicalize.
type Definer struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
}

type wireDefinition struct {
	Definition string `json:"definition"`
}

// Instructions is the Define step system prompt.
func (d *Definer) Instructions() string {
	var b strings.Builder
	b.WriteString("Một mô hình đã tự đặt tên cho một loại quan hệ giữa hai khái niệm pháp lý, và dưới đây là các trường hợp nó đã dùng tên đó.\n")
	b.WriteString("Nhiệm vụ: viết đúng một câu định nghĩa cho loại quan hệ này.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. Định nghĩa phải viết theo dạng: X ... Y, trong đó X là khái niệm đứng trước và Y là khái niệm đứng sau.\n")
	b.WriteString("2. Định nghĩa phải nêu rõ chiều của quan hệ, đủ để người đọc biết bên nào là X.\n")
	b.WriteString("3. Chỉ một câu, không ví dụ, không giải thích thêm.\n")
	b.WriteString("4. Định nghĩa phải bao được tất cả các trường hợp nêu dưới đây, không chỉ trường hợp đầu tiên.\n\n")
	b.WriteString("Trả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"definition":"..."}`)
	b.WriteString("\n")
	return b.String()
}

// DefinitionPrompt renders one proposal.
func DefinitionPrompt(p Proposal) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tên quan hệ do mô hình đặt: %s\n", p.Label)
	fmt.Fprintf(&b, "Số lần xuất hiện: %d, trong %d văn bản\n\n", p.Instances, p.Docs)
	if len(p.AsWritten) > 0 {
		b.WriteString("Cách diễn đạt đã ghi nhận:\n")
		for _, w := range p.AsWritten {
			fmt.Fprintf(&b, "  %s\n", w)
		}
		b.WriteString("\n")
	}
	b.WriteString("Các trích dẫn:\n")
	for _, ev := range p.Examples {
		fmt.Fprintf(&b, "  [%s] %s\n", ev.ProvisionID, ev.Quote)
	}
	return b.String()
}

// Define writes the one sentence definition for a proposal.
func (d *Definer) Define(ctx context.Context, p Proposal) (string, api.Usage, error) {
	var usage api.Usage
	input := DefinitionPrompt(p)
	for attempt := 0; attempt <= maxCorrections(d.MaxCorrections); attempt++ {
		resp, err := d.Completer.Complete(ctx, api.Request{
			Model: d.Model, Instructions: d.Instructions(), Input: input,
		})
		if err != nil {
			return "", usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var wire wireDefinition
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			input = DefinitionPrompt(p) + "\nLần trả lời trước không phải một đối tượng JSON hợp lệ: " + err.Error() + "\nTrả lời lại.\n"
			continue
		}
		if strings.TrimSpace(wire.Definition) == "" {
			input = DefinitionPrompt(p) + "\nLần trả lời trước không có định nghĩa.\nTrả lời lại.\n"
			continue
		}
		return strings.TrimSpace(wire.Definition), usage, nil
	}
	return "", usage, fmt.Errorf("%s: no definition after %d corrections", p.Label, maxCorrections(d.MaxCorrections))
}

// Canonicalizer is the Canonicalize step: it decides whether a proposed
// relation is one the registry already has, by comparing definitions.
type Canonicalizer struct {
	Completer      api.Completer
	Model          string
	Registry       *Registry
	MaxCorrections int
}

type wireMatch struct {
	Match      string  `json:"match"`
	Rationale  string  `json:"rationale"`
	Confidence float64 `json:"confidence"`
}

// NoMatch is what the model returns when the registry has nothing for a
// proposal. It is a first class answer: the registry learning that it is
// incomplete is the whole point of the pass, and a model pushed to always
// choose something would report the registry as complete forever.
const NoMatch = "none"

// Instructions is the Canonicalize step system prompt.
func (c *Canonicalizer) Instructions() string {
	var b strings.Builder
	b.WriteString("Bạn được cho định nghĩa của một loại quan hệ mới và danh sách định nghĩa các loại quan hệ đã có.\n")
	b.WriteString("Nhiệm vụ: xác định quan hệ mới có trùng nghĩa với một quan hệ đã có hay không.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. So sánh theo định nghĩa, không so sánh theo tên. Hai tên khác nhau vẫn có thể cùng một nghĩa.\n")
	b.WriteString("2. Chiều của quan hệ phải trùng nhau. Nếu quan hệ mới đúng nghĩa nhưng ngược chiều thì không phải là trùng.\n")
	b.WriteString("3. Nếu không quan hệ nào trùng, trả về match là \"" + NoMatch + "\". Đây là câu trả lời hợp lệ và thường đúng.\n")
	b.WriteString("4. Chỉ chọn trong danh sách được liệt kê.\n\n")
	b.WriteString("Trả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"match":"REQUIRES","rationale":"...","confidence":0.9}`)
	b.WriteString("\n")
	return b.String()
}

// MatchPrompt renders one canonicalization question.
func MatchPrompt(p Proposal, r *Registry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Quan hệ mới: %s\n", p.Label)
	fmt.Fprintf(&b, "Định nghĩa: %s\n\n", p.Definition)
	b.WriteString("Các quan hệ đã có:\n")
	for _, t := range r.Types {
		fmt.Fprintf(&b, "  [%s] %s\n", t.ID, t.Definition)
	}
	return b.String()
}

// Canonicalize returns the registry type a proposal matches, or the empty
// string when it matches nothing.
func (c *Canonicalizer) Canonicalize(ctx context.Context, p Proposal) (string, string, api.Usage, error) {
	var usage api.Usage
	r := c.Registry
	if r == nil {
		r = SeedRegistry(1)
	}
	input := MatchPrompt(p, r)

	for attempt := 0; attempt <= maxCorrections(c.MaxCorrections); attempt++ {
		resp, err := c.Completer.Complete(ctx, api.Request{
			Model: c.Model, Instructions: c.Instructions(), Input: input,
		})
		if err != nil {
			return "", "", usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var wire wireMatch
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			input = MatchPrompt(p, r) + "\nLần trả lời trước không phải một đối tượng JSON hợp lệ: " + err.Error() + "\nTrả lời lại.\n"
			continue
		}
		if wire.Match == "" || strings.EqualFold(wire.Match, NoMatch) {
			return "", wire.Rationale, usage, nil
		}
		if r.Type(wire.Match) == nil {
			input = MatchPrompt(p, r) + fmt.Sprintf("\nLần trả lời trước bị từ chối: %q không nằm trong danh sách quan hệ đã có.\nTrả lời lại.\n", wire.Match)
			continue
		}
		return wire.Match, wire.Rationale, usage, nil
	}
	// Failing to get a usable answer is not a match. Leaving the proposal
	// provisional keeps it queryable and keeps it in the review queue, which is
	// where an undecidable case belongs.
	return "", "", usage, nil
}

// Apply rewrites edges according to the canonicalization outcome. A proposal
// that matched takes its registry type. One that did not stays exactly as it
// was: provisional, in the graph, queryable and visibly marked.
//
// Neither silently dropped nor silently promoted. Dropping loses the tail,
// which is where the interesting law is, and promoting without a person makes
// the registry meaningless, which is the only thing keeping the schema
// queryable.
//
// Matching a type is not corroboration and does not promote the edge. An edge
// one provision produced under an invented label is still an edge one provision
// produced after the label is corrected, and the corroboration gate lives in
// Fold, where the counting is. Promoting here would mint canonical edges on a
// single sighting, which is the invariant Validate exists to enforce.
func Apply(edges []Edge, proposals []Proposal, r *Registry) []Edge {
	matched := map[string]Proposal{}
	for _, p := range proposals {
		if p.MatchedTo != "" {
			matched[p.Label] = p
		}
	}
	out := make([]Edge, 0, len(edges))
	for _, e := range edges {
		if e.Status == StatusProvisional {
			if p, ok := matched[e.Type]; ok {
				e.Type = p.MatchedTo
				e.Definition = ""
				e.OntologyVersion = r.Version
				// The type is decided, so the reason it is still provisional is
				// no longer the type.
				e.Why = WhySingleSupport
			}
		}
		out = append(out, e)
	}
	Sort(out)
	return out
}
