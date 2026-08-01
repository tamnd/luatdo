package concept

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// The baseline is the design this one replaced: parse the definitions article
// with a grammar, take the string before the connective, stop. It is kept and
// run rather than described, because a claim that comprehension adds something
// is worth nothing unless the thing it adds to is standing next to it.
//
// The comparison that matters is not the concept count. A grammar can produce
// a large concept count cheaply. It is competency question 6: which concepts
// does the corpus operate on that nobody ever defined. The grammar only design
// cannot answer that question at all, and not because it answers it badly. The
// concepts in the answer are exactly the ones it never created, so its answer
// is empty and its emptiness looks like a clean result.
//
// If this comparison ever comes out small, the design should change.

// Baseline is the grammar only concept layer, counted.
type Baseline struct {
	// Terms is the number of distinct defined terms the grammar found.
	Terms int `json:"terms"`
	// Documents is how many documents contributed one.
	Documents int `json:"documents"`
	// Labels is the folded set, which is what the comparison joins on.
	Labels map[string]bool `json:"-"`
}

// Layer counts the concept layer this project builds, split by where each node
// came from, because the split is the finding.
type LayerCounts struct {
	Defined   int `json:"defined"`
	Recovered int `json:"recovered"`
	Undefined int `json:"undefined_usage"`
	Total     int `json:"total"`
}

// CountLayer counts term uses by origin.
func CountLayer(terms []TermUse) LayerCounts {
	var c LayerCounts
	for i := range terms {
		switch terms[i].Origin {
		case OriginDefined:
			c.Defined++
		case OriginRecovered:
			c.Recovered++
		case OriginUndefinedUsage:
			c.Undefined++
		}
		c.Total++
	}
	return c
}

// Question6 is the competency question, run against a concept layer.
//
// "Which concepts have no definition anywhere in the corpus but are used in
// more than N provisions." The threshold is a parameter because the interesting
// answer moves with corpus size, and 100 is the number the spec named.
type Question6 struct {
	Threshold int              `json:"threshold"`
	Answers   []Question6Entry `json:"answers"`
	// Considered is how many candidate concepts the layer held at all. An empty
	// answer from a layer that considered nothing and an empty answer from a
	// layer that considered forty thousand are different results, and printing
	// only the answer count would make them look the same.
	Considered int `json:"considered"`
}

// Question6Entry is one concept the corpus operates on and never defines.
type Question6Entry struct {
	LabelVI    string `json:"label_vi"`
	Kind       string `json:"kind"`
	Provisions int    `json:"provisions"`
	Documents  int    `json:"documents"`
	Scopes     int    `json:"scopes"`
}

// AskQuestion6 answers the question from the aggregated discovery output.
func AskQuestion6(aggs []Aggregation, threshold int) Question6 {
	q := Question6{Threshold: threshold, Considered: len(aggs)}
	for i := range aggs {
		a := &aggs[i]
		if a.DefinedSomewhere || a.Sighted <= threshold {
			continue
		}
		q.Answers = append(q.Answers, Question6Entry{
			LabelVI: a.LabelVI, Kind: a.Kind, Provisions: a.Sighted,
			Documents: a.InDocs, Scopes: a.InScopes,
		})
	}
	sort.Slice(q.Answers, func(i, j int) bool {
		if q.Answers[i].Provisions != q.Answers[j].Provisions {
			return q.Answers[i].Provisions > q.Answers[j].Provisions
		}
		return q.Answers[i].LabelVI < q.Answers[j].LabelVI
	})
	return q
}

// AskQuestion6Baseline answers the same question against the grammar only
// layer. It takes the same arguments and returns the same type on purpose, so
// that the comparison is between two answers to one question rather than
// between a result and an argument about why there is no result.
//
// The answer is empty by construction and the code says why rather than
// returning a bare zero: every node in that layer came out of a definitions
// article, so every node in it is defined somewhere, so no node in it can ever
// satisfy the question. The emptiness is structural.
func AskQuestion6Baseline(b Baseline, threshold int) Question6 {
	return Question6{Threshold: threshold, Considered: b.Terms}
}

// Standoff is the two layers side by side. It is not called a comparison
// because that name already belongs to a model reading two definitions, and
// two things called the same thing in one package is how a reader ends up
// believing the model decided something it never saw.
type Standoff struct {
	Baseline  Baseline    `json:"baseline"`
	Layer     LayerCounts `json:"layer"`
	Question6 Question6   `json:"question_6"`
	BaselineQ Question6   `json:"question_6_baseline"`
	// OnlyInLayer is how many concepts this layer holds that the grammar never
	// created. It is the direct measure of what the comprehension pass added.
	OnlyInLayer int `json:"only_in_layer"`
	// OnlyInBaseline is the other direction, and it is not zero. A grammar
	// takes a substring from every clause that matches the formula, including
	// clauses the reader decided define nothing, so it produces terms this
	// layer deliberately does not. Reporting it keeps the comparison honest
	// rather than flattering.
	OnlyInBaseline int `json:"only_in_baseline"`
}

// Compare puts the two layers next to each other.
func Compare(b Baseline, terms []TermUse, aggs []Aggregation, threshold int) Standoff {
	c := Standoff{
		Baseline:  b,
		Layer:     CountLayer(terms),
		Question6: AskQuestion6(aggs, threshold),
		BaselineQ: AskQuestion6Baseline(b, threshold),
	}
	inLayer := map[string]bool{}
	for i := range terms {
		inLayer[law.Slug(terms[i].LabelVI)] = true
	}
	for key := range b.Labels {
		if !inLayer[key] {
			c.OnlyInBaseline++
		}
	}
	for key := range inLayer {
		if !b.Labels[key] {
			c.OnlyInLayer++
		}
	}
	return c
}

func (c Standoff) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "grammar only   %d terms from %d documents\n", c.Baseline.Terms, c.Baseline.Documents)
	fmt.Fprintf(&b, "concept layer  %d term uses: %d defined, %d recovered, %d from usage\n",
		c.Layer.Total, c.Layer.Defined, c.Layer.Recovered, c.Layer.Undefined)
	fmt.Fprintf(&b, "               %d only in the layer, %d only in the grammar\n", c.OnlyInLayer, c.OnlyInBaseline)
	fmt.Fprintf(&b, "question 6     concepts nobody defined, used in more than %d provisions\n", c.Question6.Threshold)
	fmt.Fprintf(&b, "  grammar only %d answers from %d concepts considered, and it cannot ever be more:\n",
		len(c.BaselineQ.Answers), c.BaselineQ.Considered)
	fmt.Fprintf(&b, "               every concept it holds came out of a definitions article\n")
	fmt.Fprintf(&b, "  this layer   %d answers from %d concepts considered\n",
		len(c.Question6.Answers), c.Question6.Considered)
	for i, a := range c.Question6.Answers {
		if i >= 10 {
			fmt.Fprintf(&b, "               and %d more\n", len(c.Question6.Answers)-10)
			break
		}
		fmt.Fprintf(&b, "               %-50s %s  %d provisions, %d documents\n",
			a.LabelVI, a.Kind, a.Provisions, a.Documents)
	}
	return b.String()
}
