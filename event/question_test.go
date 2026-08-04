package event

import (
	"strings"
	"testing"
)

// A small procedure, written the way the corpus writes one: an application is
// filed in the law, appraised and granted under the guiding decree, and a
// separate decree fines whoever files late.
func procedure() *Graph {
	nop := ID("SUBMIT", "nộp hồ sơ")
	tham := ID("REVIEW", "thẩm định hồ sơ")
	cap := ID("ISSUE", "cấp giấy phép")
	thu := ID("REVOKE", "thu hồi giấy phép")
	phat := ID("SANCTION", "phạt tiền")

	events := []Event{
		{ID: nop, Class: "SUBMIT", LabelVI: "nộp hồ sơ", Status: StatusCanonical, SupportCount: 4, SupportDocs: 2,
			Evidence: []Evidence{{ProvisionID: "d1:art:5", DocID: "d1"}, {ProvisionID: "d2:art:9", DocID: "d2"}}},
		{ID: tham, Class: "REVIEW", LabelVI: "thẩm định hồ sơ", Status: StatusCanonical, SupportCount: 2, SupportDocs: 1,
			Evidence: []Evidence{{ProvisionID: "d2:art:10", DocID: "d2"}}},
		{ID: cap, Class: "ISSUE", LabelVI: "cấp giấy phép", Status: StatusCanonical, SupportCount: 3, SupportDocs: 2,
			Evidence: []Evidence{{ProvisionID: "d1:art:6", DocID: "d1"}, {ProvisionID: "d2:art:11", DocID: "d2"}}},
		{ID: thu, Class: "REVOKE", LabelVI: "thu hồi giấy phép", Status: StatusProvisional, Why: WhySingleSupport,
			SupportCount: 1, SupportDocs: 1, Evidence: []Evidence{{ProvisionID: "d2:art:20", DocID: "d2"}}},
		{ID: phat, Class: "SANCTION", LabelVI: "phạt tiền", Status: StatusCanonical, SupportCount: 2, SupportDocs: 1,
			Evidence: []Evidence{{ProvisionID: "d3:art:4", DocID: "d3"}}},
	}
	chains := []Chain{
		{FromID: nop, ToID: tham, Type: Precedes, Status: StatusCanonical, Direction: DirectionAgreed,
			SupportCount: 2, SupportDocs: 2, Evidence: []Evidence{{ProvisionID: "d2:art:10", DocID: "d2"}}},
		{FromID: tham, ToID: cap, Type: Triggers, Status: StatusCanonical, Direction: DirectionAgreed,
			SupportCount: 2, SupportDocs: 2, Evidence: []Evidence{{ProvisionID: "d2:art:11", DocID: "d2"}}},
		{FromID: cap, ToID: thu, Type: PreconditionOf, Status: StatusProvisional, Direction: DirectionFlipped,
			SupportCount: 1, SupportDocs: 1, Evidence: []Evidence{{ProvisionID: "d2:art:20", DocID: "d2"}}},
	}
	links := []Link{
		{StatementID: "s1", ProvisionID: "d3:art:4", DocID: "d3", EventID: nop, Kind: LinkAction},
		{StatementID: "s1", ProvisionID: "d3:art:4", DocID: "d3", EventID: phat, Kind: LinkSanction},
		{StatementID: "s2", ProvisionID: "d1:art:5", DocID: "d1", EventID: nop, Kind: LinkAction},
	}
	return NewGraph(events, chains, links)
}

func TestQuestion24WalksForwardThroughTheProcedure(t *testing.T) {
	g := procedure()
	got := g.AskQuestion24(ID("SUBMIT", "nộp hồ sơ"), 3)
	if len(got.Follows) != 3 {
		t.Fatalf("follows: got %d steps, want the three the procedure runs through: %+v", len(got.Follows), got.Follows)
	}
	if got.Follows[0].ToLabel != "thẩm định hồ sơ" || got.Follows[0].Depth != 1 {
		t.Errorf("first step: %+v", got.Follows[0])
	}
	if got.Follows[2].ToLabel != "thu hồi giấy phép" || got.Follows[2].Depth != 3 {
		t.Errorf("third step: %+v", got.Follows[2])
	}
	if len(got.Precedes) != 0 {
		t.Errorf("something came before the first act of the procedure: %+v", got.Precedes)
	}
}

func TestQuestion24StopsAtTheDepthItWasAsked(t *testing.T) {
	got := procedure().AskQuestion24(ID("SUBMIT", "nộp hồ sơ"), 1)
	if len(got.Follows) != 1 {
		t.Errorf("follows: got %d, want 1, because a walk that quietly runs deeper than asked returns a report nobody sized", len(got.Follows))
	}
}

func TestQuestion24WalksBackToWhatHasToHappenFirst(t *testing.T) {
	got := procedure().AskQuestion24(ID("ISSUE", "cấp giấy phép"), 3)
	if len(got.Precedes) != 2 {
		t.Fatalf("precedes: got %d, want the appraisal and the filing behind it: %+v", len(got.Precedes), got.Precedes)
	}
	if got.Precedes[0].FromLabel != "thẩm định hồ sơ" || got.Precedes[1].FromLabel != "nộp hồ sơ" {
		t.Errorf("the order of the prerequisites is wrong: %+v", got.Precedes)
	}
}

func TestQuestion24SaysWhenAStepOnTheAnswerWasReadBackwards(t *testing.T) {
	got := procedure().AskQuestion24(ID("SUBMIT", "nộp hồ sơ"), 3)
	if got.Backwards != 1 {
		t.Fatalf("backwards steps: got %d, want 1", got.Backwards)
	}
	report := got.String()
	if !strings.Contains(report, "the other way round") {
		t.Errorf("a chain the blind pass read backwards is printed like the rest: %s", report)
	}
	if !strings.Contains(report, "provisional") {
		t.Errorf("the report hides that a step rests on one provision: %s", report)
	}
}

