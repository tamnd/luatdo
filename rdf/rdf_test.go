package rdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dump writes a small Neo4j style dump by hand.
//
// The fixtures are written out rather than produced by running the exporter,
// because these tests are about the contract between the two: the header
// conventions are what this package reads, and a test that generated its input
// with the same code that generates the real input would agree with itself
// about a convention neither of them followed.
func dump(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func export(t *testing.T, files map[string]string) (string, Summary) {
	t.Helper()
	out := t.TempDir()
	s, err := Export(dump(t, files), out)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(out, "graph.nt"))
	if err != nil {
		t.Fatalf("read graph.nt: %v", err)
	}
	return string(b), s
}

func has(t *testing.T, text, triple string) {
	t.Helper()
	if !strings.Contains(text, triple+" .\n") {
		t.Errorf("missing triple:\n  %s", triple)
	}
}

func hasNot(t *testing.T, text, substring string) {
	t.Helper()
	if strings.Contains(text, substring) {
		t.Errorf("unwanted output contains %q", substring)
	}
}

const documents = "id:ID,official_number,title,title_en,effective_from,source_url,status,:LABEL\n" +
	"vn:law:2019:45,45/2019/QH14,Bộ luật Lao động,Labour Code,2021-01-01,https://example.test/45,active,Document\n"

func TestANodeBecomesATypedSubjectWithItsPropertiesAsTriples(t *testing.T) {
	text, s := export(t, map[string]string{"documents.csv": documents})

	doc := "<https://luatdo.dev/id/vn:law:2019:45>"
	has(t, text, doc+" <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://luatdo.dev/ns#Document>")
	// Vietnamese prose carries a language tag and the English title carries its
	// own, so a consumer asking for the title in one language gets one answer.
	has(t, text, doc+` <http://purl.org/dc/terms/title> "Bộ luật Lao động"@vi`)
	has(t, text, doc+` <http://purl.org/dc/terms/title> "Labour Code"@en`)
	has(t, text, doc+` <http://purl.org/dc/terms/identifier> "45/2019/QH14"`)
	// A date is typed, so a consumer can compare it. A status is a keyword the
	// pipeline chose and carries no language tag, because it is not prose.
	has(t, text, doc+` <https://luatdo.dev/ns#effectiveFrom> "2021-01-01"^^<http://www.w3.org/2001/XMLSchema#date>`)
	has(t, text, doc+` <https://luatdo.dev/ns#status> "active"`)
	// A URL is something to follow, so it is an IRI and not a string.
	has(t, text, doc+" <http://www.w3.org/2000/01/rdf-schema#seeAlso> <https://example.test/45>")

	if s.Nodes != 1 || s.Edges != 0 {
		t.Errorf("summary = %+v, want one node and no edges", s)
	}
}

// A property graph has a column for every property any node of a label carries,
// so most rows have empty cells. Writing those as empty strings would say this
// document's English title is "" rather than that nobody has translated it.
func TestAnEmptyCellIsNotAFact(t *testing.T) {
	text, _ := export(t, map[string]string{
		"documents.csv": "id:ID,title,title_en,:LABEL\nvn:law:2019:45,Bộ luật Lao động,,Document\n",
	})
	hasNot(t, text, `""`)
	if strings.Count(text, "dc/terms/title") != 1 {
		t.Error("the untranslated title was written as a fact")
	}
}

func TestAMultiLabelNodeGetsEveryTypeIncludingTheAlignedOnes(t *testing.T) {
	text, _ := export(t, map[string]string{
		"components.csv": "id:ID,kind,:LABEL\nvn:law:2019:45:article-1,article,Component|Provision\n",
		"subjects.csv":   "id:ID,label_vi,:LABEL\nlao-dong,Lao động,Subject\n",
	})
	c := "<https://luatdo.dev/id/vn:law:2019:45:article-1>"
	rdfType := "<http://www.w3.org/1999/02/22-rdf-syntax-ns#type>"
	has(t, text, c+" "+rdfType+" <https://luatdo.dev/ns#Component>")
	has(t, text, c+" "+rdfType+" <https://luatdo.dev/ns#Provision>")

	// A subject is a SKOS concept as well as one of ours, and its label is a
	// skos:prefLabel, because that is what the subject vocabulary is.
	subj := "<https://luatdo.dev/id/lao-dong>"
	has(t, text, subj+" "+rdfType+" <https://luatdo.dev/ns#Subject>")
	has(t, text, subj+" "+rdfType+" <http://www.w3.org/2004/02/skos/core#Concept>")
	has(t, text, subj+` <http://www.w3.org/2004/02/skos/core#prefLabel> "Lao động"@vi`)
}

