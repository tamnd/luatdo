package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/luatdo/anchor"
	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/campaign"
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/store"
)

func init() {
	commands = append(commands,
		command{"concepts", "read definitions, cluster them, and merge by human decision", cmdConcepts},
	)
}

func cmdConcepts(args []string) error {
	fs := flag.NewFlagSet("concepts", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	n := fs.Int("n", 200, "units to draw for the gold set")
	seed := fs.String("seed", "m7", "sampling seed, so a draw is reproducible")
	limit := fs.Int("limit", 0, "stop after this many documents, 0 for all")
	compare := fs.Bool("compare", false, "ask the model to compare each queued pair")
	who := fs.String("by", "", "who is deciding, recorded on the edge")
	scope := fs.String("campaign", "", "restrict the reading pass to a named campaign")
	only := fs.String("doc", "", "one document, for a trial run before a campaign")
	workers := fs.String("parallel", "auto", "worker count, or auto")
	dryRun := fs.Bool("dry-run", false, "print the queue, call no model")
	sub, rest, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}

	switch sub {
	case "sample":
		return conceptSample(s, *n, *seed)
	case "prompt":
		return conceptPrompt(s, arg(rest, 0))
	case "read":
		return conceptRead(s, breadth{
			scope: *scope, only: *only, limit: *limit, workers: *workers, dryRun: *dryRun,
		})
	case "cluster":
		return conceptCluster(s, *compare)
	case "queue":
		return conceptQueue(s)
	case "answer":
		return conceptAnswer(s, *who, rest)
	case "build":
		return conceptBuild(s)
	case "score":
		return conceptScore(s)
	default:
		return fmt.Errorf("usage: luatdo concepts sample|prompt|read|cluster|queue|answer|build|score")
	}
}

// eachUnit streams the anchored definition units. The anchor stage wrote one
// file per document over a hundred thousand documents, so this reads them one
// at a time rather than holding the corpus.
func eachUnit(s *store.Store, visit func(*anchor.Result) error) error {
	entries, err := os.ReadDir(s.Anchor())
	if err != nil {
		return fmt.Errorf("no anchored documents, run luatdo anchor first: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && e.Name() != anchor.SummaryFile {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var r anchor.Result
		if err := store.ReadJSON(filepath.Join(s.Anchor(), name), &r); err != nil {
			return err
		}
		if len(r.Units) == 0 {
			continue
		}
		if err := visit(&r); err != nil {
			return err
		}
	}
	return nil
}

func scopeOf(r *anchor.Result, u *anchor.Unit) *anchor.Scope {
	for i := range r.Scopes {
		if r.Scopes[i].ID == u.ScopeID {
			return &r.Scopes[i]
		}
	}
	return nil
}

// conceptSample draws the units to annotate by hand. It is stratified by
// document type and ranked by a hash of the seed, the same way the subject
// sampler is, so the draw is reproducible on every machine in the fleet without
// a random source.
//
// The units are written out with their text and nothing else. They carry no
// model output on purpose: an annotation written next to a prediction measures
// agreement with that prediction rather than accuracy.
func conceptSample(s *store.Store, n int, seed string) error {
	type candidate struct {
		unit    anchor.Unit
		docType string
		rank    uint64
	}
	byType := map[string][]candidate{}
	total := 0
	if err := eachUnit(s, func(r *anchor.Result) error {
		for _, u := range r.Units {
			sum := sha256.Sum256([]byte(seed + "\x00" + u.ID))
			byType[r.DocType] = append(byType[r.DocType], candidate{
				unit:    u,
				docType: r.DocType,
				rank:    binary.BigEndian.Uint64(sum[:8]),
			})
			total++
		}
		return nil
	}); err != nil {
		return err
	}
	if total == 0 {
		return fmt.Errorf("no definition units found, run luatdo anchor first")
	}

	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)

	// One per type first, then the rest in proportion to how many units each
	// type holds. A gold set that is all laws says nothing about the circulars
	// and provincial decisions that are most of the corpus.
	quota := map[string]int{}
	left := n
	for _, t := range types {
		if left == 0 {
			break
		}
		quota[t] = 1
		left--
	}
	given := 0
	for _, t := range types {
		share := min(left*len(byType[t])/total, len(byType[t])-quota[t])
		quota[t] += share
		given += share
	}
	// Integer division loses a seat or two. They go to the largest types, in
	// order, until the draw is the size that was asked for, because a sample
	// command that quietly returns 198 of the 200 it was asked for makes every
	// number computed from it slightly wrong.
	bySize := append([]string(nil), types...)
	sort.SliceStable(bySize, func(i, j int) bool { return len(byType[bySize[i]]) > len(byType[bySize[j]]) })
	for given < left {
		placed := false
		for _, t := range bySize {
			if given >= left {
				break
			}
			if quota[t] >= len(byType[t]) {
				continue
			}
			quota[t]++
			given++
			placed = true
		}
		if !placed {
			break
		}
	}

	var out []concept.Gold
	for _, t := range types {
		group := byType[t]
		sort.Slice(group, func(i, j int) bool {
			if group[i].rank != group[j].rank {
				return group[i].rank < group[j].rank
			}
			return group[i].unit.ID < group[j].unit.ID
		})
		for _, c := range group[:min(quota[t], len(group))] {
			out = append(out, concept.Gold{
				UnitID:   c.unit.ID,
				DocID:    c.unit.DocID,
				ScopeID:  c.unit.ScopeID,
				TextHash: c.unit.TextHash,
				Text:     c.unit.Text,
			})
		}
	}

	path := filepath.Join(s.Concepts(), "gold_candidates.jsonl")
	if err := os.MkdirAll(s.Concepts(), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	for _, g := range out {
		if err := enc.Encode(g); err != nil {
			return err
		}
	}
	if err := store.WriteFile(path, []byte(b.String())); err != nil {
		return err
	}
	fmt.Printf("drew %d of %d units across %d document types, seed %q\n", len(out), total, len(types), seed)
	fmt.Printf("wrote %s, annotate it by hand into %s before running the reading pass\n",
		path, filepath.Join(s.Concepts(), concept.GoldFile))
	return nil
}

// conceptPrompt prints the exact prompt for one unit without calling anything.
func conceptPrompt(s *store.Store, unitID string) error {
	if unitID == "" {
		return fmt.Errorf("usage: luatdo concepts prompt <unit-id>")
	}
	var found bool
	if err := eachUnit(s, func(r *anchor.Result) error {
		for i := range r.Units {
			if r.Units[i].ID != unitID {
				continue
			}
			found = true
			reader := &concept.Reader{}
			fmt.Println(reader.Instructions())
			fmt.Println(concept.Prompt(&r.Units[i], scopeOf(r, &r.Units[i])))
		}
		return nil
	}); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("unit %s not found", unitID)
	}
	return nil
}

