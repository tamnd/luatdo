package graph

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/event"
)

// column reads one named column out of a CSV row, by name rather than by
// position, because the header moves and a test that counts columns fails
// somewhere other than where the mistake is.
func column(t *testing.T, rows [][]string, row int, name string) string {
	t.Helper()
	for i, h := range rows[0] {
		if h == name || strings.HasPrefix(h, name+":") {
			return rows[row][i]
		}
	}
	t.Fatalf("no column %q in header %v", name, rows[0])
	return ""
}

// actRow finds the row for one act by identifier.
func actRow(t *testing.T, rows [][]string, id string) int {
	t.Helper()
	for i, r := range rows[1:] {
		if r[0] == id {
			return i + 1
		}
	}
	t.Fatalf("acts.csv holds no row for %s", id)
	return 0
}

func TestExportWritesTheActsAndEveryEdgeIntoThem(t *testing.T) {
	dir := t.TempDir()
	if err := Export(dir, competencyFixture()); err != nil {
		t.Fatalf("Export: %v", err)
	}

	acts := readCSV(t, filepath.Join(dir, "acts.csv"))
	if len(acts) != 7 {
		t.Fatalf("acts.csv holds %d rows including the header, want the fixture's 6 acts", len(acts))
	}
	if acts[0][0] != "id:ID" || acts[0][len(acts[0])-1] != ":LABEL" {
		t.Errorf("acts.csv header = %v, want the shape neo4j-admin reads", acts[0])
	}
	// The header is the column list the Bolt merge sets as properties, so a
	// column added to one path and not the other is a graph whose two loaders
	// disagree about what an act is.
	for _, name := range actColumns {
		if !slices.ContainsFunc(acts[0], func(h string) bool { return h == name || strings.HasPrefix(h, name+":") }) {
			t.Errorf("acts.csv has no %s column: %v", name, acts[0])
		}
	}
	for _, r := range acts[1:] {
		if r[len(r)-1] != ActLabel {
			t.Errorf("act row %v is not labelled %s", r, ActLabel)
		}
	}

	// The insurance contribution is the one act two instruments state, and the
	// row has to carry both the count and the two document identifiers, because
	// question 26 reports the second so a reader can check the first.
	insure := actRow(t, acts, fxDongBH)
	if got := column(t, acts, insure, "support_docs"); got != "2" {
		t.Errorf("support_docs on the insurance contribution = %q, want 2", got)
	}
	if got := column(t, acts, insure, "docs"); got != fxSocial+arrayDelimiter+fxCode {
		t.Errorf("docs = %q, want both instruments that state it", got)
	}
	if got := column(t, acts, insure, "status"); got != event.StatusCanonical {
		t.Errorf("status = %q, want canonical", got)
	}
	if column(t, acts, insure, "quote") == "" {
		t.Error("the act carries no quote, so nobody can check it against the provision")
	}

	// Paying wages is stated twice inside one instrument, which is a drafter
	// repeating themselves rather than the corpus agreeing. The projection ships
	// it and says so on the row.
	wages := actRow(t, acts, fxTraLuong)
	if got := column(t, acts, wages, "support"); got != "2" {
		t.Errorf("support on paying wages = %q, want 2", got)
	}
	if got := column(t, acts, wages, "support_docs"); got != "1" {
		t.Errorf("support_docs on paying wages = %q, want 1", got)
	}
	if got := column(t, acts, wages, "status"); got != event.StatusProvisional {
		t.Errorf("status on paying wages = %q, want provisional", got)
	}
	if column(t, acts, wages, "why") == "" {
		t.Error("a provisional act with no reason on it is a row a reviewer cannot act on")
	}

	chains := readCSV(t, filepath.Join(dir, "act_chains.csv"))
	if len(chains) != 5 {
		t.Fatalf("act_chains.csv holds %d rows including the header, want the 4 chains with both ends written", len(chains))
	}
	types := map[string]int{}
	for i := range chains[1:] {
		types[column(t, chains, i+1, ":TYPE")]++
	}
	for _, want := range ActChainTypes() {
		if types[want] != 1 {
			t.Errorf("chain type %s appears %d times, want 1", want, types[want])
		}
	}
	// The direction verdict rides on the edge, on every edge and not only on the
	// ones that came back wrong. An empty direction column would leave a reader
	// unable to tell a step nobody checked from a step that passed.
	for i := range chains[1:] {
		if column(t, chains, i+1, "direction") == "" {
			t.Errorf("chain %v carries no direction verdict", chains[i+1])
		}
	}

	participants := readCSV(t, filepath.Join(dir, "act_participants.csv"))
	if len(participants) != 9 {
		t.Fatalf("act_participants.csv holds %d rows including the header, want 8", len(participants))
	}
	roles := map[string]bool{}
	for i := range participants[1:] {
		roles[column(t, participants, i+1, "role")] = true
		if column(t, participants, i+1, ":TYPE") != HasParticipant {
			t.Errorf("participant row %v has the wrong type", participants[i+1])
		}
	}
	// The role is a property and not a relationship type, and it is the property
	// question 26 prints, so an empty one is an edge that says a concept is
	// involved without saying how.
	for _, want := range []string{event.RoleAgent, event.RoleObject, event.RoleRecipient} {
		if !roles[want] {
			t.Errorf("no participant edge carries the %s role", want)
		}
	}

	about := readCSV(t, filepath.Join(dir, "about_act.csv"))
	if len(about) != 9 {
		t.Fatalf("about_act.csv holds %d rows including the header, want the 8 links whose statement was written", len(about))
	}
	slots := map[string]int{}
	for i := range about[1:] {
		slots[column(t, about, i+1, "slot")]++
	}
	if slots[event.LinkAction] != 7 || slots[event.LinkSanction] != 1 {
		t.Errorf("slots = %v, want 7 actions and the one sanction", slots)
	}
	// Question 25 joins the two slots of one statement, and it can only do that
	// while the slot is on the edge rather than implied by which act it points
	// at.
	for i := range about[1:] {
		if column(t, about, i+1, "provision_id") == "" {
			t.Errorf("link %v does not say which provision states it", about[i+1])
		}
	}
}

