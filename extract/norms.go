package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/entail"
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
	Verification    Verification  `json:"verification"`
	CompletedAt     time.Time     `json:"completed_at"`
}

// Verification is what the verification stages of this job cost and what the
// cheap gate did to that cost.
//
// Usage is the part of the job's total the judges spent, not a second total.
// The project could never say what verification cost before this field existed,
// because extraction and judging went into one number, and a milestone about
// removing judge calls has to be able to say what a judge call is worth.
type Verification struct {
	Usage     api.Usage `json:"usage"`
	Calls     int       `json:"calls"`     // judge calls actually made
	Accepted  int       `json:"accepted"`  // decided entailed by the gate, no judge call
	Rejected  int       `json:"rejected"`  // decided not entailed by the gate, no judge call
	Audited   int       `json:"audited"`   // decided by the gate and sent to the judge anyway
	Escalated int       `json:"escalated"` // the gate was not confident
}

// Add sums two verification accounts, which is how a campaign totals what its
// provisions did.
func (v Verification) Add(o Verification) Verification {
	v.Usage = addUsage(v.Usage, o.Usage)
	v.Calls += o.Calls
	v.Accepted += o.Accepted
	v.Rejected += o.Rejected
	v.Audited += o.Audited
	v.Escalated += o.Escalated
	return v
}

// Settled is the number of statements the gate decided on its own, which is the
// number of judge calls it removed. An audited statement is not settled: the
// gate decided it and the judge was called anyway, which is what makes the
// audit worth its cost.
func (v Verification) Settled() int { return v.Accepted + v.Rejected }

// Share is the fraction of statements that never reached a judge. It is the
// saving the gate produced, and it is zero when no gate ran, which reads
// correctly: no gate means no saving rather than no measurement.
func (v Verification) Share() float64 {
	total := v.Settled() + v.Audited + v.Escalated
	if total == 0 {
		return 0
	}
	return float64(v.Settled()) / float64(total)
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
	// Gate is stage 5, the cheap entailment gate, and it is optional. A runner
	// with no gate is the pass as it was: every valid statement costs a judge
	// call. A runner with one sends the judge only what the gate could not
	// decide, plus the audit sample.
	Gate *entail.Gate
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
	b.WriteString("Không có bên mang nghĩa vụ nào được nêu trong điều khoản thì để trống bearer, không suy ra từ điều khác. Dẫn nhập của điều là một phần của điều khoản, nên chủ thể nêu ở dẫn nhập vẫn được dùng làm bearer.\n")
	// The same sentence is in the judge's instructions. Two prompts that read the
	// same drafting form differently do not disagree about the law, they disagree
	// about a convention neither was told, and the gate then deletes the
	// extractor's correct output for not matching a rule nobody wrote down.
	b.WriteString("Các cách viết \"do X quy định\", \"do X ban hành\", \"do X thành lập\", \"X quyết định\", \"X có trách nhiệm\", \"Nhà nước có chính sách\" đều ghi là duty của X, kể cả khi điều khoản không có chữ \"phải\".\n\n")

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
//
// When a gate is set it runs before either judge and may settle a statement on
// its own. What it settles has a gate verdict and no entailment verdict, which
// is how a later reader tells a statement a strong model read from one a cheap
// model waved through.
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

		// Stage 5. The gate reads the pair and either decides it or hands it on.
		// Its reading is stored either way, including when the audit sample pulls
		// a decided statement back for the judge to check, because a gate that
		// records only the decisions it acted on cannot be caught being wrong.
		verified, decided := false, false
		if r.Gate != nil {
			v := r.Gate.Verdict(rec.ID, w.Text, &rec.Statement)
			rec.Gate = &v
			switch {
			case v.Decision == norm.GateJudge:
				job.Verification.Escalated++
			case v.Audited:
				job.Verification.Audited++
			default:
				verified, decided = v.Decision == norm.GateAccept, true
				if verified {
					job.Verification.Accepted++
				} else {
					job.Verification.Rejected++
				}
			}
		}

		// Stage 6. The strong judge, on what stage 5 left.
		if !decided {
			ent, usage, err := judge.Entail(ctx, w, &rec.Statement)
			job.Usage = addUsage(job.Usage, usage)
			job.Verification.Usage = addUsage(job.Verification.Usage, usage)
			job.Verification.Calls++
			if err != nil {
				return job, err
			}
			rec.Entailment = ent
			verified = ent.Verdict == norm.VerdictEntailed
		}
		if verified && job.Mode == "slow" {
			fal, usage, err := judge.Falsify(ctx, w, &rec.Statement)
			job.Usage = addUsage(job.Usage, usage)
			job.Verification.Usage = addUsage(job.Verification.Usage, usage)
			job.Verification.Calls++
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
