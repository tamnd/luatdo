package retrieve

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/subject"
	"github.com/tamnd/luatdo/temporal"
	"github.com/tamnd/luatdo/term"
)

const (
	labour = "vn:law:2019:45-2019-qh14"
	tax    = "vn:law:2019:38-2019-qh14"
)

func labourDoc() *law.Document {
	return &law.Document{
		ID: labour, Title: "Bộ luật Lao động",
		Provisions: []law.Provision{
			{ID: labour + ":article-3", Kind: "article", Number: "3", Heading: "Giải thích từ ngữ"},
			{ID: labour + ":article-3:clause-1", ParentID: labour + ":article-3", Kind: "clause", Number: "1",
				Text: "Người lao động là người làm việc cho người sử dụng lao động theo thỏa thuận."},
			{ID: labour + ":article-94", Kind: "article", Number: "94", Heading: "Nguyên tắc trả lương",
				Text: "Người sử dụng lao động phải bảo đảm các nguyên tắc sau đây:"},
			{ID: labour + ":article-94:clause-2", ParentID: labour + ":article-94", Kind: "clause", Number: "2",
				Text: "Trường hợp trả lương chậm thì phải đền bù trong thời hạn 15 ngày."},
		},
	}
}

func taxDoc() *law.Document {
	return &law.Document{
		ID: tax, Title: "Luật Quản lý thuế",
		Provisions: []law.Provision{
			{ID: tax + ":article-17", Kind: "article", Number: "17", Heading: "Trách nhiệm của người nộp thuế"},
			{ID: tax + ":article-17:clause-1", ParentID: tax + ":article-17", Kind: "clause", Number: "1",
				Text: "Thực hiện đăng ký thuế theo quy định của Luật này và nộp hồ sơ trong thời hạn 15 ngày."},
		},
	}
}

func testIndex() *Index {
	return Build(Input{
		Docs: []*law.Document{labourDoc(), taxDoc()},
		Records: []norm.Record{
			{
				ID: "vn:norm:aaa", DocID: labour, ProvisionID: labour + ":article-94:clause-2",
				Status: norm.StatusVerified,
				Statement: norm.Statement{
					Type:     "duty",
					Bearer:   &norm.Ref{Text: "người sử dụng lao động", IsActor: true},
					Action:   norm.Ref{Text: "đền bù"},
					Deadline: &norm.Deadline{Text: "trong thời hạn 15 ngày", Value: 15, Unit: norm.UnitDay},
				},
			},
			{
				ID: "vn:norm:bbb", DocID: tax, ProvisionID: tax + ":article-17:clause-1",
				Status: norm.StatusRejected,
				Statement: norm.Statement{
					Type:   "duty",
					Bearer: &norm.Ref{Text: "người nộp thuế", IsActor: true},
					Action: norm.Ref{Text: "đăng ký thuế"},
				},
			},
		},
		Subjects: []subject.Record{
			{DocID: labour, Subjects: []subject.Assignment{{SubjectID: "lao-dong/tien-luong"}}},
			{DocID: tax, Subjects: []subject.Assignment{{SubjectID: "thue"}}},
		},
		Terms: []term.Definition{
			{TermID: "vn:term:nguoi-lao-dong", Term: "Người lao động", DocID: labour, ProvisionID: labour + ":article-3:clause-1"},
		},
		Links: []cite.Link{
			{FromDoc: tax, FromProvision: tax + ":article-17:clause-1", ToNumber: "45/2019/QH14", ToDoc: labour, Kind: "cites"},
		},
		Validity: []temporal.Validity{
			{Kind: "norm", RecordID: "vn:norm:aaa", ProvisionID: labour + ":article-94:clause-2",
				From: "2021-01-01", Force: temporal.ForceInForce, Source: temporal.SourceCommencement},
			{Kind: "norm", RecordID: "vn:norm:aaa2", ProvisionID: labour + ":article-94:clause-2",
				From: "2021-01-01", Force: temporal.ForceInForce, Source: temporal.SourceCommencement},
		},
	})
}

