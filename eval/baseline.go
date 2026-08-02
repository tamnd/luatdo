package eval

import (
	"fmt"
	"sort"
	"strings"
)

// A baseline is a system this project is claiming to beat, run over the same
// corpus and asked the same 23 questions.
//
// The claim being tested is stronger and narrower than "our numbers are
// higher". It is that questions 4 through 23 are not expressible against a
// search index at all, and that a system reporting a score on them is scoring
// something other than the question. So a baseline reports three outcomes per
// question and never a single number: answered, answered wrongly, or not
// expressible with the reason.
//
// A baseline that returns text for a question about structure is the failure
// mode worth naming. Ask a search index for "deadline shorter than five working
// days" and it returns provisions containing those words, ranked. That output
// looks like an answer, can be pasted into a paper, and does not contain the
// set the question asked for, because the question asks for deadlines under a
// threshold and the index has no deadlines in it, only sentences mentioning
// some.
type Baseline struct {
	Name   string   `json:"name"`
	Layers []string `json:"layers"`
	// Degraded lists the layers this baseline has in name only. A flat triple
	// store has nodes for concepts, so every question about concepts can be
	// stated against it, and the answers are wrong in one specific way: the
	// nodes are strings, so two instruments using one phrase for different
	// things collapse into one node and the collapse is invisible.
	//
	// This is the outcome worth separating from the other two. A system that
	// cannot state a question fails loudly. A system that states it and answers
	// confidently from a conflated graph is the one that gets published.
	Degraded []string `json:"degraded,omitempty"`
	// Note is what this baseline is standing in for in the literature.
	Note string `json:"note"`
}

// Search plus a citation table is the honest strong baseline: everything the
// document layer of this project already produces, with no LLM extraction on
// top. It is what most published Vietnamese legal question answering is built
// over, and what the 85.7 percent figure in the existing work is measuring.
var Search = Baseline{
	Name:   "search+citations",
	Layers: []string{LayerStructure, LayerCitation},
	Note:   "full text retrieval over provisions plus the resolved citation and amendment table",
}

// Flat triples is the other baseline, and the more interesting one: an LLM is
// used, so fluency and extraction quality are held constant, and the only
// difference is the shape of what comes out. Subject, predicate, object with no
// conditions, no exceptions, no deadlines, no modality, no bearer flag.
//
// It scores identically to the full system on any question that only needs to
// know two things are related, and cannot express any question that turns on
// when, unless, or by whom, which is most of them.
var FlatTriples = Baseline{
	Name:     "flat-triples",
	Layers:   []string{LayerStructure, LayerCitation, LayerConcept, LayerRelation},
	Degraded: []string{LayerConcept},
	Note:     "LLM extraction into subject predicate object, the shape most knowledge graph papers produce",
}

// Full is this project with every layer built.
var Full = Baseline{
	Name:   "luatdo",
	Layers: []string{LayerStructure, LayerCitation, LayerConcept, LayerRelation, LayerNorm, LayerTemporal},
	Note:   "concepts with identity, structured norms, and validity intervals",
}

// Baselines is what the comparison command runs, weakest first.
var Baselines = []Baseline{Search, FlatTriples, Full}

// Outcome is what one system did with one question.
type Outcome struct {
	Question     int    `json:"question"`
	Expressible  bool   `json:"expressible"`
	MissingLayer string `json:"missing_layer,omitempty"`
	// Unsound marks a question the baseline will answer, from a layer it has
	// only as strings. The answer comes back looking like every other answer.
	Unsound    bool   `json:"unsound,omitempty"`
	DegradedBy string `json:"degraded_by,omitempty"`
}

// Verdict is the one word for an outcome, and there are three of them.
func (o Outcome) Verdict() string {
	switch {
	case !o.Expressible:
		return "needs " + o.MissingLayer
	case o.Unsound:
		return "unsound"
	default:
		return "answerable"
	}
}

// Run asks a baseline all 23 questions.
func (b Baseline) Run() []Outcome {
	out := make([]Outcome, 0, len(Questions))
	for _, q := range Questions {
		ok, missing := q.Expressible(b.Layers...)
		o := Outcome{Question: q.N, Expressible: ok, MissingLayer: missing}
		if ok {
			for _, need := range q.Needs {
				for _, d := range b.Degraded {
					if need == d {
						o.Unsound, o.DegradedBy = true, d
					}
				}
			}
		}
		out = append(out, o)
	}
	return out
}

// Comparison is the table the milestone argument rests on.
type Comparison struct {
	Rows map[string][]Outcome `json:"rows"`
}

// Compare runs every baseline.
func Compare(baselines ...Baseline) Comparison {
	c := Comparison{Rows: map[string][]Outcome{}}
	for _, b := range baselines {
		c.Rows[b.Name] = b.Run()
	}
	return c
}

// String renders the comparison as one line per question, so a reader can see
// which questions separate the systems rather than only that some number went
// up. The questions where every system agrees are the ones this project is not
// justified by, and they are printed too.
func (c Comparison) String() string {
	names := make([]string, 0, len(c.Rows))
	for n := range c.Rows {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("question  ")
	for _, n := range names {
		fmt.Fprintf(&b, "%-18s", n)
	}
	b.WriteString("\n")
	for i, q := range Questions {
		fmt.Fprintf(&b, "%-10d", q.N)
		for _, n := range names {
			fmt.Fprintf(&b, "%-18s", c.Rows[n][i].Verdict())
		}
		fmt.Fprintf(&b, "  %s\n", short(q.Text))
	}
	b.WriteString("\n")
	for _, n := range names {
		sound, unsound := 0, 0
		for _, o := range c.Rows[n] {
			switch {
			case o.Unsound:
				unsound++
			case o.Expressible:
				sound++
			}
		}
		fmt.Fprintf(&b, "%-18s %s answered from a layer that means what it says",
			n, rate(Ratio(sound, len(Questions)), sound, len(Questions)))
		if unsound > 0 {
			fmt.Fprintf(&b, ", and %d more answered from conflated nodes", unsound)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Separating returns the question numbers where two systems differ, which is
// the only part of the table that is evidence for anything.
func (c Comparison) Separating(a, b string) []int {
	ra, rb := c.Rows[a], c.Rows[b]
	if ra == nil || rb == nil {
		return nil
	}
	var out []int
	for i := range ra {
		if ra[i].Verdict() != rb[i].Verdict() {
			out = append(out, ra[i].Question)
		}
	}
	return out
}

func short(s string) string {
	if len(s) <= 64 {
		return s
	}
	cut := strings.LastIndex(s[:64], " ")
	if cut < 0 {
		cut = 64
	}
	return s[:cut] + "..."
}
