package event

import (
	"strings"
	"testing"
)

// A law that requires the filing and a decree that appraises and grants. The
// chain from the filing to the appraisal is only walkable if the filing named in
// the law and the filing named in the decree are one node.
func across() ([]Occurrence, []Chain) {
	occurrences := []Occurrence{
		sighting("d1:art:5", "d1", "SUBMIT", "nộp hồ sơ"),
		sighting("d2:art:9", "d2", "SUBMIT", "nộp hồ sơ"),
		sighting("d2:art:10", "d2", "REVIEW", "thẩm định hồ sơ"),
		sighting("d3:art:2", "d3", "REVIEW", "thẩm định hồ sơ"),
		sighting("d3:art:3", "d3", "ISSUE", "cấp giấy phép"),
		sighting("d1:art:6", "d1", "ISSUE", "cấp giấy phép"),
	}
	nop, tham, cap := ID("SUBMIT", "nộp hồ sơ"), ID("REVIEW", "thẩm định hồ sơ"), ID("ISSUE", "cấp giấy phép")
	chains := []Chain{
		chainIn("d2:art:10", "d2", nop, tham),
		chainIn("d3:art:3", "d3", tham, cap),
	}
	return occurrences, chains
}

func TestAblateIdentityCountsWhatTheCorpusWideNodeBuys(t *testing.T) {
	occurrences, chains := across()
	got := AblateIdentity(occurrences, chains, SeedRegistry(1), DefaultThresholds, 3)

	if got.Events != 3 {
		t.Fatalf("events: got %d, want 3", got.Events)
	}
	if got.PerDoc != 6 {
		t.Errorf("per document acts: got %d, want 6, one per instrument that names each", got.PerDoc)
	}
	if got.Merged != 3 {
		t.Errorf("merged acts: got %d, want 3, and this is the size of the bet and not a result", got.Merged)
	}
	// Corpus wide, the filing reaches the appraisal and the grant. Per document
	// the two chains live in different instruments and the walk stops after one
	// step, so the grant is lost.
	if got.Changed != 1 || got.Lost != 1 {
		t.Errorf("changed: got %d answers and %d consequences, want 1 and 1: %s", got.Changed, got.Lost, got)
	}
	if !strings.Contains(got.String(), "merges this bet rests on") {
		t.Errorf("the report prints the merge count as a win: %s", got)
	}
}

func TestAblateIdentityNeverReportsAGain(t *testing.T) {
	// Splitting a node can break a path and cannot make one, so a positive figure
	// here would be a bug in the comparison rather than a finding about the law.
	occurrences, chains := across()
	got := AblateIdentity(occurrences, chains, SeedRegistry(1), DefaultThresholds, 0)
	if got.Lost < 0 || got.Depth != 3 {
		t.Errorf("ablation: %+v", got)
	}
}

func TestAblateIdentitySaysWhenTheIdentifierBoughtNothing(t *testing.T) {
	// One instrument, so document scoped identity answers everything corpus wide
	// identity does and carries none of the risk.
	occurrences := []Occurrence{
		sighting("d1:art:5", "d1", "SUBMIT", "nộp hồ sơ"),
		sighting("d1:art:6", "d1", "ISSUE", "cấp giấy phép"),
	}
	chains := []Chain{chainIn("d1:art:5", "d1", ID("SUBMIT", "nộp hồ sơ"), ID("ISSUE", "cấp giấy phép"))}
	got := AblateIdentity(occurrences, chains, SeedRegistry(1), Thresholds{MinProvisions: 1, MinDocs: 1}, 3)
	if got.Changed != 0 {
		t.Fatalf("changed: got %d, want 0", got.Changed)
	}
	if !strings.Contains(got.String(), "carrying its risk for no answer") {
		t.Errorf("an ablation that found nothing reads as a result: %s", got)
	}
}

func TestScopedLeavesADocumentlessSightingAlone(t *testing.T) {
	o := sighting("p1", "", "SUBMIT", "nộp hồ sơ")
	got, _ := Scoped([]Occurrence{o}, nil)
	if got[0].EventID != o.EventID {
		t.Errorf("event id: got %s, want it untouched rather than suffixed with nothing", got[0].EventID)
	}
}

func TestAblateSanctionJoinCountsWhatLabelMatchingWouldInvent(t *testing.T) {
	act := ID("SUBMIT", "nộp hồ sơ")
	fine := ID("SANCTION", "phạt tiền")
	g := NewGraph(
		[]Event{{ID: act, LabelVI: "nộp hồ sơ"}, {ID: fine, LabelVI: "phạt tiền"}},
		nil,
		[]Link{
			// The sanctions decree fines a late filing.
			{StatementID: "s1", ProvisionID: "d3:art:4", DocID: "d3", EventID: act, Kind: LinkAction},
			{StatementID: "s1", ProvisionID: "d3:art:4", DocID: "d3", EventID: fine, Kind: LinkSanction},
			// The labour code requires a filing and states no penalty at all.
			{StatementID: "s2", ProvisionID: "d1:art:5", DocID: "d1", EventID: act, Kind: LinkAction},
		})
	got := AblateSanctionJoin(g)
	if got.ThroughNorm != 1 {
		t.Fatalf("through the norm: got %d, want 1", got.ThroughNorm)
	}
	if got.ByLabel != 2 || got.Invented != 1 {
		t.Errorf("by label: got %d rows and %d invented, want 2 and 1", got.ByLabel, got.Invented)
	}
	if got.CrossDoc != 1 {
		t.Errorf("cross instrument: got %d, want 1", got.CrossDoc)
	}
	report := got.String()
	if !strings.Contains(report, "d1:art:5") || !strings.Contains(report, "d3:art:4") {
		t.Errorf("the report counts the invented rows without showing one: %s", report)
	}
}

func TestAblateSanctionJoinSaysWhenTheJoinChangedNothing(t *testing.T) {
	act, fine := ID("SUBMIT", "nộp hồ sơ"), ID("SANCTION", "phạt tiền")
	g := NewGraph(nil, nil, []Link{
		{StatementID: "s1", ProvisionID: "d3:art:4", DocID: "d3", EventID: act, Kind: LinkAction},
		{StatementID: "s1", ProvisionID: "d3:art:4", DocID: "d3", EventID: fine, Kind: LinkSanction},
	})
	got := AblateSanctionJoin(g)
	if got.Invented != 0 {
		t.Fatalf("invented: got %d, want 0", got.Invented)
	}
	if !strings.Contains(got.String(), "costs nothing and buys nothing") {
		t.Errorf("an ablation that found nothing is printed as a win: %s", got)
	}
}
