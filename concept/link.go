package concept

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/law"
)

// Pass D connects the corpus text to the concept layer.
//
// Definitions are tens of thousands and mentions are tens of millions, so this
// is where cost control lives and where determinism earns its keep. The rule is
// the same one the whole project runs on, applied to a much bigger surface: the
// cases the statute already decided are decided by code, the cases that need
// judgement are scored so that a model is paid for only the ones that need it,
// and a mention nothing settles is recorded as unsettled.
//
// An unresolved mention is correct output. A confidently wrong link is a
// defect. That asymmetry is the same one cite.Index applies when it refuses to
// resolve a decision number that two provinces both issued.

// Mention is one occurrence of a term's surface form in a provision.
type Mention struct {
	ProvisionID string `json:"provision_id"`
	DocID       string `json:"doc_id"`
	Surface     string `json:"surface"`
	CharStart   int    `json:"char_start"`
	CharEnd     int    `json:"char_end"`
	// TermUseID is the resolved target, empty when nothing was settled.
	TermUseID string  `json:"term_use_id,omitempty"`
	Method    string  `json:"method"` // in_scope, scored, adjudicated, unresolved
	Score     float64 `json:"score,omitempty"`
	// Candidates is what was in play. It is kept on resolved mentions too,
	// because the second best candidate is the thing a reviewer wants to see
	// when a link looks wrong.
	Candidates []MentionCandidate `json:"candidates,omitempty"`
}

// MentionCandidate is one term use a mention could have meant, with the reason
// the score is what it is. The signals are listed rather than folded into the
// number alone, so a link can be explained without rerunning the scorer.
type MentionCandidate struct {
	TermUseID string   `json:"term_use_id"`
	Score     float64  `json:"score"`
	Signals   []string `json:"signals"`
}

// The methods a mention can be resolved by.
const (
	MethodInScope     = "in_scope"
	MethodScored      = "scored"
	MethodAdjudicated = "adjudicated"
	MethodUnresolved  = "unresolved"
)

// The signals, in the order they are worth anything here.
const (
	SignalDefinedHere = "defined_in_this_instrument"
	SignalCited       = "this_document_cites_the_definer"
	SignalHierarchy   = "implements_the_definer"
	SignalSubject     = "shares_a_subject_subdomain"
	SignalInForce     = "definition_in_force_on_this_date"
)

// Weights are what each signal contributes. They are a reviewable parameter and
// not a tuned model, and the ordering matters more than the values: the
// citation graph outranks everything else because a provincial decision that
// cites the Labour Code and then says nguoi lao dong almost certainly means the
// Labour Code's term, and that signal is free because pass L1 already built
// seven hundred thousand citation edges.
var Weights = map[string]float64{
	SignalCited:     0.5,
	SignalHierarchy: 0.25,
	SignalSubject:   0.15,
	SignalInForce:   0.1,
}

// AdjudicationMargin is how close the top two candidates have to be before a
// model is asked. Below this the scoring settles it; at or under it the pair
// goes to adjudication. The whole point of the scoring is to make the model
// call rare enough to afford.
const AdjudicationMargin = 0.15

// ResolveThreshold is the score a single candidate has to reach to be linked
// without adjudication. A candidate that reaches nothing links to nothing.
const ResolveThreshold = 0.3

// Corpus is the free context the scorer needs, all of it computed by earlier
// passes. Every map may be empty: a missing signal contributes nothing rather
// than counting against a candidate, because absent evidence and negative
// evidence are different things.
type Corpus struct {
	// Cites maps a document to the documents it cites.
	Cites map[string]map[string]bool
	// Implements maps a document to the document it implements, which for a
	// circular under a decree is the decree. A circular inherits the decree's
	// vocabulary.
	Implements map[string]string
	// Subdomains maps a document to its subject subdomains.
	Subdomains map[string][]string
	// EffectiveFrom maps a document to the date it took effect, in the corpus
	// date format. A definition that was not in force when the citing document
	// was issued is not a candidate for it.
	EffectiveFrom map[string]string
}

// Index is the surface form lookup a mention scan runs against. It is built
// once over the term uses and reused across the corpus.
type Index struct {
	// bySurface maps a folded surface form to the term uses that carry it,
	// whether as label or as declared alias.
	bySurface map[string][]string
	byID      map[string]*TermUse
	// surfaces are the distinct raw spellings to scan for, longest first, so
	// that nguoi su dung lao dong is found before nguoi lao dong inside it.
	surfaces []string
}

// NewIndex builds the surface index.
func NewIndex(terms []TermUse) *Index {
	ix := &Index{bySurface: map[string][]string{}, byID: map[string]*TermUse{}}
	raw := map[string]bool{}
	for i := range terms {
		t := &terms[i]
		ix.byID[t.ID] = t
		for _, s := range append([]string{t.LabelVI}, t.Aliases...) {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			key := law.Slug(s)
			if key == "" {
				continue
			}
			ix.bySurface[key] = append(ix.bySurface[key], t.ID)
			raw[s] = true
		}
	}
	for s := range raw {
		ix.surfaces = append(ix.surfaces, s)
	}
	sort.Slice(ix.surfaces, func(i, j int) bool {
		if len(ix.surfaces[i]) != len(ix.surfaces[j]) {
			return len(ix.surfaces[i]) > len(ix.surfaces[j])
		}
		return ix.surfaces[i] < ix.surfaces[j]
	})
	for key := range ix.bySurface {
		sort.Strings(ix.bySurface[key])
	}
	return ix
}

