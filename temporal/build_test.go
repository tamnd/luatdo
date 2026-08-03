package temporal

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/law"
)

func corpus() *Corpus { return NewCorpus([]*law.Document{target()}) }

// op builds a resolved operation, which is what the build stage consumes.
func op(id, kind, component, date string) Operation {
	return Operation{
		ID: id, Kind: kind, AmendingDoc: amendDoc, CausedBy: amendDoc + ":article-1",
		TargetDoc: docID, TargetComponent: component, EffectiveFrom: date,
		Instruction: "chỉ dẫn " + id,
	}
}

func versionsOf(l *Layer, component string) []Version {
	var out []Version
	for _, v := range l.Versions {
		if v.ComponentID == component {
			out = append(out, v)
		}
	}
	return out
}

func TestEnactWritesOneVersionOfEverything(t *testing.T) {
	l, _ := Build(corpus(), []Operation{op("x", KindAmend, clause2, "2022-07-01")})

	// Seven provisions plus the document component itself.
	if got := len(l.Versions); got < 8 {
		t.Fatalf("the enactment writes a version of every component and of the document, got %d", got)
	}
	root := versionsOf(l, docID)
	if len(root) == 0 {
		t.Fatal("the document itself has no version, so a point in time walk has nowhere to start")
	}
	if root[0].From != "2021-01-01" {
		t.Errorf("the first version starts when the instrument took effect, got %q", root[0].From)
	}
}

// This is the test the aggregation model exists for.
func TestAmendReusesUnchangedSiblings(t *testing.T) {
	l, _ := Build(corpus(), []Operation{
		func() Operation {
			o := op("a1", KindAmend, clause2, "2022-07-01")
			o.NewText = "2. Tự nguyện, bình đẳng, thiện chí."
			return o
		}(),
	})
	v := NewView(l)

	if got := len(versionsOf(l, clause1Sibling())); got != 1 {
		t.Errorf("clause 1 did not change and has %d versions, an unchanged sibling is reused rather than copied", got)
	}
	if got := len(versionsOf(l, clause2)); got != 2 {
		t.Fatalf("the amended clause has %d versions, want 2", got)
	}
	if got := len(versionsOf(l, article15)); got != 2 {
		t.Fatalf("the ancestor article has %d versions, want 2", got)
	}

	before := v.VersionAt(article15, "2021-06-01")
	after := v.VersionAt(article15, "2022-07-01")
	if before == nil || after == nil {
		t.Fatal("article 15 has no version on one of the two dates")
	}
	shared := 0
	for _, a := range before.Children {
		for _, b := range after.Children {
			if a == b {
				shared++
			}
		}
	}
	if shared != 1 {
		t.Errorf("the two article versions share %d child version, want exactly the one clause that did not change", shared)
	}
	if len(after.Children) != 2 {
		t.Errorf("the new article version holds %d children, want 2", len(after.Children))
	}

	text, ok := v.TextAt(clause2, "2022-07-01")
	if !ok || !strings.Contains(text, "thiện chí") {
		t.Errorf("the amended text is not in force after the amendment, got %q", text)
	}
	old, _ := v.TextAt(clause2, "2021-06-01")
	if strings.Contains(old, "thiện chí") {
		t.Errorf("the amendment reached back before its own date: %q", old)
	}
}

func clause1Sibling() string { return "vn:law:2019:45-2019-qh14:article-15:clause-1" }

// chaptered is a document shaped the way most decrees are: a chapter holding a
// section holding articles. The point of it is that the article identifiers are
// shorter than the identifiers of the containers they sit in, because an article
// is numbered against its instrument and not against the chapter.
func chaptered() *law.Document {
	const id = "vn:law:2004:113-2004-nd-cp"
	return &law.Document{
		ID: id, OfficialNumber: "113/2004/ND-CP", Title: "Nghị định thử",
		DocType: "decree", EffectiveFrom: "2004-06-01",
		Provisions: []law.Provision{
			{ID: id + ":chapter-2", Kind: "chapter", Number: "II", Text: "Chương II."},
			{ID: id + ":chapter-2:section-1", ParentID: id + ":chapter-2", Kind: "section", Number: "1", Text: "Mục 1."},
			{ID: id + ":article-9", ParentID: id + ":chapter-2:section-1", Kind: "article", Number: "9", Text: "Điều 9."},
			{ID: id + ":article-10", ParentID: id + ":chapter-2:section-1", Kind: "article", Number: "10", Text: "Điều 10."},
			{ID: id + ":article-10:clause-1", ParentID: id + ":article-10", Kind: "clause", Number: "1", Text: "Nội dung."},
		},
	}
}

