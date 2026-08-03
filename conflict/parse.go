package conflict

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
)

// Parser is the only place in this package a model is called, and it is called
// with one statement.
//
// The job is narrow by construction. The statement has already been extracted,
// judged, falsified and stored, so nothing here decides what the provision says.
// What is left is wording: the same duty appears as "người sử dụng lao động
// phải thông báo", "đơn vị sử dụng lao động có trách nhiệm báo" and "bên sử
// dụng lao động thông báo bằng văn bản", and a comparison over those three
// strings finds nothing. Turning them into one party and one act is a
// comprehension task, which is what a model is for, and it is the whole of what
// this one is asked.
//
// It is never shown a second statement. A model that sees a pair will produce a
// verdict about the pair whether or not there is one to produce, and every
// survey paper that tried it reported the same overconfidence. Pairing happens
// in check.go, over strings, with no model anywhere near it.
type Parser struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
}

// Instructions is the system prompt. It is fixed text so a provider can cache
// the prefix across the corpus, which on 677 comparable statements is most of
// the input tokens.
const parseInstructions = `Bạn được cho một phát biểu quy phạm đã được trích xuất và kiểm chứng từ một điều khoản luật Việt Nam.
Nhiệm vụ duy nhất: viết lại các thành phần của phát biểu ở dạng chuẩn hóa, ngắn gọn, không kèm giải thích.

Chuẩn hóa nghĩa là gì:
- Dùng danh từ hoặc cụm động từ chung nhất mà vẫn đúng nghĩa, bỏ các chữ như "phải", "có trách nhiệm", "được quyền", vì phần bắt buộc hay cấm đoán đã được ghi ở chỗ khác.
- Hai điều khoản nói cùng một việc bằng lời khác nhau phải cho ra cùng một chuỗi. "thông báo cho người lao động biết" và "báo cho người lao động" đều là "thông báo".
- Giữ nguyên thuật ngữ pháp lý. Không đổi "người sử dụng lao động" thành "công ty".
- Mỗi trường chỉ vài từ. Không chép lại cả câu.

Các trường:
- party: chủ thể mang nghĩa vụ hoặc quyền, ở dạng chuẩn. Nếu phát biểu không nêu chủ thể thì để chuỗi rỗng.
- act: hành vi, ở dạng cụm động từ, không kèm đối tượng của hành vi.
- object: đối tượng của hành vi, hoặc chuỗi rỗng.
- toward: bên còn lại mà hành vi hướng tới, hoặc chuỗi rỗng.
- conditions: mỗi điều kiện trong phát biểu viết lại thành một cụm ngắn, đúng thứ tự, đúng số lượng.
- exceptions: mỗi ngoại lệ trong phát biểu viết lại thành một cụm ngắn, đúng thứ tự, đúng số lượng.

Không thêm điều kiện hay ngoại lệ mà phát biểu không có. Không suy đoán ngoài phát biểu.
Trả về đúng một đối tượng JSON, không giải thích, theo dạng:
{"party":"...","act":"...","object":"...","toward":"...","conditions":["..."],"exceptions":["..."]}`

// Instructions is the system prompt this pass sends.
func (p *Parser) Instructions() string { return parseInstructions }

type wireForm struct {
	Party      string   `json:"party"`
	Act        string   `json:"act"`
	Object     string   `json:"object"`
	Toward     string   `json:"toward"`
	Conditions []string `json:"conditions"`
	Exceptions []string `json:"exceptions"`
}

