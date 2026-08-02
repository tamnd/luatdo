package eval

import (
	"fmt"
	"sort"
)

// Agreement is how often two labellers gave the same answer, and how often they
// would have by chance alone.
//
// This is here because a judge is a measuring instrument, and the precision
// figures in every milestone comment in this repository were produced by one.
// An instrument nobody checked produces numbers of unknown meaning, and the
// specific way it fails is silent: a judge that agrees with itself and with the
// extractor and with nobody else reports high precision on everything.
//
// Raw agreement is reported next to kappa rather than instead of it. On a set
// where nine in ten items are entailed, two labellers who both say entailed
// every time agree ninety percent of the time and have measured nothing, which
// is exactly the shape of the entailment gate's output.
type Agreement struct {
	Pairs  int                       `json:"pairs"`
	Same   int                       `json:"same"`
	Labels []string                  `json:"labels"`
	Matrix map[string]map[string]int `json:"matrix"`
}

// NewAgreement starts a comparison over a fixed label set.
func NewAgreement(labels ...string) *Agreement {
	return &Agreement{Labels: labels, Matrix: map[string]map[string]int{}}
}

// Observe records one item both labellers judged. An item only one of them saw
// is not an observation about agreement and is not passed in.
func (a *Agreement) Observe(human, machine string) {
	a.Pairs++
	if human == machine {
		a.Same++
	}
	if a.Matrix[human] == nil {
		a.Matrix[human] = map[string]int{}
	}
	a.Matrix[human][machine]++
	a.see(human)
	a.see(machine)
}

func (a *Agreement) see(label string) {
	for _, l := range a.Labels {
		if l == label {
			return
		}
	}
	a.Labels = append(a.Labels, label)
	sort.Strings(a.Labels)
}

// Raw is the share of items the two labellers answered the same way.
func (a *Agreement) Raw() float64 { return Ratio(a.Same, a.Pairs) }

// Kappa is Cohen's kappa: agreement above what the two labellers' own habits
// would produce by chance.
//
// Zero means the judge is worth exactly as much as guessing with its own
// frequencies. Negative means it disagrees with the human more than chance
// would, which happens and should be visible rather than clamped to zero.
func (a *Agreement) Kappa() float64 {
	if a.Pairs == 0 {
		return 0
	}
	total := float64(a.Pairs)
	humanTotals, machineTotals := map[string]int{}, map[string]int{}
	for human, row := range a.Matrix {
		for machine, n := range row {
			humanTotals[human] += n
			machineTotals[machine] += n
		}
	}
	expected := 0.0
	for _, label := range a.Labels {
		expected += float64(humanTotals[label]) / total * float64(machineTotals[label]) / total
	}
	if expected == 1 {
		return 0 // both labellers used one label only, and nothing was measured
	}
	return (a.Raw() - expected) / (1 - expected)
}

// Reading is the plain words for a kappa, so a report does not leave a reader
// to remember a table of thresholds. The bands are Landis and Koch's, which are
// arbitrary and conventional, and saying which convention is being used is the
// part that matters.
func (a *Agreement) Reading() string {
	k := a.Kappa()
	switch {
	case a.Pairs == 0:
		return "nothing to compare"
	case k < 0:
		return "worse than chance"
	case k < 0.20:
		return "slight"
	case k < 0.40:
		return "fair"
	case k < 0.60:
		return "moderate"
	case k < 0.80:
		return "substantial"
	default:
		return "almost perfect"
	}
}

// Skew is the largest share a single label takes of either labeller's own
// answers, and Skewed reports when that share is high enough that kappa stops
// meaning what a reader expects it to.
//
// This is the kappa paradox and it is not a footnote here, it is the ordinary
// case: an entailment gate that accepts nine statements in ten produces a set
// where raw agreement is high and kappa is near zero at the same time, and a
// report that prints only one of the two is arguing for a conclusion. The cure
// is not a different statistic, it is saying out loud that the set has almost
// no disagreement in it to measure.
func (a *Agreement) Skew() float64 {
	if a.Pairs == 0 {
		return 0
	}
	humanTotals, machineTotals := map[string]int{}, map[string]int{}
	for human, row := range a.Matrix {
		for machine, n := range row {
			humanTotals[human] += n
			machineTotals[machine] += n
		}
	}
	worst := 0
	for _, label := range a.Labels {
		worst = max(worst, max(humanTotals[label], machineTotals[label]))
	}
	return Ratio(worst, a.Pairs)
}

// Skewed is the threshold at which the paradox above is worth printing. Like
// the Landis and Koch bands it is a convention rather than a finding, and it is
// here as one number in one place so a reader can disagree with it.
func (a *Agreement) Skewed() bool { return a.Pairs > 0 && a.Skew() >= 0.85 }

// Disagreements returns the cells where the two labellers differed, largest
// first, because the shape of the disagreement is the finding and the single
// number is the headline.
func (a *Agreement) Disagreements() []Cell {
	var out []Cell
	for human, row := range a.Matrix {
		for machine, n := range row {
			if human != machine && n > 0 {
				out = append(out, Cell{Human: human, Machine: machine, N: n})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return out[i].Human+out[i].Machine < out[j].Human+out[j].Machine
	})
	return out
}

// Cell is one off diagonal entry of the confusion matrix.
type Cell struct {
	Human   string `json:"human"`
	Machine string `json:"machine"`
	N       int    `json:"n"`
}

func (a *Agreement) String() string {
	if a.Pairs == 0 {
		return "agreement     nothing to compare, no item carries both a human and a machine label"
	}
	s := fmt.Sprintf("agreement     raw %s, kappa %.3f (%s)\n",
		rate(a.Raw(), a.Same, a.Pairs), a.Kappa(), a.Reading())
	for _, c := range a.Disagreements() {
		s += fmt.Sprintf("              human said %s, judge said %s: %d\n", c.Human, c.Machine, c.N)
	}
	if a.Skewed() {
		s += fmt.Sprintf("              %.0f%% of the answers carry one label, so raw agreement reads high and kappa reads low, and neither number stands alone here\n", a.Skew()*100)
	}
	return s
}

// Sample size guidance. A kappa from twenty items is a number with a confidence
// interval most of a unit wide, and reporting it without saying so invites the
// reader to act on it.
func (a *Agreement) Thin() bool { return a.Pairs > 0 && a.Pairs < 50 }
