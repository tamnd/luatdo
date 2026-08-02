package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
)

// NormJob is the stored artifact of pass 3 over one provision: every raw
// attempt of every candidate, every record with its verdicts, and the
// accounting. A rejected statement stays in the job; the trusted store is
// assembled later by build, never here.
type NormJob struct {
	ProvisionID     string        `json:"provision_id"`
	DocID           string        `json:"doc_id"`
	Mode            string        `json:"mode"` // fast or slow
	OntologyVersion int           `json:"ontology_version"`
	Model           string        `json:"model,omitempty"`
	Candidates      []Candidate   `json:"candidates"`
	Records         []norm.Record `json:"records"`
	Usage           api.Usage     `json:"usage"`
	CompletedAt     time.Time     `json:"completed_at"`
}

// Candidate is one independent extraction run inside a job.
type Candidate struct {
	Attempts   []Attempt        `json:"attempts"`
	Statements []norm.Statement `json:"statements"`
	Err        string           `json:"error,omitempty"`
}

// NormRunner runs pass 3 and pass 4 for one provision.
type NormRunner struct {
	Completer      api.Completer
	Model          string
	Registry       *ontology.Registry
	MaxCorrections int
	Mode           string // fast or slow
	Population     int    // independent candidates in slow mode
}

type wireStatements struct {
	Statements []norm.Statement `json:"statements"`
}

// Instructions is the pass 3 system prompt. The statement types and the class
// list travel with every call, and the response must be one JSON object.
func (r *NormRunner) Instructions() string {
	var b strings.Builder
	b.WriteString("Bạn trích xuất các quy phạm pháp luật từ một điều khoản luật Việt Nam.\n")
	b.WriteString("Mỗi quy phạm là một phát biểu đầy đủ: bên mang nghĩa vụ, bên đối tác, hành vi, đối tượng, điều kiện, ngoại lệ, thời hạn, chế tài nếu có.\n")
	b.WriteString("Các loại phát biểu hợp lệ: " + strings.Join(norm.Types, ", ") + ".\n\n")

	b.WriteString("bearer là bên phải làm, được làm hoặc bị cấm. counterparty là bên còn lại của quan hệ, ví dụ bên được thông báo hoặc bên nhận tiền.\n")
	b.WriteString("Trong câu \"Bên A phải thông báo cho bên B\" thì bearer là bên A và counterparty là bên B.\n")
	b.WriteString("Đặt is_actor bằng true cho mọi tham chiếu chỉ một chủ thể pháp lý, kể cả khi chủ thể đó nằm ở vị trí đối tượng. Đặt false cho tài liệu, tiền, thời hạn và mọi thứ không phải chủ thể.\n")
	b.WriteString("Không có bên mang nghĩa vụ nào được nêu trong điều khoản thì để trống bearer, không suy ra từ điều khác.\n\n")

	// The được rule. This is one word and it is three different things, and
	// getting it wrong turns a worker's right into an employer's permission,
	// which is the single most consequential error this pass can make.
	b.WriteString("Chú ý chữ \"được\". Nó có ba nghĩa khác nhau:\n")
	b.WriteString("- \"được quyền\" và \"được phép\": đây là permission, bên đứng trước là bearer.\n")
	b.WriteString("- \"được\" đứng trước động từ trong câu bị động, ví dụ \"người lao động được trả lương đúng hạn\": đây là right của người lao động, không phải permission, và bên phải trả lương là bên mang nghĩa vụ tương ứng.\n")
	b.WriteString("- \"không được\": đây là prohibition, không phải right.\n\n")

	b.WriteString("conditions và exceptions là danh sách đối tượng, mỗi đối tượng có kind, text và quote nguyên văn.\n")
	b.WriteString("kind của condition thuộc: " + strings.Join(norm.ConditionKinds, ", ") + ".\n")
	b.WriteString("kind của exception thuộc: " + strings.Join(norm.ExceptionKinds, ", ") + ".\n")
	b.WriteString("deadline chỉ cần chép nguyên văn cụm từ chỉ thời hạn vào trường text, không tự tách số và đơn vị, không tự quy đổi ngày làm việc thành ngày.\n")
	b.WriteString("sanction phải có legal_basis, tức là điều khoản hoặc văn bản quy định chế tài đó, đúng như điều khoản viết. Không nêu được căn cứ thì không ghi sanction.\n")
	b.WriteString("procedure_id và step chỉ điền khi điều khoản là một bước của một thủ tục có tên, step là số thứ tự bước.\n\n")

	b.WriteString("Chỉ được dùng các mã lớp sau cho class_id, không được bịa mã mới, để trống nếu không chắc:\n")
	for _, c := range r.Registry.Classes {
		fmt.Fprintf(&b, "- %s: %s\n", c.ID, c.LabelVI)
	}
	b.WriteString("\nTrả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"statements":[{"statement_type":"duty",` +
		`"bearer":{"text":"...","class_id":"vn-legal:...","is_actor":true},` +
		`"counterparty":{"text":"...","class_id":"vn-legal:...","is_actor":true},` +
		`"modality":"obligation","action":{"text":"..."},"object":{"text":"...","is_actor":false},` +
		`"conditions":[{"kind":"precondition","text":"...","quote":"..."}],` +
		`"exceptions":[{"kind":"force","text":"...","quote":"..."}],` +
		`"deadline":{"text":"..."},` +
		`"sanction":{"text":"...","quote":"...","legal_basis":"..."},` +
		`"procedure_id":"","step":0,"evidence":{"quote":"..."},"confidence":0.9}]}` + "\n")
	b.WriteString("Mỗi quote phải là một đoạn nguyên văn, sao chép đúng từng ký tự từ nội dung điều khoản.\n")
	b.WriteString("Trường nào không có nội dung thì bỏ hẳn trường đó, không ghi đối tượng rỗng như {\"text\":\"\"}.\n")
	b.WriteString("Điều khoản không chứa quy phạm nào thì trả về danh sách rỗng, không suy diễn.\n")
	return b.String()
}

