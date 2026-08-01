package ontology

import (
	"testing"
	"time"
)

func TestSeedLoadSaveFreeze(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Seed()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	r, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Version != 1 || r.Frozen() {
		t.Fatalf("loaded v%d frozen=%v", r.Version, r.Frozen())
	}
	if r.Class("vn-legal:Employer") == nil {
		t.Error("seed misses Employer")
	}
	if r.Class("vn-legal:Invented") != nil {
		t.Error("unknown class resolved")
	}

	frozen, err := Freeze(dir, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if !frozen.Frozen() {
		t.Fatal("registry not frozen")
	}
	if err := Save(dir, frozen); err == nil {
		t.Fatal("saving over a frozen version must fail")
	}

	next := *frozen
	next.Version = 2
	next.FrozenAt = ""
	if err := Save(dir, &next); err != nil {
		t.Fatalf("save v2: %v", err)
	}
	r, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != 2 {
		t.Errorf("Load picked v%d, want the highest version", r.Version)
	}
}

func TestCandidatesQueue(t *testing.T) {
	dir := t.TempDir()
	proposals := []Candidate{
		{Kind: "class", Label: "Quỹ đầu tư", Status: "proposed", At: "t1"},
		{Kind: "class", Label: "Người nộp thuế", Status: "proposed", At: "t1"},
	}
	if err := AppendCandidates(dir, proposals); err != nil {
		t.Fatalf("AppendCandidates: %v", err)
	}
	decision := Candidate{Kind: "class", Label: "Quỹ đầu tư", Status: "approved", At: "t2"}
	if err := AppendCandidates(dir, []Candidate{decision}); err != nil {
		t.Fatal(err)
	}
	all, err := ReadCandidates(dir)
	if err != nil {
		t.Fatalf("ReadCandidates: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("recorded = %d, want 3, the queue is append only", len(all))
	}
	pending := Pending(all)
	if len(pending) != 1 || pending[0].Label != "Người nộp thuế" {
		t.Errorf("pending = %+v, the approved candidate must fold away", pending)
	}
}
