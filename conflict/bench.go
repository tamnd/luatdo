package conflict

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/norm"
)

// The benchmark. It is synthetic and it says so everywhere it is printed.
//
// A conflict detector needs pairs whose answer is known, and nobody has
// annotated Vietnamese norm pairs. The two ways out of that are both bad: hand
// annotation by whoever wrote the detector grades the detector against its own
// reading, and annotation by a model grades it against the thing it is supposed
// to replace. The third way, which the LegalWiz work took and which this follows,
// is to build the pairs by construction. Take a real trusted statement, change
// exactly one thing about it, and the label falls out of what was changed rather
// than out of anybody's opinion.
//
// What this measures and what it does not: it measures whether the checker fires
// on the clash it was shown and holds still for the near misses, which is the
// part that is mechanical and the part that silently rots. It does not measure
// whether the detector finds the conflicts a real corpus contains, because a
// generated pair is easier than a real one, and no number produced here should
// ever be quoted as accuracy on Vietnamese law.

// The mutations. Each names one thing changed about a real statement, and the
// label of every case is decided by the mutation and not by reading the pair.
const (
	// Conflicts by construction: the operator, the deadline or the consequence
	// is changed and nothing else is.
	MutFlip     = "flipped-operator"
	MutDeadline = "changed-deadline"
	MutSanction = "changed-sanction"
	// MutOverlap clashes on the operator under two conditions that are
	// different and can both hold, so it is a real conflict that containment
	// cannot prove. It is the near miss of MutCondition read the other way
	// round, and the two together are what stop a judge from scoring well by
	// answering the same way every time.
	MutOverlap = "compatible-conditions"

	// Near misses. Each one clashes on the operator and is distinguished by
	// exactly one other thing, which is the case a detector gets wrong.
	MutParty     = "other-party"
	MutAct       = "other-act"
	MutCondition = "disjoint-conditions"
	MutInterval  = "never-in-force-together"
	MutDefers    = "defers-to-the-other"
	MutRestated  = "same-operator-restated"
)

// Mutations lists the closed set, conflicts first.
var Mutations = []string{
	MutFlip, MutDeadline, MutSanction, MutOverlap,
	MutParty, MutAct, MutCondition, MutInterval, MutDefers, MutRestated,
}

// conflicting reports whether a mutation produces a pair that cannot both be
// complied with.
func conflicting(mut string) bool {
	switch mut {
	case MutFlip, MutDeadline, MutSanction, MutOverlap:
		return true
	}
	return false
}

// The circumstance pairs the two condition mutations are built from.
//
// They are lists rather than one pair each because a benchmark that plants the
// same two phrases on every case measures whether a judge can answer one
// question, not whether it can answer this kind of question. Both tables are
// walked by seed order, so the same store still produces the same benchmark.
//
// Written without diacritics because that is how the store holds a condition,
// and a judge that only works on the pretty form is not the judge this package
// uses.
var (
	// exclusive pairs cannot both be true of one occasion.
	//
	// Every one of them is about when the situation is, and none of them is
	// about who is in it. That is deliberate, and it is deliberate because the
	// first version of this table was not. Working in the country against
	// working abroad, and a fixed term contract against an open ended one, are
	// exclusive for one worker and both true at once for an enterprise that
	// employs hundreds, so labelling those pairs a near miss punished a judge
	// for being right. The checker compares two norms triggered by one
	// situation, and an exclusion that holds whoever the party is is the
	// exclusion this table is supposed to hold.
	exclusive = [][2]string{
		{"trong-truong-hop-khan-cap", "trong-truong-hop-thong-thuong"},
		{"trong-gio-lam-viec", "ngoai-gio-lam-viec"},
		{"trong-thoi-gian-thu-viec", "sau-khi-het-thoi-gian-thu-viec"},
		{"khi-hop-dong-con-hieu-luc", "sau-khi-hop-dong-het-hieu-luc"},
		{"truoc-khi-nop-ho-so", "sau-khi-ho-so-duoc-chap-thuan"},
		{"trong-thoi-han-quy-dinh", "khi-da-qua-thoi-han-quy-dinh"},
	}
	// compatible pairs are different and can hold at the same time.
	compatible = [][2]string{
		{"trong-gio-lam-viec", "tai-tru-so-cua-co-quan"},
		{"doi-voi-lao-dong-nu", "trong-thoi-gian-thu-viec"},
		{"khi-co-yeu-cau-bang-van-ban", "trong-truong-hop-can-thiet"},
		{"hop-dong-xac-dinh-thoi-han-12-thang", "lam-viec-theo-ca"},
		{"truoc-khi-tra-luong", "doi-voi-nguoi-lao-dong-da-qua-dao-tao"},
		{"theo-quy-dinh-cua-phap-luat", "trong-pham-vi-nhiem-vu-quyen-han-cua-minh"},
	}
)

