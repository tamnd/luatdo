package conflict

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/norm"
)

// pair builds two forms that agree on everything the checker pairs over, in two
// different instruments, so every test here starts from a pair the checker will
// look at and changes one thing about it.
func pair(opA, opB string) (*Form, *Form) {
	a := form("a", opA)
	b := form("b", opB)
	b.DocID = decree
	b.ProvisionID = decree + ":article-3"
	return a, b
}

// only runs the checker over one pair and insists on at most one finding, which
// is what the checker promises: a pair that breaks two rules is still one thing
// for a person to read.
func only(t *testing.T, a, b *Form, docs DocInfo) *Finding {
	t.Helper()
	r := Check([]*Form{a, b}, docs)
	if len(r.Findings) > 1 {
		t.Fatalf("one pair produced %d findings", len(r.Findings))
	}
	if len(r.Findings) == 0 {
		return nil
	}
	return &r.Findings[0]
}

func TestCheckFindsAnObligationAgainstAProhibition(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	f := only(t, a, b, nil)
	if f == nil {
		t.Fatal("a duty to do what another instrument forbids was not reported")
	}
	if f.Rule != RuleDuty {
		t.Errorf("rule = %q, want %q", f.Rule, RuleDuty)
	}
	if f.Circumstances != CircumstancesShared {
		t.Errorf("circumstances = %q, want %q for two unconditional norms", f.Circumstances, CircumstancesShared)
	}
	// The minimal responsible set is the whole argument: what had to agree, and
	// where they part.
	if len(f.Matched) != 2 || f.Matched[0].Name != "party" || f.Matched[1].Name != "act" {
		t.Fatalf("matched = %+v, want the party and the act", f.Matched)
	}
	if f.Matched[0].A != "người sử dụng lao động" {
		t.Errorf("matched party = %q, want the wording rather than the slug", f.Matched[0].A)
	}
	if len(f.Clashing) != 1 || f.Clashing[0].Name != "operator" {
		t.Fatalf("clashing = %+v, want the operator", f.Clashing)
	}
	if f.Clashing[0].A != Obligation || f.Clashing[0].B != Prohibition {
		t.Errorf("clashing operator = %q against %q", f.Clashing[0].A, f.Clashing[0].B)
	}
	if f.Explanation != "" {
		t.Error("the checker filled an explanation, which means a model got into detection")
	}
}

func TestCheckFindsAPermissionAgainstAProhibition(t *testing.T) {
	a, b := pair(Permission, Prohibition)
	f := only(t, a, b, nil)
	if f == nil || f.Rule != RulePermission {
		t.Fatalf("permission against prohibition gave %+v", f)
	}
}

func TestCheckIgnoresARightAgainstAProhibition(t *testing.T) {
	// A right its holder does not exercise is not violated by a prohibition on
	// somebody else doing the same thing, and the pair produced noise rather
	// than findings when the Cypher form of question 19 included it.
	a, b := pair(Right, Prohibition)
	if f := only(t, a, b, nil); f != nil {
		t.Fatalf("a right against a prohibition was reported as %s", f.Rule)
	}
}

func TestCheckIgnoresAgreeingOperators(t *testing.T) {
	for _, op := range Operators {
		a, b := pair(op, op)
		if f := only(t, a, b, nil); f != nil {
			t.Errorf("two %s norms were reported as %s, but duplication is not a conflict", op, f.Rule)
		}
	}
}

func deadline(days int, anchor string) *norm.Deadline {
	return &norm.Deadline{
		Text: "trong thời hạn nêu trên", Value: days, Unit: norm.UnitDay,
		Calendar: norm.CalendarNormal, Anchor: anchor,
	}
}

func TestCheckFindsIncompatibleDeadlines(t *testing.T) {
	a, b := pair(Obligation, Obligation)
	a.Deadline = deadline(15, "kể từ ngày nhận hồ sơ")
	b.Deadline = deadline(30, "kể từ ngày nhận hồ sơ")
	f := only(t, a, b, nil)
	if f == nil || f.Rule != RuleDeadline {
		t.Fatalf("two deadlines on one duty gave %+v", f)
	}
	if len(f.Clashing) != 1 || !strings.Contains(f.Clashing[0].A, "15 days") {
		t.Errorf("clashing = %+v, want both limits in days", f.Clashing)
	}
}

