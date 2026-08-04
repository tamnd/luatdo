package event

import (
	"strings"
	"testing"
)

func annotated() Gold {
	return Gold{
		UnitID: "p1", DocID: "d1", TextHash: "h1", Text: provision,
		Events: []GoldEvent{
			{Class: "SUBMIT", LabelVI: "nộp hồ sơ", Participants: []GoldParticipant{
				{Role: RoleAgent, LabelVI: "người đề nghị"},
				{Role: RoleRecipient, LabelVI: "cơ quan đăng ký kinh doanh"},
			}},
			{Class: "ISSUE", LabelVI: "cấp giấy chứng nhận"},
		},
		Chains:         []GoldChain{{FromLabel: "nộp hồ sơ", ToLabel: "cấp giấy chứng nhận", Type: Precedes}},
		RolesAnnotated: true,
		AnnotatedBy:    "tamnd", AnnotatedAt: "2026-08-03",
	}
}

func TestCheckGoldIsQuietAboutAGoodAnnotation(t *testing.T) {
	if problems := CheckGold([]Gold{annotated()}); len(problems) != 0 {
		t.Errorf("a good annotation was reported as broken: %v", problems)
	}
}

func TestCheckGoldCatchesWhatWouldScoreTheRulerAsTheFault(t *testing.T) {
	bad := annotated()
	bad.Events[0].Participants[0].Role = "BENEFICIARY"
	bad.Events[1].Class = "HANH_VI"
	bad.Chains[0].Type = "CAUSES"
	bad.TextHash = ""
	problems := CheckGold([]Gold{bad})
	if len(problems) < 4 {
		t.Fatalf("problems: got %v, want the role, the generic class, the chain type and the missing hash", problems)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"BENEFICIARY", "HANH_VI", "CAUSES", "text hash"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the report does not mention %q: %v", want, problems)
		}
	}
}

func TestCheckGoldCatchesSilenceAndDoubleAnnotation(t *testing.T) {
	silent := annotated()
	silent.Events = nil
	silent.Chains = nil
	problems := CheckGold([]Gold{silent, silent})
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "annotated twice") {
		t.Errorf("one provision annotated twice went unreported: %v", problems)
	}
	if !strings.Contains(joined, "neither an annotation nor a refusal") {
		t.Errorf("an empty annotation that does not say it found nothing went unreported: %v", problems)
	}
}

func TestCheckGoldCatchesAChainToAnActItDoesNotAnnotate(t *testing.T) {
	bad := annotated()
	bad.Chains[0].ToLabel = "thu hồi giấy phép"
	problems := CheckGold([]Gold{bad})
	if len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), "does not annotate") {
		t.Errorf("problems: %v", problems)
	}
}

// found builds what a pass produced for the test provision.
func found(class, label string, parts ...Participant) Occurrence {
	o := sighting("p1", "d1", class, label, parts...)
	return o
}

func hashes() map[string]string { return map[string]string{"p1": "h1"} }

func TestScoreCountsTheActsAndTheirClassesApart(t *testing.T) {
	got := Score([]Gold{annotated()}, []Occurrence{
		found("SUBMIT", "nộp hồ sơ",
			Participant{Role: RoleAgent, LabelVI: "người đề nghị"},
			Participant{Role: RoleRecipient, LabelVI: "cơ quan đăng ký kinh doanh"}),
		// The right act with the wrong class. It is one mistake, so it counts as
		// a found act with a class error and not as a miss and an invention.
		found("PUBLISH", "cấp giấy chứng nhận"),
	}, nil, hashes())

	if got.Scored != 1 {
		t.Fatalf("scored: got %d, want 1", got.Scored)
	}
	if got.Events.TP != 2 || got.Events.FP != 0 || got.Events.FN != 0 {
		t.Errorf("events: %+v, want both found", got.Events)
	}
	if got.Classes.Of != 2 || got.Classes.Right != 1 {
		t.Errorf("classes: got %d of %d, want 1 of 2", got.Classes.Right, got.Classes.Of)
	}
	if got.ClassConfusion["ISSUE"]["PUBLISH"] != 1 {
		t.Errorf("the class swap is not in the confusion table: %+v", got.ClassConfusion)
	}
	if got.Roles.Of != 2 || got.Roles.Right != 2 {
		t.Errorf("roles: got %d of %d, want 2 of 2", got.Roles.Right, got.Roles.Of)
	}
}

func TestScoreTellsAWrongSlotFromAMissingParty(t *testing.T) {
	got := Score([]Gold{annotated()}, []Occurrence{
		found("SUBMIT", "nộp hồ sơ",
			// The right party in the wrong slot, and the other one not found.
			Participant{Role: RoleObject, LabelVI: "người đề nghị"}),
		found("ISSUE", "cấp giấy chứng nhận"),
	}, nil, hashes())
	if got.Roles.Of != 1 || got.Roles.Right != 0 {
		t.Errorf("roles: got %d of %d, want 0 of 1", got.Roles.Right, got.Roles.Of)
	}
	if got.RolesMissed != 1 {
		t.Errorf("missed parties: got %d, want 1, counted apart because it is a different failure with a different fix", got.RolesMissed)
	}
}