// Case is one graded pair.
type Case struct {
	ID       string `json:"id"`
	Mutation string `json:"mutation"`
	// Conflict is the ground truth, which is a fact about how the case was
	// built.
	Conflict bool `json:"conflict"`
	// Rule is the rule that ought to fire on a conflicting case.
	Rule string `json:"rule,omitempty"`
	// Seed is the real statement the case was grown from, so a case that looks
	// wrong can be traced back to the provision it came from.
	Seed string `json:"seed"`

	A *Form `json:"a"`
	B *Form `json:"b"`
}

// Build grows a benchmark from real forms.
//
// Seeds are taken in identifier order and every mutation is applied to every
// seed it fits, so the same store produces the same benchmark on every machine
// and on every run. perMutation caps each mutation, because the corpus has 471
// duties and 8 prohibitions and an uncapped set would be almost entirely one
// shape.
func Build(seeds []*Form, perMutation int) []Case {
	usable := make([]*Form, 0, len(seeds))
	for _, f := range seeds {
		if f.Comparable() {
			usable = append(usable, f)
		}
	}
	SortForms(usable)

	var out []Case
	for _, mut := range Mutations {
		n := 0
		for _, seed := range usable {
			if perMutation > 0 && n >= perMutation {
				break
			}
			c, ok := mutate(seed, mut, n)
			if !ok {
				continue
			}
			out = append(out, c)
			n++
		}
	}
	return out
}

// opposite is the operator that cannot hold beside this one.
func opposite(op string) (string, string, bool) {
	switch op {
	case Obligation:
		return Prohibition, RuleDuty, true
	case Permission:
		return Prohibition, RulePermission, true
	case Prohibition:
		return Obligation, RuleDuty, true
	}
	// A right has no opposite here, deliberately. See incompatible in check.go.
	return "", "", false
}

