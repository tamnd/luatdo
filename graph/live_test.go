package graph

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// The competency questions run against a live database.
//
// Everything else about the queries is checked statically: that they parse as
// text, that their parameters line up, that no query names a label the export
// does not write. None of that catches a query that runs and returns the wrong
// rows, and the wrong rows are the only failure mode anybody outside this
// repository will ever see. So the fixture corpus is loaded into a real Neo4j
// and every question is asked, with the answer written out here as a number and
// a value that was worked out by reading the fixture rather than by running the
// query and pasting what came back.
//
// The tests skip unless LUATDO_NEO4J_TEST_URI is set. The variable is separate
// from LUATDO_NEO4J_URI on purpose: the first thing these tests do is delete
// every node in the target database, and a developer with a working corpus in
// their local Neo4j has that variable set already. Naming the test target
// separately means the wipe can only ever hit a database somebody pointed at it
// deliberately.

// liveTarget is the database to load the fixture into, or a skip.
func liveTarget(t *testing.T) Target {
	t.Helper()
	uri := os.Getenv("LUATDO_NEO4J_TEST_URI")
	if uri == "" {
		t.Skip("set LUATDO_NEO4J_TEST_URI to run the competency questions against a real database, which wipes it")
	}
	target := Target{
		URI:      uri,
		User:     os.Getenv("LUATDO_NEO4J_TEST_USER"),
		Password: os.Getenv("LUATDO_NEO4J_TEST_PASSWORD"),
		Database: os.Getenv("LUATDO_NEO4J_TEST_DATABASE"),
	}
	if target.User == "" {
		target.User = "neo4j"
	}
	if target.Database == "" {
		target.Database = "neo4j"
	}
	return target
}

// live is a session against the loaded fixture.
type live struct {
	ctx     context.Context
	session neo4j.SessionWithContext
}

// loadFixture wipes the target, applies the schema, merges the fixture over
// Bolt, and hands back a session to ask questions through.
//
// It merges rather than importing CSV because the merge path is the one that
// has never had a test. The CSV path is checked file by file elsewhere, and the
// import tool that reads those files is Neo4j's, not ours. What was untested
// until now is the several hundred lines of Cypher in merge.go, and the fastest
// way to test all of it is to run every competency question over its output.
func loadFixture(t *testing.T) (*live, Target) {
	t.Helper()
	target := liveTarget(t)
	ctx := context.Background()

	driver, err := neo4j.NewDriverWithContext(target.URI, neo4j.BasicAuth(target.User, target.Password, ""))
	if err != nil {
		t.Fatalf("connect %s: %v", target.URI, err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("connect %s: %v", target.URI, err)
	}
	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: target.Database})
	t.Cleanup(func() {
		_ = session.Close(ctx)
		_ = driver.Close(ctx)
	})

	l := &live{ctx: ctx, session: session}
	l.wipe(t)
	for _, statement := range strings.Split(Schema, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		l.exec(t, statement)
	}
	if err := Merge(ctx, target, competencyFixture()); err != nil {
		t.Fatalf("merge the fixture: %v", err)
	}
	return l, target
}

// wipe empties the target a batch at a time.
//
// One MATCH (n) DETACH DELETE n is shorter and works on an empty database,
// which is the only database CI ever points this at. Aimed at a developer's
// throwaway instance holding a real campaign, it dies on the transaction memory
// limit at around a hundred thousand nodes, and every test after it fails with
// a message about memory pools rather than about anything these tests are for.
func (l *live) wipe(t *testing.T) {
	t.Helper()
	for {
		rows := l.rows(t, "MATCH (n) WITH n LIMIT 10000 DETACH DELETE n RETURN count(n) AS deleted", nil)
		if len(rows) == 0 || num(t, rows[0], "deleted") == 0 {
			return
		}
	}
}

func (l *live) exec(t *testing.T, cypher string) {
	t.Helper()
	result, err := l.session.Run(l.ctx, cypher, nil)
	if err == nil {
		_, err = result.Consume(l.ctx)
	}
	if err != nil {
		t.Fatalf("run %s: %v", strings.TrimSpace(cypher), err)
	}
}

