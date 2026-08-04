package omission

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/norm"
)

const text = "Người sử dụng lao động phải trả lương đúng hạn.\n" +
	"a) Người lao động không được tiết lộ bí mật kinh doanh;\n" +
	"b) Người lao động có quyền đơn phương chấm dứt hợp đồng.\n" +
	"Mức phạt là 1.000.000 đồng."

func TestSentencesSpanTheWholeText(t *testing.T) {
	got := Sentences(text)
	if len(got) != 4 {
		for _, s := range got {
			t.Logf("%d:%d %q", s.Start, s.End, s.Text)
		}
		t.Fatalf("%d sentences, want 4", len(got))
	}
	if !strings.Contains(got[3].Text, "1.000.000") {
		t.Errorf("the amount was split: %q", got[3].Text)
	}
	for _, s := range got {
		if !strings.Contains(text[s.Start:s.End], s.Text) {
			t.Errorf("the span %d:%d does not contain the sentence %q", s.Start, s.End, s.Text)
		}
	}
	if got[0].Start != 0 || got[len(got)-1].End != len(text) {
		t.Error("the sentences do not cover the text")
	}
}

func TestSentencesHandleTextWithNoStops(t *testing.T) {
	got := Sentences("Người lao động phải làm việc")
	if len(got) != 1 || got[0].End != len("Người lao động phải làm việc") {
		t.Errorf("sentences = %+v", got)
	}
	if len(Sentences("")) != 0 {
		t.Error("empty text has no sentences")
	}
}

func TestCarriesFindsEachMarker(t *testing.T) {
	if got := carries("Người sử dụng lao động phải trả lương"); len(got) != 1 || got[0] != "phải" {
		t.Errorf("markers = %v", got)
	}
	if got := carries("nghiêm cấm mọi hành vi và có trách nhiệm báo cáo"); len(got) != 2 {
		t.Errorf("markers = %v, want both", got)
	}
	if got := carries("Bên A thanh toán cho bên B"); len(got) != 0 {
		t.Errorf("markers = %v, this sentence states a rule with none of the five words", got)
	}
}

func record(quote, status string) norm.Record {
	return norm.Record{
		ID: quote, Status: status,
		Statement: norm.Statement{Type: "duty", Evidence: norm.Evidence{Quote: quote}},
	}
}

func TestProvisionSortsSentencesIntoThreeStates(t *testing.T) {
	var r Report
	r.Provision("d1", "p1", text, []norm.Record{
		record("Người sử dụng lao động phải trả lương đúng hạn", norm.StatusVerified),
		record("Người lao động không được tiết lộ bí mật kinh doanh", norm.StatusRejected),
	})
	if r.WithMarker != 3 {
		t.Fatalf("with marker = %d, want the three sentences carrying one", r.WithMarker)
	}
	if r.Covered != 1 || r.Dropped != 1 || r.Missed != 1 {
		t.Fatalf("covered=%d dropped=%d missed=%d", r.Covered, r.Dropped, r.Missed)
	}
	if len(r.Findings) != 2 {
		t.Fatalf("%d findings, the covered sentence is not one", len(r.Findings))
	}
	if got := r.Sorted()[0].State; got != Missed {
		t.Errorf("first finding is %q, the never extracted one comes first", got)
	}
	if c := r.ByMarker["có quyền"]; c.Sentences != 1 || c.Missed != 1 {
		t.Errorf("có quyền = %+v", c)
	}
	if c := r.ByMarker["không được"]; c.Dropped != 1 {
		t.Errorf("không được = %+v", c)
	}
	out := r.String()
	for _, want := range []string{"covered", "threw away", "nothing was ever extracted", "đơn phương chấm dứt"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report never mentions %q:\n%s", want, out)
		}
	}
}

// A condition quote grounds its sentence as much as an evidence quote does.
// Counting only the evidence would report the condition's sentence as a miss.
func TestConditionQuotesCoverTheirSentence(t *testing.T) {
	var r Report
	rec := record("Người sử dụng lao động phải trả lương đúng hạn", norm.StatusVerified)
	rec.Statement.Conditions = []norm.Clause{{
		Kind: norm.CondQualifying, Text: "bí mật kinh doanh",
		Quote: "Người lao động không được tiết lộ bí mật kinh doanh",
	}}
	r.Provision("d1", "p1", text, []norm.Record{rec})
	if r.Covered != 2 {
		t.Errorf("covered = %d, the condition quote covers its own sentence", r.Covered)
	}
}

func TestApprovedStatementsCover(t *testing.T) {
	var r Report
	r.Provision("d1", "p1", text, []norm.Record{
		record("Người sử dụng lao động phải trả lương đúng hạn", norm.StatusApproved),
	})
	if r.Covered != 1 || r.Dropped != 0 {
		t.Errorf("covered=%d dropped=%d, a statement a person kept covers its sentence", r.Covered, r.Dropped)
	}
}

func TestEmptyAuditSaysSoRatherThanReadingClean(t *testing.T) {
	var r Report
	out := r.String()
	if !strings.Contains(out, "nothing to read") || !strings.Contains(out, "the screen and not the corpus") {
		t.Errorf("an empty audit has to name itself:\n%s", out)
	}
}
