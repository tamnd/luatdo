package extract

import (
	"context"
	"fmt"
	"testing"

	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
)

const clauseID = "vn:law:2019:45-2019-qh14:article-3:clause-1"

func statementJSON(subject, action string, confidence float64) string {
	return fmt.Sprintf(`{"statement_type":"duty","subject":{"text":"%s","class_id":"vn-legal:Employee"},"modality":"obligation","action":{"text":"%s"},"evidence":{"quote":"làm việc cho người sử dụng lao động"},"confidence":%v}`,
		subject, action, confidence)
}

func verdictJSON(verdict string) string {
	return fmt.Sprintf(`{"verdict":"%s","rationale":"test"}`, verdict)
}

func TestNormRunnerFastVerified(t *testing.T) {
	c := &scripted{responses: []string{
		`{"statements":[` + statementJSON("người lao động", "làm việc", 0.9) + `]}`,
		verdictJSON(norm.VerdictEntailed),
	}}
	r := &NormRunner{Completer: c, Model: "m", Registry: ontology.Seed(), MaxCorrections: 2}
	job, err := r.Run(context.Background(), testDoc(), clauseID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if job.Mode != "fast" || len(job.Candidates) != 1 || len(job.Records) != 1 {
		t.Fatalf("mode=%s candidates=%d records=%d", job.Mode, len(job.Candidates), len(job.Records))
	}
	rec := job.Records[0]
	if rec.Status != "verified" || rec.Entailment.Verdict != norm.VerdictEntailed {
		t.Errorf("record = %+v", rec)
	}
	if rec.Falsification != nil {
		t.Error("fast mode must not call the falsification judge")
	}
	if rec.ID == "" || rec.OntologyVersion != 1 {
		t.Errorf("provenance missing: id=%q v=%d", rec.ID, rec.OntologyVersion)
	}
	if rec.Statement.Evidence.Start <= 0 {
		t.Error("evidence offsets must be computed during validation")
	}
}

func TestNormRunnerJudgeRejects(t *testing.T) {
	c := &scripted{responses: []string{
		`{"statements":[` + statementJSON("người lao động", "làm việc", 0.9) + `]}`,
		verdictJSON(norm.VerdictContradicted),
	}}
	r := &NormRunner{Completer: c, Model: "m", Registry: ontology.Seed()}
	job, err := r.Run(context.Background(), testDoc(), clauseID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	rec := job.Records[0]
	if rec.Status != "rejected" {
		t.Errorf("status = %q, a contradicted statement is stored, not dropped", rec.Status)
	}
	if rec.Entailment.Verdict != norm.VerdictContradicted {
		t.Errorf("verdict = %q", rec.Entailment.Verdict)
	}
}

func TestNormRunnerInvalidStatementSkipsJudges(t *testing.T) {
	bad := `{"statements":[{"statement_type":"duty","subject":{"text":"ai đó"},"action":{"text":"làm gì đó"},"evidence":{"quote":"câu này không có trong điều khoản"},"confidence":0.9}]}`
	c := &scripted{responses: []string{bad}}
	r := &NormRunner{Completer: c, Model: "m", Registry: ontology.Seed()}
	job, err := r.Run(context.Background(), testDoc(), clauseID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	rec := job.Records[0]
	if rec.Status != "invalid" || rec.Invalid == "" {
		t.Errorf("record = %+v, a fabricated quote fails the invariants", rec)
	}
	if rec.Entailment != nil {
		t.Error("an invalid statement must not cost a judge call")
	}
	if len(c.inputs) != 1 {
		t.Errorf("model calls = %d, want the extraction call only", len(c.inputs))
	}
}

func TestNormRunnerSlowMode(t *testing.T) {
	// Three candidates: the first two extract the same claim, the third adds a
	// second claim. The union keeps two statements; the first survives both
	// judges, the second falls to the falsification judge.
	c := &scripted{responses: []string{
		`{"statements":[` + statementJSON("người lao động", "làm việc", 0.7) + `]}`,
		`{"statements":[` + statementJSON("người lao động", "làm việc", 0.9) + `]}`,
		`{"statements":[` + statementJSON("người lao động", "thỏa thuận", 0.8) + `]}`,
		verdictJSON(norm.VerdictEntailed),
		verdictJSON(norm.VerdictEntailed),
		verdictJSON(norm.VerdictEntailed),
		verdictJSON(norm.VerdictPartiallySupported),
	}}
	r := &NormRunner{Completer: c, Model: "m", Registry: ontology.Seed(), Mode: "slow", Population: 3}
	job, err := r.Run(context.Background(), testDoc(), clauseID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if job.Mode != "slow" || len(job.Candidates) != 3 {
		t.Fatalf("mode=%s candidates=%d", job.Mode, len(job.Candidates))
	}
	if len(job.Records) != 2 {
		t.Fatalf("records = %d, the union must dedup the shared claim", len(job.Records))
	}
	first := job.Records[0]
	if first.Status != "verified" || first.Falsification == nil {
		t.Errorf("first record = %+v, slow mode needs both judges", first)
	}
	if first.Statement.Confidence != 0.9 {
		t.Errorf("confidence = %v, the union keeps the stronger extraction", first.Statement.Confidence)
	}
	second := job.Records[1]
	if second.Status != "rejected" || second.Falsification.Verdict != norm.VerdictPartiallySupported {
		t.Errorf("second record = %+v", second)
	}
}

func TestNormRunnerCorrectsMalformedJSON(t *testing.T) {
	c := &scripted{responses: []string{
		"đây không phải JSON",
		`{"statements":[` + statementJSON("người lao động", "làm việc", 0.9) + `]}`,
		verdictJSON(norm.VerdictEntailed),
	}}
	r := &NormRunner{Completer: c, Model: "m", Registry: ontology.Seed(), MaxCorrections: 2}
	job, err := r.Run(context.Background(), testDoc(), clauseID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	cand := job.Candidates[0]
	if len(cand.Attempts) != 2 || cand.Attempts[0].Error == "" {
		t.Fatalf("attempts = %+v", cand.Attempts)
	}
	if job.Records[0].Status != "verified" {
		t.Errorf("record = %+v", job.Records[0])
	}
}

func TestUnion(t *testing.T) {
	a := norm.Statement{Type: "duty", Subject: &norm.Ref{Text: "Người lao động"}, Action: norm.Ref{Text: "làm việc"}, Confidence: 0.6}
	b := a
	b.Confidence = 0.9
	other := norm.Statement{Type: "right", Subject: &norm.Ref{Text: "Người lao động"}, Action: norm.Ref{Text: "nghỉ ngơi"}, Confidence: 0.8}
	got := Union([]Candidate{
		{Statements: []norm.Statement{a}},
		{Statements: []norm.Statement{b, other}},
	})
	if len(got) != 2 {
		t.Fatalf("union = %d statements, want 2", len(got))
	}
	if got[0].Confidence != 0.9 {
		t.Errorf("confidence = %v, the stronger duplicate wins", got[0].Confidence)
	}
}
