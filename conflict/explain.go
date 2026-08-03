package conflict

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tamnd/luatdo/api"
)

// Explainer writes the sentence a person reads under a finding.
//
// It runs downstream of detection and it cannot change the outcome. The
// detector has already decided that the pair fails a rule and which slots are
// responsible, and this pass is handed that decision and asked to put it into
// Vietnamese. If the model comes back with nothing usable the finding still
// stands, with an empty explanation, because an explanation is a courtesy and
// the finding is the result.
//
// The order matters more than it looks. A model asked to explain a conflict it
// was also asked to find will explain the one it found, and there is no way from
// the outside to tell that from a real one. Asked to explain a clash somebody
// else determined, it is doing the task it is good at.
type Explainer struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
}

const explainInstructions = `Bạn được cho hai quy định của pháp luật Việt Nam mà một bộ kiểm tra đã xác định là không thể cùng tuân thủ, kèm theo các thành phần trùng nhau và thành phần khác nhau giữa chúng.
Nhiệm vụ duy nhất: viết một đoạn ngắn bằng tiếng Việt nói rõ tình huống mà một người rơi vào thế không thể làm đúng cả hai.

Bắt buộc:
- Viết cho người đọc là cán bộ pháp chế, không dùng thuật ngữ kỹ thuật của máy.
- Nêu cụ thể chủ thể nào, hành vi nào, và trong hoàn cảnh nào thì hai quy định va nhau.
- Không kết luận quy định nào được ưu tiên áp dụng, không khuyên nên làm theo bên nào. Việc đó thuộc về người đọc.
- Không thêm dữ kiện ngoài hai quy định được cho.
- Tối đa 60 từ.

Trả về đúng một đối tượng JSON, không giải thích thêm:
{"explanation":"..."}`

// Instructions is the system prompt this pass sends.
func (e *Explainer) Instructions() string { return explainInstructions }

// maxExplanationWords bounds the answer. The limit is in the prompt and checked
// here, because a prompt that says sixty words and a checker that accepts four
// hundred is a prompt that says nothing.
const maxExplanationWords = 90

// ExplainPrompt renders one finding for the explainer.
func ExplainPrompt(f *Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Quy định thứ nhất (%s), loại %s:\n%s\n\n", f.A.ProvisionID, f.A.Operator, trim(f.A.Source.Quote))
	fmt.Fprintf(&b, "Quy định thứ hai (%s), loại %s:\n%s\n\n", f.B.ProvisionID, f.B.Operator, trim(f.B.Source.Quote))
	b.WriteString("Các thành phần trùng nhau:\n")
	for _, s := range f.Matched {
		fmt.Fprintf(&b, "  %s: %s\n", s.Name, s.A)
	}
	b.WriteString("Thành phần khác nhau:\n")
	for _, s := range f.Clashing {
		fmt.Fprintf(&b, "  %s: %s so với %s\n", s.Name, s.A, s.B)
	}
	if len(f.A.Scope.Conditions) > 0 || len(f.B.Scope.Conditions) > 0 {
		fmt.Fprintf(&b, "Điều kiện của quy định thứ nhất: %s\n", list(f.A.Scope.Conditions))
		fmt.Fprintf(&b, "Điều kiện của quy định thứ hai: %s\n", list(f.B.Scope.Conditions))
	}
	return b.String()
}

func list(in []string) string {
	if len(in) == 0 {
		return "không có"
	}
	return strings.Join(in, ", ")
}

type wireExplanation struct {
	Explanation string `json:"explanation"`
}

// Explain fills one finding's explanation and returns what the call cost.
func (e *Explainer) Explain(ctx context.Context, f *Finding) (string, api.Usage, error) {
	var usage api.Usage
	input := ExplainPrompt(f)
	problem := ""
	for attempt := 0; attempt <= maxCorrections(e.MaxCorrections); attempt++ {
		resp, err := e.Completer.Complete(ctx, api.Request{
			Model: e.Model, Instructions: e.Instructions(), Input: input,
		})
		if err != nil {
			return "", usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var wire wireExplanation
		if jerr := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); jerr != nil {
			problem = "câu trả lời không phải một đối tượng JSON hợp lệ: " + jerr.Error()
		} else if strings.TrimSpace(wire.Explanation) == "" {
			problem = "explanation rỗng"
		} else if n := len(strings.Fields(wire.Explanation)); n > maxExplanationWords {
			problem = fmt.Sprintf("đoạn giải thích dài %d từ, phải ngắn hơn", n)
		} else {
			return strings.TrimSpace(wire.Explanation), usage, nil
		}
		input = ExplainPrompt(f) + correction(problem)
	}
	return "", usage, fmt.Errorf("%s: no usable explanation after %d corrections: %s",
		f.ID(), maxCorrections(e.MaxCorrections), problem)
}