func (l *live) rows(t *testing.T, cypher string, params map[string]any) []map[string]any {
	t.Helper()
	result, err := l.session.Run(l.ctx, cypher, params)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	records, err := result.Collect(l.ctx)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := make([]map[string]any, 0, len(records))
	for _, r := range records {
		out = append(out, r.AsMap())
	}
	return out
}

// str, num, yes and list read one field out of a returned row. The driver hands
// back any, and a test that type asserts inline reads as noise around the thing
// it is actually asserting.
func str(t *testing.T, row map[string]any, key string) string {
	t.Helper()
	v, ok := row[key].(string)
	if !ok && row[key] != nil {
		t.Fatalf("%s is %T, not a string", key, row[key])
	}
	return v
}

func num(t *testing.T, row map[string]any, key string) int64 {
	t.Helper()
	v, ok := row[key].(int64)
	if !ok {
		t.Fatalf("%s is %T, not a number", key, row[key])
	}
	return v
}

func yes(t *testing.T, row map[string]any, key string) bool {
	t.Helper()
	v, ok := row[key].(bool)
	if !ok {
		t.Fatalf("%s is %T, not a boolean", key, row[key])
	}
	return v
}

func list(t *testing.T, row map[string]any, key string) []any {
	t.Helper()
	if row[key] == nil {
		return nil
	}
	v, ok := row[key].([]any)
	if !ok {
		t.Fatalf("%s is %T, not a list", key, row[key])
	}
	return v
}

func eq(t *testing.T, field, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %q, want %q", field, got, want)
	}
}

// answer is one question, the parameters to ask it with, and what the fixture
// says comes back. A question can appear more than once, which is how question
// 16 gets asked on two dates.
type answer struct {
	n int
	// why says what the entry is pinning down, for the failure message. A
	// competency test that fails with "got 2 rows, want 1" and no other words
	// sends the reader back to the fixture to reconstruct the intent.
	why    string
	params map[string]any
	rows   int
	check  func(t *testing.T, got []map[string]any)
}

