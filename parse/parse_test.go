package parse

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/law"
)

const codeBody = `# Bộ luật Lao động

**Số hiệu:** 45/2019/QH14

**Ngày hiệu lực:** 01/01/2021

---

QUỐC HỘI

Số: 45/2019/QH14

BỘ LUẬT

Chương I

NHỮNG QUY ĐỊNH CHUNG

Điều 1. Phạm vi điều chỉnh

Bộ luật Lao động quy định tiêu chuẩn lao động.

Điều 2. Đối tượng áp dụng

1. Người lao động làm việc theo hợp đồng.

a) Cán bộ theo quy định;

phần nối tiếp của điểm a

b) Công chức theo quy định.

2. Người sử dụng lao động.

Điều 3

Nội dung của điều không có tiêu đề.

CHỦ TỊCH QUỐC HỘI

Nguyễn Thị Kim Ngân
`

func mustParse(t *testing.T, in Input) *law.Document {
	t.Helper()
	doc, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

func find(doc *law.Document, id string) *law.Provision {
	for i := range doc.Provisions {
		if doc.Provisions[i].ID == id {
			return &doc.Provisions[i]
		}
	}
	return nil
}

func TestParseCode(t *testing.T) {
	doc := mustParse(t, Input{OfficialNumber: "45/2019/QH14", Content: codeBody})
	if doc.Status != "parsed" {
		t.Fatalf("status = %q, quarantine %q", doc.Status, doc.Quarantine)
	}
	if doc.EffectiveFrom != "01/01/2021" {
		t.Errorf("effective from = %q", doc.EffectiveFrom)
	}

	base := "vn:law:2019:45-2019-qh14"
	chapter := find(doc, base+":chapter-1")
	if chapter == nil || chapter.Heading != "NHỮNG QUY ĐỊNH CHUNG" {
		t.Fatalf("chapter = %+v", chapter)
	}

	art1 := find(doc, base+":article-1")
	if art1 == nil || art1.Heading != "Phạm vi điều chỉnh" {
		t.Fatalf("article 1 = %+v", art1)
	}
	if art1.ParentID != chapter.ID {
		t.Errorf("article 1 parent = %q", art1.ParentID)
	}
	if !strings.Contains(art1.Text, "tiêu chuẩn lao động") {
		t.Errorf("article 1 text = %q", art1.Text)
	}

	clause1 := find(doc, base+":article-2:clause-1")
	if clause1 == nil || !strings.HasPrefix(clause1.Text, "Người lao động") {
		t.Fatalf("clause 1 = %+v", clause1)
	}
	pointA := find(doc, base+":article-2:clause-1:point-a")
	if pointA == nil {
		t.Fatal("point a missing")
	}
	if !strings.Contains(pointA.Text, "phần nối tiếp của điểm a") {
		t.Errorf("point a continuation not attached: %q", pointA.Text)
	}
	if find(doc, base+":article-2:clause-1:point-b") == nil {
		t.Error("point b missing")
	}
	if find(doc, base+":article-2:clause-2") == nil {
		t.Error("clause 2 missing")
	}

	art3 := find(doc, base+":article-3")
	if art3 == nil {
		t.Fatal("bare article heading not recognized")
	}
	if art3.Heading != "" {
		t.Errorf("bare article heading = %q", art3.Heading)
	}
	for i := range doc.Provisions {
		if strings.Contains(doc.Provisions[i].Text, "CHỦ TỊCH QUỐC HỘI") {
			t.Errorf("signature block leaked into %s", doc.Provisions[i].ID)
		}
	}
}

const amendingBody = `**Số hiệu:** 26/2012/QH13

---

Số: 26/2012/QH13

LUẬT

Điều 1 Sửa đổi, bổ sung một số điều của Luật Thuế thu nhập cá nhân:

1. Khoản 2 Điều 3 được sửa đổi, bổ sung như sau:

“2. Thu nhập từ tiền lương, tiền công.”

Điều 22 được sửa đổi, bổ sung như sau:

Điều 2

Luật này có hiệu lực thi hành từ ngày 01 tháng 7 năm 2013.
`

func TestParseAmendingLaw(t *testing.T) {
	doc := mustParse(t, Input{OfficialNumber: "26/2012/QH13", Content: amendingBody})
	if doc.Status != "parsed" {
		t.Fatalf("status = %q, quarantine %q", doc.Status, doc.Quarantine)
	}
	articles := 0
	for i := range doc.Provisions {
		if doc.Provisions[i].Kind == "article" {
			articles++
		}
	}
	if articles != 2 {
		t.Fatalf("articles = %d, want 2", articles)
	}

	base := "vn:law:2012:26-2012-qh13"
	art1 := find(doc, base+":article-1")
	if art1 == nil || !strings.HasPrefix(art1.Heading, "Sửa đổi") {
		t.Fatalf("dotless article heading = %+v", art1)
	}
	clause1 := find(doc, base+":article-1:clause-1")
	if clause1 == nil {
		t.Fatal("clause 1 missing")
	}
	if !strings.Contains(clause1.Text, "Điều 22 được sửa đổi") {
		t.Errorf("lowercase article reference should stay clause text: %q", clause1.Text)
	}
	if !strings.Contains(clause1.Text, "Thu nhập từ tiền lương") {
		t.Errorf("quoted amendment text should stay clause text: %q", clause1.Text)
	}
}

func TestQuarantineWrongNumber(t *testing.T) {
	body := "**Số hiệu:** 45/2019/QH14\n\n---\n\nSố: 15/2020/NĐ-CP\n\nNGHỊ ĐỊNH\n"
	doc := mustParse(t, Input{OfficialNumber: "45/2019/QH14", Content: body})
	if doc.Status != "quarantined" {
		t.Fatalf("status = %q, want quarantined", doc.Status)
	}
	if !strings.Contains(doc.Quarantine, "15/2020/NĐ-CP") {
		t.Errorf("quarantine reason = %q", doc.Quarantine)
	}
	if len(doc.Provisions) != 0 {
		t.Errorf("quarantined document has %d provisions", len(doc.Provisions))
	}
}

func TestQuarantineNoArticles(t *testing.T) {
	body := "**Số hiệu:** 45/2019/QH14\n\n---\n\nVăn bản không có cấu trúc điều khoản nào cả.\n"
	doc := mustParse(t, Input{OfficialNumber: "45/2019/QH14", Content: body})
	if doc.Status != "quarantined" {
		t.Fatalf("status = %q, want quarantined", doc.Status)
	}
	if doc.Quarantine != "no article structure found in body" {
		t.Errorf("quarantine reason = %q", doc.Quarantine)
	}
}

func TestParseBareClauseNumber(t *testing.T) {
	body := "**Số hiệu:** 46/2010/QH12\n\n---\n\n" +
		"Điều 6. Giải thích từ ngữ\n\n" +
		"Trong Luật này, các từ ngữ dưới đây được hiểu như sau:\n\n" +
		"1.\n\nHoạt động ngân hàng\n\nlà việc kinh doanh tiền tệ.\n\n" +
		"2. Kiểm dịch y tế là việc kiểm tra y tế.\n\n" +
		"Điều 7. Nguyên tắc\n\n1. Nguyên tắc thứ nhất.\n"
	doc := mustParse(t, Input{OfficialNumber: "46/2010/QH12", Content: body})
	if doc.Status != "parsed" {
		t.Fatalf("status = %q: %s", doc.Status, doc.Quarantine)
	}
	var clause1 *law.Provision
	for i := range doc.Provisions {
		if doc.Provisions[i].ID == "vn:law:2010:46-2010-qh12:article-6:clause-1" {
			clause1 = &doc.Provisions[i]
		}
	}
	if clause1 == nil {
		t.Fatal("the bare number line must open clause 1")
	}
	want := "Hoạt động ngân hàng\nlà việc kinh doanh tiền tệ."
	if clause1.Text != want {
		t.Errorf("clause 1 text = %q, want %q", clause1.Text, want)
	}
}

// decisionBody is the shape almost every provincial decision takes: three
// articles of housekeeping, a signature block, and then the regulation that
// carries the entire substance travelling underneath as an annex. The annex
// restarts its article numbering at one.
const decisionBody = `**Số hiệu:** 01/2019/QĐ-UBND

---

ỦY BAN NHÂN DÂN TỈNH BẮC GIANG

Số: 01/2019/QĐ-UBND

QUYẾT ĐỊNH

Ban hành Quy định về quản lý đầu tư

Điều 1. Ban hành kèm theo Quyết định này Quy định về quản lý đầu tư và xây dựng.

Điều 2. Quyết định này có hiệu lực kể từ ngày 20 tháng 01 năm 2019.

TM. ỦY BAN NHÂN DÂN

CHỦ TỊCH

Nguyễn Văn Linh

QUY ĐỊNH

Về quản lý đầu tư và xây dựng

(Ban hành kèm theo Quyết định số 01/2019/QĐ-UBND ngày 10 tháng 01 năm 2019 của Ủy ban nhân dân tỉnh Bắc Giang)

Chương I

QUY ĐỊNH CHUNG

Điều 1. Phạm vi điều chỉnh

Quy định này quy định về quản lý đầu tư trên địa bàn tỉnh.

Điều 2. Giải thích từ ngữ

Trong Quy định này, các từ ngữ dưới đây được hiểu như sau:

1. Chủ đầu tư là cơ quan được giao quản lý dự án.

Chương II

QUẢN LÝ ĐẦU TƯ VÀ XÂY DỰNG CÁC DỰ ÁN

SỬ DỤNG VỐN NHÀ NƯỚC

Điều 3. Thẩm quyền quyết định

Chủ tịch Ủy ban nhân dân tỉnh quyết định đầu tư.
`

func parseDecision(t *testing.T) *law.Document {
	t.Helper()
	return mustParse(t, Input{
		OfficialNumber: "01/2019/QĐ-UBND",
		IssuingBody:    "Ủy ban nhân dân tỉnh Bắc Giang",
		DocType:        "quyết định",
		Content:        decisionBody,
	})
}

func TestParseAnnexAfterSignature(t *testing.T) {
	doc := parseDecision(t)
	if doc.Status != "parsed" {
		t.Fatalf("status = %q: %s", doc.Status, doc.Quarantine)
	}
	annex := find(doc, doc.ID+":annex-1")
	if annex == nil {
		t.Fatal("the annex under the signature block was dropped")
	}
	if annex.Heading != "QUY ĐỊNH Về quản lý đầu tư và xây dựng" {
		t.Errorf("annex heading = %q", annex.Heading)
	}
	if find(doc, doc.ID+":annex-1:article-3") == nil {
		t.Error("article 3 of the annex missing: the walk stopped short")
	}
	for i := range doc.Provisions {
		if strings.Contains(doc.Provisions[i].Text, "Nguyễn Văn Linh") ||
			strings.Contains(doc.Provisions[i].Text, "kèm theo Quyết định số") {
			t.Errorf("signature or issuance block leaked into %s: %q",
				doc.Provisions[i].ID, doc.Provisions[i].Text)
		}
	}
}

// The two Điều 1 in this document are different provisions, and merging them
// would attribute the annex's substance to the decision that issued it.
func TestParseAnnexArticlesDoNotCollideWithTheParent(t *testing.T) {
	doc := parseDecision(t)
	parent := find(doc, doc.ID+":article-1")
	inner := find(doc, doc.ID+":annex-1:article-1")
	if parent == nil || inner == nil {
		t.Fatalf("parent = %+v, annex article = %+v", parent, inner)
	}
	if !strings.HasPrefix(parent.Heading, "Ban hành kèm theo") {
		t.Errorf("parent article 1 = %q", parent.Heading)
	}
	if inner.Heading != "Phạm vi điều chỉnh" {
		t.Errorf("annex article 1 = %q", inner.Heading)
	}
	if inner.ParentID != doc.ID+":annex-1:chapter-1" {
		t.Errorf("annex article 1 parent = %q, want the annex's own chapter", inner.ParentID)
	}
	if find(doc, doc.ID+":annex-1:chapter-1").ParentID != doc.ID+":annex-1" {
		t.Error("the annex's chapter does not hang off the annex")
	}
}

// A chapter title that runs past one line is one title, and a chapter that
// opens after the first article still gets one.
func TestParseChapterHeadingsAfterTheFirstArticle(t *testing.T) {
	doc := parseDecision(t)
	two := find(doc, doc.ID+":annex-1:chapter-2")
	if two == nil {
		t.Fatal("chapter 2 missing")
	}
	want := "QUẢN LÝ ĐẦU TƯ VÀ XÂY DỰNG CÁC DỰ ÁN SỬ DỤNG VỐN NHÀ NƯỚC"
	if two.Heading != want {
		t.Errorf("chapter 2 heading = %q, want %q", two.Heading, want)
	}
}

// "QUY ĐỊNH" alone is also how a decision titles itself, so the kind line is
// never enough on its own. Without the issuance formula there is no annex.
// Nghị định số 72/2025/NĐ-CP puts "Mục 2" between article 7 and article 8, and
// its consolidated text is what showed that the section was being read as the
// last line of the last point of article 7.
const sectionedBody = `# Nghị định

**Số hiệu:** 72/2025/NĐ-CP

**Ngày hiệu lực:** 28/03/2025

---

Chương II

CƠ CHẾ ĐIỀU CHỈNH GIÁ

Mục 1

CƠ CHẾ ĐIỀU CHỈNH

Điều 7. Kiểm tra giá bán lẻ điện bình quân

1. Kiểm tra điều chỉnh giá.

Mục 2

THỜI GIAN ĐIỀU CHỈNH GIÁ BÁN LẺ ĐIỆN BÌNH QUÂN

Điều 8. Thời gian điều chỉnh

Giá được xét thay đổi theo Mục 2 nêu trên.
`

func TestParseSectionAfterAnArticle(t *testing.T) {
	doc := mustParse(t, Input{OfficialNumber: "72/2025/NĐ-CP", Content: sectionedBody})
	two := find(doc, doc.ID+":chapter-2:section-2")
	if two == nil {
		t.Fatal("the section between article 7 and article 8 was not read as a section")
	}
	if two.Heading != "THỜI GIAN ĐIỀU CHỈNH GIÁ BÁN LẺ ĐIỆN BÌNH QUÂN" {
		t.Errorf("section 2 heading = %q", two.Heading)
	}
	eight := find(doc, doc.ID+":article-8")
	if eight == nil {
		t.Fatal("article 8 missing")
	}
	if eight.ParentID != two.ID {
		t.Errorf("article 8 hangs off %s, want the section that opened before it", eight.ParentID)
	}
	// The article before the section keeps its own text and does not swallow the
	// section label.
	clause := find(doc, doc.ID+":article-7:clause-1")
	if clause == nil || strings.Contains(clause.Text, "Mục 2") {
		t.Errorf("clause 1 of article 7 carries the section label: %+v", clause)
	}
	// A reference to a section inside running text is not a section.
	last := find(doc, doc.ID+":article-8")
	if !strings.Contains(last.Text, "theo Mục 2 nêu trên") {
		t.Errorf("article 8 lost the sentence that mentions a section: %q", last.Text)
	}
}

func TestSectionOpens(t *testing.T) {
	cases := []struct {
		line      string
		inArticle bool
		want      bool
	}{
		{"Mục 2", false, true},
		{"Mục 2", true, true},
		{"MỤC 2", true, true},
		{"Mục 2.", true, true},
		{"Mục 1 NHỮNG QUY ĐỊNH CHUNG", false, true},
		{"Mục 1 NHỮNG QUY ĐỊNH CHUNG", true, false},
		{"Điều 7. Kiểm tra", true, false},
	}
	for _, c := range cases {
		if got := sectionOpens(c.line, c.inArticle); got != c.want {
			t.Errorf("sectionOpens(%q, %v) = %v, want %v", c.line, c.inArticle, got, c.want)
		}
	}
}

func TestParseKindLineWithoutIssuanceIsNotAnAnnex(t *testing.T) {
	body := "**Số hiệu:** 02/2019/QĐ-UBND\n\n---\n\nSố: 02/2019/QĐ-UBND\n\n" +
		"Điều 1. Phạm vi\n\nNội dung điều một.\n\n" +
		"QUY ĐỊNH\n\nNội dung tiếp theo của điều một.\n\n" +
		"Điều 2. Hiệu lực\n\nNội dung điều hai.\n"
	doc := mustParse(t, Input{OfficialNumber: "02/2019/QĐ-UBND",
		IssuingBody: "Ủy ban nhân dân tỉnh Bắc Giang", Content: body})
	if a := find(doc, doc.ID+":annex-1"); a != nil {
		t.Fatalf("a bare kind line opened an annex: %+v", a)
	}
	if find(doc, doc.ID+":article-2") == nil {
		t.Error("article 2 missing")
	}
}

// A Phụ lục label is unambiguous on its own, because a reference to an annex
// in running text never ends its line there.
func TestParseAnnexLabelNeedsNoIssuance(t *testing.T) {
	body := "**Số hiệu:** 03/2019/TT-BTC\n\n---\n\nSố: 03/2019/TT-BTC\n\n" +
		"Điều 1. Phạm vi\n\nNội dung điều một.\n\n" +
		"Điều 2. Hiệu lực\n\nNội dung điều hai.\n\n" +
		"Nơi nhận:\n\nBộ trưởng\n\n" +
		"PHỤ LỤC I\n\nDANH MỤC BIỂU MẪU\n\n" +
		"Điều 1. Biểu mẫu số 01\n\nNội dung biểu mẫu.\n"
	doc := mustParse(t, Input{OfficialNumber: "03/2019/TT-BTC", Content: body})
	annex := find(doc, doc.ID+":annex-1")
	if annex == nil {
		t.Fatal("PHỤ LỤC I did not open an annex")
	}
	if annex.Heading != "PHỤ LỤC I DANH MỤC BIỂU MẪU" {
		t.Errorf("annex heading = %q", annex.Heading)
	}
	if find(doc, doc.ID+":annex-1:article-1") == nil {
		t.Error("the annex's article 1 is missing")
	}
}

func TestParseDeterministic(t *testing.T) {
	in := Input{OfficialNumber: "45/2019/QH14", Content: codeBody}
	a := mustParse(t, in)
	b := mustParse(t, in)
	if len(a.Provisions) != len(b.Provisions) {
		t.Fatalf("provision counts differ: %d vs %d", len(a.Provisions), len(b.Provisions))
	}
	for i := range a.Provisions {
		if a.Provisions[i] != b.Provisions[i] {
			t.Fatalf("provision %d differs between runs", i)
		}
	}
	if a.SourceHash != b.SourceHash {
		t.Error("source hashes differ between runs")
	}
}

// This is the shape of every Vietnamese amending law, and the 2007 anti
// corruption amendment is the case that found it. The instruction quotes the
// article it enacts, the quotation runs over several lines, and those lines
// look exactly like structure: "Điều 73." and "1." and "2.". Reading them as
// structure gives the amending law clauses it does not have, gives two of them
// the same identifier, and leaves the model reading the instruction only the
// first line of the text it is supposed to be enacting.
const quotedBody = `**Số hiệu:** 01/2007/QH12

---

Số: 01/2007/QH12

LUẬT

Điều 1 Sửa đổi, bổ sung một số điều của Luật phòng, chống tham nhũng:

1. Điều 73 được sửa đổi, bổ sung như sau:

"Điều 73. Ban chỉ đạo phòng, chống tham nhũng

1. Ban chỉ đạo trung ương do Thủ tướng Chính phủ đứng đầu.

2. Ban chỉ đạo tỉnh do Chủ tịch Ủy ban nhân dân tỉnh đứng đầu.”

2. Điều 74 được sửa đổi, bổ sung như sau:

"Điều 74. Giám sát công tác phòng, chống tham nhũng

1. Quốc hội giám sát công tác phòng, chống tham nhũng trong phạm vi cả nước."

Điều 2

Luật này có hiệu lực thi hành từ ngày 01 tháng 8 năm 2007.
`

func TestParseKeepsQuotedTextInsideTheInstruction(t *testing.T) {
	doc := mustParse(t, Input{OfficialNumber: "01/2007/QH12", Content: quotedBody})
	base := "vn:law:2007:01-2007-qh12"

	seen := map[string]int{}
	for i := range doc.Provisions {
		seen[doc.Provisions[i].ID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("%s was written %d times, so one occurrence answers for both", id, n)
		}
	}

	clause1 := find(doc, base+":article-1:clause-1")
	if clause1 == nil {
		t.Fatal("the first instruction is missing")
	}
	// The whole quoted article is the new text, and the model that reads this
	// instruction has to see all of it.
	for _, want := range []string{"Điều 73.", "Thủ tướng Chính phủ", "Chủ tịch Ủy ban nhân dân tỉnh"} {
		if !strings.Contains(clause1.Text, want) {
			t.Errorf("the instruction does not carry %q:\n%s", want, clause1.Text)
		}
	}
	if strings.Contains(clause1.Text, "Điều 74") {
		t.Errorf("the first instruction swallowed the second:\n%s", clause1.Text)
	}
	if find(doc, base+":article-73") != nil {
		t.Error("the quoted article became an article of the amending law")
	}
	// Article 2 is outside every quotation and is still this law's own.
	if find(doc, base+":article-2") == nil {
		t.Error("the article after the quotations was swallowed")
	}
}

func TestAQuotationThatNeverClosesCostsOnlyItself(t *testing.T) {
	body := strings.Replace(quotedBody, `1. Quốc hội giám sát công tác phòng, chống tham nhũng trong phạm vi cả nước."`,
		"1. Quốc hội giám sát công tác phòng, chống tham nhũng trong phạm vi cả nước.", 1)
	doc := mustParse(t, Input{OfficialNumber: "01/2007/QH12", Content: body})
	base := "vn:law:2007:01-2007-qh12"

	// The second instruction's quotation is the one left open, so it quotes
	// nothing and the rest of the document is read as it always was.
	if find(doc, base+":article-2") == nil {
		t.Error("an unbalanced quotation swallowed the articles after it")
	}
	// The first instruction's quotation closed, and it is still honoured. This
	// is the whole difference: one bad mark used to cost the document.
	clause1 := find(doc, base+":article-1:clause-1")
	if clause1 == nil {
		t.Fatal("the first instruction is missing")
	}
	if !strings.Contains(clause1.Text, "Thủ tướng Chính phủ") {
		t.Errorf("the quotation that did close lost its text:\n%s", clause1.Text)
	}
	if find(doc, base+":article-73") != nil {
		t.Error("a quotation that closed was read as structure because a later one did not")
	}
}

func TestQuotedLines(t *testing.T) {
	lines := []string{
		`1. Điều 73 được sửa đổi như sau:`,
		`"Điều 73. Ban chỉ đạo`,
		`1. Ban chỉ đạo trung ương.”`,
		`2. Điều 74 được sửa đổi như sau:`,
		`“Điều 74. Giám sát`,
		`1. Quốc hội giám sát.`,
		`Điều 2`,
	}
	got := quotedLines(lines)
	// Lines 2 and 3 sit inside the quotation that closed. The quotation opened
	// on line 5 never closes, so nothing after it is quoted and article 2 is
	// still this law's own.
	want := []bool{false, false, true, false, false, false, false}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d %q: quoted %v, want %v", i, lines[i], got[i], want[i])
		}
	}
}

