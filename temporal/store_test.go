package temporal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOperationsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	o := op("a1", KindAmend, clause2, "2022-07-01")
	o.NewText = "2. Tự nguyện, bình đẳng, thiện chí."
	o.Anchor = &Anchor{Position: "after", Sibling: "điểm c"}
	o.Phrase = &PhraseEdit{Find: "x", Replace: "y", Targets: []string{clause2}}

	if err := WriteOperations(dir, amendDoc, []Operation{o}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadOperations(dir, amendDoc)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("read %d operations", len(got))
	}
	if got[0].NewText != o.NewText || got[0].Anchor.Sibling != "điểm c" || got[0].Phrase.Find != "x" {
		t.Errorf("the operation came back changed: %+v", got[0])
	}
}

func TestAnInstrumentWithNoAmendmentsLeavesAFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteOperations(dir, amendDoc, nil); err != nil {
		t.Fatal(err)
	}
	// An instrument read and found to hold nothing is a result. Deleting the
	// file would put it back in the queue forever.
	if _, err := os.Stat(OperationPath(dir, amendDoc)); err != nil {
		t.Errorf("no file was written for an instrument with no amendments: %v", err)
	}
	ops, err := ReadOperations(dir, amendDoc)
	if err != nil || len(ops) != 0 {
		t.Errorf("read %d operations, %v", len(ops), err)
	}
}

func TestReadingAnInstrumentNobodyReadIsNotAnError(t *testing.T) {
	ops, err := ReadOperations(t.TempDir(), amendDoc)
	if err != nil || ops != nil {
		t.Errorf("got %v, %v", ops, err)
	}
}

func TestAllOperationsSkipsDerivedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := WriteOperations(dir, amendDoc, []Operation{op("a1", KindAmend, clause2, "2022-07-01")}); err != nil {
		t.Fatal(err)
	}
	l, _ := Build(corpus(), []Operation{op("a1", KindRepeal, clause2, "2022-07-01")})
	if err := WriteLayer(dir, l); err != nil {
		t.Fatal(err)
	}
	got, err := AllOperations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("read %d operations from a directory holding the derived layer too", len(got))
	}
}

func TestLayerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	o := op("a1", KindAmend, clause2, "2022-07-01")
	o.NewText = "2. Tự nguyện, bình đẳng, thiện chí."
	l, _ := Build(corpus(), []Operation{o, op("q1", KindAmend, clause2, "2022-08-01")})

	if err := WriteLayer(dir, l); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLayer(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Versions) != len(l.Versions) || len(got.Events) != len(l.Events) {
		t.Fatalf("read %d versions and %d events, wrote %d and %d",
			len(got.Versions), len(got.Events), len(l.Versions), len(l.Events))
	}
	if len(got.Quarantined) != 1 {
		t.Errorf("the quarantine queue did not survive the round trip")
	}
	v := NewView(got)
	if _, ok := v.TextAt(clause2, "2022-09-01"); !ok {
		t.Error("the layer read back cannot answer a point in time question")
	}
}

func TestReadingAStoreNobodyBuiltIsEmptyRatherThanAnError(t *testing.T) {
	l, err := ReadLayer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Versions) != 0 {
		t.Errorf("a store nobody built has %d versions", len(l.Versions))
	}
}

func TestWritingAnEmptyLayerLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLayer(dir, &Layer{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{EventsFile, VersionsFile, QuarantineFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s exists, and an empty derived file pretends to be a result somebody computed", name)
		}
	}
}

func TestSummaryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Summary{
		Instruments: 3, Operations: 12, Applied: 9, Quarantined: 3, Undated: 1,
		Versioned: 2, Versions: 40, Components: 22,
		Reasons: map[string]int{QuarantineUnresolvedTarget: 3},
		Kinds:   map[string]int{KindAmend: 8, KindRepeal: 1},
		Ties:    []string{"two instruments on one day"},
	}
	if err := WriteSummary(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSummary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Operations != 12 || got.Reasons[QuarantineUnresolvedTarget] != 3 || len(got.Ties) != 1 {
		t.Errorf("the summary came back as %+v", got)
	}
}

func TestReadSummaryOfAStoreNobodyRan(t *testing.T) {
	got, err := ReadSummary(t.TempDir())
	if err != nil || got != nil {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestCountSummarisesALayer(t *testing.T) {
	first := op("a1", KindAmend, clause2, "2022-07-01")
	first.NewText = "2. Một."
	l, _ := Build(corpus(), []Operation{first, op("r1", KindRepeal, clause1, "2023-01-01")})

	c := Count(l)
	if c.Amended == 0 {
		t.Error("no component counted as amended")
	}
	if c.Repealed == 0 {
		t.Error("the repealed clause was not counted")
	}
	if c.ByKind[KindAmend] != 1 || c.ByKind[KindRepeal] != 1 {
		t.Errorf("kinds counted as %v", c.ByKind)
	}
}
