package graph

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/campaign"
)

// vocabulary reads back the labels and relationship types an export of the
// fixture actually writes.
//
// It is read out of the CSV files rather than declared in Go, because a list
// declared in Go is a second copy of the truth and the queries have to be
// checked against the first one. A rename in the exporter that this test did not
// catch would be a rename that silently broke every query mentioning it.
func vocabulary(t *testing.T, dir string) (labels, types map[string]bool) {
	t.Helper()
	labels, types = map[string]bool{}, map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".csv" {
			continue
		}
		rows := readCSV(t, filepath.Join(dir, e.Name()))
		if len(rows) < 2 {
			continue
		}
		labelAt := slices.Index(rows[0], ":LABEL")
		typeAt := slices.Index(rows[0], ":TYPE")
		for _, row := range rows[1:] {
			if labelAt >= 0 && labelAt < len(row) {
				for _, label := range strings.Split(row[labelAt], arrayDelimiter) {
					labels[label] = true
				}
			}
			if typeAt >= 0 && typeAt < len(row) {
				types[row[typeAt]] = true
			}
		}
	}
	return labels, types
}

// token finds every name a query uses after a colon or a pipe, which is every
// label and every relationship type it depends on. Comment lines go first, so
// prose about a relationship type is not read as a use of one.
var token = regexp.MustCompile(`[:|]([A-Za-z_][A-Za-z0-9_]*)`)

func queryTokens(text string) []string {
	var body []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		body = append(body, line)
	}
	var out []string
	for _, m := range token.FindAllStringSubmatch(strings.Join(body, "\n"), -1) {
		out = append(out, m[1])
	}
	return out
}

func TestEveryCompetencyQuestionHasAQuery(t *testing.T) {
	if len(Questions) != 26 {
		t.Fatalf("competency questions: got %d, the problem statement asks 23 and the act layer adds 3", len(Questions))
	}
	for i, q := range Questions {
		if q.N != i+1 {
			t.Errorf("question at index %d is numbered %d, and the numbers are how people refer to these", i, q.N)
		}
		text := q.Cypher()
		if strings.TrimSpace(text) == "" {
			t.Errorf("question %d has an empty query", q.N)
			continue
		}
		if !strings.Contains(text, "MATCH") || !strings.Contains(text, "RETURN") {
			t.Errorf("question %d has a query that does not read like one", q.N)
		}
		// Every query is bounded. The browser renders whatever comes back, and a
		// traversal over the full corpus that forgot its limit is how a person
		// loses a session and blames the graph.
		if !strings.Contains(text, "LIMIT $limit") {
			t.Errorf("question %d is not bounded by a parameterised LIMIT", q.N)
		}
	}
}

func TestEveryQueryParameterIsSuppliedAndEverySuppliedParameterIsUsed(t *testing.T) {
	declared := regexp.MustCompile(`\$([a-z_]+)`)
	for _, q := range Questions {
		text := q.Cypher()
		used := map[string]bool{}
		for _, m := range declared.FindAllStringSubmatch(text, -1) {
			used[m[1]] = true
		}
		for name := range used {
			if _, ok := q.Params[name]; !ok {
				t.Errorf("question %d uses $%s and supplies no default for it", q.N, name)
			}
		}
		for name := range q.Params {
			if !used[name] {
				t.Errorf("question %d supplies %s, which its query never mentions", q.N, name)
			}
		}
	}
}

func TestNoQueryNamesALabelOrRelationshipTypeTheExportDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	if err := Export(dir, competencyFixture()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	labels, types := vocabulary(t, dir)
	// Cypher keywords and function names that follow a colon or a pipe in the
	// grammar rather than naming anything in the graph.
	skip := map[string]bool{}
	upper := regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	for _, q := range Questions {
		for _, name := range queryTokens(q.Cypher()) {
			if skip[name] {
				continue
			}
			if upper.MatchString(name) {
				if !types[name] {
					t.Errorf("question %d traverses :%s, which no export writes", q.N, name)
				}
				continue
			}
			if !labels[name] {
				t.Errorf("question %d matches :%s, which no export writes", q.N, name)
			}
		}
	}
}

func TestTheProvincialPatternKnowsEverySpellingTheCampaignScopeDoes(t *testing.T) {
	re, err := regexp.Compile(provincialPattern)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, spelling := range campaign.Provincial {
		if !re.MatchString(strings.ToLower(spelling)) {
			t.Errorf("the provincial pattern misses %q, which the campaign scope excludes by name", spelling)
		}
	}
	if re.MatchString("chính phủ") {
		t.Error("the provincial pattern matches the central government, which would empty question 3")
	}
}

func TestTheDumpCarriesItsOwnQueries(t *testing.T) {
	dir := t.TempDir()
	if err := Export(dir, competencyFixture()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	for _, q := range Questions {
		path := filepath.Join(dir, "queries", q.File)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(b) != q.Cypher() {
			t.Errorf("%s in the dump is not the query the binary holds", q.File)
		}
	}
	index, err := os.ReadFile(filepath.Join(dir, "queries", "README.txt"))
	if err != nil {
		t.Fatalf("read the query index: %v", err)
	}
	for _, q := range Questions {
		if !strings.Contains(string(index), q.Ask) {
			t.Errorf("the query index does not say what question %d asks", q.N)
		}
	}
	style, err := os.ReadFile(filepath.Join(dir, "style.grass"))
	if err != nil {
		t.Fatalf("read the style file: %v", err)
	}
	// A caption for every label the export writes. A node captioned with its
	// identifier is a node nobody can read at a glance, which is the whole
	// argument for shipping Neo4j first.
	labels, _ := vocabulary(t, dir)
	for label := range labels {
		if !strings.Contains(string(style), "node."+label+" {") {
			t.Errorf("the style file gives %s no caption, so it renders as an identifier", label)
		}
	}
}

func TestQuestionByNumber(t *testing.T) {
	q, ok := QuestionByNumber(20)
	if !ok || !strings.Contains(q.Ask, "redefined") {
		t.Fatalf("question 20: got %q, %v", q.Ask, ok)
	}
	if _, ok := QuestionByNumber(27); ok {
		t.Error("there is no question 27")
	}
}
