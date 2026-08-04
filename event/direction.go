package event

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tamnd/luatdo/api"
)

// Why a consequence graph needs its own direction pass.
//
// Every chain type in this layer is ordered. TRIGGERS, PRECEDES and
// PRECONDITION_OF all say one act comes before the other, and PRECLUDES says one
// act is the reason the other cannot be done. Read backwards, each of them still
// reads like law: a graph saying the permit precedes the application is a
// sentence somebody could nod along to, and it sends every walk of the
// consequence chain to the wrong end of the procedure. This project has already
// shipped that failure once, as 75,252 backwards amendment edges, and the
// defence that worked there is the one used here.
//
// So the chain direction is asked twice and by two different questions. The
// extractor writes a prose direction check while the quote is in view. Then this
// pass reads the quote and the two act labels in a fixed order, is never told
// which chain type was claimed or which way the first pass ran it, and answers
// which act comes first. Disagreement holds the chain at provisional and is
// reported as its own number.

// The verdicts the blind verifier may return.
const (
	verdictFirst   = "first"   // the first act comes first, or is the ground for the second
	verdictSecond  = "second"  // the other way round
	verdictNeither = "neither" // the quote does not settle it
)

// Verifier is the blind direction pass over chains.
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

// Instructions is the blind system prompt.
//
// One question covers all four chain types, and it has to, because naming the
// type would hand the verifier the claim it is meant to check. The question asks
// which act comes first or is the ground for the other, which is the thing the
// four types share: the act at the tail is the one that happens or holds, and
// the act at the head is what follows from it or is blocked by it.
func (v *Verifier) Instructions() string {
	var b strings.Builder
	b.WriteString("Bạn được cho một đoạn trích từ văn bản pháp luật Việt Nam và hai hành vi được nói tới trong đoạn đó.\n")
	b.WriteString("Nhiệm vụ: xác định theo câu chữ của đoạn trích, hành vi nào xảy ra trước, hoặc là căn cứ làm phát sinh hoặc ngăn cản hành vi kia.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. Trả về \"" + verdictFirst + "\" nếu hành vi thứ nhất xảy ra trước hoặc là căn cứ dẫn tới hành vi thứ hai.\n")
	b.WriteString("2. Trả về \"" + verdictSecond + "\" nếu ngược lại, hành vi thứ hai xảy ra trước hoặc là căn cứ dẫn tới hành vi thứ nhất.\n")
	b.WriteString("3. Trả về \"" + verdictNeither + "\" nếu đoạn trích không đủ để xác định. Đây là câu trả lời hợp lệ.\n")
	b.WriteString("4. Chỉ căn cứ vào câu chữ trong đoạn trích. Không suy đoán từ hiểu biết bên ngoài về thủ tục.\n\n")
	b.WriteString("Trả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"direction":"` + verdictFirst + `","rationale":"...","confidence":0.9}`)
	b.WriteString("\n")
	return b.String()
}

// DirectionPrompt renders one quote and two acts. The order is fixed by the
// caller and Verify fixes it lexicographically, so a model that learned nothing
// from the text cannot agree with the extractor at whatever rate it happens to
// prefer the first option.
func DirectionPrompt(quote, first, second string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Đoạn trích:\n%s\n\n", quote)
	fmt.Fprintf(&b, "Hành vi thứ nhất: %s\n", first)
	fmt.Fprintf(&b, "Hành vi thứ hai: %s\n", second)
	return b.String()
}

// Verify reads one chain blind. The labels are act labels rather than
// identifiers, because an identifier carries the class the first pass chose and
// that is a hint.
func (v *Verifier) Verify(ctx context.Context, c Chain, fromLabel, toLabel string) (string, api.Usage, error) {
	var usage api.Usage
	if len(c.Evidence) == 0 {
		return DirectionUnverified, usage, fmt.Errorf("%s has no evidence to read", c.Key())
	}
	if fromLabel == toLabel {
		return DirectionUnclear, usage, nil
	}
	swapped := fromLabel > toLabel
	first, second := fromLabel, toLabel
	if swapped {
		first, second = second, first
	}
	quote := c.Evidence[0].Quote
	input := DirectionPrompt(quote, first, second)

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
			input = DirectionPrompt(quote, first, second) +
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
			input = DirectionPrompt(quote, first, second) +
				fmt.Sprintf("\nLần trả lời trước bị từ chối: direction %q không phải %s, %s hoặc %s.\nTrả lời lại.\n",
					wire.Direction, verdictFirst, verdictSecond, verdictNeither)
		}
	}
	// An answer nobody could parse is not agreement.
	return DirectionUnclear, usage, nil
}

// DirectionScore is the chain direction metric, reported on its own and never
// folded into event precision. One number covering both hides which half broke.
type DirectionScore struct {
	Chains    int `json:"chains"`
	Agreed    int `json:"agreed"`
	Flipped   int `json:"flipped"`
	Unclear   int `json:"unclear"`
	Unchecked int `json:"unchecked"`
}

// ScoreDirection counts the verdicts over a set of chains.
func ScoreDirection(chains []Chain) DirectionScore {
	var s DirectionScore
	for _, c := range chains {
		s.Chains++
		switch c.Direction {
		case DirectionAgreed:
			s.Agreed++
		case DirectionFlipped, DirectionDisputed:
			s.Flipped++
		case DirectionUnclear:
			s.Unclear++
		default:
			s.Unchecked++
		}
	}
	return s
}

// Accuracy is agreement over the chains the verifier could read. A chain it
// could not read is not evidence either way, so it stays out of the denominator
// and is reported beside it rather than counted as a pass.
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
	fmt.Fprintf(&b, "chain direction %d chains: %d agreed, %d flipped, %d unclear, %d unchecked\n",
		s.Chains, s.Agreed, s.Flipped, s.Unclear, s.Unchecked)
	if decided := s.Agreed + s.Flipped; decided > 0 {
		fmt.Fprintf(&b, "                %.1f%% of the %d the verifier could read, scored on its own and never folded into event precision\n",
			100*s.Accuracy(), decided)
	} else {
		fmt.Fprintf(&b, "                nothing was verified, so nothing here says the chains point the right way\n")
	}
	return b.String()
}