// Depth used to be the number of colons in the identifier, and on a document
// like this one that put the section below the articles it contains. The
// section's version was written first, the articles under it had no version yet,
// and it came out holding fourteen empty strings. Nothing failed loudly: the
// section was simply a component nothing could be reached through, so the
// consolidated text of the chapter above it was empty and invariant 6 read the
// broken parent links as provisions coming back from the dead.
func TestAChapterHoldsTheArticlesUnderIt(t *testing.T) {
	const id = "vn:law:2004:113-2004-nd-cp"
	l, _ := Build(NewCorpus([]*law.Document{chaptered()}), []Operation{{
		ID: "a1", Kind: KindAmend, AmendingDoc: amendDoc, CausedBy: amendDoc + ":article-1",
		TargetDoc: id, TargetComponent: id + ":article-10:clause-1", EffectiveFrom: "2010-01-01",
		NewText: "1. Nội dung mới.",
	}})
	v := NewView(l)

	for _, ver := range l.Versions {
		for i, child := range ver.Children {
			if child == "" {
				t.Errorf("%s holds an empty identifier at position %d, so a child was versioned after its parent", ver.ID, i)
			}
		}
	}

	section := v.VersionAt(id+":chapter-2:section-1", "2005-01-01")
	if section == nil {
		t.Fatal("the section has no version")
	}
	if len(section.Children) != 2 {
		t.Fatalf("the section holds %d children, want the two articles under it", len(section.Children))
	}
	chapter := v.VersionAt(id+":chapter-2", "2005-01-01")
	if chapter == nil || len(chapter.Children) != 1 {
		t.Fatalf("the chapter is %+v, want one child which is the section", chapter)
	}
	// The whole point of getting the order right: the amendment at the bottom
	// has to be visible from the top.
	text, ok := v.TextAt(id+":chapter-2", "2010-06-01")
	if !ok || !strings.Contains(text, "Nội dung mới") {
		t.Errorf("the amended clause is not in the chapter's consolidated text:\n%q", text)
	}
}

func TestSupplementInsertsAfterTheAnchor(t *testing.T) {
	o := op("s1", KindSupplement, pointD, "2022-07-01")
	o.NewText = "d) Theo công việc nhất định."
	o.Anchor = &Anchor{Position: "after", Sibling: "điểm a"}
	l, _ := Build(corpus(), []Operation{o})
	v := NewView(l)

	parent := v.VersionAt(clause1, "2022-07-01")
	if parent == nil {
		t.Fatal("the clause holding the new point has no version after the insertion")
	}
	if len(parent.Children) != 3 {
		t.Fatalf("the clause holds %d points after one insertion, want 3", len(parent.Children))
	}
	var order []string
	for _, id := range parent.Children {
		order = append(order, v.Version(id).ComponentID)
	}
	want := []string{
		"vn:law:2019:45-2019-qh14:article-20:clause-1:point-a",
		pointD,
		pointC,
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("position %d is %s, want %s", i, order[i], want[i])
		}
	}
	if _, ok := v.TextAt(pointD, "2021-06-01"); ok {
		t.Error("the inserted point exists before it was inserted")
	}
}

func TestSupplementWithoutAnAnchorAppends(t *testing.T) {
	o := op("s2", KindSupplement, pointD, "2022-07-01")
	o.NewText = "d) Theo công việc nhất định."
	l, _ := Build(corpus(), []Operation{o})
	v := NewView(l)

	parent := v.VersionAt(clause1, "2022-07-01")
	last := v.Version(parent.Children[len(parent.Children)-1])
	if last.ComponentID != pointD {
		t.Errorf("with no anchor the new point goes last, it went to %s", last.ComponentID)
	}
}

