package coverage

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/store"
)

// sample is one law with two articles. Article 1 is divided into a clause,
// which is divided into a point. Article 2 has text and no clauses.
func sample(id, docType string) *law.Document {
	return &law.Document{
		ID: id, DocType: docType, Status: "parsed",
		Provisions: []law.Provision{
			{ID: id + ":chapter-1", Kind: "chapter", Number: "I", Heading: "Quy định chung"},
			{ID: id + ":article-1", ParentID: id + ":chapter-1", Kind: "article", Number: "1",
				Heading: "Phạm vi", Text: "Ngân hàng được thực hiện các nghiệp vụ sau đây:"},
			{ID: id + ":article-1:clause-1", ParentID: id + ":article-1", Kind: "clause", Number: "1",
				Text: "Các nghiệp vụ gồm:"},
			{ID: id + ":article-1:clause-1:point-a", ParentID: id + ":article-1:clause-1", Kind: "point",
				Number: "a", Text: "Nhận tiền gửi;"},
			{ID: id + ":article-2", ParentID: id + ":chapter-1", Kind: "article", Number: "2",
				Text: "Luật này có hiệu lực từ ngày 01 tháng 01 năm 2021."},
		},
	}
}

func TestExtractableUnitIsTheClause(t *testing.T) {
	got := Extractable(sample("vn:law:2020:1-2020-qh14", "law"))
	want := []string{
		"vn:law:2020:1-2020-qh14:article-1:clause-1",
		"vn:law:2020:1-2020-qh14:article-2",
	}
	if len(got) != len(want) {
		t.Fatalf("units = %d, want %d: %+v", len(got), len(want), ids(got))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("unit %d = %s, want %s", i, got[i].ID, id)
		}
	}
}

func TestExtractableSkipsEmptyArticles(t *testing.T) {
	doc := &law.Document{ID: "d", Provisions: []law.Provision{
		{ID: "d:article-1", Kind: "article", Number: "1", Heading: "Chỉ có tiêu đề"},
	}}
	if got := Extractable(doc); len(got) != 0 {
		t.Errorf("units = %v, an article with no text has no rule to state", ids(got))
	}
}

func ids(ps []*law.Provision) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}

func TestPriority(t *testing.T) {
	if Priority("code") >= Priority("decree") {
		t.Error("codes must be extracted before decrees, everything cites them")
	}
	if Priority("Bộ luật") != Priority("code") {
		t.Error("the Vietnamese type name must rank the same as the English one")
	}
	if Priority("thông báo") != len(priorities) {
		t.Error("an unfamiliar type sorts last, it is not dropped")
	}
}

func TestQueueRecomputedFromDisk(t *testing.T) {
	s := &store.Store{Root: t.TempDir()}
	decree := sample("vn:law:2021:5-2021-nd-cp", "decree")
	code := sample("vn:law:2019:45-2019-qh14", "code")
	bad := sample("vn:law:2018:9-2018-qh14", "law")
	bad.Status = "quarantined"
	bad.Quarantine = "no article headings found"
	for _, doc := range []*law.Document{decree, code, bad} {
		if err := store.WriteJSON(filepath.Join(s.Docs(), law.FileName(doc.ID)), doc); err != nil {
			t.Fatal(err)
		}
	}

	tasks, err := Queue(s)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(tasks) != 4 {
		t.Fatalf("tasks = %+v, want two units each from the code and the decree", tasks)
	}
	if tasks[0].DocID != code.ID || tasks[2].DocID != decree.ID {
		t.Errorf("order = %s then %s, want the code first", tasks[0].DocID, tasks[2].DocID)
	}
	for _, task := range tasks {
		if task.DocID == bad.ID {
			t.Error("a quarantined document must not be queued, its structure is not trusted")
		}
	}

	// A job artifact on disk is the only record that a unit is done, so
	// writing one is what takes it out of the queue on the next run.
	done := tasks[0]
	if err := store.WriteJSON(filepath.Join(s.Norms(), law.FileName(done.ProvisionID)), map[string]string{}); err != nil {
		t.Fatal(err)
	}
	again, err := Queue(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 3 {
		t.Fatalf("tasks after one job = %d, want 3", len(again))
	}
	for _, task := range again {
		if task.ProvisionID == done.ProvisionID {
			t.Errorf("%s is still queued after its job was written", done.ProvisionID)
		}
	}

	report, err := Compute(s)
	if err != nil {
		t.Fatal(err)
	}
	if report.Extractable != 6 || report.Extracted != 1 {
		t.Errorf("report = %d extractable, %d extracted, want 6 and 1", report.Extractable, report.Extracted)
	}
	if report.Quarantined != 1 || report.Parsed != 2 {
		t.Errorf("report = %d parsed, %d quarantined", report.Parsed, report.Quarantined)
	}
	if report.Provisions["point"] != 3 {
		t.Errorf("points = %d, the report counts every provision even though none is a unit", report.Provisions["point"])
	}
}

// A document known only from the citation graph is a real node with no text.
// Counting it as parsed would report the corpus as more complete than it is,
// so content is counted separately from status.
func TestReportCountsContentApartFromStatus(t *testing.T) {
	s := &store.Store{Root: t.TempDir()}
	full := sample("vn:law:2019:45-2019-qh14", "bộ luật")
	pending := &law.Document{ID: "vn:law:2020:8-2020-tt-btc", DocType: "thông tư", Status: "metadata"}
	empty := &law.Document{ID: "vn:law:2020:9-2020-qd-ttg", DocType: "quyết định", Status: "parsed"}
	for _, doc := range []*law.Document{full, pending, empty} {
		if err := store.WriteJSON(filepath.Join(s.Docs(), law.FileName(doc.ID)), doc); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Compute(s)
	if err != nil {
		t.Fatal(err)
	}
	if report.Documents != 3 || report.Metadata != 1 || report.Parsed != 2 {
		t.Errorf("report = %d documents, %d metadata only, %d parsed", report.Documents, report.Metadata, report.Parsed)
	}
	if report.Content != 1 {
		t.Errorf("content = %d, want 1: only one of the three carries provision text", report.Content)
	}
	// The anchor stage has not run here, and a stage that has not run reports
	// as absent rather than as zero.
	if report.Anchoring != nil {
		t.Errorf("anchoring = %+v, want nil before the stage has run", report.Anchoring)
	}
	if !strings.Contains(report.String(), "anchoring  not run") {
		t.Errorf("report does not say the stage has not run:\n%s", report)
	}
}

func TestQueueOnAnEmptyStore(t *testing.T) {
	s := &store.Store{Root: t.TempDir()}
	tasks, err := Queue(s)
	if err != nil {
		t.Fatalf("Queue on a store with nothing in it: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("tasks = %+v", tasks)
	}
}
