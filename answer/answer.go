// Package answer turns retrieved components into an answer a reader can check.
//
// The inversion this package rests on is the one the Brazilian and the IRAC
// style systems both arrive at from different directions: the model is not the
// thing that knows the law, it is the thing that writes Vietnamese. What may be
// asserted is decided by the graph before the model is called and checked again
// after it answers, and a sentence that cites a component nobody retrieved or
// quotes words that are not in that component is deleted rather than shown with
// a caveat.
//
// That makes refusal a first class answer instead of a failure. A graph that
// holds nothing about the question produces a stated nothing. Every generation
// system that cannot do this produces a fluent paragraph instead, and a fluent
// paragraph about a law that says nothing of the kind is worse than silence for
// exactly the readers who cannot check it.
package answer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/retrieve"
)

// Source is one component the answerer is allowed to use, with everything it
// may quote and everything it may assert.
type Source struct {
	ComponentID string              `json:"component_id"`
	DocID       string              `json:"doc_id"`
	Title       string              `json:"title,omitempty"`
	Heading     string              `json:"heading,omitempty"`
	Text        string              `json:"text"`
	Statements  []norm.Record       `json:"statements,omitempty"`
	Intervals   []retrieve.Interval `json:"intervals,omitempty"`
}

// Claim is one assertion with the component that licenses it.
type Claim struct {
	Text        string `json:"text"`
	ComponentID string `json:"component_id"`
	StatementID string `json:"statement_id,omitempty"`
	Quote       string `json:"quote"`
}

// Dropped is a claim the grounding check removed, kept so a run can report what
// the model tried to say rather than only what survived.
type Dropped struct {
	Claim  Claim  `json:"claim"`
	Reason string `json:"reason"`
}

// Reasons a claim is removed. They are separate values because they mean
// different things about the system: an unknown component is the model
// inventing a citation, a quote that is not in the component is the model
// paraphrasing evidence, and an unknown statement is the model asserting
// something the graph never verified.
const (
	DropUnknownComponent = "cited a component that was not in front of it"
	DropQuoteNotFound    = "quoted words that are not in that component"
	DropUnknownStatement = "cited a statement that component does not carry"
	DropNoStatement      = "asserted from a component that carries no trusted statement"
	DropEmpty            = "said nothing"
)

// Answer is what came back, including what was thrown away.
type Answer struct {
	Question string    `json:"question"`
	AsOf     string    `json:"as_of,omitempty"`
	Refused  bool      `json:"refused"`
	Reason   string    `json:"reason,omitempty"`
	Claims   []Claim   `json:"claims,omitempty"`
	Dropped  []Dropped `json:"dropped,omitempty"`
	Sources  []string  `json:"sources,omitempty"`
	Model    string    `json:"model,omitempty"`
	Calls    int       `json:"calls"`
	Usage    api.Usage `json:"usage"`
}

// Grounded is the share of the model's claims that survived the check. It is
// reported beside the answer rather than folded into it, because an answer with
// three good sentences out of three and one with three out of nine are not the
// same event even when the reader sees the same three sentences.
func (a Answer) Grounded() (kept, made int) {
	return len(a.Claims), len(a.Claims) + len(a.Dropped)
}

// Answerer writes the Vietnamese. It holds no legal knowledge of its own and
// the check after it assumes it holds none.
type Answerer struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
}

const instructions = `Bạn là người soạn câu trả lời từ cơ sở dữ liệu pháp luật Việt Nam. Bạn không được dùng kiến thức bên ngoài.
Bạn nhận một câu hỏi và một danh sách điều khoản. Mỗi điều khoản có mã component_id, nguyên văn, và các phát biểu quy phạm đã được kiểm chứng kèm mã statement_id.
Quy tắc bắt buộc:
- Mỗi câu khẳng định phải dựa vào đúng một phát biểu đã kiểm chứng, ghi kèm component_id và statement_id của phát biểu đó.
- Mỗi câu phải kèm một đoạn trích quote chép nguyên văn từ nguyên văn của chính điều khoản ấy, không sửa một chữ, không rút gọn bằng dấu ba chấm.
- Không suy diễn, không tổng hợp thêm, không nhắc tới điều khoản không có trong danh sách.
- Nếu danh sách không đủ để trả lời, trả về từ chối và nói rõ thiếu gì.
Trả về đúng một đối tượng JSON:
{"claims":[{"text":"...","component_id":"...","statement_id":"...","quote":"..."}],"refusal":""}
Khi từ chối thì claims để rỗng và refusal là một câu tiếng Việt nêu lý do.`

type wire struct {
	Claims  []Claim `json:"claims"`
	Refusal string  `json:"refusal"`
}

// Request is one question with the material the retriever chose for it.
type Request struct {
	Question string
	AsOf     string
	Sources  []Source
}

