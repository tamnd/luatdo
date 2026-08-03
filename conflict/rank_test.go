package conflict

import (
	"strings"
	"testing"
)

// corpus is the DocInfo a command builds from the document store, filled in
// with the two instruments the fixtures use.
func corpus(typeA, effectiveA, typeB, effectiveB string) *Docs {
	return &Docs{
		Types:      map[string]string{labourCode: typeA, decree: typeB},
		Effectives: map[string]string{labourCode: effectiveA, decree: effectiveB},
	}
}

func TestRankReportsAllThreeRulesAndNamesNoWinner(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	b.Scope.Conditions = []string{"trong-truong-hop-khan-cap"}
	docs := corpus("Bộ luật", "2021-01-01", "Nghị định", "2022-01-01")

	f := only(t, a, b, docs)
	if f == nil {
		t.Fatal("the pair was not reported")
	}
	if f.Rank.Superior != a.StatementID {
		t.Errorf("superior = %q, want the code over the decree", f.Rank.Superior)
	}
	if f.Rank.Posterior != b.StatementID {
		t.Errorf("posterior = %q, want the later instrument", f.Rank.Posterior)
	}
	if f.Rank.Specialis != b.StatementID {
		t.Errorf("specialis = %q, want the one with more conditions on it", f.Rank.Specialis)
	}
	if f.Rank.Agree {
		t.Error("the three rules point at different statements and Agree says they do not")
	}
}

func TestRankAgreesWhenTheRulesPointOneWay(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	a.Scope.Conditions = []string{"trong-truong-hop-khan-cap"}
	// The code is higher and it is also the later of the two, and it is the
	// more specific, so every rule with an opinion names the same statement.
	docs := corpus("Bộ luật", "2022-01-01", "Nghị định", "2021-01-01")
	f := only(t, a, b, docs)
	if f == nil {
		t.Fatal("the pair was not reported")
	}
	if !f.Rank.Agree {
		t.Errorf("Agree is false where every rule named %s: %+v", a.StatementID, f.Rank)
	}
}

func TestRankSaysNothingItCannotKnow(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	// Two instruments in the same tier get no lex superior answer. A circular of
	// one ministry does not outrank a circular of another, and pretending
	// otherwise would confidently resolve exactly the cases that need a person.
	docs := corpus("Thông tư", "2021-01-01", "Thông tư liên tịch", "2021-01-01")
	f := only(t, a, b, docs)
	if f == nil {
		t.Fatal("the pair was not reported")
	}
	if f.Rank.Superior != "" {
		t.Errorf("superior = %q for two instruments of one tier", f.Rank.Superior)
	}
	if f.Rank.Posterior != "" {
		t.Errorf("posterior = %q for two instruments effective the same day", f.Rank.Posterior)
	}
	if f.Rank.Specialis != "" {
		t.Errorf("specialis = %q where neither carries a condition", f.Rank.Specialis)
	}
	if f.Rank.String() != "" {
		t.Errorf("a rank with no opinion printed %q", f.Rank.String())
	}
	if !f.Rank.Agree {
		t.Error("no opinions is agreement, not disagreement")
	}
}

func TestRankIgnoresAnInstrumentTypeItDoesNotKnow(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	docs := corpus("Bộ luật", "", "Công văn", "")
	f := only(t, a, b, docs)
	if f == nil {
		t.Fatal("the pair was not reported")
	}
	if f.Rank.Superior != "" {
		t.Errorf("superior = %q, but nothing places an official letter in the hierarchy", f.Rank.Superior)
	}
}

func TestTierOfFoldsCaseAndSpacing(t *testing.T) {
	for _, spelling := range []string{"Nghị định", "nghị định", "  NGHỊ ĐỊNH  "} {
		got, ok := tierOf(spelling)
		if !ok || got != 5 {
			t.Errorf("tierOf(%q) = %d, %v", spelling, got, ok)
		}
	}
	if _, ok := tierOf("Chương trình"); ok {
		t.Error("an unknown instrument type was given a tier")
	}
}

func TestRankSurvivesADocInfoThatKnowsNothing(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	b.Scope.Conditions = []string{"x"}
	// A command run before the document store was loaded passes nil, and the
	// one rule that needs no document still has an opinion.
	r := rank(a, b, nil)
	if r.Superior != "" || r.Posterior != "" {
		t.Errorf("rank invented an instrument comparison out of nothing: %+v", r)
	}
	if r.Specialis != b.StatementID {
		t.Errorf("specialis = %q, want %q", r.Specialis, b.StatementID)
	}
}

func TestDescribeNamesProvisionsRatherThanHashes(t *testing.T) {
	a, b := pair(Obligation, Prohibition)
	docs := corpus("Bộ luật", "2021-01-01", "Nghị định", "2022-01-01")
	f := only(t, a, b, docs)
	s := Describe(f.Rank, a, b)
	if strings.Contains(s, a.StatementID) || strings.Contains(s, b.StatementID) {
		t.Errorf("Describe printed a statement hash: %s", s)
	}
	if !strings.Contains(s, a.ProvisionID) || !strings.Contains(s, b.ProvisionID) {
		t.Errorf("Describe did not name both provisions: %s", s)
	}
	if !strings.Contains(s, "higher instrument") || !strings.Contains(s, "later instrument") {
		t.Errorf("Describe dropped one of the rules: %s", s)
	}
}
