package event

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/concept"
)

// answers is a Completer that reads from a script, so an extraction test is
// about the extraction rather than about a network.
type answers struct {
	replies []string
	err     error
	inputs  []string
	calls   int
}

func (a *answers) Complete(_ context.Context, req api.Request) (api.Response, error) {
	a.inputs = append(a.inputs, req.Input)
	a.calls++
	if a.err != nil {
		return api.Response{}, a.err
	}
	i := min(a.calls-1, len(a.replies)-1)
	return api.Response{
		Text:  a.replies[i],
		Usage: api.Usage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110},
	}, nil
}

const provision = "Người đề nghị nộp hồ sơ đến cơ quan đăng ký kinh doanh. Trong thời hạn 03 ngày làm việc kể từ ngày nhận hồ sơ, cơ quan đăng ký kinh doanh cấp giấy chứng nhận đăng ký doanh nghiệp."

func concepts() []Candidate {
	return []Candidate{
		{ID: "c1", LabelVI: "người đề nghị", Kind: concept.KindActor},
		{ID: "c2", LabelVI: "hồ sơ", Kind: concept.KindArtifact},
		{ID: "c3", LabelVI: "cơ quan đăng ký kinh doanh", Kind: concept.KindBody},
		{ID: "c4", LabelVI: "giấy chứng nhận đăng ký doanh nghiệp", Kind: concept.KindArtifact},
	}
}

// at returns a quote with its real byte offsets, so no test hand counts bytes in
// a Vietnamese sentence and gets them wrong.
func at(t *testing.T, text, quote string) (int, int) {
	t.Helper()
	i := strings.Index(text, quote)
	if i < 0 {
		t.Fatalf("the test quote is not in the test provision: %q", quote)
	}
	return i, i + len(quote)
}

func reply(t *testing.T, quote string, body string) string {
	t.Helper()
	start, end := at(t, provision, quote)
	return strings.NewReplacer(
		"$start", strconv.Itoa(start),
		"$end", strconv.Itoa(end),
		"$quote", quote,
	).Replace(body)
}

const twoActs = `{"events":[
{"class":"SUBMIT","label_vi":"nộp hồ sơ","as_written":"nộp hồ sơ",
 "participants":[{"role":"AGENT","concept_id":"c1","as_written":"người đề nghị"},
                 {"role":"INSTRUMENT","concept_id":"c2","as_written":"hồ sơ"},
                 {"role":"RECIPIENT","concept_id":"c3","as_written":"cơ quan đăng ký kinh doanh"}],
 "quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.95},
{"class":"ISSUE","label_vi":"cấp giấy chứng nhận đăng ký doanh nghiệp","as_written":"cấp giấy chứng nhận",
 "participants":[{"role":"AGENT","concept_id":"c3","as_written":"cơ quan đăng ký kinh doanh"}],
 "quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.9}],
"chains":[{"from_event":"nộp hồ sơ","to_event":"cấp giấy chứng nhận đăng ký doanh nghiệp","type":"PRECEDES",
 "quote":"$quote","char_start":$start,"char_end":$end,
 "direction_check":"nộp hồ sơ xảy ra trước, cấp giấy chứng nhận xảy ra sau","confidence":0.9}]}`

func extractor(model api.Completer) *Extractor {
	return &Extractor{Completer: model, Model: "test", Registry: SeedRegistry(1), MaxCorrections: 2}
}