// ParsePrompt renders one statement for the parser.
//
// The provision text goes in with it. The statement's own quote is the span that
// licensed it and is often a fragment of a sentence, and a fragment is where a
// negation gets lost. The parser is not asked to re-read the provision, but it
// is allowed to see what the fragment came from.
func ParsePrompt(r *norm.Record, provisionText string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Điều khoản: %s\n", r.ProvisionID)
	if provisionText != "" {
		fmt.Fprintf(&b, "\nNội dung điều khoản:\n%s\n", provisionText)
	}
	s := &r.Statement
	fmt.Fprintf(&b, "\nPhát biểu:\n")
	fmt.Fprintf(&b, "  loại: %s\n", s.Type)
	if s.Bearer != nil {
		fmt.Fprintf(&b, "  chủ thể: %s\n", s.Bearer.Text)
	}
	fmt.Fprintf(&b, "  hành vi: %s\n", s.Action.Text)
	if s.Object != nil {
		fmt.Fprintf(&b, "  đối tượng: %s\n", s.Object.Text)
	}
	if s.Counterparty != nil {
		fmt.Fprintf(&b, "  bên liên quan: %s\n", s.Counterparty.Text)
	}
	for i, c := range s.Conditions {
		fmt.Fprintf(&b, "  điều kiện %d (%s): %s\n", i+1, c.Kind, c.Text)
	}
	for i, e := range s.Exceptions {
		fmt.Fprintf(&b, "  ngoại lệ %d (%s): %s\n", i+1, e.Kind, e.Text)
	}
	fmt.Fprintf(&b, "  trích dẫn: %s\n", s.Evidence.Quote)
	return b.String()
}

// Parse maps one trusted record onto a Form.
//
// It reports (nil, usage, nil) for a record that states no modality, which is a
// definition, a procedure step or an exception. Those are a third of the trusted
// store and none of them can conflict with anything, so no call is made for
// them.
func (p *Parser) Parse(ctx context.Context, r *norm.Record, provisionText string) (*Form, api.Usage, error) {
	var usage api.Usage
	f, ok := Draft(r)
	if !ok {
		return nil, usage, nil
	}
	input := ParsePrompt(r, provisionText)
	problem := ""
	for attempt := 0; attempt <= maxCorrections(p.MaxCorrections); attempt++ {
		resp, err := p.Completer.Complete(ctx, api.Request{
			Model: p.Model, Instructions: p.Instructions(), Input: input,
		})
		if err != nil {
			return nil, usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var wire wireForm
		if jerr := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); jerr != nil {
			problem = "câu trả lời không phải một đối tượng JSON hợp lệ: " + jerr.Error()
			input = ParsePrompt(r, provisionText) + correction(problem)
			continue
		}
		if problem = check(wire, &r.Statement); problem != "" {
			input = ParsePrompt(r, provisionText) + correction(problem)
			continue
		}
		// Verbosity is asked about and not insisted on. See verbose.
		if long := verbose(wire); long != "" && attempt < maxCorrections(p.MaxCorrections) {
			problem = long
			input = ParsePrompt(r, provisionText) + correction(long)
			continue
		}
		apply(f, wire)
		return f, usage, nil
	}
	return nil, usage, fmt.Errorf("%s: the model did not produce a usable form after %d corrections: %s",
		r.ID, maxCorrections(p.MaxCorrections), problem)
}

// The bounds a canonical phrase has to stay inside.
//
// Both are generous and both fire. A canonical act is one to four words, and an
// answer of fifteen is the model having copied the sentence instead of naming
// the act, which is the failure this pass exists to avoid: a form whose act is
// the whole clause pairs with nothing and is silently absent from every
// comparison.
//
// The clause bound is looser than the act bound and it is set from the corpus
// rather than from taste. The conditions and exceptions the extraction already
// verified run to a median of 11 words, and four in ten of them are longer than
// twelve, which is where this bound started. Asking a model to name a 24 word
// condition in twelve words asks it to drop half of what the condition says,
// and it correctly refused: an earlier run of this pass lost five documents of
// nine to that refusal after paying for the calls.
const (
	maxActWords    = 8
	maxClauseWords = 20
)

