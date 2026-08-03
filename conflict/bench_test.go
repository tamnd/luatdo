package conflict

import (
	"context"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/norm"
)

// seeds are real shaped forms every mutation can be grown from: a duty with a
// deadline and a sanction, a permission, and a prohibition.
func seeds() []*Form {
	duty := form("a", Obligation)
	duty.Deadline = deadline(15, "kể từ ngày nhận hồ sơ")
	duty.Sanction, duty.Canon.Sanction = "phat-tien-5-trieu", "phạt tiền 5 triệu đồng"

	perm := form("b", Permission)
	perm.ProvisionID = clause2 + ":b"

	ban := form("c", Prohibition)
	ban.ProvisionID = clause2 + ":c"

	return []*Form{duty, perm, ban}
}

func TestBuildLabelsByConstructionAndNotByReading(t *testing.T) {
	cases := Build(seeds(), 0)
	if len(cases) == 0 {
		t.Fatal("no cases were built from three usable seeds")
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.ID] {
			t.Errorf("two cases share the identifier %s", c.ID)
		}
		seen[c.ID] = true
		if c.Conflict != conflicting(c.Mutation) {
			t.Errorf("%s: label %v does not follow from the mutation", c.ID, c.Conflict)
		}
		if c.Conflict && c.Rule == "" {
			t.Errorf("%s: a conflicting case names no rule to expect", c.ID)
		}
		if !c.Conflict && c.Rule != "" {
			t.Errorf("%s: a near miss names the rule %s it must not fire", c.ID, c.Rule)
		}
		if c.A == nil || c.B == nil {
			t.Fatalf("%s: a case is missing a side", c.ID)
		}
		if c.A.ProvisionID == c.B.ProvisionID {
			t.Errorf("%s: both sides are the same provision, which the checker skips by design", c.ID)
		}
		// The generated side says what it is in its identifiers rather than in
		// its sentence. The sentence has to read as a norm, because the baseline
		// is asked about it, and a note about the test suite in the middle of it
		// is both unanswerable and a hint about which side was planted.
		if !strings.HasPrefix(c.B.DocID, "vn:bench:") || !strings.HasPrefix(c.B.StatementID, "vn:bench:") {
			t.Errorf("%s: the generated side is not marked as generated: %s %s", c.ID, c.B.DocID, c.B.StatementID)
		}
	}
	// Every mutation a seed can carry has to appear, or a gate passes because a
	// whole shape of pair was quietly absent.
	for _, mut := range Mutations {
		found := false
		for _, c := range cases {
			if c.Mutation == mut {
				found = true
			}
		}
		if !found {
			t.Errorf("mutation %s produced no case", mut)
		}
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	first := Build(seeds(), 2)
	second := Build(seeds(), 2)
	if len(first) != len(second) {
		t.Fatalf("%d cases then %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("case %d is %s then %s", i, first[i].ID, second[i].ID)
		}
	}
}

func TestBuildCapsEachMutation(t *testing.T) {
	cases := Build(seeds(), 1)
	counts := map[string]int{}
	for _, c := range cases {
		counts[c.Mutation]++
	}
	for mut, n := range counts {
		if n > 1 {
			t.Errorf("%s produced %d cases against a cap of 1", mut, n)
		}
	}
}

func TestBuildIgnoresAFormNothingCanBeComparedAgainst(t *testing.T) {
	// A right has no opposite here, deliberately, so it cannot seed the
	// mutations that need one.
	right := form("d", Right)
	for _, c := range Build([]*Form{right}, 0) {
		if c.Mutation != MutRestated {
			t.Errorf("a right seeded %s, which needs an opposite operator", c.Mutation)
		}
	}
	// A form the checker would never look at cannot seed anything.
	if got := Build([]*Form{form("e", "")}, 0); len(got) != 0 {
		t.Errorf("%d cases from a form with no operator", len(got))
	}
}

