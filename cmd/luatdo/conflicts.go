package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/campaign"
	"github.com/tamnd/luatdo/conflict"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/store"
	"github.com/tamnd/luatdo/temporal"
)

func init() {
	commands = append(commands,
		command{"conflicts", "parse trusted statements into a comparable form and check the pairs", cmdConflicts},
	)
}

func cmdConflicts(args []string) error {
	fs := flag.NewFlagSet("conflicts", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	limit := fs.Int("limit", 0, "stop after this many documents, or pairs, or findings, 0 for all")
	only := fs.String("doc", "", "one document, for a trial run before a campaign")
	scope := fs.String("campaign", "", "restrict the parse pass to a named campaign")
	workers := fs.String("parallel", "auto", "worker count, or auto")
	dryRun := fs.Bool("dry-run", false, "print the queue, call no model")
	corrections := fs.Int("max-corrections", 2, "bounded retries on invalid model output")
	perMutation := fs.Int("per-mutation", 20, "benchmark pairs per mutation")
	rule := fs.String("rule", "", "restrict the listing to one rule")
	judge := fs.Bool("judge", true, "ask the model whether two sets of circumstances can hold at once")
	sub, rest, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	dir := s.Conflict()

	switch sub {
	case "prompt":
		return conflictPrompt(s, arg(rest, 0))
	case "parse":
		return conflictParse(s, dir, breadth{
			scope: *scope, only: *only, limit: *limit, workers: *workers, dryRun: *dryRun,
		}, *corrections)
	case "check":
		return conflictCheck(s, dir, *judge, *corrections)
	case "bench":
		return conflictBench(s, dir, *perMutation, *judge, *corrections)
	case "baseline":
		return conflictBaseline(s, dir, *limit, *corrections)
	case "explain":
		return conflictExplain(s, dir, *limit, *corrections)
	case "list":
		return conflictList(dir, *rule, *limit)
	case "report":
		return conflictReport(dir)
	default:
		return fmt.Errorf("usage: luatdo conflicts prompt|parse|check|bench|baseline|explain|list|report")
	}
}

// trustedByDoc groups the trusted statements by the instrument they came from,
// and returns the instruments in a fixed order.
//
// The document is the unit the parse pass commits, for the reason every model
// pass in this repo works that way: a run that dies halfway has to leave the
// documents it did not finish in the queue, and a per statement artifact makes
// a thousand files per instrument to discover that with.
func trustedByDoc(s *store.Store) (map[string][]norm.Record, []string, error) {
	records, err := loadTrusted(s)
	if err != nil {
		return nil, nil, err
	}
	byDoc := map[string][]norm.Record{}
	for _, r := range records {
		if r.DocID == "" {
			continue
		}
		byDoc[r.DocID] = append(byDoc[r.DocID], r)
	}
	ids := make([]string, 0, len(byDoc))
	for id := range byDoc {
		ids = append(ids, id)
		sort.Slice(byDoc[id], func(i, j int) bool { return byDoc[id][i].ID < byDoc[id][j].ID })
	}
	sort.Strings(ids)
	return byDoc, ids, nil
}

// conflictPrompt prints the exact prompt for one statement and calls nothing.
func conflictPrompt(s *store.Store, statementID string) error {
	if statementID == "" {
		return fmt.Errorf("usage: luatdo conflicts prompt <statement-id>")
	}
	records, err := loadTrusted(s)
	if err != nil {
		return err
	}
	for i := range records {
		r := &records[i]
		if r.ID != statementID {
			continue
		}
		text := ""
		if doc, derr := loadDoc(s, r.ProvisionID); derr == nil {
			text = provisionTexts(doc)[r.ProvisionID]
		}
		p := &conflict.Parser{}
		fmt.Println(p.Instructions())
		fmt.Println(conflict.ParsePrompt(r, text))
		if _, ok := conflict.Draft(r); !ok {
			fmt.Printf("this statement is a %s, which states no modality, so no model would be called for it\n",
				r.Statement.Type)
		}
		return nil
	}
	return fmt.Errorf("no trusted statement %s", statementID)
}

// conflictParse maps every comparable trusted statement onto a Form.
//
// This is the only pass in the milestone that calls a model over the corpus, and
// it calls it once per statement rather than once per pair. That is the whole
// economic argument for the design: 677 comparable statements is 677 calls paid
// once, and the pairs it makes comparable are quadratic in that number and cost
// nothing to check.
func conflictParse(s *store.Store, dir string, b breadth, corrections int) error {
	byDoc, ids, err := trustedByDoc(s)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("no trusted statements, run luatdo build first")
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	p := &conflict.Parser{Completer: eng.completer, Model: eng.model, MaxCorrections: corrections}
	fmt.Printf("conflicts: parsing with %s from %s\n", eng.model, eng.source)

	b.name, b.store = "conflicts parse", s
	b.done = func(docID string) bool {
		_, err := os.Stat(conflict.FormPath(dir, docID))
		return err == nil
	}
	var mu sync.Mutex
	dropped := 0
	summary, err := b.run(ids, func(ctx context.Context, docID string) (campaign.Outcome, error) {
		var out campaign.Outcome
		texts := map[string]string{}
		if doc, derr := loadDoc(s, docID); derr == nil {
			texts = provisionTexts(doc)
		}
		var forms []*conflict.Form
		records := byDoc[docID]
		for i := range records {
			r := &records[i]
			f, u, perr := p.Parse(ctx, r, texts[r.ProvisionID])
			if f == nil && perr == nil {
				// A definition, a procedure step or an exception. No call was
				// made for it and none should be counted.
				continue
			}
			out.Calls++
			out.Usage = addAPIUsage(out.Usage, u)
			if perr != nil {
				if ctx.Err() != nil {
					return out, perr
				}
				// One statement, not the instrument. An earlier version of this
				// pass failed the document here, and on the real store that cost
				// five instruments of nine over the wording of one statement in
				// each, with every call before it already paid for. A statement
				// with no canonical form is missing from every comparison, which
				// is the same thing that happens to a statement in a document
				// that was thrown away, except that this one is named and counted.
				fmt.Fprintf(os.Stderr, "  %v\n", perr)
				mu.Lock()
				dropped++
				mu.Unlock()
				continue
			}
			forms = append(forms, f)
		}
		if out.Calls == 0 {
			// Nothing in this instrument states a modality. That is coverage
			// rather than failure, and the file is still written so the document
			// does not come back.
			out.Skipped = true
		}
		out.Produced = len(forms)
		return out, conflict.WriteForms(dir, docID, forms)
	})
	if err != nil {
		return err
	}
	if dropped > 0 {
		fmt.Printf("%d statements the model would not put in canonical form, left out of every comparison\n", dropped)
	}
	reportRoutes(eng)
	if summary.Done > 0 {
		fmt.Println("nothing is compared yet, run luatdo conflicts check")
	}
	return nil
}

// forms is the parsed corpus with everything the parse pass could not know
// filled in around it.
type forms struct {
	all  []*conflict.Form
	docs *conflict.Docs
	// dated counts forms whose interval came from the version graph, and
	// approximate counts those that fell back to the instrument's effective
	// date.
	dated, approximate, undated int
}

// loadForms reads the forms and stamps their validity intervals.
func loadForms(s *store.Store, dir string) (*forms, error) {
	all, err := conflict.AllForms(dir)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no forms, run luatdo conflicts parse first")
	}
	f := &forms{all: all, docs: &conflict.Docs{Types: map[string]string{}, Effectives: map[string]string{}}}
	seen := map[string]bool{}
	for _, form := range all {
		if form.DocID == "" || seen[form.DocID] {
			continue
		}
		seen[form.DocID] = true
		doc, derr := loadDoc(s, form.DocID)
		if derr != nil {
			continue
		}
		f.docs.Types[doc.ID] = doc.DocType
		if d := law.ISODate(doc.EffectiveFrom); d != "" {
			f.docs.Effectives[doc.ID] = d
		}
	}

	var view *temporal.View
	if layer, lerr := temporal.ReadLayer(s.Temporal()); lerr == nil && layer != nil {
		view = temporal.NewView(layer)
	}
	f.stamp(view)
	return f, nil
}