// liveAnswers is the fixture's answer to every question.
//
// The row counts are small on purpose. A fixture that returned forty rows would
// be checked with a count and nothing else, and a count alone passes happily
// when the query returns forty of the wrong thing.
var liveAnswers = []answer{
	{n: 1, why: "the 2019 code amends the 2012 code directly and the 1994 code through it", rows: 2,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "nearest", str(t, got[0], "amended"), fxOldCode)
			if h := num(t, got[0], "hops"); h != 2 {
				t.Errorf("hops to the 2012 code: got %d, want 2, one CONTAINS and one AMENDS", h)
			}
			eq(t, "furthest", str(t, got[1], "amended"), fxAncient)
			if h := num(t, got[1], "hops"); h != 4 {
				t.Errorf("hops to the 1994 code: got %d, want 4", h)
			}
		}},
	{n: 2, why: "the 2019 code cites the 2012 code, which a repeal event terminated", rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "citing", str(t, got[0], "in_force_document"), fxCode)
			eq(t, "cited", str(t, got[0], "repealed_document"), fxOldCode)
			eq(t, "how", str(t, got[0], "how"), "repeal")
		}},
	{n: 3, why: "the provincial decision of 2020 cites a central decree that took effect in 2021", rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "decision", str(t, got[0], "provincial_decision"), fxDecision)
			eq(t, "decree", str(t, got[0], "central_decree"), fxDecree)
		}},
	{n: 4, why: "two instruments define the word, and a reviewer recorded that they differ", rows: 2,
		check: func(t *testing.T, got []map[string]any) {
			// Ordered by instrument, and the 2014 law sorts before the 2019 code.
			eq(t, "first instrument", str(t, got[0], "instrument"), fxSocial)
			eq(t, "second instrument", str(t, got[1], "instrument"), fxCode)
			for _, row := range got {
				eq(t, "concept", str(t, row, "concept"), fxNLD)
				if n := len(list(t, row, "differs_from")); n != 1 {
					t.Errorf("%s differs from %d instruments, want 1", str(t, row, "instrument"), n)
				}
			}
			if str(t, got[0], "definition") == str(t, got[1], "definition") {
				t.Error("the two definitions came back identical, which is the opposite of the point of the question")
			}
		}},
	{n: 5, why: "both instruments hold a version in force on the date asked about", rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "term", str(t, got[0], "term"), "người lao động")
			eq(t, "from", str(t, got[0], "instrument_a"), fxSocial)
			eq(t, "to", str(t, got[0], "instrument_b"), fxCode)
		}},
	{n: 6, why: "the authority is named by three provisions and defined by none", params: map[string]any{"used_in": 1}, rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "concept", str(t, got[0], "concept"), fxCQCTQ)
			if p := num(t, got[0], "provisions"); p != 3 {
				t.Errorf("provisions using it: got %d, want 3", p)
			}
		}},
	{n: 7, why: "the committee has two parents, one of them the root, which is the inconsistency", params: map[string]any{"concept": fxCQNN}, rows: 3,
		check: func(t *testing.T, got []map[string]any) {
			flagged := map[string]bool{}
			for _, row := range got {
				flagged[str(t, row, "concept")] = yes(t, row, "inconsistent")
			}
			if !flagged[fxUBND] {
				t.Error("the committee is broader than both the authority and the root and was not flagged")
			}
			if flagged[fxCQCTQ] {
				t.Error("the authority has one parent and was flagged anyway")
			}
		}},
	{n: 8, why: "the minimum wage was supplemented into article 95 by the decree", rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "term", str(t, got[0], "term"), "mức lương tối thiểu")
			eq(t, "how", str(t, got[0], "introduced_by"), "supplement")
			eq(t, "instrument", str(t, got[0], "amending_instrument"), "145/2021/NĐ-CP")
		}},
	{n: 9, why: "three employer duties in the code, one of them sanctioned", rows: 3,
		check: func(t *testing.T, got []map[string]any) {
			// Ordered so the sanctioned ones come first, because that is the half
			// of the answer the question is really after.
			if !yes(t, got[0], "carries_sanction") {
				t.Error("the sanctioned duty did not sort first")
			}
			eq(t, "provision", str(t, got[0], "provision"), fxCode+":article-94")
			for _, row := range got[1:] {
				if yes(t, row, "carries_sanction") {
					t.Errorf("%s carries a sanction the fixture never gave it", str(t, row, "provision"))
				}
			}
		}},
	{n: 10, why: "one duty in the fixture was written with no bearer at all", rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "norm", str(t, got[0], "norm"), "vn:norm:fixture-orphan")
		}},
	{n: 11, why: "the permit procedure is two steps, apply then issue", rows: 2,
		check: func(t *testing.T, got []map[string]any) {
			if s := num(t, got[0], "step"); s != 1 {
				t.Errorf("first step: got %d, want 1", s)
			}
			if n := len(list(t, got[0], "requirements")); n != 1 {
				t.Errorf("the application step has %d requirements, want 1", n)
			}
			if s := num(t, got[1], "step"); s != 2 {
				t.Errorf("second step: got %d, want 2", s)
			}
			eq(t, "deadline", str(t, got[1], "deadline"), "trong thời hạn 20 ngày")
		}},
	{n: 12, why: "one deadline is under five working days, the other is twenty ordinary days", rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			if d := num(t, got[0], "working_days"); d != 3 {
				t.Errorf("working days: got %d, want 3", d)
			}
			eq(t, "provision", str(t, got[0], "provision"), fxCode+":article-95")
		}},
	{n: 13, why: "the document keeping prohibition stands alone, the pay prohibition sits beside a sanctioned duty", rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "provision", str(t, got[0], "provision"), fxCode+":article-97")
		}},
	{n: 14, why: "the paying duty has one condition and one exception", params: map[string]any{"norm": "vn:norm:fixture-pay"}, rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "type", str(t, got[0], "norm_type"), "duty")
			eq(t, "provision", str(t, got[0], "provision"), fxCode+":article-94")
			if n := len(list(t, got[0], "conditions")); n != 1 {
				t.Errorf("conditions: got %d, want 1", n)
			}
			if n := len(list(t, got[0], "exceptions")); n != 1 {
				t.Errorf("exceptions: got %d, want 1", n)
			}
		}},
	{n: 15, why: "three norms name the authority and nothing in the corpus defines it", rows: 3,
		check: func(t *testing.T, got []map[string]any) {
			for _, row := range got {
				if !strings.Contains(str(t, row, "bearer"), "cơ quan có thẩm quyền") {
					t.Errorf("%s came back without the role in its bearer", str(t, row, "provision"))
				}
			}
		}},
	{n: 16, why: "on this date article 94 still said what it was enacted saying", params: map[string]any{"date": "2021-03-01"}, rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			if s := num(t, got[0], "seq"); s != 1 {
				t.Errorf("version in force in March 2021: got seq %d, want 1", s)
			}
			eq(t, "to", str(t, got[0], "to_date"), "2021-06-01")
			// The clause hangs off the article, and the article as it stood is not
			// the article's own sentence alone.
			if n := len(list(t, got[0], "included_text")); n != 1 {
				t.Errorf("included clause texts: got %d, want 1", n)
			}
			if n := len(list(t, got[0], "actions")); n != 1 {
				t.Errorf("actions on the article: got %d, want 1", n)
			}
		}},
	{n: 16, why: "after the decree amended it, the same question on a later date reads the second version", params: map[string]any{"date": "2023-01-01"}, rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			if s := num(t, got[0], "seq"); s != 2 {
				t.Errorf("version in force in January 2023: got seq %d, want 2", s)
			}
			eq(t, "still open", str(t, got[0], "to_date"), "")
			if !strings.Contains(str(t, got[0], "text"), "đúng hạn") {
				t.Errorf("the amended text did not come back: %q", str(t, got[0], "text"))
			}
		}},
	{n: 17, why: "article 94 stood for 151 days before the decree amended it", rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "component", str(t, got[0], "component"), fxCode+":article-94")
			if d := num(t, got[0], "days"); d != 151 {
				t.Errorf("days in force: got %d, want 151 from 2021-01-01 to 2021-06-01", d)
			}
			eq(t, "ended by", str(t, got[0], "ended_by"), "amend")
		}},
	{n: 18, why: "article 94 has two versions, enacted then amended", rows: 2,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "opened", str(t, got[0], "opened_by"), "enact")
			eq(t, "closed", str(t, got[0], "closed_by"), "amend")
			eq(t, "reopened", str(t, got[1], "opened_by"), "amend")
			eq(t, "by", str(t, got[1], "instrument"), "145/2021/NĐ-CP")
			eq(t, "still open", str(t, got[1], "closed_by"), "")
		}},
	{n: 19, why: "article 94 requires paying wages and its clause 1 forbids the same thing of the same actor", rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "concept", str(t, got[0], "concept"), "tiền lương")
			eq(t, "action", str(t, got[0], "action"), "trả lương")
			eq(t, "first modality", str(t, got[0], "modality_a"), "prohibition")
			eq(t, "second modality", str(t, got[0], "modality_b"), "duty")
		}},
	{n: 20, why: "the code redefined a word the 2014 law had defined, and the 2014 norm was never revisited", rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "concept", str(t, got[0], "concept"), "người lao động")
			eq(t, "redefined by", str(t, got[0], "redefined_by"), fxCode)
			eq(t, "affected", str(t, got[0], "affected_provision"), fxSocial+":article-5")
			if !yes(t, got[0], "never_revisited") {
				t.Error("the affected provision has no amendment event and should have come back as never revisited")
			}
		}},
	{n: 21, why: "a permit needs a design file, which needs a land certificate", params: map[string]any{"concept": fxGPXD}, rows: 2,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "first prerequisite", str(t, got[0], "prerequisite"), fxHSTK)
			eq(t, "second prerequisite", str(t, got[1], "prerequisite"), fxGCN)
			if d := num(t, got[1], "depth"); d != 2 {
				t.Errorf("depth of the second prerequisite: got %d, want 2", d)
			}
		}},
	{n: 22, why: "each instrument uses a word the other defines, and neither cites the other", rows: 2,
		check: func(t *testing.T, got []map[string]any) {
			for _, row := range got {
				if yes(t, row, "connected_by_citation") {
					t.Errorf("%s and %s came back connected, and the fixture has no citation between them",
						str(t, row, "uses_it"), str(t, row, "defines_it"))
				}
			}
		}},
	{n: 23, why: "one alternative pair carries enough support to report", rows: 1,
		check: func(t *testing.T, got []map[string]any) {
			eq(t, "first concept", str(t, got[0], "concept_a"), "hợp đồng lao động")
			eq(t, "second concept", str(t, got[0], "concept_b"), "hợp đồng làm việc")
			if p := num(t, got[0], "provisions"); p != 3 {
				t.Errorf("support: got %d, want 3", p)
			}
		}},
}

