package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/answer"
	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/eval"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/retrieve"
	"github.com/tamnd/luatdo/store"
	"github.com/tamnd/luatdo/subject"
	"github.com/tamnd/luatdo/temporal"
	"github.com/tamnd/luatdo/term"
)

func init() {
	commands = append(commands,
		command{"retrieve", "find the components that bear on a question, scope first", cmdRetrieve},
		command{"answer", "answer in Vietnamese, saying only what a trusted statement supports", cmdAnswer},
		command{"statute", "run the committed statute benchmark, scored in three parts", cmdStatute},
	)
}

// corpus is the material both the retriever and the answerer work from.
//
// It is built over the documents the trusted store holds statements for rather
// than over all 128 thousand parsed documents. That is not a shortcut for the
// benchmark's sake: a component with no verified statement cannot license a
// claim, so putting the rest of the corpus in the index would add candidates
// the answerer is forbidden to use. The --doc flag widens it for anyone who
// wants to see what the retriever does over text alone.
type corpus struct {
	index    *retrieve.Index
	docs     []*law.Document
	titles   map[string]string
	byComp   map[string][]norm.Record
	stamped  int
	unstampd int
}

type docList []string

func (d *docList) String() string { return strings.Join(*d, ",") }
func (d *docList) Set(v string) error {
	*d = append(*d, v)
	return nil
}

// loadCorpus reads only the files it needs. Reading the whole cite and terms
// directories would be 128 thousand opens for a nine document index.
func loadCorpus(dataDir string, extra []string, plain bool) (*corpus, error) {
	s, err := openStore(dataDir)
	if err != nil {
		return nil, err
	}
	records, err := loadTrusted(s)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, r := range records {
		if r.Trusted() {
			ids[r.DocID] = true
		}
	}
	for _, id := range extra {
		ids[id] = true
	}
	c := &corpus{titles: map[string]string{}, byComp: map[string][]norm.Record{}}
	for _, id := range sortedDocIDs(ids) {
		var doc law.Document
		if err := store.ReadJSON(filepath.Join(s.Docs(), law.FileName(id)), &doc); err != nil {
			return nil, fmt.Errorf("read %s: %w", id, err)
		}
		c.docs = append(c.docs, &doc)
		c.titles[doc.ID] = doc.Title
	}
	for _, r := range records {
		if r.Trusted() {
			c.byComp[r.ProvisionID] = append(c.byComp[r.ProvisionID], r)
		}
	}

	in := retrieve.Input{Docs: c.docs}
	if !plain {
		in.Records = records
		in.Subjects = readSubjects(s, ids)
		in.Terms = readTerms(s, ids)
		in.Links = readCites(s, ids)
		validity, verr := temporal.ReadValidity(s.Temporal())
		if verr != nil && !os.IsNotExist(verr) {
			return nil, verr
		}
		in.Validity = validity
	}
	c.index = retrieve.Build(in)
	for _, u := range c.index.Units() {
		if len(u.Intervals) > 0 {
			c.stamped++
			continue
		}
		c.unstampd++
	}
	return c, nil
}

// readSubjects, readTerms and readCites all answer with what is there. A layer
// nobody has run yet contributes nothing to the index, which is the plain text
// index, and the report says so rather than failing.
func readSubjects(s *store.Store, ids map[string]bool) []subject.Record {
	all, err := subject.ReadRecords(filepath.Join(s.Subject(), subject.AssignmentsFile))
	if err != nil {
		return nil
	}
	var out []subject.Record
	for _, r := range all {
		if ids[r.DocID] {
			out = append(out, r)
		}
	}
	return out
}

func readTerms(s *store.Store, ids map[string]bool) []term.Definition {
	var out []term.Definition
	for _, id := range sortedDocIDs(ids) {
		var defs []term.Definition
		if err := store.ReadJSON(filepath.Join(s.Terms(), law.FileName(id)), &defs); err != nil {
			continue
		}
		out = append(out, defs...)
	}
	return out
}

func readCites(s *store.Store, ids map[string]bool) []cite.Link {
	var out []cite.Link
	for _, id := range sortedDocIDs(ids) {
		var links []cite.Link
		if err := store.ReadJSON(filepath.Join(s.Cite(), law.FileName(id)), &links); err != nil {
			continue
		}
		out = append(out, links...)
	}
	return out
}