func TestQuotedLinesPairsNestedAndSameLineMarks(t *testing.T) {
	lines := []string{
		`mở “ ngoài`,
		`mở “ trong`,
		`đóng ” trong`,
		`đóng ” ngoài`,
		`“mở và đóng” trên một dòng`,
		`kết thúc ” không mở gì`,
	}
	got := quotedLines(lines)
	want := []bool{false, true, true, true, false, false}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d %q: quoted %v, want %v", i, lines[i], got[i], want[i])
		}
	}
}

// The corpus has 1,775 documents that repeat exactly one number and 170 that
// repeat more than fifty, and the ones that repeat a point letter are usually
// two adjacent points the drafter lettered the same.
const repeatedBody = `**Số hiệu:** 07/2011/TT-BNV

---

Số: 07/2011/TT-BNV

THÔNG TƯ

Điều 1. Phạm vi điều chỉnh

Thông tư này quy định về hồ sơ.

1. Hồ sơ gồm có:

a) Đơn đề nghị;

a) Bản sao giấy tờ tùy thân.

Điều 2. Hiệu lực

Thông tư này có hiệu lực từ ngày 01 tháng 01 năm 2012.
`

func TestARepeatedNumberGetsItsOwnIdentifier(t *testing.T) {
	doc := mustParse(t, Input{OfficialNumber: "07/2011/TT-BNV", Content: repeatedBody})
	base := "vn:law:2011:07-2011-tt-bnv:article-1:clause-1"

	seen := map[string]int{}
	for i := range doc.Provisions {
		seen[doc.Provisions[i].ID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("%s was written %d times, so one occurrence answers for both", id, n)
		}
	}

	first := find(doc, base+":point-a")
	second := find(doc, base+":point-a~2")
	if first == nil || second == nil {
		t.Fatalf("both points lettered a must survive: first %v second %v", first != nil, second != nil)
	}
	if !strings.Contains(first.Text, "Đơn đề nghị") {
		t.Errorf("the first point a lost its text: %q", first.Text)
	}
	// The text of the second point is the whole reason not to drop it. Under
	// the old parser it overwrote the first one everywhere downstream.
	if !strings.Contains(second.Text, "Bản sao giấy tờ") {
		t.Errorf("the second point a lost its text: %q", second.Text)
	}
	if second.ParentID != first.ParentID {
		t.Errorf("the two points a belong to one clause: %q and %q", first.ParentID, second.ParentID)
	}
	if !law.Repeated(second.ID) || law.Repeated(first.ID) {
		t.Errorf("only the second occurrence is a repeat: %q, %q", first.ID, second.ID)
	}
}