// anchoredDocs lists the documents that hold at least one definition unit.
//
// The anchor stage wrote a file per document over a hundred thousand of them
// and seven thousand hold units, so the queue is built by reading the ones a
// campaign asks about rather than all of them. Without a campaign there is
// nothing to narrow by and the whole directory is read, which takes about a
// minute and is the price of an accurate total.
func anchoredDocs(s *store.Store, ids []string) ([]string, error) {
	if len(ids) > 0 {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			var r anchor.Result
			err := store.ReadJSON(filepath.Join(s.Anchor(), law.FileName(id)), &r)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if len(r.Units) > 0 {
				out = append(out, r.DocID)
			}
		}
		return out, nil
	}
	var out []string
	err := eachUnit(s, func(r *anchor.Result) error {
		out = append(out, r.DocID)
		return nil
	})
	return out, err
}

func conceptRead(s *store.Store, b breadth) error {
	eng, err := openEngine()
	if err != nil {
		return fmt.Errorf("the reading pass needs a model: %w", err)
	}
	reader := &concept.Reader{Completer: eng.completer, Model: eng.model, MaxCorrections: 2}

	// A campaign narrows the queue before anything is read off disk, so a
	// campaign pass does not pay for a scan of the whole anchor store.
	var ids []string
	if b.scope != "" {
		sc, err := campaign.LookupScope(b.scope)
		if err != nil {
			return err
		}
		_, inScope, err := campaignDocs(s, sc)
		if err != nil {
			return err
		}
		for id := range inScope {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}
	candidates, err := anchoredDocs(s, ids)
	if err != nil {
		return err
	}
	b.name, b.store = "concepts read", s
	b.done = func(docID string) bool {
		_, err := os.Stat(concept.JobPath(s.Concepts(), docID))
		return err == nil
	}

	summary, err := b.run(candidates, func(ctx context.Context, docID string) (campaign.Outcome, error) {
		var r anchor.Result
		if err := store.ReadJSON(filepath.Join(s.Anchor(), law.FileName(docID)), &r); err != nil {
			return campaign.Outcome{}, err
		}
		var out campaign.Outcome
		var jobs []concept.Job
		for i := range r.Units {
			job, err := reader.Read(ctx, &r.Units[i], scopeOf(&r, &r.Units[i]))
			if job != nil {
				out.Usage = addAPIUsage(out.Usage, job.Usage)
				out.Calls += len(job.Attempts)
				out.Produced += len(job.TermUses)
				jobs = append(jobs, *job)
			}
			if err != nil {
				// The units already read are thrown away with the document.
				// Writing them would mark the document read, and a document
				// read to unit four of eleven is worse than one not read at
				// all, because nothing will ever come back for the other seven.
				return campaign.Outcome{}, err
			}
		}
		if len(jobs) == 0 {
			out.Skipped = true
			return out, nil
		}
		return out, concept.WriteJob(s.Concepts(), jobs)
	})
	if err != nil {
		return err
	}
	fmt.Printf("usage %d input, %d output, %d total tokens\n",
		summary.Usage.InputTokens, summary.Usage.OutputTokens, summary.Usage.TotalTokens)
	reportRoutes(eng)
	return nil
}

func conceptCluster(s *store.Store, compare bool) error {
	terms, err := loadTermUses(s)
	if err != nil {
		return err
	}
	if len(terms) == 0 {
		return fmt.Errorf("no readings to cluster, run luatdo concepts read first")
	}
	byID := map[string]concept.TermUse{}
	for _, t := range terms {
		byID[t.ID] = t
	}

	links := concept.Links(terms)
	clusters := concept.Clusters(terms, links)
	if err := concept.WriteClusters(s.Concepts(), clusters); err != nil {
		return err
	}

	existing, err := concept.ReadQuestions(s.Concepts())
	if err != nil {
		return err
	}
	answers, err := concept.ReadAnswers(s.Concepts())
	if err != nil {
		return err
	}
	settled := map[[2]string]bool{}
	for _, a := range answers {
		settled[[2]string{a.A, a.B}] = true
	}
	for _, q := range existing {
		settled[[2]string{q.A.ID, q.B.ID}] = true
	}

	var comparer *concept.Comparer
	var eng *engine
	if compare {
		eng, err = openEngine()
		if err != nil {
			return fmt.Errorf("comparing needs a model: %w", err)
		}
		comparer = &concept.Comparer{Completer: eng.completer, Model: eng.model, MaxCorrections: 2}
	}

	now := concept.Now(time.Now())
	var usage api.Usage
	var questions []concept.Question
	for _, c := range clusters {
		for _, pair := range c.Pairs() {
			if settled[[2]string{pair[0], pair[1]}] {
				continue
			}
			a, b := byID[pair[0]], byID[pair[1]]
			q := concept.Question{ClusterID: c.ID, A: a, B: b, Bases: c.Bases, At: now}
			if comparer != nil {
				cmp, u, err := comparer.Compare(context.Background(), &a, &b)
				usage = addAPIUsage(usage, u)
				if err != nil {
					return err
				}
				q.Comparison = cmp
			}
			questions = append(questions, q)
		}
	}
	if err := concept.AskQuestions(s.Concepts(), questions); err != nil {
		return err
	}

	fmt.Printf("%d term uses, %d links, %d clusters, %d new questions\n",
		len(terms), len(links), len(clusters), len(questions))
	if comparer != nil {
		fmt.Printf("usage %d input, %d output, %d total tokens\n", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
		reportRoutes(eng)
	}
	fmt.Println("nothing is merged until somebody answers, run luatdo concepts queue")
	return nil
}

func conceptQueue(s *store.Store) error {
	questions, err := concept.ReadQuestions(s.Concepts())
	if err != nil {
		return err
	}
	answers, err := concept.ReadAnswers(s.Concepts())
	if err != nil {
		return err
	}
	pending := concept.Pending(questions, answers)
	for _, q := range pending {
		fmt.Printf("\n%s\n", q.ClusterID)
		fmt.Printf("  A %s\n    %s\n", q.A.ID, q.A.Quote)
		fmt.Printf("  B %s\n    %s\n", q.B.ID, q.B.Quote)
		if q.Comparison != nil {
			fmt.Printf("  model says %s: %s\n", q.Comparison.Relation, q.Comparison.Rationale)
		}
		fmt.Printf("  luatdo concepts answer -by <you> %s %s <same|broader|narrower|differs|defer> <rationale>\n", q.A.ID, q.B.ID)
	}
	fmt.Printf("\n%d pending of %d asked, %d answered\n", len(pending), len(questions), len(answers))
	return nil
}

func conceptAnswer(s *store.Store, who string, args []string) error {
	if len(args) < 4 || who == "" {
		return fmt.Errorf("usage: luatdo concepts answer -by <you> <a> <b> <same|broader|narrower|differs|defer> <rationale>")
	}
	rationale := strings.Join(args[3:], " ")
	if strings.TrimSpace(rationale) == "" {
		return fmt.Errorf("a merge with no stated reason cannot be reviewed later, say why")
	}
	a := concept.Answer{
		A: args[0], B: args[1], Verdict: args[2], Rationale: rationale,
		DecidedBy: who, DecidedAt: concept.Now(time.Now()),
	}
	switch a.Verdict {
	case concept.RelationSame, concept.RelationBroader, concept.RelationNarrower,
		concept.RelationDiffers, concept.VerdictDefer:
	default:
		return fmt.Errorf("verdict %q is not one of same, broader, narrower, differs, defer", a.Verdict)
	}
	if err := concept.RecordAnswers(s.Concepts(), []concept.Answer{a}); err != nil {
		return err
	}
	fmt.Printf("recorded %s %s %s by %s\n", a.A, a.Verdict, a.B, who)
	return nil
}

func conceptBuild(s *store.Store) error {
	terms, err := loadTermUses(s)
	if err != nil {
		return err
	}
	answers, err := concept.ReadAnswers(s.Concepts())
	if err != nil {
		return err
	}
	layer := concept.Apply(terms, answers)

	// The invariants are a build failure and not a warning. A concept layer
	// that is nearly consistent is a concept layer nobody can query.
	if problems := layer.Check(); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "  "+p)
		}
		return fmt.Errorf("%d concept layer invariants failed", len(problems))
	}
	if err := store.WriteJSON(filepath.Join(s.Concepts(), concept.LayerFile), layer); err != nil {
		return err
	}
	fmt.Printf("%d term uses, %d concepts, %d memberships, %d differences\n",
		len(layer.TermUses), len(layer.Concepts), len(layer.Memberships), len(layer.Differences))
	return nil
}