func sortedDocIDs(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sources turns a ranked result into what the answerer is allowed to see. The
// statements travel with the component, because the answerer has to cite one by
// identifier and cannot be trusted to remember which component carried it.
func (c *corpus) sources(hits []retrieve.Hit) []answer.Source {
	out := make([]answer.Source, 0, len(hits))
	for _, h := range hits {
		u := h.Unit
		out = append(out, answer.Source{
			ComponentID: u.ComponentID,
			DocID:       u.DocID,
			Title:       c.titles[u.DocID],
			Heading:     u.Heading,
			// The span rather than the component's own words, because a clause
			// that is a stem with lettered points carries statements about
			// words that live in the points, and an answerer that may only
			// quote the stem cannot support them.
			Text:       u.Span,
			Statements: c.byComp[u.ComponentID],
			Intervals:  u.Intervals,
		})
	}
	return out
}

// scopeFlags is the retrieval primitive set from #38, one flag each.
type scopeFlags struct {
	docs, subjects, components, kinds docList
	date                              string
	statements                        bool
	unread                            bool
}

func (sf *scopeFlags) bind(fs *flag.FlagSet) {
	fs.Var(&sf.docs, "doc", "restrict to an instrument, repeatable")
	fs.Var(&sf.subjects, "subject", "restrict to a subject or any subject under it, repeatable")
	fs.Var(&sf.components, "component", "restrict to a component and its subtree, repeatable")
	fs.Var(&sf.kinds, "kind", "restrict to a component kind such as clause, repeatable")
	fs.StringVar(&sf.date, "date", "", "restrict to what was in force on this date, as YYYY-MM-DD")
	fs.BoolVar(&sf.statements, "statements", false, "restrict to components carrying a trusted statement")
	fs.BoolVar(&sf.unread, "assume-unread", false, "at a date, keep wordings whose amendment nobody has read, which is a guess")
}

func (sf *scopeFlags) scope() retrieve.Scope {
	return retrieve.Scope{
		Docs: sf.docs, Subjects: sf.subjects, Components: sf.components,
		Kinds: sf.kinds, Date: sf.date, Statements: sf.statements, Unread: sf.unread,
	}
}

func cmdRetrieve(args []string) error {
	fs := flag.NewFlagSet("retrieve", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	k := fs.Int("k", 8, "how many components to return")
	explain := fs.Bool("explain", false, "show which aspect contributed what to each score")
	plain := fs.Bool("plain", false, "index the words only, with no aspects from the graph")
	flat := fs.Bool("flat", false, "the flat chunk baseline instead, which has no scope and no aspects")
	asJSON := fs.Bool("json", false, "write the result as JSON")
	dupes := fs.Float64("duplicates", 0, "overlap above which a component counts as a restatement, negative to keep both")
	var sf scopeFlags
	sf.bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("usage: luatdo retrieve [flags] <query>")
	}
	c, err := loadCorpus(*dataDir, sf.docs, *plain)
	if err != nil {
		return err
	}

	if *flat {
		f := retrieve.BuildFlat(c.docs, retrieve.DefaultChunk, retrieve.DefaultOverlap)
		hits := f.Search(query, *k)
		if *asJSON {
			return writeJSON(hits)
		}
		fmt.Printf("flat baseline over %d chunks, no scope, no aspects\n", f.Len())
		for i, ch := range hits {
			fmt.Printf("%2d. %s\n    covers %s\n    %s\n", i+1, ch.ID, strings.Join(ch.Covers, " "), snippet(ch.Text))
		}
		return nil
	}

	res := c.index.Search(retrieve.Query{Text: query, K: *k, Scope: sf.scope(), Duplicates: *dupes})
	if *asJSON {
		return writeJSON(res)
	}
	printResult(c, res, *explain)
	return nil
}

func printResult(c *corpus, res retrieve.Result, explain bool) {
	fmt.Printf("%d components indexed, %d stamped with a validity interval, %d unstamped\n",
		res.Indexed, c.stamped, c.unstampd)
	for _, st := range res.Steps {
		fmt.Printf("  scope  %s\n", st)
	}
	fmt.Printf("%d in scope, %d matched the query, %d suppressed as restatements\n",
		res.InScope, res.Matched, res.Suppressed)
	if len(res.Hits) == 0 {
		if res.InScope == 0 {
			fmt.Println("nothing to rank: the scope kept no component")
			return
		}
		fmt.Println("nothing matched: the query shares no word with any component in scope")
		return
	}
	for i, h := range res.Hits {
		fmt.Printf("%2d. %-8.3f %s\n", i+1, h.Score, h.ID)
		if h.Unit.Heading != "" {
			fmt.Printf("    %s\n", h.Unit.Heading)
		}
		fmt.Printf("    %s\n", snippet(h.Unit.Text))
		if len(h.Unit.Statements) > 0 {
			fmt.Printf("    %d trusted statements\n", len(h.Unit.Statements))
		}
		if len(h.Duplicates) > 0 {
			fmt.Printf("    restated by %s\n", strings.Join(h.Duplicates, " "))
		}
		if explain {
			for _, a := range sortedAspects(h.ByAspect) {
				fmt.Printf("      %-10s %.3f  %s\n", a, h.ByAspect[a], snippet(strings.Join(h.Unit.Aspect(a), " | ")))
			}
		}
	}
}

func sortedAspects(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v > 0 {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return m[out[i]] > m[out[j]] })
	return out
}

