package subject

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func classified() []Record {
	return []Record{
		{DocID: "vn:law:2019:45-2019-qh14", DocType: "Bộ luật", Subjects: []Assignment{
			{SubjectID: "lao-dong/hop-dong-lao-dong", Confidence: 0.71, Method: MethodLexical, Matched: []string{"hợp đồng lao động"}},
			{SubjectID: "lao-dong", Confidence: 0.71, Method: MethodParent},
		}},
		{DocID: "vn:law:2013:45-2013-qh13", DocType: "Luật", Subjects: []Assignment{
			{SubjectID: "dat-dai/giao-dat-cho-thue-dat", Confidence: 0.6, Method: MethodLexical},
			{SubjectID: "dat-dai/gia-dat", Confidence: 0.5, Method: MethodLexical},
			{SubjectID: "dat-dai", Confidence: 0.6, Method: MethodParent},
		}},
		{DocID: "vn:law:2004:12-2004-qd-ub:ubnd-tinh-long-an", DocType: "Quyết định"},
	}
}

func TestRecordsSurviveTheRoundTrip(t *testing.T) {
	data, err := Encode(classified())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if lines := strings.Count(string(data), "\n"); lines != 3 {
		t.Errorf("encoded %d lines for 3 records, the file is read and edited by line", lines)
	}

	path := filepath.Join(t.TempDir(), AssignmentsFile)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadRecords(path)
	if err != nil {
		t.Fatalf("ReadRecords: %v", err)
	}
	want := classified()
	if len(got) != len(want) {
		t.Fatalf("read %d records, wrote %d", len(got), len(want))
	}
	for i := range want {
		if got[i].DocID != want[i].DocID || got[i].DocType != want[i].DocType {
			t.Errorf("record %d = %+v, want %+v", i, got[i], want[i])
		}
		if len(got[i].Subjects) != len(want[i].Subjects) {
			t.Fatalf("record %d came back with %d subjects, want %d", i, len(got[i].Subjects), len(want[i].Subjects))
		}
		for j := range want[i].Subjects {
			a, b := got[i].Subjects[j], want[i].Subjects[j]
			if a.SubjectID != b.SubjectID || a.Confidence != b.Confidence || a.Method != b.Method {
				t.Errorf("record %d subject %d = %+v, want %+v", i, j, a, b)
			}
		}
	}
	// The document nothing matched is a record with no subjects rather than an
	// absent record. Dropping it would make the file disagree with the corpus
	// and would take the unclassified stratum out of every later sample.
	if len(got[2].Subjects) != 0 {
		t.Errorf("the unclassified document came back with %v", got[2].Subjects)
	}
}

func TestReadRecordsSkipsBlankLinesAndRefusesRubbish(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.jsonl")
	if err := os.WriteFile(good, []byte("\n{\"doc_id\":\"a\"}\n\n{\"doc_id\":\"b\"}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadRecords(good)
	if err != nil || len(got) != 2 {
		t.Fatalf("ReadRecords = %v, %v, want two records", got, err)
	}

	bad := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(bad, []byte("{\"doc_id\":\"a\"}\nnot json\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadRecords(bad); err == nil {
		t.Error("a half readable assignments file was read as if it were whole")
	}
	if _, err := ReadRecords(filepath.Join(dir, "absent.jsonl")); err == nil {
		t.Error("a missing assignments file read without error")
	}
}

func TestSelectionsSurviveTheRoundTrip(t *testing.T) {
	data, err := EncodeSelection(Sample(classified(), 2, "m6"))
	if err != nil {
		t.Fatalf("EncodeSelection: %v", err)
	}
	if lines := strings.Count(string(data), "\n"); lines != 2 {
		t.Errorf("encoded %d lines for a sample of 2", lines)
	}
	if !strings.Contains(string(data), "\"stratum\"") {
		t.Errorf("the selection does not carry the cell it came from: %s", data)
	}
}

func TestSummaryCountsDomainsAndSubdomainsApart(t *testing.T) {
	sum := NewSummary()
	for _, r := range classified() {
		sum.Add(&r)
	}
	if sum.Documents != 3 || sum.Assigned != 2 || sum.Unassigned != 1 {
		t.Errorf("summary = %d documents, %d assigned, %d not", sum.Documents, sum.Assigned, sum.Unassigned)
	}
	if sum.ByDomain["lao-dong"] != 1 || sum.ByDomain["dat-dai"] != 1 {
		t.Errorf("by domain = %v", sum.ByDomain)
	}
	if sum.BySubdomain["dat-dai/gia-dat"] != 1 || len(sum.BySubdomain) != 3 {
		t.Errorf("by subdomain = %v", sum.BySubdomain)
	}
	// A run that files everything under one subdomain and a run that files
	// everything under three both look fine in a total, and both are wrong, so
	// the label counts are kept apart.
	if sum.Labels[0] != 1 || sum.Labels[1] != 1 || sum.Labels[2] != 1 {
		t.Errorf("labels = %v, want one document at each width", sum.Labels)
	}

	report := sum.String()
	for _, want := range []string{"2 classified, 1 under nothing", "domains    2", "subdomains 3", "lao-dong"} {
		if !strings.Contains(report, want) {
			t.Errorf("summary report is missing %q:\n%s", want, report)
		}
	}
	if strings.HasSuffix(report, "\n") {
		t.Error("the report ends in a newline, and the caller prints it with one")
	}
}
