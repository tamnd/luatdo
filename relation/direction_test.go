package relation

import (
	"context"
	"strings"
	"testing"
)

func withQuote(quote string) Edge {
	return Edge{FromID: "c1", ToID: "c2", Type: Requires,
		Evidence: []Evidence{{ProvisionID: "p1", DocID: "d1", Quote: quote,
			DirectionCheck: "giấy phép cần giấy chứng nhận"}}}
}

func TestVerifyIsBlindToWhatTheExtractorClaimed(t *testing.T) {
	// A verifier shown a claim verifies the claim rather than the text, which is
	// how a second pass ends up agreeing with the first for free.
	model := &answers{replies: []string{`{"direction":"first","rationale":"x","confidence":0.9}`}}
	v := &Verifier{Completer: model, Model: "m"}
	if _, _, err := v.Verify(context.Background(), withQuote("q"), "giấy phép", "hồ sơ"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	prompt := model.inputs[0] + v.Instructions()
	for what, leak := range map[string]string{
		"the relation type":              Requires,
		"the subject identifier":         "c1",
		"the object identifier":          "c2",
		"the extractor's direction call": "giấy phép cần giấy chứng nhận",
	} {
		if strings.Contains(prompt, leak) {
			t.Errorf("the blind pass was shown %s, and a verifier shown the claim verifies the claim", what)
		}
	}
	if !strings.Contains(prompt, "q") {
		t.Error("the quote never reached the verifier, so it read nothing")
	}
}

func TestVerifyFixesThePresentationOrderByTheLabels(t *testing.T) {
	// Presenting the claimed subject first every time would let a model that
	// learned nothing from the text agree with the extractor at whatever rate it
	// prefers the first option, and the number would look like verification.
	//
	// The same pair is presented the same way round whichever way the edge runs,
	// so the same verdict word means opposite things and the mapping has to
	// account for it.
	forward := &answers{replies: []string{`{"direction":"first","rationale":"x","confidence":0.9}`}}
	v := &Verifier{Completer: forward, Model: "m"}
	got, _, err := v.Verify(context.Background(), withQuote("q"), "a-giấy phép", "b-hồ sơ")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != DirectionAgreed {
		t.Errorf("verdict = %q, the labels were already in order", got)
	}

	backward := &answers{replies: []string{`{"direction":"first","rationale":"x","confidence":0.9}`}}
	v = &Verifier{Completer: backward, Model: "m"}
	got, _, err = v.Verify(context.Background(), withQuote("q"), "b-hồ sơ", "a-giấy phép")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != DirectionFlipped {
		t.Errorf("verdict = %q, the same answer about the same pair means the edge runs the other way", got)
	}

	if forward.inputs[0] != backward.inputs[0] {
		t.Error("the two edges were presented differently, so the verifier could tell which way the claim ran")
	}
}

func TestVerifyMapsEveryVerdict(t *testing.T) {
	for name, tc := range map[string]struct {
		reply    string
		from, to string
		want     string
	}{
		"second on ordered labels":      {"second", "a", "b", DirectionFlipped},
		"second on swapped labels":      {"second", "b", "a", DirectionAgreed},
		"neither settles nothing":       {"neither", "a", "b", DirectionUnclear},
		"neither on swapped labels":     {"neither", "b", "a", DirectionUnclear},
		"upper case is still an answer": {"FIRST", "a", "b", DirectionAgreed},
		"padded":                        {"  first  ", "a", "b", DirectionAgreed},
	} {
		model := &answers{replies: []string{`{"direction":"` + tc.reply + `","rationale":"x","confidence":0.9}`}}
		v := &Verifier{Completer: model, Model: "m"}
		got, _, err := v.Verify(context.Background(), withQuote("q"), tc.from, tc.to)
		if err != nil {
			t.Errorf("%s: Verify: %v", name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: verdict = %q, want %q", name, got, tc.want)
		}
	}
}

func TestVerifyCorrectsAVerdictItDoesNotRecognise(t *testing.T) {
	model := &answers{replies: []string{
		`{"direction":"cả hai","rationale":"x","confidence":0.5}`,
		`{"direction":"first","rationale":"x","confidence":0.9}`,
	}}
	v := &Verifier{Completer: model, Model: "m", MaxCorrections: 1}
	got, _, err := v.Verify(context.Background(), withQuote("q"), "a", "b")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != DirectionAgreed {
		t.Errorf("verdict = %q, want the corrected answer", got)
	}
	if !strings.Contains(model.inputs[1], "cả hai") {
		t.Error("the correction did not name the answer it refused")
	}
}

func TestVerifyTreatsAnUnreadableAnswerAsUnclear(t *testing.T) {
	// Not as agreement. Leaving it unclear keeps the edge in the graph and out
	// of anything that trusts direction.
	model := &answers{replies: []string{"tôi không chắc"}}
	v := &Verifier{Completer: model, Model: "m", MaxCorrections: 1}
	got, _, err := v.Verify(context.Background(), withQuote("q"), "a", "b")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != DirectionUnclear {
		t.Errorf("verdict = %q, an answer nobody could read is not agreement", got)
	}
}

func TestVerifyRefusesAnEdgeWithNothingToRead(t *testing.T) {
	v := &Verifier{Completer: &answers{replies: []string{`{"direction":"first"}`}}, Model: "m"}
	e := Edge{FromID: "c1", ToID: "c2", Type: Requires}
	if _, _, err := v.Verify(context.Background(), e, "a", "b"); err == nil {
		t.Error("an edge with no evidence was verified, which is verification of nothing")
	}
}

func TestScoreDirectionKeepsWhatItCouldNotReadOutOfTheDenominator(t *testing.T) {
	// An edge the verifier could not read is not evidence either way, and
	// counting it as a pass would be reporting a number that means nothing.
	edges := []Edge{
		{Direction: DirectionAgreed}, {Direction: DirectionAgreed}, {Direction: DirectionAgreed},
		{Direction: DirectionFlipped},
		{Direction: DirectionDisputed},
		{Direction: DirectionUnclear},
		{Direction: DirectionUnverified},
		{},
	}
	s := ScoreDirection(edges)
	if s.Edges != 8 || s.Agreed != 3 || s.Flipped != 2 || s.Unclear != 1 || s.Unchecked != 2 {
		t.Fatalf("score = %+v", s)
	}
	if got := s.Accuracy(); got != 0.6 {
		t.Errorf("accuracy = %v, want 3 of the 5 it could read", got)
	}
	out := s.String()
	if !strings.Contains(out, "unchecked") || !strings.Contains(out, "never folded into relation precision") {
		t.Errorf("the metric hides its denominators:\n%s", out)
	}

	// Nothing verified is not a perfect score, and saying so is the point.
	empty := ScoreDirection([]Edge{{}, {}})
	if empty.Accuracy() != 0 {
		t.Errorf("accuracy = %v with nothing verified", empty.Accuracy())
	}
	if !strings.Contains(empty.String(), "nothing here says the arrows point the right way") {
		t.Errorf("an unverified set printed as though it had been checked:\n%s", empty.String())
	}
}
