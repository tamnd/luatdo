package relation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/law"
)

// Resolver turns a phrase as a drafter wrote it into the identifier of a term
// use the layer already holds.
//
// It resolves inside the defining instrument first and corpus wide only when
// exactly one candidate exists, which is the same rule the mention linker uses
// and for the same reason: a phrase that several instruments define means
// several things, and picking one of them by string match is how a graph
// acquires confident wrong edges.
type Resolver struct {
	inScope map[string]string   // scope + folded label
	global  map[string][]string // folded label
}

// NewResolver indexes term uses by folded label.
func NewResolver(terms []concept.TermUse) *Resolver {
	r := &Resolver{inScope: map[string]string{}, global: map[string][]string{}}
	for _, t := range terms {
		key := law.Slug(t.LabelVI)
		if key == "" {
			continue
		}
		r.inScope[t.ScopeID+"|"+key] = t.ID
		r.global[key] = append(r.global[key], t.ID)
		for _, a := range t.Aliases {
			ak := law.Slug(a)
			if ak == "" {
				continue
			}
			r.inScope[t.ScopeID+"|"+ak] = t.ID
			r.global[ak] = append(r.global[ak], t.ID)
		}
	}
	for k := range r.global {
		sort.Strings(r.global[k])
		r.global[k] = dedupe(r.global[k])
	}
	return r
}

// Resolve returns the identifier a phrase names, and whether it is resolvable at
// all. An unresolved phrase produces no edge, which is the correct outcome: an
// edge to a concept nobody can name is not queryable and not checkable.
func (r *Resolver) Resolve(scopeID, phrase string) (string, bool) {
	key := law.Slug(phrase)
	if key == "" {
		return "", false
	}
	if id, ok := r.inScope[scopeID+"|"+key]; ok {
		return id, true
	}
	if ids := r.global[key]; len(ids) == 1 {
		return ids[0], true
	}
	return "", false
}

func dedupe(in []string) []string {
	out := in[:0]
	var last string
	for i, s := range in {
		if i == 0 || s != last {
			out = append(out, s)
		}
		last = s
	}
	return out
}

// Definitional derives the relations the drafter wrote, with no model involved.
//
// Pass B already returned genus and enumerated subtypes for every defined term,
// with both verified against the clause. A genus is a BROADER edge and an
// enumerated subtype is a BROADER edge pointing the other way, and getting them
// out costs one loop rather than a corpus of model calls. They are the most
// reliable relations in the graph because a person drafting a statute wrote
// them, which is also why they are the only ones allowed to be canonical on a
// single provision.
//
// They are derived rather than stored: a term use whose definition changes gets
// its edges rebuilt from the new reading, because a BROADER edge inherited from
// a genus that was corrected would otherwise outlive the mistake it came from.
func Definitional(terms []concept.TermUse, r *Resolver) []Edge {
	var out []Edge
	for _, t := range terms {
		if t.DefinedBy == "" || t.Quote == "" {
			continue
		}
		ev := Evidence{
			ProvisionID: t.DefinedBy,
			Quote:       t.Quote,
			CharStart:   t.CharStart,
			CharEnd:     t.CharEnd,
			DocID:       t.DocID,
			AsWritten:   t.Genus,
		}
		if t.Genus != "" {
			if id, ok := r.Resolve(t.ScopeID, t.Genus); ok && id != t.ID {
				out = append(out, Edge{
					FromID: t.ID, ToID: id, Type: Broader,
					Status: StatusCanonical, Source: SourceDefinitional,
					Evidence: []Evidence{ev}, SupportCount: 1, SupportDocs: 1,
					Confidence: t.Confidence,
				})
			}
		}
		for _, sub := range t.EnumeratedSubtypes {
			id, ok := r.Resolve(t.ScopeID, sub)
			if !ok || id == t.ID {
				continue
			}
			e := ev
			e.AsWritten = sub
			out = append(out, Edge{
				FromID: id, ToID: t.ID, Type: Broader,
				Status: StatusCanonical, Source: SourceDefinitional,
				Evidence: []Evidence{e}, SupportCount: 1, SupportDocs: 1,
				Confidence: t.Confidence,
			})
		}
	}
	Sort(out)
	return out
}

// Candidate is one concept offered to the model for a provision. The candidate
// set is bounded on purpose: the model is asked to state the relations a
// provision shows between concepts somebody else already found, which is a
// comprehension task, rather than to find concepts and relations at once, which
// is two tasks and fails at both.
type Candidate struct {
	ID      string
	LabelVI string
	Kind    string
}

// Extractor is source 4.2, the provision level read.
type Extractor struct {
	Completer      api.Completer
	Model          string
	Registry       *Registry
	MaxCorrections int
}

