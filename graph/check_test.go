package graph

import (
	"strings"
	"testing"
)

func liveCounts(s Summary) map[string]int {
	got := map[string]int{}
	for _, c := range counters {
		got[c.name] = c.want(s)
	}
	return got
}

func TestDriftNoneWhenTheDatabaseMatches(t *testing.T) {
	want := Summary{Documents: 3, Components: 40, TextVersions: 31, Contains: 40, Cites: 7, Unresolved: 2,
		Terms: 12, Defines: 12, Concepts: 9, Subjects: 168, AboutSubject: 6, Mentions: 30, Norms: 15}
	if got := Drift(want, liveCounts(want)); len(got) != 0 {
		t.Errorf("drift = %v, want none", got)
	}
}

func TestDriftReportsBothDirections(t *testing.T) {
	want := Summary{Documents: 3, Components: 40, Norms: 15}
	got := liveCounts(want)
	got["norms"] = 20      // the database kept statements the store no longer has
	got["components"] = 38 // the last merge did not finish
	lines := Drift(want, got)
	if len(lines) != 2 {
		t.Fatalf("drift = %v, want two counters", lines)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "norms: store 15, database 20, difference +5") {
		t.Errorf("drift = %v, want the surplus reported with its sign", lines)
	}
	if !strings.Contains(joined, "components: store 40, database 38, difference -2") {
		t.Errorf("drift = %v, want the shortfall reported with its sign", lines)
	}
	if lines[0] > lines[1] {
		t.Errorf("drift = %v, want a sorted report so two runs read the same", lines)
	}
}

func TestDriftMissingCounter(t *testing.T) {
	want := Summary{Documents: 1}
	got := liveCounts(want)
	delete(got, "terms")
	lines := Drift(want, got)
	if len(lines) != 1 || !strings.Contains(lines[0], "terms: not counted") {
		t.Errorf("drift = %v, an uncounted part is drift, not agreement", lines)
	}
}

func TestTheAliasLabelIsCountedAgainstTheRealOne(t *testing.T) {
	want := Summary{Components: 40}
	got := liveCounts(want)
	got["provisions"] = 12 // half the nodes came from a merge that predates the split
	lines := Drift(want, got)
	if len(lines) != 1 || !strings.Contains(lines[0], "provisions: store 40, database 12") {
		t.Fatalf("drift = %v, an alias that no longer covers every component is drift", lines)
	}
	// The alias is a promise that the old queries still work, so it has to be
	// checked, and it has to be checked against the component count rather than
	// against a count of its own. When the alias is dropped this test goes with
	// it, and the counter with the same name.
	if want.Components != liveCounts(want)["provisions"] {
		t.Error("the alias counter expects a different number from the label it aliases")
	}
}

func TestUnresolvedIsNotACounter(t *testing.T) {
	for _, c := range counters {
		if c.name == "unresolved" {
			t.Error("unresolved citations are a fact about the store, the database never holds them")
		}
	}
}