func TestARepeatedNumberTakesItsChildrenWithIt(t *testing.T) {
	// A document that opens Điều 3 twice is usually an annex the walk did not
	// recognise, so the second article has a full set of clauses of its own and
	// every one of them would land on the first article without this.
	body := "**Số hiệu:** 07/2011/TT-BNV\n\n---\n\nSố: 07/2011/TT-BNV\n\nTHÔNG TƯ\n\n" +
		"Điều 3. Trách nhiệm\n\n1. Bộ trưởng chịu trách nhiệm.\n\n" +
		"Điều 4. Hiệu lực\n\nThông tư này có hiệu lực.\n\n" +
		"Điều 3. Tổ chức thực hiện\n\n1. Thủ trưởng đơn vị tổ chức thực hiện.\n"
	doc := mustParse(t, Input{OfficialNumber: "07/2011/TT-BNV", Content: body})
	base := "vn:law:2011:07-2011-tt-bnv"

	second := find(doc, base+":article-3~2")
	if second == nil {
		t.Fatal("the second article 3 is missing")
	}
	clause := find(doc, base+":article-3~2:clause-1")
	if clause == nil {
		t.Fatal("the second article 3 did not keep its own clause")
	}
	if !strings.Contains(clause.Text, "Thủ trưởng đơn vị") {
		t.Errorf("the second article's clause 1 carries the first article's text: %q", clause.Text)
	}
	if first := find(doc, base+":article-3:clause-1"); first == nil ||
		!strings.Contains(first.Text, "Bộ trưởng") {
		t.Errorf("the first article's clause 1 was overwritten: %+v", first)
	}
}

