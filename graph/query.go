package graph

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The twenty three competency questions, as queries.
//
// The list comes from the problem statement the project was specified against,
// and it is the only test of the graph that matters to anybody outside it. A
// projection that loads cleanly, passes every invariant, and cannot answer
// question 16 is a well built object nobody needed.
//
// Every query is shipped rather than described, for two reasons. A query written
// out in a document rots the first time a label is renamed, and nobody types a
// twelve line traversal from a README. These files are embedded in the binary,
// written into every dump, and run against a fixture graph in CI, so a rename
// that breaks one breaks the build.

//go:embed cypher/*.cypher
var cypherFS embed.FS

// Question is one competency question with the query that answers it and the
// parameters that query takes.
//
// The defaults are examples, not settings. They name real identifiers from the
// labour corpus so that a person who has just loaded a dump can run any of these
// unedited and see rows come back, and every one of them is meant to be replaced
// by the identifier the reader actually cares about.
type Question struct {
	N      int
	Ask    string
	File   string
	Params map[string]any
}

// provincialPattern matches the issuing bodies of a province, in the spellings
// the corpus uses, against a lowercased issuing body.
//
// It is a regular expression and not a property because no document in the
// corpus carries a field saying which level of government issued it. The name of
// the body is the only evidence there is. The spellings are the same list the
// campaign scope uses, including both of the two ways Vietnamese writes uỷ and
// ủy, and a test holds the two lists to each other.
const provincialPattern = `.*(ubnd|ủy ban nhân dân|uỷ ban nhân dân|hđnd|hội đồng nhân dân).*`

// labourCode and its article 94 are the worked example throughout the questions
// and the README, because the spec asks about them by name.
const (
	labourCode        = "vn:law:2019:45-2019-qh14"
	labourCodeArt94   = "vn:law:2019:45-2019-qh14:article-94"
	socialInsuranceQ4 = "người lao động"
)

// anyRule is what question 19 passes when it is not asking about one rule.
//
// Cypher has no default parameter, and a query run without every parameter it
// names fails rather than treating the missing one as null. A named nil is
// clearer at the call site than a bare one and says what the absence means.
var anyRule any

// Questions is the list, in the order the problem statement asks them.
var Questions = []Question{
	{1, "Which documents does 45/2019/QH14 amend, transitively?", "q01.cypher",
		map[string]any{"doc": labourCode, "limit": 100}},
	{2, "Which currently in force documents cite a document that has been repealed?", "q02.cypher",
		map[string]any{"limit": 100}},
	{3, "Which provincial decisions cite a central decree issued after them?", "q03.cypher",
		map[string]any{"provincial": provincialPattern, "central_type": "decree", "limit": 100}},
	{4, `Where is "người lao động" defined, and do the Labour Code and the Social Insurance Law define it the same way?`, "q04.cypher",
		map[string]any{"term": socialInsuranceQ4, "limit": 100}},
	{5, "List every term whose definition differs between two instruments that are both in force.", "q05.cypher",
		map[string]any{"date": "2025-01-01", "limit": 100}},
	{6, "Which concepts have no definition anywhere in the corpus but are used in more than 100 provisions?", "q06.cypher",
		map[string]any{"used_in": 100, "limit": 100}},
	{7, `Show the hierarchy under "cơ quan nhà nước" as the corpus actually uses it, and flag where the corpus is inconsistent about it.`, "q07.cypher",
		map[string]any{"concept": "vn:concept:co-quan-nha-nuoc", "limit": 200}},
	{8, "Which defined terms were introduced by an amendment rather than by the original instrument?", "q08.cypher",
		map[string]any{"limit": 100}},
	{9, "What duties does the Labour Code place on employers, and which of them carry a sanction?", "q09.cypher",
		map[string]any{"doc": labourCode, "norm_type": "duty", "class": "vn-legal:Employer", "limit": 200}},
	{10, "Which duties in the corpus have no identified bearer, and are they drafting defects or extraction failures?", "q10.cypher",
		map[string]any{"norm_type": "duty", "limit": 100}},
	{11, "What must a business do to obtain a construction permit, as a sequence of procedural steps with their document requirements and deadlines?", "q11.cypher",
		map[string]any{"procedure": "cấp giấy phép xây dựng", "limit": 100}},
	{12, "Every deadline shorter than 5 working days, with the actor who must meet it.", "q12.cypher",
		map[string]any{"days": 5, "limit": 100}},
	{13, "Which prohibitions have no corresponding sanction anywhere in the corpus?", "q13.cypher",
		map[string]any{"limit": 100}},
	{14, "For a given duty, what conditions must hold and what exceptions release the bearer from it?", "q14.cypher",
		map[string]any{"norm": labourCodeArt94 + ":clause-1:norm-1", "limit": 10}},
	{15, `Which norms name a "cơ quan có thẩm quyền" that no provision in the corpus ever resolves?`, "q15.cypher",
		map[string]any{"role": "cơ quan có thẩm quyền", "limit": 100}},
	{16, "What did article 94 of the Labour Code require on 2020-01-01, and on 2023-01-01?", "q16.cypher",
		map[string]any{"component": labourCodeArt94, "date": "2020-01-01", "limit": 10}},
	{17, "Which norms were in force for less than a year before being amended?", "q17.cypher",
		map[string]any{"days": 365, "limit": 100}},
	{18, "Show the amendment history of one provision as a chain of events with the instrument that caused each.", "q18.cypher",
		map[string]any{"component": labourCodeArt94, "limit": 50}},
	{19, "Two provisions both regulate the same concept and impose incompatible modalities on the same actor for the same action. Find them.", "q19.cypher",
		map[string]any{"rule": anyRule, "limit": 100}},
	{20, "A concept was redefined by a later instrument. Which earlier norms mentioning it are now potentially affected, and which of them were never amended?", "q20.cypher",
		map[string]any{"limit": 200}},
	{21, "What must exist before a construction permit can be issued, following only concept level prerequisite relations and not the text?", "q21.cypher",
		map[string]any{"concept": "vn:concept:giay-phep-xay-dung", "limit": 100}},
	{22, "Which concepts are required by a norm in one instrument and defined in another, and are those two instruments connected in the citation graph at all?", "q22.cypher",
		map[string]any{"limit": 200}},
	{23, "Which concept pairs does the corpus treat as alternatives, in the sense that a provision offering one always offers the other?", "q23.cypher",
		map[string]any{"support": 2, "limit": 100}},
}