// mutate builds one case, or reports that this seed cannot carry this mutation.
//
// n is how many cases this mutation has already produced, and it is used only to
// walk the circumstance tables, so the cases of one mutation are spread over
// them rather than repeating one pair of phrases.
func mutate(seed *Form, mut string, n int) (Case, bool) {
	a := seed.clone()
	b := seed.clone()
	b.StatementID = "vn:bench:" + mut + ":" + short(seed.StatementID)
	b.DocID = "vn:bench:" + mut
	b.ProvisionID = b.DocID + ":article-1"

	c := Case{
		ID: mut + "|" + short(seed.StatementID), Mutation: mut, Seed: seed.StatementID,
		Conflict: conflicting(mut), A: a, B: b,
	}

	switch mut {
	case MutFlip:
		op, rule, ok := opposite(seed.Operator)
		if !ok {
			return Case{}, false
		}
		b.Operator, c.Rule = op, rule

	case MutDeadline:
		if seed.Operator != Obligation {
			return Case{}, false
		}
		d, ok := days(seed)
		if !ok {
			return Case{}, false
		}
		b.Deadline = &norm.Deadline{
			Text:     fmt.Sprintf("%d ngày", d*2+1),
			Value:    d*2 + 1,
			Unit:     norm.UnitDay,
			Calendar: norm.CalendarNormal,
			Anchor:   seed.Deadline.Anchor,
			AnchorAt: seed.Deadline.AnchorAt,
		}
		c.Rule = RuleDeadline

	case MutSanction:
		if seed.Sanction == "" {
			return Case{}, false
		}
		b.Sanction = "phat-tien-khac-" + short(seed.StatementID)
		b.Canon.Sanction = "phạt tiền theo mức khác"
		c.Rule = RuleSanction

	case MutParty:
		op, _, ok := opposite(seed.Operator)
		if !ok {
			return Case{}, false
		}
		b.Operator = op
		b.Party = "vn:bench:party:" + short(seed.StatementID)
		b.Canon.Party = "một chủ thể khác"

	case MutAct:
		op, _, ok := opposite(seed.Operator)
		if !ok {
			return Case{}, false
		}
		b.Operator = op
		b.Act = "vn:bench:act:" + short(seed.StatementID)
		b.Canon.Act = "một hành vi khác"

	case MutCondition:
		op, _, ok := opposite(seed.Operator)
		if !ok {
			return Case{}, false
		}
		p, ok := pick(exclusive, seed, n)
		if !ok {
			return Case{}, false
		}
		b.Operator = op
		// Both sides get a condition the other does not have, because a
		// condition on one side alone is not a distinction: an unconditional
		// norm reaches every case the conditional one reaches, so that pair
		// really does clash wherever the condition holds.
		a.Scope.Conditions = append(a.Scope.Conditions, p[0])
		b.Scope.Conditions = append(b.Scope.Conditions, p[1])

	case MutOverlap:
		op, rule, ok := opposite(seed.Operator)
		if !ok {
			return Case{}, false
		}
		p, ok := pick(compatible, seed, n)
		if !ok {
			return Case{}, false
		}
		b.Operator = op
		// The same shape as MutCondition with the ground truth the other way
		// round. A judge that answers every condition pair the same way scores
		// on one of these two mutations and fails the other.
		a.Scope.Conditions = append(a.Scope.Conditions, p[0])
		b.Scope.Conditions = append(b.Scope.Conditions, p[1])
		c.Rule = rule

	case MutInterval:
		op, _, ok := opposite(seed.Operator)
		if !ok {
			return Case{}, false
		}
		b.Operator = op
		a.Scope.From, a.Scope.To = "2010-01-01", "2015-01-01"
		b.Scope.From, b.Scope.To = "2015-01-01", ""

	case MutDefers:
		op, _, ok := opposite(seed.Operator)
		if !ok {
			return Case{}, false
		}
		b.Operator = op
		a.Scope.Defers = append(a.Scope.Defers, b.DocID)

	case MutRestated:
		// Same operator, different instrument. Two instruments saying the same
		// thing is duplication and duplication is not a conflict, and a
		// detector that pairs on party and act alone reports it as one.

	default:
		return Case{}, false
	}

	// Both sides get their sentence rewritten last, after the mutation, because
	// neither of them says what the case says any more. The generated side never
	// existed in any document, and the seed side has had a condition or an
	// interval planted on it by half of these mutations. Leaving the original
	// quote on one side and a note on the other is how the first baseline run
	// came to score zero: it was reading a real provision against a sentence
	// that stated no norm.
	a.Source.Quote = Restate(a)
	b.Source.Quote = Restate(b)
	return c, true
}

func (f *Form) clone() *Form {
	c := *f
	c.Scope.Conditions = append([]string(nil), f.Scope.Conditions...)
	c.Scope.Exceptions = append([]string(nil), f.Scope.Exceptions...)
	c.Scope.Defers = append([]string(nil), f.Scope.Defers...)
	if f.Deadline != nil {
		d := *f.Deadline
		c.Deadline = &d
	}
	return &c
}

