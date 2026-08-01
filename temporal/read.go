package temporal

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/law"
)

// The temptation is to treat this layer as bookkeeping. It is not, and the
// reason is that a Vietnamese amending instruction is a sentence rather than a
// data structure.
//
//	1. Sửa đổi, bổ sung khoản 2 Điều 15 như sau: "2. Người sử dụng lao động ..."
//	2. Bổ sung điểm d vào sau điểm c khoản 1 Điều 20 như sau: ...
//	3. Bãi bỏ khoản 3 Điều 22.
//	4. Thay thế cụm từ "cơ quan quản lý nhà nước" bằng cụm từ "cơ quan nhà nước
//	   có thẩm quyền" tại các điều 5, 7 và 12.
//
// A citation parser gets the target out of the first three and nothing useful
// out of the fourth, which is a phrase level edit scattered across articles.
// Worse, it cannot tell "sửa đổi, bổ sung khoản 2" replacing a whole clause
// from "bổ sung điểm d vào sau điểm c" inserting one point beside its siblings,
// and those two produce different version graphs. Getting it wrong makes the
// point in time text wrong, which makes every norm read out of that text wrong,
// and it fails silently because the result is still well formed Vietnamese law.
//
// So the instruction is read by a model and everything around the reading stays
// deterministic.

// Reader is the amending instruction pass.
type Reader struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
}

type wireOperation struct {
	Operation string `json:"operation"`
	TargetDoc string `json:"target_doc"`
	TargetRef string `json:"target_component"`
	Scope     string `json:"scope"`
	Anchor    *struct {
		Position string `json:"position"`
		Sibling  string `json:"sibling"`
	} `json:"anchor"`
	NewText string `json:"new_text"`
	Phrase  *struct {
		Find    string   `json:"find"`
		Replace string   `json:"replace"`
		Targets []string `json:"targets"`
	} `json:"phrase_edit"`
	EffectiveFrom string  `json:"effective_from"`
	Instruction   string  `json:"instruction_quote"`
	CharStart     int     `json:"char_start"`
	CharEnd       int     `json:"char_end"`
	Confidence    float64 `json:"confidence"`
}

type wireReading struct {
	Operations []wireOperation `json:"operations"`
}

// Instructions is the system prompt for the amending instruction pass.
func (r *Reader) Instructions() string {
	var b strings.Builder
	b.WriteString("Bạn được cho một điều khoản của một văn bản sửa đổi văn bản khác.\n")
	b.WriteString("Nhiệm vụ: đọc từng chỉ dẫn sửa đổi trong điều khoản và mô tả nó bằng cấu trúc.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. operation phải là một trong: " + strings.Join(Kinds, ", ") + ".\n")
	b.WriteString("2. instruction_quote phải sao chép nguyên văn từ nội dung điều khoản, và char_start, char_end là vị trí byte thật của đoạn trích đó.\n")
	b.WriteString("3. target_component ghi đúng như văn bản viết, ví dụ \"khoản 2 Điều 15\" hoặc \"điểm d khoản 1 Điều 20\". Không tự suy ra mã.\n")
	b.WriteString("3b. Mỗi thành phần bị sửa là một phần tử riêng trong operations. Một chỉ dẫn nêu nhiều thành phần, ví dụ \"sửa đổi, bổ sung điểm a và điểm b khoản 1 Điều 5\", phải tách thành hai phần tử: một cho \"điểm a khoản 1 Điều 5\" với new_text là đoạn của điểm a, một cho \"điểm b khoản 1 Điều 5\" với new_text là đoạn của điểm b. instruction_quote của cả hai là cùng một câu chỉ dẫn.\n")
	b.WriteString("4. target_doc ghi số hiệu văn bản bị sửa nếu điều khoản có nêu, ví dụ \"45/2019/QH14\". Nếu không nêu thì để trống.\n")
	b.WriteString("5. scope phải là một trong: " + strings.Join([]string{ScopeDocument, ScopeArticle, ScopeClause, ScopePoint, ScopePhrase}, ", ") + ".\n")
	b.WriteString("6. Phân biệt rõ: \"sửa đổi, bổ sung khoản 2\" là thay toàn bộ khoản 2, còn \"bổ sung điểm d vào sau điểm c\" là chèn thêm một điểm mới. Trường hợp chèn thêm phải ghi anchor với position là before hoặc after và sibling là thành phần đứng cạnh.\n")
	b.WriteString("7. Nếu chỉ dẫn là thay cụm từ, hãy dùng phrase_edit với find, replace và targets. targets phải liệt kê đầy đủ các thành phần bị ảnh hưởng như văn bản nêu. Danh sách rỗng là sai.\n")
	b.WriteString("8. new_text là nội dung mới nguyên văn nếu điều khoản có nêu, ngược lại để trống.\n")
	b.WriteString("9. effective_from chỉ ghi khi chính điều khoản nêu ngày có hiệu lực, theo dạng YYYY-MM-DD. Không được đoán.\n")
	b.WriteString("10. Nếu điều khoản không chứa chỉ dẫn sửa đổi nào, trả về danh sách rỗng. Danh sách rỗng là câu trả lời đúng.\n\n")
	b.WriteString("Trả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"operations":[{"operation":"supplement","target_doc":"45/2019/QH14","target_component":"điểm d khoản 1 Điều 20","scope":"point","anchor":{"position":"after","sibling":"điểm c"},"new_text":"d) ...","phrase_edit":null,"effective_from":"2025-01-01","instruction_quote":"...","char_start":0,"char_end":0,"confidence":0.9}]}`)
	b.WriteString("\n")
	return b.String()
}

