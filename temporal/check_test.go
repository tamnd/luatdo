package temporal

import (
	"strings"
	"testing"
)

func TestCheckPassesOnAGoodLayer(t *testing.T) {
	first := op("a1", KindAmend, clause2, "2022-07-01")
	first.NewText = "2. Tự nguyện, bình đẳng, thiện chí."
	l, _ := Build(corpus(), []Operation{first})

	r := Check(l)
	if !r.OK() {
		t.Fatalf("a layer built from one clean amendment broke invariants:\n%v", r.Problems)
	}
	if r.Versions == 0 || r.Components == 0 || r.Events == 0 {
		t.Errorf("the report counted nothing: %+v", r)
	}
	if !strings.Contains(r.String(), "all nine invariants hold") {
		t.Errorf("the report does not say it passed:\n%s", r.String())
	}
}

func TestCheckCatchesABackwardsEvent(t *testing.T) {
	// Invariant 1. Hand built, because the builder refuses to produce it.
	l := &Layer{
		Versions: []Version{{
			ID: clause2 + "@v1", ComponentID: clause2, DocID: docID, Seq: 1,
			From: "2021-01-01", To: "2020-01-01", Force: ForceInForce,
			ProducedBy: "e0", TerminatedBy: "e1",
		}},
		Events: []Event{{
			ID: "e1", Kind: KindAmend, Date: "2020-01-01", CausedByDoc: amendDoc,
			CausedBy: amendDoc + ":article-1", Targets: clause2,
			Terminates: []string{clause2 + "@v1"},
		}},
	}
	if got := invariants(Check(l), 1); got == 0 {
		t.Error("an event that terminates a version starting after it was not caught")
	}
}

func TestCheckCatchesAGap(t *testing.T) {
	// Invariant 3. A gap answers a point in time query the same way a repeal
	// does while meaning something else entirely.
	l := &Layer{
		Versions: []Version{
			{ID: clause2 + "@v1", ComponentID: clause2, Seq: 1, From: "2021-01-01", To: "2022-01-01", Force: ForceInForce},
			{ID: clause2 + "@v2", ComponentID: clause2, Seq: 2, From: "2023-01-01", Force: ForceInForce},
		},
	}
	r := Check(l)
	if got := invariants(r, 3); got == 0 {
		t.Fatal("a year with no version in force was not reported")
	}
	if !strings.Contains(r.Problems[0].Detail, "nothing is in force") {
		t.Errorf("the report does not say what is wrong: %s", r.Problems[0].Detail)
	}
}

func TestCheckCatchesAnOverlap(t *testing.T) {
	l := &Layer{
		Versions: []Version{
			{ID: clause2 + "@v1", ComponentID: clause2, Seq: 1, From: "2021-01-01", To: "2023-01-01", Force: ForceInForce},
			{ID: clause2 + "@v2", ComponentID: clause2, Seq: 2, From: "2022-01-01", Force: ForceInForce},
		},
	}
	if got := invariants(Check(l), 3); got == 0 {
		t.Error("two versions of one clause in force on the same day were not reported")
	}
}

func TestCheckCatchesAMissingEndDate(t *testing.T) {
	l := &Layer{
		Versions: []Version{
			{ID: clause2 + "@v1", ComponentID: clause2, Seq: 1, From: "2021-01-01", Force: ForceInForce},
			{ID: clause2 + "@v2", ComponentID: clause2, Seq: 2, From: "2022-01-01", Force: ForceInForce},
		},
	}
	if got := invariants(Check(l), 2); got == 0 {
		t.Error("a superseded version with no end date was not reported")
	}
}

func TestCheckCatchesAnEventWithNoCause(t *testing.T) {
	l := &Layer{
		Versions: []Version{{ID: clause2 + "@v1", ComponentID: clause2, Seq: 1, From: "2021-01-01", Force: ForceInForce}},
		Events: []Event{{
			ID: "e1", Kind: KindAmend, Date: "2022-01-01", Targets: clause2,
			Produces: []string{clause2 + "@v1"},
		}},
	}
	r := Check(l)
	if invariants(r, 5) == 0 {
		t.Error("an event with no amending instrument was not reported")
	}
	if invariants(r, 4) == 0 {
		t.Error("an event with no provision holding the instruction was not reported")
	}
}

func TestCheckCatchesLifeAfterRepeal(t *testing.T) {
	l, _ := Build(corpus(), []Operation{op("r1", KindRepeal, clause2, "2022-07-01")})
	// A version appearing after the repeal, produced by nothing that explains it.
	l.Versions = append(l.Versions, Version{
		ID: clause2 + "@v9", ComponentID: clause2, DocID: docID, Seq: 9,
		From: "2023-01-01", Force: ForceInForce,
	})
	SortVersions(l.Versions)
	if invariants(Check(l), 6) == 0 {
		t.Error("a clause that came back after a repeal with nothing explaining it was not reported")
	}
}

