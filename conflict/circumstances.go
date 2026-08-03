package conflict

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tamnd/luatdo/api"
)

// Judge settles whether two sets of circumstances can hold at once.
//
// It is an interface because the checker must not depend on a model being
// reachable. Check decides every conflict on its own and labels the pairs it
// could not place, and a Judge is asked afterwards about the labelled ones and
// about nothing else.
//
// The party is passed because it decides several of these questions and it
// gives nothing away. Working in the country and working abroad exclude each
// other for one worker and hold together for one enterprise, and a judge shown
// two lists of phrases with nobody attached to them can only say it is unsure.
// Every pair that gets here matched on the party, so there is one to name.
type Judge interface {
	Together(ctx context.Context, party string, a, b Scope) (bool, error)
}

// Adjudicator answers the containment question with a model.
//
// Circumstances compares two condition sets by containment, which is sound and
// often silent. Two conditions where neither set contains the other may describe
// the same situation or may exclude each other, and no amount of string
// comparison tells those apart. On the generated pairs that gap was the entire
// error of the detector: twenty of twenty near misses reported, every one of
// them a pair where one side says trong truong hop khan cap and the other says
// trong truong hop thong thuong, which share no word and exclude each other.
//
// The model is shown the two condition sets and the party they are about, and
// nothing else. It never sees an operator, an act, a deadline, a rule or a
// quote, so it has no way of knowing what either provision requires and cannot
// be answering whether the two conflict. It is asked whether one situation can
// satisfy both lists of phrases, which is a question about Vietnamese rather
// than about law, and the deterministic checker keeps the conflict verdict
// either way.
//
// The default runs one way on purpose. A model that is unsure is told to answer
// that the circumstances can hold together, so an adjudicator can only ever
// remove a finding it is confident about, and an unreachable or unhelpful model
// costs precision rather than recall.
type Adjudicator struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int

	// Calls and Usage accumulate across the run, because the caller has to be
	// able to say what a benchmark figure cost. Nothing here is called
	// concurrently, so they are plain fields.
	Calls int
	Usage api.Usage
}

const circumstancesInstructions = `Bạn được cho hai nhóm hoàn cảnh. Mỗi nhóm gồm các cụm điều kiện đã rút gọn, lấy từ hai quy định khác nhau của pháp luật Việt Nam.
Nhiệm vụ duy nhất: cho biết có tồn tại một tình huống thực tế nào mà mọi cụm của cả hai nhóm cùng đúng hay không.

Bắt buộc:
- Chỉ xét bản thân các cụm hoàn cảnh và chủ thể được nêu. Không suy đoán về nội dung quy định, không nói bên nào đúng, không nói hai quy định có mâu thuẫn hay không.
- Xét theo đúng chủ thể được nêu, trong một tình huống duy nhất. Ví dụ làm việc trong nước và làm việc ở nước ngoài loại trừ nhau đối với một người lao động.
- together = true khi có ít nhất một tình huống thoả mãn tất cả các cụm của cả hai nhóm.
- together = false chỉ khi không có tình huống nào thoả mãn được cả hai nhóm, ví dụ một bên ghi trong truong hop khan cap còn bên kia ghi trong truong hop thong thuong.
- Khi không chắc chắn, chọn together = true.
- Các cụm được lưu không dấu và nối bằng dấu gạch ngang. Đó là cách lưu trữ, không phải lỗi chính tả.

Trả về đúng một đối tượng JSON, không giải thích thêm:
{"together":true,"why":"một câu ngắn"}`

// Instructions is the system prompt this pass sends.
func (a *Adjudicator) Instructions() string { return circumstancesInstructions }

// CircumstancesPrompt renders one pair of condition sets for the judge.
func CircumstancesPrompt(party string, x, y Scope) string {
	var b strings.Builder
	if party != "" {
		fmt.Fprintf(&b, "Cả hai nhóm hoàn cảnh dưới đây nói về cùng một chủ thể: %s\n\n", party)
	}
	fmt.Fprintf(&b, "Nhóm hoàn cảnh thứ nhất:\n%s\n\n", bullets(x.Conditions))
	fmt.Fprintf(&b, "Nhóm hoàn cảnh thứ hai:\n%s\n", bullets(y.Conditions))
	return b.String()
}

func bullets(in []string) string {
	if len(in) == 0 {
		return "  (không có điều kiện nào, quy định áp dụng chung)"
	}
	var b strings.Builder
	for _, s := range in {
		fmt.Fprintf(&b, "  - %s\n", s)
	}
	return strings.TrimRight(b.String(), "\n")
}

type wireTogether struct {
	Together *bool  `json:"together"`
	Why      string `json:"why"`
}

// Together reports whether one situation can satisfy both condition sets.
func (a *Adjudicator) Together(ctx context.Context, party string, x, y Scope) (bool, error) {
	input := CircumstancesPrompt(party, x, y)
	problem := ""
	for attempt := 0; attempt <= maxCorrections(a.MaxCorrections); attempt++ {
		resp, err := a.Completer.Complete(ctx, api.Request{
			Model: a.Model, Instructions: a.Instructions(), Input: input,
		})
		a.Calls++
		if err != nil {
			return true, err
		}
		a.Usage = addUsage(a.Usage, resp.Usage)

		var wire wireTogether
		if jerr := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); jerr != nil {
			problem = "câu trả lời không phải một đối tượng JSON hợp lệ: " + jerr.Error()
		} else if wire.Together == nil {
			problem = "thiếu trường together"
		} else {
			return *wire.Together, nil
		}
		input = CircumstancesPrompt(party, x, y) + correction(problem)
	}
	// The pair keeps its finding. See the default in the type comment: a model
	// that will not answer must not be able to delete a conflict.
	return true, fmt.Errorf("no usable answer about the circumstances after %d corrections: %s",
		maxCorrections(a.MaxCorrections), problem)
}

// Adjudicate settles every finding that containment could not place.
//
// Findings the judge says can never both be triggered move out of Findings and
// into Disjoint, where they are still readable and are no longer counted as
// conflicts. The rest are marked possible, which says a model was asked and
// answered, as distinct from shared, which says the code proved containment.
//
// A finding the judge could not answer about keeps its place and its unknown
// label, and the error comes back so the caller can say how many. Detection is
// not allowed to depend on a model answering.
func Adjudicate(ctx context.Context, r *Report, j Judge) (asked, dropped, failed int, err error) {
	if r == nil || j == nil {
		return 0, 0, 0, nil
	}
	keep := r.Findings[:0]
	for i := range r.Findings {
		f := r.Findings[i]
		if f.Circumstances != CircumstancesUnknown {
			keep = append(keep, f)
			continue
		}
		if ctx.Err() != nil {
			keep = append(keep, f)
			continue
		}
		asked++
		party, _, _ := f.A.Words()
		together, jerr := j.Together(ctx, party, f.A.Scope, f.B.Scope)
		if jerr != nil {
			failed++
			if err == nil {
				err = jerr
			}
			keep = append(keep, f)
			continue
		}
		if !together {
			f.Circumstances = CircumstancesDisjoint
			r.Disjoint = append(r.Disjoint, f)
			dropped++
			continue
		}
		f.Circumstances = CircumstancesPossible
		keep = append(keep, f)
	}
	r.Findings = keep
	return asked, dropped, failed, err
}
