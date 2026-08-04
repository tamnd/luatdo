package answer

import (
	"context"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/norm"
)

// scripted replays canned replies in order, the same discipline the rest of the
// repository uses: no network, no credentials.
type scripted struct {
	replies []string
	inputs  []string
}

func (s *scripted) Complete(_ context.Context, req api.Request) (api.Response, error) {
	if len(s.inputs) >= len(s.replies) {
		return api.Response{}, context.Canceled
	}
	s.inputs = append(s.inputs, req.Input)
	return api.Response{
		Text:  s.replies[len(s.inputs)-1],
		Usage: api.Usage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120},
	}, nil
}

const clause = "vn:law:2019:45-2019-qh14:article-94:clause-2"

func source() Source {
	return Source{
		ComponentID: clause,
		DocID:       "vn:law:2019:45-2019-qh14",
		Title:       "Bộ luật Lao động",
		Text:        "Trường hợp trả lương chậm thì người sử dụng lao động phải đền bù trong thời hạn 15 ngày.",
		Statements: []norm.Record{{
			ID: "vn:norm:aaa", ProvisionID: clause, Status: norm.StatusVerified,
			Statement: norm.Statement{
				Type:   "duty",
				Bearer: &norm.Ref{Text: "người sử dụng lao động", IsActor: true},
				Action: norm.Ref{Text: "đền bù"},
			},
		}},
	}
}

func ask(t *testing.T, reply string, sources ...Source) (Answer, *scripted) {
	t.Helper()
	s := &scripted{replies: []string{reply}}
	a := &Answerer{Completer: s, Model: "test-model"}
	out, err := a.Answer(context.Background(), Request{Question: "Ai phải đền bù khi trả lương chậm?", Sources: sources})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	return out, s
}

func TestAGroundedClaimSurvivesWithItsComponentAndQuote(t *testing.T) {
	out, _ := ask(t, `{"claims":[{"text":"Người sử dụng lao động phải đền bù.","component_id":"`+clause+
		`","statement_id":"vn:norm:aaa","quote":"người sử dụng lao động phải đền bù trong thời hạn 15 ngày"}]}`, source())
	if out.Refused {
		t.Fatalf("a grounded claim was refused: %s", out.Reason)
	}
	if len(out.Claims) != 1 || out.Claims[0].ComponentID != clause {
		t.Fatalf("claims are %v", out.Claims)
	}
	if kept, made := out.Grounded(); kept != 1 || made != 1 {
		t.Errorf("grounded reports %d of %d", kept, made)
	}
	if out.Usage.TotalTokens != 120 || out.Calls != 1 {
		t.Errorf("the answer did not carry what the call cost: %d calls, %d tokens", out.Calls, out.Usage.TotalTokens)
	}
}

func TestAClaimCitingAComponentNobodyRetrievedIsDeleted(t *testing.T) {
	out, _ := ask(t, `{"claims":[{"text":"Điều 96 quy định khác.","component_id":"vn:law:2019:45-2019-qh14:article-96",`+
		`"statement_id":"vn:norm:zzz","quote":"bất kỳ"}]}`, source())
	if !out.Refused {
		t.Fatal("a claim about a component that was never in front of the model survived")
	}
	if len(out.Dropped) != 1 || out.Dropped[0].Reason != DropUnknownComponent {
		t.Fatalf("dropped for %v", out.Dropped)
	}
}

func TestAParaphrasedQuoteIsDeleted(t *testing.T) {
	out, _ := ask(t, `{"claims":[{"text":"Phải đền bù.","component_id":"`+clause+
		`","statement_id":"vn:norm:aaa","quote":"người sử dụng lao động phải bồi thường trong 15 ngày"}]}`, source())
	if !out.Refused || len(out.Dropped) != 1 || out.Dropped[0].Reason != DropQuoteNotFound {
		t.Fatalf("a quote that is not in the provision survived: refused=%v dropped=%v", out.Refused, out.Dropped)
	}
}