func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= 200 {
		return s
	}
	return s[:200] + " ..."
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func cmdAnswer(args []string) error {
	fs := flag.NewFlagSet("answer", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	k := fs.Int("k", 8, "how many components to put in front of the model")
	corrections := fs.Int("max-corrections", 2, "bounded retries on invalid model output")
	none := fs.Bool("no-retrieval", false, "ask the model with nothing, which is the baseline")
	asJSON := fs.Bool("json", false, "write the answer as JSON")
	var sf scopeFlags
	sf.bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	question := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(question) == "" {
		return fmt.Errorf("usage: luatdo answer [flags] <question>")
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	req := answer.Request{Question: question, AsOf: sf.date}
	if !*none {
		c, cerr := loadCorpus(*dataDir, sf.docs, false)
		if cerr != nil {
			return cerr
		}
		res := c.index.Search(retrieve.Query{Text: question, K: *k, Scope: sf.scope()})
		req.Sources = c.sources(res.Hits)
		fmt.Fprintf(os.Stderr, "retrieved %d of %d components in scope\n", len(res.Hits), res.InScope)
	}

	a := &answer.Answerer{Completer: eng.completer, Model: eng.model, MaxCorrections: *corrections}
	out, err := a.Answer(context.Background(), req)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(out)
	}
	printAnswer(out)
	reportRoutes(eng)
	return nil
}

func printAnswer(a answer.Answer) {
	if a.Refused {
		fmt.Printf("từ chối: %s\n", a.Reason)
	}
	for i, cl := range a.Claims {
		fmt.Printf("%d. %s\n   %s\n   trích: %s\n", i+1, cl.Text, cl.ComponentID, cl.Quote)
	}
	for _, d := range a.Dropped {
		fmt.Printf("dropped: %s\n   %s (%s)\n", d.Reason, d.Claim.Text, d.Claim.ComponentID)
	}
	kept, made := a.Grounded()
	fmt.Printf("%d of %d sentences survived the check, %d calls, %d tokens\n", kept, made, a.Calls, a.Usage.TotalTokens)
}

// cmdStatute runs the committed benchmark.
//
// Construction and retrieval cost nothing and always run. Generation needs the
// model and only runs with --generate, so the retrieval numbers can be
// regenerated on a laptop with no endpoint at all.
func cmdStatute(args []string) error {
	fs := flag.NewFlagSet("statute", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	k := fs.Int("k", 8, "how many components retrieval may return")
	mode := fs.String("mode", "graph", "graph, flat or none, where none asks the model with no retrieval")
	unread := fs.Bool("assume-unread", false, "at a date, keep wordings whose amendment nobody has read, which is a guess")
	generate := fs.Bool("generate", false, "also ask the model, which spends tokens")
	corrections := fs.Int("max-corrections", 2, "bounded retries on invalid model output")
	out := fs.String("out", "", "write the run to this file as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	bench := eval.Statute()
	c, err := loadCorpus(*dataDir, nil, *mode == "flat")
	if err != nil {
		return err
	}
	var flat *retrieve.Flat
	if *mode == "flat" {
		flat = retrieve.BuildFlat(c.docs, retrieve.DefaultChunk, retrieve.DefaultOverlap)
	}

	construction := eval.ScoreConstruction(bench,
		func(id string) bool { return c.index.Unit(id) != nil },
		func(id string) bool { return len(c.byComp[id]) > 0 })
	built := map[int]bool{}
	for _, n := range construction.Reachable() {
		built[n] = true
	}

	var eng *engine
	var writer *answer.Answerer
	if *generate {
		eng, err = openEngine()
		if err != nil {
			return err
		}
		writer = &answer.Answerer{Completer: eng.completer, Model: eng.model, MaxCorrections: *corrections}
	}

	var runs []eval.Retrieved
	var gens []eval.Generated
	var answers []answer.Answer
	for _, q := range bench.Questions {
		ranked, sources, inScope := retrieveFor(c, flat, q, *k, *mode, *unread)
		runs = append(runs, eval.Retrieved{
			N: q.N, Ranked: ranked, Gold: q.Gold, Built: built[q.N],
			InScope: inScope, Answered: q.Answerable(),
		})
		if writer == nil {
			continue
		}
		a, aerr := writer.Answer(context.Background(), answer.Request{Question: q.Question, AsOf: q.AsOf, Sources: sources})
		if aerr != nil {
			return fmt.Errorf("question %d: %w", q.N, aerr)
		}
		answers = append(answers, a)
		gens = append(gens, scoreAnswer(q, a))
		fmt.Fprintf(os.Stderr, "  q%-3d %s\n", q.N, verdict(q, a))
	}

	retrieval := eval.ScoreRetrieval(*k, runs)
	generation := eval.ScoreGeneration(gens)
	fmt.Print(eval.Report(*mode+" retrieval", construction, retrieval, generation))
	if !*generate {
		fmt.Println("  note: generation was not run, so its figures are absent rather than zero")
	}
	if eng != nil {
		reportRoutes(eng)
	}
	if *out != "" {
		return store.WriteJSON(*out, map[string]any{
			"mode": *mode, "k": *k, "generated": *generate,
			"construction": construction, "retrieval": retrieval,
			"generation": generation, "answers": answers,
		})
	}
	return nil
}

// retrieveFor is where the three systems differ and nothing else does. They see
// the same questions, return the same shape, and are scored by the same code.
func retrieveFor(c *corpus, flat *retrieve.Flat, q eval.BenchQuestion, k int, mode string, unread bool) ([]string, []answer.Source, int) {
	switch mode {
	case "none":
		return nil, nil, 0
	case "flat":
		var ranked []string
		var sources []answer.Source
		for _, ch := range flat.Search(q.Question, k) {
			ranked = append(ranked, ch.Covers...)
			// The baseline has no component to cite, so what the answerer gets
			// is the chunk under the identifier of the first component it
			// covers. That is the honest version of the comparison: flat
			// retrieval really does hand a model a window rather than a
			// provision.
			id := ch.StartID
			sources = append(sources, answer.Source{
				ComponentID: id, DocID: ch.DocID, Title: c.titles[ch.DocID],
				Text: ch.Text, Statements: c.byComp[id],
			})
		}
		return ranked, sources, flat.Len()
	default:
		res := c.index.Search(retrieve.Query{Text: q.Question, K: k,
			Scope: retrieve.Scope{Date: q.AsOf, Unread: unread}})
		ranked := make([]string, 0, len(res.Hits))
		for _, h := range res.Hits {
			ranked = append(ranked, h.ID)
		}
		return ranked, c.sources(res.Hits), res.InScope
	}
}

func scoreAnswer(q eval.BenchQuestion, a answer.Answer) eval.Generated {
	g := eval.Generated{N: q.N, Answered: q.Answerable(), Refused: a.Refused}
	gold := map[string]bool{}
	for _, id := range q.Gold {
		gold[id] = true
	}
	for _, cl := range a.Claims {
		g.Claims++
		g.Grounded++
		g.Cited = append(g.Cited, cl.ComponentID)
		if gold[cl.ComponentID] {
			g.OnGold++
		}
	}
	for _, d := range a.Dropped {
		g.Claims++
		if d.Reason == answer.DropUnknownComponent || d.Reason == answer.DropUnknownStatement {
			g.Invented++
		}
	}
	return g
}

func verdict(q eval.BenchQuestion, a answer.Answer) string {
	kept, made := a.Grounded()
	if !q.Answerable() {
		if a.Refused {
			return "refused, which is right"
		}
		return fmt.Sprintf("answered a question the corpus cannot answer, %d of %d sentences survived", kept, made)
	}
	if a.Refused {
		return "refused"
	}
	return fmt.Sprintf("%d of %d sentences survived", kept, made)
}