// A repeal terminates every version above the clause it takes out, because each
// of those components is re-issued without that child. Reading those as repeals
// of the parent reported nine violations on the labour campaign and not one of
// them was a provision coming back from the dead.
func TestAnArticleThatLostAClauseWasNotItselfRepealed(t *testing.T) {
	l, _ := Build(corpus(), []Operation{op("r1", KindRepeal, clause2, "2022-07-01")})
	r := Check(l)
	if got := invariants(r, 6); got != 0 {
		t.Errorf("repealing one clause reported %d components as coming back from the dead:\n%v", got, r.Problems)
	}
	// The article does get a new version, and that is the point: it is the same
	// article with one fewer clause, not a different one.
	if n := len(l.Versions); n < 2 {
		t.Fatalf("the repeal produced %d versions, so this test proves nothing", n)
	}
}

func TestResumeExplainsLifeAfterSuspension(t *testing.T) {
	l, _ := Build(corpus(), []Operation{
		op("p1", KindSuspend, clause2, "2022-07-01"),
		op("p2", KindResume, clause2, "2023-01-01"),
	})
	if got := invariants(Check(l), 6); got != 0 {
		t.Errorf("a resume after a suspension is not a defect, %d reported", got)
	}
}

func TestCheckNormsContainment(t *testing.T) {
	// Invariant 7: a norm cannot outlive the text it was read from.
	first := op("a1", KindAmend, clause2, "2022-07-01")
	first.NewText = "2. Tự nguyện, bình đẳng, thiện chí."
	l, _ := Build(corpus(), []Operation{first})
	v := NewView(l)

	r := &Report{}
	CheckNorms(v, []Interval{
		{ID: "norm-good", VersionID: clause2 + "@v1", From: "2021-06-01", To: "2022-07-01"},
		{ID: "norm-outlives", VersionID: clause2 + "@v1", From: "2021-06-01", To: "2024-01-01"},
		{ID: "norm-unknown", VersionID: clause2 + "@v9", From: "2021-06-01"},
	}, r)

	if len(r.Problems) != 2 {
		t.Fatalf("want two problems, got %v", r.Problems)
	}
	for _, p := range r.Problems {
		if p.Invariant != 7 {
			t.Errorf("reported as invariant %d", p.Invariant)
		}
		if p.Subject == "norm-good" {
			t.Error("a norm inside its version's interval was reported")
		}
	}
}

func TestCheckTermUsesContainment(t *testing.T) {
	l, _ := Build(corpus(), []Operation{op("r1", KindRepeal, clause2, "2022-07-01")})
	v := NewView(l)
	r := &Report{}
	CheckTermUses(v, []Interval{
		{ID: "use-1", VersionID: clause2 + "@v1", From: "2021-01-01", To: "2022-07-01"},
		{ID: "use-2", VersionID: clause2 + "@v1", From: "2021-01-01"},
	}, r)
	if len(r.Problems) != 1 || r.Problems[0].Subject != "use-2" {
		t.Fatalf("a term use outliving the component that defines it was not caught: %v", r.Problems)
	}
	if r.Problems[0].Invariant != 8 {
		t.Errorf("reported as invariant %d, want 8", r.Problems[0].Invariant)
	}
}

func TestWithin(t *testing.T) {
	cases := []struct {
		from, to, outerFrom, outerTo string
		want                         bool
	}{
		{"2021-01-01", "2022-01-01", "2021-01-01", "2022-01-01", true},
		{"2020-01-01", "2022-01-01", "2021-01-01", "2022-01-01", false},
		{"2021-06-01", "", "2021-01-01", "", true},
		// An open inner interval inside a closed outer one runs past its end.
		{"2021-06-01", "", "2021-01-01", "2022-01-01", false},
		{"", "2022-01-01", "2021-01-01", "2022-01-01", false},
	}
	for _, c := range cases {
		if got := within(c.from, c.to, c.outerFrom, c.outerTo); got != c.want {
			t.Errorf("within(%q,%q,%q,%q) = %v", c.from, c.to, c.outerFrom, c.outerTo, got)
		}
	}
}

func TestReportCountsUndatedAndQuarantined(t *testing.T) {
	undated := op("u1", KindAmend, clause2, "")
	undated.NewText = "2. Nội dung mới."
	dropped := op("q1", KindAmend, clause2, "2022-07-01") // no text
	valid := op("a1", KindAmend, clause2, "2022-07-01")
	valid.NewText = "2. Tự nguyện, bình đẳng, thiện chí."

	l, _ := Build(corpus(), []Operation{undated, dropped, valid})
	r := Check(l)
	if r.Undated != 1 {
		t.Errorf("undated events counted as %d, want 1", r.Undated)
	}
	if r.Quarantined != 1 {
		t.Errorf("quarantined operations counted as %d, want 1", r.Quarantined)
	}
}

func invariants(r *Report, n int) int {
	count := 0
	for _, p := range r.Problems {
		if p.Invariant == n {
			count++
		}
	}
	return count
}
