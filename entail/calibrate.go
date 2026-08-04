package entail

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/norm"
)

// The two edges are calibrated, not chosen.
//
// A threshold picked because it looked safe is a claim about the error rate with
// no measurement behind it. What this does instead is take a budget for each
// mistake, stated up front, and find the widest band that stays inside it on data
// the gate was not fitted to. If no such band exists the band is switched off and
// the model file says so, which is the honest outcome when a perceptron cannot
// separate the classes well enough to be trusted with either tray.
//
// The two mistakes are not the same mistake. Accepting a statement the judge
// would have rejected puts a false norm in the trusted store. Rejecting one the
// judge would have accepted loses a true norm silently, and silence is the
// failure mode this project keeps finding late. They get separate bands and
// separate numbers everywhere.

// Scored is one instance with the gate's reading of it.
type Scored struct {
	Instance
	Score float64
}

// Bands is a calibrated pair of edges.
type Bands struct {
	Accept  float64 `json:"accept"`
	Reject  float64 `json:"reject"`
	Accepts bool    `json:"accepts"`
	Rejects bool    `json:"rejects"`
	Budget  float64 `json:"budget"`
	// What the calibration set said the bands would do, kept so a band that
	// looked wide can be told from one that covered two statements.
	CalibratedOn int `json:"calibrated_on"`
	AcceptShare  float64
	RejectShare  float64
}

// Calibrate finds the widest bands whose error rate on this set is inside the
// budget.
//
// The rates are per class. False acceptance is the share of the not entailed
// statements that land above the accept edge, and false rejection the share of
// the entailed ones that land below the reject edge. Dividing by the class
// rather than by everything is what keeps the number readable when the classes
// are as lopsided as these are: 934 of the judge's 1076 verdicts are entailed,
// so a rate over the whole set would call a gate that rejects a tenth of the
// true norms a one percent error.
func Calibrate(scored []Scored, budget float64) Bands {
	b := Bands{Budget: budget, CalibratedOn: len(scored)}
	var pos, neg []float64
	for _, s := range scored {
		if s.Entailed {
			pos = append(pos, s.Score)
		} else {
			neg = append(neg, s.Score)
		}
	}
	sort.Float64s(pos)
	sort.Float64s(neg)
	if len(pos) == 0 || len(neg) == 0 {
		// One class only says nothing about where the edges go, and a band drawn
		// here would be drawn from a set that cannot contradict it.
		return b
	}

	// The accept edge is the lowest score at which the share of negatives above
	// it is still inside the budget. Walking the candidate edges downwards from
	// the top negative gives the widest such band.
	for _, t := range candidates(scored) {
		if shareAbove(neg, t) <= budget {
			if share := shareAbove(pos, t); share > 0 && (!b.Accepts || t < b.Accept) {
				b.Accepts, b.Accept, b.AcceptShare = true, t, share
			}
		}
		if shareBelow(pos, t) <= budget {
			if share := shareBelow(neg, t); share > 0 && (!b.Rejects || t > b.Reject) {
				b.Rejects, b.Reject, b.RejectShare = true, t, share
			}
		}
	}
	// Bands that crossed would send the same score to both trays. That happens
	// when the classes barely separate, and the answer is to keep neither.
	if b.Accepts && b.Rejects && b.Reject >= b.Accept {
		b.Accepts, b.Rejects = false, false
	}
	return b
}

// candidates is the set of thresholds worth trying: the observed scores, which
// are the only places a decision can change.
func candidates(scored []Scored) []float64 {
	seen := map[float64]bool{}
	var out []float64
	for _, s := range scored {
		if !seen[s.Score] {
			seen[s.Score] = true
			out = append(out, s.Score)
		}
	}
	sort.Float64s(out)
	return out
}

// shareAbove and shareBelow are the share of a class that falls in a band. Read
// over the negatives they are the false acceptance rate the budget bounds, and
// read over the positives they are how much work the band actually removes, so
// both are the same two functions.
func shareAbove(scores []float64, t float64) float64 {
	n := 0
	for _, v := range scores {
		if v >= t {
			n++
		}
	}
	return float64(n) / float64(len(scores))
}

func shareBelow(scores []float64, t float64) float64 {
	n := 0
	for _, v := range scores {
		if v <= t {
			n++
		}
	}
	return float64(n) / float64(len(scores))
}

