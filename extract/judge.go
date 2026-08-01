package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
)

// Judge runs pass 4. It is candidate-blind: a judge call sees the source
// text, the proposed statement, and the ontology definitions, and never the
// extractor's reasoning. The falsification judge is a second, adversarial
// reading whose named targets are negation, exception-versus-rule confusion,
// and scope, because a plausible extraction is easier to produce than a false
// one is to reject.
type Judge struct {
	Completer      api.Completer
	Model          string
	Registry       *ontology.Registry
	MaxCorrections int
}

const entailInstructions = `Bạn là giám khảo kiểm chứng. Bạn nhận nội dung một điều khoản luật Việt Nam và một phát biểu quy phạm được đề xuất.
Nhiệm vụ duy nhất: xét xem điều khoản có thực sự khẳng định phát biểu đó hay không.
Trả về đúng một đối tượng JSON theo dạng:
{"verdict":"entailed|contradicted|partially_supported|not_enough_information","rationale":"..."}
Chỉ trả entailed khi điều khoản khẳng định đầy đủ phát biểu, kể cả chủ thể, hành vi và phạm vi.`

const falsifyInstructions = `Bạn là giám khảo phản biện. Bạn nhận nội dung một điều khoản luật Việt Nam và một phát biểu quy phạm đã qua một vòng kiểm chứng.
Nhiệm vụ duy nhất: cố gắng bác bỏ phát biểu. Chú ý ba lỗi thường gặp:
- phủ định bị đọc ngược, "không được" thành "được";
- ngoại lệ bị đọc thành quy tắc chung;
- phạm vi bị mở rộng, phát biểu nói nhiều hơn điều khoản.
Trả về đúng một đối tượng JSON theo dạng:
{"verdict":"entailed|contradicted|partially_supported|not_enough_information","rationale":"..."}
Chỉ trả entailed khi bạn đã cố bác bỏ mà không tìm được lỗi nào.`

// Entail asks the entailment judge for a verdict on one statement.
func (j *Judge) Entail(ctx context.Context, w *Window, s *norm.Statement) (*norm.Judgment, api.Usage, error) {
	return j.call(ctx, entailInstructions, w, s)
}

// Falsify asks the falsification judge for its independent verdict.
func (j *Judge) Falsify(ctx context.Context, w *Window, s *norm.Statement) (*norm.Judgment, api.Usage, error) {
	return j.call(ctx, falsifyInstructions, w, s)
}

func (j *Judge) call(ctx context.Context, instructions string, w *Window, s *norm.Statement) (*norm.Judgment, api.Usage, error) {
	input, err := judgeInput(j.Registry, w, s)
	if err != nil {
		return nil, api.Usage{}, err
	}
	var usage api.Usage
	lastErr := "no attempt made"
	for attempt := 0; attempt <= max(0, j.MaxCorrections); attempt++ {
		resp, err := j.Completer.Complete(ctx, api.Request{
			Model:        j.Model,
			Instructions: instructions,
			Input:        input,
		})
		if err != nil {
			return nil, usage, err
		}
		usage = addUsage(usage, resp.Usage)
		var verdict norm.Judgment
		perr := json.Unmarshal([]byte(StripFences(resp.Text)), &verdict)
		if perr == nil && slices.Contains(norm.Verdicts, verdict.Verdict) {
			return &verdict, usage, nil
		}
		if perr != nil {
			lastErr = "not a single JSON object: " + perr.Error()
		} else {
			lastErr = fmt.Sprintf("verdict %q is not in the enum", verdict.Verdict)
		}
		input, _ = judgeInput(j.Registry, w, s)
		input += "\nLần trả lời trước bị lỗi: " + lastErr + "\nTrả lời lại, chỉ một đối tượng JSON.\n"
	}
	return nil, usage, fmt.Errorf("judge %s: no valid verdict: %s", w.ProvisionID, lastErr)
}

// judgeInput renders what the judge is allowed to see and nothing else.
func judgeInput(reg *ontology.Registry, w *Window, s *norm.Statement) (string, error) {
	stmt, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Nội dung điều khoản %s:\n%s\n\n", w.ProvisionID, w.Text)
	b.WriteString("Phát biểu được đề xuất:\n")
	b.Write(stmt)
	b.WriteString("\n")
	classes := map[string]bool{}
	for _, ref := range []*norm.Ref{s.Subject, &s.Action, s.Object} {
		if ref == nil || ref.ClassID == "" || classes[ref.ClassID] {
			continue
		}
		classes[ref.ClassID] = true
		if c := reg.Class(ref.ClassID); c != nil {
			fmt.Fprintf(&b, "\nĐịnh nghĩa lớp %s: %s", c.ID, c.LabelVI)
			if c.DefinitionVI != "" {
				fmt.Fprintf(&b, ", %s", c.DefinitionVI)
			}
		}
	}
	b.WriteString("\n")
	return b.String(), nil
}
