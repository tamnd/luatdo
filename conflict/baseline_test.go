package conflict

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/api"
)

func TestBaselineAsksWithTheTwoNormsAndNothingElse(t *testing.T) {
	model := &answers{replies: []string{`{"conflict":true,"rationale":"Một bên buộc, một bên cấm."}`}}
	b := &Baseline{Completer: model, Model: "test", MaxCorrections: 2}
	a, other := pair(Obligation, Prohibition)

	said, why, usage, err := b.Ask(context.Background(), a, other)
	if err != nil {
		t.Fatal(err)
	}
	if !said || why == "" {
		t.Errorf("said = %v, rationale = %q", said, why)
	}
	if usage.TotalTokens != 110 {
		t.Errorf("usage = %+v", usage)
	}
	// A baseline handed the detector's own analysis is not a baseline. It gets
	// the two norms as sentences and none of the machinery that compared them.
	prompt := model.inputs[0]
	for _, leak := range []string{a.Operator, other.Operator, a.Party, a.Act, RuleDuty, "shared"} {
		if strings.Contains(prompt, leak) {
			t.Errorf("the baseline prompt leaks %q from the pipeline:\n%s", leak, prompt)
		}
	}
	// Both norms have to be readable in it, including the modality, or the
	// question has no answer. This is the check that would have caught the
	// placeholder sentence the generated side used to carry.
	for _, want := range []string{"người sử dụng lao động", "phải thông báo", "không được thông báo"} {
		if !strings.Contains(strings.ToLower(prompt), want) {
			t.Errorf("the baseline prompt does not state %q:\n%s", want, prompt)
		}
	}
}

func TestBaselineCorrectsUnusableJSON(t *testing.T) {
	model := &answers{replies: []string{"tôi nghĩ là có", `{"conflict":false,"rationale":"Không."}`}}
	b := &Baseline{Completer: model, Model: "test", MaxCorrections: 2}
	a, other := pair(Obligation, Prohibition)

	said, _, _, err := b.Ask(context.Background(), a, other)
	if err != nil {
		t.Fatal(err)
	}
	if said {
		t.Error("the corrected answer was not the one used")
	}
	if model.calls != 2 {
		t.Errorf("calls = %d", model.calls)
	}
}

func TestBaselineGivesUp(t *testing.T) {
	model := &answers{replies: []string{"không phải JSON"}}
	b := &Baseline{Completer: model, Model: "test", MaxCorrections: 1}
	a, other := pair(Obligation, Prohibition)
	if _, _, _, err := b.Ask(context.Background(), a, other); err == nil {
		t.Fatal("prose was accepted as a verdict")
	}
}

// fixedAsker answers from a map keyed by mutation, so the grading logic can be
// tested without a model and without a network.
type fixedAsker struct {
	says  map[string]bool
	err   error
	calls int
}

func (f *fixedAsker) Ask(_ context.Context, a, _ *Form) (bool, string, api.Usage, error) {
	f.calls++
	if f.err != nil {
		return false, "", api.Usage{InputTokens: 50, TotalTokens: 50}, f.err
	}
	// The generated side carries the mutation in its identifier, which is how a
	// fixed answerer can be right about some shapes and wrong about others.
	return f.says[a.StatementID], "", api.Usage{InputTokens: 50, TotalTokens: 50}, nil
}

func TestGradeBaselineScoresAnAnswererThatSaysYesToEverything(t *testing.T) {
	cases := Build(seeds(), 0)
	yes := map[string]bool{}
	for _, c := range cases {
		yes[c.A.StatementID] = true
	}
	g, err := GradeBaseline(context.Background(), cases, &fixedAsker{says: yes})
	if err != nil {
		t.Fatal(err)
	}
	if g.Cases != len(cases) || g.Calls != len(cases) {
		t.Fatalf("graded %d cases with %d calls, want %d of each", g.Cases, g.Calls, len(cases))
	}
	// An answerer that always says conflict has perfect recall and the precision
	// of the base rate, which is exactly the failure the survey papers reported
	// and the reason both numbers are printed rather than one.
	if g.Recall != 1 {
		t.Errorf("recall = %.2f, want 1 from an answerer that never says no", g.Recall)
	}
	if g.Precision >= 1 {
		t.Logf("precision %.2f", g.Precision)
	} else if g.FalsePositives == 0 {
		t.Error("saying yes to every near miss produced no false positives")
	}
	if g.Usage.TotalTokens != 50*len(cases) {
		t.Errorf("usage = %+v, want every call counted", g.Usage)
	}
}

func TestGradeBaselineScoresAnAnswererThatSaysNoToEverything(t *testing.T) {
	cases := Build(seeds(), 0)
	g, err := GradeBaseline(context.Background(), cases, &fixedAsker{says: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	if g.TruePositives != 0 || g.FalsePositives != 0 {
		t.Errorf("an answerer that never says conflict reported %d findings", g.TruePositives+g.FalsePositives)
	}
	if g.FalseNegatives != g.Conflicts {
		t.Errorf("%d false negatives against %d conflicts", g.FalseNegatives, g.Conflicts)
	}
	if g.Precision != 0 || g.Recall != 0 || g.F1 != 0 {
		t.Errorf("precision %.2f, recall %.2f, f1 %.2f, want zero throughout", g.Precision, g.Recall, g.F1)
	}
}

func TestGradeBaselineCountsSilenceAsAnErrorAndAsNoConflict(t *testing.T) {
	cases := Build(seeds(), 1)
	g, err := GradeBaseline(context.Background(), cases, &fixedAsker{err: errors.New("no answer")})
	if err != nil {
		t.Fatal(err)
	}
	if g.Errors != len(cases) {
		t.Errorf("errors = %d, want %d", g.Errors, len(cases))
	}
	// Silence is not a verdict, and scoring it as one would flatter or punish
	// the baseline for something it did not say.
	if g.TruePositives != 0 || g.FalsePositives != 0 {
		t.Error("an unanswered pair was scored as a verdict")
	}
	if !strings.Contains(g.String(), "no usable answer") {
		t.Errorf("the grade hides how many pairs went unanswered:\n%s", g)
	}
}

func TestGradeBaselineStopsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	asker := &fixedAsker{err: context.Canceled}
	if _, err := GradeBaseline(ctx, Build(seeds(), 2), asker); err == nil {
		t.Fatal("a cancelled run graded every pair anyway")
	}
	if asker.calls != 1 {
		t.Errorf("calls = %d after cancellation, want the run to stop at the first", asker.calls)
	}
}

func TestBaselineGradePrintsEveryMutation(t *testing.T) {
	cases := Build(seeds(), 1)
	g, err := GradeBaseline(context.Background(), cases, &fixedAsker{says: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.String()
	for _, mut := range Mutations {
		if g.ByMutation[mut] != nil && !strings.Contains(s, mut) {
			t.Errorf("the grade does not print %s:\n%s", mut, s)
		}
	}
}