// This is the guarantee, stated once over every shape the package knows about,
// rather than an assertion repeated inside each test that happens to think of
// it. An identifier that names two provisions answers a query with a
// neighbour's text while looking exactly like a correct answer, and the ways to
// produce one are not a list anybody can finish: a quotation read as structure,
// an annex that restarts its numbering, a drafter who letters two points the
// same. So the walk is required to come out the other side with one identifier
// per provision whatever went in.
func TestNoDocumentEverNamesTwoProvisionsTheSame(t *testing.T) {
	bodies := map[string]struct{ number, content string }{
		"code":      {"45/2019/QH14", codeBody},
		"amending":  {"26/2012/QH13", amendingBody},
		"decision":  {"01/2019/QĐ-UBND", decisionBody},
		"sectioned": {"72/2025/NĐ-CP", sectionedBody},
		"quoted":    {"01/2007/QH12", quotedBody},
		"repeated":  {"07/2011/TT-BNV", repeatedBody},
	}
	for name, b := range bodies {
		t.Run(name, func(t *testing.T) {
			doc, err := Parse(Input{OfficialNumber: b.number, IssuingBody: "Ủy ban nhân dân tỉnh Bắc Giang", Content: b.content})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			seen := map[string]bool{}
			for i := range doc.Provisions {
				id := doc.Provisions[i].ID
				if seen[id] {
					t.Errorf("%s names more than one provision", id)
				}
				seen[id] = true
			}
		})
	}
}

