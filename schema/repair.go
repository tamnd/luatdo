package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/eval"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
)

// Repairer runs the structural check first and the model only on what fails.
//
// This is the cheap half of the loop doing the work it is good at. The
// invariants are exact, they cost nothing, and they say precisely which part of
// the statement is wrong. The model is asked one narrow question about one
// broken record with the provision text in front of it, and its answer is
// checked by the same invariants before it is kept.
type Repairer struct {
	Completer api.Completer
	Model     string
	MaxRounds int
}

// DefaultRounds is how many times a record goes back to the model.
//
// Bounded because a repair loop that runs until it succeeds will eventually
// succeed at producing something valid and wrong, and because the second round
// is where nearly all of the remaining fixes land: a record the model cannot
// fix twice with the provision in front of it is a record that needs a person.
const DefaultRounds = 2

func rounds(n int) int {
	if n <= 0 {
		return DefaultRounds
	}
	return n
}

const repairInstructions = `Bạn sửa một bản ghi trích xuất từ một điều khoản luật Việt Nam.
Bản ghi vi phạm một số ràng buộc bắt buộc. Danh sách vi phạm được liệt kê bên dưới.

Quy tắc bắt buộc:
1. Chỉ sửa những phần bị nêu trong danh sách vi phạm, giữ nguyên mọi phần khác.
2. Mọi trích dẫn phải là đoạn nguyên văn có thật trong điều khoản, sao chép đúng từng ký tự.
3. Nếu điều khoản không có thông tin để điền, hãy trả về {"repaired":null} kèm lý do. Bịa ra một giá trị là sai nặng hơn để trống.
4. Không thêm, không xoá các phần không liên quan đến vi phạm.

Trả về đúng một đối tượng JSON, không giải thích, theo dạng:
{"repaired":{...bản ghi đã sửa...},"reason":"..."}`

// RepairPrompt renders one repair question.
func RepairPrompt(it Item, vs []norm.Violation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Điều khoản %s:\n%s\n\n", it.ProvisionID, it.Text)
	raw, err := json.MarshalIndent(it.Statement, "", "  ")
	if err != nil {
		// A statement that will not marshal cannot be sent, and saying so in
		// the prompt is better than sending an empty object the model will
		// happily fill in.
		fmt.Fprintf(&b, "Bản ghi hiện tại không đọc được: %v\n", err)
	} else {
		fmt.Fprintf(&b, "Bản ghi hiện tại:\n%s\n\n", raw)
	}
	b.WriteString("Các vi phạm:\n")
	for _, v := range vs {
		fmt.Fprintf(&b, "  - [%s] %s\n", v.Code, v.Detail)
	}
	return b.String()
}

type wireRepair struct {
	Repaired *norm.Statement `json:"repaired"`
	Reason   string          `json:"reason"`
}

// Repair is one record taken through the loop.
//
// Before and After are both kept as code lists. A repair that turns three
// breaks into one is progress and a repair that turns one break into a
// different one is not, and only the two lists side by side tell them apart.
type Repair struct {
	RecordID    string          `json:"record_id"`
	ProvisionID string          `json:"provision_id"`
	Before      []string        `json:"before"`
	After       []string        `json:"after"`
	Introduced  []string        `json:"introduced,omitempty"`
	Rounds      int             `json:"rounds"`
	Valid       bool            `json:"valid"`
	Declined    bool            `json:"declined"`
	Reason      string          `json:"reason,omitempty"`
	Changed     []string        `json:"changed_fields,omitempty"`
	Statement   *norm.Statement `json:"statement,omitempty"`
	Usage       api.Usage       `json:"usage"`
	Calls       int             `json:"calls"`
}

// Fix takes one broken record through the bounded loop.
//
// A model that answers null is recorded as declining rather than as failing.
// The instruction says to decline when the provision does not carry the missing
// part, and a record that stays broken because the text really is silent is the
// loop working, not the loop losing.
func (r *Repairer) Fix(ctx context.Context, it Item, reg *ontology.Registry) (Repair, error) {
	out := Repair{RecordID: it.RecordID, ProvisionID: it.ProvisionID}
	current := it.Statement
	vs := norm.Violations(current, reg, it.Text)
	out.Before = codes(vs)
	if len(vs) == 0 {
		out.Valid = true
		out.After = nil
		return out, nil
	}
	before := map[string]bool{}
	for _, c := range out.Before {
		before[c] = true
	}
	for round := 0; round < rounds(r.MaxRounds); round++ {
		out.Rounds++
		resp, err := r.Completer.Complete(ctx, api.Request{
			Model: r.Model, Instructions: repairInstructions, Input: RepairPrompt(
				Item{RecordID: it.RecordID, ProvisionID: it.ProvisionID, DocID: it.DocID, Statement: current, Text: it.Text}, vs),
		})
		out.Calls++
		if err != nil {
			out.After = codes(vs)
			return out, err
		}
		out.Usage = addUsage(out.Usage, resp.Usage)

		var wire wireRepair
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			continue
		}
		out.Reason = strings.TrimSpace(wire.Reason)
		if wire.Repaired == nil {
			out.Declined = true
			break
		}
		next := wire.Repaired
		nextVs := norm.Violations(next, reg, it.Text)
		// The repaired record is kept whether or not it is clean, because a
		// record that went from three breaks to one is worth storing and the
		// counts below are what say so. What is not kept is a record that
		// broke something it was not asked about.
		out.Changed = changedFields(current, next)
		current = next
		vs = nextVs
		if len(vs) == 0 {
			break
		}
	}
	out.After = codes(vs)
	for _, c := range out.After {
		if !before[c] {
			out.Introduced = append(out.Introduced, c)
		}
	}
	out.Valid = len(vs) == 0
	out.Statement = current
	return out, nil
}