// stamp fills each form's validity interval, preferring the version graph and
// falling back to the instrument's effective date.
//
// The interval taken from the version graph is the union of the provision's
// versions, from the first version's start to the last one's end. That is
// deliberately wider than the truth. A statement was extracted from one version
// of the text and nothing records which, so narrowing the interval to a guessed
// version would drop pairs on a guess. A union can only keep a pair that ought
// to have been dropped, which the reader sees, rather than drop one that ought
// to have been kept, which nobody sees.
func (f *forms) stamp(view *temporal.View) {
	for _, form := range f.all {
		if view != nil {
			if vs := view.Versions(form.ProvisionID); len(vs) > 0 {
				form.Scope.From, form.Scope.To = vs[0].From, vs[len(vs)-1].To
				f.dated++
				continue
			}
		}
		if d := f.docs.Effectives[form.DocID]; d != "" {
			form.Scope.From, form.Scope.To = d, ""
			f.approximate++
			continue
		}
		f.undated++
	}
}

func (f *forms) String() string {
	return fmt.Sprintf("%d forms, %d dated from the version graph, %d from the instrument's effective date, %d undated\n",
		len(f.all), f.dated, f.approximate, f.undated)
}

// conflictCheck compares every pair and writes the report. Nothing here calls a
// model, which is the point of the milestone.
func conflictCheck(s *store.Store, dir string, judge bool, corrections int) error {
	f, err := loadForms(s, dir)
	if err != nil {
		return err
	}
	fmt.Print(f)

	material := conflict.Materials(f.all)
	fmt.Print(material)

	report := conflict.Check(f.all, f.docs)
	if judge {
		if err := adjudicate(report, corrections); err != nil {
			return err
		}
	}
	if err := conflict.WriteReport(dir, report); err != nil {
		return err
	}
	fmt.Print(report)

	noise := conflict.NoiseFloor(f.all, f.docs)
	fmt.Print(noise)
	if err := conflict.WriteSummary(dir, conflict.Summarize(report, noise, material)); err != nil {
		return err
	}
	if len(report.Findings) > 0 {
		fmt.Println("run luatdo conflicts list to read them, and luatdo conflicts explain to put them into Vietnamese")
	}
	return nil
}

