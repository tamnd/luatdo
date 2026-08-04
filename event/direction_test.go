package event

import (
	"context"
	"strings"
	"testing"
)

func verifier(model *answers) *Verifier {
	return &Verifier{Completer: model, Model: "test", MaxCorrections: 1}
}

func toVerify() Chain {
	return Chain{
		FromID: ID("SUBMIT", "nộp hồ sơ"), ToID: ID("ISSUE", "cấp giấy phép"), Type: Precedes,
		Evidence: []Evidence{{ProvisionID: "p1", Quote: "trong thời hạn 03 ngày kể từ ngày nhận hồ sơ, cơ quan cấp giấy phép"}},
	}
}

func TestVerifyAgreesWhenTheBlindReadingMatches(t *testing.T) {
	// The labels sort with cấp before nộp, so the chain's tail is presented
	// second and agreement means the verifier answered "second".
	model := &answers{replies: []string{`{"direction":"second","rationale":"nhận hồ sơ trước","confidence":0.9}`}}
	got, usage, err := verifier(model).Verify(context.Background(), toVerify(), "nộp hồ sơ", "cấp giấy phép")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != DirectionAgreed {
		t.Errorf("direction: got %s, want agreed", got)
	}
	if usage.TotalTokens == 0 {
		t.Error("usage was not carried back")
	}
}

func TestVerifyCatchesABackwardsChain(t *testing.T) {
	model := &answers{replies: []string{`{"direction":"first","rationale":"cấp trước","confidence":0.8}`}}
	got, _, err := verifier(model).Verify(context.Background(), toVerify(), "nộp hồ sơ", "cấp giấy phép")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != DirectionFlipped {
		t.Errorf("direction: got %s, want flipped", got)
	}
}

func TestVerifyPresentsTheActsInAFixedOrder(t *testing.T) {
	// A verifier always shown the claimed tail first agrees at whatever rate it
	// prefers the first option, and that number then looks like verification.
	model := &answers{replies: []string{`{"direction":"second"}`, `{"direction":"second"}`}}
	v := verifier(model)
	if _, _, err := v.Verify(context.Background(), toVerify(), "nộp hồ sơ", "cấp giấy phép"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if _, _, err := v.Verify(context.Background(), toVerify().Reverse(), "cấp giấy phép", "nộp hồ sơ"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if model.inputs[0] != model.inputs[1] {
		t.Errorf("one pair was presented two ways:\n%s\nand\n%s", model.inputs[0], model.inputs[1])
	}
}

func TestVerifyNeverNamesTheClaim(t *testing.T) {
	model := &answers{replies: []string{`{"direction":"second"}`}}
	v := verifier(model)
	if _, _, err := v.Verify(context.Background(), toVerify(), "nộp hồ sơ", "cấp giấy phép"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	prompt := model.inputs[0] + v.Instructions()
	for _, leak := range []string{Precedes, Triggers, PreconditionOf, Precludes, "vn:event:"} {
		if strings.Contains(prompt, leak) {
			t.Errorf("the blind pass was shown %q, so it is checking a claim rather than the text", leak)
		}
	}
}

func TestVerifyAcceptsThatTheQuoteDoesNotSettleIt(t *testing.T) {
	model := &answers{replies: []string{`{"direction":"neither","rationale":"đoạn trích không nói"}`}}
	got, _, err := verifier(model).Verify(context.Background(), toVerify(), "nộp hồ sơ", "cấp giấy phép")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != DirectionUnclear {
		t.Errorf("direction: got %s, want unclear", got)
	}
}

func TestVerifySendsBackAVerdictItDoesNotKnow(t *testing.T) {
	model := &answers{replies: []string{`{"direction":"maybe"}`, `{"direction":"second"}`}}
	got, _, err := verifier(model).Verify(context.Background(), toVerify(), "nộp hồ sơ", "cấp giấy phép")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != DirectionAgreed {
		t.Errorf("direction: got %s, want agreed after the correction", got)
	}
	if model.calls != 2 {
		t.Errorf("calls: got %d, want 2", model.calls)
	}
}

func TestVerifyTreatsAnUnreadableAnswerAsUnclearAndNotAsAgreement(t *testing.T) {
	model := &answers{replies: []string{"nothing like json"}}
	got, _, err := verifier(model).Verify(context.Background(), toVerify(), "nộp hồ sơ", "cấp giấy phép")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != DirectionUnclear {
		t.Errorf("direction: got %s, want unclear, because an answer nobody could parse is not agreement", got)
	}
}

func TestVerifyRefusesAChainWithNothingToRead(t *testing.T) {
	c := toVerify()
	c.Evidence = nil
	if _, _, err := verifier(&answers{}).Verify(context.Background(), c, "a", "b"); err == nil {
		t.Fatal("a chain with no quote was verified out of thin air")
	}
}

func TestScoreDirectionKeepsTheUnreadableOutOfTheDenominator(t *testing.T) {
	s := ScoreDirection([]Chain{
		{Direction: DirectionAgreed}, {Direction: DirectionAgreed}, {Direction: DirectionAgreed},
		{Direction: DirectionFlipped},
		{Direction: DirectionUnclear},
		{Direction: DirectionUnverified},
	})
	if s.Chains != 6 || s.Agreed != 3 || s.Flipped != 1 || s.Unclear != 1 || s.Unchecked != 1 {
		t.Fatalf("counts: %+v", s)
	}
	if got := s.Accuracy(); got != 0.75 {
		t.Errorf("accuracy: got %v, want 0.75 over the four the verifier could read", got)
	}
	report := s.String()
	if !strings.Contains(report, "unchecked") || !strings.Contains(report, "unclear") {
		t.Errorf("the report hides what was not scored: %s", report)
	}
}

func TestScoreDirectionSaysNothingWhenNothingWasVerified(t *testing.T) {
	s := ScoreDirection([]Chain{{}, {}})
	if s.Accuracy() != 0 {
		t.Errorf("accuracy: got %v, want 0", s.Accuracy())
	}
	if !strings.Contains(s.String(), "nothing was verified") {
		t.Errorf("an unverified layer reports a number as if it meant something: %s", s.String())
	}
}

func TestDisputedCountsAsFlipped(t *testing.T) {
	// A chain two provisions read opposite ways is not half right.
	s := ScoreDirection([]Chain{{Direction: DirectionDisputed}})
	if s.Flipped != 1 {
		t.Errorf("counts: %+v", s)
	}
}
