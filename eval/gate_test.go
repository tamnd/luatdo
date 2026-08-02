package eval

import (
	"strings"
	"testing"
)

func gate(t *testing.T, name string) Gate {
	t.Helper()
	g, err := Named(name)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestAGateWithNothingToMeasureBlocksRatherThanPasses(t *testing.T) {
	g := gate(t, "statement-f1")
	r := g.Check(0, 0)
	if r.Passed || !r.Skipped {
		t.Fatalf("result = %+v", r)
	}
	ok, reasons := Verdict{Results: []Result{r}}.Ship()
	if ok {
		t.Error("the cheapest way past a quality gate is to produce nothing for it to measure")
	}
	if !strings.Contains(reasons[0], "cannot pass") {
		t.Errorf("reasons = %v", reasons)
	}
}

func TestAFailingGateSaysWhatItProtects(t *testing.T) {
	g := gate(t, "bearer-placement")
	v := Verdict{Results: []Result{g.Check(10, 100)}}
	ok, reasons := v.Ship()
	if ok {
		t.Fatal("one bearer in ten placed is not a shippable campaign")
	}
	if !strings.Contains(reasons[0], "shadow ontology") {
		t.Errorf("a threshold with no stated reason gets lowered by the person it stops: %v", reasons)
	}
}

func TestGatesPassWhenTheNumbersAreThere(t *testing.T) {
	v := Verdict{Results: []Result{
		gate(t, "bearer-placement").Check(95, 100),
		gate(t, "statement-f1").Check(70, 100),
		gate(t, "evidence").Check(400, 400),
	}}
	if ok, reasons := v.Ship(); !ok {
		t.Errorf("blocked for %v", reasons)
	}
	if !strings.Contains(v.String(), "may be exported") {
		t.Errorf("verdict = %s", v)
	}
}

func TestEvidenceGateAdmitsNoExceptions(t *testing.T) {
	g := gate(t, "evidence")
	if g.Min != 1.0 {
		t.Error("a graph where most claims can be checked is a graph where an unchecked claim is invisible")
	}
	if r := g.Check(399, 400); r.Passed {
		t.Error("one unverifiable quote in four hundred is one claim nobody can check")
	}
}

func TestJudgeAgreementGateNeedsARealSample(t *testing.T) {
	g := gate(t, "judge-agreement")
	if r := g.CheckValue(0.9, 10); !r.Skipped {
		t.Error("a kappa from ten items is a number with an interval most of a unit wide")
	}
	if r := g.CheckValue(0.55, 60); !r.Passed {
		t.Errorf("result = %+v, moderate agreement over sixty items clears the floor", r)
	}
}

func TestVerdictListsEveryGateIncludingThePasses(t *testing.T) {
	v := Verdict{Results: []Result{
		gate(t, "evidence").Check(400, 400),
		gate(t, "statement-f1").Check(10, 100),
	}}
	out := v.String()
	if !strings.Contains(out, "evidence") || !strings.Contains(out, "FAIL") {
		t.Errorf("verdict = %s", out)
	}
	if !strings.Contains(out, "400 of 400") {
		t.Error("a gate result carries the sample it was computed from like every other number here")
	}
}

func TestNamedRefusesAGateThatDoesNotExist(t *testing.T) {
	if _, err := Named("vibes"); err == nil {
		t.Error("a typo in a gate name would otherwise disable the gate silently")
	}
}