// Prompt renders one provision of an amending instrument.
func Prompt(provisionID, text string) string {
	return fmt.Sprintf("Điều khoản: %s\n\nNội dung:\n%s\n", provisionID, text)
}

// Read reads one provision and returns the operations it states. Nothing is
// resolved here: the target reference comes back as the drafter wrote it and is
// resolved against the corpus by code afterwards.
func (r *Reader) Read(ctx context.Context, docID, provisionID, text, instrumentFrom string) ([]Operation, api.Usage, error) {
	var usage api.Usage
	input := Prompt(provisionID, text)
	last := ""
	for attempt := 0; attempt <= maxCorrections(r.MaxCorrections); attempt++ {
		resp, err := r.Completer.Complete(ctx, api.Request{
			Model: r.Model, Instructions: r.Instructions(), Input: input,
		})
		if err != nil {
			return nil, usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var wire wireReading
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			last = "câu trả lời không phải một đối tượng JSON hợp lệ: " + err.Error()
			input = Prompt(provisionID, text) + correction(last)
			continue
		}
		ops, problem := r.convert(wire, docID, provisionID, text, instrumentFrom)
		if problem != "" {
			last = problem
			input = Prompt(provisionID, text) + correction(problem)
			continue
		}
		return ops, usage, nil
	}
	// The last thing that was wrong is the whole diagnosis. Without it a failed
	// provision is a provision nobody can fix, and the failures are the queue of
	// work this pass leaves behind.
	return nil, usage, fmt.Errorf("%s: the model did not produce a usable answer after %d corrections, last problem: %s",
		provisionID, maxCorrections(r.MaxCorrections), last)
}