func TestBuildIndexesOnlyComponentsWithWords(t *testing.T) {
	ix := testIndex()
	if ix.Len() != 4 {
		t.Fatalf("indexed %d components, want the 4 that carry text", ix.Len())
	}
	if ix.Unit(labour+":article-3") != nil {
		t.Error("an article with a heading and no words of its own was indexed, nothing can be quoted from it")
	}
	u := ix.Unit(labour + ":article-94:clause-2")
	if got := u.Aspect(AspectHeading); len(got) != 2 {
		t.Errorf("heading chain is %v, want the article heading above it and the document title", got)
	}
}

// The shape that runs through the corpus: a stem clause whose duties live in
// its lettered points. A statement read from the clause is about words the
// clause itself does not contain.
func TestAComponentCarriesTheWordsOfEverythingUnderIt(t *testing.T) {
	doc := labourDoc()
	doc.Provisions = append(doc.Provisions,
		law.Provision{ID: labour + ":article-94:clause-2:point-a", ParentID: labour + ":article-94:clause-2",
			Kind: "point", Number: "a", Text: "Đền bù một khoản tiền ít nhất bằng số tiền lãi."},
		law.Provision{ID: labour + ":article-94:clause-2:point-b", ParentID: labour + ":article-94:clause-2",
			Kind: "point", Number: "b", Text: "Thông báo cho người lao động biết."})
	ix := Build(Input{Docs: []*law.Document{doc}})

	u := ix.Unit(labour + ":article-94:clause-2")
	if strings.Contains(u.Text, "khoản tiền lãi") {
		t.Error("the fixture no longer separates the stem from its points")
	}
	for _, want := range []string{"đền bù trong thời hạn 15 ngày", "ít nhất bằng số tiền lãi", "Thông báo cho người lao động"} {
		if !strings.Contains(u.Span, want) {
			t.Errorf("the span does not carry %q, so nothing may be quoted from the point that says it", want)
		}
	}
	// The article above it carries the whole subtree, and a point carries only
	// itself.
	if art := ix.Unit(labour + ":article-94"); !strings.Contains(art.Span, "số tiền lãi") {
		t.Error("an article's span stops before its clauses")
	}
	if pt := ix.Unit(labour + ":article-94:clause-2:point-a"); pt.Span != pt.Text {
		t.Errorf("a leaf span is %q, want its own words", pt.Span)
	}
	// Ranking is unchanged: the span is for quoting, not for matching, or a
	// stem would inherit every word its points contain and outrank them.
	res := ix.Search(Query{Text: "số tiền lãi", K: 3})
	if len(res.Hits) == 0 || res.Hits[0].ID != labour+":article-94:clause-2:point-a" {
		t.Errorf("the point that says it is not the top hit: %v", res.Hits)
	}
}

func TestOnlyTrustedStatementsBecomeAspects(t *testing.T) {
	ix := testIndex()
	u := ix.Unit(tax + ":article-17:clause-1")
	if len(u.Statements) != 0 {
		t.Fatalf("a rejected statement reached the index as %v", u.Statements)
	}
	if got := u.Aspect(AspectBearer); len(got) != 0 {
		t.Errorf("a rejected statement contributed the bearer aspect %v", got)
	}
}

// The clause says nothing about an employer. The graph does, and that is the
// whole argument for indexing aspects rather than words.
func TestTheGraphReachesAClauseItsOwnWordsDoNot(t *testing.T) {
	ix := testIndex()
	const clause = labour + ":article-94:clause-2"
	res := ix.Search(Query{Text: "người sử dụng", K: 3})
	var hit *Hit
	for i := range res.Hits {
		if res.Hits[i].ID == clause {
			hit = &res.Hits[i]
		}
	}
	if hit == nil {
		t.Fatal("the clause was not retrieved for a phrase the graph attached to it")
	}
	if hit.ByAspect[AspectBearer] == 0 {
		t.Errorf("the bearer aspect contributed nothing, so the hit came from the words after all: %v", hit.ByAspect)
	}

	plain := Build(Input{Docs: []*law.Document{labourDoc(), taxDoc()}})
	for _, h := range plain.Search(Query{Text: "người sử dụng", K: 3}).Hits {
		if h.ID == clause {
			t.Error("a text only index found the clause by the phrase that is not in it")
		}
	}
}