func conceptScore(s *store.Store) error {
	gold, err := concept.ReadGold(s.Concepts())
	if err != nil {
		return err
	}
	if len(gold) == 0 {
		return fmt.Errorf("no gold set at %s, draw one with luatdo concepts sample and annotate it by hand",
			filepath.Join(s.Concepts(), concept.GoldFile))
	}
	pairs, err := concept.ReadGoldPairs(s.Concepts())
	if err != nil {
		return err
	}
	if bad := concept.CheckGold(gold, pairs); len(bad) > 0 {
		for _, b := range bad {
			fmt.Fprintln(os.Stderr, "gold:", b)
		}
		return fmt.Errorf("%d problems in the gold set, fix the annotations before scoring against them", len(bad))
	}
	jobs, err := loadJobs(s)
	if err != nil {
		return err
	}
	fmt.Println(concept.Score(gold, jobs))

	if len(pairs) == 0 {
		fmt.Println("merge      no annotated pairs")
		return nil
	}
	questions, err := concept.ReadQuestions(s.Concepts())
	if err != nil {
		return err
	}
	var comparisons []concept.Comparison
	for _, q := range questions {
		if q.Comparison != nil {
			comparisons = append(comparisons, *q.Comparison)
		}
	}
	fmt.Println(concept.ScoreMerges(pairs, comparisons))
	return nil
}

