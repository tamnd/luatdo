package eval

import (
	"fmt"
	"sort"
	"strings"
)

// A gate is a threshold the export path checks before it will ship a campaign.
//
// The reason these are code and not a checklist is that a checklist is worked
// by whoever is shipping, at the moment they most want to ship. The failure
// this prevents is specific and has already nearly happened once in this
// project: a build where the human review path wrote records nothing read, and
// every metric downstream looked fine because the records simply were not
// there. A gate on coverage catches the shape of that, where a gate on quality
// does not.
//
// Every gate names what it protects rather than only what it measures, because
// a threshold with no stated reason gets lowered by the person it stops.
type Gate struct {
	Name     string  `json:"name"`
	Protects string  `json:"protects"`
	Min      float64 `json:"min"`
	MinCases int     `json:"min_cases"`
}

// Result is one gate's verdict, with the numbers it saw.
type Result struct {
	Gate   Gate    `json:"gate"`
	Value  float64 `json:"value"`
	N      int     `json:"n"`
	Of     int     `json:"of"`
	Passed bool    `json:"passed"`
	// Skipped is a gate that had nothing to measure. It is not a pass. A
	// campaign with no annotated clauses has not cleared the annotation gate,
	// it has avoided it, and shipping on that basis is the accident this
	// package exists to prevent.
	Skipped bool `json:"skipped"`
}

// The release gates. The thresholds are deliberately low and are floors rather
// than targets: a floor that is never hit tells nobody anything, and one set
// where the project currently sits would block every future run that regressed
// by a point of noise.
var Gates = []Gate{
	{
		Name:     "evidence",
		Protects: "every claim in the graph can be checked against the official text in one step",
		Min:      1.0, MinCases: 1,
	},
	{
		Name:     "invariants",
		Protects: "a layer that is nearly consistent is a layer nobody can query",
		Min:      1.0, MinCases: 1,
	},
	{
		Name:     "judge-agreement",
		Protects: "the precision figures in every report were produced by this judge",
		Min:      0.4, MinCases: 50,
	},
	{
		Name:     "bearer-placement",
		Protects: "a norm whose bearer is a bare string is the shadow ontology coming back",
		Min:      0.9, MinCases: 20,
	},
	{
		Name:     "statement-f1",
		Protects: "the extraction is finding the norms that are there",
		Min:      0.6, MinCases: 30,
	},
}

// Check runs one gate over a rate and the sample it came from.
func (g Gate) Check(n, of int) Result {
	r := Result{Gate: g, N: n, Of: of, Value: Ratio(n, of)}
	if of < g.MinCases {
		r.Skipped = true
		return r
	}
	r.Passed = r.Value >= g.Min
	return r
}

// CheckValue runs a gate over a rate that is not a proportion of counted cases,
// such as a kappa. The sample size still travels with it.
func (g Gate) CheckValue(v float64, of int) Result {
	r := Result{Gate: g, Value: v, Of: of, N: of}
	if of < g.MinCases {
		r.Skipped = true
		return r
	}
	r.Passed = v >= g.Min
	return r
}

// Verdict is the whole gate run, and what the export path acts on.
type Verdict struct {
	Results []Result `json:"results"`
}

// Ship reports whether the campaign may be exported, and why not.
//
// A skipped gate blocks. That is the whole design: the cheapest way past a
// quality gate is to produce nothing for it to measure, and a system that
// treats no data as a pass rewards exactly that.
func (v Verdict) Ship() (bool, []string) {
	var reasons []string
	for _, r := range v.Results {
		switch {
		case r.Skipped:
			reasons = append(reasons, fmt.Sprintf("%s has %d cases and needs %d, so it did not run and cannot pass",
				r.Gate.Name, r.Of, r.Gate.MinCases))
		case !r.Passed:
			reasons = append(reasons, fmt.Sprintf("%s is %.3f and the floor is %.3f, which protects: %s",
				r.Gate.Name, r.Value, r.Gate.Min, r.Gate.Protects))
		}
	}
	sort.Strings(reasons)
	return len(reasons) == 0, reasons
}

func (v Verdict) String() string {
	var b strings.Builder
	for _, r := range v.Results {
		status := "pass"
		switch {
		case r.Skipped:
			status = "not run"
		case !r.Passed:
			status = "FAIL"
		}
		fmt.Fprintf(&b, "  %-18s %-8s %s  floor %.3f\n", r.Gate.Name, status, rate(r.Value, r.N, r.Of), r.Gate.Min)
	}
	ok, reasons := v.Ship()
	if ok {
		b.WriteString("  gates passed, the campaign may be exported\n")
		return b.String()
	}
	b.WriteString("  export blocked:\n")
	for _, r := range reasons {
		fmt.Fprintf(&b, "    %s\n", r)
	}
	return b.String()
}

// Named finds a gate by name so a caller does not repeat its thresholds.
func Named(name string) (Gate, error) {
	for _, g := range Gates {
		if g.Name == name {
			return g, nil
		}
	}
	return Gate{}, fmt.Errorf("no release gate named %q", name)
}
