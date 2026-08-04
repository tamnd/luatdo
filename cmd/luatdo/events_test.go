package main

import (
	"testing"

	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/event"
)

func TestEventCandidatesOffersEachConceptOncePerProvision(t *testing.T) {
	in := &eventInputs{
		labels: map[string]string{"c2": "người lao động", "c1": "người sử dụng lao động"},
		kinds:  map[string]string{"c2": "actor", "c1": "actor"},
	}
	report := &concept.MentionReport{DocID: "d1", Mentions: []concept.Mention{
		{ProvisionID: "p1", TermUseID: "c2"},
		{ProvisionID: "p1", TermUseID: "c1"},
		// The same concept named three times in one paragraph is one candidate,
		// and a prompt that lists it three times is paying for the repetition.
		{ProvisionID: "p1", TermUseID: "c2"},
		{ProvisionID: "p2", TermUseID: "c1"},
		{ProvisionID: "p2", TermUseID: ""},
	}}
	got := eventCandidates(report, in)
	if len(got["p1"]) != 2 {
		t.Fatalf("p1: got %+v, want two candidates", got["p1"])
	}
	if got["p1"][0].ID != "c1" {
		t.Errorf("candidates came back in %+v, and a prompt has to be the same on two machines", got["p1"])
	}
	if got["p1"][0].LabelVI != "người sử dụng lao động" {
		t.Errorf("the label was not carried: %+v", got["p1"][0])
	}
	if len(got["p2"]) != 1 {
		t.Errorf("p2: got %+v, want the unresolved mention left out", got["p2"])
	}
}

func TestKeepLinksDropsANormPointingAtAnActThatDidNotSurvive(t *testing.T) {
	events := []event.Event{{ID: "a"}}
	got, dropped := keepLinks([]event.Link{
		{StatementID: "s2", EventID: "a", Kind: event.LinkAction},
		{StatementID: "s1", EventID: "gone", Kind: event.LinkSanction},
		{StatementID: "s1", EventID: "a", Kind: event.LinkAction},
	}, events)
	if dropped != 1 {
		t.Fatalf("dropped: got %d, want the link to the act the fold does not hold", dropped)
	}
	if len(got) != 2 || got[0].StatementID != "s1" {
		t.Errorf("links: %+v, want them sorted so a rebuild writes the same file", got)
	}
}

func TestCountProvisionsCountsTheProvisionAndNotTheAct(t *testing.T) {
	occurrences := []event.Occurrence{
		{Evidence: event.Evidence{ProvisionID: "p1"}},
		{Evidence: event.Evidence{ProvisionID: "p1"}},
		{Evidence: event.Evidence{ProvisionID: "p2"}},
	}
	if got := countProvisions(occurrences); got != 2 {
		t.Errorf("provisions: got %d, want 2", got)
	}
}

func TestReadSightingsCollectsWhatEveryDocumentProduced(t *testing.T) {
	dir := t.TempDir()
	for _, doc := range []string{"vn:doc:b", "vn:doc:a"} {
		s := event.Sighting{
			DocID: doc,
			Occurrences: []event.Occurrence{{
				EventID: event.ID("SUBMIT", "nộp hồ sơ"), Class: "SUBMIT", LabelVI: "nộp hồ sơ",
				Evidence: event.Evidence{ProvisionID: doc + ":art:1", DocID: doc, Quote: "nộp hồ sơ"},
			}},
			Links: []event.Link{{StatementID: "s1", EventID: "a", Kind: event.LinkAction}},
		}
		if err := event.WriteSighting(dir, s); err != nil {
			t.Fatalf("WriteSighting: %v", err)
		}
	}
	occurrences, chains, links, docs, err := readSightings(dir)
	if err != nil {
		t.Fatalf("readSightings: %v", err)
	}
	if docs != 2 || len(occurrences) != 2 || len(links) != 2 || len(chains) != 0 {
		t.Errorf("got %d documents, %d acts, %d chains, %d links", docs, len(occurrences), len(chains), len(links))
	}
}

func TestTheQueueHoldsTheDocumentsBothLayersReached(t *testing.T) {
	// The pass reads a provision that has a linked concept or a trusted norm, so
	// the document queue has to be the union of the two. It was the mention
	// documents alone, which skipped every instrument the norm layer reached and
	// the concept layer did not, and reported each of them as done.
	got := mergeDocs([]string{"vn:law:b", "vn:law:a", "vn:law:b"}, map[string]bool{"vn:law:c": true, "vn:law:a": true})
	want := []string{"vn:law:a", "vn:law:b", "vn:law:c"}
	if len(got) != len(want) {
		t.Fatalf("queue = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("queue = %v, want %v", got, want)
		}
	}
}

func TestEventGoldCandidatePathKeepsCampaignsApart(t *testing.T) {
	if eventCandidatePath("") == eventCandidatePath("lao-dong") {
		t.Error("a labour draw would overwrite the corpus wide one, and the annotations under it would be scored against the wrong sample")
	}
}
