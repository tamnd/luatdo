package temporal

import (
	"testing"

	"github.com/tamnd/luatdo/law"
)

func TestParsePathReadsWhatDraftersWrite(t *testing.T) {
	cases := []struct {
		ref  string
		want string
		ok   bool
	}{
		{"khoản 2 Điều 15", "article-15:clause-2", true},
		{"Điều 15 khoản 2", "article-15:clause-2", true},
		{"điểm d khoản 1 Điều 20", "article-20:clause-1:point-d", true},
		// Đ is a letter of the alphabet, not a decorated d, and the two are
		// neighbouring points of the same clause all through this corpus.
		{"điểm đ khoản 1 Điều 20", "article-20:clause-1:point-dd", true},
		{"Điều 20", "article-20", true},
		{"Điều 15a", "article-15a", true},
		{"Chương II", "chapter-2", true},
		// A chapter named alongside an article is context rather than path.
		{"Điều 15 Chương II", "article-15", true},
		{"Luật này", "", false},
	}
	for _, c := range cases {
		got, ok := ParsePath(c.ref)
		if ok != c.ok || got != c.want {
			t.Errorf("ParsePath(%q) = %q, %v, want %q, %v", c.ref, got, ok, c.want, c.ok)
		}
	}
}

func amends() map[string][]string { return map[string][]string{amendDoc: {docID}} }

func read(kind, ref string) Operation {
	return Operation{
		ID: "op", Kind: kind, AmendingDoc: amendDoc, CausedBy: amendDoc + ":article-1",
		TargetRef: ref, EffectiveFrom: "2022-07-01",
	}
}

func TestResolveUsesTheStatedNumberFirst(t *testing.T) {
	o := read(KindAmend, "khoản 2 Điều 15")
	o.TargetNumber = "45/2019/QH14"
	got := Resolve([]Operation{o}, corpus(), nil)[0]
	if got.Quarantine != "" {
		t.Fatalf("quarantined as %s", got.Quarantine)
	}
	if got.TargetComponent != clause2 {
		t.Errorf("resolved to %s, want %s", got.TargetComponent, clause2)
	}
}

func TestResolveFallsBackToTheSingleAmendedDocument(t *testing.T) {
	got := Resolve([]Operation{read(KindAmend, "khoản 2 Điều 15")}, corpus(), amends())[0]
	if got.TargetDoc != docID {
		t.Errorf("target document is %q, want the one document this instrument amends", got.TargetDoc)
	}
}

func TestResolveRefusesToGuessBetweenTwoDocuments(t *testing.T) {
	two := map[string][]string{amendDoc: {docID, "vn:law:2020:1-2020-qh14"}}
	got := Resolve([]Operation{read(KindAmend, "khoản 2 Điều 15")}, corpus(), two)[0]
	if got.Quarantine != QuarantineMissingDocument {
		t.Errorf("two candidates with nothing to choose between them is unresolved, got %q", got.Quarantine)
	}
}

func TestResolveQuarantinesAComponentThatIsNotThere(t *testing.T) {
	got := Resolve([]Operation{read(KindAmend, "khoản 9 Điều 15")}, corpus(), amends())[0]
	if got.Quarantine != QuarantineUnresolvedTarget {
		t.Errorf("an amendment to a clause that does not exist must be quarantined, got %q", got.Quarantine)
	}
}

func TestCompound(t *testing.T) {
	cases := []struct {
		ref  string
		want bool
	}{
		{"khoản 2 Điều 15", false},
		{"điểm d khoản 1 Điều 20", false},
		{"Điều 20 khoản 1 điểm d", false},
		{"Luật này", false},
		{"điểm a và điểm b khoản 1 Điều 5", true},
		{"khoản 1 và khoản 2 Điều 5", true},
		{"Điều 5 và Điều 6", true},
		{"điểm d và điểm đ khoản 1 Điều 20", true},
	}
	for _, c := range cases {
		if got := Compound(c.ref); got != c.want {
			t.Errorf("Compound(%q) = %v, want %v", c.ref, got, c.want)
		}
	}
}

