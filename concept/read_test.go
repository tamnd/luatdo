package concept

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/anchor"
	"github.com/tamnd/luatdo/api"
)

// scripted answers with a fixed list of responses in order, and records what it
// was asked. No model routes are configured on any machine in the fleet yet, so
// every test here runs against a script. That is a real limit and it is stated
// rather than hidden: these tests prove the machinery around the call, not the
// quality of a reading.
type scripted struct {
	replies []string
	inputs  []string
	calls   int
}

func (s *scripted) Complete(_ context.Context, req api.Request) (api.Response, error) {
	s.inputs = append(s.inputs, req.Input)
	i := s.calls
	s.calls++
	if i >= len(s.replies) {
		i = len(s.replies) - 1
	}
	return api.Response{Text: s.replies[i], Usage: api.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150}}, nil
}

func unit() *anchor.Unit {
	return &anchor.Unit{
		ID:        "vn:law:2019:45-2019-qh14:article-3:clause-1",
		DocID:     "vn:law:2019:45-2019-qh14",
		ScopeID:   "vn:law:2019:45-2019-qh14",
		ArticleID: "vn:law:2019:45-2019-qh14:article-3",
		Number:    "1",
		Text:      clause,
		TextHash:  "3f9a1c2b",
	}
}

func reply(t *testing.T, r wireReading) string {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

func goodReading(t *testing.T) wireReading {
	t.Helper()
	return wireReading{Terms: []wireTerm{{
		LabelVI:      "Người lao động",
		DefinitionVI: "người làm việc cho người sử dụng lao động theo thỏa thuận",
		Genus:        "người làm việc cho người sử dụng lao động",
		Differentiae: []Differentia{{Text: "được trả lương", Quote: "được trả lương"}},
		Kind:         KindActor,
		Quote:        clause,
		CharStart:    0,
		CharEnd:      len(clause),
		Confidence:   0.93,
	}}}
}

func TestAReadingIsAcceptedOnceItsQuotesCheckOut(t *testing.T) {
	model := &scripted{replies: []string{reply(t, goodReading(t))}}
	r := &Reader{Completer: model, Model: "test", MaxCorrections: 2}

	job, err := r.Read(context.Background(), unit(), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if model.calls != 1 {
		t.Errorf("a clean reading took %d calls", model.calls)
	}
	if len(job.TermUses) != 1 {
		t.Fatalf("job carries %d term uses", len(job.TermUses))
	}
	got := job.TermUses[0]
	if want := TermUseID("vn:law:2019:45-2019-qh14", "Người lao động"); got.ID != want {
		t.Errorf("id = %q, want %q", got.ID, want)
	}
	if got.DefinedBy != unit().ID {
		t.Errorf("defined_by = %q, want the clause pass A anchored", got.DefinedBy)
	}
	if got.Origin != OriginDefined {
		t.Errorf("origin = %q", got.Origin)
	}
	if job.Usage.TotalTokens != 150 {
		t.Errorf("usage = %+v, and a job that does not carry its cost cannot be budgeted", job.Usage)
	}
}

func TestTheModelDoesNotGetToMintIdentifiers(t *testing.T) {
	// The model is shown the clause identifier and the scope. Nothing it says
	// can change either: the identifier is minted from what pass A found, and a
	// scope it invents is refused rather than believed.
	reading := goodReading(t)
	reading.Terms[0].Scope = "vn:law:2014:58-2014-qh13"
	model := &scripted{replies: []string{reply(t, reading), reply(t, goodReading(t))}}
	r := &Reader{
		Completer:      model,
		Model:          "test",
		MaxCorrections: 2,
		Scopes:         map[string]bool{"vn:law:2019:45-2019-qh14": true},
	}

	job, err := r.Read(context.Background(), unit(), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(job.Attempts) != 2 || job.Attempts[0].Error == "" {
		t.Fatalf("the invented scope was not refused: %+v", job.Attempts)
	}
	if !strings.Contains(job.Attempts[0].Error, "không phải một văn bản có thật") {
		t.Errorf("refusal reads %q", job.Attempts[0].Error)
	}
	if job.TermUses[0].ScopeID != "vn:law:2019:45-2019-qh14" {
		t.Errorf("scope = %q", job.TermUses[0].ScopeID)
	}
}

func TestAFabricatedQuoteIsSentBackWithTheReason(t *testing.T) {
	bad := goodReading(t)
	bad.Terms[0].Differentiae = []Differentia{{Text: "từ đủ 15 tuổi", Quote: "từ đủ 15 tuổi trở lên"}}
	model := &scripted{replies: []string{reply(t, bad), reply(t, goodReading(t))}}
	r := &Reader{Completer: model, Model: "test", MaxCorrections: 2}

	job, err := r.Read(context.Background(), unit(), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if model.calls != 2 {
		t.Fatalf("the fabricated quote was accepted, calls = %d", model.calls)
	}
	if len(job.TermUses) != 1 || job.Err != "" {
		t.Fatalf("the corrected reading was not kept: %+v", job)
	}
	// The correction has to name the failure. A retry that says only that
	// something was wrong gets the same answer back and burns the budget.
	second := model.inputs[1]
	if !strings.Contains(second, "từ đủ 15 tuổi trở lên") || !strings.Contains(second, "bị từ chối") {
		t.Errorf("the correction does not name what failed:\n%s", second)
	}
	if len(job.Attempts) != 2 || job.Attempts[0].Error == "" || job.Attempts[1].Error != "" {
		t.Errorf("the attempt trail is %+v, and a job that drops its failed attempts hides prompt problems", job.Attempts)
	}
}

func TestTheBudgetRunsOutAndTheJobSaysSo(t *testing.T) {
	model := &scripted{replies: []string{"not json at all"}}
	r := &Reader{Completer: model, Model: "test", MaxCorrections: 2}

	job, err := r.Read(context.Background(), unit(), nil)
	if err != nil {
		t.Fatalf("Read returned an error for a model that answered: %v", err)
	}
	if model.calls != 3 {
		t.Errorf("made %d calls for a budget of 2 corrections, want 3", model.calls)
	}
	if job.Err == "" || len(job.TermUses) != 0 {
		t.Errorf("an unreadable run produced %+v", job)
	}
	if len(job.Rejected) == 0 {
		t.Error("the job does not say why it gave up")
	}
}

func TestAClauseThatDefinesNothingIsAnAnswer(t *testing.T) {
	// A definitions article routinely carries a clause saying the remaining
	// terms follow another law. Forcing a term out of it is how a pipeline
	// manufactures vocabulary, so the empty answer is legitimate and distinct
	// from silence.
	model := &scripted{replies: []string{reply(t, wireReading{DefinesNothing: true})}}
	r := &Reader{Completer: model, Model: "test", MaxCorrections: 2}

	job, err := r.Read(context.Background(), unit(), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !job.DefinesNo || len(job.TermUses) != 0 || job.Err != "" {
		t.Fatalf("job = %+v", job)
	}

	// An empty list with no such statement is ambiguous, so it is refused.
	quiet := &scripted{replies: []string{`{"terms":[]}`, reply(t, wireReading{DefinesNothing: true})}}
	r.Completer = quiet
	job, err = r.Read(context.Background(), unit(), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if quiet.calls != 2 || !job.DefinesNo {
		t.Errorf("an empty answer was taken for a clause that defines nothing: %d calls, %+v", quiet.calls, job)
	}
}

func TestADefinitionByReferenceKeepsThePointerAndNothingElse(t *testing.T) {
	const pointer = "Các từ ngữ khác trong Nghị định này được hiểu theo quy định tại Luật Bảo hiểm xã hội."
	u := unit()
	u.Text = pointer
	reading := wireReading{Terms: []wireTerm{{
		LabelVI:            "Các từ ngữ khác",
		Kind:               KindStatus,
		DefinesByReference: "Luật Bảo hiểm xã hội",
		ReferenceQuote:     "được hiểu theo quy định tại Luật Bảo hiểm xã hội",
		Quote:              pointer,
		CharStart:          0,
		CharEnd:            len(pointer),
		Confidence:         0.88,
	}}}
	model := &scripted{replies: []string{reply(t, reading)}}
	r := &Reader{Completer: model, Model: "test", MaxCorrections: 2}

	job, err := r.Read(context.Background(), u, nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got := job.TermUses[0]
	if got.DefinesByReference == nil || got.DefinesByReference.Instrument != "Luật Bảo hiểm xã hội" {
		t.Fatalf("the pointer was lost: %+v", got)
	}
	if got.DefinitionVI != "" {
		t.Errorf("a pointer definition came back carrying %q", got.DefinitionVI)
	}
}

func TestRoleIsAskedAndCarried(t *testing.T) {
	const text = "Cơ quan có thẩm quyền là cơ quan được giao thực hiện nhiệm vụ theo quy định của pháp luật."
	u := unit()
	u.Text = text
	reading := wireReading{Terms: []wireTerm{{
		LabelVI:    "Cơ quan có thẩm quyền",
		Genus:      "cơ quan được giao thực hiện nhiệm vụ",
		Kind:       KindActor,
		IsRole:     true,
		Quote:      text,
		CharStart:  0,
		CharEnd:    len(text),
		Confidence: 0.9,
	}}}
	model := &scripted{replies: []string{reply(t, reading)}}
	r := &Reader{Completer: model, Model: "test", MaxCorrections: 1}

	job, err := r.Read(context.Background(), u, nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !job.TermUses[0].IsRole {
		t.Error("the role answer was dropped, and a role resolved globally is the worst error this layer can make")
	}
	// The question has to be in the prompt. Inferring the role afterwards from
	// the label is exactly the shortcut that resolves co quan co tham quyen to
	// one ministry for the whole corpus.
	if !strings.Contains(r.Instructions(), "is_role") {
		t.Error("the prompt never asks about roles")
	}
}

func TestThePromptShowsTheAnnexScopeWithoutAskingForIt(t *testing.T) {
	u := unit()
	u.ScopeID = "vn:decision:2020:15-2020-qd-ttg:annex-1"
	scope := &anchor.Scope{
		ID:         u.ScopeID,
		Kind:       "annex",
		Instrument: "Quy chế này",
		Formula:    "Trong Quy chế này, các từ ngữ dưới đây được hiểu như sau:",
	}
	got := Prompt(u, scope)
	for _, want := range []string{"Quy chế này", u.ScopeID, "không được trích dẫn", u.Text} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt is missing %q:\n%s", want, got)
		}
	}
}
