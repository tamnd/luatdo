package main

import (
	"path/filepath"
	"testing"

	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/store"
	"github.com/tamnd/luatdo/temporal"
)

func TestAmendingWordsFiltersInFrontOfTheModel(t *testing.T) {
	// The filter is loose on purpose. Being told an instruction is not there
	// costs a model call, and skipping one that is costs a missing amendment.
	yes := []string{
		"Sửa đổi, bổ sung khoản 2 Điều 15 như sau:",
		"Bãi bỏ khoản 3 Điều 22.",
		"Thay thế cụm từ này bằng cụm từ kia.",
		"Điều này hết hiệu lực kể từ ngày 01 tháng 01 năm 2025.",
		"Ngưng hiệu lực thi hành Điều 5.",
	}
	for _, text := range yes {
		if !amendingWords(text) {
			t.Errorf("an amending instruction was filtered out before the model saw it: %q", text)
		}
	}
	no := []string{
		"Luật này quy định về hợp đồng lao động.",
		"Người sử dụng lao động phải trả lương đúng hạn.",
	}
	for _, text := range no {
		if amendingWords(text) {
			t.Errorf("a provision with no instruction in it would be paid for: %q", text)
		}
	}
}

func TestAmendingProvisionsPicksOnlyTheOnesWorthAsking(t *testing.T) {
	doc := &law.Document{
		ID: "vn:law:2022:10-2022-qh15", DocType: "law",
		Provisions: []law.Provision{
			{ID: "vn:law:2022:10-2022-qh15:article-1", Kind: "article", Number: "1",
				Text: "Sửa đổi, bổ sung khoản 2 Điều 15 của Bộ luật Lao động số 45/2019/QH14."},
			{ID: "vn:law:2022:10-2022-qh15:article-2", Kind: "article", Number: "2",
				Text: "Luật này có hiệu lực thi hành."},
		},
	}
	got := amendingProvisions(doc)
	if len(got) != 1 {
		t.Fatalf("picked %d provisions, want the one holding an instruction", len(got))
	}
	if got[0].Number != "1" {
		t.Errorf("picked article %s", got[0].Number)
	}
}

func TestAmendingLinksReadsOnlyAmendsEdges(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	links := []cite.Link{
		{FromDoc: "a", ToDoc: "b", Kind: "amends", ToNumber: "45/2019/QH14"},
		{FromDoc: "a", ToDoc: "b", Kind: "amends", ToNumber: "45/2019/QH14"},
		{FromDoc: "a", ToDoc: "c", Kind: "cites", ToNumber: "10/2020/QH14"},
		{FromDoc: "a", ToNumber: "99/2020/QH14", Kind: "amends"}, // unresolved
	}
	if err := store.WriteJSON(filepath.Join(s.Cite(), "a.json"), links); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	got, err := amendingLinks(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got["a"]) != 1 || got["a"][0] != "b" {
		t.Errorf("the amendment graph is %v, want b once and nothing else", got["a"])
	}
}

func TestConsolidatesMatchesByTitle(t *testing.T) {
	target := &law.Document{ID: "vn:law:2019:45-2019-qh14", OfficialNumber: "45/2019/QH14", Title: "Bộ luật Lao động"}
	other := &law.Document{ID: "vn:law:2014:50-2014-qh13", OfficialNumber: "50/2014/QH13", Title: "Luật Xây dựng"}
	docs := []*law.Document{target, other}
	consolidated := &law.Document{
		ID: "vn:law:2022:1-2022-vbhn-vpqh", Title: "Văn bản hợp nhất Bộ luật Lao động",
	}
	if !consolidates(consolidated, target.ID, docs, nil) {
		t.Error("a consolidated text naming the instrument in its title was not matched to it")
	}
	if consolidates(consolidated, other.ID, docs, nil) {
		t.Error("a consolidated text was matched to an instrument it does not name")
	}
}

func TestConsolidatesFallsBackToTheDatasetRelation(t *testing.T) {
	// This is the real shape of văn bản hợp nhất số 68/2026/VBHN-NĐ-BCT. Its
	// title repeats the subject of the decree it consolidates and names neither
	// the number nor the words Nghị định, so the title match cannot find it.
	target := &law.Document{
		ID: "vn:law:2025:72-2025-nd-cp", OfficialNumber: "72/2025/NĐ-CP",
		Title: "Nghị định số 72/2025/NĐ-CP Quy định về cơ chế, thời gian điều chỉnh giá bán lẻ điện bình quân",
	}
	docs := []*law.Document{target}
	consolidated := &law.Document{
		ID:    "vn:law:2026:68-2026-vbhn-nd-bct",
		Title: "Văn bản hợp nhất số 68/2026/VBHN-NĐ-BCT Nghị định quy định về cơ chế, thời gian điều chỉnh giá bán lẻ điện bình quân",
	}
	if consolidates(consolidated, target.ID, docs, nil) {
		t.Fatal("the title alone matched, so this test no longer covers the case it was written for")
	}
	of := map[string][]string{consolidated.ID: {target.ID}}
	if !consolidates(consolidated, target.ID, docs, of) {
		t.Error("a Hợp nhất relation from the dataset did not match the consolidated text to its instrument")
	}
	if consolidates(consolidated, "vn:law:2014:50-2014-qh13", docs, of) {
		t.Error("a consolidated text was matched to an instrument no relation names")
	}
}

func TestConsolidationLinksReadTheLabelNotTheKind(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	links := []cite.Link{
		{FromDoc: "vbhn", ToDoc: "base", Kind: "cites", Method: "official", Snippet: "Hợp nhất"},
		{FromDoc: "vbhn", ToDoc: "other", Kind: "cites", Method: "official", Snippet: "Căn cứ"},
	}
	if err := store.WriteJSON(filepath.Join(s.Cite(), "vbhn.json"), links); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	got, err := consolidationLinks(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got["vbhn"]) != 1 || got["vbhn"][0] != "base" {
		t.Errorf("consolidation links are %v, want base once and nothing the Căn cứ edge names", got["vbhn"])
	}
}

func TestSortedKeysIsDeterministic(t *testing.T) {
	got := sortedKeys(map[string]int{"b": 1, "a": 2, "c": 3})
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedKeys = %v", got)
		}
	}
}

func TestRefusedDocsCountsDocumentsNotReasons(t *testing.T) {
	layer := &temporal.Layer{Quarantined: []temporal.Operation{
		{TargetDoc: "a", Quarantine: temporal.QuarantineCollidingParse},
		{TargetDoc: "a", Quarantine: temporal.QuarantineCollidingParse},
		{TargetDoc: "b", Quarantine: temporal.QuarantineUndatedDocument},
		// An unresolved target is a reading problem, not a refused document.
		{TargetDoc: "c", Quarantine: temporal.QuarantineUnresolvedTarget},
	}}
	if got := refusedDocs(layer); got != 2 {
		t.Errorf("refusedDocs = %d, want the two documents that were refused", got)
	}
}