func TestMergeKeepsTheDateOneDatasetHasAndTheTextTheOtherHas(t *testing.T) {
	// th1nhng0 carries the commencement date and rougher text, UTS_VLC carries
	// clean text and no date at all, and both publish this instrument.
	breadth := &law.Document{
		ID: "vn:law:2010:46-2010-qh12", OfficialNumber: "46/2010/QH12",
		Title: "Luật Ngân hàng Nhà nước Việt Nam", DocType: "law",
		EffectiveFrom: "01/01/2011", Source: "th1nhng0", Status: "parsed",
		Provisions: []law.Provision{{ID: "vn:law:2010:46-2010-qh12:article-1", Kind: "article", Number: "1"}},
	}
	seed := &law.Document{
		ID: "vn:law:2010:46-2010-qh12", OfficialNumber: "46/2010/QH12",
		Title: "Luật Ngân hàng Nhà nước Việt Nam", DocType: "law",
		Source: "uts_vlc", Status: "parsed",
		Provisions: []law.Provision{
			{ID: "vn:law:2010:46-2010-qh12:article-1", Kind: "article", Number: "1", Text: "Sạch."},
			{ID: "vn:law:2010:46-2010-qh12:article-2", Kind: "article", Number: "2", Text: "Sạch."},
		},
	}

	got := Merge(breadth, seed)
	if got.EffectiveFrom != "01/01/2011" {
		t.Errorf("the date the other dataset published is the only one there is, got %q", got.EffectiveFrom)
	}
	if len(got.Provisions) != 2 || got.Source != "uts_vlc" {
		t.Errorf("the incoming publication has the text, so it keeps it: %d provisions from %q", len(got.Provisions), got.Source)
	}
}

