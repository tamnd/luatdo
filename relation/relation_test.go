package relation

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/concept"
)

const permit = "vn:concept:giay-phep-xay-dung"
const certificate = "vn:concept:giay-chung-nhan-quyen-su-dung-dat"

func kinds() Kinds {
	return Kinds{
		permit:      concept.KindArtifact,
		certificate: concept.KindArtifact,
	}
}

func edge() Edge {
	return Edge{
		FromID: permit, ToID: certificate, Type: Requires,
		Status: StatusCanonical, Source: SourceCorpus,
		SupportCount: 4, SupportDocs: 3, Confidence: 0.9,
		Evidence: []Evidence{{ProvisionID: "vn:law:2014:50-2014-qh13:article-95:clause-1", Quote: "giấy tờ chứng minh quyền sử dụng đất"}},
	}
}

func TestSeedHasNoGenericRelation(t *testing.T) {
	// The generic attractor is a model under uncertainty reaching for the
	// vaguest relation available. There is nothing in the seed set for it to
	// reach for, which is the mitigation.
	for _, ty := range Seed {
		if Forbidden[ty.ID] {
			t.Errorf("%s is in the seed set and is forbidden", ty.ID)
		}
	}
}

func TestEverySeedTypeCarriesADefinition(t *testing.T) {
	// Canonicalization matches on definitions. A type without one cannot be
	// matched against, so it silently stops being reachable.
	for _, ty := range Seed {
		if strings.TrimSpace(ty.Definition) == "" {
			t.Errorf("%s has no definition", ty.ID)
		}
		if !strings.Contains(ty.Definition, "X") || !strings.Contains(ty.Definition, "Y") {
			t.Errorf("%s does not say which end is which: %q", ty.ID, ty.Definition)
		}
	}
}

func TestSeedDomainsAndRangesUseTheConceptKindEnum(t *testing.T) {
	for _, ty := range Seed {
		for _, k := range append(append([]string{}, ty.Domain...), ty.Range...) {
			if !concept.ValidKind(k) {
				t.Errorf("%s names kind %q, which is not in the enum", ty.ID, k)
			}
		}
	}
}

func TestValidateAcceptsACorroboratedEdge(t *testing.T) {
	if err := edge().Validate(SeedRegistry(1), kinds()); err != nil {
		t.Errorf("a good edge was rejected: %v", err)
	}
}

func TestValidateRefusesASelfEdge(t *testing.T) {
	e := edge()
	e.ToID = e.FromID
	if err := e.Validate(SeedRegistry(1), kinds()); err == nil {
		t.Error("a concept was allowed to relate to itself")
	}
}

func TestValidateRefusesAnIdentityRelation(t *testing.T) {
	// Identity belongs to a person with a written rationale, and a model that
	// could emit SAME_AS here would be minting identity out of a sentence.
	for _, typ := range []string{"SAME_AS", "INSTANCE_OF", "DIFFERS_FROM"} {
		e := edge()
		e.Type = typ
		if err := e.Validate(SeedRegistry(1), kinds()); err == nil {
			t.Errorf("%s was allowed", typ)
		}
	}
}

func TestValidateRefusesTheGenericRelation(t *testing.T) {
	e := edge()
	e.Type = "RELATED_TO"
	e.Definition = "X có liên quan đến Y"
	if err := e.Validate(SeedRegistry(1), kinds()); err == nil {
		t.Error("RELATED_TO was allowed, and an edge that says only that two things are connected cannot be queried for anything")
	}
}

func TestValidateRefusesAnEdgeWithNoEvidence(t *testing.T) {
	e := edge()
	e.Evidence = nil
	if err := e.Validate(SeedRegistry(1), kinds()); err == nil {
		t.Error("an edge nobody can check was allowed")
	}
}

func TestValidateRefusesACanonicalEdgeOnOneProvision(t *testing.T) {
	// The corroboration threshold. It costs nothing but patience and it is the
	// cheapest available defence against one confident hallucination.
	e := edge()
	e.SupportCount, e.SupportDocs = 1, 1
	if err := e.Validate(SeedRegistry(1), kinds()); err == nil {
		t.Error("a canonical edge stood on a single provision")
	}
}