func TestScopeRunsBeforeRankingAndReportsEachFilter(t *testing.T) {
	ix := testIndex()
	res := ix.Search(Query{Text: "thời hạn 15 ngày", K: 5, Scope: Scope{Docs: []string{tax}}})
	for _, h := range res.Hits {
		if h.DocID != tax {
			t.Fatalf("hit %s is outside the scope the caller asked for", h.ID)
		}
	}
	if res.InScope != 1 {
		t.Errorf("scope kept %d components, want the 1 in that document", res.InScope)
	}
	if len(res.Steps) != 1 || res.Steps[0].Filter != "doc" || res.Steps[0].After != 1 {
		t.Errorf("steps are %v, want one doc filter that reports what it removed", res.Steps)
	}
}

func TestSubjectScopeReachesSubdomains(t *testing.T) {
	ix := testIndex()
	res := ix.Search(Query{Text: "trả lương", K: 5, Scope: Scope{Subjects: []string{"lao-dong"}}})
	if res.InScope != 3 {
		t.Fatalf("subject lao-dong kept %d components, want the 3 in the labour code", res.InScope)
	}
}

func TestComponentScopeIsASubtree(t *testing.T) {
	ix := testIndex()
	res := ix.Search(Query{Text: "ngày", K: 5, Scope: Scope{Components: []string{labour + ":article-94"}}})
	if res.InScope != 2 || len(res.Hits) == 0 || res.Hits[0].ID != labour+":article-94:clause-2" {
		t.Fatalf("scoping to an article did not reach its clause: %d in scope, hits %v", res.InScope, res.Hits)
	}
}

func TestTheDateFilterSeparatesUnstampedFromOutOfForce(t *testing.T) {
	ix := testIndex()
	keep, steps := ix.Select(Scope{Date: "2022-06-01"})
	kept := 0
	for _, ok := range keep {
		if ok {
			kept++
		}
	}
	if kept != 1 {
		t.Fatalf("%d components in force, want the 1 with a stamped interval covering that day", kept)
	}
	last := steps[len(steps)-1]
	if !strings.Contains(last.Note, "no recorded interval") {
		t.Errorf("the date filter did not say how many components it dropped for being unstamped: %q", last.Note)
	}
	if keep2, _ := ix.Select(Scope{Date: "2019-01-01"}); keep2[indexOf(t, ix, labour+":article-94:clause-2")] {
		t.Error("a component was in force before the interval stamped on it began")
	}
}

// Most of this corpus is stamped from a commencement date on a document that
// something amends, where the amendment has not been read. The temporal layer
// answers "not in force" to that, meaning it does not know, and a date filter
// that reports it as "out of force" is lying about which of the two happened.
func TestTheDateFilterSaysWhenItDoesNotKnowRatherThanSayingNo(t *testing.T) {
	doc := labourDoc()
	ix := Build(Input{
		Docs: []*law.Document{doc},
		Validity: []temporal.Validity{{
			Kind: "norm", RecordID: "vn:norm:aaa", ProvisionID: labour + ":article-94:clause-2",
			From: "2021-01-01", Force: temporal.ForceInForce, Source: temporal.SourceCommencementAmended,
		}},
	})
	const clause = labour + ":article-94:clause-2"

	keep, steps := ix.Select(Scope{Date: "2022-06-01"})
	if keep[indexOf(t, ix, clause)] {
		t.Error("a wording whose amendment nobody has read was called in force")
	}
	last := steps[len(steps)-1]
	if !strings.Contains(last.Note, "does not know") {
		t.Errorf("the filter reported it as out of force rather than as unread: %q", last.Note)
	}

	keep, steps = ix.Select(Scope{Date: "2022-06-01", Unread: true})
	if !keep[indexOf(t, ix, clause)] {
		t.Error("asking for the guess did not admit the unread wording")
	}
	if !strings.Contains(steps[len(steps)-1].Detail, "assuming") {
		t.Errorf("the guess was not written into the step: %q", steps[len(steps)-1].Detail)
	}
	// The guess is about an amendment, not about time. A date before the
	// wording began is still a no.
	if keep, _ := ix.Select(Scope{Date: "2019-01-01", Unread: true}); keep[indexOf(t, ix, clause)] {
		t.Error("the guess admitted a wording that had not started yet")
	}
}

