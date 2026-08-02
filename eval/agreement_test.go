package eval

import (
	"strings"
	"testing"
)

// agree builds an agreement from paired labels, human first.
func agree(pairs ...[2]string) *Agreement {
	a := NewAgreement("entailed", "not_entailed")
	for _, p := range pairs {
		a.Observe(p[0], p[1])
	}
	return a
}

func TestKappaSeesThroughAJudgeThatAlwaysSaysYes(t *testing.T) {
	// Nine of ten items really are entailed and the judge says entailed to
	// everything. Raw agreement is 0.9 and the judge has measured nothing.
	var pairs [][2]string
	for i := 0; i < 9; i++ {
		pairs = append(pairs, [2]string{"entailed", "entailed"})
	}
	pairs = append(pairs, [2]string{"not_entailed", "entailed"})
	a := agree(pairs...)
	if a.Raw() != 0.9 {
		t.Fatalf("raw = %v", a.Raw())
	}
	if a.Kappa() > 0.01 {
		t.Errorf("kappa = %.3f, a judge that answers before reading is worth nothing above chance", a.Kappa())
	}
	if a.Reading() != "slight" {
		t.Errorf("reading = %q", a.Reading())
	}
}

func TestKappaIsHighWhenTheJudgeTracksTheHuman(t *testing.T) {
	a := agree(
		[2]string{"entailed", "entailed"}, [2]string{"entailed", "entailed"},
		[2]string{"entailed", "entailed"}, [2]string{"entailed", "entailed"},
		[2]string{"not_entailed", "not_entailed"}, [2]string{"not_entailed", "not_entailed"},
		[2]string{"not_entailed", "not_entailed"}, [2]string{"not_entailed", "entailed"},
	)
	if a.Kappa() < 0.6 {
		t.Errorf("kappa = %.3f, seven of eight with a balanced set is substantial", a.Kappa())
	}
}

func TestKappaIsNegativeWhenTheJudgeDisagreesMoreThanChance(t *testing.T) {
	a := agree(
		[2]string{"entailed", "not_entailed"}, [2]string{"entailed", "not_entailed"},
		[2]string{"not_entailed", "entailed"}, [2]string{"not_entailed", "entailed"},
	)
	if a.Kappa() >= 0 {
		t.Errorf("kappa = %.3f, a judge that inverts the human is not zero", a.Kappa())
	}
	if a.Reading() != "worse than chance" {
		t.Errorf("reading = %q", a.Reading())
	}
}

func TestAgreementNamesTheShapeOfTheDisagreement(t *testing.T) {
	a := agree(
		[2]string{"not_entailed", "entailed"}, [2]string{"not_entailed", "entailed"},
		[2]string{"entailed", "not_entailed"}, [2]string{"entailed", "entailed"},
	)
	cells := a.Disagreements()
	if len(cells) != 2 || cells[0].Human != "not_entailed" || cells[0].N != 2 {
		t.Fatalf("disagreements = %+v, the commonest cell comes first", cells)
	}
	if !strings.Contains(a.String(), "human said not_entailed, judge said entailed: 2") {
		t.Errorf("the direction of the error is the finding:\n%s", a)
	}
}

func TestAgreementOnOneLabelOnlyMeasuresNothing(t *testing.T) {
	a := agree([2]string{"entailed", "entailed"}, [2]string{"entailed", "entailed"})
	if a.Kappa() != 0 {
		t.Errorf("kappa = %.3f, two labellers who only ever say one word have not been compared", a.Kappa())
	}
}

func TestThinSampleIsFlagged(t *testing.T) {
	if !agree([2]string{"entailed", "entailed"}).Thin() {
		t.Error("a kappa from one item is a number nobody should act on")
	}
	if NewAgreement().Thin() {
		t.Error("an empty comparison is empty rather than thin")
	}
}

func TestEmptyAgreementSaysSoRatherThanReportingZero(t *testing.T) {
	if !strings.Contains(NewAgreement().String(), "nothing to compare") {
		t.Error("zero agreement and no comparison are opposite results")
	}
}

func TestSkewedSetIsNamedRatherThanLeftToTheReader(t *testing.T) {
	pairs := make([][2]string, 0, 50)
	for i := 0; i < 49; i++ {
		pairs = append(pairs, [2]string{"entailed", "entailed"})
	}
	pairs = append(pairs, [2]string{"not_entailed", "entailed"})
	a := agree(pairs...)
	if !a.Skewed() {
		t.Fatalf("skew = %.3f, a set with one disagreement in fifty has almost nothing to measure", a.Skew())
	}
	if a.Raw() < 0.9 || a.Kappa() > 0.1 {
		t.Errorf("raw %.3f and kappa %.3f, this is the pair of numbers the note exists for", a.Raw(), a.Kappa())
	}
	if !strings.Contains(a.String(), "neither number stands alone here") {
		t.Errorf("the paradox has to be in the output, not in a reader's memory:\n%s", a)
	}
}

func TestBalancedSetGetsNoSkewNote(t *testing.T) {
	a := agree(
		[2]string{"entailed", "entailed"}, [2]string{"entailed", "entailed"},
		[2]string{"not_entailed", "not_entailed"}, [2]string{"not_entailed", "entailed"},
	)
	if a.Skewed() {
		t.Errorf("skew = %.3f, half and half is the case kappa was built for", a.Skew())
	}
	if strings.Contains(a.String(), "stands alone") {
		t.Errorf("the note belongs only where it is true:\n%s", a)
	}
}
