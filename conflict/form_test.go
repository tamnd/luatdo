package conflict

import (
	"testing"

	"github.com/tamnd/luatdo/norm"
)

// The fixtures every test in the package builds on.
//
// They are deliberately small and deliberately real in shape: a duty of an
// employer to notify, from the Labour Code, which is the worked example
// throughout this repository.

const (
	labourCode = "vn:law:2019:45-2019-qh14"
	clause1    = labourCode + ":article-94:clause-1"
	clause2    = labourCode + ":article-94:clause-2"
	decree     = "vn:law:2020:145-2020-nd-cp"
)

// record builds a trusted statement of the shape the parse pass meets.
func record(id, typ string) *norm.Record {
	return &norm.Record{
		ID: "vn:norm:" + id, DocID: labourCode, ProvisionID: clause1,
		Status: norm.StatusVerified,
		Statement: norm.Statement{
			Type:   typ,
			Bearer: &norm.Ref{Text: "Người sử dụng lao động", IsActor: true},
			Action: norm.Ref{Text: "thông báo cho người lao động biết"},
			Evidence: norm.Evidence{
				Quote: "Người sử dụng lao động phải thông báo cho người lao động biết trước 15 ngày.",
			},
		},
	}
}

// form builds a parsed form directly, for the tests that are about the checker
// rather than about the parser.
func form(id, op string) *Form {
	return &Form{
		StatementID: "vn:norm:" + id, DocID: labourCode, ProvisionID: clause1 + ":" + id,
		Operator: op, Party: "nguoi-su-dung-lao-dong", Act: "thong-bao",
		Canon:  Canon{Party: "người sử dụng lao động", Act: "thông báo"},
		Source: Source{Type: "duty", Action: "thông báo", Quote: "Câu " + id + "."},
	}
}

func TestOperatorOfMapsTheTypeFirst(t *testing.T) {
	cases := []struct {
		typ, modality, want string
		ok                  bool
	}{
		{"duty", "", Obligation, true},
		{"prohibition", "", Prohibition, true},
		{"permission", "", Permission, true},
		{"right", "", Right, true},
		// The type wins over a modality that says something else, because the
		// type is the field the extraction invariants enforce.
		{"duty", "permission", Obligation, true},
		// A type with no modality in it falls back on the free text field.
		{"procedure", "Obligation", Obligation, true},
		{"definition", "", "", false},
		{"procedure", "", "", false},
		{"exception", "bắt buộc", "", false},
	}
	for _, c := range cases {
		s := &norm.Statement{Type: c.typ, Modality: c.modality}
		got, ok := operatorOf(s)
		if got != c.want || ok != c.ok {
			t.Errorf("operatorOf(%q, %q) = %q, %v, want %q, %v", c.typ, c.modality, got, ok, c.want, c.ok)
		}
	}
}

func TestDraftFillsOnlyWhatNeedsNoModel(t *testing.T) {
	r := record("a", "duty")
	r.Statement.Bearer.ConceptID = "vn:concept:nguoi-su-dung-lao-dong"
	r.Statement.Sanction = &norm.Sanction{Text: "Phạt tiền từ 5.000.000 đồng", LegalBasis: "Điều 17"}
	r.Statement.Exceptions = []norm.Clause{
		{Kind: norm.ExcOverride, Text: "Trừ trường hợp Nghị định 145/2020/NĐ-CP quy định khác"},
		{Kind: norm.ExcCarveOut, Text: "Không áp dụng với lao động thử việc"},
	}

	f, ok := Draft(r)
	if !ok {
		t.Fatal("Draft refused a duty")
	}
	if f.Operator != Obligation {
		t.Errorf("operator = %q, want %q", f.Operator, Obligation)
	}
	if f.Party != "vn:concept:nguoi-su-dung-lao-dong" {
		t.Errorf("party = %q, want the concept identifier", f.Party)
	}
	// The act is the parser's job and Draft must not guess at it, or a store
	// with no parse pass would compare surface strings and call it canonical.
	if f.Act != "" {
		t.Errorf("act = %q, want empty until the parser fills it", f.Act)
	}
	if f.Sanction != "phat-tien-tu-5-000-000-dong" {
		t.Errorf("sanction slug = %q", f.Sanction)
	}
	if f.Canon.Sanction != "Phạt tiền từ 5.000.000 đồng" {
		t.Errorf("sanction wording = %q", f.Canon.Sanction)
	}
	// Only the override exception is a deferral, and it resolves to the same
	// identifier the document store uses. A carve out narrows this norm and does
	// not point at another instrument.
	if len(f.Scope.Defers) != 1 || f.Scope.Defers[0] != decree {
		t.Fatalf("defers = %v, want [%s] from the override exception", f.Scope.Defers, decree)
	}
	if f.Source.Quote != r.Statement.Evidence.Quote {
		t.Error("Draft dropped the quote, which is the only thing a reader can check the pair against")
	}
}