func TestCheckIgnoresDeadlinesThatCannotBeCompared(t *testing.T) {
	a, b := pair(Obligation, Obligation)
	// A deadline with no anchor is not counted from anything, so it is not the
	// same kind of limit as one that is, and treating them as one produced
	// findings that meant nothing.
	a.Deadline = deadline(15, "")
	b.Deadline = deadline(30, "kể từ ngày nhận hồ sơ")
	if f := only(t, a, b, nil); f != nil {
		t.Fatalf("an unanchored deadline was compared: %s", f)
	}
	// The same limit written in different units is the same limit.
	a.Deadline = deadline(30, "kể từ ngày nhận hồ sơ")
	b.Deadline = &norm.Deadline{
		Text: "01 tháng", Value: 1, Unit: norm.UnitMonth,
		Calendar: norm.CalendarNormal, Anchor: "kể từ ngày nhận hồ sơ",
	}
	if f := only(t, a, b, nil); f != nil {
		t.Fatalf("30 days against one month was reported: %s", f)
	}
}

func TestCheckFindsIncompatibleSanctions(t *testing.T) {
	a, b := pair(Obligation, Obligation)
	a.Sanction, a.Canon.Sanction = "phat-tien-5-trieu", "phạt tiền 5 triệu đồng"
	b.Sanction, b.Canon.Sanction = "phat-tien-20-trieu", "phạt tiền 20 triệu đồng"
	f := only(t, a, b, nil)
	if f == nil || f.Rule != RuleSanction {
		t.Fatalf("two consequences on one act gave %+v", f)
	}
	if f.Clashing[0].A != "phạt tiền 5 triệu đồng" || f.Clashing[0].B != "phạt tiền 20 triệu đồng" {
		t.Errorf("clashing = %+v, want the wording rather than the slugs", f.Clashing)
	}
	// One side carrying no sanction is silence, not disagreement.
	b.Sanction, b.Canon.Sanction = "", ""
	if f := only(t, a, b, nil); f != nil {
		t.Fatalf("a sanction against no sanction was reported: %s", f)
	}
}

func TestCheckSkipsTwoStatementsFromOneProvision(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	b.DocID, b.ProvisionID = a.DocID, a.ProvisionID
	r := Check([]*Form{a, b}, nil)
	if len(r.Findings) != 0 {
		t.Fatalf("a pair from one provision was reported: %s", r.Findings[0].String())
	}
	if r.SkippedSameNorm != 1 {
		t.Errorf("skipped_same_provision = %d, want 1", r.SkippedSameNorm)
	}
}

func TestCheckSkipsNormsThatWereNeverInForceTogether(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	a.Scope.From, a.Scope.To = "2010-01-01", "2015-01-01"
	b.Scope.From, b.Scope.To = "2015-01-01", ""
	r := Check([]*Form{a, b}, nil)
	if len(r.Findings) != 0 {
		t.Fatalf("a repealed norm was reported against its replacement: %s", r.Findings[0].String())
	}
	if r.SkippedNoOverlap != 1 {
		t.Errorf("skipped_no_overlap = %d, want 1", r.SkippedNoOverlap)
	}
}

func TestCheckSkipsWhereOneNormDefersToTheOther(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	// A norm that says another instrument governs instead is pointing at that
	// instrument rather than disagreeing with it.
	a.Scope.Defers = []string{decree}
	r := Check([]*Form{a, b}, nil)
	if len(r.Findings) != 0 {
		t.Fatalf("a deferral was reported as a clash: %s", r.Findings[0].String())
	}
	if r.SkippedDeferred != 1 {
		t.Errorf("skipped_deferred = %d, want 1", r.SkippedDeferred)
	}
}

func TestCheckDoesNotPairAcrossDifferentActsOrParties(t *testing.T) {
	for _, change := range []func(*Form){
		func(f *Form) { f.Act = "cong-bo" },
		func(f *Form) { f.Party = "nguoi-lao-dong" },
		func(f *Form) { f.Object = "du-lieu-ca-nhan" },
	} {
		a, b := pair(Obligation, Prohibition)
		change(b)
		r := Check([]*Form{a, b}, nil)
		if r.Pairs != 0 {
			t.Errorf("%d pairs, want none where the forms do not share a key", r.Pairs)
		}
		if len(r.Findings) != 0 {
			t.Errorf("reported %s across a key difference", r.Findings[0].Rule)
		}
	}
}

func TestCheckDropsFormsItCannotCompare(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	c := form("c", "")
	r := Check([]*Form{a, b, c}, nil)
	if r.Forms != 2 {
		t.Errorf("forms = %d, want the two that state a modality", r.Forms)
	}
}

func TestCheckReportsUnknownCircumstancesSeparately(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	a.Scope.Conditions = []string{"trong-truong-hop-khan-cap"}
	b.Scope.Conditions = []string{"trong-truong-hop-thong-thuong"}
	r := Check([]*Form{a, b}, nil)
	if len(r.Findings) != 1 {
		t.Fatalf("findings = %d, want one on unknown circumstances", len(r.Findings))
	}
	if r.Findings[0].Circumstances != CircumstancesUnknown {
		t.Errorf("circumstances = %q, want %q", r.Findings[0].Circumstances, CircumstancesUnknown)
	}
	if r.Shared() != 0 {
		t.Errorf("shared = %d, want none, a weaker claim must never be added to the stronger ones", r.Shared())
	}
	if s := r.String(); !strings.Contains(s, "containment is not proved") {
		t.Errorf("the report does not say the finding is the weaker kind:\n%s", s)
	}
}