func TestResolveQuarantinesAReferenceNamingTwoComponents(t *testing.T) {
	// Nghị định số 278/2026/NĐ-CP writes it this way, and resolving it to điểm a
	// alone put the replacement text for both points on the first one.
	got := Resolve([]Operation{read(KindAmend, "điểm a và điểm b khoản 1 Điều 15")}, corpus(), amends())[0]
	if got.Quarantine != QuarantineCompoundTarget {
		t.Errorf("a reference naming two components must be quarantined, got %q", got.Quarantine)
	}
	if got.TargetComponent != "" {
		t.Errorf("a quarantined operation resolved to %s, it must resolve to nothing", got.TargetComponent)
	}
}

func TestResolveAllowsASupplementToNameSomethingNew(t *testing.T) {
	got := Resolve([]Operation{read(KindSupplement, "điểm d khoản 1 Điều 20")}, corpus(), amends())[0]
	if got.Quarantine != "" {
		t.Fatalf("a supplement creates the component it names, got %q", got.Quarantine)
	}
	if got.TargetComponent != pointD {
		t.Errorf("resolved to %s, want %s", got.TargetComponent, pointD)
	}
}

func TestResolveOnlyLetsReplaceNameAWholeInstrument(t *testing.T) {
	replace := Resolve([]Operation{read(KindReplace, "Luật này")}, corpus(), amends())[0]
	if replace.Quarantine != "" || replace.TargetComponent != docID {
		t.Errorf("replace names the whole instrument: %+v", replace)
	}
	amend := Resolve([]Operation{read(KindAmend, "Luật này")}, corpus(), amends())[0]
	if amend.Quarantine != QuarantineUnresolvedTarget {
		t.Errorf("an amendment that names no component has no target, got %q", amend.Quarantine)
	}
}

func TestResolveNeverTurnsAPhraseEditIntoAWildcard(t *testing.T) {
	o := read(KindAmend, "")
	o.Phrase = &PhraseEdit{Find: "x", Replace: "y", Targets: []string{"Điều 99", "Điều 98"}}
	got := Resolve([]Operation{o}, corpus(), amends())[0]
	if got.Quarantine != QuarantineNoTargets {
		t.Fatalf("a phrase edit whose targets are all missing must be quarantined, got %q", got.Quarantine)
	}
}

func TestResolveKeepsThePhraseTargetsThatExist(t *testing.T) {
	o := read(KindAmend, "")
	o.Phrase = &PhraseEdit{Find: "x", Replace: "y", Targets: []string{"Điều 15", "Điều 99"}}
	got := Resolve([]Operation{o}, corpus(), amends())[0]
	if got.Quarantine != "" {
		t.Fatalf("quarantined as %s", got.Quarantine)
	}
	if len(got.Phrase.Targets) != 1 || got.Phrase.Targets[0] != article15 {
		t.Errorf("targets resolved to %v", got.Phrase.Targets)
	}
}

func TestResolveRejectsAKindOutsideTheTen(t *testing.T) {
	got := Resolve([]Operation{read("modify", "Điều 15")}, corpus(), amends())[0]
	if got.Quarantine != QuarantineUnknownKind {
		t.Errorf("got %q", got.Quarantine)
	}
}

func TestCorpusIndexesOfficialNumbers(t *testing.T) {
	c := NewCorpus([]*law.Document{target(), nil})
	if id, ok := c.DocByNumber("45/2019/qh14"); !ok || id != docID {
		t.Errorf("DocByNumber = %q, %v", id, ok)
	}
	if len(c.Documents()) != 1 {
		t.Errorf("a nil document was indexed")
	}
}

// Construction and resolution have to spell a number the same way. This test
// builds the identifier the way the parser does and then asks resolution for
// the reference a drafter would write, which is the round trip that matters.
func TestResolutionSpellsNumbersTheWayTheParserDoes(t *testing.T) {
	clause := law.ProvisionID(law.ProvisionID(docID, "article", "20"), "clause", "1")
	for _, letter := range []string{"c", "d", "đ", "e"} {
		id := law.ProvisionID(clause, "point", letter)
		path, ok := ParsePath("điểm " + letter + " khoản 1 Điều 20")
		if !ok {
			t.Fatalf("điểm %s did not resolve to anything", letter)
		}
		if got := docID + ":" + path; got != id {
			t.Errorf("điểm %s resolves to %q, the parser wrote %q", letter, got, id)
		}
	}
}