func TestStatementScopeKeepsWhatTheAnswererMayAssertFrom(t *testing.T) {
	ix := testIndex()
	keep, _ := ix.Select(Scope{Statements: true})
	for i, u := range ix.Units() {
		if keep[i] != (len(u.Statements) > 0) {
			t.Fatalf("%s kept=%v with %d statements", u.ComponentID, keep[i], len(u.Statements))
		}
	}
}

// The snapshot problem: an amending law restates the article it amends, so the
// corpus holds the same words twice under two identifiers.
func TestARestatementIsFoldedIntoTheProvisionItRestates(t *testing.T) {
	original := labourDoc()
	amending := &law.Document{
		ID: "vn:law:2022:11-2022-qh15", Title: "Luật sửa đổi",
		Provisions: []law.Provision{
			{ID: "vn:law:2022:11-2022-qh15:article-1", Kind: "article", Number: "1",
				Text: original.Provisions[3].Text},
		},
	}
	ix := Build(Input{Docs: []*law.Document{original, amending}})
	const restatement = "vn:law:2022:11-2022-qh15:article-1"
	res := ix.Search(Query{Text: "trả lương chậm thì phải đền bù", K: 5})
	for _, h := range res.Hits {
		if h.ID == restatement {
			t.Fatal("the restatement came back as a hit of its own")
		}
	}
	if res.Suppressed != 1 {
		t.Errorf("suppressed count is %d, want 1", res.Suppressed)
	}
	if len(res.Hits) == 0 || len(res.Hits[0].Duplicates) != 1 || res.Hits[0].Duplicates[0] != restatement {
		t.Errorf("the suppressed identifier was thrown away instead of kept on the surviving hit: %v", res.Hits)
	}
	off := ix.Search(Query{Text: "trả lương chậm thì phải đền bù", K: 5, Duplicates: -1})
	found := false
	for _, h := range off.Hits {
		if h.ID == restatement {
			found = true
		}
	}
	if !found || off.Suppressed != 0 {
		t.Errorf("suppression could not be turned off, hits %v", off.Hits)
	}
}

func TestSearchSaysWhetherTheScopeOrTheQueryEmptiedTheResult(t *testing.T) {
	ix := testIndex()
	byScope := ix.Search(Query{Text: "trả lương", Scope: Scope{Docs: []string{"vn:law:1990:1-1990-qh8"}}})
	if byScope.InScope != 0 || byScope.Matched != 0 {
		t.Errorf("scope emptied the corpus but the result reads %d in scope, %d matched", byScope.InScope, byScope.Matched)
	}
	byQuery := ix.Search(Query{Text: "tàu biển chở dầu"})
	if byQuery.InScope != 4 || byQuery.Matched != 0 {
		t.Errorf("query matched nothing but the result reads %d in scope, %d matched", byQuery.InScope, byQuery.Matched)
	}
}

func TestTokensIndexSyllablePairsAndKeepDiacritics(t *testing.T) {
	got := Tokens("Người lao động")
	want := map[string]bool{"người": true, "lao": true, "động": true, "người_lao": true, "lao_động": true}
	if len(got) != len(want) {
		t.Fatalf("tokens are %v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected token %q in %v", g, got)
		}
	}
	for _, tok := range Tokens("phải") {
		if tok == "phai" {
			t.Error("diacritics were folded away, which merges phải with phái")
		}
	}
}

func indexOf(t *testing.T, ix *Index, id string) int {
	t.Helper()
	for i, u := range ix.Units() {
		if u.ComponentID == id {
			return i
		}
	}
	t.Fatalf("no unit %s", id)
	return -1
}
