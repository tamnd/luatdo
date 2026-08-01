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