type wireRelation struct {
	Subject           string  `json:"subject_concept"`
	Relation          string  `json:"relation"`
	RelationAsWritten string  `json:"relation_as_written"`
	Object            string  `json:"object_concept"`
	Quote             string  `json:"quote"`
	CharStart         int     `json:"char_start"`
	CharEnd           int     `json:"char_end"`
	DirectionCheck    string  `json:"direction_check"`
	Confidence        float64 `json:"confidence"`
}

type wireExtraction struct {
	Relations []wireRelation `json:"relations"`
}

// Instructions is the provision level system prompt.
func (x *Extractor) Instructions() string {
	var b strings.Builder
	b.WriteString("Bạn được cho một điều khoản và danh sách các khái niệm đã được xác định trong điều khoản đó.\n")
	b.WriteString("Nhiệm vụ: nêu các quan hệ mà chính điều khoản này thể hiện giữa các khái niệm đó.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. subject_concept và object_concept phải là mã của hai khái niệm khác nhau trong danh sách. Không được nêu khái niệm ngoài danh sách.\n")
	b.WriteString("2. quote phải sao chép nguyên văn từ nội dung điều khoản, và char_start, char_end là vị trí byte thật của đoạn trích đó.\n")
	b.WriteString("3. relation nên chọn trong danh sách quan hệ có sẵn nếu đúng nghĩa. Nếu không quan hệ nào đúng, hãy tự đặt tên quan hệ bằng chữ in hoa và dấu gạch dưới, và mô tả nó ở relation_as_written.\n")
	b.WriteString("4. Không dùng những quan hệ chung chung như LIEN_QUAN hay RELATED_TO. Nếu điều khoản không thể hiện quan hệ cụ thể nào, hãy trả về danh sách rỗng. Danh sách rỗng là câu trả lời đúng.\n")
	b.WriteString("5. direction_check phải viết bằng lời một câu nói rõ chiều của quan hệ, ví dụ: giấy phép cần giấy chứng nhận, không phải ngược lại.\n")
	b.WriteString("6. Không suy đoán ngoài nội dung điều khoản. Chỉ nêu quan hệ mà câu chữ trong điều khoản thể hiện.\n\n")
	b.WriteString("Các quan hệ có sẵn:\n")
	for _, t := range x.registry().Types {
		fmt.Fprintf(&b, "  %s: %s\n", t.ID, t.Definition)
	}
	b.WriteString("\nTrả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"relations":[{"subject_concept":"...","relation":"REQUIRES","relation_as_written":"...","object_concept":"...","quote":"...","char_start":0,"char_end":0,"direction_check":"...","confidence":0.9}]}`)
	b.WriteString("\n")
	return b.String()
}

func (x *Extractor) registry() *Registry {
	if x.Registry != nil {
		return x.Registry
	}
	return SeedRegistry(1)
}

// Prompt renders one provision and its candidate concepts.
func Prompt(provisionID, text string, cands []Candidate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Điều khoản: %s\n\n", provisionID)
	fmt.Fprintf(&b, "Nội dung:\n%s\n\n", text)
	b.WriteString("Các khái niệm trong điều khoản:\n")
	for _, c := range cands {
		fmt.Fprintf(&b, "  [%s] %s (%s)\n", c.ID, c.LabelVI, c.Kind)
	}
	return b.String()
}

// Extract reads one provision. It returns the edges the provision supports,
// each with a quote checked byte for byte, and each between two concepts that
// were offered. A provision showing nothing specific returns nothing.
func (x *Extractor) Extract(ctx context.Context, provisionID, docID, text string, cands []Candidate) ([]Edge, api.Usage, error) {
	var usage api.Usage
	if len(cands) < 2 {
		// One concept cannot relate to anything, and calling a model to be told
		// so is paying for a foregone conclusion.
		return nil, usage, nil
	}
	offered := map[string]Candidate{}
	for _, c := range cands {
		offered[c.ID] = c
	}
	input := Prompt(provisionID, text, cands)

	for attempt := 0; attempt <= maxCorrections(x.MaxCorrections); attempt++ {
		resp, err := x.Completer.Complete(ctx, api.Request{
			Model: x.Model, Instructions: x.Instructions(), Input: input,
		})
		if err != nil {
			return nil, usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var wire wireExtraction
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			input = Prompt(provisionID, text, cands) + correction("câu trả lời không phải một đối tượng JSON hợp lệ: "+err.Error())
			continue
		}
		edges, problem := x.convert(wire, provisionID, docID, text, offered)
		if problem != "" {
			input = Prompt(provisionID, text, cands) + correction(problem)
			continue
		}
		Sort(edges)
		return edges, usage, nil
	}
	return nil, usage, fmt.Errorf("%s: the model did not produce a usable answer after %d corrections", provisionID, maxCorrections(x.MaxCorrections))
}

// convert turns the wire form into edges, or names the first problem so the
// model can be told what was wrong rather than being asked again identically.
func (x *Extractor) convert(wire wireExtraction, provisionID, docID, text string, offered map[string]Candidate) ([]Edge, string) {
	r := x.registry()
	var out []Edge
	seen := map[string]bool{}
	for _, w := range wire.Relations {
		if _, ok := offered[w.Subject]; !ok {
			return nil, fmt.Sprintf("subject_concept %q không nằm trong danh sách khái niệm được cung cấp", w.Subject)
		}
		if _, ok := offered[w.Object]; !ok {
			return nil, fmt.Sprintf("object_concept %q không nằm trong danh sách khái niệm được cung cấp", w.Object)
		}
		if w.Subject == w.Object {
			return nil, fmt.Sprintf("quan hệ nối %q với chính nó", w.Subject)
		}
		typ := strings.ToUpper(strings.TrimSpace(w.Relation))
		if typ == "" {
			return nil, "một quan hệ không có tên"
		}
		if Forbidden[typ] {
			return nil, fmt.Sprintf("%s là quan hệ chung chung, không được dùng. Nếu không có quan hệ cụ thể thì trả về danh sách rỗng", typ)
		}
		if err := checkQuote(text, w.Quote, w.CharStart, w.CharEnd); err != nil {
			return nil, fmt.Sprintf("đoạn trích %q: %v", w.Quote, err)
		}
		if strings.TrimSpace(w.DirectionCheck) == "" {
			return nil, fmt.Sprintf("quan hệ %s thiếu direction_check", typ)
		}
		if w.Confidence < 0 || w.Confidence > 1 {
			return nil, fmt.Sprintf("confidence %v nằm ngoài khoảng 0 đến 1", w.Confidence)
		}

		e := Edge{
			FromID: w.Subject, ToID: w.Object, Type: typ,
			Source:     SourceProvision,
			Confidence: w.Confidence,
			Evidence: []Evidence{{
				ProvisionID: provisionID, DocID: docID,
				Quote: w.Quote, CharStart: w.CharStart, CharEnd: w.CharEnd,
				AsWritten: w.RelationAsWritten, DirectionCheck: w.DirectionCheck,
			}},
			SupportCount: 1, SupportDocs: 1,
		}
		// Nothing read out of one provision is canonical, whatever the registry
		// says about its type and whatever confidence the model attached. One
		// sighting is one sentence, and promoting on it is the single cheapest
		// way to put a confident hallucination in the graph. Folding across
		// provisions is what promotes, and it happens later with the counting in
		// front of it.
		e.Status = StatusProvisional
		if r.Type(typ) == nil {
			// A type the registry does not hold carries the model's own words
			// forward, which is what the Define step needs. It is not dropped:
			// the tail is where the interesting law is.
			e.Why = WhyUnknownType
			e.Definition = strings.TrimSpace(w.RelationAsWritten)
			if e.Definition == "" {
				return nil, fmt.Sprintf("quan hệ %s không có trong danh sách nên bắt buộc phải mô tả ở relation_as_written", typ)
			}
		} else {
			e.Why = WhySingleSupport
		}
		e.OntologyVersion = r.Version

		if seen[e.Key()] {
			continue
		}
		seen[e.Key()] = true
		out = append(out, e)
	}
	return out, ""
}

func checkQuote(text, quote string, start, end int) error {
	if quote == "" {
		return fmt.Errorf("rỗng")
	}
	if start < 0 || end > len(text) || start >= end {
		return fmt.Errorf("vị trí %d đến %d nằm ngoài đoạn văn bản dài %d byte", start, end, len(text))
	}
	if text[start:end] != quote {
		if !strings.Contains(text, quote) {
			return fmt.Errorf("không có trong nội dung điều khoản")
		}
		return fmt.Errorf("có trong nội dung nhưng không nằm ở vị trí %d đến %d", start, end)
	}
	return nil
}

func correction(problem string) string {
	return "\nLần trả lời trước bị từ chối: " + problem +
		"\nMọi đoạn trích phải sao chép nguyên văn từ nội dung điều khoản ở trên, và char_start, char_end phải là vị trí byte thật của đoạn trích đó." +
		"\nTrả lời lại, chỉ một đối tượng JSON.\n"
}

func maxCorrections(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

func addUsage(a, b api.Usage) api.Usage {
	a.InputTokens += b.InputTokens
	a.CachedInputTokens += b.CachedInputTokens
	a.CacheWriteTokens += b.CacheWriteTokens
	a.OutputTokens += b.OutputTokens
	a.ReasoningTokens += b.ReasoningTokens
	a.TotalTokens += b.TotalTokens
	return a
}
