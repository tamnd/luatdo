package relation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tamnd/luatdo/api"
)

// The lesson this file exists for.
//
// M4 read the vbpl.vn relationship labels in the wrong direction and produced
// 75,252 backwards AMENDS edges. Every one of them looked entirely reasonable.
// Relation direction is the same failure with the same signature: a REQUIRES
// edge pointing from the land certificate to the building permit reads fine to
// anyone skimming, and it is exactly wrong, and no amount of relation precision
// saves a graph whose arrows point the wrong way. A graph with 95 percent
// relation precision and 80 percent direction accuracy is worse than useless for
// traversal, because it answers confidently and answers backwards.
//
// So direction gets three defences. The extractor states it in prose while the
// quote is in view, which is a cheap consistency signal. A second pass reads
// only the quote and the two concepts and says which way the text runs, blind
// to what the first pass answered. And the gold set scores it as its own metric
// rather than folding it into relation precision.

// The verdicts the blind verifier may return, in its own terms rather than the
// extractor's, so nothing about the claim leaks into the question.
const (
	verdictFirst   = "first"   // the relation runs from the first concept to the second
	verdictSecond  = "second"  // it runs the other way
	verdictNeither = "neither" // the quote does not settle it
)

// Verifier is the blind direction pass.
type Verifier struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
}

type wireDirection struct {
	Direction  string  `json:"direction"`
	Rationale  string  `json:"rationale"`
	Confidence float64 `json:"confidence"`
}

