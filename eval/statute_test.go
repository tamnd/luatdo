package eval

import (
	"strings"
	"testing"
)

func TestTheCommittedBenchmarkIsWellFormed(t *testing.T) {
	b := Statute()
	if len(b.Questions) < 20 {
		t.Fatalf("%d questions, want a set large enough for a rate to mean anything", len(b.Questions))
	}
	seen := map[int]bool{}
	answerable, unanswerable := 0, 0
	for _, q := range b.Questions {
		if seen[q.N] {
			t.Errorf("question number %d appears twice", q.N)
		}
		seen[q.N] = true
		if strings.TrimSpace(q.Question) == "" || strings.TrimSpace(q.Note) == "" {
			t.Errorf("question %d has no text or no note saying what the answer is", q.N)
		}
		switch q.Kind {
		case KindAnswerable:
			answerable++
			if len(q.Gold) == 0 {
				t.Errorf("question %d is answerable with no gold component, so nothing can be scored", q.N)
			}
			for _, id := range q.Gold {
				if !strings.HasPrefix(id, "vn:law:") {
					t.Errorf("question %d cites %q, which is not a component identifier", q.N, id)
				}
			}
		case KindUnanswerable:
			unanswerable++
			if len(q.Gold) != 0 {
				t.Errorf("question %d is unanswerable but names gold components %v", q.N, q.Gold)
			}
		default:
			t.Errorf("question %d has kind %q", q.N, q.Kind)
		}
	}
	if unanswerable < 3 {
		t.Errorf("%d unanswerable questions, want enough to catch a system that never refuses", unanswerable)
	}
	if len(b.Answerable()) != answerable {
		t.Errorf("Answerable returned %d of %d", len(b.Answerable()), answerable)
	}
}

// The point in time pair is the reason the benchmark carries dates at all: the
// same words, two answers, and only the date separates them.
func TestTheBenchmarkAsksOneQuestionAtTwoDates(t *testing.T) {
	byText := map[string][]BenchQuestion{}
	for _, q := range Statute().Questions {
		byText[q.Question] = append(byText[q.Question], q)
	}
	pairs := 0
	for text, qs := range byText {
		if len(qs) < 2 {
			continue
		}
		pairs++
		if qs[0].AsOf == "" || qs[1].AsOf == "" || qs[0].AsOf == qs[1].AsOf {
			t.Errorf("%q is asked twice without two different dates", text)
		}
		if sameSet(qs[0].Gold, qs[1].Gold) {
			t.Errorf("%q has the same gold at both dates, so it does not test the date filter", text)
		}
	}
	if pairs == 0 {
		t.Error("no question is asked at two dates, so point in time answering is not measured")
	}
}

func TestConstructionSeparatesAMissingComponentFromASilentOne(t *testing.T) {
	b := Bench{Questions: []BenchQuestion{
		{N: 1, Kind: KindAnswerable, Gold: []string{"a", "b"}},
		{N: 2, Kind: KindAnswerable, Gold: []string{"c"}},
		{N: 3, Kind: KindUnanswerable},
	}}
	present := func(id string) bool { return id != "c" }
	stated := func(id string) bool { return id == "a" }
	c := ScoreConstruction(b, present, stated)

	if c.Questions != 2 || c.Gold != 3 {
		t.Fatalf("scored %d questions over %d gold components", c.Questions, c.Gold)
	}
	if c.Present.Right != 2 || c.Present.Of != 3 {
		t.Errorf("presence is %s", c.Present)
	}
	// The missing component is not counted as a silent one, because it was never
	// given the chance to carry a statement.
	if c.Stated.Of != 2 {
		t.Errorf("statement rate was computed over %d components, want only the ones that exist", c.Stated.Of)
	}
	if got := c.Per[0]; len(got.Silent) != 1 || got.Silent[0] != "b" || len(got.Missing) != 0 {
		t.Errorf("question 1 is %+v", got)
	}
	if got := c.Per[1]; len(got.Missing) != 1 || got.Missing[0] != "c" {
		t.Errorf("question 2 is %+v", got)
	}
	if r := c.Reachable(); len(r) != 0 {
		t.Errorf("reachable is %v, want neither question", r)
	}
}

