package main

import (
	"path/filepath"
	"testing"

	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/relation"
	"github.com/tamnd/luatdo/store"
)

func TestCandidatesAreOneSetPerProvision(t *testing.T) {
	// A term mentioned four times in one clause is one candidate. Offering it
	// four times would spend prompt on repetition and invite the model to relate
	// a concept to itself.
	in := &inputs{
		labels: map[string]string{"t1": "giấy phép xây dựng", "t2": "giấy chứng nhận"},
		kinds:  relation.Kinds{"t1": "artifact", "t2": "artifact"},
	}
	report := &concept.MentionReport{DocID: "d1", Mentions: []concept.Mention{
		{ProvisionID: "p1", TermUseID: "t2"},
		{ProvisionID: "p1", TermUseID: "t1"},
		{ProvisionID: "p1", TermUseID: "t1"},
		{ProvisionID: "p2", TermUseID: "t1"},
		{ProvisionID: "p2", Method: concept.MethodUnresolved},
	}}

	got := candidates(report, in)
	if len(got["p1"]) != 2 {
		t.Fatalf("p1 = %+v, want each concept offered once", got["p1"])
	}
	if got["p1"][0].ID != "t1" {
		t.Errorf("p1 = %+v, the order is not fixed so two runs build different prompts", got["p1"])
	}
	if got["p1"][0].LabelVI != "giấy phép xây dựng" || got["p1"][0].Kind != "artifact" {
		t.Errorf("candidate = %+v, a model offered a bare identifier cannot read anything from it", got["p1"][0])
	}
	if len(got["p2"]) != 1 {
		// An unresolved mention is correct output from the linker and it is not a
		// concept, so it is not offered as one.
		t.Errorf("p2 = %+v, an unresolved mention was offered as a concept", got["p2"])
	}
}

func TestRelationTextsReadsOnlyTheDocumentsTheEdgesTouch(t *testing.T) {
	// The corpus is a hundred thousand documents and the layer is not.
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	doc := &law.Document{
		ID: "vn:law:2019:1-2019-qh14", DocType: "law",
		Provisions: []law.Provision{
			{ID: "vn:law:2019:1-2019-qh14:article-1", Kind: "article", Number: "1", Text: "Giấy phép xây dựng."},
		},
	}
	if err := store.WriteJSON(filepath.Join(s.Docs(), law.FileName(doc.ID)), doc); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	texts := relationTexts(s, []relation.Edge{{
		FromID: "a", ToID: "b", Type: relation.Requires,
		Evidence: []relation.Evidence{
			{ProvisionID: "vn:law:2019:1-2019-qh14:article-1", DocID: doc.ID},
			{ProvisionID: "vn:law:2020:9-2020-qh14:article-1", DocID: "vn:law:2020:9-2020-qh14"},
		},
	}})
	if texts == nil {
		t.Fatal("no texts, so the checker would skip the invariant that catches a quote in the wrong provision")
	}
	if got, ok := texts("vn:law:2019:1-2019-qh14:article-1"); !ok || got != "Giấy phép xây dựng." {
		t.Errorf("text = %q ok = %v", got, ok)
	}
	// A document the store never parsed is a gap and not a crash, and the
	// checker reports the provision as absent from the corpus, which it is.
	if _, ok := texts("vn:law:2020:9-2020-qh14:article-1"); ok {
		t.Error("a provision from a document nobody parsed came back with text")
	}
}

func TestRelationTextsOnAnEmptyLayerReadsNothing(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if texts := relationTexts(s, nil); texts != nil {
		t.Error("an empty layer produced a corpus, and Check would pass invariant 3 on nothing")
	}
}

func TestCitationLookupAnswersTheCrossInstrumentQuestion(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	links := []cite.Link{
		{FromDoc: "a", ToDoc: "b", Kind: "cites"},
		{FromDoc: "a", ToDoc: "", ToNumber: "99/2020/QH14", Kind: "cites"},
	}
	if err := store.WriteJSON(filepath.Join(s.Cite(), "a.json"), links); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	cites, err := citationLookup(s)
	if err != nil {
		t.Fatalf("citationLookup: %v", err)
	}
	if !cites("a", "b") {
		t.Error("a citation the corpus holds was not found")
	}
	// An unresolved citation names no document, so it cannot answer whether one
	// instrument cites another, and counting it would turn the finding into noise.
	if cites("b", "a") || cites("a", "vn:law:2020:99-2020-qh14") {
		t.Error("a citation nobody resolved was reported as a citation")
	}
}
