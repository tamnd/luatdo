package relation

import (
	"context"
	"strings"
	"testing"
)

func seen(provision, doc string, confidence float64) Edge {
	return Edge{
		FromID: "c1", ToID: "c2", Type: Requires,
		Status: StatusProvisional, Source: SourceProvision, Why: WhySingleSupport,
		Confidence: confidence,
		Evidence: []Evidence{{ProvisionID: provision, DocID: doc,
			Quote: "q" + provision, CharStart: 0, CharEnd: 2}},
		SupportCount: 1, SupportDocs: 1,
	}
}

func TestFoldPromotesOnCorroborationAcrossDocuments(t *testing.T) {
	r := SeedRegistry(1)
	out := Fold([]Edge{seen("p1", "d1", 0.9), seen("p2", "d2", 0.7)}, r, DefaultThresholds)
	if len(out) != 1 {
		t.Fatalf("edges = %d, two sightings of one relation are one edge", len(out))
	}
	e := out[0]
	if e.Status != StatusCanonical {
		t.Errorf("status = %q why = %q", e.Status, e.Why)
	}
	if e.SupportCount != 2 || e.SupportDocs != 2 {
		t.Errorf("support = %d in %d", e.SupportCount, e.SupportDocs)
	}
	if e.Source != SourceCorpus {
		t.Errorf("source = %q, a relation folded across provisions is a corpus claim", e.Source)
	}
	// The mean rather than the maximum, because forty weak sightings are forty
	// weak sightings and taking the best one launders volume into confidence.
	if e.Confidence != 0.8 {
		t.Errorf("confidence = %v, want the mean", e.Confidence)
	}
	if len(e.Evidence) != 2 {
		t.Errorf("evidence = %d, the folded edge carries what it was built from", len(e.Evidence))
	}
	if err := e.Validate(r, nil); err != nil {
		t.Errorf("a folded edge does not validate: %v", err)
	}
}

func TestFoldWillNotPromoteOnOneDocumentRepeatingItself(t *testing.T) {
	// Forty provisions of one decree are one drafter's habit, and a relation
	// supported inside a single document never promotes on volume alone.
	var edges []Edge
	for i := range 40 {
		edges = append(edges, seen("p"+strings.Repeat("x", i+1), "d1", 0.95))
	}
	out := Fold(edges, SeedRegistry(1), DefaultThresholds)
	if len(out) != 1 {
		t.Fatalf("edges = %d", len(out))
	}
	if out[0].SupportCount != 40 || out[0].SupportDocs != 1 {
		t.Fatalf("support = %d in %d", out[0].SupportCount, out[0].SupportDocs)
	}
	if out[0].Status != StatusProvisional || out[0].Why != WhySingleSupport {
		t.Errorf("status = %q why = %q, forty sightings in one document is one source",
			out[0].Status, out[0].Why)
	}
}

func TestFoldCountsProvisionsNotSightings(t *testing.T) {
	// One provision read twice, whether by a retry or by two passes over the
	// same document, is one provision. Counting it as two is how a single
	// sighting talks its own way past the corroboration gate.
	out := Fold([]Edge{seen("p1", "d1", 0.9), seen("p1", "d1", 0.9)}, SeedRegistry(1), DefaultThresholds)
	if len(out) != 1 {
		t.Fatalf("edges = %d", len(out))
	}
	if out[0].SupportCount != 1 {
		t.Errorf("support = %d, the same quote in the same provision is one sighting", out[0].SupportCount)
	}
	if len(out[0].Evidence) != 1 {
		t.Errorf("evidence = %d, want the duplicate dropped", len(out[0].Evidence))
	}
	if out[0].Status != StatusProvisional {
		t.Error("one provision read twice promoted itself")
	}
}