func TestAClaimOnAStatementTheComponentDoesNotCarryIsDeleted(t *testing.T) {
	out, _ := ask(t, `{"claims":[{"text":"Phải đền bù.","component_id":"`+clause+
		`","statement_id":"vn:norm:invented","quote":"phải đền bù trong thời hạn 15 ngày"}]}`, source())
	if !out.Refused || out.Dropped[0].Reason != DropUnknownStatement {
		t.Fatalf("refused=%v dropped=%v", out.Refused, out.Dropped)
	}
}

func TestQuotesAreCheckedAcrossLineBreaksButNotAcrossEdits(t *testing.T) {
	if !Quoted("Trường hợp trả lương chậm\n  thì phải đền bù.", "trả lương chậm thì phải đền bù") {
		t.Error("a quote was rejected for whitespace the source wrapped differently")
	}
	if Quoted("Trường hợp trả lương chậm thì phải đền bù.", "trả lương chậm phải đền bù") {
		t.Error("a quote with words removed was accepted")
	}
	if Quoted("bất kỳ", "") {
		t.Error("an empty quote was accepted")
	}
}

func TestARefusalKeepsTheModelsOwnReason(t *testing.T) {
	out, _ := ask(t, `{"claims":[],"refusal":"Danh sách điều khoản không nói về thời hạn nộp hồ sơ."}`, source())
	if !out.Refused || !strings.Contains(out.Reason, "không nói về") {
		t.Fatalf("refusal is %q", out.Reason)
	}
	if len(out.Claims) != 0 {
		t.Error("a refusal carried claims")
	}
}

// The no retrieval baseline. The model is called with nothing, and whatever it
// writes from memory is what the check has to catch.
func TestAnEmptySourceListStillAsksTheModelAndNothingSurvives(t *testing.T) {
	out, s := ask(t, `{"claims":[{"text":"Theo Bộ luật Lao động, phải trả lương đúng hạn.",`+
		`"component_id":"vn:law:2019:45-2019-qh14:article-94","statement_id":"vn:norm:xxx","quote":"phải trả lương đúng hạn"}]}`)
	if len(s.inputs) != 1 {
		t.Fatal("the model was not called, so a baseline run would measure nothing")
	}
	if !strings.Contains(s.inputs[0], "rỗng") {
		t.Error("the empty source list was not stated to the model")
	}
	if !out.Refused || len(out.Dropped) != 1 {
		t.Fatalf("refused=%v dropped=%v", out.Refused, out.Dropped)
	}
	if !strings.Contains(out.Reason, "no sentence in it survived") {
		t.Errorf("reason is %q, want it to say the answer was withdrawn rather than never made", out.Reason)
	}
}

func TestAsOfIsStatedToTheModelAndCarriedOnTheAnswer(t *testing.T) {
	s := &scripted{replies: []string{`{"claims":[],"refusal":"Không đủ căn cứ."}`}}
	a := &Answerer{Completer: s, Model: "test-model"}
	out, err := a.Answer(context.Background(), Request{Question: "Điều 94 quy định gì?", AsOf: "2021-06-01", Sources: []Source{source()}})
	if err != nil {
		t.Fatal(err)
	}
	if out.AsOf != "2021-06-01" || !strings.Contains(s.inputs[0], "2021-06-01") {
		t.Errorf("the date the question was asked about did not reach the model or the answer")
	}
}

func TestABrokenReplyIsRetriedWithTheProblemStated(t *testing.T) {
	s := &scripted{replies: []string{"not json at all", "```json\n{\"claims\":[],\"refusal\":\"Không đủ căn cứ.\"}\n```"}}
	a := &Answerer{Completer: s, Model: "test-model", MaxCorrections: 1}
	out, err := a.Answer(context.Background(), Request{Question: "Ai phải đền bù?", Sources: []Source{source()}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Calls != 2 {
		t.Errorf("%d calls, want the broken reply retried once", out.Calls)
	}
	if !strings.Contains(s.inputs[1], "Lần trả lời trước bị lỗi") {
		t.Error("the retry did not tell the model what was wrong with the first reply")
	}
	if !out.Refused {
		t.Error("the fenced refusal was not read")
	}
}
