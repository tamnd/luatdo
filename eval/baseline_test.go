package eval

import (
	"strings"
	"testing"
)

func TestSearchBaselineCannotStateMostOfTheQuestions(t *testing.T) {
	answerable := 0
	for _, o := range Search.Run() {
		if o.Expressible {
			answerable++
		}
	}
	if answerable > 3 {
		t.Errorf("%d answerable, the argument for this project is that search reaches the citation questions and stops", answerable)
	}
	if answerable == 0 {
		t.Error("questions 1 and 3 are citation queries and search plus a citation table answers them")
	}
}

func TestTheFullSystemCanStateAllOfThem(t *testing.T) {
	for _, o := range Full.Run() {
		if !o.Expressible {
			t.Errorf("question %d needs %s, which the full system claims to have", o.Question, o.MissingLayer)
		}
	}
}

func TestFlatTriplesFailOnTheQuestionsThatTurnOnConditions(t *testing.T) {
	c := Compare(FlatTriples, Full)
	sep := c.Separating(FlatTriples.Name, Full.Name)
	for _, want := range []int{12, 13, 14} {
		found := false
		for _, n := range sep {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("question %d turns on a deadline or a condition and a triple has neither", want)
		}
	}
}

func TestFlatTriplesAnswerTheConceptQuestionsFromConflatedNodes(t *testing.T) {
	// This is the outcome that separates a flat triple store from a system with
	// no concepts at all. It does not fail on question 6, it returns a list,
	// and the list is built from string identity, so two instruments using one
	// phrase for different things are one node in it.
	rows := FlatTriples.Run()
	for _, n := range []int{4, 6, 7} {
		o := rows[n-1]
		if !o.Expressible {
			t.Errorf("question %d: a triple store has nodes and will answer, which is the problem", n)
		}
		if !o.Unsound || o.DegradedBy != LayerConcept {
			t.Errorf("question %d = %+v, an answer from conflated nodes is not the same result as a right one", n, o)
		}
		if o.Verdict() != "unsound" {
			t.Errorf("question %d verdict = %q", n, o.Verdict())
		}
	}
}

func TestTheFullSystemIsSoundEverywhereItAnswers(t *testing.T) {
	for _, o := range Full.Run() {
		if o.Unsound {
			t.Errorf("question %d is answered from a degraded layer, which the full system claims not to have", o.Question)
		}
	}
}

func TestComparisonPrintsEveryQuestionIncludingTheUninformativeOnes(t *testing.T) {
	out := Compare(Baselines...).String()
	for _, want := range []string{"needs concept", "needs norm", "answerable", "unsound", "conflated nodes"} {
		if !strings.Contains(out, want) {
			t.Errorf("comparison is missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "\n") < len(Questions) {
		t.Error("a table that hides the questions where nothing separates is a table arguing for a conclusion")
	}
}

func TestExpressibleNamesTheFirstMissingLayer(t *testing.T) {
	q, err := Ask(12)
	if err != nil {
		t.Fatal(err)
	}
	ok, missing := q.Expressible(LayerStructure, LayerCitation)
	if ok || missing != LayerNorm {
		t.Errorf("expressible = %v missing = %q, a deadline is a norm layer object", ok, missing)
	}
}

func TestAskRefusesANumberThatIsNotAQuestion(t *testing.T) {
	if _, err := Ask(24); err == nil {
		t.Error("there is no question 24 and pretending there is would hide a typo in a command")
	}
}

func TestCoverageGroupsByFamily(t *testing.T) {
	n, byFamily := Coverage(Search.Layers...)
	if n != 2 {
		t.Errorf("expressible = %d", n)
	}
	if byFamily["concept"].Right != 0 {
		t.Error("search reaches none of the concept questions, which is the finding")
	}
	if byFamily["citation"].Of != 3 {
		t.Errorf("by family = %+v", byFamily)
	}
}
