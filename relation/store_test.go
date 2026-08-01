package relation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stored(from, to, doc string) Edge {
	return Edge{
		FromID: from, ToID: to, Type: Requires,
		Status: StatusProvisional, Source: SourceProvision, Why: WhySingleSupport,
		Confidence: 0.8, SupportCount: 1, SupportDocs: 1,
		Evidence: []Evidence{{ProvisionID: doc + ":d1", DocID: doc, Quote: "q", CharEnd: 1}},
	}
}

func TestSightingsAreOneFilePerDocument(t *testing.T) {
	// Extraction is a long job against a metered service, so a document that
	// fails has to leave no artifact, which is what puts it back in the queue.
	dir := t.TempDir()
	want := []Edge{stored("c1", "c2", "vbpl:1"), stored("c3", "c4", "vbpl:1")}
	if err := WriteSightings(dir, "vbpl:1", want); err != nil {
		t.Fatalf("WriteSightings: %v", err)
	}
	if err := WriteSightings(dir, "vbpl:2", []Edge{stored("c5", "c6", "vbpl:2")}); err != nil {
		t.Fatalf("WriteSightings: %v", err)
	}

	got, err := ReadSightings(dir, "vbpl:1")
	if err != nil {
		t.Fatalf("ReadSightings: %v", err)
	}
	if len(got) != 2 || got[0].FromID != "c1" || got[1].ToID != "c4" {
		t.Fatalf("edges = %+v", got)
	}
	if got[0].Evidence[0].Quote != "q" {
		t.Errorf("the evidence did not survive the round trip: %+v", got[0].Evidence)
	}

	// The colon in a document identifier is not a path separator anywhere the
	// campaign runs, and the servers include Windows machines.
	name := filepath.Base(SightingPath(dir, "vbpl:1"))
	if strings.ContainsAny(name, `:/\`) {
		t.Errorf("file name = %q, which does not open on every machine in the fleet", name)
	}
}

func TestRewritingOneDocumentReplacesItRatherThanAppending(t *testing.T) {
	dir := t.TempDir()
	if err := WriteSightings(dir, "vbpl:1", []Edge{stored("c1", "c2", "vbpl:1")}); err != nil {
		t.Fatalf("WriteSightings: %v", err)
	}
	if err := WriteSightings(dir, "vbpl:1", []Edge{stored("c9", "c8", "vbpl:1")}); err != nil {
		t.Fatalf("WriteSightings: %v", err)
	}
	got, err := ReadSightings(dir, "vbpl:1")
	if err != nil {
		t.Fatalf("ReadSightings: %v", err)
	}
	if len(got) != 1 || got[0].FromID != "c9" {
		t.Errorf("edges = %+v, a rerun of one document doubled its sightings", got)
	}
}

func TestADocumentReadAndFoundToHoldNothingIsAResult(t *testing.T) {
	// Deleting the file would put the document back in the queue forever, and a
	// document with no relations is a perfectly ordinary outcome.
	dir := t.TempDir()
	if err := WriteSightings(dir, "vbpl:1", nil); err != nil {
		t.Fatalf("WriteSightings: %v", err)
	}
	if _, err := os.Stat(SightingPath(dir, "vbpl:1")); err != nil {
		t.Fatalf("the file is not there: %v", err)
	}
	got, err := ReadSightings(dir, "vbpl:1")
	if err != nil || len(got) != 0 {
		t.Errorf("edges = %+v err = %v", got, err)
	}
}

func TestADocumentNobodyReadIsNotAnError(t *testing.T) {
	got, err := ReadSightings(t.TempDir(), "vbpl:404")
	if err != nil || got != nil {
		t.Errorf("edges = %+v err = %v, an unread document is a gap and not a failure", got, err)
	}
}

func TestEachSightingReadsOnlyTheSightingFiles(t *testing.T) {
	// One directory holds the sightings, the folded layer and the review queue,
	// and a proposal unmarshalled as an edge would be read as silence rather than
	// as an error.
	dir := t.TempDir()
	for _, doc := range []string{"vbpl:1", "vbpl:2"} {
		if err := WriteSightings(dir, doc, []Edge{stored("c1", "c2", doc)}); err != nil {
			t.Fatalf("WriteSightings: %v", err)
		}
	}
	if err := WriteEdges(dir, []Edge{stored("c1", "c2", "vbpl:1")}); err != nil {
		t.Fatalf("WriteEdges: %v", err)
	}
	if err := WriteProposals(dir, []Proposal{{Label: "DUOC_MIEN", Definition: "X được miễn Y", Instances: 3}}); err != nil {
		t.Fatalf("WriteProposals: %v", err)
	}
	if err := WriteRegistry(dir, SeedRegistry(2)); err != nil {
		t.Fatalf("WriteRegistry: %v", err)
	}

	seen := map[string]int{}
	if err := EachSighting(dir, func(docID string, edges []Edge) error {
		seen[docID] = len(edges)
		return nil
	}); err != nil {
		t.Fatalf("EachSighting: %v", err)
	}
	if len(seen) != 2 || seen["vbpl:1"] != 1 || seen["vbpl:2"] != 1 {
		t.Errorf("visited = %v, want the two sighting files and nothing else in the directory", seen)
	}
}

func TestEachSightingStopsAtTheFirstRefusal(t *testing.T) {
	dir := t.TempDir()
	if err := WriteSightings(dir, "vbpl:1", []Edge{stored("c1", "c2", "vbpl:1")}); err != nil {
		t.Fatalf("WriteSightings: %v", err)
	}
	if err := EachSighting(dir, func(string, []Edge) error { return os.ErrInvalid }); err == nil {
		t.Error("a visitor that refused was ignored")
	}
}

func TestEachSightingOnADirectoryThatWasNeverWrittenIsNotAnError(t *testing.T) {
	err := EachSighting(filepath.Join(t.TempDir(), "nothing-here"), func(string, []Edge) error {
		t.Error("something was visited in a directory that does not exist")
		return nil
	})
	if err != nil {
		t.Errorf("EachSighting: %v", err)
	}
}

func TestADerivedFileIsRewrittenWholeAndRemovedWhenItIsEmpty(t *testing.T) {
	// A stale fold is worse than no fold because it looks like a result somebody
	// computed, and an empty derived file sitting on disk pretends to be one.
	dir := t.TempDir()
	if err := WriteEdges(dir, []Edge{stored("c1", "c2", "vbpl:1"), stored("c3", "c4", "vbpl:1")}); err != nil {
		t.Fatalf("WriteEdges: %v", err)
	}
	if err := WriteEdges(dir, []Edge{stored("c9", "c8", "vbpl:1")}); err != nil {
		t.Fatalf("WriteEdges: %v", err)
	}
	got, err := ReadEdges(dir)
	if err != nil {
		t.Fatalf("ReadEdges: %v", err)
	}
	if len(got) != 1 || got[0].FromID != "c9" {
		t.Fatalf("edges = %+v, the fold was appended to rather than replaced", got)
	}

	if err := WriteEdges(dir, nil); err != nil {
		t.Fatalf("WriteEdges: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, EdgesFile)); !os.IsNotExist(err) {
		t.Errorf("an empty fold was left on disk: %v", err)
	}
	if got, err := ReadEdges(dir); err != nil || got != nil {
		t.Errorf("edges = %+v err = %v", got, err)
	}
}

func TestTheReviewQueueRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := []Proposal{
		{Label: "DUOC_MIEN", Definition: "X được miễn nghĩa vụ có Y", Instances: 9, Docs: 4,
			AsWritten: []string{"được miễn", "không phải có"},
			Examples:  []Evidence{{ProvisionID: "p1", Quote: "q"}}},
		{Label: "LA_DIEU_KIEN_DE", Definition: "Y phải có thì X mới được cấp",
			Instances: 3, Docs: 2, MatchedTo: Requires, Rationale: "cùng nghĩa"},
	}
	if err := WriteProposals(dir, want); err != nil {
		t.Fatalf("WriteProposals: %v", err)
	}
	got, err := ReadProposals(dir)
	if err != nil {
		t.Fatalf("ReadProposals: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("proposals = %+v", got)
	}
	if got[0].Definition != want[0].Definition || len(got[0].AsWritten) != 2 || got[0].Examples[0].Quote != "q" {
		t.Errorf("proposal = %+v, the review queue lost what a reviewer reads", got[0])
	}
	if got[1].MatchedTo != Requires || got[1].Rationale != "cùng nghĩa" {
		t.Errorf("proposal = %+v, the canonicalization outcome did not survive", got[1])
	}
}

func TestTheRegistryIsPinnedSoAnOldLayerCanStillSayWhatItWasMatchedAgainst(t *testing.T) {
	dir := t.TempDir()
	r := SeedRegistry(4)
	r.Types = append(r.Types, Type{ID: "DUOC_MIEN", Definition: "X được miễn nghĩa vụ có Y"})
	if err := WriteRegistry(dir, r); err != nil {
		t.Fatalf("WriteRegistry: %v", err)
	}
	got, err := ReadRegistry(dir)
	if err != nil {
		t.Fatalf("ReadRegistry: %v", err)
	}
	if got.Version != 4 || len(got.Types) != len(r.Types) {
		t.Fatalf("registry = v%d with %d types", got.Version, len(got.Types))
	}
	if t2 := got.Type("DUOC_MIEN"); t2 == nil || t2.Definition == "" {
		t.Error("the definition canonicalization matches on did not survive the round trip")
	}
	if t3 := got.Type(Requires); t3 == nil || len(t3.Range) == 0 {
		t.Error("the domain and range the checker enforces did not survive the round trip")
	}
}

func TestAStoreThatNeverRanThePassFallsBackToTheSeed(t *testing.T) {
	got, err := ReadRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("ReadRegistry: %v", err)
	}
	if got.Version != 1 || len(got.Types) != len(Seed) {
		t.Errorf("registry = v%d with %d types, want the seed vocabulary", got.Version, len(got.Types))
	}
}

func TestABrokenRegistrySaysWhichFileItCouldNotRead(t *testing.T) {
	// Falling back to the seed here would silently rewrite the vocabulary an
	// existing layer's canonical edges cite.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, RegistryFile), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ReadRegistry(dir)
	if err == nil {
		t.Fatal("an unreadable registry was read as the seed")
	}
	if !strings.Contains(err.Error(), RegistryFile) {
		t.Errorf("error = %v, it does not name the file", err)
	}
}

func TestTheSummaryRoundTrips(t *testing.T) {
	dir := t.TempDir()
	if got, err := ReadSummary(dir); err != nil || got != nil {
		t.Errorf("summary = %+v err = %v, a store that never ran has no numbers to report", got, err)
	}

	want := Summary{
		Documents: 3,
		Counts:    Count([]Edge{stored("c1", "c2", "vbpl:1")}),
		Direction: ScoreDirection([]Edge{{Direction: DirectionAgreed}, {Direction: DirectionFlipped}}),
		Densest:   Densities([]Edge{stored("c1", "c2", "vbpl:1")}),
	}
	if err := WriteSummary(dir, want); err != nil {
		t.Fatalf("WriteSummary: %v", err)
	}
	got, err := ReadSummary(dir)
	if err != nil {
		t.Fatalf("ReadSummary: %v", err)
	}
	if got == nil || got.Documents != 3 {
		t.Fatalf("summary = %+v", got)
	}
	if got.Counts.Provisional != 1 || got.Counts.ByWhy[WhySingleSupport] != 1 {
		t.Errorf("counts = %+v, the review queue size did not survive", got.Counts)
	}
	if got.Direction.Agreed != 1 || got.Direction.Flipped != 1 {
		t.Errorf("direction = %+v, the metric that is never folded into precision did not survive", got.Direction)
	}
	if len(got.Densest) != 1 || got.Densest[0].DocID != "vbpl:1" {
		t.Errorf("densest = %+v", got.Densest)
	}
}
