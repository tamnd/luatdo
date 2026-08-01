package graph

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/subject"
)

func fixture() ([]*law.Document, []cite.Link) {
	docs := []*law.Document{
		{
			ID: "vn:law:2019:45-2019-qh14", OfficialNumber: "45/2019/QH14",
			Title: "Bộ luật Lao động", DocType: "code", Status: "parsed",
			EffectiveFrom: "2021-01-01",
			Provisions: []law.Provision{
				{ID: "vn:law:2019:45-2019-qh14:chapter-1", Kind: "chapter", Number: "1", Heading: "Những quy định chung", Position: 1},
				{ID: "vn:law:2019:45-2019-qh14:article-1", ParentID: "vn:law:2019:45-2019-qh14:chapter-1", Kind: "article", Number: "1",
					Heading: "Phạm vi điều chỉnh", Text: "Bộ luật Lao động quy định tiêu chuẩn lao động.",
					TextHash: "9f2c4a1b7d3e5608aa11", Position: 2},
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

	components := readCSV(t, filepath.Join(dir, "components.csv"))
	if len(components) != 3 {
		t.Errorf("components.csv rows = %d, want header plus 2", len(components))
	}
	if label := components[1][len(components[1])-1]; label != "Component;Provision" {
		t.Errorf("component label = %q, want the alias alongside the real label", label)
	}

	// The chapter has a heading and no text of its own, so the split gives it a
	// component and no version. One version for two components is the point.
	versions := readCSV(t, filepath.Join(dir, "text_versions.csv"))
	if len(versions) != 2 {
		t.Errorf("text_versions.csv rows = %d, want header plus the one provision that says something", len(versions))
	}
	hasVersion := readCSV(t, filepath.Join(dir, "has_version.csv"))
	if len(hasVersion) != 2 {
		t.Fatalf("has_version.csv rows = %d, want header plus 1", len(hasVersion))
	}
	if hasVersion[1][0] != docs[0].Provisions[1].ID || hasVersion[1][1] != versions[1][0] {
		t.Errorf("has_version row = %v, want the article pointing at its wording", hasVersion[1])
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

// oldProvisionRow is the row provisions.csv carried before the split, written
// out here rather than described, because the exit criterion for the split is
// that every one of these rows can still be read back out of the projection.
// The header was id:ID, kind, number, heading, text, position:int, :LABEL.
func oldProvisionRow(p *law.Provision) []string {
	return []string{p.ID, p.Kind, p.Number, p.Heading, p.Text, strconv.Itoa(p.Position), "Provision"}
}

func TestTheSplitLosesNothingTheOldExportCarried(t *testing.T) {
	docs, links := fixture()
	// A provision that says nothing, a provision that says something, and a
	// provision with text and no heading. The first is where a split most easily
	// goes wrong, by inventing an empty version and reporting a component that
	// said the empty string rather than one that said nothing.
	docs[1].EffectiveFrom = "2013-01-01"
	docs[1].Provisions = []law.Provision{
		{ID: "vn:law:2012:10-2012-qh13:chapter-1", Kind: "chapter", Number: "1", Heading: "Chương mở đầu", Position: 1},
		{ID: "vn:law:2012:10-2012-qh13:article-3", ParentID: "vn:law:2012:10-2012-qh13:chapter-1", Kind: "article",
			Number: "3", Text: "Trong Bộ luật này, các từ ngữ dưới đây được hiểu như sau:", TextHash: "aa01bb02cc03dd04", Position: 2},
	}

	dir := t.TempDir()
	if err := Export(dir, Input{Docs: docs, Links: links}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Rebuild the old rows out of the new files, the way a reader of the
	// projection would: a component, the version it has, and the text on it.
	versionText := map[string]string{}
	for _, row := range readCSV(t, filepath.Join(dir, "text_versions.csv"))[1:] {
		versionText[row[0]] = row[1]
	}
	textOf := map[string]string{}
	for _, row := range readCSV(t, filepath.Join(dir, "has_version.csv"))[1:] {
		if _, already := textOf[row[0]]; already {
			t.Fatalf("component %s has two current versions, and the old row had one text", row[0])
		}
		textOf[row[0]] = versionText[row[1]]
	}
	rebuilt := map[string][]string{}
	for _, row := range readCSV(t, filepath.Join(dir, "components.csv"))[1:] {
		labels := strings.Split(row[6], ";")
		if !slices.Contains(labels, "Provision") {
			t.Errorf("component %s carries %v, and a query for the old label would miss it", row[0], labels)
		}
		rebuilt[row[0]] = []string{row[0], row[1], row[2], row[3], textOf[row[0]], row[4], "Provision"}
	}

	rows := 0
	for _, d := range docs {
		for i := range d.Provisions {
			p := &d.Provisions[i]
			want := oldProvisionRow(p)
			got, ok := rebuilt[p.ID]
			if !ok {
				t.Errorf("provision %s is not in the split export at all", p.ID)
				continue
			}
			if !slices.Equal(got, want) {
				t.Errorf("provision %s\n got  %q\n want %q", p.ID, got, want)
			}
			rows++
		}
	}
	if rows != len(rebuilt) {
		t.Errorf("the split export has %d components and the corpus has %d provisions", len(rebuilt), rows)
	}
}

func TestExportSubjects(t *testing.T) {
	docs, links := fixture()
	vocabulary := subject.MustLoad()
	records := []subject.Record{
		{DocID: docs[0].ID, DocType: "code", Subjects: []subject.Assignment{
			{SubjectID: "lao-dong", Confidence: 0.6, Method: subject.MethodParent},
			{SubjectID: "lao-dong/hop-dong-lao-dong", Confidence: 0.6, Method: subject.MethodLexical},
		}},
		// A record filed under a subject the current vocabulary does not have,
		// which is what an assignments file written before a vocabulary edit looks
		// like. neo4j-admin rejects the entire import over one dangling edge, so
		// the row is dropped rather than allowed to take the import down.
		{DocID: docs[1].ID, DocType: "code", Subjects: []subject.Assignment{
			{SubjectID: "lao-dong/nghe-ca", Confidence: 0.5, Method: subject.MethodLexical},
		}},
	}

	dir := t.TempDir()
	in := Input{Docs: docs, Links: links, Vocabulary: vocabulary, Subjects: records}
	if err := Export(dir, in); err != nil {
		t.Fatalf("Export: %v", err)
	}

	subjects := readCSV(t, filepath.Join(dir, "subjects.csv"))
	if len(subjects)-1 != len(vocabulary.Subjects) {
		t.Errorf("subjects.csv has %d rows for a vocabulary of %d", len(subjects)-1, len(vocabulary.Subjects))
	}
	parents := readCSV(t, filepath.Join(dir, "subject_parents.csv"))
	if len(parents)-1 != len(vocabulary.Subjects)-len(vocabulary.Domains()) {
		t.Errorf("subject_parents.csv has %d rows, want one per subdomain", len(parents)-1)
	}

	about := readCSV(t, filepath.Join(dir, "about_subject.csv"))
	if len(about) != 3 {
		t.Fatalf("about_subject.csv rows = %v, want header plus the two edges the vocabulary can hold", about)
	}
	if about[1][0] != docs[0].ID || about[1][1] != "lao-dong" || about[1][2] != "0.60" || about[1][3] != subject.MethodParent {
		t.Errorf("about row = %v, the method belongs on the edge so a reader knows a domain was carried up", about[1])
	}

	s := Summarize(in)
	if s.Subjects != len(vocabulary.Subjects) || s.AboutSubject != 2 {
		t.Errorf("Summarize subjects = %d, about = %d", s.Subjects, s.AboutSubject)
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
	want := Summary{Documents: 2, Components: 2, TextVersions: 1, Contains: 2, Cites: 1, Unresolved: 1}
	if s != want {
		t.Errorf("Summarize = %+v, want %+v", s, want)
	}
}
