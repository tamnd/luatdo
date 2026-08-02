package temporal

import "testing"

// stampView builds a graph where one clause was amended once and one was never
// touched, which is the shape every stamp question turns on.
func stampView(t *testing.T) *View {
	t.Helper()
	o := op("a1", KindAmend, clause2, "2022-07-01")
	o.NewText = "van ban moi"
	l, _ := Build(corpus(), []Operation{o})
	return NewView(l)
}

func TestStampGivesAReadingTheIntervalOfTheWordingItReads(t *testing.T) {
	v := stampView(t)
	got := Stamp(v, []Reading{{Kind: AnchorNorm, RecordID: "n1", DocID: docID, ProvisionID: clause2}}, nil)
	if len(got) != 2 {
		t.Fatalf("clause 2 has two wordings, so a norm read from it has two candidate intervals, got %d", len(got))
	}
	if got[0].From >= got[1].From {
		t.Error("intervals come out oldest first, otherwise nobody can read the list")
	}
	if got[0].To != got[1].From {
		t.Errorf("the first interval has to end where the second begins, got %q then %q", got[0].To, got[1].From)
	}
	if got[1].To != "" {
		t.Error("the newest wording is still current, so its interval has no end")
	}
	for _, r := range got {
		if r.Kind != AnchorNorm || r.RecordID != "n1" || r.ProvisionID != clause2 {
			t.Errorf("the stamp lost which record it belongs to: %+v", r)
		}
		if r.VersionID == "" {
			t.Error("a stamp names the wording it came from, or nobody can check it")
		}
	}
}

func TestStampGivesAnUnamendedProvisionOneInterval(t *testing.T) {
	v := stampView(t)
	got := Stamp(v, []Reading{{Kind: AnchorTermUse, RecordID: "t1", DocID: docID, ProvisionID: clause1}}, nil)
	if len(got) != 1 {
		t.Fatalf("a provision nothing amended has one wording covering its whole life, got %d", len(got))
	}
	if got[0].To != "" || !got[0].InForceAt("2099-01-01") {
		t.Errorf("that wording runs to the end of time, got %+v", got[0])
	}
}

func TestStampInventsNothingForAProvisionTheGraphHasNeverSeen(t *testing.T) {
	v := stampView(t)
	other := "vn:law:2000:1-2000-nd-cp"
	readings := []Reading{{Kind: AnchorRelation, RecordID: "e1", DocID: other, ProvisionID: other + ":article-1"}}

	got := Stamp(v, readings, nil)
	if len(got) != 0 {
		t.Fatalf("a document nothing knows the commencement of gets no interval, got %d", len(got))
	}
	if by := StampCoverage(readings, got); by["none"] != 1 {
		t.Errorf("the run has to report what it could not date, got %v", by)
	}
}

func TestStampFallsBackToCommencementAndSaysSo(t *testing.T) {
	v := stampView(t)
	other := "vn:law:2000:1-2000-nd-cp"
	readings := []Reading{{Kind: AnchorNorm, RecordID: "n9", DocID: other, ProvisionID: other + ":article-1"}}

	got := Stamp(v, readings, map[string]Commencement{other: {From: "2000-05-01"}})
	if len(got) != 1 || got[0].From != "2000-05-01" {
		t.Fatalf("a provision the version graph does not cover takes the day its document commenced, got %+v", got)
	}
	if got[0].Source != SourceCommencement {
		t.Errorf("the stamp has to say where the interval came from, got %q", got[0].Source)
	}
	if !got[0].InForceAt("2020-01-01") {
		t.Error("nothing says these words ever changed, so they are still in force")
	}
}

func TestAnUnreadAmendmentLeavesTheEndUnknownRatherThanOpen(t *testing.T) {
	v := stampView(t)
	other := "vn:law:2000:1-2000-nd-cp"
	readings := []Reading{{Kind: AnchorNorm, RecordID: "n9", DocID: other, ProvisionID: other + ":article-1"}}

	got := Stamp(v, readings, map[string]Commencement{other: {From: "2000-05-01", Amended: true}})
	if len(got) != 1 || got[0].Source != SourceCommencementAmended {
		t.Fatalf("something amended this document and nobody read it, which the stamp has to record: %+v", got)
	}
	if got[0].InForceAt("2020-01-01") {
		t.Error("answering yes here is the mistake this layer exists to prevent, the honest answer is that it does not know")
	}
}

func TestAReadingOfSuspendedWordsIsNotInForce(t *testing.T) {
	r := Validity{From: "2021-01-01", Force: ForceSuspended}
	if r.InForceAt("2022-01-01") {
		t.Error("a norm read from suspended words is not in force, which is the whole reason the third state exists")
	}
}

func TestValiditySidecarRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := []Validity{{Kind: AnchorNorm, RecordID: "n1", ProvisionID: clause2, VersionID: "v1", From: "2021-01-01"}}
	if err := WriteValidity(dir, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadValidity(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 || got[0].RecordID != "n1" || got[0].From != "2021-01-01" {
		t.Errorf("the sidecar did not come back as it went in: %+v", got)
	}
}
