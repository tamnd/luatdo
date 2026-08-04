package event

import "testing"

func sighting(provisionID, docID, class, label string, parts ...Participant) Occurrence {
	return Occurrence{
		EventID: ID(class, label), Class: class, LabelVI: label, AsWritten: label,
		Participants: parts,
		Evidence: Evidence{
			ProvisionID: provisionID, DocID: docID,
			Quote: label + " tại " + provisionID, AsWritten: label,
		},
		Confidence: 0.9,
	}
}

func TestFoldMergesOneActAcrossTwoDocuments(t *testing.T) {
	in := []Occurrence{
		sighting("d1:art:5", "d1", "SUBMIT", "nộp hồ sơ", Participant{Role: RoleAgent, ConceptID: "c1", LabelVI: "người đề nghị"}),
		sighting("d2:art:9", "d2", "SUBMIT", "Nộp hồ sơ", Participant{Role: RoleAgent, ConceptID: "c1", LabelVI: "người đề nghị"}),
	}
	got := Fold(in, SeedRegistry(1), DefaultThresholds)
	if len(got) != 1 {
		t.Fatalf("events: got %d, want 1, because one act named in two documents is one node", len(got))
	}
	e := got[0]
	if e.SupportCount != 2 || e.SupportDocs != 2 {
		t.Errorf("support: got %d provisions in %d documents, want 2 in 2", e.SupportCount, e.SupportDocs)
	}
	if e.Status != StatusCanonical {
		t.Errorf("status: got %s with %s, want canonical", e.Status, e.Why)
	}
	if len(e.Participants) != 1 || e.Participants[0].SupportCount != 2 {
		t.Errorf("participants: got %+v, want one agent counted twice", e.Participants)
	}
	if len(e.Evidence) != 2 {
		t.Errorf("evidence: got %d quotes, want both", len(e.Evidence))
	}
}

func TestFoldHoldsAnActSeenInOneDocument(t *testing.T) {
	in := []Occurrence{
		sighting("d1:art:5", "d1", "SUBMIT", "nộp hồ sơ"),
		sighting("d1:art:7", "d1", "SUBMIT", "nộp hồ sơ"),
	}
	got := Fold(in, SeedRegistry(1), DefaultThresholds)
	if len(got) != 1 {
		t.Fatalf("events: got %d, want 1", len(got))
	}
	if got[0].Status != StatusProvisional || got[0].Why != WhySingleSupport {
		t.Errorf("status: got %s %s, want provisional on single support, because two provisions of one decree are one drafter",
			got[0].Status, got[0].Why)
	}
	if got[0].SupportCount != 2 || got[0].SupportDocs != 1 {
		t.Errorf("support: got %d in %d, want 2 in 1", got[0].SupportCount, got[0].SupportDocs)
	}
}

func TestFoldNeverPromotesAnInventedClass(t *testing.T) {
	in := []Occurrence{
		sighting("d1:art:5", "d1", "CHUYEN_NHUONG_CO_PHAN", "chuyển nhượng cổ phần"),
		sighting("d2:art:9", "d2", "CHUYEN_NHUONG_CO_PHAN", "chuyển nhượng cổ phần"),
		sighting("d3:art:1", "d3", "CHUYEN_NHUONG_CO_PHAN", "chuyển nhượng cổ phần"),
	}
	got := Fold(in, SeedRegistry(1), DefaultThresholds)
	if got[0].Status != StatusProvisional || got[0].Why != WhyUnknownClass {
		t.Errorf("status: got %s %s, want provisional on an unknown class however often it was seen", got[0].Status, got[0].Why)
	}
}

func TestFoldKeepsTheOtherWordingsAsAliases(t *testing.T) {
	a := sighting("d1:art:5", "d1", "SUBMIT", "nộp hồ sơ")
	b := sighting("d2:art:9", "d2", "SUBMIT", "nộp hồ sơ")
	b.AsWritten = "gửi hồ sơ"
	got := Fold([]Occurrence{a, b}, SeedRegistry(1), DefaultThresholds)
	if len(got[0].Aliases) != 1 || got[0].Aliases[0] != "gửi hồ sơ" {
		t.Errorf("aliases: got %v, want the other wording, because a merged node has to say what it merged", got[0].Aliases)
	}
}

func TestFoldAveragesConfidenceRatherThanTakingTheBest(t *testing.T) {
	a := sighting("d1:art:5", "d1", "SUBMIT", "nộp hồ sơ")
	a.Confidence = 0.4
	b := sighting("d2:art:9", "d2", "SUBMIT", "nộp hồ sơ")
	b.Confidence = 1.0
	got := Fold([]Occurrence{a, b}, SeedRegistry(1), DefaultThresholds)
	if got[0].Confidence != 0.7 {
		t.Errorf("confidence: got %v, want 0.7, because volume must not launder uncertainty", got[0].Confidence)
	}
}

func TestFoldCountsOneQuoteOnce(t *testing.T) {
	a := sighting("d1:art:5", "d1", "SUBMIT", "nộp hồ sơ")
	got := Fold([]Occurrence{a, a, a}, SeedRegistry(1), DefaultThresholds)
	if got[0].SupportCount != 1 {
		t.Errorf("support: got %d, want 1, because one sentence read three times is one sentence", got[0].SupportCount)
	}
}