func TestFindingIDDoesNotDependOnPairOrder(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	x := &Finding{Rule: RuleDuty, A: a, B: b}
	y := &Finding{Rule: RuleDuty, A: b, B: a}
	if x.ID() != y.ID() {
		t.Errorf("%s and %s are the same pair with two identifiers", x.ID(), y.ID())
	}
}

func TestCheckIsDeterministic(t *testing.T) {
	var forms []*Form
	for _, id := range []string{"d", "a", "c", "b"} {
		f := form(id, Obligation)
		f.DocID = decree + ":" + id
		f.ProvisionID = decree + ":" + id + ":article-1"
		forms = append(forms, f)
	}
	forms[1].Operator = Prohibition
	forms[3].Operator = Prohibition

	first := Check(forms, nil)
	// Reversed input, same answer in the same order, or two machines running
	// the same corpus disagree about what the corpus says.
	for i, j := 0, len(forms)-1; i < j; i, j = i+1, j-1 {
		forms[i], forms[j] = forms[j], forms[i]
	}
	second := Check(forms, nil)
	if len(first.Findings) != len(second.Findings) || len(first.Findings) == 0 {
		t.Fatalf("%d findings then %d", len(first.Findings), len(second.Findings))
	}
	for i := range first.Findings {
		if first.Findings[i].ID() != second.Findings[i].ID() {
			t.Errorf("finding %d is %s then %s", i, first.Findings[i].ID(), second.Findings[i].ID())
		}
	}
}

func TestReportSaysSoWhenNothingFired(t *testing.T) {
	a, b := pair(Obligation, Obligation)
	s := Check([]*Form{a, b}, nil).String()
	if !strings.Contains(s, "not about the corpus") {
		t.Errorf("an empty report claims more than it knows:\n%s", s)
	}
}

func TestMaterialsCountsWhatEachRuleNeeds(t *testing.T) {
	duty, ban := pair(Obligation, Prohibition)
	duty.Deadline = deadline(15, "kể từ ngày nhận hồ sơ")
	// A deadline fixed to a date is not material for the deadline rule, because
	// the rule compares two spans counted from one event.
	ban.Deadline = deadline(30, "")
	ban.Sanction = "phat-tien-5-trieu"
	right := form("c", Right)
	// A form the parser could not finish is in the store and is in no comparison,
	// so it is in no count either.
	half := form("d", Permission)
	half.Act = ""

	m := Materials([]*Form{duty, ban, right, half})
	if m.Obligations != 1 || m.Prohibitions != 1 || m.Rights != 1 {
		t.Errorf("operators = %+v", m)
	}
	if m.Permissions != 0 {
		t.Errorf("permissions = %d, but the only permission has no act and is compared against nothing", m.Permissions)
	}
	if m.AnchoredDeadlines != 1 {
		t.Errorf("anchored deadlines = %d, want only the one counted from an event", m.AnchoredDeadlines)
	}
	if m.Sanctions != 1 {
		t.Errorf("sanctions = %d", m.Sanctions)
	}
}

func TestMaterialSaysWhichRulesCouldNotHaveFired(t *testing.T) {
	// Zero findings is two different results. This is the one the corpus caused:
	// eight prohibitions and no sanction at all in the scope that was checked, so
	// most of the rules had nothing to fire on whatever the norms said.
	a, b := pair(Obligation, Obligation)
	s := Materials([]*Form{a, b}).String()
	for _, want := range []string{"no prohibition", "no sanction"} {
		if !strings.Contains(s, want) {
			t.Errorf("the material does not say %q:\n%s", want, s)
		}
	}

	a.Operator = Prohibition
	b.Sanction = "phat-tien-5-trieu"
	s = Materials([]*Form{a, b}).String()
	if strings.Contains(s, "no prohibition") || strings.Contains(s, "no sanction") {
		t.Errorf("a scope that holds both still claims it holds neither:\n%s", s)
	}
	if !strings.Contains(s, "1 obligations, 1 prohibitions") {
		t.Errorf("the counts are not printed:\n%s", s)
	}
}

func TestFindingStringCarriesBothQuotes(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	f := only(t, a, b, nil)
	s := f.String()
	for _, want := range []string{a.Source.Quote, b.Source.Quote, RuleDuty} {
		if !strings.Contains(s, want) {
			t.Errorf("the finding does not print %q:\n%s", want, s)
		}
	}
}
