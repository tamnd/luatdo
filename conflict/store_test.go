package conflict

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := []*Form{form("b", Obligation), form("a", Prohibition)}
	want[0].Scope = Scope{From: "2021-01-01", Conditions: []string{"x"}, Defers: []string{decree}}

	if err := WriteForms(dir, labourCode, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadForms(dir, labourCode)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d forms, wrote 2", len(got))
	}
	// Written sorted, so two runs over the same store produce byte identical
	// files and a diff of the data directory means something changed.
	if got[0].StatementID != "vn:norm:a" {
		t.Errorf("first form is %s, want the file written in identifier order", got[0].StatementID)
	}
	for _, f := range got {
		if f.StatementID != "vn:norm:b" {
			continue
		}
		if f.Scope.From != "2021-01-01" || len(f.Scope.Conditions) != 1 || len(f.Scope.Defers) != 1 {
			t.Errorf("the scope did not survive the round trip: %+v", f.Scope)
		}
	}
}

func TestWriteFormsRecordsADocumentThatHeldNothing(t *testing.T) {
	dir := t.TempDir()
	if err := WriteForms(dir, labourCode, nil); err != nil {
		t.Fatal(err)
	}
	// An instrument read and found to hold no comparable statement is a result.
	// Leaving no file would put it back in the queue on every run forever.
	if _, err := os.Stat(FormPath(dir, labourCode)); err != nil {
		t.Fatalf("no artifact for a document that held nothing: %v", err)
	}
	got, err := ReadForms(dir, labourCode)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("read %d forms from an empty file", len(got))
	}
}

func TestReadFormsOfADocumentNobodyParsed(t *testing.T) {
	got, err := ReadForms(t.TempDir(), labourCode)
	if err != nil {
		t.Fatalf("an unparsed document is not an error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v", got)
	}
}

func TestFormPathIsPortable(t *testing.T) {
	// Colons are not legal in file names on Windows, and the fleet includes a
	// Windows machine.
	name := filepath.Base(FormPath("dir", labourCode))
	if strings.Contains(name, ":") {
		t.Errorf("form file name %q cannot be written on Windows", name)
	}
	if !strings.HasPrefix(name, FormPrefix) {
		t.Errorf("form file name %q does not carry the prefix that tells the artifacts apart", name)
	}
}

func TestAllFormsReadsEveryDocumentInOneOrder(t *testing.T) {
	dir := t.TempDir()
	if err := WriteForms(dir, decree, []*Form{form("c", Obligation)}); err != nil {
		t.Fatal(err)
	}
	if err := WriteForms(dir, labourCode, []*Form{form("b", Obligation), form("a", Obligation)}); err != nil {
		t.Fatal(err)
	}
	// A file the pass did not write is not a form file and must be ignored.
	if err := os.WriteFile(filepath.Join(dir, ReportFile), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := AllForms(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("read %d forms, wrote 3", len(got))
	}
	for i, want := range []string{"vn:norm:a", "vn:norm:b", "vn:norm:c"} {
		if got[i].StatementID != want {
			t.Errorf("form %d is %s, want %s", i, got[i].StatementID, want)
		}
	}
}

func TestAllFormsOfAnEmptyStore(t *testing.T) {
	got, err := AllForms(filepath.Join(t.TempDir(), "never-created"))
	if err != nil {
		t.Fatalf("a store nobody has parsed into is not an error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v", got)
	}
}

func TestReportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a, b := pair(Obligation, Prohibition)
	want := Check([]*Form{a, b}, nil)
	if err := WriteReport(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadReport(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got.Findings) != 1 {
		t.Fatalf("read %+v", got)
	}
	if got.Findings[0].ID() != want.Findings[0].ID() {
		t.Errorf("the finding changed identity across the round trip")
	}
	if got.Findings[0].A == nil || got.Findings[0].A.Source.Quote == "" {
		t.Error("the report was written without the words a reader checks the pair against")
	}
}

func TestReadReportOfAStoreNobodyChecked(t *testing.T) {
	got, err := ReadReport(t.TempDir())
	if err != nil || got != nil {
		t.Errorf("got %v, %v, want nil and no error", got, err)
	}
}

func TestBenchRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cases := Build(seeds(), 2)
	want := &Bench{PerMutation: 2, Cases: cases, Grade: GradeCases(context.Background(), cases, nil, nil)}
	if err := WriteBench(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadBench(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got.Cases) != len(cases) {
		t.Fatalf("read %d cases, wrote %d", len(got.Cases), len(cases))
	}
	// The pairs are stored and not only the score, because the baseline has to
	// be graded over the same list or the two numbers answer different
	// questions.
	if got.Cases[0].A == nil || got.Cases[0].B == nil {
		t.Error("the pairs did not survive, so no baseline can be graded against this run")
	}
	if got.Grade == nil || got.Grade.Recall != want.Grade.Recall {
		t.Errorf("the grade did not survive: %+v", got.Grade)
	}
}

func TestReadBenchAndBaselineOfAStoreWithNeither(t *testing.T) {
	dir := t.TempDir()
	if got, err := ReadBench(dir); err != nil || got != nil {
		t.Errorf("ReadBench = %v, %v", got, err)
	}
	if got, err := ReadBaseline(dir); err != nil || got != nil {
		t.Errorf("ReadBaseline = %v, %v", got, err)
	}
}

func TestBaselineRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := &BaselineGrade{Cases: 4, TruePositives: 2, Precision: 0.5, ByMutation: map[string]*KindScore{}}
	if err := WriteBaseline(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadBaseline(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Precision != 0.5 || got.Cases != 4 {
		t.Errorf("read %+v", got)
	}
}

func TestSummarizeCarriesTheNoiseFloorBesideTheCount(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	report := Check([]*Form{a, b}, nil)

	noisy := form("x", Obligation)
	quiet := form("y", Prohibition)
	quiet.ProvisionID = noisy.ProvisionID
	noise := NoiseFloor([]*Form{noisy, quiet}, nil)

	s := Summarize(report, noise, Materials([]*Form{a, b}))
	if s.Findings != 1 || s.Shared != 1 {
		t.Errorf("findings %d, shared %d", s.Findings, s.Shared)
	}
	if s.ByRule[RuleDuty] != 1 {
		t.Errorf("by_rule = %v", s.ByRule)
	}
	// A finding count with no noise floor beside it is a number nobody can read.
	// Forty findings at a noise floor of zero is a detector, and forty at a
	// third is a detector firing on one norm read twice.
	if s.Noise.Fired != 1 {
		t.Errorf("noise = %+v, want the measurement carried into the summary", s.Noise)
	}
	if s.Forms != report.Forms || s.Pairs != report.Pairs || s.Compared != report.Compared {
		t.Errorf("the funnel did not survive: %+v", s)
	}
	// A finding count of zero and a detector that could not have fired look the
	// same in the funnel, so what the scope gave the rules is carried too.
	if s.Material.Prohibitions != 1 || s.Material.Obligations != 1 {
		t.Errorf("material = %+v", s.Material)
	}

	dir := t.TempDir()
	if err := WriteSummary(dir, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, SummaryFile)); err != nil {
		t.Fatal(err)
	}
}