func TestAnEdgeBecomesAPredicateAndAnAlignedOneKeepsBoth(t *testing.T) {
	text, s := export(t, map[string]string{
		"documents.csv":       documents,
		"subjects.csv":        "id:ID,label_vi,:LABEL\nlao-dong,Lao động,Subject\nviec-lam,Việc làm,Subject\n",
		"subject_parents.csv": ":START_ID,:END_ID,:TYPE\nviec-lam,lao-dong,BROADER\n",
		"about_subject.csv":   ":START_ID,:END_ID,confidence:float,method,:TYPE\nvn:law:2019:45,lao-dong,0.9,classifier,ABOUT_SUBJECT\n",
	})
	child, parent := "<https://luatdo.dev/id/viec-lam>", "<https://luatdo.dev/id/lao-dong>"
	has(t, text, child+" <http://www.w3.org/2004/02/skos/core#broader> "+parent)
	has(t, text, child+" <https://luatdo.dev/ns#broader> "+parent)
	has(t, text, "<https://luatdo.dev/id/vn:law:2019:45> <http://purl.org/dc/terms/subject> "+parent)
	if s.Edges != 2 {
		t.Errorf("edges = %d, want 2", s.Edges)
	}
}

// An edge here can carry six properties and a triple has three slots, so the
// properties go on a reified statement. The plain triple stays, because making
// the common query need reification to answer would be a bad trade.
func TestAnEdgeWithPropertiesIsReifiedAndTheTripleStays(t *testing.T) {
	text, s := export(t, map[string]string{
		"documents.csv": "id:ID,:LABEL\nvn:law:2019:45,Document\nvn:law:2020:145,Document\n",
		"cites.csv":     ":START_ID,:END_ID,method,snippet,:TYPE\nvn:law:2020:145,vn:law:2019:45,number,theo Bộ luật Lao động,CITES\n",
	})
	from, to := "<https://luatdo.dev/id/vn:law:2020:145>", "<https://luatdo.dev/id/vn:law:2019:45>"
	has(t, text, from+" <https://luatdo.dev/ns#cites> "+to)
	if s.Reified != 1 {
		t.Fatalf("reified = %d, want 1", s.Reified)
	}
	// The statement names the triple it is about and then carries the evidence.
	for _, want := range []string{
		"<http://www.w3.org/1999/02/22-rdf-syntax-ns#Statement>",
		"<http://www.w3.org/1999/02/22-rdf-syntax-ns#subject> " + from,
		"<http://www.w3.org/1999/02/22-rdf-syntax-ns#object> " + to,
		`<https://luatdo.dev/ns#method> "number"`,
		`<https://luatdo.dev/ns#snippet> "theo Bộ luật Lao động"@vi`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the reified statement is missing %s", want)
		}
	}
	// An edge with no properties is not reified, or every CONTAINS edge in the
	// corpus would carry four triples saying nothing.
	text, s = export(t, map[string]string{
		"documents.csv":  "id:ID,:LABEL\nvn:law:2019:45,Document\n",
		"components.csv": "id:ID,:LABEL\nvn:law:2019:45:article-1,Component\n",
		"contains.csv":   ":START_ID,:END_ID,:TYPE\nvn:law:2019:45,vn:law:2019:45:article-1,CONTAINS\n",
	})
	if s.Reified != 0 {
		t.Errorf("reified = %d, want 0 for an edge with no properties", s.Reified)
	}
	hasNot(t, text, "Statement")
}

// The importer refuses an entire import over one relationship row naming a node
// it does not have. RDF accepts it and leaves a subject nothing describes, which
// is worse, because nothing says so.
func TestAnEdgeIntoAMissingNodeIsDroppedAndCounted(t *testing.T) {
	text, s := export(t, map[string]string{
		"documents.csv": "id:ID,:LABEL\nvn:law:2019:45,Document\n",
		"cites.csv":     ":START_ID,:END_ID,:TYPE\nvn:law:2019:45,vn:law:1994:35,CITES\n",
	})
	if s.Dangling != 1 || s.Edges != 0 {
		t.Errorf("summary = %+v, want one dangling edge and none written", s)
	}
	hasNot(t, text, "vn:law:1994:35")
}

