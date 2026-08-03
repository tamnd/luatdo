package conflict

import (
	"fmt"
	"sort"
	"strings"
)

// The rules. Each one is a named reason a pair cannot both be complied with,
// and the name travels with the finding so a reader can disagree with the rule
// rather than with the pair.
const (
	// RuleDuty is an obligation to do the thing another provision forbids.
	RuleDuty = "obligation-against-prohibition"
	// RulePermission is a permission to do the thing another provision
	// forbids. This is the common one in practice, because a law permits and
	// a later decree forbids and neither drafter read the other.
	RulePermission = "permission-against-prohibition"
	// RuleDeadline is two obligations on one party to do one thing by two
	// different times.
	RuleDeadline = "incompatible-deadlines"
	// RuleSanction is two different consequences attached to one act by one
	// party. Vietnamese administrative law is where this lives: a decree sets a
	// fine band for an act and a later decree sets another, and which one an
	// inspector applies is a real question with money on it.
	RuleSanction = "incompatible-sanctions"
)

// Rules lists the closed set, in the order the report prints them.
var Rules = []string{RuleDuty, RulePermission, RuleDeadline, RuleSanction}

// Slot is one field of the comparison, with what the two forms had in it.
//
// This is the minimal responsible set from the SMT literature, spelled out for
// a reader rather than for a solver. A finding that says only "these two
// provisions conflict" gives a person two paragraphs of Vietnamese and no
// starting point. A finding that says the party and the act matched, and the
// operator is where they part, tells them what to check first and what to
// argue with if the machine is wrong.
type Slot struct {
	Name string `json:"name"`
	A    string `json:"a"`
	B    string `json:"b,omitempty"`
}

// Finding is one pair the checker could not reconcile.
type Finding struct {
	Rule string `json:"rule"`
	A    *Form  `json:"a"`
	B    *Form  `json:"b"`

	// Matched is what had to agree for the pair to be comparable, and
	// Clashing is where they part. Together they are the whole argument.
	Matched  []Slot `json:"matched"`
	Clashing []Slot `json:"clashing"`

	// Circumstances is shared or unknown, per Circumstances. A finding on
	// unknown circumstances is a weaker claim and is counted separately
	// everywhere, never added to the shared ones to make a bigger number.
	Circumstances string `json:"circumstances"`

	Rank Rank `json:"rank"`

	// Explanation is filled by a later pass, by a model, from the pair. It is
	// empty here on purpose: nothing in this file has ever asked a model
	// anything.
	Explanation string `json:"explanation,omitempty"`
}

// ID is a stable identifier for the pair, so the same finding on two runs is
// the same finding.
func (f *Finding) ID() string {
	a, b := f.A.StatementID, f.B.StatementID
	if a > b {
		a, b = b, a
	}
	return f.Rule + "|" + a + "|" + b
}

func (f *Finding) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", f.Rule, f.Circumstances)
	fmt.Fprintf(&b, "  a %s %s\n     %s\n", f.A.ProvisionID, f.A.Operator, trim(f.A.Source.Quote))
	fmt.Fprintf(&b, "  b %s %s\n     %s\n", f.B.ProvisionID, f.B.Operator, trim(f.B.Source.Quote))
	for _, s := range f.Matched {
		fmt.Fprintf(&b, "  same %s: %s\n", s.Name, s.A)
	}
	for _, s := range f.Clashing {
		fmt.Fprintf(&b, "  differs %s: %s against %s\n", s.Name, s.A, s.B)
	}
	if r := f.Rank.String(); r != "" {
		fmt.Fprintf(&b, "  %s\n", r)
	}
	return b.String()
}

func trim(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		return s[:200] + " ..."
	}
	return s
}

// Report is what a checker run found and what it looked at to find it.
//
// Pairs and Compared are here because a precision figure is meaningless without
// them. Two findings out of four comparisons and two out of forty thousand are
// different claims, and the second is the one that says the detector is usable.
type Report struct {
	Forms    int `json:"forms"`
	Pairs    int `json:"pairs"`
	Compared int `json:"compared"`
	// Skipped counts, by reason, so the funnel is readable rather than a
	// single drop from pairs to findings.
	SkippedNoOverlap int `json:"skipped_no_overlap"`
	SkippedDeferred  int `json:"skipped_deferred"`
	SkippedSameNorm  int `json:"skipped_same_provision"`

	Findings []Finding `json:"findings,omitempty"`
	// Disjoint holds the pairs a judge took out of Findings, because the two
	// sets of circumstances cannot both hold and the norms are therefore never
	// triggered together. They are kept rather than deleted so that a person
	// who disagrees with the judge can see what it threw away. See Adjudicate.
	Disjoint []Finding `json:"disjoint,omitempty"`
}

