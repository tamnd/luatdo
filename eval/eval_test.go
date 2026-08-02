package eval

import (
	"math"
	"strings"
	"testing"
)

func TestCountKeepsItsDenominator(t *testing.T) {
	small := Count{TP: 9, FP: 1}
	large := Count{TP: 1800, FP: 200}
	if small.Precision() != large.Precision() {
		t.Fatal("the two rates are the same number, which is the point")
	}
	if small.String() == large.String() {
		t.Error("ninety percent of ten and ninety percent of two thousand are different claims")
	}
	if !strings.Contains(small.String(), "9 of 10") {
		t.Errorf("count = %q", small.String())
	}
}

func TestCountFindingNothingIsZeroRatherThanUndefined(t *testing.T) {
	var c Count
	c.Observe(true, false)
	if c.FN != 1 || c.F1() != 0 {
		t.Errorf("count = %+v f1 = %v, a pass that found nothing scores zero", c, c.F1())
	}
	if !strings.Contains(Count{}.String(), "n/a (0 cases)") {
		t.Errorf("an empty table reports that it is empty: %q", Count{}.String())
	}
}

func TestObserveSortsTheFourCases(t *testing.T) {
	var c Count
	c.Observe(true, true)
	c.Observe(false, true)
	c.Observe(true, false)
	c.Observe(false, false) // agreed there is nothing here, and that is not a case
	if c.TP != 1 || c.FP != 1 || c.FN != 1 || c.Decided() != 3 {
		t.Errorf("count = %+v, silence agreed on by both is not evidence of anything", c)
	}
}

func TestIntervalWidensAsTheSampleShrinks(t *testing.T) {
	nlo, nhi := Interval(90, 100)
	blo, bhi := Interval(900, 1000)
	if (nhi - nlo) <= (bhi - blo) {
		t.Error("a hundred cases know less than a thousand and the interval has to say so")
	}
	if nlo > 0.9 || nhi < 0.9 {
		t.Errorf("interval = %.3f..%.3f, it has to contain the estimate", nlo, nhi)
	}
	if lo, hi := Interval(0, 0); lo != 0 || hi != 0 {
		t.Error("no cases is no interval rather than a panic")
	}
}

func TestSeparatesRefusesToCallASmallDifferenceAnImprovement(t *testing.T) {
	if Separates(88, 100, 91, 100) {
		t.Error("0.88 to 0.91 on a hundred clauses is a claim the sample cannot support")
	}
	if !Separates(500, 1000, 900, 1000) {
		t.Error("half to nine tenths on a thousand is a real difference")
	}
}

func TestAccuracyCountsOnlyWhereBothHadAnOpinion(t *testing.T) {
	var a Accuracy
	a.Observe(true)
	a.Observe(false)
	if a.Of != 2 || a.Rate() != 0.5 {
		t.Errorf("accuracy = %+v", a)
	}
	if !strings.Contains(a.String(), "1 of 2") {
		t.Errorf("accuracy = %q", a.String())
	}
}

func TestTableRendersInAStableOrder(t *testing.T) {
	tab := NewTable("norms", "labour-2025, 400 provisions")
	tab.Count("statements", Count{TP: 3, FP: 1})
	tab.Count("bearers", Count{TP: 2})
	tab.Rate("duoc", Accuracy{Right: 4, Of: 5})
	tab.Note("%d records were near duplicates", 20)
	out := tab.String()
	if strings.Index(out, "bearers") > strings.Index(out, "statements") {
		t.Error("names are sorted so two runs of the suite diff cleanly")
	}
	for _, want := range []string{"labour-2025, 400 provisions", "4 of 5", "note: 20 records"} {
		if !strings.Contains(out, want) {
			t.Errorf("table is missing %q:\n%s", want, out)
		}
	}
}

func TestRatioDoesNotDivideByZero(t *testing.T) {
	if math.IsNaN(Ratio(1, 0)) || Ratio(1, 0) != 0 {
		t.Error("a metric over a layer that has not run is a report, not a crash")
	}
}
