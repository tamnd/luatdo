package anchor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/law"
)

// bankingLaw is article 6 of the Law on the State Bank as the corpus actually
// carries it. Two details are real and both matter: the defined term is
// separated from its connective by a line break, because the source bolds the
// term, and clause 2 defines by enumeration with "bao gồm" rather than "là".
// Anchoring must survive both without knowing either, since it never splits a
// term from its definition.
func bankingLaw() *law.Document {
	const id = "vn:law:2010:46-2010-qh12"
	return &law.Document{
		ID: id, DocType: "luật", Status: "parsed", IssuingBody: "Quốc hội",
		Provisions: []law.Provision{
			{ID: id + ":article-5", Kind: "article", Number: "5", Heading: "Nhiệm vụ, quyền hạn",
				Text: "Ngân hàng Nhà nước Việt Nam (sau đây gọi là Ngân hàng Nhà nước) thực hiện chức năng quản lý."},
			{ID: id + ":article-6", Kind: "article", Number: "6", Heading: "Giải thích từ ngữ",
				Text: "Trong Luật này, các từ ngữ dưới đây được hiểu như sau:"},
			{ID: id + ":article-6:clause-1", ParentID: id + ":article-6", Kind: "clause", Number: "1",
				Text:     "Hoạt động ngân hàng\nlà việc kinh doanh, cung ứng thường xuyên một hoặc một số nghiệp vụ sau đây:",
				TextHash: "hash-1"},
			{ID: id + ":article-6:clause-2", ParentID: id + ":article-6", Kind: "clause", Number: "2",
				Text: "Ngoại hối\nbao gồm:", TextHash: "hash-2"},
			{ID: id + ":article-6:clause-2:point-a", ParentID: id + ":article-6:clause-2", Kind: "point",
				Number: "a", Text: "Đồng tiền của quốc gia khác;"},
		},
	}
}

func TestAnchorFindsTheDefinitionsArticle(t *testing.T) {
	r := Anchor(bankingLaw())
	if len(r.Scopes) != 1 {
		t.Fatalf("scopes = %d, want 1: %+v", len(r.Scopes), r.Scopes)
	}
	s := r.Scopes[0]
	if s.ID != "vn:law:2010:46-2010-qh12" || s.Kind != "document" {
		t.Errorf("scope = %s %s, want the document itself", s.ID, s.Kind)
	}
	if s.Instrument != "luật" {
		t.Errorf("instrument = %q, want luật", s.Instrument)
	}
	if s.FoundBy != "both" {
		t.Errorf("found_by = %q, want both: the heading and the formula are both present", s.FoundBy)
	}
	if r.Residue != "" {
		t.Errorf("residue = %q, want empty", r.Residue)
	}
}

// The unit is the whole clause. A pass that split the term off here would be
// deciding what the clause means, which is the reading and not the anchoring.
func TestAnchorSplitsClausesAndKeepsThemWhole(t *testing.T) {
	r := Anchor(bankingLaw())
	if len(r.Units) != 2 {
		t.Fatalf("units = %d, want 2: %+v", len(r.Units), r.Units)
	}
	first := r.Units[0]
	if first.ID != "vn:law:2010:46-2010-qh12:article-6:clause-1" {
		t.Errorf("unit id = %s", first.ID)
	}
	if first.ArticleID != "vn:law:2010:46-2010-qh12:article-6" {
		t.Errorf("article id = %s", first.ArticleID)
	}
	if first.ScopeID != "vn:law:2010:46-2010-qh12" {
		t.Errorf("scope id = %s", first.ScopeID)
	}
	if !strings.HasPrefix(first.Text, "Hoạt động ngân hàng\nlà việc") {
		t.Errorf("unit text = %q, want the clause verbatim including its line break", first.Text)
	}
	if first.TextHash != "hash-1" {
		t.Errorf("text hash = %q, want the clause hash so the unit is pinned to one version", first.TextHash)
	}
	if r.Units[1].Text != "Ngoại hối\nbao gồm:" {
		t.Errorf("second unit = %q, want the enumerating definition whole", r.Units[1].Text)
	}
}