func TestFoldKeepsADefinitionalEdgeStandingAlone(t *testing.T) {
	// The drafter wrote it in a definition clause, which is the one source
	// allowed to stand on a single provision, and it is allowed because a person
	// wrote it rather than because a model was confident.
	def := Edge{FromID: "c1", ToID: "c2", Type: Broader,
		Status: StatusCanonical, Source: SourceDefinitional, Confidence: 0.95,
		Evidence:     []Evidence{{ProvisionID: "p1", DocID: "d1", Quote: "q"}},
		SupportCount: 1, SupportDocs: 1}
	out := Fold([]Edge{def}, SeedRegistry(1), DefaultThresholds)
	if out[0].Status != StatusCanonical {
		t.Errorf("status = %q why = %q", out[0].Status, out[0].Why)
	}

	// A definitional edge that is also used across ten provisions is still
	// definitional. Downgrading it to corpus would lose the one fact that lets
	// it stand alone.
	mixed := Fold([]Edge{def, {FromID: "c1", ToID: "c2", Type: Broader,
		Status: StatusProvisional, Source: SourceProvision, Confidence: 0.5,
		Evidence: []Evidence{{ProvisionID: "p2", DocID: "d2", Quote: "q2"}}}},
		SeedRegistry(1), DefaultThresholds)
	if mixed[0].Source != SourceDefinitional {
		t.Errorf("source = %q, the strongest source wins", mixed[0].Source)
	}
}

func TestFoldWillNotPromoteWhatTheBlindPassReadBackwards(t *testing.T) {
	// A graph with 95 percent relation precision and 80 percent direction
	// accuracy is worse than useless for traversal, because it answers
	// confidently and answers backwards.
	a, b := seen("p1", "d1", 0.9), seen("p2", "d2", 0.9)
	a.Direction, b.Direction = DirectionFlipped, DirectionFlipped
	out := Fold([]Edge{a, b}, SeedRegistry(1), DefaultThresholds)
	if out[0].Status != StatusProvisional || out[0].Why != WhyDirectionWrong {
		t.Errorf("status = %q why = %q, corroboration does not fix an arrow pointing the wrong way",
			out[0].Status, out[0].Why)
	}
}

func TestFoldReportsDisagreementRatherThanAveragingIt(t *testing.T) {
	// Two sightings that read opposite ways are disputed. The disagreement is
	// the finding, and averaging it away is throwing away the finding.
	for name, tc := range map[string]struct {
		a, b string
		want string
	}{
		"agreed and flipped":     {DirectionAgreed, DirectionFlipped, DirectionDisputed},
		"unverified and agreed":  {DirectionUnverified, DirectionAgreed, DirectionAgreed},
		"unclear and agreed":     {DirectionUnclear, DirectionAgreed, DirectionAgreed},
		"unclear and unverified": {DirectionUnclear, DirectionUnverified, DirectionUnclear},
		"both agreed":            {DirectionAgreed, DirectionAgreed, DirectionAgreed},
	} {
		a, b := seen("p1", "d1", 0.9), seen("p2", "d2", 0.9)
		a.Direction, b.Direction = tc.a, tc.b
		if got := Fold([]Edge{a, b}, SeedRegistry(1), DefaultThresholds)[0].Direction; got != tc.want {
			t.Errorf("%s: direction = %q, want %q", name, got, tc.want)
		}
	}
}

func TestFoldNeverPromotesATypeTheRegistryDoesNotHold(t *testing.T) {
	// Nothing else about the edge matters until the type is decided, and a type
	// with no definition behind it cannot be queried for anything.
	var edges []Edge
	for i, doc := range []string{"d1", "d2", "d3", "d4"} {
		e := seen("p"+strings.Repeat("y", i+1), doc, 0.9)
		e.Type = "DUOC_MIEN"
		e.Definition = "X được miễn Y"
		edges = append(edges, e)
	}
	out := Fold(edges, SeedRegistry(1), DefaultThresholds)
	if out[0].Status != StatusProvisional || out[0].Why != WhyUnknownType {
		t.Errorf("status = %q why = %q, four documents do not add a type to the registry",
			out[0].Status, out[0].Why)
	}
	if out[0].Definition == "" {
		t.Error("the definition was lost, and canonicalization has nothing to match on")
	}
}

