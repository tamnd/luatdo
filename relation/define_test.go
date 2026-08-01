package relation

import (
	"context"
	"strings"
	"testing"
)

func sighting(typ, provision, doc, asWritten string) Edge {
	return Edge{
		FromID: "c1", ToID: "c2", Type: typ,
		Status: StatusProvisional, Source: SourceProvision, Why: WhyUnknownType,
		Definition: "X được miễn Y", Confidence: 0.8,
		Evidence:     []Evidence{{ProvisionID: provision, DocID: doc, Quote: "q", AsWritten: asWritten}},
		SupportCount: 1, SupportDocs: 1,
	}
}

func TestProposeFoldsBeforeAnybodyReadsTheLabel(t *testing.T) {
	// A label proposed once is a guess and a label proposed across forty
	// documents is the registry being incomplete. The two get told apart by
	// counting rather than by reading the label and forming an impression.
	edges := []Edge{
		sighting("DUOC_MIEN", "p1", "d1", "được miễn"),
		sighting("DUOC_MIEN", "p2", "d1", "không phải có"),
		sighting("DUOC_MIEN", "p3", "d2", "được miễn"),
		sighting("HIEM_KHI", "p4", "d3", "ít khi"),
		{FromID: "c1", ToID: "c2", Type: Requires, Status: StatusCanonical,
			Source: SourceDefinitional, Evidence: []Evidence{{ProvisionID: "p5", DocID: "d4"}}},
	}
	ps := Propose(edges)
	if len(ps) != 2 {
		t.Fatalf("proposals = %d, a canonical edge is not a proposal", len(ps))
	}
	if ps[0].Label != "DUOC_MIEN" {
		t.Errorf("first = %q, want the most seen one first", ps[0].Label)
	}
	if ps[0].Instances != 3 || ps[0].Docs != 2 {
		t.Errorf("instances = %d docs = %d, documents are counted apart from provisions",
			ps[0].Instances, ps[0].Docs)
	}
	if len(ps[0].AsWritten) != 2 {
		t.Errorf("as written = %v, want the distinct wordings the Define step generalises over", ps[0].AsWritten)
	}
	if ps[0].AsWritten[0] > ps[0].AsWritten[1] {
		t.Error("the wordings are not sorted, so two runs write different files")
	}
}

func TestProposeCapsTheExamplesAReviewerHasToRead(t *testing.T) {
	var edges []Edge
	for i := range 20 {
		edges = append(edges, sighting("DUOC_MIEN", "p"+string(rune('a'+i)), "d1", "được miễn"))
	}
	ps := Propose(edges)
	if len(ps) != 1 {
		t.Fatalf("proposals = %d", len(ps))
	}
	if ps[0].Instances != 20 {
		t.Errorf("instances = %d, the count is not capped", ps[0].Instances)
	}
	if len(ps[0].Examples) != MaxExamples {
		t.Errorf("examples = %d, a file holding all twenty is a file nobody opens", len(ps[0].Examples))
	}
}

func TestDefineWritesOneSentence(t *testing.T) {
	model := &answers{replies: []string{`{"definition":"  X được miễn nghĩa vụ có Y  "}`}}
	d := &Definer{Completer: model, Model: "m"}
	got, usage, err := d.Define(context.Background(), Proposal{Label: "DUOC_MIEN", Instances: 3, Docs: 2})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	if got != "X được miễn nghĩa vụ có Y" {
		t.Errorf("definition = %q, want it trimmed", got)
	}
	if usage.TotalTokens == 0 {
		t.Error("the definition pass reported no tokens, and a campaign cannot report what it does not add up")
	}
}

func TestDefineAsksAgainForAnEmptyDefinition(t *testing.T) {
	// A definition is what canonicalization matches on. An empty one leaves a
	// proposal that can never be decided, which is worse than a bad one because
	// nothing about it is reviewable.
	for name, bad := range map[string]string{
		"not json":       "tôi không biết",
		"no definition":  `{"definition":""}`,
		"only spaces":    `{"definition":"   "}`,
		"another object": `{"something_else":"x"}`,
	} {
		model := &answers{replies: []string{bad, `{"definition":"X được miễn Y"}`}}
		d := &Definer{Completer: model, Model: "m", MaxCorrections: 1}
		got, _, err := d.Define(context.Background(), Proposal{Label: "DUOC_MIEN"})
		if err != nil {
			t.Errorf("%s: Define: %v", name, err)
			continue
		}
		if got != "X được miễn Y" {
			t.Errorf("%s: definition = %q", name, got)
		}
		if model.calls != 2 {
			t.Errorf("%s: calls = %d, want one correction", name, model.calls)
		}
	}

	model := &answers{replies: []string{`{"definition":""}`}}
	d := &Definer{Completer: model, Model: "m"}
	if _, _, err := d.Define(context.Background(), Proposal{Label: "DUOC_MIEN"}); err == nil {
		t.Error("a proposal with no definition was returned as defined")
	}
}

func TestCanonicalizeMatchesOnDefinitionsAndAcceptsNoMatch(t *testing.T) {
	// Two names can differ and mean the same thing, and no match is a first
	// class answer. A model pushed to always choose something would report the
	// registry as complete forever.
	p := Proposal{Label: "LA_DIEU_KIEN_DE", Definition: "Y phải có thì X mới được cấp"}

	model := &answers{replies: []string{`{"match":"REQUIRES","rationale":"cùng nghĩa","confidence":0.9}`}}
	c := &Canonicalizer{Completer: model, Model: "m"}
	got, why, _, err := c.Canonicalize(context.Background(), p)
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	if got != Requires || why == "" {
		t.Errorf("match = %q rationale = %q", got, why)
	}
	if !strings.Contains(model.inputs[0], "Y phải có thì X mới được cấp") {
		t.Error("the proposal definition was not put in front of the model, which is the only thing it can match on")
	}

	for name, reply := range map[string]string{
		"said none":  `{"match":"none","rationale":"không có quan hệ nào tương ứng","confidence":0.8}`,
		"said NONE":  `{"match":"NONE","rationale":"x","confidence":0.8}`,
		"said empty": `{"match":"","rationale":"x","confidence":0.8}`,
	} {
		c := &Canonicalizer{Completer: &answers{replies: []string{reply}}, Model: "m"}
		if got, _, _, err := c.Canonicalize(context.Background(), p); err != nil || got != "" {
			t.Errorf("%s: match = %q err = %v, want no match", name, got, err)
		}
	}
}