func TestNoActEdgeNamesANodeTheExportDidNotWrite(t *testing.T) {
	dir := t.TempDir()
	if err := Export(dir, competencyFixture()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	ids := func(file string) map[string]bool {
		out := map[string]bool{}
		for _, r := range readCSV(t, filepath.Join(dir, file))[1:] {
			out[r[0]] = true
		}
		return out
	}
	acts, norms, concepts := ids("acts.csv"), ids("norms.csv"), ids("merged_concepts.csv")
	// neo4j-admin refuses an entire import over one relationship row naming a
	// node it does not have, so this is the check that decides whether a dump
	// loads at all.
	for _, r := range readCSV(t, filepath.Join(dir, "act_chains.csv"))[1:] {
		if !acts[r[0]] || !acts[r[1]] {
			t.Errorf("chain %v names an act acts.csv does not hold", r)
		}
	}
	for _, r := range readCSV(t, filepath.Join(dir, "act_participants.csv"))[1:] {
		if !acts[r[0]] || !concepts[r[1]] {
			t.Errorf("participant %v names a node no file holds", r)
		}
	}
	for _, r := range readCSV(t, filepath.Join(dir, "about_act.csv"))[1:] {
		if !norms[r[0]] || !acts[r[1]] {
			t.Errorf("link %v names a node no file holds", r)
		}
	}
}

func TestAChainIntoAnActTheFoldDidNotKeepIsDropped(t *testing.T) {
	in := competencyFixture()
	revoked := event.ID(event.Revoke, "thu hồi giấy phép")
	if !slices.ContainsFunc(in.Chains, func(c event.Chain) bool { return c.ToID == revoked }) {
		t.Fatal("the fixture no longer holds a dangling chain, so this test proves nothing")
	}
	var got []chainEdge
	if err := eachActChain(in, func(c chainEdge) error {
		got = append(got, c)
		return nil
	}); err != nil {
		t.Fatalf("eachActChain: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("chains: got %d, want the 4 whose two ends the fold kept", len(got))
	}
	for _, c := range got {
		if c.To == revoked {
			t.Error("the chain into the act the fold dropped was written anyway, which stops the whole import")
		}
	}
}

func TestALinkFromAStatementTheProjectionDidNotWriteIsDropped(t *testing.T) {
	in := competencyFixture()
	var got []normActEdge
	if err := eachNormAct(in, func(l normActEdge) error {
		got = append(got, l)
		return nil
	}); err != nil {
		t.Fatalf("eachNormAct: %v", err)
	}
	if len(got) != 8 {
		t.Fatalf("links: got %d, want the 8 whose statement was written", len(got))
	}
	for _, l := range got {
		if l.NormID == "vn:norm:fixture-gone" {
			t.Error("a link from a statement no norms.csv row holds was written")
		}
	}
	// The order is fixed in the walker rather than taken from the store, because
	// the links arrive from as many sighting files as there are documents and two
	// machines reading a directory are not promised the same order.
	if !slices.IsSortedFunc(got, func(a, b normActEdge) int {
		if a.NormID != b.NormID {
			return strings.Compare(a.NormID, b.NormID)
		}
		return strings.Compare(a.Slot, b.Slot)
	}) {
		t.Errorf("the links came out in an order the export does not fix: %v", got)
	}
}

func TestAParticipantWhoseConceptTheLayerNoLongerHoldsIsDropped(t *testing.T) {
	in := competencyFixture()
	before := Summarize(in).ActParticipants
	for i := range in.Layer.Concepts {
		if in.Layer.Concepts[i].ID == fxBHXH {
			in.Layer.Concepts = slices.Delete(in.Layer.Concepts, i, i+1)
			break
		}
	}
	// The layer moving under the act layer is a thing that happens between
	// milestones. It costs the one edge whose concept went, and not the import.
	after := Summarize(in)
	if after.ActParticipants != before-1 {
		t.Errorf("participants: got %d, want %d now that one concept is gone", after.ActParticipants, before-1)
	}
	if after.Acts != Summarize(competencyFixture()).Acts {
		t.Error("dropping a concept took an act with it")
	}
}

func TestTheActNodeSaysTheSameThingOnBothLoadingPaths(t *testing.T) {
	var acts []actNode
	if err := eachAct(competencyFixture(), func(a actNode) error {
		acts = append(acts, a)
		return nil
	}); err != nil {
		t.Fatalf("eachAct: %v", err)
	}
	if len(acts) == 0 {
		t.Fatal("the fixture holds no acts")
	}
	for _, a := range acts {
		fields := a.fields()
		if fields["id"] != a.ID {
			t.Errorf("the Bolt form of %s carries no identifier", a.ID)
		}
		// The CSV path packs a list into text because that file format has no
		// other way to say it. Bolt has, and a property that arrived as a joined
		// string would make aliases unqueryable in the database while the CSV
		// import made it a list.
		if _, ok := fields["aliases"].([]any); !ok {
			t.Errorf("aliases on %s is %T, not a list", a.ID, fields["aliases"])
		}
		if _, ok := fields["support"].(int); !ok {
			t.Errorf("support on %s is %T, not a number", a.ID, fields["support"])
		}
		for i, name := range actColumns {
			switch name {
			case "aliases", "docs", "support", "support_docs", "confidence", "ontology_version":
				continue
			}
			if fields[name] != a.values()[i] {
				t.Errorf("%s on %s: CSV says %q and Bolt says %v", name, a.ID, a.values()[i], fields[name])
			}
		}
		if len(fields) != len(actColumns)+1 {
			t.Errorf("the Bolt form of %s carries %d properties and the CSV header has %d columns",
				a.ID, len(fields), len(actColumns)+1)
		}
	}
}

func TestImportScriptLoadsTheActFiles(t *testing.T) {
	dir := t.TempDir()
	if err := Export(dir, competencyFixture()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	// A file written and never loaded is a projection nobody can query, and the
	// fleet runs this on Windows as well as on Linux.
	for _, name := range []string{"import.sh", "import.cmd"} {
		text := readFile(t, filepath.Join(dir, name))
		for _, want := range []string{"acts.csv", "act_chains.csv", "act_participants.csv", "about_act.csv"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not load %s", name, want)
			}
		}
	}
}

func TestSummarizeCountsTheChainsThatRestOnOneProvisionSeparately(t *testing.T) {
	s := Summarize(competencyFixture())
	if s.Acts != 6 || s.ActChains != 4 {
		t.Errorf("acts %d and chains %d, want 6 and 4", s.Acts, s.ActChains)
	}
	// Three of the four chains are one sentence each. The proportion is printed
	// beside the total rather than subtracted from it, because this projection
	// ships those chains and a reader is owed the number before they walk one.
	if s.ChainsOnOneProvision != 3 {
		t.Errorf("chains on one provision: got %d, want 3", s.ChainsOnOneProvision)
	}
	if s.ActParticipants != 8 || s.NormActs != 8 {
		t.Errorf("participants %d and norm links %d, want 8 and 8", s.ActParticipants, s.NormActs)
	}
	if !strings.Contains(s.String(), "chains 4 of which 3 rest on one provision") {
		t.Errorf("the summary hides how thin the consequence graph is:\n%s", s)
	}
}

func TestSummarizeSaysNothingAboutActsWhenThereAreNone(t *testing.T) {
	in := competencyFixture()
	in.Acts, in.Chains, in.NormActs = nil, nil, nil
	if got := Summarize(in).String(); strings.Contains(got, "acts") {
		t.Errorf("a store with no act layer advertises one:\n%s", got)
	}
}

func TestRestrictCutsTheActLayerToWhatTheCampaignStillStates(t *testing.T) {
	got := Restrict(competencyFixture(), labourOnly())
	kept := map[string]bool{}
	for _, a := range got.Acts {
		kept[a.ID] = true
	}
	// The inspection is stated once, in the insurance law, and no statement left
	// in the campaign names it. It goes, and the one canonical chain in the
	// fixture goes with it, because a chain with one end outside the dump is a
	// row naming a node that does not exist.
	if kept[fxKiemTra] {
		t.Error("an act only the excluded instrument states survived the cut")
	}
	if len(got.Acts) != 5 {
		t.Errorf("acts: got %d, want the 5 the campaign still states", len(got.Acts))
	}
	for _, c := range got.Chains {
		if !kept[c.FromID] || !kept[c.ToID] {
			t.Errorf("chain %s to %s hangs off an act that is not in the dump", c.FromID, c.ToID)
		}
	}
	if len(got.Chains) != 3 {
		t.Errorf("chains: got %d, want 3 now that the inspection is gone", len(got.Chains))
	}

	// What the cut does not do is rewrite the act. The support counts and the
	// document list are statements about the whole corpus, and recomputing them
	// inside a campaign would report an act two instruments corroborate as one
	// instrument's provisional guess.
	for _, a := range got.Acts {
		if a.ID != fxDongBH {
			continue
		}
		if a.SupportDocs != 2 || a.Status != event.StatusCanonical {
			t.Errorf("the insurance contribution came out of the cut as %s on %d documents, want canonical on 2",
				a.Status, a.SupportDocs)
		}
	}
	for _, l := range got.NormActs {
		if l.DocID == fxSocial {
			t.Errorf("link %v belongs to a document outside the campaign", l)
		}
	}
	if len(competencyFixture().Acts) != 6 {
		t.Error("Restrict changed the projection it was given")
	}
}
