package graph

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/conflict"
	"github.com/tamnd/luatdo/norm"
)

// twoNorms is a projection holding two statements and one finding about them,
// built by hand rather than by the detector so the test can bend one field at a
// time.
func twoNorms() Input {
	a := &conflict.Form{
		StatementID: "vn:norm:b", DocID: fxCode, ProvisionID: fxCode + ":article-94:clause-1",
		Operator: conflict.Prohibition, Party: "nguoi-su-dung-lao-dong", Act: "tra-luong",
		Canon: conflict.Canon{Party: "người sử dụng lao động", Act: "trả lương"},
	}
	b := &conflict.Form{
		StatementID: "vn:norm:a", DocID: fxDecree, ProvisionID: fxDecree + ":article-3",
		Operator: conflict.Obligation, Party: "nguoi-su-dung-lao-dong", Act: "tra-luong",
		Canon: conflict.Canon{Party: "người sử dụng lao động", Act: "trả lương"},
	}
	return Input{
		Statements: []norm.Record{
			{ID: "vn:norm:a", DocID: fxDecree, ProvisionID: b.ProvisionID},
			{ID: "vn:norm:b", DocID: fxCode, ProvisionID: a.ProvisionID},
		},
		Conflicts: []conflict.Finding{{
			Rule: conflict.RuleDuty, A: a, B: b,
			Matched: []conflict.Slot{
				{Name: "party", A: "người sử dụng lao động", B: "người sử dụng lao động"},
				{Name: "act", A: "trả lương", B: "trả lương"},
			},
			Clashing:      []conflict.Slot{{Name: "operator", A: conflict.Prohibition, B: conflict.Obligation}},
			Circumstances: conflict.CircumstancesShared,
			Rank:          conflict.Rank{Superior: "vn:norm:b", Agree: true},
			Explanation:   "Một bên buộc trả lương, một bên cấm.",
		}},
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func nodes(t *testing.T, in Input) []conflictNode {
	t.Helper()
	var out []conflictNode
	if err := eachConflict(in, func(c conflictNode) error {
		out = append(out, c)
		return nil
	}); err != nil {
		t.Fatalf("eachConflict: %v", err)
	}
	return out
}

func TestConflictNodeFlattensTheArgumentIntoQueryableFields(t *testing.T) {
	got := nodes(t, twoNorms())
	if len(got) != 1 {
		t.Fatalf("projected %d nodes, want 1", len(got))
	}
	c := got[0]
	if !strings.HasPrefix(c.ID, "vn:conflict:") {
		t.Errorf("id = %q, want the identifier scheme the rest of the graph uses", c.ID)
	}
	if c.Rule != conflict.RuleDuty || c.Circumstances != conflict.CircumstancesShared {
		t.Errorf("rule %q, circumstances %q", c.Rule, c.Circumstances)
	}
	// The party and the act are the words a drafter wrote, not the slugs the
	// checker compared, because a person reads the node in the browser.
	if c.Party != "người sử dụng lao động" || c.Act != "trả lương" {
		t.Errorf("party %q, act %q", c.Party, c.Act)
	}
	// The matched slots are one readable string and the clashing slot is three
	// fields, because a query filters on the clash and only reads the rest.
	if !strings.Contains(c.Matched, "party: người sử dụng lao động") || !strings.Contains(c.Matched, "act: trả lương") {
		t.Errorf("matched = %q", c.Matched)
	}
	if c.ClashingSlot != "operator" || c.ClashingA != conflict.Prohibition || c.ClashingB != conflict.Obligation {
		t.Errorf("clash = %q %q %q", c.ClashingSlot, c.ClashingA, c.ClashingB)
	}
	// The ranking is written out as the sentence Describe produces, which names
	// provisions. A node carrying statement identifiers would make the reader
	// look them up to learn which side the rule pointed at.
	if !strings.Contains(c.Rank, fxCode+":article-94:clause-1") {
		t.Errorf("rank = %q, want the provision named", c.Rank)
	}
	if c.NormA != "vn:norm:b" || c.NormB != "vn:norm:a" {
		t.Errorf("norms = %q, %q, want the sides the finding holds", c.NormA, c.NormB)
	}
	if c.Explanation == "" {
		t.Error("the explanation was dropped, and it is the one field a person reads first")
	}
}

func TestConflictNodeCarriesNoClashingSlotWhenThereIsNone(t *testing.T) {
	in := twoNorms()
	in.Conflicts[0].Clashing = nil
	c := nodes(t, in)[0]
	if c.ClashingSlot != "" || c.ClashingA != "" || c.ClashingB != "" {
		t.Errorf("clash = %q %q %q, want empty rather than invented", c.ClashingSlot, c.ClashingA, c.ClashingB)
	}
}

func TestEachConflictSkipsAFindingWhoseNormsWereNotWritten(t *testing.T) {
	in := twoNorms()
	in.Statements = in.Statements[:1] // vn:norm:a survives, vn:norm:b does not
	if got := nodes(t, in); len(got) != 0 {
		t.Errorf("projected %d nodes over a norm the export did not write", len(got))
	}

	// A finding with a missing side is data corruption rather than a smaller
	// finding, and it must not become a node with one edge.
	half := twoNorms()
	half.Conflicts[0].B = nil
	if got := nodes(t, half); len(got) != 0 {
		t.Errorf("projected %d nodes from a finding with one side", len(got))
	}
}

func TestEachConflictIsOrderedByFindingIdentifier(t *testing.T) {
	in := twoNorms()
	second := in.Conflicts[0]
	second.Rule = conflict.RulePermission
	// Two findings about the same pair, told apart by the rule that fired, and
	// the rule leads the identifier they sort on.
	in.Conflicts = append([]conflict.Finding{second}, in.Conflicts...)

	got := nodes(t, in)
	if len(got) != 2 {
		t.Fatalf("projected %d nodes, want 2", len(got))
	}
	if got[0].Rule != conflict.RuleDuty || got[1].Rule != conflict.RulePermission {
		t.Errorf("order = %q, %q, want the fixed one that makes two exports of one store identical",
			got[0].Rule, got[1].Rule)
	}
	if got[0].ID == got[1].ID {
		t.Error("two findings share a node identifier, and the merge would write one over the other")
	}
}

func TestEachConflictOfAProjectionWithNoDetectorRun(t *testing.T) {
	in := competencyFixture()
	in.Conflicts = nil
	if got := nodes(t, in); len(got) != 0 {
		t.Errorf("projected %d nodes with no findings", len(got))
	}
}

func TestShortIDIsLegibleAndUnique(t *testing.T) {
	got := shortID("obligation-against-prohibition|vn:norm:fixture-nopay|vn:norm:fixture-pay")
	// The node identifier keeps colons as its own separator, so the segments of
	// a statement identifier cannot be mistaken for segments of this one.
	if strings.Contains(got, "|") || strings.Contains(got, "vn:norm:") {
		t.Errorf("shortID = %q", got)
	}
	if !strings.Contains(got, "fixture-nopay") || !strings.Contains(got, "fixture-pay") {
		t.Errorf("shortID = %q, want both statements still readable in it", got)
	}
}

func TestExportWritesTheConflictAndBothOfItsEdges(t *testing.T) {
	dir := t.TempDir()
	if err := Export(dir, twoNorms()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	rows := readCSV(t, filepath.Join(dir, "conflicts.csv"))
	if len(rows) != 2 {
		t.Fatalf("conflicts.csv holds %d rows including the header", len(rows))
	}
	// The header is the column list the Bolt merge sets as properties, so a
	// column added to one and not the other is a graph whose two loaders
	// disagree.
	for _, name := range conflictColumns {
		if !slices.Contains(rows[0], name) {
			t.Errorf("conflicts.csv has no %s column: %v", name, rows[0])
		}
	}
	if rows[0][0] != "id:ID" || rows[0][len(rows[0])-1] != ":LABEL" {
		t.Errorf("conflicts.csv header = %v, want the shape neo4j-admin reads", rows[0])
	}
	if rows[1][len(rows[1])-1] != ConflictLabel {
		t.Errorf("label = %q", rows[1][len(rows[1])-1])
	}

	edges := readCSV(t, filepath.Join(dir, "involves.csv"))
	if len(edges) != 3 {
		t.Fatalf("involves.csv holds %d rows including the header, want an edge to each side", len(edges))
	}
	sides := []string{edges[1][2], edges[2][2]}
	if sides[0] != "a" || sides[1] != "b" {
		t.Errorf("sides = %v, want the side carried as a property rather than as two relationship types", sides)
	}
	for _, e := range edges[1:] {
		if e[0] != rows[1][0] {
			t.Errorf("edge %v starts at a conflict this export did not write", e)
		}
		if e[3] != Involves {
			t.Errorf("edge type = %q", e[3])
		}
	}
	// Every relationship row has to name a node the nodes files hold, or
	// neo4j-admin refuses the whole import.
	norms := map[string]bool{}
	for _, r := range readCSV(t, filepath.Join(dir, "norms.csv"))[1:] {
		norms[r[0]] = true
	}
	for _, e := range edges[1:] {
		if !norms[e[1]] {
			t.Errorf("involves.csv names %s, which norms.csv does not hold", e[1])
		}
	}
}

func TestImportScriptLoadsTheConflictFiles(t *testing.T) {
	dir := t.TempDir()
	if err := Export(dir, twoNorms()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	for _, name := range []string{"import.sh", "import.cmd"} {
		text := readFile(t, filepath.Join(dir, name))
		// A file written and never loaded is a projection nobody can query, and
		// the fleet runs this on Windows as well as on Linux.
		for _, want := range []string{"conflicts.csv", "involves.csv"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not load %s", name, want)
			}
		}
	}
}

func TestSummarizeCountsSharedCircumstancesSeparately(t *testing.T) {
	in := twoNorms()
	unknown := in.Conflicts[0]
	unknown.Rule = conflict.RulePermission
	unknown.Circumstances = conflict.CircumstancesUnknown
	in.Conflicts = append(in.Conflicts, unknown)

	s := Summarize(in)
	if s.Conflicts != 2 || s.Shared != 1 {
		t.Errorf("conflicts %d of which %d shared, want 2 and 1", s.Conflicts, s.Shared)
	}
	// The two are printed together because they are different claims. A pair on
	// unknown circumstances may never be triggered at once, and adding it to the
	// shared ones would make one bigger number out of two smaller true ones.
	if !strings.Contains(s.String(), "conflicts 2 of which 1 on shared circumstances") {
		t.Errorf("the summary hides the split:\n%s", s)
	}
}

func TestSummarizeSaysNothingAboutConflictsWhenThereAreNone(t *testing.T) {
	in := twoNorms()
	in.Conflicts = nil
	if got := Summarize(in).String(); strings.Contains(got, "conflict") {
		t.Errorf("a store with no detector run advertises conflicts:\n%s", got)
	}
}

func TestRestrictDropsAConflictWithOneNormOutsideTheDump(t *testing.T) {
	full := competencyFixture()
	if len(full.Conflicts) == 0 {
		t.Fatal("the fixture detector found nothing, so this test proves nothing")
	}
	// The pay and no-pay pair lives entirely inside the labour code, so the
	// labour campaign keeps it.
	if got := Restrict(full, labourOnly()); len(got.Conflicts) != len(full.Conflicts) {
		t.Errorf("kept %d of %d findings that are wholly inside the campaign", len(got.Conflicts), len(full.Conflicts))
	}
	// Cut the code out and the pair goes with it. Half a pair is a node a reader
	// can only see one side of, which is worse than not shipping it.
	if got := Restrict(full, map[string]bool{fxSocial: true}); len(got.Conflicts) != 0 {
		t.Errorf("kept %d findings whose norms are outside the dump", len(got.Conflicts))
	}
	if len(competencyFixture().Conflicts) != len(full.Conflicts) {
		t.Error("Restrict changed the projection it was given")
	}
}

func TestQuestion19ReadsTheDetectorRatherThanGuessing(t *testing.T) {
	var q Question
	for _, candidate := range Questions {
		if candidate.N == 19 {
			q = candidate
		}
	}
	text := q.Cypher()
	if !strings.Contains(text, ConflictLabel) || !strings.Contains(text, Involves) {
		t.Fatalf("question 19 does not read the projected findings:\n%s", text)
	}
	// The old version compared bearer and action as the drafters wrote them and
	// said in its own comment that the rows were candidates. Deciding a pair
	// needs canonical wording, intersected intervals, condition containment and
	// honoured deferrals, none of which is a thing to write in Cypher.
	if strings.Contains(text, "$incompatible") {
		t.Error("question 19 still decides modality compatibility in Cypher")
	}
	// Circumstances is returned because a row means different things with each
	// value, and a reader who cannot see it cannot tell the two apart.
	if !strings.Contains(text, "circumstances") {
		t.Errorf("question 19 does not return the circumstances:\n%s", text)
	}
	for _, name := range []string{"rule", "limit"} {
		if _, ok := q.Params[name]; !ok {
			t.Errorf("question 19 names $%s and the query set does not bind it", name)
		}
	}
}
