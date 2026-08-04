package event

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/concept"
)

func TestSeedClassesAreDefinedAndDistinct(t *testing.T) {
	r := SeedRegistry(1)
	seen := map[string]bool{}
	for _, c := range r.Classes {
		if seen[c.ID] {
			t.Errorf("%s is in the registry twice", c.ID)
		}
		seen[c.ID] = true
		if c.ID != strings.ToUpper(c.ID) {
			t.Errorf("%s is not upper case", c.ID)
		}
		if c.Definition == "" {
			t.Errorf("%s has no definition, so nobody annotating can tell what belongs in it", c.ID)
		}
		if strings.Count(c.Definition, ".") > 1 {
			t.Errorf("%s has a definition of more than one sentence: %q", c.ID, c.Definition)
		}
		if Forbidden[c.ID] {
			t.Errorf("%s is both a seed class and a forbidden generic name", c.ID)
		}
		for _, role := range c.Roles {
			if !validRole(role) {
				t.Errorf("%s expects the role %s which is not one of the roles", c.ID, role)
			}
		}
	}
	if len(r.Classes) == 0 {
		t.Fatal("the seed registry is empty")
	}
}

func TestRegistryLooksUpByIDAndSaysNothingForTheRest(t *testing.T) {
	r := SeedRegistry(3)
	if r.Version != 3 {
		t.Errorf("version: got %d, want 3", r.Version)
	}
	if c := r.Class("SUBMIT"); c == nil {
		t.Fatal("SUBMIT is a seed class and the registry does not hold it")
	}
	if c := r.Class("CHUYEN_NHUONG_CO_PHAN"); c != nil {
		t.Errorf("the registry claims to hold an invented class: %+v", c)
	}
	// Registry order and not alphabetical order, because the seed is written in
	// the order a procedure runs and a reader of the prompt gets that for free.
	ids := r.IDs()
	if len(ids) != len(r.Classes) {
		t.Fatalf("IDs: got %d, want %d", len(ids), len(r.Classes))
	}
	for i, id := range ids {
		if id != r.Classes[i].ID {
			t.Errorf("IDs are not in registry order: got %s at %d, want %s", id, i, r.Classes[i].ID)
		}
	}
}

func TestForbiddenHoldsTheGenericAttractors(t *testing.T) {
	// These are the names a model reaches for when it has nothing to say, and an
	// event node meaning "something was done" answers no question.
	for _, name := range []string{"ACTION", "EVENT", "HANH_VI", "THUC_HIEN", "OTHER", "QUY_DINH"} {
		if !Forbidden[name] {
			t.Errorf("%s is not on the forbidden list", name)
		}
	}
}

func TestIDIsTheSameForOneActInTwoDocuments(t *testing.T) {
	a := ID("SUBMIT", "nộp hồ sơ đăng ký doanh nghiệp")
	b := ID("SUBMIT", "Nộp hồ sơ đăng ký doanh nghiệp")
	if a != b {
		t.Errorf("one act got two identifiers: %s and %s", a, b)
	}
	if !strings.HasPrefix(a, "vn:event:submit:") {
		t.Errorf("identifier: got %s, want the vn:event:submit prefix", a)
	}
	if ID("SUBMIT", "") != "" {
		t.Errorf("an act with no label got an identifier: %q", ID("SUBMIT", ""))
	}
	if ID("ISSUE", "nộp hồ sơ") == ID("SUBMIT", "nộp hồ sơ") {
		t.Error("two classes of one label collapsed to one identifier, so a filing and an issuing would merge")
	}
}

func occurrence() Occurrence {
	return Occurrence{
		EventID: ID("SUBMIT", "nộp hồ sơ"), Class: "SUBMIT", LabelVI: "nộp hồ sơ",
		Participants: []Participant{
			{Role: RoleAgent, ConceptID: "c1", LabelVI: "người đề nghị"},
			{Role: RoleInstrument, ConceptID: "c2", LabelVI: "hồ sơ"},
		},
		Evidence:   Evidence{ProvisionID: "p1", Quote: "nộp hồ sơ", CharStart: 0, CharEnd: 9},
		Confidence: 0.9,
	}
}

