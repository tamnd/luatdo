package graph

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
)

func fixture() ([]*law.Document, []cite.Link) {
	docs := []*law.Document{
		{
			ID: "vn:law:2019:45-2019-qh14", OfficialNumber: "45/2019/QH14",
			Title: "Bộ luật Lao động", DocType: "code", Status: "parsed",
			Provisions: []law.Provision{
				{ID: "vn:law:2019:45-2019-qh14:chapter-1", Kind: "chapter", Number: "1", Position: 1},
				{ID: "vn:law:2019:45-2019-qh14:article-1", ParentID: "vn:law:2019:45-2019-qh14:chapter-1", Kind: "article", Number: "1", Position: 2},
			},
		},
		{ID: "vn:law:2012:10-2012-qh13", OfficialNumber: "10/2012/QH13", Title: "Bộ luật Lao động 2012", DocType: "code", Status: "parsed"},
	}
	links := []cite.Link{
		{FromDoc: docs[0].ID, FromProvision: docs[0].Provisions[1].ID, ToNumber: "10/2012/QH13", ToDoc: docs[1].ID, Kind: "cites", Method: "pattern"},
		{FromDoc: docs[0].ID, ToNumber: "99/2099/NĐ-CP", Kind: "cites", Method: "pattern"},
	}
	return docs, links
}

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return rows
}

func TestExport(t *testing.T) {
	docs, links := fixture()
	dir := t.TempDir()
	if err := Export(dir, Input{Docs: docs, Links: links}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	documents := readCSV(t, filepath.Join(dir, "documents.csv"))
	if len(documents) != 3 {
		t.Errorf("documents.csv rows = %d, want header plus 2", len(documents))
	}
	if documents[0][0] != "id:ID" || documents[0][len(documents[0])-1] != ":LABEL" {
		t.Errorf("documents header = %v", documents[0])
	}

	provisions := readCSV(t, filepath.Join(dir, "provisions.csv"))
	if len(provisions) != 3 {
		t.Errorf("provisions.csv rows = %d, want header plus 2", len(provisions))
	}

	contains := readCSV(t, filepath.Join(dir, "contains.csv"))
	if len(contains) != 3 {
		t.Fatalf("contains.csv rows = %d, want header plus 2", len(contains))
	}
	if contains[1][0] != docs[0].ID {
		t.Errorf("chapter parent = %q, want the document", contains[1][0])
	}
	if contains[2][0] != docs[0].Provisions[0].ID {
		t.Errorf("article parent = %q, want the chapter", contains[2][0])
	}

	cites := readCSV(t, filepath.Join(dir, "cites.csv"))
	if len(cites) != 2 {
		t.Errorf("cites.csv rows = %d, unresolved links must be excluded", len(cites))
	}

	for _, name := range []string{"schema.cypher", "import.sh", "import.cmd"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}

func TestExportNorms(t *testing.T) {
	docs, links := fixture()
	statements := []norm.Record{{
		ID:          "vn:norm:abc123",
		DocID:       docs[0].ID,
		ProvisionID: docs[0].Provisions[1].ID,
		Statement: norm.Statement{
			Type:       "duty",
			Subject:    &norm.Ref{Text: "người sử dụng lao động", ClassID: "vn-legal:Employer"},
			Action:     norm.Ref{Text: "trả lương"},
			Conditions: []string{"khi đến kỳ hạn"},
			Exceptions: []string{"trừ trường hợp bất khả kháng"},
			Sanction:   "phạt tiền",
			Evidence:   norm.Evidence{Quote: "phải trả lương", Start: 10, End: 30},
			Confidence: 0.94,
		},
		Status:          "verified",
		Entailment:      &norm.Judgment{Verdict: norm.VerdictEntailed},
		Model:           "test-model",
		OntologyVersion: 1,
	}}
	dir := t.TempDir()
	if err := Export(dir, Input{Docs: docs, Links: links, Statements: statements}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	norms := readCSV(t, filepath.Join(dir, "norms.csv"))
	if len(norms) != 2 {
		t.Fatalf("norms.csv rows = %d, want header plus 1", len(norms))
	}
	row := norms[1]
	if row[0] != "vn:norm:abc123" || row[1] != "duty" || row[12] != "entailed" || row[13] != "test-model" {
		t.Errorf("norm row = %v, provenance must ride on the node", row)
	}

	details := readCSV(t, filepath.Join(dir, "norm_details.csv"))
	if len(details) != 4 {
		t.Errorf("norm_details.csv rows = %d, want condition, exception, and sanction", len(details))
	}

	hasNorm := readCSV(t, filepath.Join(dir, "has_norm.csv"))
	if len(hasNorm) != 2 || hasNorm[1][0] != docs[0].Provisions[1].ID {
		t.Errorf("has_norm rows = %v", hasNorm)
	}

	edges := readCSV(t, filepath.Join(dir, "norm_edges.csv"))
	types := map[string]int{}
	for _, e := range edges[1:] {
		types[e[2]]++
	}
	for _, want := range []string{"HAS_BEARER", "HAS_LEGAL_BASIS", "HAS_CONDITION", "HAS_EXCEPTION", "HAS_SANCTION"} {
		if types[want] != 1 {
			t.Errorf("edge %s count = %d, want 1", want, types[want])
		}
	}

	s := Summarize(Input{Docs: docs, Links: links, Statements: statements})
	if s.Norms != 1 {
		t.Errorf("Summarize norms = %d", s.Norms)
	}
}

func TestSummarize(t *testing.T) {
	docs, links := fixture()
	s := Summarize(Input{Docs: docs, Links: links})
	want := Summary{Documents: 2, Provisions: 2, Contains: 2, Cites: 1, Unresolved: 1}
	if s != want {
		t.Errorf("Summarize = %+v, want %+v", s, want)
	}
}