// adjudicate asks the model about the findings containment could not place, and
// calls nothing when there are none.
//
// The check pass is otherwise model free, and this keeps it that way for the
// common case: a run that proved containment on everything it found never opens
// an engine, so the pass still works on a machine with no route file. A run that
// has pairs to ask about and cannot reach a model says so and keeps them, which
// costs precision and never costs a conflict.
func adjudicate(report *conflict.Report, corrections int) error {
	pending := 0
	for i := range report.Findings {
		if report.Findings[i].Circumstances == conflict.CircumstancesUnknown {
			pending++
		}
	}
	if pending == 0 {
		return nil
	}
	eng, err := openEngine()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %d findings need a judge and none is reachable, they keep the weaker label: %v\n", pending, err)
		return nil
	}
	adj := &conflict.Adjudicator{Completer: eng.completer, Model: eng.model, MaxCorrections: corrections}
	fmt.Printf("conflicts: %s from %s judges %d pairs containment could not place\n", eng.model, eng.source, pending)

	ctx, stop := drainOnSignal(os.Stderr, "draining, finishing the pair in flight, signal again to abort")
	defer stop()

	asked, dropped, failed, jerr := conflict.Adjudicate(ctx, report, adj)
	if jerr != nil {
		fmt.Fprintf(os.Stderr, "  %v\n", jerr)
	}
	fmt.Printf("%d pairs judged, %d dropped as never triggered together, %d unanswered\n", asked, dropped, failed)
	fmt.Printf("usage %d input, %d output, %d total tokens over %d calls\n",
		adj.Usage.InputTokens, adj.Usage.OutputTokens, adj.Usage.TotalTokens, adj.Calls)
	reportRoutes(eng)
	return nil
}

