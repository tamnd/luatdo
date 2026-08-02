// Package eval is the measuring instrument every layer reports through.
//
// It exists because the concept layer and the norm layer had each grown their
// own confusion table with the same three fields and the same division by zero
// guard, and two copies of an arithmetic is two chances for a number in a
// report to mean something other than what the reader takes it to mean.
//
// The rule this package enforces is that a rate never travels without its
// counts. Ninety percent of ten and ninety percent of two thousand are
// different claims about the world, and a report that prints 0.900 for both has
// thrown away the difference before the reader could see it.
package eval

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Count is one confusion table over a decision the pipeline makes: how often it
// said yes and was right, said yes and was wrong, and said nothing where the
// annotation said yes.
//
// There is no true negative field. Every decision here is over an open set, so
// the true negatives are every claim nobody made about every provision, which
// is not a number and would only make accuracy look good by counting silence.
type Count struct {
	TP int `json:"tp"`
	FP int `json:"fp"`
	FN int `json:"fn"`
}

// Add folds another table into this one, for summing across documents.
func (c *Count) Add(o Count) { c.TP, c.FP, c.FN = c.TP+o.TP, c.FP+o.FP, c.FN+o.FN }

// Observe records one decision against one annotation.
func (c *Count) Observe(want, got bool) {
	switch {
	case want && got:
		c.TP++
	case got:
		c.FP++
	case want:
		c.FN++
	}
}

func (c Count) Precision() float64 { return Ratio(c.TP, c.TP+c.FP) }
func (c Count) Recall() float64    { return Ratio(c.TP, c.TP+c.FN) }

// F1 is the harmonic mean, and it is zero rather than undefined when the
// pipeline found nothing, which is the honest reading of finding nothing.
func (c Count) F1() float64 {
	p, r := c.Precision(), c.Recall()
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

// Decided is how many annotated or extracted items the table rests on.
func (c Count) Decided() int { return c.TP + c.FP + c.FN }

// String renders the table with its counts attached, never without.
func (c Count) String() string {
	return fmt.Sprintf("precision %s recall %s f1 %.3f",
		rate(c.Precision(), c.TP, c.TP+c.FP), rate(c.Recall(), c.TP, c.TP+c.FN), c.F1())
}

// Accuracy is right out of decided, over the cases where the annotation and the
// pipeline both had an opinion. It is the shape for a choice among known
// options, where a confusion table would need one row per option to say the
// same thing.
type Accuracy struct {
	Right int `json:"right"`
	Of    int `json:"of"`
}

// Observe records one comparison.
func (a *Accuracy) Observe(right bool) {
	a.Of++
	if right {
		a.Right++
	}
}

func (a *Accuracy) Add(o Accuracy) { a.Right, a.Of = a.Right+o.Right, a.Of+o.Of }
func (a Accuracy) Rate() float64   { return Ratio(a.Right, a.Of) }
func (a Accuracy) String() string  { return rate(a.Rate(), a.Right, a.Of) }

// Ratio is the division every rate in this project goes through. An empty
// denominator is zero and not a panic, because a metric over a layer that has
// not run yet is a report and not a crash.
func Ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// rate is the one rendering of a proportion in this project: the number, then
// what it was computed from.
func rate(r float64, n, of int) string {
	if of == 0 {
		return "n/a (0 cases)"
	}
	return fmt.Sprintf("%.3f (%d of %d)", r, n, of)
}

// Interval is the Wilson score interval at 95 percent for a proportion.
//
// It is here so a report can refuse to let two numbers look different when the
// sample cannot tell them apart. A gold set of a hundred clauses puts about
// nine points of width on a rate near ninety percent, and a milestone comment
// claiming an improvement from 0.88 to 0.91 on that sample is claiming
// something the sample does not know.
func Interval(n, of int) (lo, hi float64) {
	if of == 0 {
		return 0, 0
	}
	const z = 1.96
	p, total := float64(n)/float64(of), float64(of)
	denom := 1 + z*z/total
	centre := (p + z*z/(2*total)) / denom
	spread := z * math.Sqrt(p*(1-p)/total+z*z/(4*total*total)) / denom
	return math.Max(0, centre-spread), math.Min(1, centre+spread)
}

// Separates reports whether two proportions have non overlapping intervals,
// which is the weakest honest reason to call one of them better.
func Separates(aN, aOf, bN, bOf int) bool {
	alo, ahi := Interval(aN, aOf)
	blo, bhi := Interval(bN, bOf)
	return ahi < blo || bhi < alo
}

// Table is a named set of metrics from one layer, which is what a suite prints
// and what a release gate reads.
type Table struct {
	Layer  string              `json:"layer"`
	Scope  string              `json:"scope"`
	Counts map[string]Count    `json:"counts,omitempty"`
	Rates  map[string]Accuracy `json:"rates,omitempty"`
	Notes  []string            `json:"notes,omitempty"`
}

// NewTable starts a table for a layer over a stated scope. The scope is not
// decoration: a precision figure without the set it was computed over is a
// number somebody will quote against a different set next quarter.
func NewTable(layer, scope string) *Table {
	return &Table{Layer: layer, Scope: scope,
		Counts: map[string]Count{}, Rates: map[string]Accuracy{}}
}

func (t *Table) Count(name string, c Count)   { t.Counts[name] = c }
func (t *Table) Rate(name string, a Accuracy) { t.Rates[name] = a }
func (t *Table) Note(format string, a ...any) { t.Notes = append(t.Notes, fmt.Sprintf(format, a...)) }

// String renders the table, names sorted so two runs read the same.
func (t *Table) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s over %s\n", t.Layer, t.Scope)
	width := 0
	for name := range t.Counts {
		width = max(width, len(name))
	}
	for name := range t.Rates {
		width = max(width, len(name))
	}
	for _, name := range sorted(t.Counts) {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, name, t.Counts[name])
	}
	for _, name := range sortedRates(t.Rates) {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, name, t.Rates[name])
	}
	for _, n := range t.Notes {
		fmt.Fprintf(&b, "  note: %s\n", n)
	}
	return b.String()
}

func sorted(m map[string]Count) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedRates(m map[string]Accuracy) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