func TestRetrievalReportsRecallTwiceSoAConstructionGapIsVisible(t *testing.T) {
	runs := []Retrieved{
		{N: 1, Answered: true, Built: true, Gold: []string{"a", "b"}, Ranked: []string{"x", "a", "y"}},
		{N: 2, Answered: true, Built: false, Gold: []string{"c"}, Ranked: []string{"x", "y"}},
		{N: 3, Answered: true, Built: true, Gold: []string{"d"}, Ranked: []string{"d"}},
		{N: 4, Answered: false, Gold: nil, Ranked: []string{"x"}},
	}
	r := ScoreRetrieval(5, runs)

	if r.Gold.Right != 2 || r.Gold.Of != 4 {
		t.Errorf("recall over all gold is %s, want 2 of 4", r.Gold)
	}
	// Question 2's gold was never built, so counting it against retrieval would
	// bill retrieval for an extraction failure.
	if r.Built.Right != 2 || r.Built.Of != 3 {
		t.Errorf("recall over built gold is %s, want 2 of 3", r.Built)
	}
	if r.Hit.Right != 2 || r.Hit.Of != 3 {
		t.Errorf("hit rate is %s", r.Hit)
	}
	if r.Full.Right != 1 {
		t.Errorf("full recall is %s, want only the question whose single gold came back", r.Full)
	}
	want := (1.0/2 + 0 + 1.0) / 3
	if diff := r.MRR - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("mrr is %.4f, want %.4f", r.MRR, want)
	}
	if miss := r.Misses(); len(miss) != 1 || miss[0] != 2 {
		t.Errorf("misses are %v, want the question nothing was found for", miss)
	}
}

// A flat chunk covers many components, so at the same k it puts far more of
// them in play. Counting that is the difference between comparing two systems
// and comparing one system against another that was handed more chances.
func TestRetrievalCountsHowManyComponentsWerePutInPlay(t *testing.T) {
	graph := ScoreRetrieval(2, []Retrieved{{N: 1, Answered: true, Gold: []string{"a"}, Ranked: []string{"a", "b"}}})
	flat := ScoreRetrieval(2, []Retrieved{{N: 1, Answered: true, Gold: []string{"a"},
		Ranked: []string{"x", "y", "a", "b", "c", "x"}}})
	if graph.Offered != 2 {
		t.Errorf("graph offered %d", graph.Offered)
	}
	if flat.Offered != 5 {
		t.Errorf("flat offered %d, want the repeat counted once", flat.Offered)
	}
	if !contains(flat.String(), "put in play") {
		t.Errorf("the report does not say how many were offered:\n%s", flat)
	}
}

func TestAnUnanswerableQuestionIsNotCountedAsARetrievalMiss(t *testing.T) {
	r := ScoreRetrieval(5, []Retrieved{{N: 1, Answered: false, Ranked: []string{"x", "y"}}})
	if r.Hit.Of != 0 || r.Gold.Of != 0 {
		t.Errorf("a question with no right answer entered the retrieval denominator: %s, %s", r.Hit, r.Gold)
	}
	if len(r.Misses()) != 0 {
		t.Error("a question with no gold was reported as a miss")
	}
}

func TestGenerationCountsRefusalOnTheQuestionsThatDeserveIt(t *testing.T) {
	g := ScoreGeneration([]Generated{
		{N: 1, Answered: true, Claims: 2, Grounded: 2, OnGold: 1},
		{N: 2, Answered: true, Refused: true},
		{N: 3, Answered: true, Claims: 1, Grounded: 0, Invented: 1},
		{N: 4, Answered: false, Refused: true},
		{N: 5, Answered: false, Claims: 1, Grounded: 1},
	})
	if g.Spoke.Right != 1 || g.Spoke.Of != 3 {
		t.Errorf("grounded answer rate is %s", g.Spoke)
	}
	if g.Silence.Right != 1 || g.Silence.Of != 2 {
		t.Errorf("refusal rate is %s", g.Silence)
	}
	// Question 2 refused, so it is not held against citation accuracy: a system
	// that says nothing has not cited the wrong thing.
	if g.Cited.Of != 2 || g.Cited.Right != 1 {
		t.Errorf("citation rate is %s", g.Cited)
	}
	if g.Claims != 4 || g.Grounded != 3 || g.Invented != 1 {
		t.Errorf("totals are %d claims, %d grounded, %d invented", g.Claims, g.Grounded, g.Invented)
	}
	if made := g.Fabricated(); len(made) != 1 || made[0] != 5 {
		t.Errorf("fabricated is %v, want the unanswerable question that was answered anyway", made)
	}
}

func TestTheReportKeepsTheThreeScoresApart(t *testing.T) {
	out := Report("graph retrieval",
		ScoreConstruction(Bench{Questions: []BenchQuestion{{N: 1, Kind: KindAnswerable, Gold: []string{"a"}}}},
			func(string) bool { return true }, func(string) bool { return true }),
		ScoreRetrieval(5, []Retrieved{{N: 1, Answered: true, Built: true, Gold: []string{"a"}, Ranked: []string{"a"}}}),
		ScoreGeneration([]Generated{{N: 1, Answered: true, Claims: 1, Grounded: 1, OnGold: 1}}))

	for _, want := range []string{"construction over", "retrieval over", "generation over"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report has no %q section:\n%s", want, out)
		}
	}
	for _, banned := range []string{"overall", "score:", "total score"} {
		if strings.Contains(strings.ToLower(out), banned) {
			t.Errorf("the report combined the three scores into %q", banned)
		}
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	in := map[string]bool{}
	for _, s := range a {
		in[s] = true
	}
	for _, s := range b {
		if !in[s] {
			return false
		}
	}
	return true
}