func loadJobs(s *store.Store) ([]concept.Job, error) {
	entries, err := os.ReadDir(s.Concepts())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []concept.Job
	for _, e := range entries {
		// One directory holds the reading jobs, the discovery sightings, the
		// mention reports and the built layer, and only the reading jobs are
		// jobs. The prefixes are how they are told apart, and a file that is
		// not a job read as one would silently unmarshal into an empty job.
		if !strings.HasSuffix(e.Name(), ".json") || e.Name() == concept.LayerFile ||
			strings.HasPrefix(e.Name(), concept.SightingPrefix) ||
			strings.HasPrefix(e.Name(), concept.MentionPrefix) ||
			e.Name() == concept.TaggerFile || e.Name() == concept.DiscoverySummary {
			continue
		}
		var jobs []concept.Job
		if err := store.ReadJSON(filepath.Join(s.Concepts(), e.Name()), &jobs); err != nil {
			return nil, err
		}
		out = append(out, jobs...)
	}
	return out, nil
}

func loadTermUses(s *store.Store) ([]concept.TermUse, error) {
	jobs, err := loadJobs(s)
	if err != nil {
		return nil, err
	}
	return concept.TermUses(jobs), nil
}

func addAPIUsage(a, b api.Usage) api.Usage {
	a.InputTokens += b.InputTokens
	a.CachedInputTokens += b.CachedInputTokens
	a.CacheWriteTokens += b.CacheWriteTokens
	a.OutputTokens += b.OutputTokens
	a.ReasoningTokens += b.ReasoningTokens
	a.TotalTokens += b.TotalTokens
	return a
}