func TestExtractReadsTheActsAndTheChainBetweenThem(t *testing.T) {
	model := &answers{replies: []string{reply(t, "nộp hồ sơ đến cơ quan đăng ký kinh doanh", twoActs)}}
	got, usage, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(got.Occurrences) != 2 {
		t.Fatalf("acts: got %d, want 2: %+v", len(got.Occurrences), got.Occurrences)
	}
	first := got.Occurrences[0]
	if first.EventID != ID("SUBMIT", "nộp hồ sơ") {
		t.Errorf("identifier: got %s, want the one the class and label produce", first.EventID)
	}
	if first.Evidence.ProvisionID != "p1" || first.Evidence.DocID != "d1" {
		t.Errorf("evidence: got %s in %s, want p1 in d1", first.Evidence.ProvisionID, first.Evidence.DocID)
	}
	if len(first.Participants) != 3 {
		t.Errorf("participants: got %d, want 3: %+v", len(first.Participants), first.Participants)
	}
	if first.Participants[0].Role != RoleAgent || first.Participants[0].LabelVI != "người đề nghị" {
		t.Errorf("the first participant: got %+v, want the agent with the label from the concept layer", first.Participants[0])
	}
	if len(got.Chains) != 1 {
		t.Fatalf("chains: got %d, want 1", len(got.Chains))
	}
	c := got.Chains[0]
	if c.FromID != ID("SUBMIT", "nộp hồ sơ") || c.ToID != ID("ISSUE", "cấp giấy chứng nhận đăng ký doanh nghiệp") {
		t.Errorf("chain ends: got %s to %s", c.FromID, c.ToID)
	}
	if c.Status != StatusProvisional {
		t.Errorf("a chain from one provision came back %s, and one sentence is not corroboration", c.Status)
	}
	if c.Evidence[0].DirectionCheck == "" {
		t.Error("the chain carries no direction check, so nobody can read what the first pass thought")
	}
	if usage.TotalTokens == 0 {
		t.Error("usage was not carried back, so a run cannot report what it cost")
	}
}

func TestExtractRefusesAQuoteThatIsNotInTheProvision(t *testing.T) {
	bad := `{"events":[{"class":"SUBMIT","label_vi":"nộp hồ sơ","quote":"nộp đơn xin phép","char_start":0,"char_end":16,"confidence":0.9}]}`
	good := reply(t, "nộp hồ sơ đến cơ quan đăng ký kinh doanh", twoActs)
	model := &answers{replies: []string{bad, good}}
	got, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if model.calls != 2 {
		t.Fatalf("calls: got %d, want 2, because an unsupported quote has to be sent back", model.calls)
	}
	if !strings.Contains(model.inputs[1], "bị từ chối") {
		t.Error("the second prompt does not say what was wrong, so the model is being asked the same question again")
	}
	if len(got.Occurrences) != 2 {
		t.Errorf("acts after the correction: got %d, want 2", len(got.Occurrences))
	}
}

func TestOffsetsComeFromTheTextAndNotFromTheAnswer(t *testing.T) {
	// The quote is really in the provision and the offsets sent with it are
	// somebody counting UTF-8 bytes by eye, which is the demand that lost two
	// whole documents on the first real run. The quote is the evidence and the
	// text says where it is, so the answer is kept and the offsets are repaired.
	body := `{"events":[{"class":"SUBMIT","label_vi":"nộp hồ sơ","quote":"nộp hồ sơ","char_start":0,"char_end":9,"confidence":0.9}]}`
	model := &answers{replies: []string{body}}
	got, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if model.calls != 1 {
		t.Errorf("calls: got %d, want 1, because there was nothing to correct", model.calls)
	}
	if len(got.Occurrences) != 1 {
		t.Fatalf("acts: got %d, want 1", len(got.Occurrences))
	}
	start, end := at(t, provision, "nộp hồ sơ")
	ev := got.Occurrences[0].Evidence
	if ev.CharStart != start || ev.CharEnd != end {
		t.Errorf("offsets: got %d to %d, want %d to %d, the ones the provision has", ev.CharStart, ev.CharEnd, start, end)
	}
	if provision[ev.CharStart:ev.CharEnd] != ev.Quote {
		t.Error("the offsets on the evidence do not cut the quote out of the provision")
	}
}

func TestExtractRefusesAGenericClass(t *testing.T) {
	bad := reply(t, "nộp hồ sơ", `{"events":[{"class":"HANH_VI","label_vi":"nộp hồ sơ","quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.9}]}`)
	model := &answers{replies: []string{bad}}
	_, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err == nil {
		t.Fatal("the generic class HANH_VI was accepted")
	}
	if !strings.Contains(model.inputs[1], "HANH_VI") {
		t.Errorf("the correction does not name the class it refused: %q", model.inputs[1])
	}
}

func TestExtractKeepsAnInventedClassWithItsDefinition(t *testing.T) {
	invented := reply(t, "nộp hồ sơ", `{"events":[{"class":"CHUYEN_NHUONG_CO_PHAN","label_vi":"chuyển nhượng cổ phần",
"class_definition":"Một bên chuyển quyền sở hữu cổ phần cho bên khác.",
"quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.8}]}`)
	model := &answers{replies: []string{invented}}
	got, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(got.Occurrences) != 1 {
		t.Fatalf("acts: got %d, want 1", len(got.Occurrences))
	}
	if got.Occurrences[0].Definition == "" {
		t.Error("the invented class lost its definition, and the definition is the only thing the review queue can be read on")
	}
}

