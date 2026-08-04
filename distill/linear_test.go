package distill

import (
	"fmt"
	"testing"
)

// examples is a replayable sequence, because Fit walks it once per epoch.
func examples(rows []struct {
	f    []string
	want bool
}) func(func([]string, bool) bool) {
	return func(yield func([]string, bool) bool) {
		for _, r := range rows {
			if !yield(r.f, r.want) {
				return
			}
		}
	}
}

func TestFitLearnsASeparableSet(t *testing.T) {
	rows := []struct {
		f    []string
		want bool
	}{
		{[]string{"bias", "yes"}, true},
		{[]string{"bias", "no"}, false},
		{[]string{"bias", "yes", "other"}, true},
		{[]string{"bias", "no", "other"}, false},
	}
	w := Fit(10, examples(rows))
	for _, r := range rows {
		if got := Dot(w, r.f) > 0; got != r.want {
			t.Errorf("features %v scored %v, want %v", r.f, Dot(w, r.f), r.want)
		}
	}
	if w["yes"] <= 0 || w["no"] >= 0 {
		t.Errorf("weights = %v", w)
	}
	if _, ok := w["never seen"]; ok {
		t.Error("a feature the training set never had has no weight")
	}
}

func TestFitIsOrderDependentAndRepeatable(t *testing.T) {
	rows := []struct {
		f    []string
		want bool
	}{
		{[]string{"a"}, true},
		{[]string{"b"}, false},
		{[]string{"a", "b"}, true},
	}
	first := Fit(5, examples(rows))
	again := Fit(5, examples(rows))
	if fmt.Sprint(first) != fmt.Sprint(again) {
		t.Fatalf("two runs over one sequence disagree:\n%v\n%v", first, again)
	}
}

func TestDotIgnoresUnknownFeatures(t *testing.T) {
	w := map[string]float64{"a": 1.5}
	if got := Dot(w, []string{"a", "z"}); got != 1.5 {
		t.Errorf("Dot = %v", got)
	}
	if got := Dot(w, nil); got != 0 {
		t.Errorf("Dot of nothing = %v", got)
	}
}
