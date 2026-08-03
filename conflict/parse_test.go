package conflict

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/norm"
)

// answers is a Completer that reads from a script, so a parsing test is about
// the parsing rather than about a network. The last reply is repeated, so a
// test that expects a correction loop to give up writes one bad answer.
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

const goodParse = `{"party":"người sử dụng lao động","act":"thông báo","object":"",` +
	`"toward":"người lao động","conditions":[],"exceptions":[]}`

func TestParseFillsTheCanonicalFormAndTheSlugsBesideIt(t *testing.T) {
	model := &answers{replies: []string{goodParse}}
	p := &Parser{Completer: model, Model: "test", MaxCorrections: 2}

	f, usage, err := p.Parse(context.Background(), record("a", "duty"), "Toàn văn điều khoản.")
	if err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 {
		t.Errorf("calls = %d, want one per statement", model.calls)
	}
	if usage.TotalTokens != 110 {
		t.Errorf("usage = %+v, want the call reported", usage)
	}
	if f.Operator != Obligation {
		t.Errorf("operator = %q, which the model was never asked about", f.Operator)
	}
	if f.Party != "nguoi-su-dung-lao-dong" || f.Act != "thong-bao" {
		t.Errorf("slugs = %q, %q", f.Party, f.Act)
	}
	if f.Canon.Party != "người sử dụng lao động" || f.Canon.Act != "thông báo" {
		t.Errorf("wording = %q, %q", f.Canon.Party, f.Canon.Act)
	}
	if f.Toward != "nguoi-lao-dong" {
		t.Errorf("toward = %q", f.Toward)
	}
	if !f.Comparable() {
		t.Error("a parsed duty is not comparable, so it would silently never enter a pair")
	}
}

func TestParsePrefersTheConceptIdentifierOverTheParsedParty(t *testing.T) {
	r := record("a", "duty")
	r.Statement.Bearer.ConceptID = "vn:concept:nguoi-su-dung-lao-dong"
	p := &Parser{Completer: &answers{replies: []string{goodParse}}, Model: "test"}

	f, _, err := p.Parse(context.Background(), r, "")
	if err != nil {
		t.Fatal(err)
	}
	// The concept identifier is the same key across every instrument that words
	// the party differently, so where the concept layer resolved one it wins.
	if f.Party != "vn:concept:nguoi-su-dung-lao-dong" {
		t.Errorf("party = %q, want the concept identifier", f.Party)
	}
	if f.Canon.Party == "" {
		t.Error("the wording was dropped, and a slug is a poor thing to show a lawyer")
	}
}

func TestParseCallsNothingForAStatementWithNoModality(t *testing.T) {
	model := &answers{replies: []string{goodParse}}
	p := &Parser{Completer: model, Model: "test"}

	f, usage, err := p.Parse(context.Background(), record("a", "definition"), "")
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Errorf("a definition produced a form: %+v", f)
	}
	if model.calls != 0 {
		t.Errorf("calls = %d, want none, a third of the trusted store cannot conflict with anything", model.calls)
	}
	if usage.TotalTokens != 0 {
		t.Errorf("usage = %+v for a call that was never made", usage)
	}
}

func TestParseCorrectsAnInventedCondition(t *testing.T) {
	// A parser free to invent a condition can make any pair non-comparable, and
	// one free to drop a condition makes every pair comparable. Both fail
	// silently and in opposite directions, so the count is fixed against the
	// statement the extractor already verified.
	invented := `{"party":"người sử dụng lao động","act":"thông báo","object":"","toward":"",` +
		`"conditions":["khi chấm dứt hợp đồng"],"exceptions":[]}`
	model := &answers{replies: []string{invented, goodParse}}
	p := &Parser{Completer: model, Model: "test", MaxCorrections: 2}

	f, usage, err := p.Parse(context.Background(), record("a", "duty"), "")
	if err != nil {
		t.Fatal(err)
	}
	if model.calls != 2 {
		t.Fatalf("calls = %d, want the answer corrected once", model.calls)
	}
	if len(f.Scope.Conditions) != 0 {
		t.Errorf("conditions = %v, want none, the statement has none", f.Scope.Conditions)
	}
	if usage.TotalTokens != 220 {
		t.Errorf("usage = %+v, want both calls counted", usage)
	}
	if !strings.Contains(model.inputs[1], "0 điều kiện") {
		t.Errorf("the correction does not say what was wrong:\n%s", model.inputs[1])
	}
}

