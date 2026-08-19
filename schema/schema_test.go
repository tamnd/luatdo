package schema

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/eval"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
)

// answers is a Completer that reads from a script, so a pass test is about the
// pass rather than about a network.
type answers struct {
	replies []string
	err     error
	inputs  []string
	calls   int
}

func (a *answers) Complete(_ context.Context, req api.Request) (api.Response, error) {
	a.inputs = append(a.inputs, req.Input)
	a.calls++
	if a.err != nil {
		return api.Response{}, a.err
	}
	i := min(a.calls-1, len(a.replies)-1)
	return api.Response{
		Text:  a.replies[i],
		Usage: api.Usage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110},
	}, nil
}

const provision = "Người sử dụng lao động phải trả lương đầy đủ cho người lao động trong thời hạn 05 ngày làm việc."

func duty() *norm.Statement {
	return &norm.Statement{
		Type:       "duty",
		Bearer:     &norm.Ref{Text: "người sử dụng lao động", ClassID: "vn-legal:Employer", IsActor: true},
		Action:     norm.Ref{Text: "trả lương đầy đủ"},
		Evidence:   norm.Evidence{Quote: "phải trả lương đầy đủ"},
		Confidence: 0.9,
	}
}

func item(id string, s *norm.Statement) Item {
	return Item{RecordID: id, ProvisionID: "vn:prov:" + id, DocID: "vn:doc:1", Statement: s, Text: provision}
}

func TestCountInvariantsSeparatesRecordsFromBreaks(t *testing.T) {
	reg := ontology.Seed()
	twice := duty()
	twice.Conditions = []norm.Clause{
		{Kind: "vibes", Text: "x", Quote: "trong thời hạn 05 ngày làm việc"},
		{Kind: "vibes", Text: "y", Quote: "trong thời hạn 05 ngày làm việc"},
	}
	noBearer := duty()
	noBearer.Bearer = nil
	inv := CountInvariants(reg, []Item{item("a", duty()), item("b", twice), item("c", noBearer)})

	if inv.Records != 3 || inv.Broken != 2 {
		t.Fatalf("counted %d records and %d broken, want 3 and 2", inv.Records, inv.Broken)
	}
	for _, f := range inv.Firings {
		switch f.Code {
		case norm.ViolationConditionKind:
			if f.Records != 1 || f.Breaks != 2 {
				t.Errorf("one record breaking a condition kind twice is %d records and %d breaks, want 1 and 2",
					f.Records, f.Breaks)
			}
		case norm.ViolationBearerMissing:
			if f.Records != 1 || !f.Mandatory {
				t.Errorf("a missing bearer is %d records, mandatory %v, want 1 and true", f.Records, f.Mandatory)
			}
		}
	}
	if got := inv.MandatoryShare(); got.Right != 1 || got.Of != 2 {
		t.Errorf("mandatory share is %s, want one of the two broken records", got)
	}
}

// An invariant nobody broke has to appear in the result, because a report that
// lists only what fired cannot tell a check that never fires from a defect the
// corpus does not have.
func TestSilentInvariantsAreStillReported(t *testing.T) {
	inv := CountInvariants(ontology.Seed(), []Item{item("a", duty())})
	if len(inv.Firings) != len(norm.Codes) {
		t.Fatalf("%d firings for %d codes", len(inv.Firings), len(norm.Codes))
	}
	if len(inv.Silent()) != len(norm.Codes) {
		t.Errorf("%d silent invariants over a clean corpus, want all %d", len(inv.Silent()), len(norm.Codes))
	}
	if !strings.Contains(inv.String(), "never fired") {
		t.Error("the report does not say which invariants never fired")
	}
}

