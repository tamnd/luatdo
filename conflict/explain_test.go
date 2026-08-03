package conflict

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// finding is one pair the checker could not reconcile, for the passes that run
// downstream of detection.
func finding(t *testing.T) *Finding {
	t.Helper()
	a, b := pair(Obligation, Prohibition)
	a.Scope.Conditions = []string{"trong-truong-hop-khan-cap"}
	f := only(t, a, b, nil)
	if f == nil {
		t.Fatal("the fixture pair was not reported, so there is nothing downstream to test")
	}
	return f
}

func TestExplainReturnsTheSentenceAPersonReads(t *testing.T) {
	model := &answers{replies: []string{
		`{"explanation":"Người sử dụng lao động vừa phải thông báo vừa bị cấm thông báo trong trường hợp khẩn cấp."}`,
	}}
	e := &Explainer{Completer: model, Model: "test", MaxCorrections: 2}

	got, usage, err := e.Explain(context.Background(), finding(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Người sử dụng lao động") {
		t.Errorf("explanation = %q", got)
	}
	if model.calls != 1 || usage.TotalTokens != 110 {
		t.Errorf("calls = %d, usage = %+v", model.calls, usage)
	}
}

func TestExplainRejectsAnEssay(t *testing.T) {
	long := strings.Repeat("từ ", maxExplanationWords+1)
	model := &answers{replies: []string{
		`{"explanation":"` + long + `"}`,
		`{"explanation":"Hai quy định va nhau khi hợp đồng chấm dứt."}`,
	}}
	e := &Explainer{Completer: model, Model: "test", MaxCorrections: 2}

	got, _, err := e.Explain(context.Background(), finding(t))
	if err != nil {
		t.Fatal(err)
	}
	if model.calls != 2 {
		t.Fatalf("calls = %d, want the first answer corrected", model.calls)
	}
	if strings.Contains(got, long) {
		t.Error("the essay was accepted after all")
	}
	if !strings.Contains(model.inputs[1], "dài") {
		t.Errorf("the correction does not say what was wrong:\n%s", model.inputs[1])
	}
}

func TestExplainRejectsAnEmptyAnswer(t *testing.T) {
	model := &answers{replies: []string{`{"explanation":"   "}`}}
	e := &Explainer{Completer: model, Model: "test", MaxCorrections: 1}
	if _, _, err := e.Explain(context.Background(), finding(t)); err == nil {
		t.Fatal("an empty explanation was accepted")
	}
}

func TestExplainReturnsTheTransportError(t *testing.T) {
	boom := errors.New("the endpoint is down")
	e := &Explainer{Completer: &answers{err: boom}, Model: "test", MaxCorrections: 2}
	if _, _, err := e.Explain(context.Background(), finding(t)); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the transport error", err)
	}
}

func TestExplainPromptCarriesTheDecisionAndNotTheQuestion(t *testing.T) {
	f := finding(t)
	prompt := ExplainPrompt(f)
	for _, want := range []string{f.A.Source.Quote, f.B.Source.Quote, f.A.ProvisionID, f.B.ProvisionID,
		"người sử dụng lao động", "trong-truong-hop-khan-cap"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt does not carry %q:\n%s", want, prompt)
		}
	}
	// The pass is handed a decision somebody else made. A model asked to explain
	// a conflict it was also asked to find will explain the one it found, and
	// nothing on the outside can tell that from a real one.
	if !strings.Contains(prompt, "Thành phần khác nhau") {
		t.Errorf("the prompt does not hand over the responsible slots:\n%s", prompt)
	}
}

func TestExplainInstructionsForbidPickingAWinner(t *testing.T) {
	e := &Explainer{}
	// Lex superior, lex posterior and lex specialis are reported by rank.go and
	// applied by nobody, and an explanation that resolved the pair would undo
	// that in the one field a reader actually reads.
	if !strings.Contains(e.Instructions(), "Không kết luận quy định nào được ưu tiên áp dụng") {
		t.Errorf("the instructions let the model decide which provision prevails:\n%s", e.Instructions())
	}
}
