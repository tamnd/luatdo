package eval

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// statute.json is compiled in rather than read from a path, so a benchmark
// figure in a report cannot have come from a file somebody edited locally.
//
//go:embed statute.json
var statuteJSON []byte

// Question kinds. An unanswerable question has no gold component because the
// corpus does not contain the answer, and the right behaviour is a refusal.
const (
	KindAnswerable   = "answerable"
	KindUnanswerable = "unanswerable"
)

// BenchQuestion is one question with the components that actually answer it.
//
// Gold holds component identifiers rather than an answer string. The thing
// being measured is whether the system reaches the provision a lawyer would
// cite, and a string comparison against a model written sentence measures the
// wording as much as the retrieval.
type BenchQuestion struct {
	N        int      `json:"n"`
	Question string   `json:"question"`
	Family   string   `json:"family"`
	Kind     string   `json:"kind"`
	Gold     []string `json:"gold"`
	AsOf     string   `json:"as_of,omitempty"`
	Note     string   `json:"note"`
}

// Answerable reports whether the corpus is supposed to contain the answer.
func (q BenchQuestion) Answerable() bool { return q.Kind == KindAnswerable }

// Bench is the committed statute benchmark.
type Bench struct {
	Version   int             `json:"version"`
	Corpus    string          `json:"corpus"`
	Questions []BenchQuestion `json:"questions"`
}

// Statute returns the embedded benchmark. It panics on a malformed file because
// the file ships inside the binary, so a parse failure is a build that should
// never have been released rather than a condition a caller can handle.
func Statute() Bench {
	var b Bench
	if err := json.Unmarshal(statuteJSON, &b); err != nil {
		panic("eval: statute.json is malformed: " + err.Error())
	}
	return b
}

// Answerable returns the questions the corpus is supposed to answer.
func (b Bench) Answerable() []BenchQuestion {
	out := make([]BenchQuestion, 0, len(b.Questions))
	for _, q := range b.Questions {
		if q.Answerable() {
			out = append(out, q)
		}
	}
	return out
}

// Families lists the question families in a stable order, so a report can say
// which kind of question a system is bad at rather than only how bad it is.
func (b Bench) Families() []string {
	seen := map[string]bool{}
	var out []string
	for _, q := range b.Questions {
		if !seen[q.Family] {
			seen[q.Family] = true
			out = append(out, q.Family)
		}
	}
	sort.Strings(out)
	return out
}

// Construction is the first of the three scores: whether the graph even holds
// what the question needs, before anybody asks whether retrieval found it.
//
// It is separate because a retrieval miss on a provision that was never parsed
// and a retrieval miss on a provision sitting in the index are two different
// failures with two different repairs, and one recall figure hides which one
// happened.
type Construction struct {
	Questions int           `json:"questions"`
	Gold      int           `json:"gold"`
	Present   Accuracy      `json:"present"`
	Stated    Accuracy      `json:"stated"`
	Per       []Constructed `json:"per"`
}

// Constructed is one question's construction result.
type Constructed struct {
	N       int      `json:"n"`
	Missing []string `json:"missing,omitempty"`
	Silent  []string `json:"silent,omitempty"`
}

// OK reports whether every gold component for this question exists and carries
// a trusted statement, which is the precondition for the answerer being allowed
// to say anything from it at all.
func (c Constructed) OK() bool { return len(c.Missing) == 0 && len(c.Silent) == 0 }

// ScoreConstruction asks the caller two questions per gold component: does a
// component with that identifier exist, and does it carry a trusted statement.
// Both predicates come from the caller because eval does not know how the store
// is laid out and should not learn.
func ScoreConstruction(b Bench, present, stated func(id string) bool) Construction {
	c := Construction{}
	for _, q := range b.Questions {
		if !q.Answerable() {
			continue
		}
		c.Questions++
		one := Constructed{N: q.N}
		for _, id := range q.Gold {
			c.Gold++
			exists := present(id)
			c.Present.Observe(exists)
			if !exists {
				one.Missing = append(one.Missing, id)
				continue
			}
			// A component nobody extracted a statement from is only text, and
			// the claim constrained answerer cannot assert from it.
			said := stated(id)
			c.Stated.Observe(said)
			if !said {
				one.Silent = append(one.Silent, id)
			}
		}
		c.Per = append(c.Per, one)
	}
	return c
}