func TestGradeCasesScoresTheChecker(t *testing.T) {
	cases := Build(seeds(), 0)
	g := GradeCases(context.Background(), cases, nil, nil)
	if g.Cases != len(cases) {
		t.Fatalf("graded %d of %d cases", g.Cases, len(cases))
	}
	if g.Conflicts+g.Negatives != g.Cases {
		t.Errorf("%d conflicts and %d negatives do not add to %d cases", g.Conflicts, g.Negatives, g.Cases)
	}
	if g.FalseNegatives != 0 {
		t.Errorf("the checker missed %d planted conflicts: %v", g.FalseNegatives, g.Misses)
	}
	if g.WrongRule != 0 {
		t.Errorf("%d conflicts were caught by a rule other than the one planted", g.WrongRule)
	}
	if g.Recall != 1 {
		t.Errorf("recall = %.2f over generated pairs, which are the easy ones", g.Recall)
	}
	// The disjoint conditions mutation is built to fire, so overall precision is
	// below one by construction and the benchmark is not lying about it.
	if k := g.ByMutation[MutCondition]; k == nil || k.Fired == 0 {
		t.Fatalf("the disjoint conditions mutation did not fire, so the two precisions cannot differ")
	}
	if g.Precision >= 1 {
		t.Errorf("precision = %.2f, which hides the case the checker cannot resolve", g.Precision)
	}
	// Every other near miss must be held still, and the misses list has to name
	// the ones that were not.
	for _, mut := range []string{MutParty, MutAct, MutInterval, MutDefers, MutRestated} {
		k := g.ByMutation[mut]
		if k == nil {
			t.Fatalf("%s was not graded", mut)
		}
		if k.Fired != 0 {
			t.Errorf("%s fired %d times and must never fire", mut, k.Fired)
		}
		if k.Correct != k.Cases {
			t.Errorf("%s: %d of %d correct", mut, k.Correct, k.Cases)
		}
	}
	if len(g.Misses) != g.FalsePositives+g.FalseNegatives {
		t.Errorf("%d misses listed against %d wrong cases", len(g.Misses), g.FalsePositives+g.FalseNegatives)
	}
}

func TestGradeSeparatesSharedCircumstancesFromTheRest(t *testing.T) {
	g := GradeCases(context.Background(), Build(seeds(), 0), nil, nil)
	// The containment test is what the two numbers measure the value of: the
	// disjoint conditions pairs are counted in Precision and excluded from
	// SharedPrecision, so the gap is what the test buys.
	if g.SharedPrecision <= g.Precision {
		t.Errorf("shared precision %.2f is not better than overall %.2f, so the containment test buys nothing",
			g.SharedPrecision, g.Precision)
	}
	if g.SharedFindings > g.TruePositives+g.FalsePositives {
		t.Errorf("%d shared findings out of %d findings", g.SharedFindings, g.TruePositives+g.FalsePositives)
	}
}

func TestGradeSaysItsNumbersAreNotAccuracyOnTheCorpus(t *testing.T) {
	s := GradeCases(context.Background(), Build(seeds(), 1), nil, nil).String()
	if !strings.Contains(s, "not real law") {
		t.Errorf("the grade does not disclaim what it measured:\n%s", s)
	}
}

func TestMutatedDeadlinesStayComparable(t *testing.T) {
	// A mutation that produced an unanchored deadline would be graded as a
	// missed conflict when the checker was right to hold still, which would
	// make the benchmark punish correct behaviour.
	duty := form("a", Obligation)
	duty.Deadline = deadline(15, "kể từ ngày nhận hồ sơ")
	c, ok := mutate(duty, MutDeadline, 0)
	if !ok {
		t.Fatal("a duty with a deadline could not carry the deadline mutation")
	}
	da, oka := days(c.A)
	db, okb := days(c.B)
	if !oka || !okb || da == db {
		t.Errorf("deadlines %d (%v) and %d (%v) are not two comparable limits", da, oka, db, okb)
	}
}

func TestMutateRefusesASeedThatCannotCarryTheMutation(t *testing.T) {
	plain := form("a", Obligation)
	for _, mut := range []string{MutDeadline, MutSanction} {
		if _, ok := mutate(plain, mut, 0); ok {
			t.Errorf("%s was applied to a form with nothing to change", mut)
		}
	}
}