// params merges an entry's overrides over the question's shipped defaults, so
// each entry states only what the fixture needs to differ.
func (a answer) merged(q Question) map[string]any {
	out := map[string]any{}
	for k, v := range q.Params {
		out[k] = v
	}
	for k, v := range a.params {
		out[k] = v
	}
	return out
}

func TestEveryCompetencyQuestionAnswersOnTheFixtureGraph(t *testing.T) {
	l, _ := loadFixture(t)
	asked := map[int]bool{}
	for _, a := range liveAnswers {
		q, ok := QuestionByNumber(a.n)
		if !ok {
			t.Fatalf("there is no question %d to answer", a.n)
		}
		asked[a.n] = true
		t.Run(q.File+" "+a.why, func(t *testing.T) {
			got := l.rows(t, q.Cypher(), a.merged(q))
			if len(got) != a.rows {
				t.Fatalf("question %d returned %d rows, want %d, because %s\n%v", a.n, len(got), a.rows, a.why, got)
			}
			if a.check != nil {
				a.check(t, got)
			}
		})
	}
	for _, q := range Questions {
		if !asked[q.N] {
			t.Errorf("question %d runs against nothing with a known answer, so nobody would notice it breaking", q.N)
		}
	}
}

func TestTheMergedDatabaseHoldsWhatTheStoreSaysItShould(t *testing.T) {
	_, target := loadFixture(t)
	got, err := Live(context.Background(), target)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	// The merge path and the CSV path are two separate pieces of Cypher over the
	// same input, and until now only the CSV one was ever counted. Anything the
	// merge forgets to write, or writes twice, shows up here as drift against
	// the same summary the export reports.
	for _, line := range Drift(Summarize(competencyFixture()), got) {
		t.Errorf("after merging the fixture: %s", line)
	}
}