// Run extracts and verifies the norms of one provision. In fast mode one
// candidate is drawn and the entailment judge gates each statement. In slow
// mode Population independent candidates are drawn, the selector unions them
// by claim, and a statement must pass both the entailment judge and the
// falsification judge. Every statement keeps its verdicts either way.
func (r *NormRunner) Run(ctx context.Context, doc *law.Document, provisionID string) (*NormJob, error) {
	w, err := BuildWindow(doc, provisionID)
	if err != nil {
		return nil, err
	}
	job := &NormJob{
		ProvisionID:     provisionID,
		DocID:           doc.ID,
		Mode:            r.mode(),
		OntologyVersion: r.Registry.Version,
		Model:           r.Model,
	}
	for i := 0; i < r.population(); i++ {
		c := r.extractOnce(ctx, job, w, i)
		job.Candidates = append(job.Candidates, c)
	}
	statements := Union(job.Candidates)

	judge := &Judge{Completer: r.Completer, Model: r.Model, Registry: r.Registry, MaxCorrections: r.MaxCorrections}
	for i := range statements {
		s := &statements[i]
		rec := norm.Record{
			DocID:           doc.ID,
			ProvisionID:     provisionID,
			Statement:       *s,
			Model:           r.Model,
			OntologyVersion: r.Registry.Version,
		}
		norm.Normalize(&rec.Statement)
		if verr := norm.Validate(&rec.Statement, r.Registry, w.Text); verr != nil {
			rec.Status = "invalid"
			rec.Invalid = verr.Error()
			rec.ID = norm.ID(provisionID, &rec.Statement)
			job.Records = append(job.Records, rec)
			continue
		}
		rec.ID = norm.ID(provisionID, &rec.Statement)
		ent, usage, err := judge.Entail(ctx, w, &rec.Statement)
		job.Usage = addUsage(job.Usage, usage)
		if err != nil {
			return job, err
		}
		rec.Entailment = ent
		verified := ent.Verdict == norm.VerdictEntailed
		if verified && job.Mode == "slow" {
			fal, usage, err := judge.Falsify(ctx, w, &rec.Statement)
			job.Usage = addUsage(job.Usage, usage)
			if err != nil {
				return job, err
			}
			rec.Falsification = fal
			verified = fal.Verdict == norm.VerdictEntailed
		}
		if verified {
			rec.Status = "verified"
		} else {
			rec.Status = "rejected"
		}
		job.Records = append(job.Records, rec)
	}
	job.CompletedAt = time.Now().UTC()
	return job, nil
}

// extractOnce draws one candidate with the bounded correction loop. The
// candidate index is woven into the input so slow mode populations are not
// byte-identical prompts.
func (r *NormRunner) extractOnce(ctx context.Context, job *NormJob, w *Window, index int) Candidate {
	var c Candidate
	input := w.Prompt()
	if r.population() > 1 {
		input += fmt.Sprintf("\nLượt trích xuất độc lập số %d. Đọc kỹ lại điều khoản trước khi trả lời.\n", index+1)
	}
	for attempt := 0; attempt <= max(0, r.MaxCorrections); attempt++ {
		resp, err := r.Completer.Complete(ctx, api.Request{
			Model:        r.Model,
			Instructions: r.Instructions(),
			Input:        input,
		})
		if err != nil {
			c.Err = err.Error()
			return c
		}
		job.Usage = addUsage(job.Usage, resp.Usage)
		var parsed wireStatements
		perr := json.Unmarshal([]byte(StripFences(resp.Text)), &parsed)
		if perr == nil {
			c.Attempts = append(c.Attempts, Attempt{Raw: resp.Text})
			c.Statements = parsed.Statements
			return c
		}
		c.Attempts = append(c.Attempts, Attempt{Raw: resp.Text, Error: perr.Error()})
		input = w.Prompt() + "\nLần trả lời trước không phải một đối tượng JSON hợp lệ: " + perr.Error() + "\nTrả lời lại, chỉ một đối tượng JSON.\n"
	}
	c.Err = "no well formed response within the correction budget"
	return c
}

// Union is the slow mode selector: statements from all candidates are merged
// by claim key, and when two candidates extracted the same claim the one with
// higher confidence carries the details. The union exists because extraction
// misses are omissions; independent candidates recover each other's missed
// norms, and the judges, not the selector, decide what is true.
func Union(candidates []Candidate) []norm.Statement {
	var out []norm.Statement
	index := map[string]int{}
	for _, c := range candidates {
		for _, s := range c.Statements {
			key := norm.Key(&s)
			if i, seen := index[key]; seen {
				if s.Confidence > out[i].Confidence {
					out[i] = s
				}
				continue
			}
			index[key] = len(out)
			out = append(out, s)
		}
	}
	return out
}

func (r *NormRunner) mode() string {
	if r.Mode == "slow" {
		return "slow"
	}
	return "fast"
}

func (r *NormRunner) population() int {
	if r.mode() == "fast" {
		return 1
	}
	if r.Population < 1 {
		return 3
	}
	return min(r.Population, 5)
}
