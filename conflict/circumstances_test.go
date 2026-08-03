package conflict

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// judge answers from a script, and records what it was shown, because what the
// judge is not shown is the point of the design.
type judge struct {
	answer bool
	err    error
	calls  int
	party  string
	seen   []Scope
}

func (j *judge) Together(_ context.Context, party string, a, b Scope) (bool, error) {
	j.calls++
	j.party = party
	j.seen = append(j.seen, a, b)
	return j.answer, j.err
}

// disjointPair is the pair the containment test cannot place: an obligation and
// a prohibition on one party and one act, each under a condition the other does
// not have.
func disjointPair() (*Form, *Form) {
	a, b := pair(Obligation, Prohibition)
	a.Scope.Conditions = []string{"trong-truong-hop-khan-cap"}
	b.Scope.Conditions = []string{"trong-truong-hop-thong-thuong"}
	return a, b
}

func TestAdjudicateDropsAPairThatCannotBothBeTriggered(t *testing.T) {
	a, b := disjointPair()
	r := Check([]*Form{a, b}, nil)
	j := &judge{answer: false}

	asked, dropped, failed, err := Adjudicate(context.Background(), r, j)
	if err != nil || asked != 1 || dropped != 1 || failed != 0 {
		t.Fatalf("asked %d, dropped %d, failed %d, err %v", asked, dropped, failed, err)
	}
	if len(r.Findings) != 0 {
		t.Errorf("%d findings survived a pair that is never triggered together", len(r.Findings))
	}
	// Dropped rather than deleted, so somebody who disagrees with the judge can
	// see what it took.
	if len(r.Disjoint) != 1 || r.Disjoint[0].Circumstances != CircumstancesDisjoint {
		t.Errorf("disjoint = %+v", r.Disjoint)
	}
	if s := r.String(); !strings.Contains(s, "cannot both hold") {
		t.Errorf("the report does not say what was dropped:\n%s", s)
	}
}

func TestAdjudicateKeepsAPairTheJudgeAllows(t *testing.T) {
	a, b := disjointPair()
	r := Check([]*Form{a, b}, nil)
	j := &judge{answer: true}

	if _, dropped, _, err := Adjudicate(context.Background(), r, j); err != nil || dropped != 0 {
		t.Fatalf("dropped %d, err %v", dropped, err)
	}
	if len(r.Findings) != 1 {
		t.Fatalf("findings = %d", len(r.Findings))
	}
	// Possible and not shared. Containment was not proved, a model said the two
	// can hold at once, and the report says which of those happened.
	if got := r.Findings[0].Circumstances; got != CircumstancesPossible {
		t.Errorf("circumstances = %q, want %q", got, CircumstancesPossible)
	}
	if r.Shared() != 0 {
		t.Errorf("an answer from a model was counted as containment proved here")
	}
}

func TestAdjudicateShowsTheJudgeTheCircumstancesAndNothingElse(t *testing.T) {
	a, b := disjointPair()
	r := Check([]*Form{a, b}, nil)
	j := &judge{answer: true}
	if _, _, _, err := Adjudicate(context.Background(), r, j); err != nil {
		t.Fatal(err)
	}
	if len(j.seen) != 2 {
		t.Fatalf("the judge saw %d scopes", len(j.seen))
	}
	// The party is named and nothing else is. Without an operator, an act or a
	// deadline there is no way to know what either provision requires, so the
	// question this pass can ask is not the question the checker answers.
	if want, _, _ := a.Words(); j.party != want {
		t.Errorf("the judge was told the party is %q, want %q", j.party, want)
	}
	prompt := CircumstancesPrompt(j.party, j.seen[0], j.seen[1])
	if !strings.Contains(prompt, j.party) {
		t.Errorf("the prompt does not say whose circumstances these are:\n%s", prompt)
	}
	for _, forbidden := range []string{a.Operator, b.Operator, a.Act, a.Source.Quote} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("the prompt shows the judge %q, which lets it answer the question the checker owns:\n%s",
				forbidden, prompt)
		}
	}
	for _, want := range []string{"trong-truong-hop-khan-cap", "trong-truong-hop-thong-thuong"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt does not carry %q:\n%s", want, prompt)
		}
	}
}

