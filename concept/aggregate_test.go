package concept

import "testing"

// sighting builds one provision's discovery output. The provision identifier
// carries the instrument, so the scope counting is exercised by the identifiers
// rather than by a field somebody set by hand.
func sighting(provisionID, docID string, labels ...string) Sighting {
	s := Sighting{ProvisionID: provisionID, DocID: docID}
	for _, l := range labels {
		s.Candidates = append(s.Candidates, Candidate{
			LabelVI: l, Kind: KindThing, Quote: l, Shows: "dùng khái niệm này",
			Confidence: 0.9, ProvisionID: provisionID, DocID: docID,
		})
	}
	return s
}

func TestAggregateCountsDocumentsAndInstrumentsApart(t *testing.T) {
	// Four sightings inside one instrument is one drafter's habit. The same
	// four across three instruments is a concept the corpus operates on, and a
	// single number cannot tell those apart.
	sightings := []Sighting{
		sighting("vn:law:2019:45-2019-qh14:article-3:clause-1", "vn:law:2019:45-2019-qh14", "hợp đồng thử việc"),
		sighting("vn:law:2019:45-2019-qh14:article-24:clause-1", "vn:law:2019:45-2019-qh14", "hợp đồng thử việc"),
		sighting("vn:decree:2020:145-2020-nd-cp:article-5:clause-2", "vn:decree:2020:145-2020-nd-cp", "hợp đồng thử việc"),
		sighting("vn:circular:2021:10-2021-tt-bldtbxh:article-2", "vn:circular:2021:10-2021-tt-bldtbxh", "hợp đồng thử việc"),
	}

	aggs := Aggregate(sightings, nil, nil, nil)
	if len(aggs) != 1 {
		t.Fatalf("want one concept, got %d", len(aggs))
	}
	a := aggs[0]
	if a.Sighted != 4 {
		t.Errorf("sighted %d, want 4", a.Sighted)
	}
	if a.InDocs != 3 {
		t.Errorf("documents %d, want 3", a.InDocs)
	}
	if a.InScopes != 3 {
		t.Errorf("instruments %d, want 3", a.InScopes)
	}
}

func TestAggregateKeepsAnAnnexInsideItsOwnScope(t *testing.T) {
	sightings := []Sighting{
		sighting("vn:decree:2020:145-2020-nd-cp:article-5", "vn:decree:2020:145-2020-nd-cp", "mẫu số 01"),
		sighting("vn:decree:2020:145-2020-nd-cp:annex-1:article-2", "vn:decree:2020:145-2020-nd-cp", "mẫu số 01"),
	}
	aggs := Aggregate(sightings, nil, nil, nil)
	if aggs[0].InScopes != 2 {
		t.Errorf("an annex is its own scope, got %d", aggs[0].InScopes)
	}
	if aggs[0].InDocs != 1 {
		t.Errorf("an annex is not its own document, got %d", aggs[0].InDocs)
	}
}

func TestAggregateKeepsDisagreementAboutKindRatherThanResolvingIt(t *testing.T) {
	// A phrase two readings filed under two different kinds is usually two
	// concepts, and flattening it to a majority hides that.
	a := sighting("vn:law:2019:45-2019-qh14:article-3:clause-1", "vn:law:2019:45-2019-qh14", "người lao động")
	b := sighting("vn:decree:2020:145-2020-nd-cp:article-5", "vn:decree:2020:145-2020-nd-cp", "người lao động")
	b.Candidates[0].Kind = KindActor

	aggs := Aggregate([]Sighting{a, b}, nil, nil, nil)
	if len(aggs[0].Kinds) != 2 {
		t.Fatalf("both kinds should survive, got %v", aggs[0].Kinds)
	}
	if aggs[0].Kind == "" {
		t.Error("no dominant kind was chosen")
	}
}

func TestAggregateMarksWhatTheCorpusDefinesSomewhere(t *testing.T) {
	terms := []TermUse{{LabelVI: "Người lao động", Origin: OriginDefined}}
	sightings := []Sighting{
		sighting("vn:decree:2020:145-2020-nd-cp:article-5", "vn:decree:2020:145-2020-nd-cp", "người lao động", "người giúp việc"),
	}
	aggs := Aggregate(sightings, DefinedLabels(terms), nil, nil)
	byKey := map[string]bool{}
	for _, a := range aggs {
		byKey[a.Key] = a.DefinedSomewhere
	}
	if !byKey["nguoi-lao-dong"] {
		t.Error("a concept the corpus defines was not marked as defined")
	}
	if byKey["nguoi-giup-viec"] {
		t.Error("a concept nothing defines was marked as defined")
	}
}

func TestAggregateSortsSoTheMostUsedComesFirst(t *testing.T) {
	var sightings []Sighting
	sightings = append(sightings, sighting("vn:law:a:article-1", "vn:law:a", "hiếm"))
	for _, id := range []string{"vn:law:b", "vn:law:c", "vn:law:d"} {
		sightings = append(sightings, sighting(id+":article-1", id, "phổ biến"))
	}
	aggs := Aggregate(sightings, nil, nil, nil)
	if aggs[0].LabelVI != "phổ biến" {
		t.Errorf("most used should come first, got %q", aggs[0].LabelVI)
	}
}

