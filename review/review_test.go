package review

import (
	"testing"

	"github.com/tamnd/luatdo/norm"
)

func record(confidence float64) *norm.Record {
	return &norm.Record{
		ID:     "vn:norm:abc",
		Status: "verified",
		Statement: norm.Statement{
			Type:       "duty",
			Confidence: confidence,
		},
		Entailment: &norm.Judgment{Verdict: norm.VerdictEntailed},
	}
}

func TestReasons(t *testing.T) {
	if got := Reasons(record(0.95)); got != nil {
		t.Errorf("clean record routed to review: %v", got)
	}
	if got := Reasons(record(0.5)); len(got) != 1 {
		t.Errorf("low confidence reasons = %v", got)
	}
	r := record(0.95)
	r.Statement.Sanction = &norm.Sanction{Text: "phạt tiền", LegalBasis: "Điều 17"}
	r.Statement.Exceptions = []norm.Clause{{Kind: norm.ExcForce, Text: "bất khả kháng", Quote: "trừ trường hợp bất khả kháng"}}
	r.Entailment = &norm.Judgment{Verdict: norm.VerdictPartiallySupported}
	if got := Reasons(r); len(got) != 3 {
		t.Errorf("reasons = %v, want sanction, exception, and verdict", got)
	}

	// A penalty that lives in another instrument is the one a wrong extraction
	// does the most damage with, so it goes to a human whatever the confidence.
	r = record(0.99)
	r.DocID = "vn:law:2019:45-2019-qh14"
	r.Statement.Sanction = &norm.Sanction{Text: "phạt tiền", LegalBasis: "Điều 17 Nghị định số 12/2022/NĐ-CP", BasisDoc: "vn:decree:2022:12-2022-nd-cp"}
	if got := Reasons(r); len(got) != 2 {
		t.Errorf("reasons = %v, want the sanction and its foreign basis", got)
	}

	// Question 10 asks which duties have no identified bearer and whether that
	// is the drafter's doing or the extractor's, and a bearer nobody could place
	// is exactly the case a person has to look at.
	r = record(0.99)
	r.Statement.Bearer = &norm.Ref{Text: "cơ quan có thẩm quyền", IsActor: true}
	if got := Reasons(r); len(got) != 1 {
		t.Errorf("reasons = %v, want the unplaced bearer", got)
	}

	r = record(0.1)
	r.Status = "invalid"
	if got := Reasons(r); got != nil {
		t.Errorf("invalid records must not reach the queue: %v", got)
	}
}

func TestQueueFold(t *testing.T) {
	dir := t.TempDir()
	items := []Item{
		{StatementID: "vn:norm:a", Reasons: []string{"sanction extracted"}, At: "t1"},
		{StatementID: "vn:norm:b", Reasons: []string{"confidence"}, At: "t1"},
		{StatementID: "vn:norm:a", Reasons: []string{"sanction extracted"}, At: "t2"},
	}
	if err := Enqueue(dir, items); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := Decide(dir, Decision{StatementID: "vn:norm:a", Verdict: "approved", At: "t3"}); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	queued, err := ReadQueue(dir)
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := ReadDecisions(dir)
	if err != nil {
		t.Fatal(err)
	}
	pending := Pending(queued, decisions)
	if len(pending) != 1 || pending[0].StatementID != "vn:norm:b" {
		t.Errorf("pending = %+v, decided and duplicate entries must fold away", pending)
	}
	if !Approved(decisions, "vn:norm:a") {
		t.Error("vn:norm:a was approved")
	}
	if Approved(decisions, "vn:norm:b") {
		t.Error("vn:norm:b has no decision")
	}
}