func kinds() Kinds {
	return Kinds{"c1": concept.KindActor, "c2": concept.KindArtifact}
}

func TestValidateAcceptsAWellFormedOccurrence(t *testing.T) {
	if err := occurrence().Validate(SeedRegistry(1), kinds()); err != nil {
		t.Fatalf("a well formed occurrence was rejected: %v", err)
	}
}

func TestValidateRefusesAGenericClass(t *testing.T) {
	o := occurrence()
	o.Class = "HANH_VI"
	o.EventID = ID(o.Class, o.LabelVI)
	err := o.Validate(SeedRegistry(1), kinds())
	if err == nil {
		t.Fatal("a generic class was accepted")
	}
	if !strings.Contains(err.Error(), "HANH_VI") {
		t.Errorf("the error does not name the class: %v", err)
	}
}

func TestValidateWantsASentenceForAnInventedClass(t *testing.T) {
	o := occurrence()
	o.Class = "CHUYEN_NHUONG_CO_PHAN"
	o.EventID = ID(o.Class, o.LabelVI)
	if err := o.Validate(SeedRegistry(1), kinds()); err == nil {
		t.Fatal("an invented class with no definition was accepted, so the candidates queue would hold a name and nothing to review it on")
	}
	o.Definition = "Một bên chuyển quyền sở hữu cổ phần cho bên khác."
	if err := o.Validate(SeedRegistry(1), kinds()); err != nil {
		t.Errorf("an invented class with a definition was rejected: %v", err)
	}
}

func TestValidateRefusesAParticipantOfTheWrongKind(t *testing.T) {
	o := occurrence()
	o.Participants = []Participant{{Role: RoleAuthority, ConceptID: "c2", LabelVI: "hồ sơ"}}
	err := o.Validate(SeedRegistry(1), kinds())
	if err == nil {
		t.Fatal("a form was accepted as the authority a filing is made to")
	}
	if !strings.Contains(err.Error(), RoleAuthority) {
		t.Errorf("the error does not name the role: %v", err)
	}
}

func TestValidateIsQuietAboutAConceptItDoesNotKnow(t *testing.T) {
	// The kind map is what the concept layer holds. A participant it has never
	// heard of is not evidence that the role is wrong, so the check stays out of
	// it rather than inventing a verdict.
	o := occurrence()
	o.Participants = []Participant{{Role: RoleAuthority, ConceptID: "c9", LabelVI: "cơ quan nào đó"}}
	if err := o.Validate(SeedRegistry(1), kinds()); err != nil {
		t.Errorf("a participant the concept layer does not hold was rejected: %v", err)
	}
}

func TestValidateRefusesTheSamePartyTwiceInOneSlot(t *testing.T) {
	o := occurrence()
	o.Participants = append(o.Participants, Participant{Role: RoleAgent, ConceptID: "c1"})
	if err := o.Validate(SeedRegistry(1), kinds()); err == nil {
		t.Fatal("one party was accepted twice in one role")
	}
}

func TestValidateRefusesAnIdentifierThatDoesNotMatchTheAct(t *testing.T) {
	o := occurrence()
	o.EventID = "vn:event:submit:nop-ho-so-khac"
	if err := o.Validate(SeedRegistry(1), kinds()); err == nil {
		t.Fatal("an occurrence whose identifier is not the one its class and label produce was accepted")
	}
}

func TestValidateRefusesAQuoteThatIsNotThere(t *testing.T) {
	o := occurrence()
	o.Evidence.Quote = ""
	if err := o.Validate(SeedRegistry(1), kinds()); err == nil {
		t.Fatal("an occurrence with no quote was accepted")
	}
}

func chain() Chain {
	return Chain{
		FromID: ID("SUBMIT", "nộp hồ sơ"), ToID: ID("ISSUE", "cấp giấy phép"),
		Type: Precedes, Status: StatusProvisional, Why: WhySingleSupport,
		Evidence:     []Evidence{{ProvisionID: "p1", Quote: "sau khi nhận hồ sơ", DirectionCheck: "nộp trước, cấp sau"}},
		SupportCount: 1, SupportDocs: 1,
	}
}