// pick returns the first pair of the table that the seed does not already state
// either half of, starting at n so the cases spread across the table.
//
// The skip matters. A seed whose own conditions already carry one of the phrases
// would produce two sets where one contains the other, the containment test
// would place the pair without asking anybody, and the case would silently stop
// measuring the thing it was built to measure.
func pick(table [][2]string, seed *Form, n int) ([2]string, bool) {
	for i := range table {
		p := table[(n+i)%len(table)]
		if !has(seed.Scope.Conditions, p[0]) && !has(seed.Scope.Conditions, p[1]) {
			return p, true
		}
	}
	return [2]string{}, false
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func short(id string) string {
	if i := strings.LastIndex(id, ":"); i >= 0 && i+1 < len(id) {
		return id[i+1:]
	}
	return id
}

// KindScore is one mutation's line in the table.
type KindScore struct {
	Cases    int  `json:"cases"`
	Conflict bool `json:"conflict"`
	Fired    int  `json:"fired"`
	// Correct is cases the checker got right, which for a conflicting mutation
	// means it fired with the expected rule and for a near miss means it held
	// still.
	Correct int `json:"correct"`
}

// Grade is what a benchmark run produced.
//
// Two precisions are reported and they are not interchangeable. Precision is
// over every finding, and SharedPrecision is over findings where one norm's
// conditions contain the other's, which are the pairs the checker claims can
// really both be triggered. The disjoint conditions mutation is built to fire on
// the first and not the second, so the gap between the two numbers is the
// benchmark showing what the containment test buys.
type Grade struct {
	Cases     int `json:"cases"`
	Conflicts int `json:"conflicts"`
	Negatives int `json:"negatives"`

	TruePositives  int `json:"true_positives"`
	FalsePositives int `json:"false_positives"`
	TrueNegatives  int `json:"true_negatives"`
	FalseNegatives int `json:"false_negatives"`

	// WrongRule counts conflicting cases the checker caught with a rule other
	// than the one the mutation planted. They count as found, because the pair
	// was reported, and they are printed because a rule firing for the wrong
	// reason is a defect waiting for a corpus that exercises it.
	WrongRule int `json:"wrong_rule"`

	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`

	SharedFindings  int     `json:"shared_findings"`
	SharedTrue      int     `json:"shared_true"`
	SharedPrecision float64 `json:"shared_precision"`

	// Adjudicated counts pairs where containment failed and a judge was asked,
	// and Dropped counts the ones it took out. A grade with a judge behind it
	// and a grade without one are different measurements, so the counts sit in
	// the artifact rather than in the run that produced it.
	Adjudicated int `json:"adjudicated"`
	Dropped     int `json:"dropped_disjoint"`
	// JudgeFailed counts pairs the judge would not answer about. Those keep
	// their finding, so this number is a floor under the false positives.
	JudgeFailed int `json:"judge_failed"`
	// Stopped says the run was cut short, so the scores are over the cases it
	// reached and not over the list it was given.
	Stopped bool `json:"stopped,omitempty"`

	ByMutation map[string]*KindScore `json:"by_mutation"`

	// Misses names the cases that went wrong, capped, so a run that fails the
	// gate says which pairs to look at rather than only by how much.
	Misses []string `json:"misses,omitempty"`
}

// maxMisses is how many wrong cases a grade lists. A grade with 400 misses needs
// a look at the first few and a rewrite, not a longer list.
const maxMisses = 20

// GradeCases runs the checker over every case and scores it.
//
// The judge may be nil, and a nil judge is a real configuration rather than a
// degraded one: it grades the deterministic checker alone, which is the number
// to quote when the question is what the code does without a model behind it.
// With a judge, every pair the checker could not place on containment costs one
// call, and the counts of those calls end up in the grade.
func GradeCases(ctx context.Context, cases []Case, docs DocInfo, j Judge) *Grade {
	g := &Grade{ByMutation: map[string]*KindScore{}}
	for i := range cases {
		if ctx.Err() != nil {
			// A grade over half the cases is a grade over half the cases, and
			// saying so is the only way the number stays honest.
			g.Stopped = true
			break
		}
		c := cases[i]
		g.Cases++
		k := g.ByMutation[c.Mutation]
		if k == nil {
			k = &KindScore{Conflict: c.Conflict}
			g.ByMutation[c.Mutation] = k
		}
		k.Cases++

		report := Check([]*Form{c.A, c.B}, docs)
		asked, dropped, failed, _ := Adjudicate(ctx, report, j)
		g.Adjudicated += asked
		g.Dropped += dropped
		g.JudgeFailed += failed
		fired := len(report.Findings) > 0
		if fired {
			k.Fired++
		}
		shared := report.Shared() > 0
		if shared {
			g.SharedFindings++
			if c.Conflict {
				g.SharedTrue++
			}
		}

		switch {
		case c.Conflict && fired:
			g.Conflicts++
			g.TruePositives++
			if report.Findings[0].Rule != c.Rule {
				g.WrongRule++
			}
			k.Correct++
		case c.Conflict && !fired:
			g.Conflicts++
			g.FalseNegatives++
			g.miss(c, "not caught")
		case !c.Conflict && fired:
			g.Negatives++
			g.FalsePositives++
			g.miss(c, "reported as "+report.Findings[0].Rule)
		default:
			g.Negatives++
			g.TrueNegatives++
			k.Correct++
		}
	}
	g.Precision = ratio(g.TruePositives, g.TruePositives+g.FalsePositives)
	g.Recall = ratio(g.TruePositives, g.TruePositives+g.FalseNegatives)
	if g.Precision+g.Recall > 0 {
		g.F1 = 2 * g.Precision * g.Recall / (g.Precision + g.Recall)
	}
	g.SharedPrecision = ratio(g.SharedTrue, g.SharedFindings)
	return g
}

func (g *Grade) miss(c Case, why string) {
	if len(g.Misses) >= maxMisses {
		return
	}
	g.Misses = append(g.Misses, c.ID+": "+why)
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func (g *Grade) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d generated pairs, %d conflicting by construction and %d near misses\n",
		g.Cases, g.Conflicts, g.Negatives)
	fmt.Fprintf(&b, "precision %.2f, recall %.2f, f1 %.2f\n", g.Precision, g.Recall, g.F1)
	fmt.Fprintf(&b, "precision on shared circumstances %.2f over %d findings\n", g.SharedPrecision, g.SharedFindings)
	if g.Adjudicated > 0 {
		fmt.Fprintf(&b, "%d pairs went to the judge, %d dropped as never triggered together, %d it would not answer\n",
			g.Adjudicated, g.Dropped, g.JudgeFailed)
	} else {
		fmt.Fprintf(&b, "no judge behind this run, so every pair the code could not place stands as a finding\n")
	}
	if g.Stopped {
		fmt.Fprintf(&b, "the run was stopped early, these scores are over the cases it reached\n")
	}
	if g.WrongRule > 0 {
		fmt.Fprintf(&b, "%d caught by a rule other than the one planted\n", g.WrongRule)
	}
	for _, mut := range Mutations {
		k := g.ByMutation[mut]
		if k == nil {
			continue
		}
		want := "must not fire"
		if k.Conflict {
			want = "must fire"
		}
		fmt.Fprintf(&b, "  %-26s %3d cases, %s, fired %3d, correct %3d\n", mut, k.Cases, want, k.Fired, k.Correct)
	}
	for _, m := range g.Misses {
		fmt.Fprintf(&b, "  wrong: %s\n", m)
	}
	fmt.Fprintf(&b, "these pairs are generated, not real law, and this is not accuracy on the corpus\n")
	return b.String()
}

// Noise is the annotation-free half of the evaluation.
//
// It runs the checker over pairs of statements from the same provision, which is
// the one place a false positive can be recognised without anybody labelling
// anything: a drafter does not contradict themselves inside one clause, so a
// rule that fires there has fired on two readings of one norm. The rate is a
// ceiling on the checker's own noise and it is measured on the real corpus
// rather than on anything generated.
type Noise struct {
	Pairs    int       `json:"pairs"`
	Fired    int       `json:"fired"`
	Rate     float64   `json:"rate"`
	Examples []Finding `json:"examples,omitempty"`
}

// maxNoiseExamples is how many same-provision firings are kept for reading.
const maxNoiseExamples = 5

// NoiseFloor measures the same-provision firing rate over real forms.
func NoiseFloor(forms []*Form, docs DocInfo) Noise {
	var n Noise
	groups := map[string][]*Form{}
	for _, f := range forms {
		if f.Comparable() {
			groups[f.ProvisionID+"|"+f.Key()] = append(groups[f.ProvisionID+"|"+f.Key()], f)
		}
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		g := groups[k]
		for i := range g {
			for j := i + 1; j < len(g); j++ {
				n.Pairs++
				if f := compare(g[i], g[j], docs); f != nil {
					n.Fired++
					if len(n.Examples) < maxNoiseExamples {
						n.Examples = append(n.Examples, *f)
					}
				}
			}
		}
	}
	n.Rate = ratio(n.Fired, n.Pairs)
	return n
}

func (n Noise) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d pairs from within one provision, %d fired a rule, rate %.3f\n", n.Pairs, n.Fired, n.Rate)
	if n.Fired == 0 && n.Pairs > 0 {
		fmt.Fprintf(&b, "nothing fired where nothing should, which is the result this measurement wants\n")
	}
	for i := range n.Examples {
		b.WriteString(n.Examples[i].String())
	}
	return b.String()
}
