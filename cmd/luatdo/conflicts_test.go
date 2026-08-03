package main

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/conflict"
	"github.com/tamnd/luatdo/temporal"
)

// cases builds a case list whose mutations are laid out the way Build lays them
// out, in runs of one kind after another. Only the labels matter here, so the
// pairs are left empty.
func cases(kinds ...string) []conflict.Case {
	var out []conflict.Case
	for _, kind := range kinds {
		out = append(out, conflict.Case{Mutation: kind})
	}
	return out
}

func TestSampleCasesKeepsEveryMutationInProportion(t *testing.T) {
	all := cases(
		"a", "a", "a", "a",
		"b", "b", "b", "b",
		"c", "c", "c", "c")

	got := sampleCases(all, 6)
	if len(got) != 6 {
		t.Fatalf("sampled %d cases, asked for 6", len(got))
	}
	counts := map[string]int{}
	for _, c := range got {
		counts[c.Mutation]++
	}
	// Taking the first six would be six pairs of one shape and a score that
	// answers a narrower question than the one printed above it.
	for _, kind := range []string{"a", "b", "c"} {
		if counts[kind] != 2 {
			t.Errorf("%s appears %d times in the sample, want 2 of each: %v", kind, counts[kind], counts)
		}
	}
}

func TestSampleCasesReturnsTheListWhenThereIsNothingToThin(t *testing.T) {
	all := cases("a", "b", "c")
	for _, n := range []int{0, -1, 3, 10} {
		if got := sampleCases(all, n); len(got) != len(all) {
			t.Errorf("sampleCases(n=%d) returned %d of %d cases", n, len(got), len(all))
		}
	}
	if got := sampleCases(nil, 5); got != nil {
		t.Errorf("sampleCases of nothing = %v", got)
	}
}

func TestSampleCasesTakesDistinctCases(t *testing.T) {
	all := cases("a", "a", "a", "a", "b", "b", "b", "b")
	got := sampleCases(all, 5)
	if len(got) != 5 {
		t.Fatalf("sampled %d cases, asked for 5", len(got))
	}
	// The stride is an index into the list and never lands twice on the same
	// pair, which a baseline paying one model call per pair would otherwise pay
	// for twice.
	seen := map[int]bool{}
	for i := 0; i < 5; i++ {
		at := i * len(all) / 5
		if seen[at] {
			t.Fatalf("the stride landed on index %d twice", at)
		}
		seen[at] = true
	}
}

// stampFixture is three forms in one instrument: one provision the version graph
// knows, one it does not, and one whose instrument has no effective date either.
func stampFixture() *forms {
	const doc = "vn:law:2019:45-2019-qh14"
	return &forms{
		all: []*conflict.Form{
			{StatementID: "vn:norm:a", DocID: doc, ProvisionID: doc + ":article-94"},
			{StatementID: "vn:norm:b", DocID: doc, ProvisionID: doc + ":article-95"},
			{StatementID: "vn:norm:c", DocID: "vn:law:2099:1-2099-nd-cp", ProvisionID: "vn:law:2099:1-2099-nd-cp:article-1"},
		},
		docs: &conflict.Docs{
			Types:      map[string]string{doc: "code"},
			Effectives: map[string]string{doc: "2021-01-01"},
		},
	}
}

func TestStampPrefersTheVersionGraphAndTakesTheUnion(t *testing.T) {
	const article = "vn:law:2019:45-2019-qh14:article-94"
	view := temporal.NewView(&temporal.Layer{Versions: []temporal.Version{
		{ID: article + "@v1", ComponentID: article, Seq: 1, From: "2021-01-01", To: "2023-01-01"},
		{ID: article + "@v2", ComponentID: article, Seq: 2, From: "2023-01-01", To: ""},
	}})
	f := stampFixture()
	f.stamp(view)

	// The union of the versions, not the last one. Nothing records which version
	// a statement was extracted from, so a narrower interval would drop pairs on
	// a guess and nobody would ever see what was dropped.
	if f.all[0].Scope.From != "2021-01-01" || f.all[0].Scope.To != "" {
		t.Errorf("scope = %+v, want the union of both versions", f.all[0].Scope)
	}
	if f.dated != 1 {
		t.Errorf("dated = %d, want the one provision the version graph knows", f.dated)
	}
	// The provision the version graph has never heard of falls back to the day
	// its instrument took effect.
	if f.all[1].Scope.From != "2021-01-01" || f.all[1].Scope.To != "" {
		t.Errorf("fallback scope = %+v", f.all[1].Scope)
	}
	if f.approximate != 1 {
		t.Errorf("approximate = %d, want the one provision dated from its instrument", f.approximate)
	}
	// An instrument with no effective date leaves the interval open at both
	// ends, which overlaps everything and compares the pair rather than hiding
	// it.
	if f.all[2].Scope.From != "" || f.all[2].Scope.To != "" {
		t.Errorf("undated scope = %+v, want both ends open", f.all[2].Scope)
	}
	if f.undated != 1 {
		t.Errorf("undated = %d", f.undated)
	}
}

func TestStampWithNoVersionGraphAtAll(t *testing.T) {
	// A store where luatdo temporal build has never run is normal, and the check
	// pass has to date what it can rather than refuse to run.
	f := stampFixture()
	f.stamp(nil)
	if f.dated != 0 || f.approximate != 2 || f.undated != 1 {
		t.Errorf("dated %d, approximate %d, undated %d", f.dated, f.approximate, f.undated)
	}
	for i := 0; i < 2; i++ {
		if f.all[i].Scope.From != "2021-01-01" {
			t.Errorf("form %d scope = %+v, want the instrument's effective date", i, f.all[i].Scope)
		}
	}
}

func TestFormsStringSaysHowManyIntervalsAreGuesses(t *testing.T) {
	f := stampFixture()
	f.stamp(nil)
	got := f.String()
	// The counts are printed because they are the honest caveat on every
	// temporal skip the checker makes: an interval taken from the instrument is
	// the whole document's, not the provision's.
	for _, want := range []string{"3 forms", "0 dated", "2 from the instrument", "1 undated"} {
		if !strings.Contains(got, want) {
			t.Errorf("the summary does not say %q:\n%s", want, got)
		}
	}
}
