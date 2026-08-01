package distill

import (
	"strings"
	"testing"
)

func TestEvaluateAgainstTheTeacherIsAgreement(t *testing.T) {
	examples := teacherOutput()
	tagger := Train(examples, 8)
	s := Evaluate(tagger, examples, SourceTeacher)
	if s.Against != SourceTeacher {
		t.Errorf("the reference is not named on the score: %q", s.Against)
	}
	if s.Examples != len(examples) {
		t.Errorf("scored %d examples of %d", s.Examples, len(examples))
	}
	if s.Reference == 0 {
		t.Fatal("no reference spans, so the score means nothing")
	}
	if s.Recall == 0 {
		t.Errorf("the student recovered none of its own training data: %+v", s)
	}
}

func TestEvaluateMatchesOnFoldedFormRatherThanOffsets(t *testing.T) {
	// A teacher and a student that both found thoi gio lam viec binh thuong
	// agree, and making them disagree over a capital letter would measure
	// tokenisation instead of tagging.
	examples := teacherOutput()
	tagger := Train(examples, 8)

	shifted := []Example{{
		ProvisionID: examples[0].ProvisionID,
		Text:        examples[0].Text,
		Source:      SourceGold,
		Spans:       []Span{{Text: "thời giờ làm việc bình thường", Start: 0, End: 1, Kind: "amount"}},
	}}
	if s := Evaluate(tagger, shifted, SourceGold); s.Exact == 0 {
		t.Errorf("a case difference was scored as a miss: %+v", s)
	}
}

func TestEvaluateReportsBoundaryFailuresSeparately(t *testing.T) {
	// A student that finds thoi gio lam viec where the teacher said thoi gio
	// lam viec binh thuong has found the concept and got the boundary wrong,
	// which is a different failure from finding nothing.
	examples := teacherOutput()
	tagger := Train(examples, 8)
	shorter := []Example{{
		ProvisionID: examples[0].ProvisionID,
		Text:        examples[0].Text,
		Source:      SourceGold,
		Spans:       []Span{{Text: "Thời giờ làm việc", Start: 0, End: len("Thời giờ làm việc")}},
	}}
	s := Evaluate(tagger, shorter, SourceGold)
	if s.Overlap == 0 {
		t.Errorf("a boundary only failure was not counted: %+v", s)
	}
	if s.Exact != 0 {
		t.Errorf("a boundary failure was counted as exact: %+v", s)
	}
}

func TestEvaluateScoresKindsOnlyOverExactMatches(t *testing.T) {
	// A kind on a span that is not a concept is not wrong about the kind.
	examples := teacherOutput()
	tagger := Train(examples, 8)
	s := Evaluate(tagger, examples, SourceTeacher)
	if s.KindOf > s.Exact {
		t.Errorf("kinds were scored over %d spans but only %d matched exactly", s.KindOf, s.Exact)
	}
	if s.KindRight > s.KindOf {
		t.Errorf("more kinds right than kinds scored: %+v", s)
	}
}

func TestScoreStringNamesWhatItWasMeasuredAgainst(t *testing.T) {
	// The same student measured against the teacher and against the gold set
	// produces two numbers that mean completely different things, and they get
	// confused the moment they are printed without their labels.
	out := Score{Against: SourceGold, Examples: 10, Predicted: 20, Reference: 25, Exact: 15, KindOf: 15, KindRight: 12}.String()
	if !strings.Contains(out, SourceGold) {
		t.Errorf("the reference is not printed: %s", out)
	}
	if !strings.Contains(out, "kind") {
		t.Errorf("the kind accuracy is not printed: %s", out)
	}
}

func TestSplitIsStableAcrossMachines(t *testing.T) {
	// There is no random source, so a number reported here can be reproduced by
	// somebody else on another platform.
	examples := teacherOutput()
	train1, test1 := Split(examples, 0.5)
	shuffled := []Example{examples[2], examples[3], examples[0], examples[1]}
	train2, test2 := Split(shuffled, 0.5)

	if len(train1) != len(train2) || len(test1) != len(test2) {
		t.Fatalf("the split moved with the order: %d/%d and %d/%d", len(train1), len(test1), len(train2), len(test2))
	}
	for i := range train1 {
		if train1[i].ProvisionID != train2[i].ProvisionID {
			t.Errorf("training set differs at %d: %s and %s", i, train1[i].ProvisionID, train2[i].ProvisionID)
		}
	}
}

func TestSplitPutsEveryExampleOnExactlyOneSide(t *testing.T) {
	examples := teacherOutput()
	train, test := Split(examples, 0.3)
	if len(train)+len(test) != len(examples) {
		t.Fatalf("%d train plus %d test is not %d", len(train), len(test), len(examples))
	}
	seen := map[string]bool{}
	for _, e := range append(append([]Example{}, train...), test...) {
		if seen[e.ProvisionID] {
			t.Errorf("%s is on both sides", e.ProvisionID)
		}
		seen[e.ProvisionID] = true
	}
}

func TestSplitOfZeroHoldsNothingBack(t *testing.T) {
	train, test := Split(teacherOutput(), 0)
	if len(test) != 0 || len(train) != 4 {
		t.Errorf("a holdout of zero split %d off", len(test))
	}
}