func TestArrayValuesBecomeOneTripleEach(t *testing.T) {
	text, _ := export(t, map[string]string{
		"differs_from.csv": ":START_ID,:END_ID,basis:string[],:TYPE\na,b,scope|effect,DIFFERS_FROM\n",
		"nodes.csv":        "id:ID,:LABEL\na,TermUse\nb,TermUse\n",
	})
	has(t, text, `<https://luatdo.dev/ns#basis> "scope"`)
	has(t, text, `<https://luatdo.dev/ns#basis> "effect"`)
	hasNot(t, text, `"scope|effect"`)
}

// Vietnamese legal text is a paragraph and a paragraph has newlines in it. It
// also has quotation marks, and the corpus has backslashes in a few places.
func TestLiteralsAreEscapedSoOneTripleStaysOneLine(t *testing.T) {
	text, _ := export(t, map[string]string{
		"text_versions.csv": "id:ID,text,:LABEL\nv1,\"Điều 1.\nNgười \"\"lao động\"\" \\ hết\",TextVersion\n",
	})
	has(t, text, `<https://luatdo.dev/id/v1> <https://luatdo.dev/ns#text> "Điều 1.\nNgười \"lao động\" \\ hết"@vi`)
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if !strings.HasSuffix(line, " .") {
			t.Errorf("a line is not a complete triple: %q", line)
		}
	}
}

// Two exports of one dump have to produce the same bytes, or nobody can diff
// them, and a projection nobody can diff is a projection nobody checks.
func TestTheSameDumpProducesTheSameBytes(t *testing.T) {
	files := map[string]string{
		"documents.csv":     documents,
		"subjects.csv":      "id:ID,label_vi,:LABEL\nlao-dong,Lao động,Subject\n",
		"about_subject.csv": ":START_ID,:END_ID,confidence:float,method,:TYPE\nvn:law:2019:45,lao-dong,0.9,classifier,ABOUT_SUBJECT\n",
	}
	in := dump(t, files)
	var out [2]string
	for i := range out {
		dir := t.TempDir()
		if _, err := Export(in, dir); err != nil {
			t.Fatalf("Export: %v", err)
		}
		b, err := os.ReadFile(filepath.Join(dir, "graph.nt"))
		if err != nil {
			t.Fatal(err)
		}
		out[i] = string(b)
	}
	if out[0] != out[1] {
		t.Error("two exports of one dump differ")
	}
}

func TestTheVocabularyShipsBesideTheData(t *testing.T) {
	out := t.TempDir()
	if _, err := Export(dump(t, map[string]string{"documents.csv": documents}), out); err != nil {
		t.Fatalf("Export: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(out, "vocabulary.ttl"))
	if err != nil {
		t.Fatalf("read vocabulary.ttl: %v", err)
	}
	text := string(b)
	// The ELI alignment is a reading of somebody else's specification, so it
	// lives here and not in the data, and it is stated as a subclass rather
	// than as an equivalence.
	if !strings.Contains(text, "rdfs:subClassOf eli:LegalResource") {
		t.Error("the vocabulary does not state the ELI alignment")
	}
	if strings.Contains(text, "owl:equivalentClass") {
		t.Error("the vocabulary claims an equivalence, which is more than was checked")
	}
	// Every class the writer can emit a type for should be described here. A
	// term with no definition is a term nobody can use.
	for _, class := range []string{"luatdo:Document", "luatdo:Component", "luatdo:Norm", "luatdo:Event", "luatdo:TemporalVersion"} {
		if !strings.Contains(text, class+" a owl:Class") {
			t.Errorf("the vocabulary does not define %s", class)
		}
	}
}

func TestExportSaysWhereToLookWhenThereIsNoDump(t *testing.T) {
	_, err := Export(filepath.Join(t.TempDir(), "nothing"), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "luatdo export neo4j") {
		t.Errorf("error = %v, want one naming the command that writes the dump", err)
	}
}