func (r *Reader) convert(wire wireReading, docID, provisionID, text, instrumentFrom string) ([]Operation, string) {
	var out []Operation
	for i, w := range wire.Operations {
		kind := strings.ToLower(strings.TrimSpace(w.Operation))
		if !KnownKind(kind) {
			return nil, fmt.Sprintf("operation %q không thuộc danh sách %s", w.Operation, strings.Join(Kinds, ", "))
		}
		start, end, err := locateQuote(text, w.Instruction, w.CharStart, w.CharEnd)
		if err != nil {
			return nil, fmt.Sprintf("instruction_quote %q: %v", w.Instruction, err)
		}
		scope := strings.ToLower(strings.TrimSpace(w.Scope))
		switch scope {
		case ScopeDocument, ScopeArticle, ScopeClause, ScopePoint, ScopePhrase:
		default:
			return nil, fmt.Sprintf("scope %q không hợp lệ", w.Scope)
		}
		if w.Confidence < 0 || w.Confidence > 1 {
			return nil, fmt.Sprintf("confidence %v nằm ngoài khoảng 0 đến 1", w.Confidence)
		}
		if w.EffectiveFrom != "" && !isDate(w.EffectiveFrom) {
			return nil, fmt.Sprintf("effective_from %q không theo dạng YYYY-MM-DD", w.EffectiveFrom)
		}

		op := Operation{
			Kind: kind, AmendingDoc: docID, CausedBy: provisionID,
			TargetRef: strings.TrimSpace(w.TargetRef), Scope: scope,
			NewText: w.NewText, EffectiveFrom: w.EffectiveFrom, InstrumentFrom: instrumentFrom,
			Instruction: w.Instruction, CharStart: start, CharEnd: end,
			Confidence: w.Confidence, Model: r.Model,
		}
		// The number is copied and the identifier is not. Turning "45/2019/QH14"
		// into a document identifier is exact, and anything exact is never asked
		// of a model.
		op.TargetNumber = strings.TrimSpace(w.TargetDoc)
		if w.Anchor != nil && strings.TrimSpace(w.Anchor.Sibling) != "" {
			position := strings.ToLower(strings.TrimSpace(w.Anchor.Position))
			if position != "after" && position != "before" {
				return nil, fmt.Sprintf("anchor.position %q phải là after hoặc before", w.Anchor.Position)
			}
			op.Anchor = &Anchor{Position: position, Sibling: strings.TrimSpace(w.Anchor.Sibling)}
		}
		if w.Phrase != nil && strings.TrimSpace(w.Phrase.Find) != "" {
			if len(w.Phrase.Targets) == 0 {
				// An empty target list is not a wildcard. A phrase edit scoped
				// to three articles and applied corpus wide corrupts everything
				// it touches and stays well formed Vietnamese while doing it.
				return nil, "phrase_edit không liệt kê targets, phải nêu rõ các thành phần bị ảnh hưởng"
			}
			op.Phrase = &PhraseEdit{
				Find: w.Phrase.Find, Replace: w.Phrase.Replace, Targets: w.Phrase.Targets,
			}
			op.Scope = ScopePhrase
		}
		if op.Scope == ScopePhrase && op.Phrase == nil {
			return nil, "scope là phrase nhưng không có phrase_edit"
		}
		op.ID = OperationID(docID, refKey(op), i)
		out = append(out, op)
	}
	return out, ""
}

// refKey is what an operation is identified by before its target resolves. The
// reference as written is stable across runs, which a sequence number alone is
// not once a document is re-read and one instruction is dropped.
func refKey(o Operation) string {
	ref := o.TargetRef
	if o.Phrase != nil && len(o.Phrase.Targets) > 0 {
		ref = strings.Join(o.Phrase.Targets, "+")
	}
	// The telex spelling runs first so that "điểm d" and "điểm đ" of the same
	// clause do not become one key, which would give two operations one
	// identifier and let the second overwrite the first.
	return law.Slug(law.NumberSegment(ref))
}

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func isDate(s string) bool { return datePattern.MatchString(s) }

// locateQuote checks that an instruction quote is really in the provision and
// returns the byte offsets it is really at.
//
// The quote is the claim and the offsets are the citation. Only the claim has
// to come from the model. Asking a model to count bytes in a script where a
// vowel costs three of them is asking for the one part of the answer it cannot
// check, and the first real instrument this pass read lost all three of its
// amendments to a leading newline: the quote was verbatim and the offsets were
// off by one.
//
// So a quote that appears exactly once has its offsets corrected here and the
// reading stands. A quote that appears twice keeps the model's offsets as the
// only thing saying which occurrence was meant, and wrong offsets on a repeated
// quote are still a rejection.
func locateQuote(text, quote string, start, end int) (int, int, error) {
	if quote == "" {
		return 0, 0, fmt.Errorf("rỗng")
	}
	if start >= 0 && end <= len(text) && start < end && text[start:end] == quote {
		return start, end, nil
	}
	at := strings.Index(text, quote)
	if at < 0 {
		return 0, 0, fmt.Errorf("không có trong nội dung điều khoản")
	}
	if strings.Contains(text[at+len(quote):], quote) {
		return 0, 0, fmt.Errorf("xuất hiện nhiều lần, char_start %d và char_end %d không chỉ đúng lần nào", start, end)
	}
	return at, at + len(quote), nil
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
