package event

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSightingSurvivesARoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Sighting{
		DocID:       "vn:doc:luat-doanh-nghiep-2020",
		Occurrences: []Occurrence{sighting("p1", "vn:doc:luat-doanh-nghiep-2020", "SUBMIT", "nộp hồ sơ")},
		Chains:      []Chain{chainIn("p1", "vn:doc:luat-doanh-nghiep-2020", "a", "b")},
		Links:       []Link{{StatementID: "s1", ProvisionID: "p1", EventID: "a", Kind: LinkAction}},
	}
	if err := WriteSighting(dir, want); err != nil {
		t.Fatalf("WriteSighting: %v", err)
	}
	got, err := ReadSighting(dir, want.DocID)
	if err != nil {
		t.Fatalf("ReadSighting: %v", err)
	}
	if len(got.Occurrences) != 1 || got.Occurrences[0].EventID != want.Occurrences[0].EventID {
		t.Errorf("occurrences: got %+v", got.Occurrences)
	}
	if len(got.Chains) != 1 || len(got.Links) != 1 {
		t.Errorf("chains and links did not survive: %+v", got)
	}
	if strings.ContainsAny(filepath.Base(SightingPath(dir, want.DocID)), `:`) {
		t.Errorf("the file name holds a colon, which Windows will not open: %s", SightingPath(dir, want.DocID))
	}
}

func TestReadingADocumentNobodyReadIsNotAnError(t *testing.T) {
	got, err := ReadSighting(t.TempDir(), "vn:doc:nothing")
	if err != nil {
		t.Fatalf("ReadSighting: %v", err)
	}
	if len(got.Occurrences) != 0 {
		t.Errorf("something came back from a store nobody wrote: %+v", got)
	}
}

func TestADocumentThatNamesNoActStillLeavesAFile(t *testing.T) {
	// Otherwise the document is back in the queue forever, and a corpus of
	// definition clauses is read again on every run at the full price.
	dir := t.TempDir()
	if err := WriteSighting(dir, Sighting{DocID: "vn:doc:d1"}); err != nil {
		t.Fatalf("WriteSighting: %v", err)
	}
	if _, err := os.Stat(SightingPath(dir, "vn:doc:d1")); err != nil {
		t.Errorf("a document read and found empty left no record: %v", err)
	}
}

func TestEachSightingReadsOnlySightings(t *testing.T) {
	dir := t.TempDir()
	for _, doc := range []string{"vn:doc:b", "vn:doc:a"} {
		if err := WriteSighting(dir, Sighting{DocID: doc, Occurrences: []Occurrence{sighting("p1", doc, "SUBMIT", "nộp hồ sơ")}}); err != nil {
			t.Fatalf("WriteSighting: %v", err)
		}
	}
	if err := WriteEvents(dir, []Event{{ID: "vn:event:submit:nop-ho-so"}}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}
	if err := WriteRegistry(dir, SeedRegistry(1)); err != nil {
		t.Fatalf("WriteRegistry: %v", err)
	}
	var seen []string
	if err := EachSighting(dir, func(s Sighting) error {
		seen = append(seen, s.DocID)
		return nil
	}); err != nil {
		t.Fatalf("EachSighting: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("documents: got %v, want the two sightings and neither derived file", seen)
	}
	if seen[0] != "vn:doc:a" {
		t.Errorf("documents came back in %v, and a fold has to be the same on two machines", seen)
	}
}

func TestEachSightingOverAStoreNobodyWroteIsQuiet(t *testing.T) {
	err := EachSighting(filepath.Join(t.TempDir(), "missing"), func(Sighting) error {
		t.Error("something was visited in a directory that does not exist")
		return nil
	})
	if err != nil {
		t.Errorf("EachSighting: %v", err)
	}
}

func TestDerivedFilesAreRewrittenWholeAndRemovedWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := WriteEvents(dir, []Event{{ID: "a"}, {ID: "b"}}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}
	if err := WriteEvents(dir, []Event{{ID: "a"}}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}
	got, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("events: got %d, want 1, because a derived file is rewritten and not appended", len(got))
	}
	if err := WriteEvents(dir, nil); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, EventsFile)); !os.IsNotExist(err) {
		t.Error("an empty derived file was left on disk, where it reads as a result somebody computed")
	}
}

func TestChainsAndLinksRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := WriteChains(dir, []Chain{chainIn("p1", "d1", "a", "b")}); err != nil {
		t.Fatalf("WriteChains: %v", err)
	}
	if err := WriteLinks(dir, []Link{{StatementID: "s1", EventID: "a", Kind: LinkSanction}}); err != nil {
		t.Fatalf("WriteLinks: %v", err)
	}
	chains, err := ReadChains(dir)
	if err != nil || len(chains) != 1 {
		t.Fatalf("ReadChains: %v, %+v", err, chains)
	}
	links, err := ReadLinks(dir)
	if err != nil || len(links) != 1 || links[0].Kind != LinkSanction {
		t.Fatalf("ReadLinks: %v, %+v", err, links)
	}
}