func TestParseKeepsOneAtomPerClause(t *testing.T) {
	r := record("a", "duty")
	r.Statement.Conditions = []norm.Clause{
		{Kind: "trigger", Text: "khi hợp đồng lao động chấm dứt"},
		{Kind: "precondition", Text: "nếu người lao động đã làm việc đủ 12 tháng"},
	}
	reply := `{"party":"người sử dụng lao động","act":"thông báo","object":"","toward":"",` +
		`"conditions":["chấm dứt hợp đồng lao động","làm việc đủ 12 tháng"],"exceptions":[]}`
	p := &Parser{Completer: &answers{replies: []string{reply}}, Model: "test"}

	f, _, err := p.Parse(context.Background(), r, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cham-dut-hop-dong-lao-dong", "lam-viec-du-12-thang"}
	if len(f.Scope.Conditions) != len(want) {
		t.Fatalf("conditions = %v, want %v", f.Scope.Conditions, want)
	}
	for i := range want {
		if f.Scope.Conditions[i] != want[i] {
			t.Errorf("condition %d = %q, want %q", i, f.Scope.Conditions[i], want[i])
		}
	}
}

// wordy builds an answer whose one condition is longer than the pass would like.
func wordy(words int) string {
	return `{"party":"người sử dụng lao động","act":"thông báo","object":"","toward":"",` +
		`"conditions":["` + strings.TrimSpace(strings.Repeat("từ ", words)) + `"],"exceptions":[]}`
}

func withOneCondition(t *testing.T) *norm.Record {
	t.Helper()
	r := record("a", "duty")
	r.Statement.Conditions = []norm.Clause{{Kind: "trigger", Text: "khi hợp đồng lao động chấm dứt"}}
	return r
}

func TestParseAsksForAShorterClauseAndThenTakesWhatItIsGiven(t *testing.T) {
	long := wordy(maxClauseWords + 1)
	model := &answers{replies: []string{long}}
	p := &Parser{Completer: model, Model: "test", MaxCorrections: 2}

	f, _, err := p.Parse(context.Background(), withOneCondition(t), "")
	// A clause stated at length makes the form worse and not wrong. The count is
	// right, so the condition is the one the extractor already verified, and the
	// only cost is that its slug is unique to this norm and containment will not
	// hold against the other side. Refusing the answer instead throws away the
	// document and every call paid for the statements before it.
	if err != nil {
		t.Fatalf("a long condition lost the form: %v", err)
	}
	if len(f.Scope.Conditions) != 1 {
		t.Fatalf("conditions = %v", f.Scope.Conditions)
	}
	if model.calls != 3 {
		t.Errorf("calls = %d, want the shorter wording asked for twice before the answer is taken", model.calls)
	}
	if !strings.Contains(model.inputs[1], "viết ngắn lại") {
		t.Errorf("the correction does not ask for a shorter clause:\n%s", model.inputs[1])
	}
}

func TestParseStopsAskingOnceTheClauseIsShort(t *testing.T) {
	short := `{"party":"người sử dụng lao động","act":"thông báo","object":"","toward":"",` +
		`"conditions":["chấm dứt hợp đồng lao động"],"exceptions":[]}`
	model := &answers{replies: []string{wordy(maxClauseWords + 5), short}}
	p := &Parser{Completer: model, Model: "test", MaxCorrections: 2}

	f, _, err := p.Parse(context.Background(), withOneCondition(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if model.calls != 2 {
		t.Errorf("calls = %d, want the corrected answer accepted", model.calls)
	}
	if f.Scope.Conditions[0] != "cham-dut-hop-dong-lao-dong" {
		t.Errorf("condition = %q, want the shorter wording", f.Scope.Conditions[0])
	}
}

func TestClauseBoundIsSetAboveWhatTheCorpusHolds(t *testing.T) {
	// The conditions and exceptions in the trusted store run to a median of 11
	// words. A bound of twelve sat under half of what it was asking the model to
	// compress, and the model refused rather than drop meaning, which cost five
	// documents of nine in a real run.
	if maxClauseWords <= 12 {
		t.Errorf("maxClauseWords = %d, which is at or below the median clause it has to fit", maxClauseWords)
	}
	if verbose(wireForm{Conditions: []string{strings.Repeat("từ ", maxClauseWords)}}) != "" {
		t.Error("a clause at the bound was called long")
	}
	// The bound still fires, because a condition given as the whole sentence is
	// the failure this check exists for.
	if verbose(wireForm{Exceptions: []string{strings.Repeat("từ ", maxClauseWords+1)}}) == "" {
		t.Error("a clause over the bound was not noticed at all")
	}
}

func TestParseGivesUpRatherThanWriteAFormItDoesNotBelieve(t *testing.T) {
	model := &answers{replies: []string{`{"party":"","act":"","conditions":[],"exceptions":[]}`}}
	p := &Parser{Completer: model, Model: "test", MaxCorrections: 1}

	f, usage, err := p.Parse(context.Background(), record("a", "duty"), "")
	if err == nil {
		t.Fatal("a form with no party and no act was accepted")
	}
	if f != nil {
		t.Errorf("a failed parse still produced %+v", f)
	}
	if model.calls != 2 {
		t.Errorf("calls = %d, want the first try and one correction", model.calls)
	}
	if usage.TotalTokens != 220 {
		t.Errorf("usage = %+v, want both failed calls counted, they were paid for", usage)
	}
}

func TestParseSurvivesFencedJSON(t *testing.T) {
	model := &answers{replies: []string{"```json\n" + goodParse + "\n```"}}
	p := &Parser{Completer: model, Model: "test", MaxCorrections: 1}
	if _, _, err := p.Parse(context.Background(), record("a", "duty"), ""); err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 {
		t.Errorf("calls = %d, a fenced answer should not need a correction", model.calls)
	}
}

func TestParseReturnsTheTransportError(t *testing.T) {
	boom := errors.New("the endpoint is down")
	p := &Parser{Completer: &answers{err: boom}, Model: "test", MaxCorrections: 2}
	if _, _, err := p.Parse(context.Background(), record("a", "duty"), ""); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the transport error rather than a correction loop", err)
	}
}

func TestCheckNamesTheFirstProblemInTheModelsLanguage(t *testing.T) {
	s := &norm.Statement{
		Type: "duty", Bearer: &norm.Ref{Text: "Người sử dụng lao động"},
		Action: norm.Ref{Text: "thông báo"},
	}
	cases := []struct {
		name string
		w    wireForm
		want string
	}{
		{"no act", wireForm{Party: "x"}, "act rỗng"},
		{"the sentence copied into the act", wireForm{
			Party: "x", Act: strings.Repeat("từ ", maxActWords+1),
		}, "phải là cụm động từ ngắn"},
		{"the party dropped", wireForm{Act: "thông báo"}, "để party rỗng"},
		{"a clause left empty", wireForm{Party: "x", Act: "y", Conditions: []string{}}, ""},
	}
	for _, c := range cases {
		got := check(c.w, s)
		if c.want == "" && got != "" {
			t.Errorf("%s: check refused a usable answer: %s", c.name, got)
		}
		if c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: check said %q, want it to mention %q", c.name, got, c.want)
		}
	}
}

func TestCheckRefusesAPartyTheProvisionDoesNotName(t *testing.T) {
	s := &norm.Statement{Type: "duty", Action: norm.Ref{Text: "thông báo"}}
	got := check(wireForm{Party: "cơ quan nhà nước", Act: "thông báo"}, s)
	if !strings.Contains(got, "không nêu chủ thể") {
		t.Errorf("check accepted a party nobody wrote down: %q", got)
	}
}

func TestParsePromptCarriesTheProvisionAndTheStatement(t *testing.T) {
	r := record("a", "duty")
	r.Statement.Conditions = []norm.Clause{{Kind: "trigger", Text: "khi hợp đồng chấm dứt"}}
	text := "Toàn văn của khoản này, dài hơn trích dẫn."
	prompt := ParsePrompt(r, text)
	// The quote is the span that licensed the statement and is often a fragment,
	// and a fragment is where a negation gets lost.
	for _, want := range []string{r.ProvisionID, text, r.Statement.Action.Text,
		r.Statement.Bearer.Text, "khi hợp đồng chấm dứt", r.Statement.Evidence.Quote} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt does not carry %q:\n%s", want, prompt)
		}
	}
	// One statement at a time, never two. A model shown a pair produces a
	// verdict about the pair whether or not there is one to produce.
	if strings.Count(prompt, "Phát biểu:") != 1 {
		t.Errorf("the prompt holds more than one statement:\n%s", prompt)
	}
}