func TestExtractSendsBackAnInventedClassWithNoDefinition(t *testing.T) {
	bare := reply(t, "nộp hồ sơ", `{"events":[{"class":"CHUYEN_NHUONG_CO_PHAN","label_vi":"chuyển nhượng cổ phần",
"quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.8}]}`)
	model := &answers{replies: []string{bare}}
	_, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err == nil {
		t.Fatal("a class nobody has defined was accepted with no sentence saying what it means")
	}
	if !strings.Contains(model.inputs[1], "class_definition") {
		t.Errorf("the correction does not ask for a definition: %q", model.inputs[1])
	}
}

func TestExtractRefusesAParticipantNobodyOffered(t *testing.T) {
	bad := reply(t, "nộp hồ sơ", `{"events":[{"class":"SUBMIT","label_vi":"nộp hồ sơ",
"participants":[{"role":"AGENT","concept_id":"c99","as_written":"ai đó"}],
"quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.9}]}`)
	model := &answers{replies: []string{bad}}
	_, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err == nil {
		t.Fatal("a participant outside the offered concepts was accepted, which is how a string gets back into the graph")
	}
}

func TestExtractRefusesARoleOutsideTheSet(t *testing.T) {
	bad := reply(t, "nộp hồ sơ", `{"events":[{"class":"SUBMIT","label_vi":"nộp hồ sơ",
"participants":[{"role":"BENEFICIARY","concept_id":"c1"}],
"quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.9}]}`)
	model := &answers{replies: []string{bad}}
	_, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err == nil {
		t.Fatal("a role outside the closed set was accepted")
	}
}

func TestExtractRefusesAChainToAnActItDidNotName(t *testing.T) {
	bad := reply(t, "nộp hồ sơ", `{"events":[{"class":"SUBMIT","label_vi":"nộp hồ sơ",
"quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.9}],
"chains":[{"from_event":"nộp hồ sơ","to_event":"thu hồi giấy phép","type":"PRECEDES",
"quote":"$quote","char_start":$start,"char_end":$end,"direction_check":"nộp trước","confidence":0.9}]}`)
	model := &answers{replies: []string{bad}}
	_, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err == nil {
		t.Fatal("a chain to an act nobody described was accepted, so the graph would carry an edge to a node with no evidence")
	}
	if !strings.Contains(model.inputs[1], "thu hồi giấy phép") {
		t.Errorf("the correction does not name the act it could not find: %q", model.inputs[1])
	}
}

func TestExtractRefusesAChainWithNoDirectionCheck(t *testing.T) {
	bad := reply(t, "nộp hồ sơ", `{"events":[{"class":"SUBMIT","label_vi":"nộp hồ sơ",
"quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.9},
{"class":"ISSUE","label_vi":"cấp giấy phép","quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.9}],
"chains":[{"from_event":"nộp hồ sơ","to_event":"cấp giấy phép","type":"PRECEDES",
"quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.9}]}`)
	model := &answers{replies: []string{bad}}
	_, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err == nil {
		t.Fatal("a chain with nothing said about its direction was accepted")
	}
}

func TestExtractAcceptsAProvisionThatNamesNoAct(t *testing.T) {
	// A definition clause or a scope clause names no act, and saying so is the
	// right answer. An extractor that treats silence as a failure teaches the
	// model to invent something.
	model := &answers{replies: []string{`{"events":[]}`}}
	got, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err != nil {
		t.Fatalf("an empty answer was treated as a failure: %v", err)
	}
	if len(got.Occurrences) != 0 || len(got.Chains) != 0 {
		t.Errorf("something was invented out of an empty answer: %+v", got)
	}
	if model.calls != 1 {
		t.Errorf("calls: got %d, want 1, because an empty answer needs no correction", model.calls)
	}
}