func TestAggregateCapsTheProvisionList(t *testing.T) {
	var sightings []Sighting
	for i := range MaxProvisionsPerConcept + 50 {
		id := "vn:law:2019:45-2019-qh14:article-" + itoa(i)
		sightings = append(sightings, sighting(id, "vn:law:2019:45-2019-qh14", "khái niệm"))
	}
	aggs := Aggregate(sightings, nil, nil, nil)
	if len(aggs[0].Provisions) != MaxProvisionsPerConcept {
		t.Errorf("provision list is %d, want the cap of %d", len(aggs[0].Provisions), MaxProvisionsPerConcept)
	}
	if aggs[0].Sighted != MaxProvisionsPerConcept+50 {
		t.Errorf("the cap changed the count, got %d", aggs[0].Sighted)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestPromoteNeedsBothThresholds(t *testing.T) {
	aggs := []Aggregation{
		{Key: "a", LabelVI: "a", InDocs: 5, InScopes: 1},
		{Key: "b", LabelVI: "b", InDocs: 2, InScopes: 2},
		{Key: "c", LabelVI: "c", InDocs: 3, InScopes: 2},
	}
	promotions := Promote(aggs, DefaultThresholds)
	if len(promotions) != 1 || promotions[0].Key != "c" {
		t.Fatalf("want only c promoted, got %v", promotions)
	}
	if promotions[0].Rule != RuleFrequency {
		t.Errorf("rule %q, want %q", promotions[0].Rule, RuleFrequency)
	}
}

func TestPromoteLetsANormSlotPastTheFrequencyThreshold(t *testing.T) {
	// A concept a duty is borne by matters at two sightings, and the rule that
	// fired is stored so a reviewer can see why it is there on so little.
	aggs := []Aggregation{{Key: "a", LabelVI: "a", InDocs: 1, InScopes: 1, Sighted: 2, InNormSlot: true}}
	promotions := Promote(aggs, DefaultThresholds)
	if len(promotions) != 1 {
		t.Fatalf("want the norm slot concept promoted, got %d", len(promotions))
	}
	if promotions[0].Rule != RuleNormSlot {
		t.Errorf("rule %q, want %q", promotions[0].Rule, RuleNormSlot)
	}
}

func TestPromoteRefusesAConceptTheCorpusAlreadyDefines(t *testing.T) {
	// Pass B already made a TermUse for it, scoped to the instrument that
	// defined it, and minting a second node from usage would split it in half.
	aggs := []Aggregation{{Key: "a", LabelVI: "a", InDocs: 40, InScopes: 20, DefinedSomewhere: true}}
	if promotions := Promote(aggs, DefaultThresholds); len(promotions) != 0 {
		t.Fatalf("a defined concept was promoted from usage: %v", promotions)
	}
}

func TestPromoteToTermUsesFencesTheOrigin(t *testing.T) {
	aggs := []Aggregation{{
		Key: "thoi-gio-lam-viec", LabelVI: "thời giờ làm việc", Kind: KindAmount,
		InDocs: 4, InScopes: 3,
		Provisions: []string{"vn:decree:2020:145-2020-nd-cp:article-5:clause-1"},
	}}
	terms := PromoteToTermUses(Promote(aggs, DefaultThresholds), aggs)
	if len(terms) != 1 {
		t.Fatalf("want one term use, got %d", len(terms))
	}
	term := terms[0]
	if term.Origin != OriginUndefinedUsage {
		t.Errorf("origin %q, want %q, which is the fence everything downstream reads", term.Origin, OriginUndefinedUsage)
	}
	if term.ScopeID != UndefinedScope {
		t.Errorf("scope %q, want %q: a promoted concept has no defining instrument to be scoped to", term.ScopeID, UndefinedScope)
	}
	if term.DefinitionVI != "" {
		t.Error("a promoted concept must not carry a definition, because nobody wrote one")
	}
	if term.DocID != "vn:decree:2020:145-2020-nd-cp" {
		t.Errorf("document %q, want the instrument the first sighting was in", term.DocID)
	}
}

func TestDefinedLabelsIgnoresWhatUsagePromoted(t *testing.T) {
	// Otherwise the second aggregation run would see the first run's promotions
	// as definitions, and question 6 would answer nothing forever after.
	terms := []TermUse{
		{LabelVI: "Người lao động", Origin: OriginDefined, Aliases: []string{"NLĐ"}},
		{LabelVI: "thời giờ làm việc", Origin: OriginUndefinedUsage},
	}
	defined := DefinedLabels(terms)
	if !defined["nguoi-lao-dong"] || !defined["nld"] {
		t.Errorf("a defined term or its alias is missing: %v", defined)
	}
	if defined["thoi-gio-lam-viec"] {
		t.Error("a concept promoted from usage was counted as defined")
	}
}

func TestScopeOfProvision(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"vn:law:2019:45-2019-qh14:article-3:clause-1", "vn:law:2019:45-2019-qh14"},
		{"vn:law:2019:45-2019-qh14:chapter-2:article-3", "vn:law:2019:45-2019-qh14"},
		{"vn:decree:2020:145-2020-nd-cp:annex-1:article-2", "vn:decree:2020:145-2020-nd-cp:annex-1"},
		{"vn:law:2019:45-2019-qh14", "vn:law:2019:45-2019-qh14"},
	} {
		if got := scopeOfProvision(c.in); got != c.want {
			t.Errorf("scope of %s is %s, want %s", c.in, got, c.want)
		}
	}
}