func TestRegistryFallsBackToTheSeed(t *testing.T) {
	got, err := ReadRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("ReadRegistry: %v", err)
	}
	if len(got.Classes) == 0 {
		t.Error("a store that never ran the pass came back with no vocabulary at all")
	}
}

func TestRegistryIsPinnedSoAnOldLayerCanStillSayWhatItCited(t *testing.T) {
	dir := t.TempDir()
	r := SeedRegistry(2)
	r.Classes = append(r.Classes, Class{ID: "CHUYEN_NHUONG_CO_PHAN", Definition: "Một bên chuyển quyền sở hữu cổ phần cho bên khác."})
	if err := WriteRegistry(dir, r); err != nil {
		t.Fatalf("WriteRegistry: %v", err)
	}
	got, err := ReadRegistry(dir)
	if err != nil {
		t.Fatalf("ReadRegistry: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("version: got %d, want 2", got.Version)
	}
	if got.Class("CHUYEN_NHUONG_CO_PHAN") == nil {
		t.Error("the reviewed class did not survive the round trip")
	}
}

func TestProposeCollectsTheInventedClassesWithTheirEvidence(t *testing.T) {
	events := []Event{
		{
			ID: "vn:event:chuyen_nhuong_co_phan:x", Class: "CHUYEN_NHUONG_CO_PHAN", LabelVI: "chuyển nhượng cổ phần",
			Definition: "Một bên chuyển quyền sở hữu cổ phần cho bên khác.",
			Evidence: []Evidence{
				{ProvisionID: "p1", DocID: "d1", Quote: "chuyển nhượng cổ phần", AsWritten: "chuyển nhượng"},
				{ProvisionID: "p2", DocID: "d2", Quote: "chuyển nhượng cổ phần phổ thông", AsWritten: "chuyển nhượng cổ phần"},
			},
		},
		{ID: "vn:event:submit:nop-ho-so", Class: "SUBMIT", LabelVI: "nộp hồ sơ", Evidence: []Evidence{{ProvisionID: "p3", DocID: "d1"}}},
	}
	got := Propose(events, SeedRegistry(1))
	if len(got) != 1 {
		t.Fatalf("proposals: got %d, want only the invented class: %+v", len(got), got)
	}
	p := got[0]
	if p.Instances != 2 || p.Docs != 2 {
		t.Errorf("counts: got %d instances in %d documents, want 2 in 2", p.Instances, p.Docs)
	}
	if p.Definition == "" {
		t.Error("the proposal carries no definition, so a reviewer has a name and nothing to judge it on")
	}
	if len(p.AsWritten) != 2 || len(p.Examples) != 2 {
		t.Errorf("the spread was lost: %+v", p)
	}
}

func TestTallyCountsWhatTheLayerHolds(t *testing.T) {
	events := []Event{
		{ID: "a", Class: "SUBMIT", Status: StatusCanonical, Participants: []Participant{{Role: RoleAgent}, {Role: RoleObject}}},
		{ID: "b", Class: "CHUYEN_NHUONG_CO_PHAN", Status: StatusProvisional},
		{ID: "c", Class: "CHUYEN_NHUONG_CO_PHAN", Status: StatusProvisional},
	}
	chains := []Chain{{Type: Precedes, Status: StatusCanonical}, {Type: Triggers, Status: StatusProvisional}}
	links := []Link{{Kind: LinkAction}, {Kind: LinkAction}, {Kind: LinkSanction}}
	got := Tally(events, chains, links, SeedRegistry(1))
	if got.Events != 3 || got.EventsCanonical != 1 || got.EventsProvisional != 2 {
		t.Errorf("events: %+v", got)
	}
	if got.UnknownClasses != 1 {
		t.Errorf("invented classes: got %d, want 1, counted by class and not by node", got.UnknownClasses)
	}
	if got.Participants != 2 || got.ByRole[RoleAgent] != 1 {
		t.Errorf("participants: %+v", got)
	}
	if got.Chains != 2 || got.ChainsCanonical != 1 {
		t.Errorf("chains: %+v", got)
	}
	if got.ActionLinks != 2 || got.SanctionLinks != 1 {
		t.Errorf("links: %+v", got)
	}
	if !strings.Contains(got.String(), "provisional") {
		t.Errorf("the report hides how much is provisional: %s", got.String())
	}
}

func TestSummaryRoundTrips(t *testing.T) {
	dir := t.TempDir()
	if got, err := ReadSummary(dir); err != nil || got != nil {
		t.Fatalf("a store nobody ran: %+v, %v", got, err)
	}
	want := Summary{Documents: 3, Provisions: 40, Counts: Counts{Events: 7}, Direction: DirectionScore{Chains: 2, Agreed: 2}}
	if err := WriteSummary(dir, want); err != nil {
		t.Fatalf("WriteSummary: %v", err)
	}
	got, err := ReadSummary(dir)
	if err != nil {
		t.Fatalf("ReadSummary: %v", err)
	}
	if got == nil || got.Documents != 3 || got.Counts.Events != 7 || got.Direction.Agreed != 2 {
		t.Errorf("summary: got %+v, want %+v", got, want)
	}
}