// Cypher is the query text.
//
// A question naming a file the embed does not hold is a programming error that
// no input can cause and every test run catches, so it panics rather than
// growing an error return that twenty call sites would have to ignore.
func (q Question) Cypher() string {
	b, err := cypherFS.ReadFile("cypher/" + q.File)
	if err != nil {
		panic(fmt.Sprintf("competency question %d names %s, which is not embedded", q.N, q.File))
	}
	return string(b)
}

// QuestionByNumber finds a question by the number the problem statement gives
// it, which is the number people use when they talk about these.
func QuestionByNumber(n int) (Question, bool) {
	for _, q := range Questions {
		if q.N == n {
			return q, true
		}
	}
	return Question{}, false
}

// writeQueryScripts writes the queries into a queries subdirectory of the dump,
// with a README listing which question each answers.
//
// The dump is the unit somebody is handed, so the questions travel with it. A
// dump that arrives with data and no queries is a database somebody has to
// reverse engineer before they can ask it anything, and the label names in these
// files are the fastest documentation of the projection that exists.
func writeQueryScripts(dir string) error {
	qdir := filepath.Join(dir, "queries")
	if err := os.MkdirAll(qdir, 0o755); err != nil {
		return err
	}
	var index strings.Builder
	index.WriteString("The twenty three competency questions, one file each.\n\n")
	index.WriteString("Every query takes parameters. In the Neo4j Browser, set them first with\n")
	index.WriteString(":param name => value, or run them from the command line with\n")
	index.WriteString("luatdo graph ask --question N, which fills in the defaults below.\n\n")
	for _, q := range Questions {
		if err := os.WriteFile(filepath.Join(qdir, q.File), []byte(q.Cypher()), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(&index, "%s\n  %s\n", q.File, q.Ask)
		if len(q.Params) > 0 {
			fmt.Fprintf(&index, "  parameters: %s\n", paramList(q))
		}
		index.WriteString("\n")
	}
	return os.WriteFile(filepath.Join(qdir, "README.txt"), []byte(index.String()), 0o644)
}

// paramList renders a question's parameters in a fixed order, so two runs of the
// export produce byte identical files.
func paramList(q Question) string {
	names := paramNames(q)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s = %v", name, q.Params[name]))
	}
	return strings.Join(parts, ", ")
}

// paramNames lists a question's parameters in the order they first appear in its
// query text, which is the order somebody reading the query meets them, and puts
// any that the text does not mention at the end where they are visible as the
// mistake they are.
func paramNames(q Question) []string {
	text := q.Cypher()
	type seen struct {
		name string
		at   int
	}
	var found []seen
	var missing []string
	for name := range q.Params {
		if at := strings.Index(text, "$"+name); at >= 0 {
			found = append(found, seen{name, at})
			continue
		}
		missing = append(missing, name)
	}
	for i := 1; i < len(found); i++ {
		for j := i; j > 0 && found[j].at < found[j-1].at; j-- {
			found[j], found[j-1] = found[j-1], found[j]
		}
	}
	out := make([]string, 0, len(q.Params))
	for _, f := range found {
		out = append(out, f.name)
	}
	for i := 1; i < len(missing); i++ {
		for j := i; j > 0 && missing[j] < missing[j-1]; j-- {
			missing[j], missing[j-1] = missing[j-1], missing[j]
		}
	}
	return append(out, missing...)
}