func TestMergeDoesNotLetAMetadataRowDemoteAParsedDocument(t *testing.T) {
	parsed := &law.Document{
		ID: "vn:law:2010:46-2010-qh12", Status: "parsed", Source: "uts_vlc", SourceRef: "abc",
		Provisions: []law.Provision{{ID: "vn:law:2010:46-2010-qh12:article-1", Kind: "article", Number: "1"}},
	}
	metadataOnly := &law.Document{
		ID: "vn:law:2010:46-2010-qh12", Status: "metadata", Source: "th1nhng0",
		EffectiveFrom: "01/01/2011",
	}

	got := Merge(parsed, metadataOnly)
	if got.Status != "parsed" || len(got.Provisions) != 1 {
		t.Errorf("a row with no text must not take the text away, got status %q with %d provisions", got.Status, len(got.Provisions))
	}
	if got.EffectiveFrom != "01/01/2011" {
		t.Errorf("it does still bring its date, got %q", got.EffectiveFrom)
	}
	if got.Source != "uts_vlc" || got.SourceRef != "abc" {
		t.Errorf("the source is whoever the text came from, got %q at %q", got.Source, got.SourceRef)
	}
}

func TestMergeOfNothingIsTheOtherOne(t *testing.T) {
	d := &law.Document{ID: "x"}
	if Merge(nil, d) != d || Merge(d, nil) != d {
		t.Error("a document with nothing to merge into comes back as it is")
	}
}