// Answer asks the model and then checks what it said.
//
// The model is called even when the source list is empty. That is how the no
// retrieval baseline is measured: the same answerer with nothing in front of it
// should refuse, and every sentence it produces instead is an invention that
// the check then catches. Short circuiting on an empty list would make the
// baseline look perfect while measuring nothing.
func (a *Answerer) Answer(ctx context.Context, req Request) (Answer, error) {
	out := Answer{Question: req.Question, AsOf: req.AsOf, Model: a.Model}
	for _, s := range req.Sources {
		out.Sources = append(out.Sources, s.ComponentID)
	}
	input, err := render(req)
	if err != nil {
		return out, err
	}
	lastErr := "no attempt made"
	for attempt := 0; attempt <= max(0, a.MaxCorrections); attempt++ {
		resp, cerr := a.Completer.Complete(ctx, api.Request{
			Model:        a.Model,
			Instructions: instructions,
			Input:        input,
		})
		out.Calls++
		if cerr != nil {
			return out, cerr
		}
		out.Usage = addUsage(out.Usage, resp.Usage)
		var w wire
		if perr := json.Unmarshal([]byte(stripFences(resp.Text)), &w); perr != nil {
			lastErr = "not a single JSON object: " + perr.Error()
			input = render2(input, lastErr)
			continue
		}
		a.settle(&out, req, w)
		return out, nil
	}
	return out, fmt.Errorf("answerer: no usable reply: %s", lastErr)
}

// settle applies the check. It is separate from the call so that a stored model
// reply can be re-checked later without spending anything.
func (a *Answerer) settle(out *Answer, req Request, w wire) {
	byID := map[string]*Source{}
	for i := range req.Sources {
		byID[req.Sources[i].ComponentID] = &req.Sources[i]
	}
	for _, c := range w.Claims {
		if strings.TrimSpace(c.Text) == "" {
			out.Dropped = append(out.Dropped, Dropped{Claim: c, Reason: DropEmpty})
			continue
		}
		src, ok := byID[c.ComponentID]
		if !ok {
			out.Dropped = append(out.Dropped, Dropped{Claim: c, Reason: DropUnknownComponent})
			continue
		}
		if len(src.Statements) == 0 {
			out.Dropped = append(out.Dropped, Dropped{Claim: c, Reason: DropNoStatement})
			continue
		}
		if !hasStatement(src, c.StatementID) {
			out.Dropped = append(out.Dropped, Dropped{Claim: c, Reason: DropUnknownStatement})
			continue
		}
		if !Quoted(src.Text, c.Quote) {
			out.Dropped = append(out.Dropped, Dropped{Claim: c, Reason: DropQuoteNotFound})
			continue
		}
		out.Claims = append(out.Claims, c)
	}
	if len(out.Claims) > 0 {
		return
	}
	out.Refused = true
	switch {
	case strings.TrimSpace(w.Refusal) != "":
		out.Reason = strings.TrimSpace(w.Refusal)
	case len(out.Dropped) > 0:
		out.Reason = "the answer was withdrawn because no sentence in it survived the grounding check"
	default:
		out.Reason = "the model returned nothing"
	}
}

func hasStatement(src *Source, id string) bool {
	for i := range src.Statements {
		if src.Statements[i].ID == id {
			return true
		}
	}
	return false
}

// Quoted reports whether a quote is really in the component's words.
//
// Whitespace is normalised on both sides and nothing else is. A quote that
// differs by a comma is not the provision's words, and accepting it would let
// the model edit the evidence it is being checked against, which is the one
// thing this check exists to prevent.
func Quoted(text, quote string) bool {
	q := strings.Join(strings.Fields(quote), " ")
	if q == "" {
		return false
	}
	return strings.Contains(strings.Join(strings.Fields(text), " "), q)
}

// render writes what the model is allowed to see.
func render(req Request) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Câu hỏi: %s\n", req.Question)
	if req.AsOf != "" {
		fmt.Fprintf(&b, "Thời điểm cần trả lời: %s. Nguyên văn dưới đây là bản có hiệu lực tại thời điểm đó.\n", req.AsOf)
	}
	if len(req.Sources) == 0 {
		b.WriteString("\nDanh sách điều khoản: rỗng.\n")
		return b.String(), nil
	}
	b.WriteString("\nDanh sách điều khoản:\n")
	for _, s := range req.Sources {
		fmt.Fprintf(&b, "\ncomponent_id: %s\n", s.ComponentID)
		if s.Title != "" {
			fmt.Fprintf(&b, "văn bản: %s\n", s.Title)
		}
		if s.Heading != "" {
			fmt.Fprintf(&b, "tiêu đề: %s\n", s.Heading)
		}
		fmt.Fprintf(&b, "nguyên văn: %s\n", s.Text)
		for i := range s.Statements {
			rec := &s.Statements[i]
			stmt, err := json.Marshal(rec.Statement)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "statement_id: %s, phát biểu: %s\n", rec.ID, stmt)
		}
	}
	return b.String(), nil
}

func render2(input, problem string) string {
	return input + "\nLần trả lời trước bị lỗi: " + problem + "\nTrả lời lại, chỉ một đối tượng JSON.\n"
}

// stripFences removes a markdown code fence the model wrapped its JSON in.
func stripFences(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return t
	}
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[i+1:]
	}
	if i := strings.LastIndex(t, "```"); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

func addUsage(a, b api.Usage) api.Usage {
	a.InputTokens += b.InputTokens
	a.OutputTokens += b.OutputTokens
	a.TotalTokens += b.TotalTokens
	return a
}