func TestAdjudicateLeavesTheProvedPairsAlone(t *testing.T) {
	// Containment holds here, so there is nothing to ask and asking would cost a
	// call to be told what the code already knows.
	a, b := pair(Obligation, Prohibition)
	r := Check([]*Form{a, b}, nil)
	j := &judge{answer: false}
	asked, dropped, _, err := Adjudicate(context.Background(), r, j)
	if err != nil || asked != 0 || dropped != 0 || j.calls != 0 {
		t.Fatalf("asked %d, dropped %d, calls %d, err %v", asked, dropped, j.calls, err)
	}
	if len(r.Findings) != 1 || r.Findings[0].Circumstances != CircumstancesShared {
		t.Errorf("a proved finding was changed: %+v", r.Findings)
	}
}

func TestAdjudicateKeepsTheFindingWhenTheJudgeWillNotAnswer(t *testing.T) {
	a, b := disjointPair()
	r := Check([]*Form{a, b}, nil)
	j := &judge{answer: true, err: errors.New("no")}

	asked, dropped, failed, err := Adjudicate(context.Background(), r, j)
	if err == nil {
		t.Fatal("the caller was not told the judge failed")
	}
	if asked != 1 || dropped != 0 || failed != 1 {
		t.Fatalf("asked %d, dropped %d, failed %d", asked, dropped, failed)
	}
	// A model that will not answer must not be able to delete a conflict.
	if len(r.Findings) != 1 || r.Findings[0].Circumstances != CircumstancesUnknown {
		t.Errorf("findings = %+v", r.Findings)
	}
}

func TestAdjudicateWithNoJudge(t *testing.T) {
	a, b := disjointPair()
	r := Check([]*Form{a, b}, nil)
	if asked, dropped, failed, err := Adjudicate(context.Background(), r, nil); asked+dropped+failed != 0 || err != nil {
		t.Fatalf("asked %d, dropped %d, failed %d, err %v", asked, dropped, failed, err)
	}
	if len(r.Findings) != 1 || r.Findings[0].Circumstances != CircumstancesUnknown {
		t.Errorf("a run with no judge changed the report: %+v", r.Findings)
	}
}

func TestAdjudicateStopsAtACancelledContext(t *testing.T) {
	a, b := disjointPair()
	r := Check([]*Form{a, b}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	j := &judge{answer: false}
	if asked, _, _, _ := Adjudicate(ctx, r, j); asked != 0 || j.calls != 0 {
		t.Errorf("asked %d and called %d times after the run was cancelled", asked, j.calls)
	}
	if len(r.Findings) != 1 {
		t.Errorf("a cancelled run dropped a finding: %+v", r.Findings)
	}
}

// tableJudge answers from the same tables the generator plants from, which is
// what a judge that is right every time would say. It stands in for the model so
// the tests can show what the pass is worth when the answers are right, beside
// what it costs when they are not.
type tableJudge struct{ calls int }

func (j *tableJudge) Together(_ context.Context, _ string, a, b Scope) (bool, error) {
	j.calls++
	// Either way round, because a finding puts its two forms in a fixed order
	// that has nothing to do with which side the generator planted.
	for _, p := range exclusive {
		if has(a.Conditions, p[0]) && has(b.Conditions, p[1]) {
			return false, nil
		}
		if has(b.Conditions, p[0]) && has(a.Conditions, p[1]) {
			return false, nil
		}
	}
	return true, nil
}

func TestGradeWithAJudgeThatIsRightEveryTime(t *testing.T) {
	cases := Build(seeds(), 0)
	g := GradeCases(context.Background(), cases, nil, &tableJudge{})

	// The two condition mutations are the whole error of the deterministic
	// checker and they pull in opposite directions, so a judge that answers both
	// correctly is the only way to score one on both numbers.
	for _, mut := range []string{MutCondition, MutOverlap} {
		k := g.ByMutation[mut]
		if k == nil || k.Correct != k.Cases {
			t.Fatalf("%s scored %+v", mut, k)
		}
	}
	if g.Precision != 1 || g.Recall != 1 {
		t.Errorf("precision %.2f, recall %.2f with a judge that is right every time", g.Precision, g.Recall)
	}
	if g.Adjudicated == 0 || g.Dropped == 0 || g.Dropped == g.Adjudicated {
		t.Errorf("adjudicated %d and dropped %d, want the judge answering both ways", g.Adjudicated, g.Dropped)
	}
	// The pairs the code placed on its own cost nothing.
	if g.Adjudicated >= g.Cases {
		t.Errorf("%d calls over %d cases, so the containment test bought nothing", g.Adjudicated, g.Cases)
	}
	if s := g.String(); !strings.Contains(s, "went to the judge") {
		t.Errorf("the grade does not say a model was behind it:\n%s", s)
	}
}

func TestAJudgeWithOneAnswerCannotScoreOnBothMutations(t *testing.T) {
	// This is why the compatible conditions mutation exists. Without it a judge
	// that says disjoint to everything scores a perfect precision and nothing in
	// the benchmark notices what it deleted.
	cases := Build(seeds(), 0)
	always := GradeCases(context.Background(), cases, nil, &judge{answer: false})
	if always.Recall >= 1 {
		t.Errorf("a judge that drops every pair kept recall at %.2f", always.Recall)
	}
	if always.FalseNegatives != always.ByMutation[MutOverlap].Cases {
		t.Errorf("%d conflicts lost against %d compatible condition cases",
			always.FalseNegatives, always.ByMutation[MutOverlap].Cases)
	}

	never := GradeCases(context.Background(), cases, nil, &judge{answer: true})
	if never.Precision >= 1 {
		t.Errorf("a judge that allows every pair reached precision %.2f", never.Precision)
	}
	if never.Recall != 1 {
		t.Errorf("a judge that allows every pair cost recall %.2f, which it cannot do", never.Recall)
	}
}

func TestGradeSaysWhenNoJudgeWasBehindIt(t *testing.T) {
	s := GradeCases(context.Background(), Build(seeds(), 1), nil, nil).String()
	if !strings.Contains(s, "no judge behind this run") {
		t.Errorf("a grade with no judge does not say so:\n%s", s)
	}
}

func TestTogetherReadsTheAnswer(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{`{"together":true,"why":"cả hai đều nói về hồ sơ"}`, true},
		{`{"together":false,"why":"một bên khẩn cấp, một bên thông thường"}`, false},
		{"```json\n{\"together\":false}\n```", false},
	} {
		c := &answers{replies: []string{tc.text}}
		a := &Adjudicator{Completer: c, Model: "m"}
		got, err := a.Together(context.Background(), "người lao động", Scope{}, Scope{})
		if err != nil || got != tc.want {
			t.Errorf("%s gave %v, %v", tc.text, got, err)
		}
		if a.Calls != 1 {
			t.Errorf("%d calls for one answer", a.Calls)
		}
	}
}