// Shared counts findings whose circumstances one contains the other, which are
// the ones a gate is computed over.
func (r *Report) Shared() int {
	n := 0
	for i := range r.Findings {
		if r.Findings[i].Circumstances == CircumstancesShared {
			n++
		}
	}
	return n
}

func (r *Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d forms, %d comparable pairs, %d compared after scope\n", r.Forms, r.Pairs, r.Compared)
	fmt.Fprintf(&b, "  %d skipped, never in force together\n", r.SkippedNoOverlap)
	fmt.Fprintf(&b, "  %d skipped, one defers to the other's instrument\n", r.SkippedDeferred)
	fmt.Fprintf(&b, "  %d skipped, same provision\n", r.SkippedSameNorm)
	if n := len(r.Disjoint); n > 0 {
		fmt.Fprintf(&b, "  %d dropped, the two sets of circumstances cannot both hold\n", n)
	}
	if len(r.Findings) == 0 {
		fmt.Fprintf(&b, "no pair failed a rule, which is a fact about this scope and not about the corpus\n")
		return b.String()
	}
	byRule := map[string]int{}
	for i := range r.Findings {
		byRule[r.Findings[i].Rule]++
	}
	fmt.Fprintf(&b, "%d findings, %d on shared circumstances and %d where containment is not proved\n",
		len(r.Findings), r.Shared(), len(r.Findings)-r.Shared())
	for _, rule := range Rules {
		if byRule[rule] > 0 {
			fmt.Fprintf(&b, "  %-32s %d\n", rule, byRule[rule])
		}
	}
	return b.String()
}

// Material is what the corpus gives the rules to work with.
//
// A run that reports nothing is either a detector working on a scope that holds
// no conflict or a detector that could never have fired, and the funnel above
// cannot tell those apart: both end at zero. Each rule needs particular
// material. Both operator rules need a prohibition on one side. The deadline
// rule needs two obligations whose deadlines are counted from an event rather
// than fixed to a date. The sanction rule needs a sanction, which the extraction
// only records with a legal basis under it.
//
// So the counts are reported beside the zero, and a reader can see which of the
// two zeroes they are looking at without going back to the corpus.
type Material struct {
	Prohibitions      int `json:"prohibitions"`
	Obligations       int `json:"obligations"`
	Permissions       int `json:"permissions"`
	Rights            int `json:"rights"`
	AnchoredDeadlines int `json:"anchored_deadlines"`
	Sanctions         int `json:"sanctions"`
}

// Materials counts what the comparable forms hold.
func Materials(forms []*Form) Material {
	var m Material
	for _, f := range forms {
		if !f.Comparable() {
			continue
		}
		switch f.Operator {
		case Obligation:
			m.Obligations++
		case Prohibition:
			m.Prohibitions++
		case Permission:
			m.Permissions++
		case Right:
			m.Rights++
		}
		if _, ok := days(f); ok {
			m.AnchoredDeadlines++
		}
		if f.Sanction != "" {
			m.Sanctions++
		}
	}
	return m
}

func (m Material) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d obligations, %d prohibitions, %d permissions, %d rights\n",
		m.Obligations, m.Prohibitions, m.Permissions, m.Rights)
	fmt.Fprintf(&b, "%d deadlines counted from an event, %d sanctions\n", m.AnchoredDeadlines, m.Sanctions)
	if m.Prohibitions == 0 {
		fmt.Fprintf(&b, "no prohibition in this scope, so neither operator rule can fire whatever the corpus says\n")
	}
	if m.Sanctions == 0 {
		fmt.Fprintf(&b, "no sanction in this scope, so the sanction rule can never fire here\n")
	}
	return b.String()
}

// incompatible names the rule for an operator pair, or reports that the pair is
// fine.
//
// Right against prohibition is deliberately absent. A right its holder does not
// exercise is not violated by a prohibition on somebody else doing the same
// thing, and the pair produced noise rather than findings when the Cypher form
// of question 19 included it.
func incompatible(a, b string) (string, bool) {
	switch {
	case a == Obligation && b == Prohibition, a == Prohibition && b == Obligation:
		return RuleDuty, true
	case a == Permission && b == Prohibition, a == Prohibition && b == Permission:
		return RulePermission, true
	}
	return "", false
}

