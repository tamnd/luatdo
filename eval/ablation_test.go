package eval

import (
	"strings"
	"testing"
)

func TestEveryAblationCostsSomething(t *testing.T) {
	for _, a := range Ablations {
		if len(a.Affected()) == 0 {
			t.Errorf("%s costs nothing, which is an argument for deleting the layer rather than for keeping it", a.Name)
		}
	}
}

func TestRemovingConceptsBreaksMoreThanTheConceptQuestions(t *testing.T) {
	var a Ablation
	for _, x := range Ablations {
		if x.Name == "no-concepts" {
			a = x
		}
	}
	lost := a.Affected()
	has := func(n int) bool {
		for _, l := range lost {
			if l == n {
				return true
			}
		}
		return false
	}
	if !has(13) {
		t.Error("question 13 matches a prohibition to a sanction through the concept layer, so it goes with it")
	}
	if has(10) {
		t.Error("question 10 asks which duties have no bearer at all, and that needs no concept to answer")
	}
}

func TestDroppingConditionsMakesAnswersWrongRatherThanMissing(t *testing.T) {
	var a Ablation
	for _, x := range Ablations {
		if x.Name == "no-conditions" {
			a = x
		}
	}
	lost := a.Affected()
	if len(lost) == 0 {
		t.Fatal("the norm layer is still there, so nothing derives from the layer table, and the cost is named instead")
	}
	found := false
	for _, n := range lost {
		if n == 19 {
			found = true
		}
	}
	if !found {
		t.Error("two provisions look incompatible until you notice they hold under different conditions")
	}
}

func TestAblationReportSaysWhatEachRemovalSimulates(t *testing.T) {
	out := AblationReport()
	for _, want := range []string{"no-concepts", "no-conditions", "no-temporal", "shadow ontology"} {
		if !strings.Contains(out, want) {
			t.Errorf("report is missing %q:\n%s", want, out)
		}
	}
}