func codes(vs []norm.Violation) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vs {
		if seen[v.Code] {
			continue
		}
		seen[v.Code] = true
		out = append(out, v.Code)
	}
	sort.Strings(out)
	return out
}

// changedFields names the top level fields that differ.
//
// It is mechanical and free, and it answers the question a validity rate cannot:
// a repair asked to supply a bearer that also rewrote the action and the
// evidence has done something nobody asked for, and it will pass every
// structural check on the way through.
func changedFields(before, after *norm.Statement) []string {
	var out []string
	add := func(name string, a, b any) {
		x, errA := json.Marshal(a)
		y, errB := json.Marshal(b)
		if errA != nil || errB != nil || string(x) != string(y) {
			out = append(out, name)
		}
	}
	add("statement_type", before.Type, after.Type)
	add("bearer", before.Bearer, after.Bearer)
	add("counterparty", before.Counterparty, after.Counterparty)
	add("modality", before.Modality, after.Modality)
	add("action", before.Action, after.Action)
	add("object", before.Object, after.Object)
	add("conditions", before.Conditions, after.Conditions)
	add("exceptions", before.Exceptions, after.Exceptions)
	add("deadline", before.Deadline, after.Deadline)
	add("sanction", before.Sanction, after.Sanction)
	add("evidence", before.Evidence.Quote, after.Evidence.Quote)
	add("confidence", before.Confidence, after.Confidence)
	return out
}