// conflictBench grows the generated benchmark from the real forms and grades the
// checker on it.
//
// The judge is optional and the flag says which number is being asked for.
// Without it the grade is the deterministic checker alone, which is the honest
// figure for what the code does on its own. With it, every pair the code could
// not place on containment costs one call, and those are the pairs where the
// checker's whole error lives.
func conflictBench(s *store.Store, dir string, perMutation int, judge bool, corrections int) error {
	f, err := loadForms(s, dir)
	if err != nil {
		return err
	}
	cases := conflict.Build(f.all, perMutation)
	if len(cases) == 0 {
		return fmt.Errorf("no comparable form could carry a mutation, so there is no benchmark to grade")
	}

	ctx := context.Background()
	var j conflict.Judge
	var adj *conflict.Adjudicator
	if judge {
		eng, eerr := openEngine()
		if eerr != nil {
			return eerr
		}
		adj = &conflict.Adjudicator{Completer: eng.completer, Model: eng.model, MaxCorrections: corrections}
		j = adj
		fmt.Printf("conflicts: %s from %s judges the pairs containment could not place\n", eng.model, eng.source)
		var stop func()
		ctx, stop = drainOnSignal(os.Stderr, "draining, finishing the pair in flight, signal again to abort")
		defer stop()
		defer func() { reportRoutes(eng) }()
	}

	grade := conflict.GradeCases(ctx, cases, f.docs, j)
	if err := conflict.WriteBench(dir, &conflict.Bench{
		PerMutation: perMutation, Judged: judge, Cases: cases, Grade: grade,
	}); err != nil {
		return err
	}
	fmt.Print(grade)
	if adj != nil {
		fmt.Printf("usage %d input, %d output, %d total tokens over %d calls\n",
			adj.Usage.InputTokens, adj.Usage.OutputTokens, adj.Usage.TotalTokens, adj.Calls)
	}
	fmt.Println("run luatdo conflicts baseline to score direct prompting over the same pairs")
	return nil
}

// conflictBaseline scores the approach this package argues against, over the
// pairs the benchmark already graded the checker on.
func conflictBaseline(s *store.Store, dir string, limit, corrections int) error {
	bench, err := conflict.ReadBench(dir)
	if err != nil {
		return err
	}
	if bench == nil || len(bench.Cases) == 0 {
		return fmt.Errorf("no benchmark, run luatdo conflicts bench first")
	}
	cases := sampleCases(bench.Cases, limit)
	eng, err := openEngine()
	if err != nil {
		return err
	}
	b := &conflict.Baseline{Completer: eng.completer, Model: eng.model, MaxCorrections: corrections}
	fmt.Printf("conflicts: asking %s from %s about %d pairs, one call each\n", eng.model, eng.source, len(cases))

	ctx, stop := drainOnSignal(os.Stderr, "draining, finishing the pair in flight, signal again to abort")
	defer stop()
	grade, gerr := conflict.GradeBaseline(ctx, cases, b)
	if grade != nil {
		if werr := conflict.WriteBaseline(dir, grade); werr != nil {
			return werr
		}
		fmt.Print(grade)
	}
	reportRoutes(eng)
	if gerr != nil {
		return gerr
	}
	if bench.Grade != nil {
		fmt.Printf("the checker scored precision %.2f and recall %.2f over the same construction\n",
			bench.Grade.Precision, bench.Grade.Recall)
	}
	return nil
}