func TestNoiseFloorMeasuresTheSameProvisionRate(t *testing.T) {
	// Two readings of one clause. A drafter does not contradict themselves
	// inside one clause, so a rule that fires here has fired on noise, and this
	// is the one false positive anybody can recognise without annotating.
	a := form("a", Obligation)
	b := form("b", Prohibition)
	b.ProvisionID = a.ProvisionID
	n := NoiseFloor([]*Form{a, b}, nil)
	if n.Pairs != 1 {
		t.Fatalf("pairs = %d, want the one pair from inside the provision", n.Pairs)
	}
	if n.Fired != 1 || n.Rate != 1 {
		t.Errorf("fired = %d, rate = %.2f, want the firing counted", n.Fired, n.Rate)
	}
	if len(n.Examples) != 1 {
		t.Errorf("examples = %d, want the firing kept for reading", len(n.Examples))
	}
}

func TestNoiseFloorIgnoresPairsFromDifferentProvisions(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	n := NoiseFloor([]*Form{a, b}, nil)
	if n.Pairs != 0 {
		t.Errorf("pairs = %d, want none, this measurement is about one provision at a time", n.Pairs)
	}
	if !strings.Contains(n.String(), "0 pairs") {
		t.Errorf("the noise report is not readable when there is nothing to report:\n%s", n)
	}
}

func TestNoiseFloorSaysSoWhenNothingFires(t *testing.T) {
	a := form("a", Obligation)
	b := form("b", Obligation)
	b.ProvisionID = a.ProvisionID
	n := NoiseFloor([]*Form{a, b}, nil)
	if n.Fired != 0 {
		t.Fatalf("two agreeing duties in one clause fired %d times", n.Fired)
	}
	if !strings.Contains(n.String(), "the result this measurement wants") {
		t.Errorf("a clean noise floor reads as an absence of data:\n%s", n)
	}
}

func TestCloneDoesNotShareSlicesWithTheSeed(t *testing.T) {
	seed := form("a", Obligation)
	seed.Scope.Conditions = []string{"x"}
	seed.Deadline = deadline(15, "kể từ ngày nhận hồ sơ")
	c := seed.clone()
	c.Scope.Conditions[0] = "y"
	c.Deadline.Value = 99
	if seed.Scope.Conditions[0] != "x" {
		t.Error("mutating a case changed the real statement it was grown from")
	}
	if seed.Deadline.Value != 15 {
		t.Error("the deadline is shared between the seed and its mutation")
	}
	if c.Deadline == nil {
		t.Fatal("clone dropped the deadline")
	}
}

func TestConflictingIsClosed(t *testing.T) {
	want := map[string]bool{MutFlip: true, MutDeadline: true, MutSanction: true, MutOverlap: true}
	for _, mut := range Mutations {
		if conflicting(mut) != want[mut] {
			t.Errorf("%s is labelled %v", mut, conflicting(mut))
		}
	}
	if conflicting("something-nobody-defined") {
		t.Error("an unknown mutation was labelled a conflict")
	}
}

func TestOppositeHasNoAnswerForARight(t *testing.T) {
	if _, _, ok := opposite(Right); ok {
		t.Error("a right was given an opposite, which check.go deliberately does not have")
	}
	for _, op := range []string{Obligation, Permission, Prohibition} {
		other, rule, ok := opposite(op)
		if !ok {
			t.Fatalf("%s has no opposite", op)
		}
		if got, bad := incompatible(op, other); !bad || got != rule {
			t.Errorf("%s against %s is %q, %v, but opposite promised %q", op, other, got, bad, rule)
		}
	}
}

func TestBuildUsesTheRealDeadlineType(t *testing.T) {
	// The mutated deadline has to be a norm.Deadline the checker can read, not
	// a string the benchmark understands and nothing else does.
	duty := form("a", Obligation)
	duty.Deadline = deadline(30, "kể từ ngày nhận hồ sơ")
	c, _ := mutate(duty, MutDeadline, 0)
	if c.B.Deadline.Unit != norm.UnitDay {
		t.Errorf("unit = %q, want %q", c.B.Deadline.Unit, norm.UnitDay)
	}
	if c.B.Deadline.Anchor != duty.Deadline.Anchor {
		t.Errorf("anchor = %q, want the seed's, or the pair is not comparable", c.B.Deadline.Anchor)
	}
}
