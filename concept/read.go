package concept

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tamnd/luatdo/anchor"
	"github.com/tamnd/luatdo/api"
)

// Reader runs pass B: a model reads one definition clause that pass A anchored,
// and returns what the clause defines. The division of labour is the rule the
// whole project runs on. Pass A found the clause with a grammar, so the reader
// is never asked where the definition is, which clause number it carries or
// which instrument it is scoped to. It is asked what the clause means, which is
// the one thing a grammar cannot get.
//
// Nothing the model returns is trusted on its word. Every span it quotes is
// checked byte for byte against the clause before the reading is kept, and a
// reading that fails goes back with the failure named and a bounded number of
// chances to correct it.
type Reader struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
	// Scopes lets the reader confirm that a scope the model names is a real
	// instrument. It is optional: when it is empty the check is skipped rather
	// than failing everything, because a caller reading a single clause in
	// isolation has nothing to check against.
	Scopes map[string]bool
}

// Job is the stored artifact of one clause read. Failed attempts stay in it
// alongside the accepted reading, because a clause that took three tries is
// evidence about the prompt and throwing it away hides that.
type Job struct {
	UnitID    string    `json:"unit_id"`
	DocID     string    `json:"doc_id"`
	ScopeID   string    `json:"scope_id"`
	TextHash  string    `json:"text_hash"`
	Model     string    `json:"model,omitempty"`
	Attempts  []Attempt `json:"attempts,omitempty"`
	TermUses  []TermUse `json:"term_uses"`
	Rejected  []string  `json:"rejected,omitempty"`
	Usage     api.Usage `json:"usage"`
	ReadAt    time.Time `json:"read_at"`
	Err       string    `json:"error,omitempty"`
	DefinesNo bool      `json:"defines_nothing,omitempty"`
}

// Attempt is one raw response and why it was not accepted.
type Attempt struct {
	Raw   string `json:"raw"`
	Error string `json:"error,omitempty"`
}

// wireTerm is the shape the model answers in. It is separate from TermUse
// because the model supplies content and never identity: there is no id field
// here, and the scope it names is checked rather than taken.
type wireTerm struct {
	LabelVI      string        `json:"label_vi"`
	DefinitionVI string        `json:"definition_vi"`
	Genus        string        `json:"genus"`
	Differentiae []Differentia `json:"differentiae"`
	Kind         string        `json:"kind"`
	Aliases      []string      `json:"aliases"`
	IsRole       bool          `json:"is_role"`
	// DefinesByReference is the instrument named when the clause defines
	// nothing itself and points elsewhere.
	DefinesByReference string   `json:"defines_by_reference"`
	ReferenceQuote     string   `json:"reference_quote"`
	EnumeratedSubtypes []string `json:"enumerated_subtypes"`
	ReferencedTerms    []string `json:"referenced_terms"`
	Scope              string   `json:"scope"`
	Quote              string   `json:"quote"`
	CharStart          int      `json:"char_start"`
	CharEnd            int      `json:"char_end"`
	Confidence         float64  `json:"confidence"`
}

type wireReading struct {
	Terms []wireTerm `json:"terms"`
	// DefinesNothing is how the model says the clause defines no term. A
	// definitions article routinely carries a clause that only says the
	// remaining terms follow another law, and forcing a term out of it would
	// manufacture one.
	DefinesNothing bool `json:"defines_nothing"`
}

