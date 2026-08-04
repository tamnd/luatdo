package graph

import (
	"testing"
)

// The campaign in these tests is the labour side of the fixture: the code, the
// code it replaced, the decree that amends it, and the 1994 code at the end of
// the chain. It leaves out the social insurance law and the provincial
// decision, which is what makes it a cut rather than a copy.
func labourOnly() map[string]bool {
	return map[string]bool{fxCode: true, fxOldCode: true, fxDecree: true, fxAncient: true}
}

func TestRestrictKeepsOnlyTheDocumentsAsked(t *testing.T) {
	got := Restrict(competencyFixture(), labourOnly())
	if len(got.Docs) != 4 {
		t.Fatalf("documents: got %d, want 4", len(got.Docs))
	}
	for _, d := range got.Docs {
		if !labourOnly()[d.ID] {
			t.Errorf("%s is not in the campaign and came out of the cut anyway", d.ID)
		}
	}
	// The original is not touched. A cut that mutated its input would corrupt
	// the store for anything the command does after it.
	if len(competencyFixture().Docs) != 6 {
		t.Error("Restrict changed the projection it was given")
	}
}

func TestRestrictDropsEveryEdgeWithAnEndOutsideTheDump(t *testing.T) {
	got := Restrict(competencyFixture(), labourOnly())
	kept := labourOnly()
	for _, l := range got.Links {
		if !kept[l.FromDoc] || !kept[l.ToDoc] {
			t.Errorf("citation %s to %s crosses the edge of the dump", l.FromDoc, l.ToDoc)
		}
	}
	// The provincial decision citing the decree is gone with the decision, and
	// neo4j-admin refuses an entire import over one relationship row naming a
	// node the nodes file does not hold.
	if len(got.Links) != 3 {
		t.Errorf("citations: got %d, want the 3 that stay inside the campaign", len(got.Links))
	}
	for _, r := range got.Statements {
		if !kept[r.DocID] {
			t.Errorf("norm %s belongs to %s, which is not in the campaign", r.ID, r.DocID)
		}
	}
	// Two of the eleven norms live in the social insurance law.
	if len(got.Statements) != 9 {
		t.Errorf("norms: got %d, want 9", len(got.Statements))
	}
}

func TestRestrictKeepsAConceptOnlyWhileSomethingInTheDumpReachesIt(t *testing.T) {
	got := Restrict(competencyFixture(), labourOnly())
	reachable := map[string]bool{}
	for _, c := range got.Layer.Concepts {
		reachable[c.ID] = true
	}
	// The wage concept is reached by a term use the code defines and by two of
	// its norms. The authority is reached by norms alone, with no definition
	// anywhere, which is the case a naive cut on term uses would lose.
	for _, id := range []string{fxTL, fxCQCTQ, fxNLD, fxGPXD} {
		if !reachable[id] {
			t.Errorf("%s is used by something in the campaign and was cut anyway", id)
		}
	}
	// Nothing left in the dump mentions these. Keeping them would put isolated
	// nodes in the graph and a concept count that means nothing.
	for _, id := range []string{fxCQNN, fxUBND, fxHDLD, fxHDLV} {
		if reachable[id] {
			t.Errorf("%s has nothing left pointing at it and survived the cut", id)
		}
	}
	// The social insurance concept goes even though a labour norm names it,
	// which it does: article 99 is about it. So it stays.
	if !reachable[fxBHXH] {
		t.Error("article 99 is about social insurance and the concept was cut")
	}

	for _, e := range got.Relations {
		if !reachable[e.FromID] || !reachable[e.ToID] {
			t.Errorf("relation %s to %s hangs off a concept that is not in the dump", e.FromID, e.ToID)
		}
	}
	// The design file is reached by nothing in the norm layer inside this
	// campaign. It arrives because the decree's application act names it as the
	// thing submitted, and that is the whole argument for letting an act keep a
	// concept alive: without it the dump holds the act and none of the parties
	// to it.
	if !reachable[fxHSTK] {
		t.Error("the design file is a party to an act the dump keeps and was cut anyway")
	}
	// The hierarchy under the state agency goes with the concepts. What stays is
	// the wage to insurance edge and the permit prerequisite, the second of which
	// only survives because the act layer kept the design file.
	if len(got.Relations) != 2 {
		t.Errorf("relations: got %d, want the two edges whose ends are both still inside", len(got.Relations))
	}
}

func TestRestrictDropsATermUseAndTheDifferenceThatNeededBothHalves(t *testing.T) {
	got := Restrict(competencyFixture(), labourOnly())
	for _, tu := range got.Layer.TermUses {
		if tu.DocID == fxSocial {
			t.Errorf("term use %s belongs to a document outside the campaign", tu.ID)
		}
	}
	// A difference is a claim about two term uses at once. Half of one is not a
	// weaker claim, it is a different claim nobody made.
	if len(got.Layer.Differences) != 0 {
		t.Errorf("differences: got %d, want 0 now that one side of the only one is gone", len(got.Layer.Differences))
	}
	if len(got.Layer.Memberships) != 3 {
		t.Errorf("memberships: got %d, want the 3 whose term use survived", len(got.Layer.Memberships))
	}
}

func TestRestrictDropsAnEventThatNoLongerChangesAnything(t *testing.T) {
	got := Restrict(competencyFixture(), labourOnly())
	for _, v := range got.Temporal.Versions {
		if v.DocID == fxSocial {
			t.Errorf("version %s belongs to a document outside the campaign", v.ID)
		}
	}
	if len(got.Temporal.Versions) != 5 {
		t.Errorf("versions: got %d, want 5 of the 6", len(got.Temporal.Versions))
	}
	// The social enactment produced one version, in a document the campaign
	// excluded. An event node with no edges tells a reader nothing except that
	// the dump is partial, which the document count already said.
	for _, e := range got.Temporal.Events {
		if e.ID == "vn:event:fixture-enact-social" {
			t.Error("an event that opens and closes nothing in the dump survived the cut")
		}
		if len(e.Produces) == 0 && len(e.Terminates) == 0 {
			t.Errorf("event %s has no effect left and survived the cut", e.ID)
		}
	}
	if len(got.Temporal.Events) != 4 {
		t.Errorf("events: got %d, want 4 of the 5", len(got.Temporal.Events))
	}
}

func TestARestrictedProjectionStillExports(t *testing.T) {
	dir := t.TempDir()
	in := Restrict(competencyFixture(), labourOnly())
	if err := Export(dir, in); err != nil {
		t.Fatalf("export a campaign dump: %v", err)
	}
	// The point of the cut is a dump somebody can be handed, and a dump whose
	// summary disagrees with its own files is not one.
	labels, types := vocabulary(t, dir)
	if !labels["Document"] || !types["CONTAINS"] {
		t.Error("the campaign dump does not hold a document graph")
	}
	summary := Summarize(in)
	if summary.Documents != 4 {
		t.Errorf("the summary reports %d documents for a 4 document campaign", summary.Documents)
	}
}