func TestExtractLinksANormToItsAct(t *testing.T) {
	norms := []Norm{{StatementID: "s1", Type: "obligation", Action: "nộp hồ sơ", Sanction: "phạt tiền"}}
	body := reply(t, "nộp hồ sơ", `{"events":[{"class":"SUBMIT","label_vi":"nộp hồ sơ",
"quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.9},
{"class":"PENALISE","label_vi":"phạt tiền","quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.8}],
"links":[{"statement_id":"s1","slot":"action","event":"nộp hồ sơ"},
{"statement_id":"s1","slot":"sanction","event":"phạt tiền"}]}`)
	model := &answers{replies: []string{body}}
	got, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), norms)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(got.Links) != 2 {
		t.Fatalf("links: got %d, want 2: %+v", len(got.Links), got.Links)
	}
	if got.Links[0].Kind != LinkAction || got.Links[0].EventID != ID("SUBMIT", "nộp hồ sơ") {
		t.Errorf("the action link: got %+v", got.Links[0])
	}
	if got.Links[1].Kind != LinkSanction || got.Links[1].EventID != ID("PENALISE", "phạt tiền") {
		t.Errorf("the sanction link: got %+v", got.Links[1])
	}
	if !strings.Contains(model.inputs[0], "s1") {
		t.Error("the prompt does not offer the statement, so the model could not have known what to link")
	}
}

func TestExtractRefusesALinkToANormNobodyOffered(t *testing.T) {
	body := reply(t, "nộp hồ sơ", `{"events":[{"class":"SUBMIT","label_vi":"nộp hồ sơ",
"quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.9}],
"links":[{"statement_id":"s9","slot":"action","event":"nộp hồ sơ"}]}`)
	model := &answers{replies: []string{body}}
	_, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(),
		[]Norm{{StatementID: "s1", Type: "obligation", Action: "nộp hồ sơ"}})
	if err == nil {
		t.Fatal("a link to a statement the prompt never offered was accepted")
	}
}

func TestExtractCountsOneActNamedTwiceAsOneAct(t *testing.T) {
	body := reply(t, "nộp hồ sơ", `{"events":[{"class":"SUBMIT","label_vi":"nộp hồ sơ",
"quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.9},
{"class":"SUBMIT","label_vi":"Nộp hồ sơ","quote":"$quote","char_start":$start,"char_end":$end,"confidence":0.7}]}`)
	model := &answers{replies: []string{body}}
	got, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(got.Occurrences) != 1 {
		t.Fatalf("acts: got %d, want 1, because one provision naming an act twice is one act", len(got.Occurrences))
	}
}

func TestExtractGivesUpAfterTheCorrections(t *testing.T) {
	model := &answers{replies: []string{"not json at all"}}
	x := extractor(model)
	x.MaxCorrections = 1
	_, _, err := x.Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err == nil {
		t.Fatal("an answer nobody could parse was accepted")
	}
	if model.calls != 2 {
		t.Errorf("calls: got %d, want 2, one and one correction", model.calls)
	}
	if !strings.Contains(err.Error(), "p1") {
		t.Errorf("the error does not say which provision failed: %v", err)
	}
}

func TestExtractCarriesTheProviderErrorOut(t *testing.T) {
	model := &answers{err: errors.New("route is down")}
	_, _, err := extractor(model).Extract(context.Background(), "p1", "d1", provision, concepts(), nil)
	if err == nil || !strings.Contains(err.Error(), "route is down") {
		t.Errorf("a provider error was swallowed: %v", err)
	}
}

func TestInstructionsNameTheClassesAndForbidTheGenericOnes(t *testing.T) {
	got := extractor(&answers{}).Instructions()
	for _, id := range []string{"SUBMIT", "ISSUE", "PENALISE"} {
		if !strings.Contains(got, id) {
			t.Errorf("the prompt does not offer %s", id)
		}
	}
	for _, id := range []string{Triggers, Precedes, PreconditionOf, Precludes} {
		if !strings.Contains(got, id) {
			t.Errorf("the prompt does not offer the chain type %s", id)
		}
	}
	if !strings.Contains(got, "HANH_VI") {
		t.Error("the prompt does not tell the model which generic names are refused")
	}
	if !strings.Contains(got, "rỗng") {
		t.Error("the prompt does not say an empty answer is allowed, which is how a model learns to invent an act")
	}
}

func TestPromptShowsTheConceptsAndTheNorms(t *testing.T) {
	got := Prompt("p1", provision, concepts(), []Norm{{StatementID: "s1", Type: "obligation", Action: "nộp hồ sơ", Sanction: "phạt tiền"}})
	for _, want := range []string{"p1", provision, "c1", "người đề nghị", "s1", "phạt tiền"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt does not carry %q", want)
		}
	}
}