// The heading alone is enough, and so is the formula alone. Both paths are
// exercised because a third of the corpus carries only one of them.
func TestAnchorFindsByHeadingOrByFormulaAlone(t *testing.T) {
	tests := []struct {
		name    string
		heading string
		text    string
		want    string
	}{
		{"heading only", "Giải thích từ ngữ", "Các quy định chung áp dụng như sau:", "heading"},
		{"formula only", "Nguyên tắc chung", "Trong Nghị định này, các từ ngữ dưới đây được hiểu như sau:", "formula"},
		{"formula reversed", "Nguyên tắc chung", "Các từ ngữ trong Thông tư này được hiểu như sau:", "formula"},
		{"formula without comma", "Nguyên tắc chung", "Trong Luật này các từ ngữ dưới đây được hiểu như sau:", "formula"},
		{"technical wording", "Giải thích thuật ngữ", "Trong Thông tư này, các thuật ngữ dưới đây được hiểu như sau:", "both"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const id = "vn:law:2020:1-2020-nd-cp"
			doc := &law.Document{ID: id, Status: "parsed", Provisions: []law.Provision{
				{ID: id + ":article-3", Kind: "article", Number: "3", Heading: tt.heading, Text: tt.text},
				{ID: id + ":article-3:clause-1", ParentID: id + ":article-3", Kind: "clause",
					Number: "1", Text: "Cơ sở dữ liệu là tập hợp các dữ liệu."},
			}}
			r := Anchor(doc)
			if len(r.Scopes) != 1 {
				t.Fatalf("scopes = %d, want 1", len(r.Scopes))
			}
			if r.Scopes[0].FoundBy != tt.want {
				t.Errorf("found_by = %q, want %q", r.Scopes[0].FoundBy, tt.want)
			}
			if len(r.Units) != 1 {
				t.Errorf("units = %d, want 1", len(r.Units))
			}
		})
	}
}

// A decision's annex carries its own definitions with its own scoping clause,
// and the scope is the annex. Flattening it onto the decision would claim a
// definition the decision never made.
func TestAnchorScopesAnnexDefinitionsToTheAnnex(t *testing.T) {
	const id = "vn:law:2019:01-2019-qd-ubnd:ubnd-tinh-bac-giang"
	doc := &law.Document{ID: id, DocType: "quyết định", Status: "parsed", Provisions: []law.Provision{
		{ID: id + ":article-1", Kind: "article", Number: "1",
			Text: "Ban hành kèm theo Quyết định này Quy chế quản lý."},
		{ID: id + ":annex-1", Kind: "annex", Number: "1", Heading: "QUY CHẾ quản lý"},
		{ID: id + ":annex-1:article-2", ParentID: id + ":annex-1", Kind: "article", Number: "2",
			Heading: "Giải thích từ ngữ", Text: "Trong Quy chế này, các từ ngữ dưới đây được hiểu như sau:"},
		{ID: id + ":annex-1:article-2:clause-1", ParentID: id + ":annex-1:article-2", Kind: "clause",
			Number: "1", Text: "Chủ đầu tư là cơ quan được giao quản lý dự án."},
	}}
	r := Anchor(doc)
	if len(r.Scopes) != 1 {
		t.Fatalf("scopes = %d, want 1", len(r.Scopes))
	}
	if got := r.Scopes[0]; got.ID != id+":annex-1" || got.Kind != "annex" {
		t.Errorf("scope = %s %s, want the annex", got.ID, got.Kind)
	}
	if r.Units[0].ScopeID != id+":annex-1" {
		t.Errorf("unit scope = %s, want the annex", r.Units[0].ScopeID)
	}
}