// Instructions is the pass B system prompt. It is Vietnamese because the text
// being read is Vietnamese and a prompt in another language makes the model
// translate before it reads.
func (r *Reader) Instructions() string {
	var b strings.Builder
	b.WriteString("Bạn đọc một khoản trong điều giải thích từ ngữ của một văn bản quy phạm pháp luật Việt Nam.\n")
	b.WriteString("Nhiệm vụ: nói rõ khoản này định nghĩa thuật ngữ nào và định nghĩa như thế nào.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. Mọi đoạn trích phải sao chép nguyên văn từng ký tự từ nội dung khoản. Không sửa chính tả, không rút gọn, không thêm dấu.\n")
	b.WriteString("2. char_start và char_end là vị trí byte của quote trong nội dung khoản, tính từ 0.\n")
	b.WriteString("3. genus là loại lớn hơn mà thuật ngữ thuộc vào, lấy nguyên văn trong khoản. Nếu khoản không nêu thì để trống.\n")
	b.WriteString("4. Mỗi differentia là một đặc điểm phân biệt, kèm đoạn trích nguyên văn chứa đặc điểm đó.\n")
	b.WriteString("5. Nếu khoản chỉ dẫn chiếu sang văn bản khác thì điền defines_by_reference bằng tên văn bản đó, để trống definition_vi, genus và differentiae. Tuyệt đối không viết lại định nghĩa của văn bản khác theo trí nhớ.\n")
	b.WriteString("6. is_role là true khi thuật ngữ chỉ một vai trò xác định theo từng văn bản, ví dụ cơ quan có thẩm quyền, người có thẩm quyền, cơ quan quản lý nhà nước, chứ không chỉ đích danh một cơ quan cụ thể.\n")
	b.WriteString("7. Nếu khoản định nghĩa bằng cách liệt kê thì ghi từng loại vào enumerated_subtypes, nguyên văn.\n")
	b.WriteString("8. Nếu khoản không định nghĩa thuật ngữ nào thì trả về {\"defines_nothing\":true,\"terms\":[]}. Đây là câu trả lời hợp lệ, không được nặn ra thuật ngữ.\n")
	b.WriteString("9. Không suy đoán ngoài nội dung khoản. Không dùng kiến thức về văn bản khác.\n\n")
	b.WriteString("kind phải là một trong các giá trị sau:\n")
	for _, k := range Kinds {
		fmt.Fprintf(&b, "- %s: %s\n", k, KindLabels[k])
	}
	b.WriteString("\nTrả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"defines_nothing":false,"terms":[{"label_vi":"...","definition_vi":"...","genus":"...","differentiae":[{"text":"...","quote":"..."}],"kind":"actor","aliases":[],"is_role":false,"defines_by_reference":"","reference_quote":"","enumerated_subtypes":[],"referenced_terms":[],"scope":"","quote":"...","char_start":0,"char_end":0,"confidence":0.9}]}`)
	b.WriteString("\n")
	return b.String()
}

// Prompt renders one unit as the model input. The clause identifier and the
// scope are shown but never asked for, so the model can see where it is
// without being invited to decide it.
func Prompt(u *anchor.Unit, scope *anchor.Scope) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Văn bản: %s\n", u.DocID)
	if scope != nil && scope.Kind == "annex" {
		fmt.Fprintf(&b, "Phạm vi: %s (%s)\n", scope.ID, scope.Instrument)
	}
	if scope != nil && scope.Formula != "" {
		fmt.Fprintf(&b, "Câu xác định phạm vi, chỉ để đọc hiểu, không được trích dẫn:\n%s\n", scope.Formula)
	}
	fmt.Fprintf(&b, "Mã khoản: %s\n", u.ID)
	if u.Number != "" {
		fmt.Fprintf(&b, "Số khoản: %s\n", u.Number)
	}
	fmt.Fprintf(&b, "\nNội dung khoản:\n%s\n", u.Text)
	return b.String()
}

// Read reads one definition unit. It returns a job whether or not the reading
// succeeded: an error from the model ends the job, but a reading the model
// could not get right within the correction budget is a recorded outcome and
// not a failure of the pipeline.
func (r *Reader) Read(ctx context.Context, u *anchor.Unit, scope *anchor.Scope) (*Job, error) {
	job := &Job{UnitID: u.ID, DocID: u.DocID, ScopeID: u.ScopeID, TextHash: u.TextHash, Model: r.Model}
	input := Prompt(u, scope)

	for attempt := 0; attempt <= maxCorrections(r.MaxCorrections); attempt++ {
		resp, err := r.Completer.Complete(ctx, api.Request{
			Model:        r.Model,
			Instructions: r.Instructions(),
			Input:        input,
		})
		if err != nil {
			job.Err = err.Error()
			job.ReadAt = time.Now().UTC()
			return job, err
		}
		job.Usage = addUsage(job.Usage, resp.Usage)

		terms, definesNothing, problem := r.parse(u, resp.Text)
		if problem == "" {
			job.Attempts = append(job.Attempts, Attempt{Raw: resp.Text})
			job.TermUses = terms
			job.DefinesNo = definesNothing
			job.ReadAt = time.Now().UTC()
			return job, nil
		}
		job.Attempts = append(job.Attempts, Attempt{Raw: resp.Text, Error: problem})
		input = Prompt(u, scope) + correction(problem)
	}

	job.Err = "no valid reading within the correction budget"
	job.Rejected = append(job.Rejected, job.Attempts[len(job.Attempts)-1].Error)
	job.ReadAt = time.Now().UTC()
	return job, nil
}