func TestDeferralsResolveOfficialNumbersAndNothingElse(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"Trừ trường hợp Nghị định 145/2020/NĐ-CP quy định khác", []string{decree}},
		{"trừ trường hợp Luật số 45/2019/QH14 có quy định khác", []string{labourCode}},
		{"theo Nghị định 145/2020/NĐ-CP và Luật số 45/2019/QH14", []string{decree, labourCode}},
		// No number is no deferral. The pair is then compared normally, which is
		// the safe direction: a finding a reader dismisses beats a pair removed
		// where nobody will see it.
		{"Trừ trường hợp pháp luật về bảo hiểm xã hội quy định khác", nil},
		{"", nil},
	}
	for _, c := range cases {
		got := deferrals(c.text)
		if len(got) != len(c.want) {
			t.Errorf("deferrals(%q) = %v, want %v", c.text, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("deferrals(%q)[%d] = %q, want %q", c.text, i, got[i], c.want[i])
			}
		}
	}
}

func TestDraftRefusesAStatementWithNoModality(t *testing.T) {
	for _, typ := range []string{"definition", "procedure", "exception"} {
		if _, ok := Draft(record("a", typ)); ok {
			t.Errorf("Draft accepted a %s, which states no modality over an act", typ)
		}
	}
}

func TestComparableNeedsAnOperatorAPartyAndAnAct(t *testing.T) {
	full := form("a", Obligation)
	if !full.Comparable() {
		t.Fatal("a complete form is not comparable")
	}
	for _, blank := range []func(*Form){
		func(f *Form) { f.Operator = "" },
		func(f *Form) { f.Party = "" },
		func(f *Form) { f.Act = "" },
	} {
		f := full.clone()
		blank(f)
		if f.Comparable() {
			t.Errorf("%+v is comparable with a field missing", f)
		}
	}
}

func TestOverlapsIsHalfOpen(t *testing.T) {
	cases := []struct {
		name                   string
		aFrom, aTo, bFrom, bTo string
		want                   bool
	}{
		{"identical", "2020-01-01", "2021-01-01", "2020-01-01", "2021-01-01", true},
		{"touching, a then b", "2020-01-01", "2021-01-01", "2021-01-01", "", false},
		{"touching, b then a", "2021-01-01", "", "2020-01-01", "2021-01-01", false},
		{"overlapping", "2020-01-01", "2022-01-01", "2021-01-01", "", true},
		{"disjoint", "2010-01-01", "2011-01-01", "2020-01-01", "2021-01-01", false},
		{"both open", "", "", "", "", true},
		{"a open ended", "2020-01-01", "", "2030-01-01", "", true},
		{"b started before time was recorded", "2020-01-01", "2021-01-01", "", "2019-01-01", false},
	}
	for _, c := range cases {
		a := Scope{From: c.aFrom, To: c.aTo}
		b := Scope{From: c.bFrom, To: c.bTo}
		if got := a.Overlaps(b); got != c.want {
			t.Errorf("%s: Overlaps = %v, want %v", c.name, got, c.want)
		}
		if got := b.Overlaps(a); got != c.want {
			t.Errorf("%s: reversed Overlaps = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCircumstancesReportsContainmentAndAdmitsWhenItCannotTell(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want string
	}{
		{"neither has a condition", nil, nil, CircumstancesShared},
		{"one is unconditional", nil, []string{"x"}, CircumstancesShared},
		{"a is contained in b", []string{"x"}, []string{"x", "y"}, CircumstancesShared},
		{"the same conditions", []string{"x", "y"}, []string{"y", "x"}, CircumstancesShared},
		{"neither contains the other", []string{"x"}, []string{"y"}, CircumstancesUnknown},
		{"partly overlapping", []string{"x", "z"}, []string{"x", "y"}, CircumstancesUnknown},
	}
	for _, c := range cases {
		got := Circumstances(Scope{Conditions: c.a}, Scope{Conditions: c.b})
		if got != c.want {
			t.Errorf("%s: Circumstances = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestKeyIncludesTheObject(t *testing.T) {
	a := form("a", Obligation)
	b := form("b", Prohibition)
	if a.Key() != b.Key() {
		t.Fatal("two forms with the same party and act pair on the key")
	}
	// An obligation to publish a decision and a prohibition on publishing
	// personal data are not a clash, and a key without the object makes them one.
	b.Object = "du-lieu-ca-nhan"
	if a.Key() == b.Key() {
		t.Error("forms with different objects share a pairing key")
	}
}

func TestWordsPrefersTheWordingAndFallsBackToTheSlug(t *testing.T) {
	f := form("a", Obligation)
	party, act, object := f.Words()
	if party != "người sử dụng lao động" || act != "thông báo" {
		t.Errorf("Words = %q, %q, want the canonical wording", party, act)
	}
	if object != "" {
		t.Errorf("object = %q, want empty", object)
	}
	f.Canon = Canon{}
	f.Object = "tien-luong"
	party, act, object = f.Words()
	if party != f.Party || act != f.Act || object != f.Object {
		t.Errorf("Words = %q, %q, %q, want the slugs where no wording was parsed", party, act, object)
	}
}

func TestSortFormsIsByIdentifier(t *testing.T) {
	fs := []*Form{form("c", Obligation), form("a", Obligation), form("b", Obligation)}
	SortForms(fs)
	for i, want := range []string{"vn:norm:a", "vn:norm:b", "vn:norm:c"} {
		if fs[i].StatementID != want {
			t.Fatalf("sorted[%d] = %s, want %s", i, fs[i].StatementID, want)
		}
	}
}