// Where the annex boundary was never parsed, the formula still says which
// instrument it is scoping, and a Quy chế is not the decision that issued it.
func TestAnchorScopesASubInstrumentEvenWithoutAnAnnexNode(t *testing.T) {
	const id = "vn:law:2016:49-2016-qd-ttg"
	doc := &law.Document{ID: id, DocType: "quyết định", Status: "parsed", Provisions: []law.Provision{
		{ID: id + ":article-2", Kind: "article", Number: "2", Heading: "Giải thích từ ngữ",
			Text: "Các từ ngữ trong Quy chế này được hiểu như sau:"},
		{ID: id + ":article-2:clause-1", ParentID: id + ":article-2", Kind: "clause", Number: "1",
			Text: "Vùng đệm là khu vực tiếp giáp."},
	}}
	r := Anchor(doc)
	if got := r.Scopes[0]; got.Kind != "annex" || got.ID == id {
		t.Errorf("scope = %s %s, want a scope of its own rather than the decision", got.ID, got.Kind)
	}
}

// An article that was never divided into clauses is one unit, because a short
// definitions article written as a run of sentences still defines terms.
func TestAnchorTreatsAnUndividedArticleAsOneUnit(t *testing.T) {
	const id = "vn:law:2020:2-2020-tt-btc"
	doc := &law.Document{ID: id, Status: "parsed", Provisions: []law.Provision{
		{ID: id + ":article-2", Kind: "article", Number: "2", Heading: "Giải thích từ ngữ",
			Text: "Trong Thông tư này, các từ ngữ dưới đây được hiểu như sau: Hồ sơ là tập tài liệu."},
	}}
	r := Anchor(doc)
	if len(r.Units) != 1 || r.Units[0].ID != id+":article-2" {
		t.Fatalf("units = %+v, want the article itself", r.Units)
	}
}

func TestAnchorReportsResidueRatherThanReturningNothing(t *testing.T) {
	const id = "vn:law:2020:3-2020-qd-ubnd:ubnd-tinh-lang-son"
	doc := &law.Document{ID: id, Status: "parsed", Provisions: []law.Provision{
		{ID: id + ":article-1", Kind: "article", Number: "1", Text: "Quyết định này có hiệu lực."},
	}}
	if r := Anchor(doc); !strings.Contains(r.Residue, "no definitions article") {
		t.Errorf("residue = %q, want the reason nothing was found", r.Residue)
	}
	metadata := &law.Document{ID: id, Status: "metadata"}
	if r := Anchor(metadata); !strings.Contains(r.Residue, "metadata") {
		t.Errorf("residue = %q, want the status that explains the absence", r.Residue)
	}
}

func TestAliasesHarvestsTheThreeDeclaredForms(t *testing.T) {
	const id = "vn:law:2019:01-2019-qd-ubnd:ubnd-tinh-bac-giang"
	doc := &law.Document{ID: id, Status: "parsed", Provisions: []law.Provision{
		{ID: id + ":article-3:clause-2", Kind: "clause", Number: "2",
			Text: "UBND các huyện, thành phố (sau đây gọi là UBND cấp huyện); UBND các xã, phường, thị trấn (sau đây gọi là UBND cấp xã) có trách nhiệm:"},
		{ID: id + ":article-4", Kind: "article", Number: "4",
			Text: "Sở Xây dựng và Sở Tài chính (sau đây gọi tắt là Liên Sở) chủ trì."},
		{ID: id + ":article-5", Kind: "article", Number: "5",
			Text: "Những người quy định tại các khoản 1 và 2 Điều này sau đây gọi chung là người lao động."},
	}}
	got := Aliases(doc)
	want := []struct{ short, form string }{
		{"UBND cấp huyện", "goi"},
		{"UBND cấp xã", "goi"},
		{"Liên Sở", "goi-tat"},
		{"người lao động", "goi-chung"},
	}
	if len(got) != len(want) {
		t.Fatalf("aliases = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Short != w.short || got[i].Form != w.form {
			t.Errorf("alias %d = %q %q, want %q %q", i, got[i].Short, got[i].Form, w.short, w.form)
		}
	}
}

