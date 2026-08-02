package graph

import (
	"strings"
	"testing"
)

func TestCellRendersWhatAPersonReads(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		// Half the columns in these queries are optional matches, and a screen of
		// the word null reads as a failure when it is the ordinary answer.
		{"a missing optional match", nil, ""},
		{"a flag", true, "yes"},
		{"the other flag", false, "no"},
		{"a count", int64(3), "3"},
		{"a collected list", []any{"a", "b"}, "a | b"},
		{"a list with a hole in it", []any{"a", nil}, "a | "},
		{"a collected condition", map[string]any{"text": "khi đến kỳ trả lương", "kind": "temporal"}, "kind=temporal text=khi đến kỳ trả lương"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cell(c.in); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestAnAnswerSaysWhatItWasAskedAndWhatCameBack(t *testing.T) {
	q, _ := QuestionByNumber(12)
	a := Answer{
		Question: q,
		Params:   map[string]any{"days": 5, "limit": 100},
		Columns:  []string{"actor", "working_days"},
		Rows:     [][]string{{"người sử dụng lao động", "3"}},
	}
	out := a.String()
	for _, want := range []string{q.Ask, "days = 5", "actor", "người sử dụng lao động", "1 row\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("the answer does not mention %q:\n%s", want, out)
		}
	}
	// A person reading one row should not have to work out whether the tool means
	// one row or a truncated count.
	if strings.Contains(out, "1 rows") {
		t.Error("the answer says 1 rows")
	}
}

func TestAnEmptyAnswerSaysSoRatherThanPrintingNothing(t *testing.T) {
	q, _ := QuestionByNumber(2)
	out := Answer{Question: q, Params: q.Params}.String()
	if !strings.Contains(out, "no rows") {
		t.Errorf("an empty answer prints as %q, which reads as the tool having failed", out)
	}
}

func TestTheCatalogueNamesEveryQuestionAndItsParameters(t *testing.T) {
	out := Catalogue()
	for _, q := range Questions {
		if !strings.Contains(out, q.Ask) {
			t.Errorf("the catalogue does not list question %d", q.N)
		}
		for name := range q.Params {
			if !strings.Contains(out, name+" = ") {
				t.Errorf("the catalogue does not say question %d takes %s", q.N, name)
			}
		}
	}
}