func TestScoreCountsAnInventedPartyOnlyWhereTheRolesWereAnnotatedInFull(t *testing.T) {
	extra := Participant{Role: RoleInstrument, LabelVI: "hồ sơ"}
	pass := []Occurrence{
		found("SUBMIT", "nộp hồ sơ",
			Participant{Role: RoleAgent, LabelVI: "người đề nghị"},
			Participant{Role: RoleRecipient, LabelVI: "cơ quan đăng ký kinh doanh"},
			extra),
		found("ISSUE", "cấp giấy chứng nhận"),
	}
	full := Score([]Gold{annotated()}, pass, nil, hashes())
	if full.RolesInvented != 1 {
		t.Errorf("invented parties: got %d, want 1", full.RolesInvented)
	}

	partial := annotated()
	partial.RolesAnnotated = false
	if got := Score([]Gold{partial}, pass, nil, hashes()); got.RolesInvented != 0 {
		t.Errorf("invented parties over a half annotation: got %d, want 0, because the annotator never said the list was complete", got.RolesInvented)
	}
}

func TestScoreSeparatesFindingAChainFromPointingItTheRightWay(t *testing.T) {
	occurrences := []Occurrence{found("SUBMIT", "nộp hồ sơ"), found("ISSUE", "cấp giấy chứng nhận")}
	backwards := Chain{
		FromID: ID("ISSUE", "cấp giấy chứng nhận"), ToID: ID("SUBMIT", "nộp hồ sơ"), Type: Precedes,
		Evidence: []Evidence{{ProvisionID: "p1"}},
	}
	got := Score([]Gold{annotated()}, occurrences, []Chain{backwards}, hashes())
	if got.Chains.TP != 1 || got.Chains.FP != 0 || got.Chains.FN != 0 {
		t.Errorf("chains: %+v, want the connection found", got.Chains)
	}
	if got.ChainTypes.Right != 1 {
		t.Errorf("chain types: got %d of %d, want the type counted right", got.ChainTypes.Right, got.ChainTypes.Of)
	}
	if got.ChainDirection.Of != 1 || got.ChainDirection.Right != 0 {
		t.Errorf("chain arrows: got %d of %d, want 0 of 1, because this chain is pointed backwards",
			got.ChainDirection.Right, got.ChainDirection.Of)
	}
}

func TestScoreCountsAChainNobodyAnnotatedAsAnInvention(t *testing.T) {
	g := annotated()
	g.Chains = nil
	occurrences := []Occurrence{found("SUBMIT", "nộp hồ sơ"), found("ISSUE", "cấp giấy chứng nhận")}
	c := Chain{FromID: occurrences[0].EventID, ToID: occurrences[1].EventID, Type: Triggers, Evidence: []Evidence{{ProvisionID: "p1"}}}
	got := Score([]Gold{g}, occurrences, []Chain{c}, hashes())
	if got.Chains.FP != 1 {
		t.Errorf("chains: %+v, want one invention", got.Chains)
	}
}

func TestScoreCountsAProvisionThatNamesNoAct(t *testing.T) {
	quiet := annotated()
	quiet.Events = nil
	quiet.Chains = nil
	quiet.NamesNoAct = true

	right := Score([]Gold{quiet}, nil, nil, hashes())
	if right.NamesNoAct.TP != 1 {
		t.Errorf("names no act: %+v, want the refusal counted right", right.NamesNoAct)
	}
	wrong := Score([]Gold{quiet}, []Occurrence{found("SUBMIT", "nộp hồ sơ")}, nil, hashes())
	if wrong.NamesNoAct.FP != 1 || wrong.Events.FP != 1 {
		t.Errorf("an act invented in a definition clause: %+v %+v", wrong.NamesNoAct, wrong.Events)
	}
}

func TestScoreReportsWhatItDidNotScore(t *testing.T) {
	missing := annotated()
	missing.UnitID = "p9"
	stale := annotated()
	stale.UnitID = "p2"
	stale.TextHash = "old"

	got := Score([]Gold{annotated(), missing, stale},
		[]Occurrence{found("SUBMIT", "nộp hồ sơ"), found("ISSUE", "cấp giấy chứng nhận")},
		nil, map[string]string{"p1": "h1", "p2": "h2"})

	if got.Units != 3 || got.Scored != 1 {
		t.Errorf("units: got %d annotated and %d scored, want 3 and 1", got.Units, got.Scored)
	}
	if len(got.Missing) != 1 || got.Missing[0] != "p9" {
		t.Errorf("missing: got %v, want the provision the pass never reached", got.Missing)
	}
	if len(got.Stale) != 1 || got.Stale[0] != "p2" {
		t.Errorf("stale: got %v, want the provision whose text changed under the annotation", got.Stale)
	}
	report := got.String()
	if !strings.Contains(report, "not covered by the pass") || !strings.Contains(report, "text that has changed") {
		t.Errorf("the report reads as a figure over the whole gold set: %s", report)
	}
}

func TestGoldPathKeepsCampaignsApart(t *testing.T) {
	if GoldPath("/tmp", "") == GoldPath("/tmp", "lao-dong") {
		t.Error("a labour gold set and the corpus wide one share a file, and precision would then be reported over a mixture")
	}
	if !strings.HasSuffix(GoldPath("/tmp", ""), GoldFile) {
		t.Errorf("the unqualified path moved: %s", GoldPath("/tmp", ""))
	}
}

func TestGoldRoundTripsThroughTheStore(t *testing.T) {
	dir := t.TempDir()
	if err := WriteGold(dir, "lao-dong", []Gold{annotated()}); err != nil {
		t.Fatalf("WriteGold: %v", err)
	}
	got, err := ReadGold(dir, "lao-dong")
	if err != nil {
		t.Fatalf("ReadGold: %v", err)
	}
	if len(got) != 1 || len(got[0].Events) != 2 || len(got[0].Chains) != 1 {
		t.Fatalf("annotation did not survive: %+v", got)
	}
	if got[0].Events[0].Participants[0].Role != RoleAgent {
		t.Errorf("roles did not survive: %+v", got[0].Events[0])
	}
}