func TestRepealClosesTheSubtreeAndDropsItFromItsParent(t *testing.T) {
	l, _ := Build(corpus(), []Operation{op("r1", KindRepeal, clause1, "2022-07-01")})
	v := NewView(l)

	if ver := v.VersionAt(clause1, "2022-07-01"); ver != nil {
		t.Errorf("the repealed clause is still in force as %s", ver.ID)
	}
	if ver := v.VersionAt(pointC, "2022-07-01"); ver != nil {
		t.Errorf("a point under a repealed clause is still in force as %s", ver.ID)
	}
	if ver := v.VersionAt(pointC, "2021-06-01"); ver == nil {
		t.Error("the repeal reached back before its own date")
	}
	parent := v.VersionAt("vn:law:2019:45-2019-qh14:article-20", "2022-07-01")
	if parent == nil {
		t.Fatal("the article holding the repealed clause has no version")
	}
	for _, id := range parent.Children {
		if v.Version(id).ComponentID == clause1 {
			t.Error("the article still holds the repealed clause as a child")
		}
	}
}

func TestSuspendAndResume(t *testing.T) {
	l, _ := Build(corpus(), []Operation{
		op("p1", KindSuspend, clause2, "2022-07-01"),
		op("p2", KindResume, clause2, "2023-01-01"),
	})
	v := NewView(l)

	during := v.VersionAt(clause2, "2022-09-01")
	if during == nil || during.Force != ForceSuspended {
		t.Fatalf("the clause is not suspended during the suspension: %+v", during)
	}
	if during.InForceAt("2022-09-01") {
		t.Error("a suspended clause answered as in force")
	}
	if during.Text != "Tự nguyện, bình đẳng." {
		t.Errorf("suspension changed the words: %q", during.Text)
	}
	after := v.VersionAt(clause2, "2023-06-01")
	if after == nil || after.Force != ForceInForce {
		t.Fatalf("the clause did not resume: %+v", after)
	}
}

func TestPhraseEditTouchesOnlyItsTargets(t *testing.T) {
	o := op("f1", KindAmend, clause2, "2022-07-01")
	o.Phrase = &PhraseEdit{Find: "bình đẳng", Replace: "bình đẳng, thiện chí", Targets: []string{clause2}}
	l, _ := Build(corpus(), []Operation{o})
	v := NewView(l)

	text, _ := v.TextAt(clause2, "2022-07-01")
	if !strings.Contains(text, "thiện chí") {
		t.Errorf("the substitution did not happen: %q", text)
	}
	if got := len(versionsOf(l, clause1Sibling())); got != 1 {
		t.Errorf("a phrase edit scoped to one clause moved a sibling, which has %d versions", got)
	}
}

func TestPhraseEditThatMatchesNothingIsQuarantined(t *testing.T) {
	o := op("f2", KindAmend, clause2, "2022-07-01")
	o.Phrase = &PhraseEdit{Find: "cụm từ không có ở đây", Replace: "x", Targets: []string{clause2}}
	l, _ := Build(corpus(), []Operation{o})

	if len(l.Quarantined) != 1 || l.Quarantined[0].Quarantine != QuarantinePhraseNotFound {
		t.Fatalf("a substitution that matched nowhere must be quarantined, got %+v", l.Quarantined)
	}
	if got := len(versionsOf(l, clause2)); got != 1 {
		t.Errorf("the quarantined edit changed the graph anyway, %d versions", got)
	}
}

func TestUndatedAmendmentChangesNothing(t *testing.T) {
	o := op("u1", KindAmend, clause2, "")
	o.NewText = "2. Nội dung mới."
	l, _ := Build(corpus(), []Operation{o})
	v := NewView(l)

	if got := len(versionsOf(l, clause2)); got != 1 {
		t.Errorf("an undated amendment was applied, the clause has %d versions", got)
	}
	if len(v.UndatedEvents()) != 1 {
		t.Fatalf("an undated amendment is recorded and listed, got %d", len(v.UndatedEvents()))
	}
	if len(v.EventsBetween("1900-01-01", "2100-01-01")) != 1 {
		t.Error("an undated event leaked into a dated query, which is a guessed date by another name")
	}
}

func TestAmendmentBeforeItsTargetExistsIsQuarantined(t *testing.T) {
	o := op("b1", KindAmend, clause2, "2019-01-01") // the law took effect 2021-01-01
	o.NewText = "2. Nội dung mới."
	l, _ := Build(corpus(), []Operation{o})

	if len(l.Quarantined) != 1 || l.Quarantined[0].Quarantine != QuarantineNothingToChange {
		t.Fatalf("an amendment dated before the text it changes is a reading defect, got %+v", l.Quarantined)
	}
}