// Every quote has to be findable at its offsets in the provision it came from.
// This is the property the whole design rests on, so it is checked here rather
// than assumed downstream.
func TestAliasQuotesAreVerifiableAtTheirOffsets(t *testing.T) {
	doc := bankingLaw()
	got := Aliases(doc)
	if len(got) != 1 {
		t.Fatalf("aliases = %d, want 1: %+v", len(got), got)
	}
	a := got[0]
	if a.Short != "Ngân hàng Nhà nước" {
		t.Errorf("short = %q", a.Short)
	}
	if a.LongCandidate != "Ngân hàng Nhà nước Việt Nam" {
		t.Errorf("long candidate = %q", a.LongCandidate)
	}
	var text string
	for i := range doc.Provisions {
		if doc.Provisions[i].ID == a.ProvisionID {
			text = doc.Provisions[i].Text
		}
	}
	if text == "" {
		t.Fatalf("alias points at %s, which is not a provision of the document", a.ProvisionID)
	}
	if a.CharStart < 0 || a.CharEnd > len(text) || text[a.CharStart:a.CharEnd] != a.Quote {
		t.Errorf("quote %q is not at [%d,%d) of %q", a.Quote, a.CharStart, a.CharEnd, text)
	}
	if !strings.HasSuffix(a.Quote, a.Short) {
		t.Errorf("quote %q does not end with the declared short form %q", a.Quote, a.Short)
	}
}

// A declaration whose short form runs on for a sentence is not an alias, and
// taking it would be a guess dressed as an exact match.
func TestAliasesRefuseARunOnShortForm(t *testing.T) {
	long := strings.Repeat("rất dài ", 40)
	doc := &law.Document{ID: "vn:law:2020:1-2020-qh14", Status: "parsed", Provisions: []law.Provision{
		{ID: "vn:law:2020:1-2020-qh14:article-1", Kind: "article", Number: "1",
			Text: "Cơ quan quản lý sau đây gọi là " + long},
	}}
	if got := Aliases(doc); len(got) != 0 {
		t.Errorf("aliases = %+v, want none", got)
	}
}

func TestSummaryCountsContentSeparatelyFromAnchoring(t *testing.T) {
	sum := NewSummary()
	banking := bankingLaw()
	sum.Add(banking, Anchor(banking))

	silent := &law.Document{ID: "vn:law:2020:9-2020-qd-ubnd:ubnd-tinh-tay-ninh",
		DocType: "quyết định", IssuingBody: "UBND tỉnh Tây Ninh", Status: "parsed",
		Provisions: []law.Provision{{ID: "vn:law:2020:9-2020-qd-ubnd:ubnd-tinh-tay-ninh:article-1",
			Kind: "article", Number: "1", Text: "Quyết định này có hiệu lực."}}}
	sum.Add(silent, Anchor(silent))

	pending := &law.Document{ID: "vn:law:2020:8-2020-tt-btc", DocType: "thông tư", Status: "metadata"}
	sum.Add(pending, Anchor(pending))

	if sum.Documents != 3 || sum.WithContent != 2 || sum.Anchored != 1 {
		t.Errorf("documents = %d, with content = %d, anchored = %d, want 3, 2, 1",
			sum.Documents, sum.WithContent, sum.Anchored)
	}
	if got := sum.ByDocType["luật"]; got.Anchored != 1 || got.WithContent != 1 {
		t.Errorf("luật = %+v, want 1 of 1", got)
	}
	if got := sum.ByDocType["quyết định"]; got.Anchored != 0 || got.WithContent != 1 {
		t.Errorf("quyết định = %+v, want 0 of 1", got)
	}
	if got := sum.ByBody["Quốc hội"]; got.Anchored != 1 {
		t.Errorf("Quốc hội = %+v, want 1 anchored", got)
	}
	if len(sum.Unanchored) != 1 || sum.Unanchored[0] != silent.ID || sum.UnanchoredCount != 1 {
		t.Errorf("unanchored = %v, want the one document with text and no definitions", sum.Unanchored)
	}
	// The list goes to the residue file and only the count travels in the
	// summary, so a reader of the counts does not load a hundred thousand
	// identifiers to get them.
	encoded, err := json.Marshal(sum)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), silent.ID) {
		t.Errorf("the summary serialises the residue list: %s", encoded)
	}
	// A document with no text is not counted as unanchored, because its text
	// is missing rather than its definitions.
	if strings.Contains(sum.String(), pending.ID) {
		t.Errorf("summary names a document that has no text: %s", sum)
	}
}