func TestBlindspotsCountAnUnplacedReferenceAndFoldItsVariants(t *testing.T) {
	reg := ontology.Seed()
	var items []Item
	for i, text := range []string{"Thanh tra viên", "thanh tra viên", "THANH TRA VIÊN"} {
		s := duty()
		s.Counterparty = &norm.Ref{Text: text}
		items = append(items, item(string(rune('a'+i)), s))
	}
	b := FindBlindspots(reg, items, nil)
	// Six unplaced: the three counterparties and the three actions, because the
	// registry has no class for an action and the fixture does not pretend it
	// does.
	if b.Unplaced != 6 || b.Placed != 3 {
		t.Fatalf("%d unplaced and %d placed references, want 6 and the three bearers", b.Unplaced, b.Placed)
	}
	if len(b.Wanted) != 2 || b.Wanted[0].Count != 3 {
		t.Fatalf("wanted is %+v, want the inspector folded to one form asked for three times", b.Wanted)
	}
	if b.Wanted[0].Slug != "thanh-tra-vien" {
		t.Errorf("the most asked for form is %q, want the three case variants folded together", b.Wanted[0].Slug)
	}
	// Both forms cross the threshold, the inspector and the action every fixture
	// statement shares, and only the inspector is recommended: no decision about
	// the class list would place a verb.
	if got := b.Recurring(); len(got) != 1 || got[0].Slug != "thanh-tra-vien" {
		t.Errorf("recurring is %+v, want the party alone", got)
	}
	if b.ByRole[RoleAction] != 3 {
		t.Errorf("the action count is %d, want the three actions counted rather than dropped", b.ByRole[RoleAction])
	}
	if len(b.Used) == 0 || len(b.Unused) == 0 {
		t.Error("a registry of 43 classes used by one bearer has both used and unused classes")
	}
}

// A form asked for once is a drafting particular, and the report has to keep it
// out of the recurring list without dropping it from the count.
func TestOneOffAsksAreCountedAndNotRecommended(t *testing.T) {
	s := duty()
	s.Object = &norm.Ref{Text: "biên bản vi phạm"}
	b := FindBlindspots(ontology.Seed(), []Item{item("a", s)}, nil)
	if b.Unplaced != 2 || len(b.Wanted) != 2 {
		t.Fatalf("unplaced %d, wanted %+v, want the object and the action", b.Unplaced, b.Wanted)
	}
	if len(b.Recurring()) != 0 {
		t.Error("a form asked for once is recurring")
	}
}

// Predicates are reported without a usage count because no pass emits one, and
// the report has to say so rather than print a zero.
func TestPredicateUsageIsNotMeasuredRatherThanZero(t *testing.T) {
	b := FindBlindspots(ontology.Seed(), []Item{item("a", duty())}, nil)
	if b.Predicates == 0 {
		t.Fatal("the seed registry declares predicates")
	}
	if !strings.Contains(b.String(), "not measured rather than zero") {
		t.Error("the report prints a predicate usage figure nothing produced")
	}
}

func terms() ([]Term, []Term) {
	children := []Term{
		{ID: "thue-thu-nhap", Label: "thuế thu nhập", Parent: "thue"},
		{ID: "hop-dong-lao-dong", Label: "hợp đồng lao động", Parent: "lao-dong"},
	}
	parents := []Term{{ID: "thue", Label: "thuế"}, {ID: "lao-dong", Label: "lao động"}}
	return children, parents
}