func TestAmendWithNoTextIsQuarantined(t *testing.T) {
	l, _ := Build(corpus(), []Operation{op("n1", KindAmend, clause2, "2022-07-01")})
	if len(l.Quarantined) != 1 || l.Quarantined[0].Quarantine != QuarantineNoText {
		t.Fatalf("an amendment with no replacement text must be quarantined, got %+v", l.Quarantined)
	}
}

func TestQuarantinedOperationsAreCarriedThrough(t *testing.T) {
	bad := op("q1", KindAmend, "", "2022-07-01")
	bad.Quarantine = QuarantineUnresolvedTarget
	l, _ := Build(corpus(), []Operation{bad})

	if len(l.Quarantined) != 1 {
		t.Fatalf("an operation quarantined at resolution must survive the build, got %d", len(l.Quarantined))
	}
	// Nothing was enacted either. The only operation naming this document was
	// quarantined, so the document reports as unversioned rather than as
	// unamended, which are different claims.
	if len(l.Events) != 0 || len(l.Versions) != 0 {
		t.Errorf("a quarantined operation built %d events and %d versions", len(l.Events), len(l.Versions))
	}
}

func TestReplaceClosesTheWholeInstrument(t *testing.T) {
	o := op("z1", KindReplace, docID, "2025-01-01")
	l, _ := Build(corpus(), []Operation{o})
	v := NewView(l)

	if len(v.InForceAt(docID, "2025-06-01")) != 0 {
		t.Error("a replaced instrument still has components in force")
	}
	if len(v.InForceAt(docID, "2024-06-01")) == 0 {
		t.Error("the replacement reached back before its own date")
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	ops := []Operation{
		func() Operation {
			o := op("d1", KindAmend, clause2, "2022-07-01")
			o.NewText = "2. Một."
			return o
		}(),
		op("d2", KindRepeal, clause1, "2023-01-01"),
	}
	first, _ := Build(corpus(), ops)
	second, _ := Build(corpus(), ops)
	if len(first.Versions) != len(second.Versions) || len(first.Events) != len(second.Events) {
		t.Fatal("two builds of the same input produced different sizes")
	}
	for i := range first.Versions {
		if first.Versions[i].ID != second.Versions[i].ID || first.Versions[i].To != second.Versions[i].To {
			t.Fatalf("version %d differs between builds: %s and %s", i, first.Versions[i].ID, second.Versions[i].ID)
		}
	}
}

// The corpus writes 17/08/2007 and this layer compares dates as text, so the
// conversion has to happen where documents enter the layer rather than at each
// comparison. Without it the enactment lands in the wrong place on the timeline
// and every amendment after it applies to the wrong version.
func TestEnactmentUsesADateThatSorts(t *testing.T) {
	doc := target()
	doc.EffectiveFrom = "01/01/2021"
	o := op("x", KindAmend, clause2, "2022-07-01")
	o.NewText = "2. Tự nguyện, bình đẳng, thiện chí."
	l, _ := Build(NewCorpus([]*law.Document{doc}), []Operation{o})

	root := versionsOf(l, docID)
	if len(root) == 0 {
		t.Fatal("the document was not versioned")
	}
	if root[0].From != "2021-01-01" {
		t.Fatalf("the enactment starts at %q, want the date in the form the layer compares", root[0].From)
	}
	if got := versionsOf(l, clause2); len(got) != 2 {
		t.Fatalf("clause 2 has %d versions, so the amendment did not land on the enacted text", len(got))
	}
}

// A document whose date this code cannot read has no place on a timeline. An
// empty date compares as the beginning of time, which would put the text in
// force centuries before it was written.
func TestUndatedDocumentIsNotVersioned(t *testing.T) {
	doc := target()
	doc.EffectiveFrom = "ngày ký"
	l, _ := Build(NewCorpus([]*law.Document{doc}), []Operation{op("x", KindAmend, clause2, "2022-07-01")})

	if len(l.Versions) != 0 {
		t.Errorf("a document with no readable date was versioned into %d versions", len(l.Versions))
	}
	if len(l.Quarantined) != 1 || l.Quarantined[0].Quarantine != QuarantineUndatedDocument {
		t.Fatalf("the operation was not quarantined with a reason, got %+v", l.Quarantined)
	}
}

// Almost a third of the parsed corpus repeats a component identifier, because
// an amending law quotes the clauses it inserts and the parser numbers the
// quotation as structure. Versioning one of those answers "what did clause 2
// say" with the text of whichever clause 2 was parsed last.
func TestCollidingParseIsRefusedRatherThanMerged(t *testing.T) {
	doc := target()
	doc.Provisions = append(doc.Provisions, law.Provision{
		ID: clause2, ParentID: article15, Kind: "clause", Number: "2",
		Text: "2. Văn bản được dẫn lại trong luật sửa đổi.",
	})
	c := NewCorpus([]*law.Document{doc})
	if c.Colliding(docID) != 1 {
		t.Fatalf("the repeated identifier was not counted, got %d", c.Colliding(docID))
	}

	l, _ := Build(c, []Operation{op("x", KindAmend, clause2, "2022-07-01")})
	if len(l.Versions) != 0 {
		t.Errorf("a document with colliding component identifiers was versioned into %d versions", len(l.Versions))
	}
	if len(l.Quarantined) != 1 || l.Quarantined[0].Quarantine != QuarantineCollidingParse {
		t.Fatalf("the operation was not quarantined with a reason, got %+v", l.Quarantined)
	}
	// The quarantined operation keeps its instruction, so the refusal is a queue
	// of work rather than a hole in the graph.
	if l.Quarantined[0].Instruction == "" {
		t.Error("the instruction was dropped along with the operation")
	}
}

// The first real instrument this pass read is why this test exists. Both of its
// amendments state no date of their own, and before the fallback the build
// recorded two events, changed nothing, and reported itself consistent.
func TestAnAmendmentWithNoDateOfItsOwnTakesEffectWithItsInstrument(t *testing.T) {
	o := op("a1", KindAmend, clause2, "")
	o.InstrumentFrom = "2022-07-01"
	o.NewText = "2. Tự nguyện, bình đẳng, thiện chí."
	l, _ := Build(corpus(), []Operation{o})

	if got := versionsOf(l, clause2); len(got) != 2 {
		t.Fatalf("clause 2 has %d versions, the amendment was treated as undated", len(got))
	}
	v := NewView(l)
	if got, _ := v.TextAt(clause2, "2022-07-01"); !strings.Contains(got, "thiện chí") {
		t.Errorf("the new wording is not in force on the day the instrument commenced: %q", got)
	}
	if got, _ := v.TextAt(clause2, "2022-06-30"); strings.Contains(got, "thiện chí") {
		t.Errorf("the new wording is in force the day before the instrument commenced: %q", got)
	}
}

// An instrument that states no date either leaves its amendments undated, and
// undated is where they stay.
func TestAnAmendmentWithNoDateAnywhereStaysUndated(t *testing.T) {
	o := op("a1", KindAmend, clause2, "")
	o.NewText = "2. Tự nguyện, bình đẳng, thiện chí."
	if !o.Undated() {
		t.Fatal("an operation with no date anywhere reported a date")
	}
	l, _ := Build(corpus(), []Operation{o})
	if got := versionsOf(l, clause2); len(got) != 1 {
		t.Errorf("clause 2 has %d versions, an undated amendment was applied anyway", len(got))
	}
}

// Article 73 of the anti corruption law is why this test exists. The 2007
// amendment quotes the whole article, clauses and all, and before this rule the
// answer for 2008 was the 2007 wording with the 2005 clauses appended: the same
// rule stated twice, in two forms, in one answer.
func TestAmendingAComponentReplacesItsSubtree(t *testing.T) {
	o := op("a1", KindAmend, article15, "2022-07-01")
	o.NewText = "Điều 15. Nguyên tắc giao kết\n2. Thiện chí."
	l, _ := Build(corpus(), []Operation{o})
	v := NewView(l)

	// Clause 1 is in the article the amendment replaces and not in the
	// quotation, so it is gone from that day on.
	if ver := v.VersionAt(clause1of15, "2022-07-01"); ver != nil {
		t.Errorf("a clause the replacement leaves out is still in force as %s", ver.ID)
	}
	text, _ := v.TextAt(article15, "2022-07-01")
	if strings.Contains(text, "Giao kết trung thực") {
		t.Errorf("the replaced article still carries its old clauses:\n%s", text)
	}
	if !strings.Contains(text, "Thiện chí") {
		t.Errorf("the new wording is not there:\n%s", text)
	}
	// The old clauses existed and are still answerable before the amendment.
	if ver := v.VersionAt(clause2, "2021-06-01"); ver == nil {
		t.Error("the replacement reached back before its own date")
	}
}

// The quotation an amending instrument carries is a component and not a
// paragraph. Stored as a paragraph, "what does clause 2 of article 15 say
// today" is answered by nothing at all, and against the published consolidated
// text of Nghị định số 72/2025/NĐ-CP that was six components the graph did not
// have and one it had that the drafter did not.
func TestAReplacementIsParsedBackIntoComponents(t *testing.T) {
	o := op("a1", KindAmend, article15, "2022-07-01")
	o.NewText = "Điều 15. Nguyên tắc giao kết\n1. Tự nguyện.\n2. Thiện chí và hợp tác."
	l, _ := Build(corpus(), []Operation{o})
	v := NewView(l)

	got, ok := v.TextAt(clause2, "2022-07-01")
	if !ok {
		t.Fatal("clause 2 of the replaced article is not answerable at all")
	}
	if got != "Thiện chí và hợp tác." {
		t.Errorf("clause 2 says %q, want the wording the quotation states for it", got)
	}
	if first, _ := v.TextAt(clause1of15, "2022-07-01"); first != "Tự nguyện." {
		t.Errorf("clause 1 says %q, want the wording the quotation states for it", first)
	}
	// The article itself holds no text of its own, the way an article the
	// parser read holds none.
	if ver := v.VersionAt(article15, "2022-07-01"); ver == nil || strings.TrimSpace(ver.Text) != "" {
		t.Errorf("the replaced article kept the whole quotation as its own text: %+v", ver)
	}
}

func TestAReplacementWithNoStructureKeepsItsWordingWithoutTheMarker(t *testing.T) {
	o := op("a1", KindAmend, clause2, "2022-07-01")
	o.NewText = "2. Tự nguyện, bình đẳng, thiện chí."
	l, _ := Build(corpus(), []Operation{o})
	got, _ := NewView(l).TextAt(clause2, "2022-07-01")
	if got != "Tự nguyện, bình đẳng, thiện chí." {
		t.Errorf("clause 2 says %q, want the wording without the number the parser never keeps", got)
	}
}

// Two amendments on one day gave the ancestors they share a version that
// started and ended on 2007-08-17. Under half open intervals that version was
// never in force for a single day, and it came back as the answer to "which
// norms were in force for less than a year before being amended".
func TestTwoChangesOnOneDayLeaveOneVersion(t *testing.T) {
	a := op("a1", KindAmend, clause2, "2022-07-01")
	a.NewText = "2. Tự nguyện, bình đẳng, thiện chí."
	b := op("a2", KindAmend, clause1Sibling(), "2022-07-01")
	b.AmendingDoc = "vn:law:2022:11-2022-qh15"
	b.NewText = "1. Giao kết trung thực và tự nguyện."
	l, _ := Build(corpus(), []Operation{a, b})

	for _, id := range []string{article15, docID} {
		for _, v := range versionsOf(l, id) {
			if v.From != "" && v.From == v.To {
				t.Errorf("%s has a version that starts and ends on %s, so it was never in force", id, v.From)
			}
		}
		if got := len(versionsOf(l, id)); got != 2 {
			t.Errorf("%s has %d versions after two changes on one day, want 2", id, got)
		}
	}
	// Both amendments are still in the answer.
	v := NewView(l)
	after := v.VersionAt(article15, "2022-07-01")
	if after == nil || len(after.Children) != 2 {
		t.Fatalf("the article version after the two amendments is %+v", after)
	}
	for _, child := range after.Children {
		text := v.Version(child).Text
		if !strings.Contains(text, "thiện chí") && !strings.Contains(text, "tự nguyện") {
			t.Errorf("one of the two amendments was lost: %q", text)
		}
	}
}
