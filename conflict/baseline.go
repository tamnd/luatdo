package conflict

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tamnd/luatdo/api"
)

// Baseline is the thing this package argues against: ask the model whether two
// provisions conflict, and believe the answer.
//
// It is here because the argument is worth nothing without it. Claiming that a
// parser plus a checker beats direct prompting is a claim about numbers, and the
// only way to have those numbers is to run the other approach over the same
// pairs and print both. If the baseline wins, that is the result and it goes in
// the report.
//
// It is given the two norms as sentences and nothing else. No hints about
// slots, no mention of a rule, no scope arithmetic done for it. A baseline
// handed the detector's own analysis is not a baseline.
//
// The sentences come from Restate rather than from the corpus, for the reason
// set out there: on a generated pair the second norm has no sentence in the
// corpus and several mutations change the first one, so the two quotes are not
// the two norms being compared.
type Baseline struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
}

const baselineInstructions = `Bạn được cho hai quy định của pháp luật Việt Nam.
Nhiệm vụ duy nhất: xét xem một chủ thể có thể đồng thời tuân thủ cả hai quy định hay không.
Nếu có tình huống mà làm đúng quy định này thì vi phạm quy định kia, đó là xung đột.
Khác nhau về nội dung nhưng vẫn tuân thủ được cả hai thì không phải xung đột.
Một số cụm trong câu được lưu không dấu. Đó là cách lưu trữ, không phải lỗi chính tả.

Trả về đúng một đối tượng JSON, không giải thích thêm:
{"conflict":true,"rationale":"..."}`

// Instructions is the system prompt this baseline sends.
func (b *Baseline) Instructions() string { return baselineInstructions }

// BaselinePrompt renders one pair the way somebody would who had no pipeline.
func BaselinePrompt(a, b *Form) string {
	var s strings.Builder
	fmt.Fprintf(&s, "Quy định thứ nhất:\n%s\n\n", trim(Restate(a)))
	fmt.Fprintf(&s, "Quy định thứ hai:\n%s\n", trim(Restate(b)))
	return s.String()
}

type wireBaseline struct {
	Conflict  bool   `json:"conflict"`
	Rationale string `json:"rationale"`
}

// Ask puts one pair to the model.
func (b *Baseline) Ask(ctx context.Context, x, y *Form) (bool, string, api.Usage, error) {
	var usage api.Usage
	input := BaselinePrompt(x, y)
	problem := ""
	for attempt := 0; attempt <= maxCorrections(b.MaxCorrections); attempt++ {
		resp, err := b.Completer.Complete(ctx, api.Request{
			Model: b.Model, Instructions: b.Instructions(), Input: input,
		})
		if err != nil {
			return false, "", usage, err
		}
		usage = addUsage(usage, resp.Usage)
		var wire wireBaseline
		if jerr := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); jerr != nil {
			problem = "câu trả lời không phải một đối tượng JSON hợp lệ: " + jerr.Error()
			input = BaselinePrompt(x, y) + correction(problem)
			continue
		}
		return wire.Conflict, strings.TrimSpace(wire.Rationale), usage, nil
	}
	return false, "", usage, fmt.Errorf("baseline gave no usable answer after %d corrections: %s",
		maxCorrections(b.MaxCorrections), problem)
}

// Asker is what GradeBaseline needs, so a test can grade a fixed answerer and
// the command can grade a real one.
type Asker interface {
	Ask(ctx context.Context, a, b *Form) (bool, string, api.Usage, error)
}

// BaselineGrade is the baseline's score over the same cases the checker was
// graded on, plus what it cost. Cost is part of the comparison: the checker
// makes one call per statement once, and the baseline makes one call per pair
// every time anybody asks, which on a corpus is a different order of spending.
type BaselineGrade struct {
	Cases     int `json:"cases"`
	Conflicts int `json:"conflicts"`
	Negatives int `json:"negatives"`

	TruePositives  int `json:"true_positives"`
	FalsePositives int `json:"false_positives"`
	TrueNegatives  int `json:"true_negatives"`
	FalseNegatives int `json:"false_negatives"`

	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`

	Calls  int       `json:"calls"`
	Usage  api.Usage `json:"usage"`
	Errors int       `json:"errors"`

	ByMutation map[string]*KindScore `json:"by_mutation"`
}

// GradeBaseline runs the direct question over every case.
//
// A pair the model refuses to answer for counts as an error and as no conflict.
// Silence is not a verdict, and scoring it as one either way would flatter or
// punish the baseline for something it did not say.
func GradeBaseline(ctx context.Context, cases []Case, a Asker) (*BaselineGrade, error) {
	g := &BaselineGrade{ByMutation: map[string]*KindScore{}}
	for i := range cases {
		c := cases[i]
		g.Cases++
		k := g.ByMutation[c.Mutation]
		if k == nil {
			k = &KindScore{Conflict: c.Conflict}
			g.ByMutation[c.Mutation] = k
		}
		k.Cases++

		said, _, usage, err := a.Ask(ctx, c.A, c.B)
		g.Calls++
		g.Usage = addUsage(g.Usage, usage)
		if err != nil {
			if ctx.Err() != nil {
				return g, err
			}
			g.Errors++
			said = false
		}
		if said {
			k.Fired++
		}
		switch {
		case c.Conflict && said:
			g.Conflicts++
			g.TruePositives++
			k.Correct++
		case c.Conflict && !said:
			g.Conflicts++
			g.FalseNegatives++
		case !c.Conflict && said:
			g.Negatives++
			g.FalsePositives++
		default:
			g.Negatives++
			g.TrueNegatives++
			k.Correct++
		}
	}
	g.Precision = ratio(g.TruePositives, g.TruePositives+g.FalsePositives)
	g.Recall = ratio(g.TruePositives, g.TruePositives+g.FalseNegatives)
	if g.Precision+g.Recall > 0 {
		g.F1 = 2 * g.Precision * g.Recall / (g.Precision + g.Recall)
	}
	return g, nil
}

func (g *BaselineGrade) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "asked the model directly about %d pairs, %d calls\n", g.Cases, g.Calls)
	fmt.Fprintf(&b, "precision %.2f, recall %.2f, f1 %.2f\n", g.Precision, g.Recall, g.F1)
	if g.Errors > 0 {
		fmt.Fprintf(&b, "%d pairs the model gave no usable answer for, counted as no conflict\n", g.Errors)
	}
	for _, mut := range Mutations {
		k := g.ByMutation[mut]
		if k == nil {
			continue
		}
		want := "must not say conflict"
		if k.Conflict {
			want = "must say conflict"
		}
		fmt.Fprintf(&b, "  %-26s %3d cases, %s, said conflict %3d, correct %3d\n", mut, k.Cases, want, k.Fired, k.Correct)
	}
	return b.String()
}