func TestFoldIsTheSameWhateverOrderItReadsIn(t *testing.T) {
	in := []Occurrence{
		sighting("d2:art:9", "d2", "ISSUE", "cấp giấy phép"),
		sighting("d1:art:5", "d1", "SUBMIT", "nộp hồ sơ"),
		sighting("d3:art:1", "d3", "SUBMIT", "nộp hồ sơ"),
	}
	forwards := Fold(in, SeedRegistry(1), DefaultThresholds)
	reversed := Fold([]Occurrence{in[2], in[1], in[0]}, SeedRegistry(1), DefaultThresholds)
	if len(forwards) != len(reversed) {
		t.Fatalf("two orders gave %d and %d events", len(forwards), len(reversed))
	}
	for i := range forwards {
		if forwards[i].ID != reversed[i].ID {
			t.Errorf("at %d: got %s and %s from two orders of one input", i, forwards[i].ID, reversed[i].ID)
		}
	}
}

func chainIn(provisionID, docID, from, to string) Chain {
	return Chain{
		FromID: from, ToID: to, Type: Precedes,
		Evidence:   []Evidence{{ProvisionID: provisionID, DocID: docID, Quote: "sau khi " + provisionID, DirectionCheck: "trước rồi sau"}},
		Confidence: 0.9,
	}
}

func TestFoldChainsCorroboratesAcrossDocuments(t *testing.T) {
	events := []Event{{ID: "a"}, {ID: "b"}}
	in := []Chain{chainIn("d1:art:5", "d1", "a", "b"), chainIn("d2:art:9", "d2", "a", "b")}
	got := FoldChains(in, events, SeedRegistry(1), DefaultThresholds)
	if len(got) != 1 {
		t.Fatalf("chains: got %d, want 1", len(got))
	}
	if got[0].Status != StatusCanonical {
		t.Errorf("status: got %s %s, want canonical on two documents", got[0].Status, got[0].Why)
	}
	if got[0].SupportCount != 2 || got[0].SupportDocs != 2 {
		t.Errorf("support: got %d in %d", got[0].SupportCount, got[0].SupportDocs)
	}
}

func TestFoldChainsKeepsAReversedReadingApart(t *testing.T) {
	events := []Event{{ID: "a"}, {ID: "b"}}
	in := []Chain{chainIn("d1:art:5", "d1", "a", "b"), chainIn("d2:art:9", "d2", "b", "a")}
	got := FoldChains(in, events, SeedRegistry(1), DefaultThresholds)
	if len(got) != 2 {
		t.Fatalf("chains: got %d, want 2, because a chain read the other way round is a different claim", len(got))
	}
	for _, c := range got {
		if c.Status != StatusProvisional {
			t.Errorf("%s is %s, and two provisions that disagree are not corroboration", c.Key(), c.Status)
		}
	}
}

func TestFoldChainsHoldsWhatTheBlindPassReadBackwards(t *testing.T) {
	events := []Event{{ID: "a"}, {ID: "b"}}
	first := chainIn("d1:art:5", "d1", "a", "b")
	first.Direction = DirectionFlipped
	second := chainIn("d2:art:9", "d2", "a", "b")
	second.Direction = DirectionAgreed
	got := FoldChains([]Chain{first, second}, events, SeedRegistry(1), DefaultThresholds)
	if got[0].Direction != DirectionDisputed {
		t.Errorf("direction: got %s, want disputed, because the disagreement is the finding", got[0].Direction)
	}
	if got[0].Status != StatusProvisional || got[0].Why != WhyDirectionWrong {
		t.Errorf("status: got %s %s, want provisional on direction", got[0].Status, got[0].Why)
	}
}

func TestFoldChainsDropsAnEdgeToAnActNobodyExtracted(t *testing.T) {
	events := []Event{{ID: "a"}}
	got := FoldChains([]Chain{chainIn("d1:art:5", "d1", "a", "b")}, events, SeedRegistry(1), DefaultThresholds)
	if len(got) != 0 {
		t.Errorf("chains: got %d, want none, because an edge to a node with no evidence reads as a fact", len(got))
	}
}

func TestFoldChainsWithNoEventListKeepsEverything(t *testing.T) {
	// Folding sightings before the events are folded is a real call order, and
	// dropping every chain in it would be a silent loss.
	got := FoldChains([]Chain{chainIn("d1:art:5", "d1", "a", "b")}, nil, SeedRegistry(1), DefaultThresholds)
	if len(got) != 1 {
		t.Errorf("chains: got %d, want 1", len(got))
	}
}

func TestMergeDirectionPrefersAVerdictOverSilence(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{DirectionUnverified, DirectionAgreed, DirectionAgreed},
		{DirectionAgreed, DirectionUnverified, DirectionAgreed},
		{DirectionUnclear, DirectionFlipped, DirectionFlipped},
		{DirectionAgreed, DirectionAgreed, DirectionAgreed},
		{DirectionAgreed, DirectionFlipped, DirectionDisputed},
	}
	for _, c := range cases {
		if got := mergeDirection(c.a, c.b); got != c.want {
			t.Errorf("mergeDirection(%q, %q): got %q, want %q", c.a, c.b, got, c.want)
		}
	}
}
