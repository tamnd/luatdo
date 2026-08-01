package temporal

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/law"
)

// target is a small two article law used by most tests in this package. It has
// the shape that matters: an article with clauses, and a clause with points, so
// aggregation has something to reuse.
func target() *law.Document {
	return &law.Document{
		ID: "vn:law:2019:45-2019-qh14", OfficialNumber: "45/2019/QH14",
		Title: "Bộ luật Lao động", DocType: "code", EffectiveFrom: "2021-01-01",
		Provisions: []law.Provision{
			{ID: "vn:law:2019:45-2019-qh14:article-15", Kind: "article", Number: "15", Text: "Điều 15."},
			{ID: "vn:law:2019:45-2019-qh14:article-15:clause-1", ParentID: "vn:law:2019:45-2019-qh14:article-15", Kind: "clause", Number: "1", Text: "Giao kết trung thực."},
			{ID: "vn:law:2019:45-2019-qh14:article-15:clause-2", ParentID: "vn:law:2019:45-2019-qh14:article-15", Kind: "clause", Number: "2", Text: "Tự nguyện, bình đẳng."},
			{ID: "vn:law:2019:45-2019-qh14:article-20", Kind: "article", Number: "20", Text: "Điều 20."},
			{ID: "vn:law:2019:45-2019-qh14:article-20:clause-1", ParentID: "vn:law:2019:45-2019-qh14:article-20", Kind: "clause", Number: "1", Text: "Hợp đồng lao động gồm:"},
			{ID: "vn:law:2019:45-2019-qh14:article-20:clause-1:point-a", ParentID: "vn:law:2019:45-2019-qh14:article-20:clause-1", Kind: "point", Number: "a", Text: "Không xác định thời hạn."},
			{ID: "vn:law:2019:45-2019-qh14:article-20:clause-1:point-c", ParentID: "vn:law:2019:45-2019-qh14:article-20:clause-1", Kind: "point", Number: "c", Text: "Theo mùa vụ."},
		},
	}
}

const (
	docID     = "vn:law:2019:45-2019-qh14"
	amendDoc  = "vn:law:2022:10-2022-qh15"
	clause2   = "vn:law:2019:45-2019-qh14:article-15:clause-2"
	article15 = "vn:law:2019:45-2019-qh14:article-15"
	clause1   = "vn:law:2019:45-2019-qh14:article-20:clause-1"
	// clause 1 of article 15, which is the one a replacement of article 15 drops.
	clause1of15 = "vn:law:2019:45-2019-qh14:article-15:clause-1"
	pointC      = "vn:law:2019:45-2019-qh14:article-20:clause-1:point-c"
	pointD      = "vn:law:2019:45-2019-qh14:article-20:clause-1:point-d"
)

func TestKindsAreTheTen(t *testing.T) {
	if len(Kinds) != 10 {
		t.Fatalf("the specification names ten event kinds, this has %d", len(Kinds))
	}
	if !KnownKind(KindSuspend) || !KnownKind(KindResume) {
		t.Error("suspend and resume must be kinds, a suspended provision is a third state")
	}
	if KnownKind("modify") {
		t.Error("a kind outside the ten was accepted")
	}
}

func TestCoversIsHalfOpen(t *testing.T) {
	v := Version{From: "2021-01-01", To: "2023-01-01", Force: ForceInForce}
	cases := []struct {
		date string
		want bool
	}{
		{"2020-12-31", false},
		{"2021-01-01", true},
		{"2022-06-30", true},
		// The day the successor starts belongs to the successor, otherwise the
		// same date has two answers.
		{"2023-01-01", false},
	}
	for _, c := range cases {
		if got := v.Covers(c.date); got != c.want {
			t.Errorf("Covers(%s) = %v, want %v", c.date, got, c.want)
		}
	}
	open := Version{From: "2021-01-01", Force: ForceInForce}
	if !open.Covers("2099-01-01") {
		t.Error("a version with no end date runs to the end of time")
	}
}

func TestSuspendedIsNotInForce(t *testing.T) {
	v := Version{From: "2021-01-01", Force: ForceSuspended}
	if !v.Covers("2022-01-01") {
		t.Fatal("a suspended version still exists on the date")
	}
	if v.InForceAt("2022-01-01") {
		t.Error("a suspended version is not in force, and answering it as in force is the bug this state exists to prevent")
	}
}

func TestUndatedIsNeverGuessed(t *testing.T) {
	if !(Operation{}).Undated() {
		t.Error("an operation with no effective date is undated")
	}
	if (Operation{EffectiveFrom: "2022-01-01"}).Undated() {
		t.Error("an operation with a date is not undated")
	}
}

func TestOrderByDateThenRank(t *testing.T) {
	ops := []Operation{
		{ID: "c", AmendingDoc: "vn:law:2022:1-2022-nd-cp", EffectiveFrom: "2022-01-01"},
		{ID: "a", AmendingDoc: "vn:law:2022:10-2022-qh15", EffectiveFrom: "2022-01-01"},
		{ID: "b", AmendingDoc: "vn:law:2021:5-2021-qh15", EffectiveFrom: "2021-06-01"},
	}
	got, ties := Order(ops)
	want := []string{"b", "a", "c"}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("position %d is %s, want %s", i, got[i].ID, id)
		}
	}
	if len(ties) != 0 {
		t.Errorf("a law and a decree on the same day are not a tie, the law outranks: %v", ties)
	}
}

func TestOrderReportsUnbreakableTies(t *testing.T) {
	ops := []Operation{
		{ID: "a", AmendingDoc: "vn:law:2022:10-2022-qh15", EffectiveFrom: "2022-01-01", TargetComponent: clause2},
		{ID: "b", AmendingDoc: "vn:law:2022:11-2022-qh15", EffectiveFrom: "2022-01-01", TargetComponent: clause2},
	}
	_, ties := Order(ops)
	if len(ties) != 1 {
		t.Fatalf("two laws changing one clause on one day is a tie somebody should look at, got %d", len(ties))
	}
	if !strings.Contains(ties[0], clause2) {
		t.Errorf("the tie does not name the component: %s", ties[0])
	}
}

func TestVersionID(t *testing.T) {
	if got := VersionID(clause2, 2); got != clause2+"@v2" {
		t.Errorf("VersionID = %s", got)
	}
}

func TestOperationIDSurvivesAnUnresolvedTarget(t *testing.T) {
	if got := OperationID(amendDoc, "", 0); !strings.Contains(got, "unresolved") {
		t.Errorf("an operation whose target did not resolve still needs an identifier, got %s", got)
	}
}