func TestMergingTheFixtureTwiceChangesNothing(t *testing.T) {
	_, target := loadFixture(t)
	before, err := Live(context.Background(), target)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	// The whole claim of the merge path is that it is safe to run against a
	// database that already holds an earlier export. That claim is one MERGE
	// keyed wrongly away from being false, and a duplicated relationship is
	// invisible until a query starts returning every row twice.
	if err := Merge(context.Background(), target, competencyFixture()); err != nil {
		t.Fatalf("merge again: %v", err)
	}
	after, err := Live(context.Background(), target)
	if err != nil {
		t.Fatalf("count again: %v", err)
	}
	for name, n := range after {
		if before[name] != n {
			t.Errorf("%s: %d after one merge, %d after two", name, before[name], n)
		}
	}
}

func TestAskAnswersWithoutAnybodyWritingCypher(t *testing.T) {
	_, target := loadFixture(t)
	q, _ := QuestionByNumber(16)
	// The whole promise of the command is that the shipped defaults can be
	// overridden one at a time. Question 16 ships a component and a date, and
	// asking it about a different date should not mean retyping the component.
	answer, err := Ask(context.Background(), target, q, map[string]any{"date": "2023-01-01"})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if len(answer.Rows) != 1 {
		t.Fatalf("got %d rows, want the one version in force in January 2023", len(answer.Rows))
	}
	if answer.Params["component"] != q.Params["component"] {
		t.Errorf("the component default was lost: %v", answer.Params["component"])
	}
	out := answer.String()
	if !strings.Contains(out, "đúng hạn") {
		t.Errorf("the amended text is not in the rendered answer:\n%s", out)
	}

	// A parameter the question does not take is a typo, and a typo that runs
	// anyway returns the shipped defaults while the person reads the rows as an
	// answer to what they thought they asked.
	if _, err := Ask(context.Background(), target, q, map[string]any{"dat": "2023-01-01"}); err == nil {
		t.Error("a misspelled parameter was accepted")
	}
}