// Scan finds the surface forms occurring in one provision. Overlaps are
// resolved longest first and a span is claimed once, so a sentence containing
// nguoi su dung lao dong does not also produce a mention of nguoi lao dong
// sitting inside it.
func (ix *Index) Scan(docID, provisionID, text string) []Mention {
	claimed := make([]bool, len(text)+1)
	var out []Mention
	for _, surface := range ix.surfaces {
		from := 0
		for {
			i := strings.Index(text[from:], surface)
			if i < 0 {
				break
			}
			start := from + i
			end := start + len(surface)
			from = start + 1
			if overlaps(claimed, start, end) {
				continue
			}
			if !wordBoundary(text, start, end) {
				continue
			}
			for k := start; k < end; k++ {
				claimed[k] = true
			}
			out = append(out, Mention{
				ProvisionID: provisionID, DocID: docID, Surface: surface,
				CharStart: start, CharEnd: end, Method: MethodUnresolved,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CharStart < out[j].CharStart })
	return out
}

func overlaps(claimed []bool, start, end int) bool {
	for i := start; i < end; i++ {
		if claimed[i] {
			return true
		}
	}
	return false
}

// wordBoundary rejects a match that is part of a longer word. Vietnamese is
// written with spaces between syllables, so a term boundary is a boundary
// between syllables, and the test is whether the neighbouring byte is a letter
// or a digit.
func wordBoundary(text string, start, end int) bool {
	if start > 0 && isWordByte(text[start-1]) {
		return false
	}
	if end < len(text) && isWordByte(text[end]) {
		return false
	}
	return true
}

func isWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b >= 0x80:
		// A continuation or lead byte of a multibyte rune. Vietnamese letters
		// are all multibyte, so this is the case that matters most.
		return true
	}
	return false
}

// Resolve scores one mention's candidates and decides, or declines to.
//
// D1 is the first branch and it is not an optimisation. A term matched inside
// the instrument whose scoping clause defined it is a mention of that term use
// at near certainty, because Trong Luat nay says so. Code does that because the
// answer is stated in the text, and there is nothing there to understand.
func (ix *Index) Resolve(m *Mention, scopeOfDoc string, c Corpus, docDate string) {
	ids := ix.bySurface[law.Slug(m.Surface)]
	if len(ids) == 0 {
		m.Method = MethodUnresolved
		return
	}

	for _, id := range ids {
		if t := ix.byID[id]; t != nil && (t.ScopeID == scopeOfDoc || t.DocID == m.DocID) {
			m.TermUseID = id
			m.Method = MethodInScope
			m.Score = 1
			return
		}
	}

	m.Candidates = nil
	for _, id := range ids {
		t := ix.byID[id]
		if t == nil {
			continue
		}
		cand := MentionCandidate{TermUseID: id}
		if c.Cites[m.DocID][t.DocID] {
			cand.Score += Weights[SignalCited]
			cand.Signals = append(cand.Signals, SignalCited)
		}
		if c.Implements[m.DocID] == t.DocID {
			cand.Score += Weights[SignalHierarchy]
			cand.Signals = append(cand.Signals, SignalHierarchy)
		}
		if sharesSubdomain(c.Subdomains[m.DocID], c.Subdomains[t.DocID]) {
			cand.Score += Weights[SignalSubject]
			cand.Signals = append(cand.Signals, SignalSubject)
		}
		switch {
		case docDate == "" || c.EffectiveFrom[t.DocID] == "":
			// Unknown, so it neither helps nor rules out.
		case c.EffectiveFrom[t.DocID] <= docDate:
			cand.Score += Weights[SignalInForce]
			cand.Signals = append(cand.Signals, SignalInForce)
		default:
			// A definition that did not exist yet cannot be what the drafter
			// meant. This is the one signal that removes a candidate outright.
			continue
		}
		m.Candidates = append(m.Candidates, cand)
	}
	sort.Slice(m.Candidates, func(i, j int) bool {
		if m.Candidates[i].Score != m.Candidates[j].Score {
			return m.Candidates[i].Score > m.Candidates[j].Score
		}
		return m.Candidates[i].TermUseID < m.Candidates[j].TermUseID
	})

	switch {
	case len(m.Candidates) == 0:
		m.Method = MethodUnresolved
	case len(m.Candidates) > 1 && m.Candidates[0].Score-m.Candidates[1].Score <= AdjudicationMargin:
		// Close enough that the signals do not settle it. This is the pile that
		// goes to the model, and keeping it small is what the scoring is for.
		m.Method = MethodUnresolved
	case m.Candidates[0].Score >= ResolveThreshold:
		m.TermUseID = m.Candidates[0].TermUseID
		m.Score = m.Candidates[0].Score
		m.Method = MethodScored
	default:
		m.Method = MethodUnresolved
	}
}