// check validates one answer against the statement it came from, and names the
// first problem in the words the model will be told.
//
// The condition and exception counts are the load bearing check. A parser free
// to invent a condition can make any pair non-comparable, and a parser free to
// drop one makes every pair comparable: both fail silently and in opposite
// directions. Requiring one canonical atom per clause the extractor already
// verified keeps the count fixed and leaves only the wording to the model.
func check(w wireForm, s *norm.Statement) string {
	if s.Bearer == nil {
		if strings.TrimSpace(w.Party) != "" {
			return fmt.Sprintf("phát biểu không nêu chủ thể nhưng câu trả lời ghi party là %q", w.Party)
		}
	} else if strings.TrimSpace(w.Party) == "" {
		return fmt.Sprintf("phát biểu nêu chủ thể %q nhưng câu trả lời để party rỗng", s.Bearer.Text)
	}
	if strings.TrimSpace(w.Act) == "" {
		return "act rỗng"
	}
	if n := len(strings.Fields(w.Act)); n > maxActWords {
		return fmt.Sprintf("act dài %d từ, phải là cụm động từ ngắn chứ không phải cả câu", n)
	}
	if len(w.Conditions) != len(s.Conditions) {
		return fmt.Sprintf("phát biểu có %d điều kiện nhưng câu trả lời ghi %d", len(s.Conditions), len(w.Conditions))
	}
	if len(w.Exceptions) != len(s.Exceptions) {
		return fmt.Sprintf("phát biểu có %d ngoại lệ nhưng câu trả lời ghi %d", len(s.Exceptions), len(w.Exceptions))
	}
	for _, list := range [][]string{w.Conditions, w.Exceptions} {
		for _, c := range list {
			if strings.TrimSpace(c) == "" {
				return "một điều kiện hoặc ngoại lệ để rỗng"
			}
		}
	}
	return ""
}

// verbose names the first clause the model wrote at more length than it needs,
// or reports that none of them is long.
//
// This is separate from check because it is a different kind of complaint and
// deserves a different answer. A count that does not match the statement makes
// the form wrong, and a form that is wrong must not be written. A condition
// stated in more words than another drafter would use makes the form worse: the
// slug is then unique to this norm, containment does not hold against the other
// side, and Circumstances reports unknown, which is the weaker claim and is
// counted separately everywhere. So the model is asked to compress and, if it
// will not, its answer is taken.
//
// Refusing it instead is what the earlier version of this pass did, and the
// cost of that was five documents thrown away over the wording of one clause in
// each, with the calls for every statement before it already paid for.
func verbose(w wireForm) string {
	for _, list := range [][]string{w.Conditions, w.Exceptions} {
		for _, c := range list {
			if n := len(strings.Fields(c)); n > maxClauseWords {
				return fmt.Sprintf("cụm %q dài %d từ, phải viết ngắn lại", c, n)
			}
		}
	}
	return ""
}

// apply writes the answer onto the draft.
//
// The comparison keys are slugs and the wording is kept beside them. A slug
// folds case, diacritics and punctuation, so "Người sử dụng lao động" and
// "người sử dụng lao động," are one key rather than two, and that is the whole
// of the folding it does: it is deliberately not a similarity measure, because
// a comparison that fires on partly similar acts is a comparison that reports
// pairs nobody can defend.
func apply(f *Form, w wireForm) {
	f.Canon = Canon{
		Party:  strings.TrimSpace(w.Party),
		Act:    strings.TrimSpace(w.Act),
		Object: strings.TrimSpace(w.Object),
		Toward: strings.TrimSpace(w.Toward),
	}
	if f.Party == "" {
		f.Party = law.Slug(w.Party)
	}
	f.Act = law.Slug(w.Act)
	f.Object = law.Slug(w.Object)
	f.Toward = law.Slug(w.Toward)
	f.Scope.Conditions = slugs(w.Conditions)
	f.Scope.Exceptions = slugs(w.Exceptions)
}

func slugs(in []string) []string {
	var out []string
	for _, s := range in {
		if slug := law.Slug(s); slug != "" {
			out = append(out, slug)
		}
	}
	return out
}

func correction(problem string) string {
	return "\nLần trả lời trước bị từ chối: " + problem +
		"\nTrả lời lại, chỉ một đối tượng JSON, mỗi trường vài từ.\n"
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