// sampleCases thins the case list without dropping a whole mutation.
//
// The cases come out of Build grouped by mutation, so taking the first n would
// put the baseline through one shape of pair and call the result a score. An
// even stride keeps every mutation in proportion.
func sampleCases(cases []conflict.Case, n int) []conflict.Case {
	if n <= 0 || n >= len(cases) {
		return cases
	}
	out := make([]conflict.Case, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, cases[i*len(cases)/n])
	}
	return out
}

// conflictExplain fills the sentence a person reads under each finding.
//
// It runs after detection and cannot change it. A finding whose explanation the
// model would not produce keeps its place in the report with an empty one,
// because the explanation is a courtesy and the finding is the result.
func conflictExplain(s *store.Store, dir string, limit, corrections int) error {
	report, err := conflict.ReadReport(dir)
	if err != nil {
		return err
	}
	if report == nil || len(report.Findings) == 0 {
		return fmt.Errorf("no findings to explain, run luatdo conflicts check first")
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	e := &conflict.Explainer{Completer: eng.completer, Model: eng.model, MaxCorrections: corrections}

	ctx, stop := drainOnSignal(os.Stderr, "draining, finishing the finding in flight, signal again to abort")
	defer stop()

	var usage api.Usage
	calls, written, failed := 0, 0, 0
	for i := range report.Findings {
		f := &report.Findings[i]
		if f.Explanation != "" {
			continue
		}
		if limit > 0 && calls >= limit {
			break
		}
		text, u, xerr := e.Explain(ctx, f)
		calls++
		usage = addAPIUsage(usage, u)
		if xerr != nil {
			if ctx.Err() != nil {
				break
			}
			fmt.Fprintf(os.Stderr, "  %v\n", xerr)
			failed++
			continue
		}
		f.Explanation = text
		written++
	}
	if err := conflict.WriteReport(dir, report); err != nil {
		return err
	}
	fmt.Printf("%d findings, %d explained this run, %d the model gave nothing usable for\n",
		len(report.Findings), written, failed)
	fmt.Printf("usage %d input, %d output, %d total tokens\n", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
	reportRoutes(eng)
	return nil
}

// conflictList prints the findings for reading.
func conflictList(dir, rule string, limit int) error {
	report, err := conflict.ReadReport(dir)
	if err != nil {
		return err
	}
	if report == nil {
		return fmt.Errorf("nothing checked yet, run luatdo conflicts check first")
	}
	shown := 0
	for i := range report.Findings {
		f := &report.Findings[i]
		if rule != "" && f.Rule != rule {
			continue
		}
		if limit > 0 && shown >= limit {
			break
		}
		shown++
		fmt.Print(f)
		if r := conflict.Describe(f.Rank, f.A, f.B); r != "" {
			fmt.Printf("  ranking: %s\n", r)
		}
		if f.Explanation != "" {
			fmt.Printf("  %s\n", f.Explanation)
		}
		fmt.Println()
	}
	fmt.Printf("%d of %d findings shown\n", shown, len(report.Findings))
	fmt.Println("a pair here is a pair for a person to read, not a ruling that the law contradicts itself")
	return nil
}

// conflictReport prints every artifact this milestone produces, which is the
// whole evaluation in one place.
func conflictReport(dir string) error {
	report, err := conflict.ReadReport(dir)
	if err != nil {
		return err
	}
	if report != nil {
		fmt.Print(report)
	} else {
		fmt.Println("nothing checked yet, run luatdo conflicts check")
	}
	bench, err := conflict.ReadBench(dir)
	if err != nil {
		return err
	}
	if bench != nil && bench.Grade != nil {
		fmt.Println()
		fmt.Print(bench.Grade)
	} else {
		fmt.Println("\nno benchmark, run luatdo conflicts bench")
	}
	base, err := conflict.ReadBaseline(dir)
	if err != nil {
		return err
	}
	if base != nil {
		fmt.Println()
		fmt.Print(base)
	} else {
		fmt.Println("\nno baseline, run luatdo conflicts baseline")
	}
	return nil
}