// Instructions is the blind direction system prompt. It never names the
// relation type and never shows what the first pass said, because a verifier
// shown a claim verifies the claim rather than the text.
func (v *Verifier) Instructions() string {
	var b strings.Builder
	b.WriteString("Bạn được cho một đoạn trích từ văn bản pháp luật và hai khái niệm xuất hiện trong đoạn đó.\n")
	b.WriteString("Nhiệm vụ: xác định đoạn trích thể hiện quan hệ theo chiều nào giữa hai khái niệm.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. Trả về \"" + verdictFirst + "\" nếu quan hệ đi từ khái niệm thứ nhất đến khái niệm thứ hai, ví dụ khái niệm thứ nhất cần, tạo ra, hoặc bao gồm khái niệm thứ hai.\n")
	b.WriteString("2. Trả về \"" + verdictSecond + "\" nếu quan hệ đi theo chiều ngược lại.\n")
	b.WriteString("3. Trả về \"" + verdictNeither + "\" nếu đoạn trích không đủ để xác định chiều. Đây là câu trả lời hợp lệ.\n")
	b.WriteString("4. Chỉ căn cứ vào câu chữ trong đoạn trích. Không suy đoán từ hiểu biết bên ngoài.\n\n")
	b.WriteString("Trả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"direction":"` + verdictFirst + `","rationale":"...","confidence":0.9}`)
	b.WriteString("\n")
	return b.String()
}

// DirectionPrompt renders one quote and two concepts.
//
// The two concepts are presented in the order the caller passes them, and
// Verify fixes that order lexicographically rather than putting the claimed
// subject first. Presenting the subject first every time would let a model that
// learned nothing from the text agree with the extractor at whatever rate it
// prefers the first option, and the number would look like verification.
func DirectionPrompt(quote, first, second string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Đoạn trích:\n%s\n\n", quote)
	fmt.Fprintf(&b, "Khái niệm thứ nhất: %s\n", first)
	fmt.Fprintf(&b, "Khái niệm thứ hai: %s\n", second)
	return b.String()
}

// Verify reads one edge blind and returns the direction verdict for it. The
// labels are the endpoint labels, not their identifiers, because an identifier
// carries the document it came from and that is a hint about nothing useful.
func (v *Verifier) Verify(ctx context.Context, e Edge, fromLabel, toLabel string) (string, api.Usage, error) {
	var usage api.Usage
	if len(e.Evidence) == 0 {
		return DirectionUnverified, usage, fmt.Errorf("%s has no evidence to read", e.Key())
	}
	// Fixed by the labels, so the presentation order is a property of the pair
	// and not of what the extractor claimed about it.
	swapped := fromLabel > toLabel
	first, second := fromLabel, toLabel
	if swapped {
		first, second = second, first
	}
	input := DirectionPrompt(e.Evidence[0].Quote, first, second)

	for attempt := 0; attempt <= maxCorrections(v.MaxCorrections); attempt++ {
		resp, err := v.Completer.Complete(ctx, api.Request{
			Model: v.Model, Instructions: v.Instructions(), Input: input,
		})
		if err != nil {
			return DirectionUnverified, usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var wire wireDirection
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			input = DirectionPrompt(e.Evidence[0].Quote, first, second) +
				"\nLần trả lời trước không phải một đối tượng JSON hợp lệ: " + err.Error() + "\nTrả lời lại.\n"
			continue
		}
		switch strings.ToLower(strings.TrimSpace(wire.Direction)) {
		case verdictNeither:
			return DirectionUnclear, usage, nil
		case verdictFirst:
			if swapped {
				return DirectionFlipped, usage, nil
			}
			return DirectionAgreed, usage, nil
		case verdictSecond:
			if swapped {
				return DirectionAgreed, usage, nil
			}
			return DirectionFlipped, usage, nil
		default:
			input = DirectionPrompt(e.Evidence[0].Quote, first, second) +
				fmt.Sprintf("\nLần trả lời trước bị từ chối: direction %q không phải %s, %s hoặc %s.\nTrả lời lại.\n",
					wire.Direction, verdictFirst, verdictSecond, verdictNeither)
		}
	}
	// An answer nobody could parse is not agreement. Leaving it unclear keeps
	// the edge in the graph and out of anything that trusts direction.
	return DirectionUnclear, usage, nil
}

// DirectionScore is the direction metric, kept apart from relation precision on
// purpose. Folding the two produces one number that hides which half is broken.
type DirectionScore struct {
	Edges     int `json:"edges"`
	Agreed    int `json:"agreed"`
	Flipped   int `json:"flipped"`
	Unclear   int `json:"unclear"`
	Unchecked int `json:"unchecked"`
	Symmetric int `json:"symmetric"`
}

// ScoreDirection counts the verdicts over a set of edges.
func ScoreDirection(edges []Edge) DirectionScore {
	var s DirectionScore
	for _, e := range edges {
		s.Edges++
		switch e.Direction {
		case DirectionAgreed:
			s.Agreed++
		case DirectionFlipped, DirectionDisputed:
			s.Flipped++
		case DirectionUnclear:
			s.Unclear++
		case DirectionSymmetric:
			s.Symmetric++
		default:
			s.Unchecked++
		}
	}
	return s
}

// Accuracy is agreement over the edges the verifier could actually read. An
// edge it could not read is not evidence either way, so it is out of the
// denominator and reported separately rather than counted as a pass.
func (s DirectionScore) Accuracy() float64 {
	decided := s.Agreed + s.Flipped
	if decided == 0 {
		return 0
	}
	return float64(s.Agreed) / float64(decided)
}

// String prints the metric with its denominators visible.
func (s DirectionScore) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "direction      %d edges: %d agreed, %d flipped, %d unclear, %d unchecked, %d symmetric\n",
		s.Edges, s.Agreed, s.Flipped, s.Unclear, s.Unchecked, s.Symmetric)
	if decided := s.Agreed + s.Flipped; decided > 0 {
		fmt.Fprintf(&b, "               %.1f%% of the %d the verifier could read, scored on its own and never folded into relation precision\n",
			100*s.Accuracy(), decided)
	} else {
		fmt.Fprintf(&b, "               nothing was verified, so nothing here says the arrows point the right way\n")
	}
	return b.String()
}