func TestCanonicalizeRefusesATypeTheRegistryDoesNotHold(t *testing.T) {
	// A match onto a type that does not exist would produce an edge whose type
	// nothing defines, which is the thing this step exists to prevent.
	model := &answers{replies: []string{
		`{"match":"MIEN_TRU","rationale":"x","confidence":0.9}`,
		`{"match":"REQUIRES","rationale":"y","confidence":0.9}`,
	}}
	c := &Canonicalizer{Completer: model, Model: "m", MaxCorrections: 1}
	got, _, _, err := c.Canonicalize(context.Background(), Proposal{Label: "L", Definition: "d"})
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	if got != Requires {
		t.Errorf("match = %q, want the corrected answer", got)
	}
	if !strings.Contains(model.inputs[1], "MIEN_TRU") {
		t.Error("the correction did not name the type it refused")
	}
}

func TestCanonicalizeTreatsAnUndecidableAnswerAsNoMatch(t *testing.T) {
	// Leaving the proposal provisional keeps it queryable and keeps it in the
	// review queue, which is where an undecidable case belongs.
	model := &answers{replies: []string{"không thể trả lời"}}
	c := &Canonicalizer{Completer: model, Model: "m", MaxCorrections: 1}
	got, _, _, err := c.Canonicalize(context.Background(), Proposal{Label: "L", Definition: "d"})
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	if got != "" {
		t.Errorf("match = %q, an answer nobody could read is not a match", got)
	}
}

func TestApplyRetypesWithoutPromoting(t *testing.T) {
	// Matching a type is not corroboration. An edge one provision produced under
	// an invented label is still an edge one provision produced once the label is
	// corrected, and promoting here would mint a canonical edge on one sighting.
	r := SeedRegistry(3)
	edges := []Edge{sighting("LA_DIEU_KIEN_DE", "p1", "d1", "phải có")}
	out := Apply(edges, []Proposal{{Label: "LA_DIEU_KIEN_DE", MatchedTo: Requires}}, r)

	if len(out) != 1 {
		t.Fatalf("edges = %d", len(out))
	}
	e := out[0]
	if e.Type != Requires {
		t.Errorf("type = %q, want the registry type it matched", e.Type)
	}
	if e.Status != StatusProvisional {
		t.Errorf("status = %q, one sighting is one sighting whatever its label says", e.Status)
	}
	if e.Why != WhySingleSupport {
		t.Errorf("why = %q, the type is decided so it is no longer what is holding the edge back", e.Why)
	}
	if e.Definition != "" {
		t.Error("the invented definition survived onto an edge whose type the registry defines")
	}
	if e.OntologyVersion != 3 {
		t.Errorf("version = %d, an edge cites the vocabulary it was matched against", e.OntologyVersion)
	}
	if err := e.Validate(r, nil); err != nil {
		t.Errorf("the retyped edge does not validate: %v", err)
	}

	// And once a second document says it too, the count is what promotes it.
	corroborated := Apply(
		[]Edge{sighting("LA_DIEU_KIEN_DE", "p1", "d1", "phải có"), sighting("LA_DIEU_KIEN_DE", "p2", "d2", "phải có")},
		[]Proposal{{Label: "LA_DIEU_KIEN_DE", MatchedTo: Requires}}, r)
	folded := Fold(corroborated, r, DefaultThresholds)
	if len(folded) != 1 {
		t.Fatalf("folded = %d, two sightings of one relation are one edge", len(folded))
	}
	if folded[0].Status != StatusCanonical {
		t.Errorf("status = %q why = %q, two provisions in two documents is the gate", folded[0].Status, folded[0].Why)
	}
}

func TestApplyLeavesAnUnmatchedProposalExactlyAsItWas(t *testing.T) {
	// Dropping loses the tail, which is where the interesting law is.
	r := SeedRegistry(1)
	edges := []Edge{sighting("DUOC_MIEN", "p1", "d1", "được miễn")}
	out := Apply(edges, []Proposal{{Label: "DUOC_MIEN"}}, r)
	if len(out) != 1 {
		t.Fatalf("edges = %d, an unmatched proposal must not disappear", len(out))
	}
	e := out[0]
	if e.Type != "DUOC_MIEN" || e.Status != StatusProvisional || e.Why != WhyUnknownType {
		t.Errorf("edge = %+v, want it untouched and visibly marked", e)
	}
	if e.Definition == "" {
		t.Error("the definition was cleared from an edge whose type nothing else defines")
	}
}

func TestApplyDoesNotTouchACanonicalEdge(t *testing.T) {
	r := SeedRegistry(1)
	e := Edge{FromID: "c1", ToID: "c2", Type: Broader, Status: StatusCanonical,
		Source: SourceDefinitional, Evidence: []Evidence{{ProvisionID: "p1"}}}
	out := Apply([]Edge{e}, []Proposal{{Label: Broader, MatchedTo: Requires}}, r)
	if out[0].Type != Broader {
		t.Errorf("type = %q, a canonical edge is not up for retyping", out[0].Type)
	}
}
