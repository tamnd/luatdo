package term

import (
	"testing"

	"github.com/tamnd/luatdo/law"
)

func interpretationDoc(clauses ...string) *law.Document {
	doc := &law.Document{ID: "vn:law:2019:45-2019-qh14", Status: "parsed"}
	art := law.Provision{
		ID:      "vn:law:2019:45-2019-qh14:article-3",
		Kind:    "article",
		Number:  "3",
		Heading: "Giải thích từ ngữ",
	}
	doc.Provisions = append(doc.Provisions, art)
	for i, text := range clauses {
		doc.Provisions = append(doc.Provisions, law.Provision{
			ID:       law.ProvisionID(art.ID, "clause", string(rune('1'+i))),
			ParentID: art.ID,
			Kind:     "clause",
			Text:     text,
		})
	}
	return doc
}

func TestExtract(t *testing.T) {
	doc := interpretationDoc(
		"Người lao động là người làm việc cho người sử dụng lao động theo thỏa thuận.",
		"Bên ký kết Việt Nam bao gồm: các cơ quan nhà nước ở trung ương.",
		"Thành phố được thí điểm thành lập Quỹ đầu tư mạo hiểm.",
	)
	defs := Extract(doc)
	if len(defs) != 2 {
		t.Fatalf("definitions = %d, want 2", len(defs))
	}
	first := defs[0]
	if first.Term != "Người lao động" {
		t.Errorf("term = %q", first.Term)
	}
	if first.TermID != "vn:term:nguoi-lao-dong" {
		t.Errorf("term id = %q", first.TermID)
	}
	if first.Connective != "la" {
		t.Errorf("connective = %q", first.Connective)
	}
	if defs[1].Term != "Bên ký kết Việt Nam" || defs[1].Connective != "bao-gom" {
		t.Errorf("second definition = %+v", defs[1])
	}
}

func TestExtractTermAcrossLineBreak(t *testing.T) {
	doc := interpretationDoc("Hoạt động ngân hàng\nlà việc kinh doanh tiền tệ.")
	defs := Extract(doc)
	if len(defs) != 1 {
		t.Fatalf("definitions = %d, want 1", len(defs))
	}
	if defs[0].Term != "Hoạt động ngân hàng" {
		t.Errorf("term = %q", defs[0].Term)
	}
}

func TestExtractIgnoresOtherArticles(t *testing.T) {
	doc := &law.Document{ID: "vn:law:2019:45-2019-qh14", Status: "parsed", Provisions: []law.Provision{
		{ID: "a1", Kind: "article", Heading: "Phạm vi điều chỉnh"},
		{ID: "a1:c1", ParentID: "a1", Kind: "clause", Text: "Bộ luật này là văn bản quan trọng."},
	}}
	if defs := Extract(doc); len(defs) != 0 {
		t.Errorf("non-interpretation article produced %d definitions", len(defs))
	}
}