// Reachable lists the question numbers whose gold is fully constructed. These
// are the only questions where a retrieval failure is retrieval's fault.
func (c Construction) Reachable() []int {
	var out []int
	for _, one := range c.Per {
		if one.OK() {
			out = append(out, one.N)
		}
	}
	return out
}

func (c Construction) String() string {
	t := NewTable("construction", fmt.Sprintf("%d answerable questions, %d gold components", c.Questions, c.Gold))
	t.Rate("gold component exists", c.Present)
	t.Rate("gold component carries a trusted statement", c.Stated)
	t.Note("%d of %d questions have every gold component built", len(c.Reachable()), c.Questions)
	return t.String()
}

// Retrieved is one question's ranked result. Ranked holds component identifiers
// in the order the retriever put them, longest first is not assumed anywhere.
type Retrieved struct {
	N        int      `json:"n"`
	Ranked   []string `json:"ranked"`
	Gold     []string `json:"gold"`
	Built    bool     `json:"built"`
	InScope  int      `json:"in_scope"`
	Answered bool     `json:"answerable"`
}

// Rank is the one based position of the first gold component, or 0 for none.
func (r Retrieved) Rank() int {
	for i, id := range r.Ranked {
		if slices.Contains(r.Gold, id) {
			return i + 1
		}
	}
	return 0
}

// Found counts how many distinct gold components appear anywhere in the ranking.
func (r Retrieved) Found() int {
	got := map[string]bool{}
	for _, id := range r.Ranked {
		got[id] = true
	}
	n := 0
	for _, g := range r.Gold {
		if got[g] {
			n++
		}
	}
	return n
}

// Retrieval is the second score. It says nothing about what an answer said.
type Retrieval struct {
	K     int         `json:"k"`
	Runs  []Retrieved `json:"runs"`
	Gold  Accuracy    `json:"gold"`
	Built Accuracy    `json:"built"`
	Hit   Accuracy    `json:"hit"`
	Full  Accuracy    `json:"full"`
	MRR   float64     `json:"mrr"`
	Ranks []int       `json:"ranks"`

	// Offered is how many distinct components the ranking put in play across
	// every question. It is here because k does not mean the same thing to two
	// retrievers: eight components is eight components, but eight flat chunks
	// straddle far more than eight, and a recall figure that ignores that is
	// rewarding one system for being handed more chances.
	Offered int `json:"offered"`
}

// ScoreRetrieval reports recall twice on purpose. Gold is recall over every
// gold component the benchmark names, which is the number a reader outside the
// project cares about. Built is recall over the gold components construction
// actually produced, which is the number that tells the retrieval code whether
// it has a bug.
func ScoreRetrieval(k int, runs []Retrieved) Retrieval {
	r := Retrieval{K: k, Runs: runs}
	var recip float64
	var asked int
	for _, run := range runs {
		if !run.Answered {
			continue
		}
		asked++
		r.Offered += len(distinct(run.Ranked))
		found := run.Found()
		for i := range run.Gold {
			r.Gold.Observe(i < found)
			if run.Built {
				r.Built.Observe(i < found)
			}
		}
		r.Hit.Observe(found > 0)
		r.Full.Observe(found == len(run.Gold) && len(run.Gold) > 0)
		rank := run.Rank()
		r.Ranks = append(r.Ranks, rank)
		if rank > 0 {
			recip += 1 / float64(rank)
		}
	}
	if asked > 0 {
		r.MRR = recip / float64(asked)
	}
	return r
}

// Misses lists the question numbers where no gold component was retrieved at
// all, since an average hides exactly the questions worth reading.
func (r Retrieval) Misses() []int {
	var out []int
	for _, run := range r.Runs {
		if run.Answered && run.Found() == 0 {
			out = append(out, run.N)
		}
	}
	return out
}