// Check compares every comparable pair of forms and reports what it could not
// reconcile.
//
// The order of the tests is the order of their cost and of their certainty.
// Pairing is a map lookup, scope overlap is string comparison, deferral is a
// set test, and only then does a rule run. Nothing here calls anything.
func Check(forms []*Form, docs DocInfo) *Report {
	r := &Report{}
	var usable []*Form
	for _, f := range forms {
		if f.Comparable() {
			usable = append(usable, f)
		}
	}
	SortForms(usable)
	r.Forms = len(usable)

	groups := map[string][]*Form{}
	for _, f := range usable {
		groups[f.Key()] = append(groups[f.Key()], f)
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
				r.Pairs++
				a, b := g[i], g[j]
				if a.ProvisionID == b.ProvisionID {
					// A drafter does not contradict themselves inside one
					// clause. A pair from one provision is almost always the
					// same norm read twice, and counting the rate at which
					// this fires anyway is how the checker's own noise floor
					// gets measured. See bench.go.
					r.SkippedSameNorm++
					continue
				}
				if !a.Scope.Overlaps(b.Scope) {
					r.SkippedNoOverlap++
					continue
				}
				if defers(a, b) || defers(b, a) {
					r.SkippedDeferred++
					continue
				}
				r.Compared++
				if f := compare(a, b, docs); f != nil {
					r.Findings = append(r.Findings, *f)
				}
			}
		}
	}
	sort.Slice(r.Findings, func(i, j int) bool { return r.Findings[i].ID() < r.Findings[j].ID() })
	return r
}

// defers reports whether a stands down to b's instrument through an override
// exception. A norm that says another instrument governs instead is pointing at
// that instrument rather than disagreeing with it.
//
// The test is identifier equality and not a substring match. Draft resolves the
// official number out of the exception's words into the same identifier the
// document store uses, so anything looser here would be a second, weaker attempt
// at a resolution that has already been done properly.
func defers(a, b *Form) bool {
	for _, d := range a.Scope.Defers {
		if d != "" && d == b.DocID {
			return true
		}
	}
	return false
}

// compare runs the rules over one pair, in order, and returns the first that
// fires. One pair produces at most one finding: a pair that breaks two rules is
// still one thing for a person to read.
func compare(a, b *Form, docs DocInfo) *Finding {
	party, act, object := a.Words()
	matched := []Slot{{Name: "party", A: party}, {Name: "act", A: act}}
	if object != "" {
		matched = append(matched, Slot{Name: "object", A: object})
	}
	circ := Circumstances(a.Scope, b.Scope)

	if rule, bad := incompatible(a.Operator, b.Operator); bad {
		return &Finding{
			Rule: rule, A: a, B: b, Matched: matched,
			Clashing:      []Slot{{Name: "operator", A: a.Operator, B: b.Operator}},
			Circumstances: circ, Rank: rank(a, b, docs),
		}
	}
	if a.Operator == Obligation && b.Operator == Obligation {
		da, oka := days(a)
		db, okb := days(b)
		if oka && okb && da != db {
			return &Finding{
				Rule: RuleDeadline, A: a, B: b, Matched: matched,
				Clashing: []Slot{{
					Name: "deadline",
					A:    fmt.Sprintf("%s (%d days)", a.Deadline.Text, da),
					B:    fmt.Sprintf("%s (%d days)", b.Deadline.Text, db),
				}},
				Circumstances: circ, Rank: rank(a, b, docs),
			}
		}
	}
	if a.Sanction != "" && b.Sanction != "" && a.Sanction != b.Sanction {
		return &Finding{
			Rule: RuleSanction, A: a, B: b, Matched: matched,
			Clashing: []Slot{{
				Name: "sanction",
				A:    or(a.Canon.Sanction, a.Sanction),
				B:    or(b.Canon.Sanction, b.Sanction),
			}},
			Circumstances: circ, Rank: rank(a, b, docs),
		}
	}
	return nil
}

// days is the deadline in days, and whether the deadline could be compared at
// all. A deadline anchored to a fixed date is not compared against one counted
// forward from an event, because those are not the same kind of limit and
// treating them as one produced findings that meant nothing.
func days(f *Form) (int, bool) {
	if f.Deadline == nil || f.Deadline.Anchor == "" {
		return 0, false
	}
	return f.Deadline.Days()
}