// Mean is the shipped gate's bands: the average of the edges the folds found,
// and a band is kept only if every fold found one.
//
// Averaging is the conservative reading of a disagreement about where the edge
// goes. Requiring every fold to have found a band at all is the stricter part:
// a fold that could not draw an accept edge inside the budget is a fold that
// saw data where accepting anything was unsafe, and one such fold is enough to
// say the gate should not accept on its own.
func Mean(bands []Bands) (accept, reject float64, accepts, rejects bool) {
	if len(bands) == 0 {
		return 0, 0, false, false
	}
	accepts, rejects = true, true
	for _, b := range bands {
		if !b.Accepts {
			accepts = false
		}
		if !b.Rejects {
			rejects = false
		}
		accept += b.Accept
		reject += b.Reject
	}
	return accept / float64(len(bands)), reject / float64(len(bands)), accepts, rejects
}

// Outcome is what a gate did to a labelled set: the plain agreement at the sign
// threshold, and the triage the bands produced.
type Outcome struct {
	Instances   int
	Entailed    int
	NotEntailed int

	// Agreement is the gate against the labels with no bands at all, a positive
	// score read as entailed. It answers whether distillation worked, separately
	// from whether the bands are set anywhere useful.
	TruePositive  int
	FalsePositive int
	TrueNegative  int
	FalseNegative int

	// Triage is what the bands did.
	Accepted     int
	Rejected     int
	Escalated    int
	FalseAccepts int // accepted, and the label says not entailed
	FalseRejects int // rejected, and the label says entailed
}

// Accuracy is agreement with the labels at the sign threshold.
func (o Outcome) Accuracy() float64 {
	if o.Instances == 0 {
		return 0
	}
	return float64(o.TruePositive+o.TrueNegative) / float64(o.Instances)
}

// Precision and Recall are over the entailed class, which is the one the gate
// asserts. A gate that calls everything entailed scores 87 percent accuracy on
// this corpus and is worth nothing, and these two are what say so.
func (o Outcome) Precision() float64 { return ratio(o.TruePositive, o.TruePositive+o.FalsePositive) }
func (o Outcome) Recall() float64    { return ratio(o.TruePositive, o.TruePositive+o.FalseNegative) }

// FalseAcceptRate is the share of the not entailed statements the gate waved
// through, and FalseRejectRate the share of the entailed ones it threw away.
// These are the two numbers the bands were calibrated against and the two the
// milestone exists to report.
func (o Outcome) FalseAcceptRate() float64 { return ratio(o.FalseAccepts, o.NotEntailed) }
func (o Outcome) FalseRejectRate() float64 { return ratio(o.FalseRejects, o.Entailed) }

// Saved is the share of statements that needed no judge call.
func (o Outcome) Saved() float64 { return ratio(o.Accepted+o.Rejected, o.Instances) }

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// add folds one decision into the counters.
func (o *Outcome) add(entailed bool, score float64, decision string) {
	o.Instances++
	if entailed {
		o.Entailed++
	} else {
		o.NotEntailed++
	}
	switch {
	case score > 0 && entailed:
		o.TruePositive++
	case score > 0:
		o.FalsePositive++
	case entailed:
		o.FalseNegative++
	default:
		o.TrueNegative++
	}
	switch decision {
	case norm.GateAccept:
		o.Accepted++
		if !entailed {
			o.FalseAccepts++
		}
	case norm.GateReject:
		o.Rejected++
		if entailed {
			o.FalseRejects++
		}
	default:
		o.Escalated++
	}
}

// Measure runs a trained gate over a labelled set and reports what it did.
//
// This is the honest way to use a gate on the human labels: the gate was fitted
// to the judge and the humans are a different opinion, so the only thing that
// may be done with them is measurement.
func Measure(g *Gate, instances []Instance) Outcome {
	var o Outcome
	measure(g, instances, &o)
	return o
}

// The audit share is deliberately not applied here. Auditing sends a decided
// statement to the judge anyway, which changes what the run costs and not what
// the gate decided, and folding it in would make the gate's measured error rate
// depend on how much of its work is being double checked.
func measure(g *Gate, instances []Instance, o *Outcome) {
	for _, in := range instances {
		score := g.ScoreFeatures(in.Features)
		decision := norm.GateJudge
		switch {
		case g.Accepts && score >= g.Accept:
			decision = norm.GateAccept
		case g.Rejects && score <= g.Reject:
			decision = norm.GateReject
		}
		o.add(in.Entailed, score, decision)
	}
}