func (r Retrieval) String() string {
	t := NewTable("retrieval", fmt.Sprintf("top %d, %d answerable questions", r.K, r.Hit.Of))
	t.Rate("gold components retrieved", r.Gold)
	t.Rate("gold components retrieved, of those built", r.Built)
	t.Rate("questions with at least one gold component", r.Hit)
	t.Rate("questions with every gold component", r.Full)
	t.Note("mean reciprocal rank %.3f over %d questions", r.MRR, r.Hit.Of)
	t.Note("%d distinct components put in play, %.1f per question at k=%d",
		r.Offered, float64(r.Offered)/float64(max(r.Hit.Of, 1)), r.K)
	if miss := r.Misses(); len(miss) > 0 {
		t.Note("no gold component at any rank for questions %s", numbers(miss))
	}
	return t.String()
}

// Generated is one question's answer, reduced to what can be counted without
// reading Vietnamese prose.
type Generated struct {
	N        int      `json:"n"`
	Answered bool     `json:"answerable"`
	Refused  bool     `json:"refused"`
	Claims   int      `json:"claims"`
	Grounded int      `json:"grounded"`
	OnGold   int      `json:"on_gold"`
	Invented int      `json:"invented"`
	Cited    []string `json:"cited,omitempty"`
}

// Generation is the third score. It never borrows a retrieval number, so a
// system that retrieves well and writes badly cannot hide inside an average.
type Generation struct {
	Runs     []Generated `json:"runs"`
	Claims   int         `json:"claims"`
	Grounded int         `json:"grounded"`
	OnGold   int         `json:"on_gold"`
	Invented int         `json:"invented"`
	Silence  Accuracy    `json:"silence"`
	Spoke    Accuracy    `json:"spoke"`
	Cited    Accuracy    `json:"cited"`
}

// ScoreGeneration folds the per question results. Silence is the one that
// matters most on this corpus: nine instruments cannot answer most of what a
// person would ask, so a system that never refuses is wrong far more often than
// its accuracy on the answerable questions suggests.
func ScoreGeneration(runs []Generated) Generation {
	g := Generation{Runs: runs}
	for _, run := range runs {
		g.Claims += run.Claims
		g.Grounded += run.Grounded
		g.OnGold += run.OnGold
		g.Invented += run.Invented
		if run.Answered {
			g.Spoke.Observe(!run.Refused && run.Grounded > 0)
			if !run.Refused {
				g.Cited.Observe(run.OnGold > 0)
			}
			continue
		}
		g.Silence.Observe(run.Refused)
	}
	return g
}

// Fabricated lists the unanswerable questions the system answered anyway, which
// is the failure this benchmark exists to catch.
func (g Generation) Fabricated() []int {
	var out []int
	for _, run := range g.Runs {
		if !run.Answered && !run.Refused {
			out = append(out, run.N)
		}
	}
	return out
}

func (g Generation) String() string {
	t := NewTable("generation", fmt.Sprintf("%d answerable, %d unanswerable questions", g.Spoke.Of, g.Silence.Of))
	t.Rate("answerable questions with a grounded claim", g.Spoke)
	t.Rate("answered questions citing a gold component", g.Cited)
	t.Rate("unanswerable questions refused", g.Silence)
	t.Note("%d claims made, %d survived the grounding check, %d cited a gold component",
		g.Claims, g.Grounded, g.OnGold)
	t.Note("%d claims cited a component or a statement that does not exist", g.Invented)
	if made := g.Fabricated(); len(made) > 0 {
		t.Note("answered rather than refused on questions %s, which the corpus does not contain", numbers(made))
	}
	return t.String()
}

// Report renders the three scores one after another.
//
// There is no combined figure and there is no place to put one. Construction,
// retrieval and generation fail for different reasons and are fixed by
// different code, and a single headline would let a good construction number
// pay for a bad generation number.
func Report(system string, c Construction, r Retrieval, g Generation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "statute benchmark, %s\n\n", system)
	b.WriteString(c.String())
	b.WriteString("\n")
	b.WriteString(r.String())
	b.WriteString("\n")
	b.WriteString(g.String())
	return b.String()
}

func distinct(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func numbers(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ", ")
}
