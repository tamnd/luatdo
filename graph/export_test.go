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
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
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
			Type:         "duty",
			Bearer:       &norm.Ref{Text: "người sử dụng lao động", ClassID: "vn-legal:Employer", IsActor: true},
			Counterparty: &norm.Ref{Text: "người lao động", ClassID: "vn-legal:Employee", IsActor: true},
			Action:       norm.Ref{Text: "trả lương"},
			Conditions:   []norm.Clause{{Kind: norm.CondTemporal, Text: "khi đến kỳ hạn", Quote: "khi đến kỳ hạn"}},
			Exceptions:   []norm.Clause{{Kind: norm.ExcForce, Text: "bất khả kháng", Quote: "trừ trường hợp bất khả kháng"}},
			Deadline:     &norm.Deadline{Text: "trong thời hạn 05 ngày làm việc", Value: 5, Unit: norm.UnitDay, Calendar: norm.CalendarWorking},
			Sanction:     &norm.Sanction{Text: "phạt tiền", Quote: "bị phạt tiền", LegalBasis: "Điều 17 Nghị định số 12/2022/NĐ-CP"},
			Evidence:     norm.Evidence{Quote: "phải trả lương", Start: 10, End: 30},
			Confidence:   0.94,
		},
		Status:          "verified",
		Entailment:      &norm.Judgment{Verdict: norm.VerdictEntailed},
		Model:           "test-model",
		OntologyVersion: 1,
	}}
	dir := t.TempDir()
	// The registry is part of the input because the participant edges point into
	// it. Exporting the statements without it writes a HAS_BEARER row naming a
	// node the import does not contain, which stops the whole import.
	in := Input{Docs: docs, Links: links, Statements: statements, Registry: ontology.Seed()}
	if err := Export(dir, in); err != nil {
		t.Fatalf("Export: %v", err)
	}

	norms := readCSV(t, filepath.Join(dir, "norms.csv"))
	if len(norms) != 2 {
		t.Fatalf("norms.csv rows = %d, want header plus 1", len(norms))
	}
	// Columns are looked up by name rather than by position, because the schema
	// moves and a test that counts columns fails somewhere other than where the
	// mistake is.
	col := func(name string) string {
		for i, h := range norms[0] {
			if h == name || strings.HasPrefix(h, name+":") {
				return norms[1][i]
			}
		}
		t.Fatalf("norms.csv has no column %q, header is %v", name, norms[0])
		return ""
	}
	if norms[1][0] != "vn:norm:abc123" || col("norm_type") != "duty" {
		t.Errorf("norm row = %v", norms[1])
	}
	if col("verdict") != "entailed" || col("model") != "test-model" {
		t.Error("provenance must ride on the node")
	}
	if col("bearer") != "người sử dụng lao động" || col("counterparty") != "người lao động" {
		t.Errorf("bearer = %q, counterparty = %q, the two must not be folded together", col("bearer"), col("counterparty"))
	}
	if col("deadline_value") != "5" || col("deadline_calendar") != norm.CalendarWorking {
		t.Errorf("deadline projected as %q %q, question 12 needs both", col("deadline_value"), col("deadline_calendar"))
	}

	details := readCSV(t, filepath.Join(dir, "norm_details.csv"))
	if len(details) != 4 {
		t.Fatalf("norm_details.csv rows = %d, want condition, exception, and sanction", len(details))
	}
	for _, d := range details[1:] {
		if d[3] == "" {
			t.Errorf("detail %v carries no quote, so nobody can check it against the provision", d)
		}
		if d[5] == "Sanction" && d[4] == "" {
			t.Error("a sanction node without its legal basis is a summary, not a fact")
		}
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
	for _, want := range []string{"HAS_BEARER", "HAS_COUNTERPARTY", "HAS_LEGAL_BASIS", "HAS_CONDITION", "HAS_EXCEPTION", "HAS_SANCTION"} {
		if types[want] != 1 {
			t.Errorf("edge %s count = %d, want 1", want, types[want])
		}
	}

	s := Summarize(in)
	if s.Norms != 1 {
		t.Errorf("Summarize norms = %d", s.Norms)
	}
	// The count is what the drift check compares a live database against, so it
	// has to be every norm edge row the export wrote, across both files.
	if written := len(edges) - 1 + len(hasNorm) - 1; s.NormEdges != written {
		t.Errorf("Summarize norm edges = %d, the two files hold %d rows", s.NormEdges, written)
	}
	if s.NormEdges != 7 {
		t.Errorf("Summarize norm edges = %d, want the two participants, the two provision edges, and the three details", s.NormEdges)
	}

	// A participant placed in a class this export does not write is dropped
	// rather than pointed at nothing. It costs that one edge, where naming a
	// missing node costs the import.
	statements[0].Statement.Bearer.ClassID = "vn-legal:NoSuchClass"
	other := t.TempDir()
	stale := Input{Docs: docs, Links: links, Statements: statements, Registry: ontology.Seed()}
	if err := Export(other, stale); err != nil {
		t.Fatalf("Export: %v", err)
	}
	for _, e := range readCSV(t, filepath.Join(other, "norm_edges.csv"))[1:] {
		if e[2] == "HAS_BEARER" {
			t.Errorf("edge %v points at a class the registry does not hold", e)
		}
	}
	if got := Summarize(stale).NormEdges; got != 6 {
		t.Errorf("Summarize norm edges = %d after dropping one, want 6", got)
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

// conceptLayer is one label used by two instruments for two different things,
// one of which a reviewer merged into a corpus wide concept and the other of
// which the same reviewer recorded as a difference. That pair is the whole
// point of the layer, so the projection is tested on it rather than on a
// single tidy term.
func conceptLayer() *concept.Layer {
	const labour = "vn:law:2019:45-2019-qh14"
	const decree = "vn:law:2012:10-2012-qh13"
	return &concept.Layer{
		TermUses: []concept.TermUse{
			{
				ID: "vn:term:" + labour + ":nguoi-lao-dong", LabelVI: "Người lao động",
				ScopeID: labour, DocID: labour, Kind: concept.KindActor,
				DefinitionVI: "người làm việc cho người sử dụng lao động",
				Genus:        "người làm việc",
				Differentiae: []concept.Differentia{{Text: "theo thỏa thuận; được trả lương"}},
				Aliases:      []string{"NLĐ"},
				Origin:       concept.OriginDefined, DefinedBy: labour + ":article-1",
				Quote: "Người lao động", CharStart: 0, CharEnd: 14, Confidence: 0.9,
				ReferencedTerms: []string{"Người sử dụng lao động", "Hợp đồng lao động"},
			},
			{
				ID: "vn:term:" + labour + ":nguoi-su-dung-lao-dong", LabelVI: "Người sử dụng lao động",
				ScopeID: labour, DocID: labour, Kind: concept.KindActor,
				Origin: concept.OriginDefined, DefinedBy: labour + ":article-1",
				Quote: "Người sử dụng lao động", Confidence: 0.9,
			},
			{
				ID: "vn:term:" + decree + ":nguoi-lao-dong", LabelVI: "Người lao động",
				ScopeID: decree, DocID: decree, Kind: concept.KindActor,
				Origin: concept.OriginDefined, DefinedBy: decree + ":article-3",
				Quote: "Người lao động", Confidence: 0.8,
			},
		},
		Concepts: []concept.Concept{
			{ID: "vn:concept:nguoi-lao-dong", LabelVI: "Người lao động", Kind: concept.KindActor},
		},
		Memberships: []concept.Membership{{
			TermUseID: "vn:term:" + labour + ":nguoi-lao-dong", ConceptID: "vn:concept:nguoi-lao-dong",
			Relation: concept.RelationSame, DecidedBy: "tamnd", DecidedAt: "2026-08-01T00:00:00Z",
			Rationale: "định nghĩa gốc của Bộ luật Lao động",
		}},
		Differences: []concept.Difference{{
			FromID: "vn:term:" + decree + ":nguoi-lao-dong", ToID: "vn:term:" + labour + ":nguoi-lao-dong",
			DecidedBy: "tamnd", DecidedAt: "2026-08-01T00:00:00Z",
			Rationale: "phạm vi hẹp hơn",
			Basis:     []string{"độ tuổi", "hình thức hợp đồng"},
		}},
	}
}

func TestTheProjectionKeepsATermUseApartFromTheConceptItWasMergedInto(t *testing.T) {
	docs, links := fixture()
	dir := t.TempDir()
	in := Input{Docs: docs, Links: links, Layer: conceptLayer()}
	if err := Export(dir, in); err != nil {
		t.Fatalf("Export: %v", err)
	}

	uses := readCSV(t, filepath.Join(dir, "term_uses.csv"))
	if len(uses) != 4 {
		t.Fatalf("term_uses.csv rows = %d, want header plus 3", len(uses))
	}
	if label := uses[1][len(uses[1])-1]; label != "TermUse" {
		t.Errorf("term use label = %q", label)
	}
	// Two instruments, one label, two nodes. A projection that keyed on the
	// label would have written one, and the difference below would have nothing
	// to hang off.
	if uses[1][0] == uses[3][0] {
		t.Errorf("two instruments got one identifier: %s", uses[1][0])
	}

	concepts := readCSV(t, filepath.Join(dir, "merged_concepts.csv"))
	if len(concepts) != 2 {
		t.Fatalf("merged_concepts.csv rows = %d, want header plus 1", len(concepts))
	}
	if label := concepts[1][len(concepts[1])-1]; label != "Concept" {
		t.Errorf("concept label = %q", label)
	}

	// The decision rides on the edge, because the edge is the merge.
	memberships := readCSV(t, filepath.Join(dir, "instance_of.csv"))
	if len(memberships) != 2 {
		t.Fatalf("instance_of.csv rows = %d", len(memberships))
	}
	row := memberships[1]
	if row[3] != "tamnd" || row[5] == "" {
		t.Errorf("instance of = %v, want the decider and the reason on the edge", row)
	}

	differences := readCSV(t, filepath.Join(dir, "differs_from.csv"))
	if len(differences) != 2 {
		t.Fatalf("differs_from.csv rows = %d", len(differences))
	}
	if basis := differences[1][5]; basis != "độ tuổi|hình thức hợp đồng" {
		t.Errorf("basis = %q, want the two features packed on the pipe", basis)
	}
}

// A differentia written by a Vietnamese drafter is full of semicolons, and the
// importer's default array separator is the semicolon, so one feature would
// arrive as two. The export picks a separator that legal prose does not use and
// tells neo4j-admin about it in the same breath.
func TestAnArrayColumnSurvivesASemicolonInLegalProse(t *testing.T) {
	docs, links := fixture()
	dir := t.TempDir()
	if err := Export(dir, Input{Docs: docs, Links: links, Layer: conceptLayer()}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	uses := readCSV(t, filepath.Join(dir, "term_uses.csv"))
	header, row := uses[0], uses[1]
	at := slices.Index(header, "differentiae:string[]")
	if at < 0 {
		t.Fatalf("no differentiae column: %v", header)
	}
	if row[at] != "theo thỏa thuận; được trả lương" {
		t.Errorf("differentiae = %q, want the one feature whole", row[at])
	}
	script, err := os.ReadFile(filepath.Join(dir, "import.sh"))
	if err != nil {
		t.Fatalf("read import.sh: %v", err)
	}
	if !strings.Contains(string(script), `--array-delimiter="|"`) {
		t.Error("the import script does not pass the separator the export used")
	}
	for _, name := range []string{"term_uses.csv", "merged_concepts.csv", "instance_of.csv", "differs_from.csv", "term_use_edges.csv"} {
		if !strings.Contains(string(script), name) {
			t.Errorf("import.sh does not load %s", name)
		}
	}
}

// neo4j-admin refuses an entire import over one relationship row pointing at an
// identifier no node file declares, so every edge out of a term use is checked
// against the nodes this export actually writes.
func TestNoEdgeLeavesATermUseForANodeTheExportNeverWrote(t *testing.T) {
	docs, links := fixture()
	layer := conceptLayer()
	// A term recovered from a clause the corpus no longer carries, and a
	// reference to a term nobody ever defined. Both are ordinary and neither may
	// reach the import.
	layer.TermUses = append(layer.TermUses, concept.TermUse{
		ID: "vn:term:vn:law:1999:gone:tu-ngu-cu", LabelVI: "Từ ngữ cũ",
		ScopeID: "vn:law:1999:gone", DocID: "vn:law:1999:gone", Kind: concept.KindOther,
		Origin: concept.OriginRecovered, DefinedBy: "vn:law:1999:gone:article-2",
		Quote: "Từ ngữ cũ", Confidence: 0.5,
	})

	dir := t.TempDir()
	if err := Export(dir, Input{Docs: docs, Links: links, Layer: layer}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	known := map[string]bool{}
	for _, name := range []string{"documents.csv", "components.csv", "term_uses.csv", "merged_concepts.csv"} {
		rows := readCSV(t, filepath.Join(dir, name))
		for _, r := range rows[1:] {
			known[r[0]] = true
		}
	}
	for _, name := range []string{"instance_of.csv", "differs_from.csv", "term_use_edges.csv"} {
		for _, r := range readCSV(t, filepath.Join(dir, name))[1:] {
			if !known[r[0]] || !known[r[1]] {
				t.Errorf("%s points %s at %s and one of them is not a node", name, r[0], r[1])
			}
		}
	}

	edges := readCSV(t, filepath.Join(dir, "term_use_edges.csv"))
	var kinds []string
	for _, r := range edges[1:] {
		kinds = append(kinds, r[2])
	}
	// One DEFINES_TERM and one IN_SCOPE for each of the three term uses that
	// live in documents this export holds, plus the single referenced term that
	// resolves inside its own instrument. The second referenced term, hop dong
	// lao dong, is never defined and so is never linked.
	if got := strings.Count(strings.Join(kinds, " "), "REFERS_TO"); got != 1 {
		t.Errorf("REFERS_TO edges = %d, want only the reference that resolves", got)
	}
	if s := Summarize(Input{Docs: docs, Links: links, Layer: layer}); s.TermUses != 4 || s.MergedConcepts != 1 || s.TermUseEdges != len(edges)-1 {
		t.Errorf("summary = %+v, want the counts the files carry", s)
	}
}

// A document that numbers two things the same way is ordinary in this corpus.
// A decision promulgates a regulation attached to it and both print Điều 1, and
// an amending decree restates clause 1 of article 1 of every instrument it
// touches. The identifier is built from the numbering, so both readings land on
// the same one.
func TestADocumentThatUsesOneIdentifierTwiceIsWrittenOnce(t *testing.T) {
	doc := &law.Document{
		ID: "vn:law:1997:402-1997-qd-nhnn1", OfficialNumber: "402/1997/QĐ-NHNN1",
		Title: "Quyết định ban hành Thể lệ tín dụng", DocType: "decision", Status: "parsed",
		Provisions: []law.Provision{
			{ID: "vn:law:1997:402-1997-qd-nhnn1:article-1", Kind: "article", Number: "1",
				Text: "Ban hành kèm theo Quyết định này Thể lệ tín dụng.", TextHash: "aaa1", Position: 1},
			// The attached regulation, numbered from one again, and hanging under a
			// different parent than the article it collides with.
			{ID: "vn:law:1997:402-1997-qd-nhnn1:chapter-1", Kind: "chapter", Number: "1", Heading: "Quy định chung", Position: 2},
			{ID: "vn:law:1997:402-1997-qd-nhnn1:article-1", ParentID: "vn:law:1997:402-1997-qd-nhnn1:chapter-1",
				Kind: "article", Number: "1", Text: "Đối tượng áp dụng của Thể lệ này là các Ngân hàng thương mại.",
				TextHash: "bbb2", Position: 3},
		},
	}
	in := Input{Docs: []*law.Document{doc}}
	dir := t.TempDir()
	if err := Export(dir, in); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// neo4j-admin refuses the whole import over a repeated id:ID rather than
	// keeping one of them, so this is the difference between a graph and an
	// error message.
	seen := map[string]bool{}
	components := readCSV(t, filepath.Join(dir, "components.csv"))
	for _, r := range components[1:] {
		if seen[r[0]] {
			t.Errorf("components.csv declares %s twice, which stops the import", r[0])
		}
		seen[r[0]] = true
	}
	if len(components)-1 != 2 {
		t.Errorf("components.csv rows = %d, want the chapter and one article", len(components)-1)
	}

	// The edge follows the component that survived, so the second reading's
	// container does not point at a node the node file left out.
	for _, r := range readCSV(t, filepath.Join(dir, "contains.csv"))[1:] {
		if !seen[r[1]] {
			t.Errorf("contains.csv puts %s in %s and no such component was written", r[1], r[0])
		}
	}

	// Both texts survive as versions, because the version identifier carries the
	// text hash and two readings that say different things are two readings.
	if versions := readCSV(t, filepath.Join(dir, "text_versions.csv")); len(versions)-1 != 2 {
		t.Errorf("text_versions.csv rows = %d, want both readings kept", len(versions)-1)
	}

	s := Summarize(in)
	if s.Components != 2 || s.Contains != 2 {
		t.Errorf("summary components %d contains %d, want 2 and 2", s.Components, s.Contains)
	}
	// The fold is reported rather than absorbed. A count that quietly drops a
	// provision reads exactly like a corpus that never had it.
	if s.FoldedComponents != 1 || s.FoldedVersions != 0 {
		t.Errorf("folded %d components and %d versions, want 1 and 0", s.FoldedComponents, s.FoldedVersions)
	}
	if !strings.Contains(s.String(), "folded 1 components and 0 versions") {
		t.Errorf("summary reads %q and says nothing about the fold", s.String())
	}
	// A folded component is not drift. The database holds what the projection
	// wrote, and the projection wrote two.
	if lines := Drift(s, liveCounts(s)); len(lines) != 0 {
		t.Errorf("drift = %v, want none, the folded component was never written", lines)
	}
}
