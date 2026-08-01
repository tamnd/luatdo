package concept

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSightingsSurviveTheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := []Sighting{
		sighting("vn:law:2019:45-2019-qh14:article-3:clause-1", "vn:law:2019:45-2019-qh14", "người lao động"),
		sighting("vn:law:2019:45-2019-qh14:article-3:clause-2", "vn:law:2019:45-2019-qh14"),
	}
	if err := WriteSightings(dir, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadSightings(dir, "vn:law:2019:45-2019-qh14")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want two sightings, got %d", len(got))
	}
	// The provision that found nothing has to survive. Without it the
	// denominator of every discovery rate is the provisions that succeeded.
	if len(got[1].Candidates) != 0 || got[1].ProvisionID != want[1].ProvisionID {
		t.Errorf("the empty sighting did not survive: %+v", got[1])
	}
}

func TestReadSightingsOfADocumentNobodyReadIsNotAnError(t *testing.T) {
	got, err := ReadSightings(t.TempDir(), "vn:law:2019:45-2019-qh14")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != nil {
		t.Errorf("want nil, got %v", got)
	}
}

func TestEachSightingReadsOnlySightingFiles(t *testing.T) {
	// One directory holds the reading jobs, the sightings, the mention reports
	// and the built layer, and a file that is not a sighting read as one would
	// unmarshal into silence.
	dir := t.TempDir()
	if err := WriteSightings(dir, []Sighting{
		sighting("vn:law:a:article-1", "vn:law:a", "khái niệm"),
	}); err != nil {
		t.Fatalf("write sightings: %v", err)
	}
	if err := WriteJob(dir, []Job{{DocID: "vn:law:b"}}); err != nil {
		t.Fatalf("write job: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, LayerFile), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write layer: %v", err)
	}

	seen := 0
	if err := EachSighting(dir, func(ss []Sighting) error {
		seen += len(ss)
		return nil
	}); err != nil {
		t.Fatalf("each: %v", err)
	}
	if seen != 1 {
		t.Errorf("read %d sightings, want 1", seen)
	}
}

func TestEachSightingOnAnEmptyStoreIsNotAnError(t *testing.T) {
	if err := EachSighting(filepath.Join(t.TempDir(), "nothing"), func([]Sighting) error {
		t.Error("visited something in a store that does not exist")
		return nil
	}); err != nil {
		t.Fatalf("each: %v", err)
	}
}

func TestDerivedFilesSurviveTheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	aggs := []Aggregation{{Key: "a", LabelVI: "a", Sighted: 3, Kinds: map[string]int{KindThing: 3}}}
	promotions := []Promotion{{Key: "a", LabelVI: "a", Rule: RuleFrequency, InDocs: 3}}
	working := []WorkingDefinition{{TermUseID: "vn:term:vn:usage:a", Claims: []Claim{{Text: "x"}}}}

	if err := WriteAggregations(dir, aggs); err != nil {
		t.Fatalf("write aggregations: %v", err)
	}
	if err := WritePromotions(dir, promotions); err != nil {
		t.Fatalf("write promotions: %v", err)
	}
	if err := WriteWorkingDefinitions(dir, working); err != nil {
		t.Fatalf("write working: %v", err)
	}

	gotAggs, err := ReadAggregations(dir)
	if err != nil || len(gotAggs) != 1 || gotAggs[0].Kinds[KindThing] != 3 {
		t.Errorf("aggregations came back as %v, %v", gotAggs, err)
	}
	gotPromotions, err := ReadPromotions(dir)
	if err != nil || len(gotPromotions) != 1 || gotPromotions[0].Rule != RuleFrequency {
		t.Errorf("promotions came back as %v, %v", gotPromotions, err)
	}
	gotWorking, err := ReadWorkingDefinitions(dir)
	if err != nil || len(gotWorking) != 1 {
		t.Errorf("working definitions came back as %v, %v", gotWorking, err)
	}
}

func TestWritingNothingRemovesTheDerivedFile(t *testing.T) {
	// An empty derived file sitting on disk pretends to be a result somebody
	// computed.
	dir := t.TempDir()
	if err := WriteAggregations(dir, []Aggregation{{Key: "a"}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := WriteAggregations(dir, nil); err != nil {
		t.Fatalf("write nothing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, AggregateFile)); !os.IsNotExist(err) {
		t.Errorf("the file is still there: %v", err)
	}
}

func TestMentionReportSurvivesTheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := &MentionReport{DocID: "vn:law:2019:45-2019-qh14", Mentions: []Mention{
		{ProvisionID: "p", Surface: "người lao động", TermUseID: "a", Method: MethodScored},
		{ProvisionID: "p", Surface: "tiền lương", Method: MethodUnresolved},
	}}
	Summarize(r)
	if err := WriteMentions(dir, r); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadMentions(dir, "vn:law:2019:45-2019-qh14")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got == nil || got.Unresolved != 1 || got.Resolved != 1 {
		t.Errorf("report came back as %+v", got)
	}
}