func TestTogetherAsksAgainAndThenKeepsThePair(t *testing.T) {
	// A missing field is a correction, and a judge that never answers must fail
	// open. Failing closed would let a broken model delete conflicts.
	c := &answers{replies: []string{`{"why":"không rõ"}`, "not json", `{"why":"vẫn không rõ"}`}}
	a := &Adjudicator{Completer: c, Model: "m", MaxCorrections: 2}
	got, err := a.Together(context.Background(), "người lao động", Scope{Conditions: []string{"x"}}, Scope{})
	if err == nil {
		t.Fatal("three unusable answers were reported as an answer")
	}
	if !got {
		t.Error("a judge that would not answer was allowed to drop the finding")
	}
	if a.Calls != 3 {
		t.Errorf("%d calls, want the first and two corrections", a.Calls)
	}
}

func TestTogetherCarriesAnEndpointFailureUp(t *testing.T) {
	c := &answers{err: errors.New("down")}
	a := &Adjudicator{Completer: c, Model: "m"}
	got, err := a.Together(context.Background(), "", Scope{}, Scope{})
	if err == nil || !got {
		t.Errorf("got %v, %v, want the pair kept and the error reported", got, err)
	}
}

func TestCircumstancesPromptSaysWhenThereIsNoCondition(t *testing.T) {
	// An unconditional norm reaches every case, which the containment test
	// already handles, but the prompt has to be readable if it ever gets there.
	s := CircumstancesPrompt("", Scope{}, Scope{Conditions: []string{"co-du-dieu-kien"}})
	if !strings.Contains(s, "không có điều kiện nào") {
		t.Errorf("the prompt is missing a group:\n%s", s)
	}
}

func TestAdjudicatorInstructionsRefuseTheOtherQuestion(t *testing.T) {
	s := (&Adjudicator{}).Instructions()
	for _, want := range []string{"không nói hai quy định có mâu thuẫn hay không", "Khi không chắc chắn, chọn together = true"} {
		if !strings.Contains(s, want) {
			t.Errorf("the prompt does not say %q", want)
		}
	}
}