// NeedsAdjudication reports whether a mention is one of the close calls worth
// paying a model for: more than one candidate, none of them settled it.
func NeedsAdjudication(m *Mention) bool {
	return m.Method == MethodUnresolved && len(m.Candidates) > 1
}

func sharesSubdomain(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	in := map[string]bool{}
	for _, s := range a {
		in[s] = true
	}
	for _, s := range b {
		if in[s] {
			return true
		}
	}
	return false
}

// Adjudicator is D4: the model decides which sense is in play, given both
// definitions and the text around the mention. This is a hard comprehension
// task and it is exactly what a model is worth paying for. Everything above it
// exists so that it runs on the close calls and nothing else.
type Adjudicator struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
}

type wireAdjudication struct {
	TermUseID  string  `json:"term_use_id"`
	Neither    bool    `json:"neither"`
	Rationale  string  `json:"rationale"`
	Confidence float64 `json:"confidence"`
}

// Instructions is the pass D4 system prompt.
func (a *Adjudicator) Instructions() string {
	var b strings.Builder
	b.WriteString("Một cụm từ xuất hiện trong một điều khoản. Nhiều văn bản khác nhau định nghĩa cụm từ này theo những cách khác nhau.\n")
	b.WriteString("Nhiệm vụ: xác định điều khoản này đang dùng cụm từ theo nghĩa của văn bản nào.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. Chỉ chọn trong các phương án được liệt kê, trả về đúng term_use_id của phương án đó.\n")
	b.WriteString("2. Nếu điều khoản dùng cụm từ theo một nghĩa khác hẳn, hoặc không đủ căn cứ để chọn, thì đặt neither là true. Đây là câu trả lời hợp lệ và đúng đắn.\n")
	b.WriteString("3. rationale phải nêu chi tiết trong điều khoản làm căn cứ cho lựa chọn.\n")
	b.WriteString("4. Không suy đoán ngoài nội dung được cung cấp.\n\n")
	b.WriteString("Trả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"term_use_id":"...","neither":false,"rationale":"...","confidence":0.8}`)
	b.WriteString("\n")
	return b.String()
}

// AdjudicationPrompt renders one close call.
func AdjudicationPrompt(m *Mention, context string, ix *Index) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Cụm từ: %s\n", m.Surface)
	fmt.Fprintf(&b, "Điều khoản: %s\n\n", m.ProvisionID)
	fmt.Fprintf(&b, "Nội dung điều khoản:\n%s\n\n", context)
	b.WriteString("Các phương án:\n")
	for _, c := range m.Candidates {
		t := ix.byID[c.TermUseID]
		if t == nil {
			continue
		}
		fmt.Fprintf(&b, "\n[%s] văn bản %s\n%s\n", c.TermUseID, t.DocID, t.DefinitionVI)
	}
	return b.String()
}

// Adjudicate asks the model. A mention it declines comes back unresolved, which
// is the correct outcome and not a failure.
func (a *Adjudicator) Adjudicate(ctx context.Context, m *Mention, text string, ix *Index) (api.Usage, error) {
	var usage api.Usage
	allowed := map[string]bool{}
	for _, c := range m.Candidates {
		allowed[c.TermUseID] = true
	}
	input := AdjudicationPrompt(m, text, ix)

	for attempt := 0; attempt <= maxCorrections(a.MaxCorrections); attempt++ {
		resp, err := a.Completer.Complete(ctx, api.Request{
			Model: a.Model, Instructions: a.Instructions(), Input: input,
		})
		if err != nil {
			return usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var wire wireAdjudication
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			input = AdjudicationPrompt(m, text, ix) + correction("câu trả lời không phải một đối tượng JSON hợp lệ: "+err.Error())
			continue
		}
		if wire.Neither {
			m.Method = MethodUnresolved
			m.TermUseID = ""
			return usage, nil
		}
		if !allowed[wire.TermUseID] {
			input = AdjudicationPrompt(m, text, ix) +
				correction(fmt.Sprintf("term_use_id %q không nằm trong các phương án được liệt kê", wire.TermUseID))
			continue
		}
		m.TermUseID = wire.TermUseID
		m.Method = MethodAdjudicated
		m.Score = wire.Confidence
		return usage, nil
	}
	m.Method = MethodUnresolved
	m.TermUseID = ""
	return usage, nil
}

// MentionReport is what a linking run produced, per document, so coverage can
// count unresolved mentions as work rather than as silence.
type MentionReport struct {
	DocID      string    `json:"doc_id"`
	Mentions   []Mention `json:"mentions"`
	Resolved   int       `json:"resolved"`
	Unresolved int       `json:"unresolved"`
	LinkedAt   time.Time `json:"linked_at"`
}

// Summarize counts a report's outcomes.
func Summarize(r *MentionReport) {
	r.Resolved, r.Unresolved = 0, 0
	for i := range r.Mentions {
		if r.Mentions[i].TermUseID == "" {
			r.Unresolved++
		} else {
			r.Resolved++
		}
	}
}