func TestFoldIsDeterministic(t *testing.T) {
	edges := []Edge{
		seen("p2", "d2", 0.5),
		{FromID: "c3", ToID: "c1", Type: Broader, Status: StatusProvisional, Source: SourceProvision,
			Evidence: []Evidence{{ProvisionID: "p3", DocID: "d3", Quote: "q"}}},
		seen("p1", "d1", 0.9),
	}
	first := Fold(edges, SeedRegistry(1), DefaultThresholds)
	reversed := []Edge{edges[2], edges[0], edges[1]}
	second := Fold(reversed, SeedRegistry(1), DefaultThresholds)
	if len(first) != len(second) {
		t.Fatalf("%d edges one way and %d the other", len(first), len(second))
	}
	for i := range first {
		if first[i].Key() != second[i].Key() {
			t.Fatalf("row %d = %s one way and %s the other", i, first[i].Key(), second[i].Key())
		}
		if len(first[i].Evidence) != len(second[i].Evidence) {
			t.Errorf("row %d carries different evidence depending on input order", i)
		}
		for j := range first[i].Evidence {
			if first[i].Evidence[j].ProvisionID != second[i].Evidence[j].ProvisionID {
				t.Errorf("row %d evidence %d is in a different order", i, j)
			}
		}
	}
}

func TestFoldTreatsAZeroThresholdAsOne(t *testing.T) {
	// A threshold of nothing would promote everything, and a config with an
	// unset field is not a request to turn the gate off.
	out := Fold([]Edge{seen("p1", "d1", 0.9)}, SeedRegistry(1), Thresholds{})
	if out[0].Status != StatusCanonical {
		t.Errorf("status = %q, a threshold of one provision in one document is still a gate", out[0].Status)
	}
}

func TestConfirmShowsTheAggregateRatherThanOneSentence(t *testing.T) {
	// The claim being checked is a claim about the corpus, and checking it
	// against one provision would be checking a different claim.
	e := Fold([]Edge{seen("p1", "d1", 0.9), seen("p2", "d2", 0.8)}, SeedRegistry(1), DefaultThresholds)[0]
	model := &answers{replies: []string{`{"holds":true,"rationale":"trích dẫn p1 nói rõ","confidence":0.9}`}}
	c := &Confirmer{Completer: model, Model: "m"}

	holds, why, usage, err := c.Confirm(context.Background(), e, SeedRegistry(1), "giấy phép", "giấy chứng nhận")
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if !holds || why == "" {
		t.Errorf("holds = %v rationale = %q", holds, why)
	}
	if usage.TotalTokens == 0 {
		t.Error("the confirmation reported no tokens")
	}
	prompt := model.inputs[0]
	for _, want := range []string{"2, trong 2 văn bản", "qp1", "qp2", "giấy phép", "giấy chứng nhận"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt does not carry %q, and the counts are the claim", want)
		}
	}
	// The registry definition rather than the type name alone, since a name is
	// not a definition and the model is being asked about meaning.
	if !strings.Contains(prompt, SeedRegistry(1).Type(Requires).Definition) {
		t.Error("the prompt does not say what the relation means")
	}
}

func TestConfirmTruncatesTheQuotesAndSaysSo(t *testing.T) {
	var edges []Edge
	for i, doc := range []string{"d1", "d2", "d3", "d4", "d5", "d6", "d7", "d8"} {
		edges = append(edges, seen("p"+strings.Repeat("z", i+1), doc, 0.9))
	}
	e := Fold(edges, SeedRegistry(1), DefaultThresholds)[0]
	got := ConfirmationPrompt(e, SeedRegistry(1), "a", "b")
	if !strings.Contains(got, "và 3 trích dẫn khác") {
		t.Errorf("prompt does not say what it left out:\n%s", got)
	}
}

func TestConfirmTreatsAnUnreadableAnswerAsNotConfirmed(t *testing.T) {
	model := &answers{replies: []string{"không rõ"}}
	c := &Confirmer{Completer: model, Model: "m", MaxCorrections: 1}
	holds, _, _, err := c.Confirm(context.Background(), seen("p1", "d1", 0.9), SeedRegistry(1), "a", "b")
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if holds {
		t.Error("an answer nobody could read was taken as a confirmation")
	}
	if model.calls != 2 {
		t.Errorf("calls = %d, want the first try and one correction", model.calls)
	}
}

func TestConfirmerInstructionsSayWhatAFalseIsFor(t *testing.T) {
	c := &Confirmer{Completer: &answers{}, Model: "m"}
	got := c.Instructions()
	if !strings.Contains(got, "cùng xuất hiện") {
		t.Error("the prompt does not warn about co-occurrence, which is the commonest way a false edge looks true")
	}
	if !strings.Contains(got, "ngược chiều") {
		t.Error("the prompt does not say a backwards relation is a false one")
	}
}