// Report is a cross validated measurement of the whole design: the features,
// the learner, and the calibration procedure.
type Report struct {
	Instances int
	Folds     int
	Epochs    int
	Budget    float64
	Outcome   Outcome
	Bands     []Bands
	Provision int // how many distinct provisions the instances came from
	// Features is the size of the vocabulary a full training run produced, kept
	// so a gate that memorised one feature per example can be seen doing it.
	Features int
	Heaviest []string
}

// Evaluate is the nested cross validation.
//
// Folds are grouped by provision, not by statement. Two statements from the same
// article share their words, their bearer and usually their verdict, and a split
// that puts one in training and the other in test measures how well the gate
// memorises an article rather than how well it reads one.
//
// Each round uses three sets, not two. One fold is the test, the next fold along
// is the calibration set the bands are drawn on, and the rest is training. The
// bands are therefore never fitted to the data they are reported on, which is the
// mistake that makes a gate look calibrated and behave otherwise in production.
func Evaluate(instances []Instance, folds, epochs int, budget float64) Report {
	r := Report{Instances: len(instances), Folds: folds, Epochs: epochs, Budget: budget}
	if len(instances) == 0 || folds < 3 {
		return r
	}
	provisions := map[string]bool{}
	for _, in := range instances {
		provisions[in.ProvisionID] = true
	}
	r.Provision = len(provisions)

	assign := map[string]int{}
	for p := range provisions {
		assign[p] = int(hash64(p) % uint64(folds))
	}
	for test := range folds {
		calib := (test + 1) % folds
		var train, cal, tst []Instance
		for _, in := range instances {
			switch assign[in.ProvisionID] {
			case test:
				tst = append(tst, in)
			case calib:
				cal = append(cal, in)
			default:
				train = append(train, in)
			}
		}
		if len(train) == 0 || len(cal) == 0 || len(tst) == 0 {
			continue
		}
		g := Train(train, epochs)
		var scored []Scored
		for _, in := range cal {
			scored = append(scored, Scored{Instance: in, Score: g.ScoreFeatures(in.Features)})
		}
		b := Calibrate(scored, budget)
		g.Accept, g.Reject, g.Accepts, g.Rejects, g.Budget = b.Accept, b.Reject, b.Accepts, b.Rejects, budget
		r.Bands = append(r.Bands, b)
		measure(g, tst, &r.Outcome)
	}
	full := Train(instances, epochs)
	r.Features = len(full.Weights)
	r.Heaviest = Heaviest(full.Weights, 20)
	return r
}

// String prints the report the way this project prints measurements: the raw
// counts first, the rates next to the counts they came from, and the parts that
// were switched off named rather than left as a zero.
func (r Report) String() string {
	var b strings.Builder
	o := r.Outcome
	fmt.Fprintf(&b, "%d instances over %d provisions, %d folds, %d epochs, budget %.0f%%\n",
		r.Instances, r.Provision, r.Folds, r.Epochs, r.Budget*100)
	fmt.Fprintf(&b, "labels: %d entailed, %d not entailed\n", o.Entailed, o.NotEntailed)
	fmt.Fprintf(&b, "agreement at the sign threshold: %.1f%% (%d of %d), precision %.1f%%, recall %.1f%%\n",
		o.Accuracy()*100, o.TruePositive+o.TrueNegative, o.Instances, o.Precision()*100, o.Recall()*100)
	fmt.Fprintf(&b, "confusion: %d true entailed, %d false entailed, %d true not entailed, %d false not entailed\n",
		o.TruePositive, o.FalsePositive, o.TrueNegative, o.FalseNegative)
	fmt.Fprintf(&b, "triage: %d accepted, %d rejected, %d left to the judge, %.1f%% of calls saved\n",
		o.Accepted, o.Rejected, o.Escalated, o.Saved()*100)
	fmt.Fprintf(&b, "false accepts: %d of %d not entailed (%.1f%%)\n", o.FalseAccepts, o.NotEntailed, o.FalseAcceptRate()*100)
	fmt.Fprintf(&b, "false rejects: %d of %d entailed (%.1f%%)\n", o.FalseRejects, o.Entailed, o.FalseRejectRate()*100)
	var noAccept, noReject int
	for _, band := range r.Bands {
		if !band.Accepts {
			noAccept++
		}
		if !band.Rejects {
			noReject++
		}
	}
	fmt.Fprintf(&b, "bands: %d of %d folds found no accept band, %d of %d found no reject band\n",
		noAccept, len(r.Bands), noReject, len(r.Bands))
	fmt.Fprintf(&b, "vocabulary: %d features\n", r.Features)
	return b.String()
}