// AskedFields names the fields the violations were about, so a repair can be checked
// against what it was sent to do.
func AskedFields(vs []string) []string {
	field := map[string]string{
		norm.ViolationType:           "statement_type",
		norm.ViolationEvidenceEmpty:  "evidence",
		norm.ViolationEvidenceQuote:  "evidence",
		norm.ViolationBearerMissing:  "bearer",
		norm.ViolationBearerNotActor: "bearer",
		norm.ViolationBearerClass:    "bearer",
		norm.ViolationClassUnknown:   "bearer",
		norm.ViolationConditionKind:  "conditions",
		norm.ViolationConditionQuote: "conditions",
		norm.ViolationExceptionKind:  "exceptions",
		norm.ViolationExceptionQuote: "exceptions",
		norm.ViolationSanctionEmpty:  "sanction",
		norm.ViolationSanctionText:   "sanction",
		norm.ViolationSanctionBasis:  "sanction",
		norm.ViolationSanctionQuote:  "sanction",
		norm.ViolationDeadlineEmpty:  "deadline",
		norm.ViolationConfidence:     "confidence",
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range vs {
		f := field[c]
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Drift is the fields a repair touched that no violation named.
//
// The class violation maps to the bearer here, which is a simplification: an
// unknown class can sit on any of the four references. It errs towards calling
// a change asked for, so drift is under reported rather than over reported, and
// an under reported drift figure is the safer way to be wrong about your own
// safety check.
func Drift(rep Repair) []string {
	asked := map[string]bool{}
	for _, f := range AskedFields(rep.Before) {
		asked[f] = true
	}
	var out []string
	for _, f := range rep.Changed {
		if !asked[f] {
			out = append(out, f)
		}
	}
	return out
}

// RepairScore is the loop measured over a set of broken records.
//
// Fixed and Grounded are separate rows for the reason the milestone exists: a
// repair loop scored only on validity is a loop scored on its ability to
// satisfy the checker, and satisfying the checker is exactly what inventing a
// bearer does.
type RepairScore struct {
	Broken     int           `json:"broken"`
	Fixed      eval.Accuracy `json:"fixed"`
	Improved   eval.Accuracy `json:"improved"`
	Declined   int           `json:"declined"`
	Drifted    int           `json:"drifted"`
	Introduced int           `json:"introduced"`
	Grounded   eval.Accuracy `json:"grounded"`
	ByCode     []CodeRepair  `json:"by_code"`
	Calls      int           `json:"calls"`
	Usage      api.Usage     `json:"usage"`
}

// CodeRepair is one invariant's repair rate. The distribution matters more than
// the total: a loop that fixes every out of range confidence and no missing
// bearer has a respectable headline and has fixed nothing that mattered.
type CodeRepair struct {
	Code      string        `json:"code"`
	Mandatory bool          `json:"mandatory"`
	Cleared   eval.Accuracy `json:"cleared"`
}

// ScoreRepairs folds repairs into rates. Grounding is passed in rather than
// computed, because it takes a judge and the caller decides whether to pay for
// one.
func ScoreRepairs(reps []Repair, grounded eval.Accuracy) RepairScore {
	s := RepairScore{Broken: len(reps), Grounded: grounded}
	byCode := map[string]*CodeRepair{}
	for _, r := range reps {
		s.Calls += r.Calls
		s.Usage = addUsage(s.Usage, r.Usage)
		s.Fixed.Observe(r.Valid)
		s.Improved.Observe(len(r.After) < len(r.Before))
		if r.Declined {
			s.Declined++
		}
		if len(Drift(r)) > 0 {
			s.Drifted++
		}
		if len(r.Introduced) > 0 {
			s.Introduced++
		}
		after := map[string]bool{}
		for _, c := range r.After {
			after[c] = true
		}
		for _, c := range r.Before {
			cr := byCode[c]
			if cr == nil {
				cr = &CodeRepair{Code: c, Mandatory: norm.Mandatory(c)}
				byCode[c] = cr
			}
			cr.Cleared.Observe(!after[c])
		}
	}
	for _, cr := range byCode {
		s.ByCode = append(s.ByCode, *cr)
	}
	sort.Slice(s.ByCode, func(i, j int) bool {
		if s.ByCode[i].Cleared.Of != s.ByCode[j].Cleared.Of {
			return s.ByCode[i].Cleared.Of > s.ByCode[j].Cleared.Of
		}
		return s.ByCode[i].Code < s.ByCode[j].Code
	})
	return s
}

func (s RepairScore) String() string {
	t := eval.NewTable("repair", fmt.Sprintf("%d broken records", s.Broken))
	t.Rate("records repaired to valid", s.Fixed)
	t.Rate("records with fewer breaks than before", s.Improved)
	if s.Grounded.Of > 0 {
		t.Rate("repairs a judge found grounded in the provision", s.Grounded)
	} else {
		t.Note("grounding was not judged on this run, so semantic correctness is not measured rather than perfect")
	}
	t.Note("%d records the model declined to repair, %d repairs touched a field no violation named, %d introduced a new break",
		s.Declined, s.Drifted, s.Introduced)
	for _, c := range s.ByCode {
		mark := " "
		if c.Mandatory {
			mark = "*"
		}
		t.Note("%s %-34s cleared %s", mark, c.Code, c.Cleared)
	}
	t.Note("%d calls, %d tokens", s.Calls, s.Usage.TotalTokens)
	return t.String()
}

const groundInstructions = `Bạn kiểm tra một bản ghi đã được sửa có đúng với điều khoản gốc hay không.

Quy tắc bắt buộc:
1. Chỉ dựa vào chữ trong điều khoản, không suy diễn từ kiến thức bên ngoài.
2. Trả về "no" nếu bản ghi thêm một chi tiết mà điều khoản không nói, kể cả khi chi tiết đó hợp lý.
3. Trả về "no" nếu phần được sửa làm sai nghĩa của điều khoản.

Trả về đúng một đối tượng JSON, không giải thích, theo dạng:
{"grounded":"yes","reason":"..."}`

type wireGrounded struct {
	Grounded string `json:"grounded"`
	Reason   string `json:"reason"`
}

// Judge asks whether a repair is supported by the provision.
//
// This is the semantic half and it is a separate call on purpose. Asking one
// model to repair and to certify its own repair in the same breath measures
// nothing, and the check being cheap enough to run on every repair is what
// makes the drift count above a check rather than a hope.
func (r *Repairer) Judge(ctx context.Context, it Item, rep Repair) (bool, string, api.Usage, error) {
	var usage api.Usage
	if rep.Statement == nil {
		return false, "nothing was repaired", usage, nil
	}
	raw, err := json.MarshalIndent(rep.Statement, "", "  ")
	if err != nil {
		return false, "the repaired record does not marshal", usage, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Điều khoản %s:\n%s\n\n", it.ProvisionID, it.Text)
	fmt.Fprintf(&b, "Bản ghi đã sửa:\n%s\n\n", raw)
	fmt.Fprintf(&b, "Các phần đã thay đổi: %s\n", strings.Join(rep.Changed, ", "))
	resp, err := r.Completer.Complete(ctx, api.Request{
		Model: r.Model, Instructions: groundInstructions, Input: b.String(),
	})
	if err != nil {
		return false, "", usage, err
	}
	usage = addUsage(usage, resp.Usage)
	var wire wireGrounded
	if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
		return false, "the judge did not answer with JSON", usage, nil
	}
	return strings.EqualFold(strings.TrimSpace(wire.Grounded), "yes"), strings.TrimSpace(wire.Reason), usage, nil
}
