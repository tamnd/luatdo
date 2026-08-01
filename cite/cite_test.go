package cite

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/law"
)

func doc(id, number string, provisions ...law.Provision) *law.Document {
	return &law.Document{ID: id, OfficialNumber: number, Provisions: provisions}
}

func TestResolve(t *testing.T) {
	source := doc("vn:law:2019:45-2019-qh14", "45/2019/QH14", law.Provision{
		ID:   "vn:law:2019:45-2019-qh14:article-220:clause-2",
		Kind: "clause",
		Text: "Bộ luật Lao động số 10/2012/QH13 hết hiệu lực kể từ ngày Bộ luật này có hiệu lực thi hành.",
	})
	target := doc("vn:law:2012:10-2012-qh13", "10/2012/QH13")
	index := Index([]*law.Document{source, target})

	links := Resolve(source, index)
	if len(links) != 1 {
		t.Fatalf("links = %d, want 1", len(links))
	}
	l := links[0]
	if l.ToNumber != "10/2012/QH13" || l.ToDoc != target.ID {
		t.Errorf("link = %+v", l)
	}
	if l.Kind != "cites" || l.Method != "pattern" {
		t.Errorf("kind %q method %q", l.Kind, l.Method)
	}
	if l.FromProvision != source.Provisions[0].ID {
		t.Errorf("from provision = %q", l.FromProvision)
	}
	if !strings.Contains(l.Snippet, "10/2012/QH13") {
		t.Errorf("snippet = %q", l.Snippet)
	}
}

func TestIndexRefusesAmbiguousNumber(t *testing.T) {
	longAn := doc("vn:law:2024:01-2024-qd-ubnd:ubnd-tinh-long-an", "01/2024/QĐ-UBND")
	langSon := doc("vn:law:2024:01-2024-qd-ubnd:ubnd-tinh-lang-son", "01/2024/QĐ-UBND")
	law2019 := doc("vn:law:2019:45-2019-qh14", "45/2019/QH14")
	index := Index([]*law.Document{longAn, langSon, law2019})

	if id, ok := index["01/2024/QĐ-UBND"]; ok {
		t.Errorf("the number resolved to %q, but two provinces issued it", id)
	}
	if index["45/2019/QH14"] != law2019.ID {
		t.Errorf("a number one document holds still resolves, got %q", index["45/2019/QH14"])
	}

	// A citation of the ambiguous number is reported with no target rather than
	// pointed at whichever province happened to be indexed last.
	source := doc("vn:law:2024:09-2024-qd-ubnd:ubnd-tinh-long-an", "09/2024/QĐ-UBND", law.Provision{
		ID:   "vn:law:2024:09-2024-qd-ubnd:ubnd-tinh-long-an:article-1",
		Kind: "article",
		Text: "Bãi bỏ Quyết định số 01/2024/QĐ-UBND.",
	})
	links := Resolve(source, index)
	if len(links) != 1 || links[0].ToDoc != "" {
		t.Fatalf("links = %+v, want one unresolved citation", links)
	}
}

func TestResolveAmends(t *testing.T) {
	source := doc("vn:law:2012:26-2012-qh13", "26/2012/QH13", law.Provision{
		ID:   "vn:law:2012:26-2012-qh13:article-1",
		Kind: "article",
		Text: "Sửa đổi, bổ sung một số điều của Luật Thuế thu nhập cá nhân số 04/2007/QH12.",
	})
	target := doc("vn:law:2007:04-2007-qh12", "04/2007/QH12")
	links := Resolve(source, Index([]*law.Document{source, target}))
	if len(links) != 1 {
		t.Fatalf("links = %d, want 1", len(links))
	}
	if links[0].Kind != "amends" {
		t.Errorf("kind = %q, want amends", links[0].Kind)
	}
}

func TestResolveSkipsSelfCitation(t *testing.T) {
	source := doc("vn:law:2019:45-2019-qh14", "45/2019/QH14", law.Provision{
		ID:   "vn:law:2019:45-2019-qh14:article-1",
		Text: "Căn cứ Bộ luật số 45/2019/QH14.",
	})
	if links := Resolve(source, Index([]*law.Document{source})); len(links) != 0 {
		t.Errorf("self citation produced %d links", len(links))
	}
}

func TestResolveUnresolvedStaysEmpty(t *testing.T) {
	source := doc("vn:law:2019:45-2019-qh14", "45/2019/QH14", law.Provision{
		ID:   "vn:law:2019:45-2019-qh14:article-1",
		Text: "Theo Nghị định số 99/2099/NĐ-CP.",
	})
	links := Resolve(source, Index([]*law.Document{source}))
	if len(links) != 1 {
		t.Fatalf("links = %d, want 1", len(links))
	}
	if links[0].ToDoc != "" {
		t.Errorf("unresolved link got target %q", links[0].ToDoc)
	}
	if links[0].ToNumber != "99/2099/NĐ-CP" {
		t.Errorf("to number = %q", links[0].ToNumber)
	}
}

func TestResolveDeduplicates(t *testing.T) {
	source := doc("vn:law:2019:45-2019-qh14", "45/2019/QH14", law.Provision{
		ID:   "vn:law:2019:45-2019-qh14:article-1",
		Text: "Luật số 10/2012/QH13 và cũng theo Luật số 10/2012/QH13.",
	})
	target := doc("vn:law:2012:10-2012-qh13", "10/2012/QH13")
	links := Resolve(source, Index([]*law.Document{source, target}))
	if len(links) != 1 {
		t.Errorf("duplicate citation in one provision produced %d links", len(links))
	}
}

func TestMergePrefersOfficialMetadata(t *testing.T) {
	pattern := []Link{
		{FromDoc: "a", FromProvision: "a:article-1", ToNumber: "10/2012/QH13", ToDoc: "b", Kind: "amends", Method: "pattern"},
		{FromDoc: "a", FromProvision: "a:article-2", ToNumber: "11/2013/QH13", ToDoc: "c", Kind: "cites", Method: "pattern"},
		{FromDoc: "a", FromProvision: "a:article-3", ToNumber: "99/2099/NĐ-CP", Kind: "cites", Method: "pattern"},
	}
	official := []Link{
		{FromDoc: "a", ToDoc: "b", Kind: "amends", Method: "official"},
	}
	got := Merge(pattern, official)
	if len(got) != 3 {
		t.Fatalf("merged = %d links, want the official one plus the two it does not cover", len(got))
	}
	if got[0].Method != "official" {
		t.Errorf("first link = %+v, official metadata leads", got[0])
	}
	for _, l := range got[1:] {
		if l.ToDoc == "b" {
			t.Errorf("%+v duplicates an edge the dataset already states", l)
		}
	}
	if got[2].ToDoc != "" || got[2].ToNumber != "99/2099/NĐ-CP" {
		t.Errorf("unresolved pattern link = %+v, it must survive the merge", got[2])
	}
}

func TestMergeWithoutOfficialMetadata(t *testing.T) {
	pattern := []Link{{FromDoc: "a", ToDoc: "b", Kind: "cites", Method: "pattern"}}
	got := Merge(pattern, nil)
	if len(got) != 1 || got[0].Method != "pattern" {
		t.Errorf("merged = %+v, a corpus with no official graph keeps every pattern hit", got)
	}
}
