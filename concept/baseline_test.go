package concept

import (
	"strings"
	"testing"
)

func TestCountLayerSplitsByOrigin(t *testing.T) {
	// The split is the finding. A total on its own says nothing about whether
	// the comprehension pass added anything.
	terms := []TermUse{
		{Origin: OriginDefined}, {Origin: OriginDefined},
		{Origin: OriginRecovered},
		{Origin: OriginUndefinedUsage}, {Origin: OriginUndefinedUsage}, {Origin: OriginUndefinedUsage},
	}
	c := CountLayer(terms)
	if c.Defined != 2 || c.Recovered != 1 || c.Undefined != 3 || c.Total != 6 {
		t.Errorf("counts are %+v", c)
	}
}

func TestQuestion6ReturnsWhatNobodyDefined(t *testing.T) {
	aggs := []Aggregation{
		{LabelVI: "thời giờ làm việc", Kind: KindAmount, Sighted: 400, InDocs: 90, InScopes: 40},
		{LabelVI: "người lao động", Kind: KindActor, Sighted: 9000, InDocs: 2000, InScopes: 900, DefinedSomewhere: true},
		{LabelVI: "phiếu lý lịch tư pháp", Kind: KindArtifact, Sighted: 20, InDocs: 8, InScopes: 6},
	}
	q := AskQuestion6(aggs, 100)
	if len(q.Answers) != 1 {
		t.Fatalf("want one answer, got %d: %v", len(q.Answers), q.Answers)
	}
	if q.Answers[0].LabelVI != "thời giờ làm việc" {
		t.Errorf("answered %q", q.Answers[0].LabelVI)
	}
	if q.Considered != 3 {
		t.Errorf("considered %d, want 3: an empty answer from a layer that considered nothing is a different result", q.Considered)
	}
}

func TestQuestion6SortsTheMostUsedFirst(t *testing.T) {
	aggs := []Aggregation{
		{LabelVI: "b", Sighted: 200, InDocs: 5},
		{LabelVI: "a", Sighted: 900, InDocs: 5},
	}
	q := AskQuestion6(aggs, 100)
	if len(q.Answers) != 2 || q.Answers[0].LabelVI != "a" {
		t.Fatalf("answers are not in usage order: %v", q.Answers)
	}
}

func TestQuestion6OnTheGrammarOnlyLayerIsEmptyByConstruction(t *testing.T) {
	// Not empty because the grammar answers badly. Every node it holds came out
	// of a definitions article, so every node it holds is defined somewhere, so
	// no node in it can ever satisfy the question.
	b := Baseline{Terms: 23090, Documents: 6196}
	q := AskQuestion6Baseline(b, 100)
	if len(q.Answers) != 0 {
		t.Fatalf("the grammar answered question 6: %v", q.Answers)
	}
	if q.Considered != 23090 {
		t.Errorf("considered %d, want the whole grammar layer: the emptiness has to be visibly structural", q.Considered)
	}
}

func TestCompareCountsBothDirections(t *testing.T) {
	// The other direction is not zero, and reporting it keeps the comparison
	// honest rather than flattering. A grammar takes a substring from every
	// clause that matches the formula, including clauses a reader decided
	// define nothing.
	b := Baseline{
		Terms: 3, Documents: 2,
		Labels: map[string]bool{"nguoi-lao-dong": true, "hop-dong-lao-dong": true, "quy-dinh-sau-day": true},
	}
	terms := []TermUse{
		{LabelVI: "Người lao động", Origin: OriginDefined},
		{LabelVI: "Hợp đồng lao động", Origin: OriginDefined},
		{LabelVI: "thời giờ làm việc", Origin: OriginUndefinedUsage},
	}
	c := Compare(b, terms, nil, 100)
	if c.OnlyInLayer != 1 {
		t.Errorf("only in the layer is %d, want 1", c.OnlyInLayer)
	}
	if c.OnlyInBaseline != 1 {
		t.Errorf("only in the grammar is %d, want 1", c.OnlyInBaseline)
	}
	if c.Layer.Undefined != 1 {
		t.Errorf("the layer's usage concepts were not counted: %+v", c.Layer)
	}
}

func TestStandoffPrintsBothAnswersToTheSameQuestion(t *testing.T) {
	b := Baseline{Terms: 10, Documents: 4, Labels: map[string]bool{"a": true}}
	aggs := []Aggregation{{LabelVI: "thời giờ làm việc", Kind: KindAmount, Sighted: 400, InDocs: 90}}
	out := Compare(b, nil, aggs, 100).String()
	for _, want := range []string{"grammar only", "question 6", "thời giờ làm việc", "definitions article"} {
		if !strings.Contains(out, want) {
			t.Errorf("the standoff does not mention %q:\n%s", want, out)
		}
	}
}