// parse turns one response into term uses, or names the first thing wrong with
// it. The problem string goes back to the model, so it says what to fix rather
// than that something is wrong.
func (r *Reader) parse(u *anchor.Unit, raw string) ([]TermUse, bool, string) {
	var reading wireReading
	if err := json.Unmarshal([]byte(stripFences(raw)), &reading); err != nil {
		return nil, false, "câu trả lời không phải một đối tượng JSON hợp lệ: " + err.Error()
	}
	if reading.DefinesNothing {
		if len(reading.Terms) > 0 {
			return nil, false, "đã báo defines_nothing nhưng vẫn trả về thuật ngữ"
		}
		return nil, true, ""
	}
	if len(reading.Terms) == 0 {
		// Silence is not the same as a clause that defines nothing, and the
		// difference matters for recall measurement later, so the model has to
		// say which one it means.
		return nil, false, "không có thuật ngữ nào và cũng không đặt defines_nothing, hãy nói rõ khoản có định nghĩa hay không"
	}

	out := make([]TermUse, 0, len(reading.Terms))
	seen := map[string]bool{}
	for _, w := range reading.Terms {
		t, problem := r.build(u, &w)
		if problem != "" {
			return nil, false, problem
		}
		if seen[t.ID] {
			return nil, false, fmt.Sprintf("thuật ngữ %q xuất hiện hai lần trong một khoản", t.LabelVI)
		}
		seen[t.ID] = true
		out = append(out, t)
	}
	return out, false, ""
}

// build assembles one term use and checks it. The identifier is minted here
// from the scope pass A found and the label the model read, never taken from
// the response.
func (r *Reader) build(u *anchor.Unit, w *wireTerm) (TermUse, string) {
	scopeID := u.ScopeID
	if w.Scope != "" && w.Scope != scopeID {
		if len(r.Scopes) > 0 && !r.Scopes[w.Scope] {
			return TermUse{}, fmt.Sprintf("scope %q không phải một văn bản có thật", w.Scope)
		}
		if len(r.Scopes) > 0 {
			scopeID = w.Scope
		}
	}

	t := TermUse{
		ID:                 TermUseID(scopeID, w.LabelVI),
		LabelVI:            strings.TrimSpace(w.LabelVI),
		ScopeID:            scopeID,
		DocID:              u.DocID,
		DefinitionVI:       strings.TrimSpace(w.DefinitionVI),
		Genus:              strings.TrimSpace(w.Genus),
		Differentiae:       w.Differentiae,
		Kind:               w.Kind,
		Aliases:            w.Aliases,
		IsRole:             w.IsRole,
		EnumeratedSubtypes: w.EnumeratedSubtypes,
		ReferencedTerms:    w.ReferencedTerms,
		Origin:             OriginDefined,
		DefinedBy:          u.ID,
		Quote:              w.Quote,
		CharStart:          w.CharStart,
		CharEnd:            w.CharEnd,
		Confidence:         w.Confidence,
		Model:              r.Model,
	}
	if w.DefinesByReference != "" {
		t.DefinesByReference = &Reference{Instrument: w.DefinesByReference, Quote: w.ReferenceQuote}
	}
	t.ID = TermUseID(t.ScopeID, t.LabelVI)
	if err := t.Validate(u.Text); err != nil {
		return TermUse{}, err.Error()
	}
	return t, ""
}

// correction is the follow up sent after a rejected reading. It names the
// failure and repeats the one rule that failure breaks, because a bare "try
// again" gets the same answer back.
func correction(problem string) string {
	return "\nLần trả lời trước bị từ chối: " + problem +
		"\nMọi đoạn trích phải sao chép nguyên văn từ nội dung khoản ở trên, và char_start, char_end phải là vị trí byte thật của đoạn trích đó." +
		"\nTrả lời lại, chỉ một đối tượng JSON.\n"
}

func maxCorrections(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// stripFences drops a markdown code fence around a JSON response.
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