func TestBottomUpPlacesEachChildAndScoresAgainstTheHandWrittenParent(t *testing.T) {
	children, parents := terms()
	c := &answers{replies: []string{
		`{"parent":"thue","confidence":0.9}`,
		`{"parent":"thue","confidence":0.4}`,
	}}
	in := &Inducer{Completer: c, Model: "m"}
	got, err := in.InduceBottomUp(context.Background(), children, parents, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := ScoreTaxonomy(got, children)
	if s.Placed.Right != 2 || s.Correct.Right != 1 {
		t.Fatalf("placed %s and correct %s, want both placed and one right", s.Placed, s.Correct)
	}
	if len(s.Mistakes) != 1 || s.Mistakes[0].Want != "lao-dong" {
		t.Errorf("mistakes are %+v, want the second child under its real parent", s.Mistakes)
	}
}

// None is a permitted answer, and a pass that never uses it would report an
// accuracy over the list rather than over the model.
func TestAnUnplaceableChildIsLeftUnplacedRatherThanGuessed(t *testing.T) {
	children, parents := terms()
	c := &answers{replies: []string{`{"parent":"none","rationale":"không thuộc nhóm nào"}`}}
	in := &Inducer{Completer: c, Model: "m"}
	got, _, err := in.Place(context.Background(), children[0], parents)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentID != "" {
		t.Errorf("placed under %q after the model declined", got.ParentID)
	}
}

// A parent the model invented is not in the taxonomy, and the correction round
// is what keeps it out.
func TestAnInventedParentIsSentBackAndThenDropped(t *testing.T) {
	children, parents := terms()
	c := &answers{replies: []string{`{"parent":"vu-tru"}`}}
	in := &Inducer{Completer: c, Model: "m", MaxCorrections: 1}
	got, _, err := in.Place(context.Background(), children[0], parents)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentID != "" {
		t.Errorf("kept the invented parent %q", got.ParentID)
	}
	if c.calls != 2 {
		t.Errorf("%d calls, want the first and one correction", c.calls)
	}
	if !strings.Contains(c.inputs[1], "không nằm trong danh sách") {
		t.Error("the correction does not tell the model what was wrong")
	}
}

func TestTopDownLeavesAContestedChildUnlinkedAndSaysSo(t *testing.T) {
	children, parents := terms()
	c := &answers{replies: []string{
		`{"children":["thue-thu-nhap","hop-dong-lao-dong"]}`,
		`{"children":["hop-dong-lao-dong"]}`,
	}}
	in := &Inducer{Completer: c, Model: "m"}
	got, err := in.InduceTopDown(context.Background(), children, parents, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Links) != 1 || got.Links["thue-thu-nhap"] != "thue" {
		t.Fatalf("links are %v, want only the uncontested child linked", got.Links)
	}
	s := ScoreTaxonomy(got, children)
	if len(s.Contested) != 1 || s.Contested[0] != "hop-dong-lao-dong" {
		t.Errorf("contested is %v, want the child two parents claimed", s.Contested)
	}
	if s.Structure.MultiParent != 1 {
		t.Errorf("structure reports %d multi parent children, want 1", s.Structure.MultiParent)
	}
	if s.Placed.Right != 1 || s.Overall.Right != 1 {
		t.Errorf("placed %s overall %s, want one child placed and right", s.Placed, s.Overall)
	}
}

// A child no parent claimed is an orphan, and the top down direction is the
// only one that can produce them, which is why the two are scored apart.
func TestATopDownOrphanIsReportedRatherThanDropped(t *testing.T) {
	children, parents := terms()
	c := &answers{replies: []string{`{"children":[]}`}}
	in := &Inducer{Completer: c, Model: "m"}
	got, err := in.InduceTopDown(context.Background(), children, parents, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := ScoreTaxonomy(got, children)
	if s.Structure.Orphans != 2 || len(s.Unplaced) != 2 {
		t.Errorf("orphans %d and unplaced %v, want both children", s.Structure.Orphans, s.Unplaced)
	}
	if s.Correct.Of != 0 {
		t.Error("a direction that placed nothing has no accuracy over placed terms")
	}
}

// A cycle is a structural defect a per child accuracy cannot see: every link
// can be defensible on its own and the graph still not be a taxonomy.
func TestCheckStructureFindsACycle(t *testing.T) {
	children := []Term{{ID: "a"}, {ID: "b"}}
	in := &Induced{Direction: BottomUp,
		Links:  map[string]string{"a": "b", "b": "a"},
		Claims: map[string][]string{"a": {"b"}, "b": {"a"}}}
	c := CheckStructure(in, children)
	if len(c.Cycles) == 0 {
		t.Fatal("two terms under each other is a cycle")
	}
	if c.Orphans != 0 {
		t.Errorf("%d orphans, want none", c.Orphans)
	}
}

func TestAgreementCountsOnlyTheChildrenBothDirectionsPlaced(t *testing.T) {
	children, _ := terms()
	a := &Induced{Links: map[string]string{"thue-thu-nhap": "thue", "hop-dong-lao-dong": "lao-dong"}}
	b := &Induced{Links: map[string]string{"thue-thu-nhap": "lao-dong"}}
	got := Agreement(a, b, children)
	if got.Of != 1 || got.Right != 0 {
		t.Errorf("agreement is %s, want one comparable child and no agreement", got)
	}
}

func candidates() []ontology.Candidate {
	return []ontology.Candidate{
		{Kind: "class", Label: "Thanh tra viên", Provision: "p1", Quote: "thanh tra viên lập biên bản"},
		{Kind: "class", Label: "thanh tra viên", Provision: "p2", Quote: "thanh tra viên yêu cầu"},
		{Kind: "class", Label: "biên bản", Provision: "p1", Quote: "lập biên bản"},
		{Kind: "predicate", Label: "lập", Provision: "p1"},
	}
}

func TestFoldProposalsGroupsVariantsAndCountsDocuments(t *testing.T) {
	docOf := map[string]string{"p1": "d1", "p2": "d2"}
	got := FoldProposals(candidates(), func(p string) string { return docOf[p] })
	if len(got) != 2 {
		t.Fatalf("%d proposals, want the two classes folded", len(got))
	}
	if got[0].Slug == "" || got[0].Count != 2 || got[0].Docs != 2 {
		t.Errorf("first proposal is %+v, want the inspector counted twice across two documents", got[0])
	}
	if len(got[0].Quotes) != 2 {
		t.Errorf("%d quotes kept, want both", len(got[0].Quotes))
	}
}

func TestShortlistRanksByDefinitionAndNotOnlyByLabel(t *testing.T) {
	reg := ontology.Seed()
	defs := map[string]string{"vn-legal:Employer": "bên thuê mướn và trả lương cho người làm việc"}
	got := Shortlist(reg, defs, "tổ chức trả lương cho người làm việc", ShortlistSize)
	if !containsClass(got, "vn-legal:Employer") {
		t.Errorf("shortlist %v misses the class whose definition shares the words", ids(got))
	}
}

func ids(cs []ontology.Class) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func TestCanonicalizeKeepsTheNearestClassAndTheReasonItWasRejected(t *testing.T) {
	reg := ontology.Seed()
	c := &answers{replies: []string{
		`{"match":"none","nearest":"vn-legal:Employer","reason":"lớp này là bên trả lương, khái niệm mới là bên kiểm tra"}`,
	}}
	d := &Definer{Completer: c, Model: "m"}
	p := Proposal{Slug: "thanh-tra-vien", Label: "thanh tra viên", Definition: "người kiểm tra việc chấp hành pháp luật"}
	short := []ontology.Class{*reg.Class("vn-legal:Employer")}
	m, _, err := d.Canonicalize(context.Background(), p, short, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.ClassID != "" {
		t.Errorf("matched %q after the model said none", m.ClassID)
	}
	if m.Nearest != "vn-legal:Employer" || m.Reason == "" {
		t.Errorf("match is %+v, want the nearest class and the reason it lost", m)
	}
}

// An empty shortlist is not a model saying the proposal is new, and recording
// it as one would credit a decision nobody made.
func TestAnEmptyShortlistCostsNoCallAndSaysWhy(t *testing.T) {
	c := &answers{replies: []string{`{"match":"vn-legal:Employer"}`}}
	d := &Definer{Completer: c, Model: "m"}
	m, _, err := d.Canonicalize(context.Background(), Proposal{Slug: "x"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.calls != 0 {
		t.Errorf("%d calls for a proposal with nothing to compare against", c.calls)
	}
	if m.ClassID != "" || !strings.Contains(m.Reason, "no registry class") {
		t.Errorf("match is %+v, want a shortlist miss", m)
	}
}

func TestQueueCarriesTheCaseForAProposal(t *testing.T) {
	ps := []Proposal{
		{Slug: "a", Label: "thanh tra viên", Definition: "người kiểm tra", Count: 7, Docs: 3,
			Provisions: []string{"p1"}, Quotes: []string{"q1", "q2"}},
		{Slug: "b", Label: "người sử dụng lao động"},
	}
	matches := map[string]Match{
		"a": {Slug: "a", Nearest: "vn-legal:Employer", Reason: "khác vai"},
		"b": {Slug: "b", ClassID: "vn-legal:Employer"},
	}
	got := Queue(ps, matches, "2026-01-01T00:00:00Z")
	if len(got) != 1 {
		t.Fatalf("%d queued, want only the unmatched proposal", len(got))
	}
	q := got[0]
	if q.Definition == "" || q.Count != 7 || q.Docs != 3 || len(q.Quotes) != 2 || q.Nearest == "" || q.Rejected == "" {
		t.Errorf("queued row is %+v, want the definition, counts, quotes and the rejected nearest", q)
	}
	if q.Status != "proposed" || q.Source != "define" {
		t.Errorf("queued row is %s from %s, want a proposed row from the define pass", q.Status, q.Source)
	}
}

func TestRepairClearsABreakAndNamesWhatItChanged(t *testing.T) {
	reg := ontology.Seed()
	broken := duty()
	broken.Bearer = nil
	fixed := `{"repaired":{"statement_type":"duty",
		"bearer":{"text":"người sử dụng lao động","class_id":"vn-legal:Employer","is_actor":true},
		"action":{"text":"trả lương đầy đủ"},
		"evidence":{"quote":"phải trả lương đầy đủ"},"confidence":0.9},"reason":"điều khoản nêu rõ bên phải trả"}`
	c := &answers{replies: []string{fixed}}
	r := &Repairer{Completer: c, Model: "m"}
	got, err := r.Fix(context.Background(), item("a", broken), reg)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Valid || len(got.After) != 0 {
		t.Fatalf("repair left %v", got.After)
	}
	if len(got.Before) != 1 || got.Before[0] != norm.ViolationBearerMissing {
		t.Errorf("before is %v, want the missing bearer", got.Before)
	}
	if len(Drift(got)) != 0 {
		t.Errorf("a repair that only filled the bearer drifted into %v", Drift(got))
	}
}

// A repair that satisfies the checker by rewriting fields nobody asked about is
// the failure this milestone is about, and it has to be visible.
func TestARepairThatRewritesAnUnaskedFieldIsCountedAsDrift(t *testing.T) {
	reg := ontology.Seed()
	broken := duty()
	broken.Bearer = nil
	wide := `{"repaired":{"statement_type":"right",
		"bearer":{"text":"người sử dụng lao động","class_id":"vn-legal:Employer","is_actor":true},
		"action":{"text":"nhận lương"},
		"evidence":{"quote":"phải trả lương đầy đủ"},"confidence":0.9}}`
	c := &answers{replies: []string{wide}}
	r := &Repairer{Completer: c, Model: "m"}
	got, err := r.Fix(context.Background(), item("a", broken), reg)
	if err != nil {
		t.Fatal(err)
	}
	drift := Drift(got)
	if len(drift) != 2 {
		t.Fatalf("drift is %v, want the type and the action", drift)
	}
	s := ScoreRepairs([]Repair{got}, eval.Accuracy{})
	if s.Drifted != 1 {
		t.Errorf("%d drifted repairs, want 1", s.Drifted)
	}
	if !strings.Contains(s.String(), "not measured rather than perfect") {
		t.Error("an unjudged run reports grounding as if it had been measured")
	}
}

// Declining is the right answer when the provision does not carry the missing
// part, so it is recorded as its own outcome rather than as a failed repair.
func TestADeclinedRepairIsRecordedAsDeclined(t *testing.T) {
	reg := ontology.Seed()
	broken := duty()
	broken.Bearer = nil
	c := &answers{replies: []string{`{"repaired":null,"reason":"điều khoản không nêu bên nào"}`}}
	r := &Repairer{Completer: c, Model: "m"}
	got, err := r.Fix(context.Background(), item("a", broken), reg)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Declined || got.Valid {
		t.Fatalf("repair is %+v, want a decline", got)
	}
	if got.Rounds != 1 {
		t.Errorf("%d rounds after a decline, want the loop to stop", got.Rounds)
	}
	if s := ScoreRepairs([]Repair{got}, eval.Accuracy{}); s.Declined != 1 || s.Fixed.Right != 0 {
		t.Errorf("score is %+v, want one decline and nothing fixed", s)
	}
}

// A repair that trades one break for another is not progress, and the two code
// lists are what say so.
func TestARepairThatIntroducesANewBreakIsReported(t *testing.T) {
	reg := ontology.Seed()
	broken := duty()
	broken.Bearer = nil
	swap := `{"repaired":{"statement_type":"duty",
		"bearer":{"text":"người sử dụng lao động","class_id":"vn-legal:Employer","is_actor":true},
		"action":{"text":"trả lương"},
		"evidence":{"quote":"phải trả lương thật cao"},"confidence":0.9}}`
	c := &answers{replies: []string{swap}}
	r := &Repairer{Completer: c, Model: "m", MaxRounds: 1}
	got, err := r.Fix(context.Background(), item("a", broken), reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Introduced) != 1 || got.Introduced[0] != norm.ViolationEvidenceQuote {
		t.Fatalf("introduced is %v, want the quote that is no longer verbatim", got.Introduced)
	}
	s := ScoreRepairs([]Repair{got}, eval.Accuracy{})
	if s.Introduced != 1 || s.Improved.Right != 0 {
		t.Errorf("score is %+v, want a trade rather than an improvement", s)
	}
}

func TestByCodeRepairRatesAreKeptApart(t *testing.T) {
	reps := []Repair{
		{Before: []string{norm.ViolationBearerMissing}, After: nil},
		{Before: []string{norm.ViolationBearerMissing}, After: []string{norm.ViolationBearerMissing}},
		{Before: []string{norm.ViolationConfidence}, After: nil},
	}
	s := ScoreRepairs(reps, eval.Accuracy{})
	for _, c := range s.ByCode {
		switch c.Code {
		case norm.ViolationBearerMissing:
			if c.Cleared.Right != 1 || c.Cleared.Of != 2 || !c.Mandatory {
				t.Errorf("bearer repairs are %s, want one of two on a mandatory code", c.Cleared)
			}
		case norm.ViolationConfidence:
			if c.Cleared.Right != 1 || c.Cleared.Of != 1 {
				t.Errorf("confidence repairs are %s, want one of one", c.Cleared)
			}
		}
	}
}

func TestAModelErrorStopsTheLoopAndKeepsTheOriginalBreaks(t *testing.T) {
	reg := ontology.Seed()
	broken := duty()
	broken.Bearer = nil
	c := &answers{err: errors.New("no route")}
	r := &Repairer{Completer: c, Model: "m"}
	got, err := r.Fix(context.Background(), item("a", broken), reg)
	if err == nil {
		t.Fatal("a failed call was reported as a repair")
	}
	if got.Valid || len(got.After) != 1 {
		t.Errorf("repair is %+v, want the record still broken", got)
	}
}

func TestJudgeReadsTheRepairRatherThanTheChecker(t *testing.T) {
	c := &answers{replies: []string{`{"grounded":"no","reason":"điều khoản không nêu thời hạn này"}`}}
	r := &Repairer{Completer: c, Model: "m"}
	rep := Repair{Statement: duty(), Changed: []string{"deadline"}}
	ok, reason, _, err := r.Judge(context.Background(), item("a", duty()), rep)
	if err != nil {
		t.Fatal(err)
	}
	if ok || reason == "" {
		t.Errorf("judge said %v with reason %q, want a refusal with its reason", ok, reason)
	}
	if !strings.Contains(c.inputs[0], "deadline") {
		t.Error("the judge was not told which fields changed")
	}
}

func TestDefineRegistryOnlyPaysForClassesWithNoDefinition(t *testing.T) {
	reg := &ontology.Registry{Version: 1, Classes: []ontology.Class{
		{ID: "a", LabelVI: "người lao động"},
		{ID: "b", LabelVI: "hợp đồng", DefinitionVI: "thoả thuận giữa hai bên"},
	}}
	c := &answers{replies: []string{`{"definition":"người làm việc theo thoả thuận và nhận lương"}`}}
	d := &Definer{Completer: c, Model: "m"}
	got, _, err := d.DefineRegistry(context.Background(), reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.calls != 1 {
		t.Errorf("%d calls for a registry with one undefined class", c.calls)
	}
	if got["b"] != "thoả thuận giữa hai bên" {
		t.Errorf("the existing definition became %q", got["b"])
	}
	if got["a"] == "" {
		t.Error("the undefined class came back with no definition")
	}
}

// The define step has to survive a model that answers with prose, because it
// will, and a pass that drops the proposal on the first bad reply loses the
// definition rather than the reply.
func TestDefineRetriesOnAReplyThatIsNotJSON(t *testing.T) {
	c := &answers{replies: []string{"đây là một khái niệm về lao động", `{"definition":"người làm việc"}`}}
	d := &Definer{Completer: c, Model: "m", MaxCorrections: 1}
	got, _, err := d.Define(context.Background(), "người lao động", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "người làm việc" {
		t.Errorf("definition is %q", got)
	}
	if c.calls != 2 {
		t.Errorf("%d calls, want the first and one correction", c.calls)
	}
}

// The round trip is the only part of canonicalization with real gold, and its
// two halves have to be counted apart: a pass that always answers with a class
// scores everything on the first half and nothing on the second.
func TestRoundTripScoresTheHeldAndWithheldHalvesApart(t *testing.T) {
	reg := &ontology.Registry{Version: 1, Classes: []ontology.Class{
		{ID: "a", LabelVI: "người lao động", DefinitionVI: "người làm việc theo hợp đồng và nhận lương"},
	}}
	// Two replies, one per half: the class is found when it is there and the
	// pass declines when it is not.
	c := &answers{replies: []string{`{"match":"a"}`, `{"match":"none","reason":"không lớp nào trùng"}`}}
	d := &Definer{Completer: c, Model: "m"}
	s, err := d.RoundTrip(context.Background(), reg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Held.Right != 1 || s.Held.Of != 1 {
		t.Errorf("held half is %s, want the class matched back to itself", s.Held)
	}
	if s.Withheld.Right != 1 || s.Withheld.Of != 1 {
		t.Errorf("withheld half is %s, want a decline", s.Withheld)
	}
	if s.InShort.Right != 1 {
		t.Errorf("the class was not in its own lexical shortlist, %s", s.InShort)
	}
	if s.Calls != 2 {
		t.Errorf("%d calls, want one per half", s.Calls)
	}
}

// A class with no definition anywhere is skipped rather than probed with its
// label, because probing with the label would measure string matching, which is
// the thing the define pass exists to replace.
func TestRoundTripSkipsAClassWithNoDefinition(t *testing.T) {
	reg := &ontology.Registry{Version: 1, Classes: []ontology.Class{{ID: "a", LabelVI: "người lao động"}}}
	c := &answers{replies: []string{`{"match":"a"}`}}
	d := &Definer{Completer: c, Model: "m"}
	s, err := d.RoundTrip(context.Background(), reg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Calls != 0 || s.Held.Of != 0 {
		t.Errorf("score is %+v, want nothing probed", s)
	}
}

func edges() []ParentEdge {
	return []ParentEdge{
		{ChildID: "c1", ChildLabel: "hợp đồng lao động", ParentID: "p1", ParentLabel: "hợp đồng", Support: 4},
		{ChildID: "c1", ChildLabel: "hợp đồng lao động", ParentID: "p2", ParentLabel: "quan hệ lao động", Support: 1},
		{ChildID: "c2", ChildLabel: "tiền lương", ParentID: "p1", ParentLabel: "hợp đồng", Support: 2},
	}
}

func TestFindParentConflictsOnlyReportsAChildWithTwoParents(t *testing.T) {
	got := FindParentConflicts(edges())
	if len(got) != 1 || got[0].ChildID != "c1" {
		t.Fatalf("conflicts are %+v, want the one child under two parents", got)
	}
	if got[0].Parents[0].ParentID != "p1" {
		t.Errorf("parents are not ordered by support, %+v", got[0].Parents)
	}
}

// The resolver earns its calls where it disagrees with the support count, so
// the disagreement has to be recorded rather than smoothed over.
func TestResolveParentsRecordsWhenItGoesAgainstTheSupportCount(t *testing.T) {
	c := &answers{replies: []string{`{"parent":"p2","rationale":"đây là một quan hệ, không phải một loại hợp đồng"}`}}
	in := &Inducer{Completer: c, Model: "m"}
	cs := FindParentConflicts(edges())
	rs, _, err := in.ResolveParents(context.Background(), cs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Kept != "p2" {
		t.Fatalf("resolutions are %+v", rs)
	}
	if rs[0].TopByFar {
		t.Error("the resolver kept the parent with less support and the report says it agreed with the count")
	}
	if len(rs[0].Dropped) != 1 || rs[0].Dropped[0] != "p1" {
		t.Errorf("dropped is %v, want the parent that lost", rs[0].Dropped)
	}
	rep := Report("relation layer", 3, 3, cs, rs, api.Usage{})
	if rep.Resolved.Right != 1 || rep.AgreedCount != 0 {
		t.Errorf("report is %+v, want one resolved against the count", rep)
	}
}

// A graph with no hierarchy is not a graph with no conflicts, and the report
// has to say which one it is looking at.
func TestAGraphWithNoBroaderEdgeReportsThatTheResolverDidNotRun(t *testing.T) {
	rep := Report("relation layer", 3, 0, nil, nil, api.Usage{})
	if !strings.Contains(rep.String(), "stored hierarchy offered nothing to resolve") {
		t.Errorf("report reads as a clean bill of health:\n%s", rep)
	}
}

// Conflicts can come from the stored graph or from the induction pass, and a
// resolution over induced claims is not a statement about the edges on disk.
func TestTheReportNamesWhereTheConflictsCameFrom(t *testing.T) {
	cs := []ParentConflict{{ChildID: "c1", Parents: []ParentEdge{
		{ChildID: "c1", ParentID: "p1", Support: 1},
		{ChildID: "c1", ParentID: "p2", Support: 1},
	}}}
	rs := []Resolution{{ChildID: "c1", Kept: "p1", Dropped: []string{"p2"}}}
	rep := Report("induced taxonomy", 3, 0, cs, rs, api.Usage{})
	got := rep.String()
	if !strings.Contains(got, "came from the induced taxonomy") {
		t.Errorf("report does not say the conflicts were induced:\n%s", got)
	}
	if !strings.Contains(got, "stored hierarchy offered nothing to resolve") {
		t.Errorf("report hides that the stored graph had no hierarchy:\n%s", got)
	}
}