func TestQuestion24SurvivesACycle(t *testing.T) {
	// Vietnamese procedures loop: a revoked licence is applied for again. A walk
	// that follows the loop forever is a hang in the middle of an answer.
	a, b := ID("SUBMIT", "a"), ID("ISSUE", "b")
	g := NewGraph(
		[]Event{{ID: a, LabelVI: "a"}, {ID: b, LabelVI: "b"}},
		[]Chain{
			{FromID: a, ToID: b, Type: Precedes, Evidence: []Evidence{{ProvisionID: "p1"}}},
			{FromID: b, ToID: a, Type: Precedes, Evidence: []Evidence{{ProvisionID: "p2"}}},
		}, nil)
	got := g.AskQuestion24(a, 5)
	if len(got.Follows) != 2 {
		t.Errorf("follows: got %d, want each edge once: %+v", len(got.Follows), got.Follows)
	}
}

func TestQuestion24SaysPlainlyWhenTheLayerFoundNothing(t *testing.T) {
	got := procedure().AskQuestion24("vn:event:submit:khong-co", 3)
	if len(got.Follows) != 0 || len(got.Precedes) != 0 {
		t.Fatalf("an act nobody extracted came back with a procedure around it: %+v", got)
	}
	if !strings.Contains(got.String(), "no provision joined this act to another") {
		t.Errorf("an empty answer reads as an act with no consequences: %s", got.String())
	}
}

func TestQuestion25ReachesThePenaltyThroughTheNorm(t *testing.T) {
	got := procedure().AskQuestion25()
	if len(got.Rows) != 1 {
		t.Fatalf("rows: got %d, want the one norm that carries both slots: %+v", len(got.Rows), got.Rows)
	}
	r := got.Rows[0]
	if r.ActLabel != "nộp hồ sơ" || r.Sanction != "phạt tiền" {
		t.Errorf("row: %+v", r)
	}
	if r.ProvisionID != "d3:art:4" {
		t.Errorf("the row does not carry the provision it came from: %+v", r)
	}
	if got.Unlinked != 1 {
		t.Errorf("norms with no penalty: got %d, want 1", got.Unlinked)
	}
	if !strings.Contains(got.String(), "reached an act and no penalty") {
		t.Errorf("the report prints the join rate as coverage: %s", got.String())
	}
}

func TestQuestion25NeverJoinsTwoDocumentsThroughOneLabel(t *testing.T) {
	// The same act is fined in one decree and merely required in another. The
	// join is the statement, so the duty in d1 does not pick up the fine in d3.
	act := ID("SUBMIT", "nộp hồ sơ")
	g := NewGraph(
		[]Event{{ID: act, LabelVI: "nộp hồ sơ"}, {ID: ID("SANCTION", "phạt tiền"), LabelVI: "phạt tiền"}},
		nil,
		[]Link{
			{StatementID: "s1", ProvisionID: "d1:art:5", DocID: "d1", EventID: act, Kind: LinkAction},
			{StatementID: "s2", ProvisionID: "d3:art:4", DocID: "d3", EventID: act, Kind: LinkAction},
			{StatementID: "s2", ProvisionID: "d3:art:4", DocID: "d3", EventID: ID("SANCTION", "phạt tiền"), Kind: LinkSanction},
		})
	got := g.AskQuestion25()
	if len(got.Rows) != 1 || got.Rows[0].StatementID != "s2" {
		t.Fatalf("rows: %+v, want only the norm that names both", got.Rows)
	}
}

func TestQuestion26ShowsTheActsTheCorpusShares(t *testing.T) {
	got := procedure().AskQuestion26(2)
	if len(got.Rows) != 2 {
		t.Fatalf("rows: got %d, want the two acts named in two instruments: %+v", len(got.Rows), got.Rows)
	}
	// Both are named in two instruments, so the tie is broken by how much of the
	// procedure runs through them.
	if got.Rows[0].Label != "cấp giấy phép" || got.Rows[0].Chains != 2 {
		t.Errorf("rows are not sorted by how widely the act is shared and how much runs through it: %+v", got.Rows)
	}
	if got.Rows[1].Label != "nộp hồ sơ" {
		t.Errorf("second row: %+v", got.Rows[1])
	}
	if !strings.Contains(got.String(), "merges two acts into one node") {
		t.Errorf("the report presents a merge as a finding with no way to be wrong: %s", got.String())
	}
}

func TestQuestion26SaysWhenNothingWasSharedAtAll(t *testing.T) {
	g := NewGraph([]Event{{ID: "a", LabelVI: "a", Evidence: []Evidence{{ProvisionID: "p1", DocID: "d1"}}}}, nil, nil)
	got := g.AskQuestion26(2)
	if len(got.Rows) != 0 {
		t.Fatalf("rows: %+v", got.Rows)
	}
	if !strings.Contains(got.String(), "bought nothing here") {
		t.Errorf("an empty cross document answer is printed as a clean result: %s", got.String())
	}
}

func TestGraphNamesAnActItDoesNotHold(t *testing.T) {
	// A chain can outlive its endpoint when a layer is read half written, and an
	// answer full of raw identifiers is better than one that pretends.
	g := procedure()
	if got := g.Label("vn:event:submit:khong-co"); got != "vn:event:submit:khong-co" {
		t.Errorf("label: got %q", got)
	}
}