func TestChainValidateAcceptsAWellFormedChain(t *testing.T) {
	events := map[string]bool{chain().FromID: true, chain().ToID: true}
	if err := chain().Validate(events); err != nil {
		t.Fatalf("a well formed chain was rejected: %v", err)
	}
}

func TestChainValidateRefusesATypeNobodyDefined(t *testing.T) {
	c := chain()
	c.Type = "CAUSES"
	if err := c.Validate(nil); err == nil {
		t.Fatal("a chain type outside the closed set was accepted")
	}
}

func TestChainValidateRefusesAnEndTheLayerDoesNotHold(t *testing.T) {
	c := chain()
	if err := c.Validate(map[string]bool{c.FromID: true}); err == nil {
		t.Fatal("a chain to an act nobody extracted was accepted, so the export would carry an edge to nothing")
	}
}

func TestChainValidateRefusesAChainToItself(t *testing.T) {
	c := chain()
	c.ToID = c.FromID
	if err := c.Validate(nil); err == nil {
		t.Fatal("a chain from an act to itself was accepted")
	}
}

func TestChainValidateRefusesCanonicalOnOneProvision(t *testing.T) {
	c := chain()
	c.Status = StatusCanonical
	c.Why = ""
	if err := c.Validate(nil); err == nil {
		t.Fatal("a chain seen once was accepted as canonical")
	}
}

func TestChainValidateRefusesCanonicalWhenTheBlindPassDisagreed(t *testing.T) {
	c := chain()
	c.Status = StatusCanonical
	c.Why = ""
	c.SupportCount, c.SupportDocs = 3, 2
	c.Direction = DirectionFlipped
	err := c.Validate(nil)
	if err == nil {
		t.Fatal("a chain the blind pass read backwards was accepted as canonical")
	}
	if !strings.Contains(err.Error(), DirectionFlipped) {
		t.Errorf("the error does not say what the verifier read: %v", err)
	}
}

func TestChainKeyAndReverseAreDifferentClaims(t *testing.T) {
	c := chain()
	if c.Key() == c.Reverse().Key() {
		t.Error("a chain and its reverse have one key, so a backwards reading would fold into the right one")
	}
	if got := c.Reverse().Reverse(); got.Key() != c.Key() {
		t.Errorf("reversing twice: got %s, want %s", got.Key(), c.Key())
	}
}

func TestSortsAreStableAcrossRuns(t *testing.T) {
	events := []Event{{ID: "vn:event:pay:b"}, {ID: "vn:event:issue:a"}}
	SortEvents(events)
	if events[0].ID != "vn:event:issue:a" {
		t.Errorf("events are not sorted: %v", events)
	}

	chains := []Chain{
		{FromID: "b", Type: Precedes, ToID: "c"},
		{FromID: "a", Type: Triggers, ToID: "z"},
		{FromID: "a", Type: Precedes, ToID: "z"},
	}
	SortChains(chains)
	if chains[0].Type != Precedes || chains[0].FromID != "a" {
		t.Errorf("chains are not sorted: %+v", chains)
	}

	parts := []Participant{
		{Role: RoleInstrument, ConceptID: "c9"},
		{Role: RoleAgent, ConceptID: "c2"},
		{Role: RoleAgent, ConceptID: "c1"},
	}
	SortParticipants(parts)
	if parts[0].Role != RoleAgent || parts[0].ConceptID != "c1" {
		t.Errorf("participants are not sorted by role then concept: %+v", parts)
	}
}

func TestChainTypesAreDefinedAndClosed(t *testing.T) {
	for _, c := range ChainTypes {
		if c.Definition == "" {
			t.Errorf("%s has no definition", c.ID)
		}
		if !ValidChain(c.ID) {
			t.Errorf("%s is listed but ValidChain refuses it", c.ID)
		}
	}
	if ValidChain("precedes") {
		t.Error("a lower case chain type was accepted, so two spellings of one type would be two edges")
	}
}
