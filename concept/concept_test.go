package concept

import (
	"strings"
	"testing"
)

// clause is a real definition, byte for byte, from the 2019 Labour Code. The
// tests offset into it, so it has to be the real thing rather than a
// paraphrase: an offset check that runs against invented text proves nothing
// about the encoding the corpus actually holds.
const clause = "Người lao động là người làm việc cho người sử dụng lao động theo thỏa thuận, được trả lương và chịu sự quản lý, điều hành, giám sát của người sử dụng lao động."

func at(t *testing.T, quote string) (int, int) {
	t.Helper()
	i := strings.Index(clause, quote)
	if i < 0 {
		t.Fatalf("the test quote %q is not in the clause", quote)
	}
	return i, i + len(quote)
}

func labourCodeTerm(t *testing.T) TermUse {
	t.Helper()
	start, end := at(t, clause)
	return TermUse{
		ID:           TermUseID("vn:law:2019:45-2019-qh14", "Người lao động"),
		LabelVI:      "Người lao động",
		ScopeID:      "vn:law:2019:45-2019-qh14",
		DocID:        "vn:law:2019:45-2019-qh14",
		DefinitionVI: "người làm việc cho người sử dụng lao động theo thỏa thuận, được trả lương và chịu sự quản lý của người sử dụng lao động",
		Genus:        "người làm việc cho người sử dụng lao động",
		Differentiae: []Differentia{
			{Text: "làm việc theo thỏa thuận", Quote: "theo thỏa thuận"},
			{Text: "được trả lương", Quote: "được trả lương"},
			{Text: "chịu sự quản lý, điều hành, giám sát của người sử dụng lao động", Quote: "chịu sự quản lý, điều hành, giám sát"},
		},
		Kind:            KindActor,
		ReferencedTerms: []string{"người sử dụng lao động"},
		Origin:          OriginDefined,
		DefinedBy:       "vn:law:2019:45-2019-qh14:article-3:clause-1",
		Quote:           clause,
		CharStart:       start,
		CharEnd:         end,
		Confidence:      0.94,
	}
}

func TestAnIdentifierCarriesTheScopeSoTwoLawsDoNotCollide(t *testing.T) {
	labour := TermUseID("vn:law:2019:45-2019-qh14", "Người lao động")
	insurance := TermUseID("vn:law:2014:58-2014-qh13", "Người lao động")
	if labour == insurance {
		t.Fatalf("two instruments defining the same phrase minted one identifier: %s", labour)
	}
	if want := "vn:term:vn:law:2019:45-2019-qh14:nguoi-lao-dong"; labour != want {
		t.Errorf("TermUseID = %q, want %q", labour, want)
	}
	// Diacritics and case are folded, so the same term written two ways in two
	// articles of one law lands on one node instead of two.
	if TermUseID("d", "NGƯỜI LAO ĐỘNG") != TermUseID("d", "người lao động") {
		t.Error("case and diacritics changed the identifier")
	}
}

func TestAConceptIdentifierTakesADisambiguatorOnlyWhenGiven(t *testing.T) {
	if got, want := ConceptID("Người lao động", ""), "vn:concept:nguoi-lao-dong"; got != want {
		t.Errorf("ConceptID = %q, want %q", got, want)
	}
	if got, want := ConceptID("Đơn vị", "hành chính"), "vn:concept:don-vi:hanh-chinh"; got != want {
		t.Errorf("ConceptID = %q, want %q", got, want)
	}
}

func TestAReadingIsCheckedAgainstTheClauseItClaimsToHaveRead(t *testing.T) {
	good := labourCodeTerm(t)
	if err := good.Validate(clause); err != nil {
		t.Fatalf("a true reading was rejected: %v", err)
	}

	// Every one of these is a way a model has of being plausible and wrong, and
	// each has to fail in code rather than be caught by a reviewer.
	cases := []struct {
		name string
		bend func(*TermUse)
		want string
	}{
		{"a quote that is not in the clause", func(t *TermUse) {
			t.Quote = "Người lao động là công dân Việt Nam từ đủ 15 tuổi"
		}, "does not occur"},
		{"offsets that are merely plausible", func(t *TermUse) {
			t.Quote, t.CharStart, t.CharEnd = "được trả lương", 0, len("được trả lương")
		}, "not at 0"},
		{"offsets past the end of the clause", func(t *TermUse) { t.CharEnd = len(clause) + 40 }, "outside a clause"},
		{"a genus assembled rather than read", func(t *TermUse) {
			t.Genus = "cá nhân làm việc có hưởng lương"
		}, "genus"},
		{"a differentia the clause never states", func(t *TermUse) {
			t.Differentiae = append(t.Differentiae, Differentia{Text: "từ đủ 15 tuổi", Quote: "từ đủ 15 tuổi"})
		}, "not in the clause"},
		{"a kind outside the enum", func(t *TermUse) { t.Kind = "person" }, "not one of"},
		{"a label that slugs to nothing", func(t *TermUse) { t.LabelVI = "..." }, "slugs to nothing"},
		{"a confidence outside the range", func(t *TermUse) { t.Confidence = 1.4 }, "outside 0 to 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			term := labourCodeTerm(t)
			c.bend(&term)
			err := term.Validate(clause)
			if err == nil {
				t.Fatalf("accepted %s", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error is %q, which does not say %q", err, c.want)
			}
		})
	}
}