func TestValidateLetsADefinitionalEdgeStandAlone(t *testing.T) {
	// The one exception, and it is allowed because a person drafting a statute
	// wrote it rather than because a model was confident about it.
	e := edge()
	e.Type = Broader
	e.ToID = "vn:concept:giay-to"
	e.Source = SourceDefinitional
	e.SupportCount, e.SupportDocs = 1, 1
	k := kinds()
	k["vn:concept:giay-to"] = concept.KindArtifact
	if err := e.Validate(SeedRegistry(1), k); err != nil {
		t.Errorf("a definitional edge was rejected: %v", err)
	}
}

func TestValidateRefusesAnUnknownTypeWithNoDefinition(t *testing.T) {
	e := edge()
	e.Type = "PHAI_NOP_KEM"
	e.Status = StatusProvisional
	if err := e.Validate(SeedRegistry(1), kinds()); err == nil {
		t.Error("a type nobody defined was allowed, and it can never be canonicalized or reviewed")
	}
}

func TestValidateRefusesACanonicalUnknownType(t *testing.T) {
	e := edge()
	e.Type = "PHAI_NOP_KEM"
	e.Definition = "X phải nộp kèm Y"
	if err := e.Validate(SeedRegistry(1), kinds()); err == nil {
		t.Error("a type outside the registry was canonical")
	}
}

func TestValidateEnforcesDomainAndRange(t *testing.T) {
	// A PRODUCES whose subject is an artifact fails, which is invariant 2 and is
	// the cheapest catch for a direction flip that happens to cross kinds.
	e := edge()
	e.Type = Produces
	if err := e.Validate(SeedRegistry(1), kinds()); err == nil {
		t.Error("PRODUCES was allowed from an artifact")
	}
}

func TestValidateEnforcesSameKind(t *testing.T) {
	e := edge()
	e.Type = Broader
	k := kinds()
	k[certificate] = concept.KindAction
	if err := e.Validate(SeedRegistry(1), k); err == nil {
		t.Error("BROADER held between an artifact and an action")
	}
}

func TestValidateDoesNotFailAConstraintNobodyCouldEvaluate(t *testing.T) {
	// An endpoint whose kind nothing recorded is missing evidence rather than a
	// violation, and failing it would reject every edge touching a concept the
	// layer has not read yet.
	e := edge()
	e.Type = Produces
	if err := e.Validate(SeedRegistry(1), Kinds{}); err != nil {
		t.Errorf("an unknown kind was treated as a wrong kind: %v", err)
	}
}

func TestValidateRefusesACanonicalEdgeTheBlindPassFlipped(t *testing.T) {
	e := edge()
	e.Direction = DirectionFlipped
	if err := e.Validate(SeedRegistry(1), kinds()); err == nil {
		t.Error("an edge the verifier read backwards stayed canonical")
	}
}

func TestSortIsStableAcrossOrderings(t *testing.T) {
	a := []Edge{
		{FromID: "b", Type: Requires, ToID: "c"},
		{FromID: "a", Type: Requires, ToID: "z"},
		{FromID: "a", Type: Broader, ToID: "y"},
	}
	b := []Edge{a[1], a[2], a[0]}
	Sort(a)
	Sort(b)
	for i := range a {
		if a[i].Key() != b[i].Key() {
			t.Fatalf("two orderings sorted differently at %d: %s and %s", i, a[i].Key(), b[i].Key())
		}
	}
}

func TestReverseKeepsTheEvidence(t *testing.T) {
	// A flipped edge is a different fact about the same sentence, so the
	// sentence travels with it.
	r := edge().Reverse()
	if r.FromID != certificate || r.ToID != permit {
		t.Errorf("endpoints did not swap: %s %s", r.FromID, r.ToID)
	}
	if len(r.Evidence) != 1 {
		t.Error("the evidence was dropped")
	}
}