func TestADefinitionByReferenceMayNotAlsoCarryADefinition(t *testing.T) {
	const pointer = "Bảo hiểm xã hội được hiểu theo quy định tại Luật Bảo hiểm xã hội."
	start, end := 0, len(pointer)
	term := TermUse{
		ID:      TermUseID("vn:doc:1", "Bảo hiểm xã hội"),
		LabelVI: "Bảo hiểm xã hội",
		ScopeID: "vn:doc:1",
		Kind:    KindStatus,
		Origin:  OriginDefined,
		DefinesByReference: &Reference{
			Instrument: "Luật Bảo hiểm xã hội",
			Quote:      "được hiểu theo quy định tại Luật Bảo hiểm xã hội",
		},
		DefinedBy:  "vn:doc:1:article-2:clause-3",
		Quote:      pointer,
		CharStart:  start,
		CharEnd:    end,
		Confidence: 0.9,
	}
	if err := term.Validate(pointer); err != nil {
		t.Fatalf("a pointer definition was rejected: %v", err)
	}

	// The failure mode this guards against is the model filling in what the
	// other law says from memory. The corpus holds that law and the pipeline
	// will resolve the pointer against it; a remembered definition would look
	// identical and be unfalsifiable.
	term.DefinitionVI = "sự bảo đảm thay thế hoặc bù đắp một phần thu nhập của người lao động"
	err := term.Validate(pointer)
	if err == nil {
		t.Fatal("a pointer definition was allowed to carry a paraphrase")
	}
	if !strings.Contains(err.Error(), "paraphrase") {
		t.Errorf("error is %q", err)
	}
}

func TestTheLayerRefusesAMergeNobodyDecided(t *testing.T) {
	labour := labourCodeTerm(t)
	insurance := labour
	insurance.ScopeID = "vn:law:2014:58-2014-qh13"
	insurance.DocID = insurance.ScopeID
	insurance.ID = TermUseID(insurance.ScopeID, insurance.LabelVI)

	worker := Concept{ID: ConceptID("Người lao động", ""), LabelVI: "Người lao động", Kind: KindActor}
	layer := Layer{
		TermUses: []TermUse{labour, insurance},
		Concepts: []Concept{worker},
		Memberships: []Membership{
			{TermUseID: labour.ID, ConceptID: worker.ID, Relation: RelationSame,
				DecidedBy: "tamnd", DecidedAt: "2026-08-01T00:00:00Z", Rationale: "cùng định nghĩa, cùng phạm vi chủ thể"},
			{TermUseID: insurance.ID, ConceptID: worker.ID, Relation: RelationSame},
		},
	}
	problems := layer.Check()
	if len(problems) != 1 {
		t.Fatalf("Check found %d problems, want the one undecided merge:\n%s", len(problems), strings.Join(problems, "\n"))
	}
	if !strings.Contains(problems[0], "no decider or no rationale") {
		t.Errorf("problem is %q", problems[0])
	}
}

func TestTheLayerHoldsTwoInstrumentsDisagreeing(t *testing.T) {
	labour := labourCodeTerm(t)
	other := labour
	other.ScopeID = "vn:law:2014:58-2014-qh13"
	other.DocID = other.ScopeID
	other.ID = TermUseID(other.ScopeID, other.LabelVI)

	// This is the shape the whole layer exists for. Two laws use one phrase,
	// they do not mean the same thing, and the graph says so instead of picking
	// a winner or minting one node by string match.
	layer := Layer{
		TermUses: []TermUse{labour, other},
		Differences: []Difference{{
			FromID: labour.ID, ToID: other.ID,
			DecidedBy: "tamnd", DecidedAt: "2026-08-01T00:00:00Z",
			Rationale: "phạm vi bên bảo hiểm xã hội rộng hơn, gồm cả người làm việc không theo hợp đồng lao động",
			Basis:     []string{"theo thỏa thuận"},
		}},
	}
	if problems := layer.Check(); len(problems) != 0 {
		t.Fatalf("a recorded disagreement was reported as a problem:\n%s", strings.Join(problems, "\n"))
	}
}

func TestTheLayerCatchesTheWaysAMergeGoesWrong(t *testing.T) {
	labour := labourCodeTerm(t)
	dup := labour

	scopeless := labour
	scopeless.ScopeID = ""
	scopeless.ID = TermUseID("", scopeless.LabelVI)

	action := Concept{ID: ConceptID("Giao kết hợp đồng", ""), LabelVI: "Giao kết hợp đồng", Kind: KindAction}
	actor := Concept{ID: ConceptID("Người lao động", ""), LabelVI: "Người lao động", Kind: KindActor}
	decided := func(term, con string) Membership {
		return Membership{TermUseID: term, ConceptID: con, Relation: RelationSame,
			DecidedBy: "tamnd", DecidedAt: "2026-08-01T00:00:00Z", Rationale: "cùng nghĩa"}
	}

	layer := Layer{
		TermUses: []TermUse{labour, dup, scopeless},
		Concepts: []Concept{actor, action, {ID: "vn:concept:invented", LabelVI: "Khác", Kind: KindActor}},
		Memberships: []Membership{
			decided(labour.ID, actor.ID),
			decided(labour.ID, action.ID),
			decided("vn:term:nowhere:x", actor.ID),
			decided(labour.ID, "vn:concept:absent"),
		},
		Differences: []Difference{{FromID: labour.ID, ToID: labour.ID, DecidedBy: "tamnd", Rationale: "x"}},
	}

	want := []string{
		"is defined twice",
		"has no scope",
		"merged into two concepts",
		"is a actor and is merged into concept",
		"which does not exist",
		"which does not exist",
		"difference from",
		"does not match its label",
	}
	problems := layer.Check()
	joined := strings.Join(problems, "\n")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("Check missed %q:\n%s", w, joined)
		}
	}
}
