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

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/campaign"
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/event"
	"github.com/tamnd/luatdo/store"
)

func init() {
	commands = append(commands,
		command{"events", "read the acts legal text is about, chain them, and answer with them", cmdEvents},
	)
}

func cmdEvents(args []string) error {
	if len(args) > 0 && args[0] == "gold" {
		return eventGold(args[1:])
	}
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	limit := fs.Int("limit", 0, "stop after this many documents, or chains for verify, 0 for all")
	only := fs.String("doc", "", "one document, for a trial run before a campaign")
	scope := fs.String("campaign", "", "restrict the extraction pass to a named campaign")
	workers := fs.String("parallel", "auto", "worker count, or auto")
	dryRun := fs.Bool("dry-run", false, "print the queue, call no model")
	corrections := fs.Int("max-corrections", 2, "bounded retries on invalid model output")
	depth := fs.Int("depth", 3, "how far a consequence walk goes")
	sub, rest, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	dir := s.Event()

	switch sub {
	case "prompt":
		return eventPrompt(s, dir, arg(rest, 0))
	case "extract":
		return eventExtract(s, dir, breadth{
			scope: *scope, only: *only, limit: *limit, workers: *workers, dryRun: *dryRun,
		}, *corrections)
	case "build":
		return eventBuild(s, dir)
	case "verify":
		return eventVerify(dir, *limit, *corrections)
	case "ask":
		return eventAsk(dir, arg(rest, 0), arg(rest, 1), *depth)
	case "propose":
		return eventProposals(dir)
	case "ablate":
		return eventAblate(dir, *depth)
	case "report":
		return eventReport(dir)
	default:
		return fmt.Errorf("usage: luatdo events prompt|extract|build|verify|ask|propose|ablate|report|gold")
	}
}

// eventInputs is what the act pass needs that earlier milestones already built:
// the concepts a provision mentions, and the norms already read out of it.
type eventInputs struct {
	labels map[string]string
	kinds  map[string]string
	norms  map[string][]event.Norm
	// normDocs is the documents a trusted norm was read out of, which is half
	// the extraction queue. The concept layer covers a different set of
	// documents from the norm layer, and a queue built from either one alone
	// silently drops the provisions the other reached.
	normDocs map[string]bool
	registry *event.Registry
}

func eventLoad(s *store.Store, dir string) (*eventInputs, error) {
	terms, err := loadTermUses(s)
	if err != nil {
		return nil, err
	}
	if len(terms) == 0 {
		return nil, fmt.Errorf("no concepts, run luatdo concepts read first")
	}
	reg, err := event.ReadRegistry(dir)
	if err != nil {
		return nil, err
	}
	in := &eventInputs{
		labels: map[string]string{}, kinds: map[string]string{},
		norms: map[string][]event.Norm{}, normDocs: map[string]bool{}, registry: reg,
	}
	for _, t := range terms {
		in.labels[t.ID] = t.LabelVI
		in.kinds[t.ID] = t.Kind
	}
	// The norms are optional. A store where the norm layer has not been built
	// still has acts in it, and refusing to read them because no statement was
	// extracted would make this pass depend on a milestone it does not need.
	records, err := loadTrusted(s)
	if err != nil {
		return in, nil
	}
	for i := range records {
		r := &records[i]
		if !r.Trusted() || r.ProvisionID == "" {
			continue
		}
		n := event.Norm{StatementID: r.ID, Type: r.Statement.Type, Action: r.Statement.Action.Text}
		if r.Statement.Sanction != nil {
			n.Sanction = r.Statement.Sanction.Text
		}
		in.norms[r.ProvisionID] = append(in.norms[r.ProvisionID], n)
		if r.DocID != "" {
			in.normDocs[r.DocID] = true
		}
	}
	for id := range in.norms {
		sort.Slice(in.norms[id], func(i, j int) bool { return in.norms[id][i].StatementID < in.norms[id][j].StatementID })
	}
	return in, nil
}

// eventDocs is the extraction queue: every document the concept layer linked a
// mention in, and every document a trusted norm was read out of.
//
// The queue was the mention documents alone until the gold set was drawn, and
// the draw is what found it. The two labour instruments the sample landed on
// have norms and no linked mentions, so the pass would have skipped both of them
// whole while its own per provision test said a trusted norm was enough to read
// one. A queue narrower than the predicate it feeds is invisible: the run
// reports every document it looked at as done.
func eventDocs(s *store.Store, in *eventInputs) ([]string, error) {
	ids, err := mentionDocs(s)
	if err != nil {
		return nil, err
	}
	return mergeDocs(ids, in.normDocs), nil
}

// mergeDocs unions the two queues, without repeats and in a fixed order.
func mergeDocs(ids []string, extra map[string]bool) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids)+len(extra))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	for id := range extra {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// eventCandidates groups one document's resolved mentions by provision, as the
// bounded concept set the extractor fills participant slots from.
func eventCandidates(report *concept.MentionReport, in *eventInputs) map[string][]event.Candidate {
	out := map[string][]event.Candidate{}
	seen := map[string]bool{}
	for _, m := range report.Mentions {
		if m.TermUseID == "" || seen[m.ProvisionID+"|"+m.TermUseID] {
			continue
		}
		seen[m.ProvisionID+"|"+m.TermUseID] = true
		out[m.ProvisionID] = append(out[m.ProvisionID], event.Candidate{
			ID: m.TermUseID, LabelVI: in.labels[m.TermUseID], Kind: in.kinds[m.TermUseID],
		})
	}
	for id := range out {
		sort.Slice(out[id], func(i, j int) bool { return out[id][i].ID < out[id][j].ID })
	}
	return out
}

// eventPrompt prints the exact prompt for one provision and calls nothing.
func eventPrompt(s *store.Store, dir, provisionID string) error {
	if provisionID == "" {
		return fmt.Errorf("usage: luatdo events prompt <provision-id>")
	}
	in, err := eventLoad(s, dir)
	if err != nil {
		return err
	}
	doc, err := loadDoc(s, provisionID)
	if err != nil {
		return err
	}
	text := provisionTexts(doc)[provisionID]
	if text == "" {
		return fmt.Errorf("provision %s has no extractable text", provisionID)
	}
	var cands []event.Candidate
	if report, rerr := concept.ReadMentions(s.Concepts(), doc.ID); rerr == nil && report != nil {
		cands = eventCandidates(report, in)[provisionID]
	}
	x := &event.Extractor{Registry: in.registry}
	fmt.Println(x.Instructions())
	fmt.Println(event.Prompt(provisionID, text, cands, in.norms[provisionID]))
	if len(cands) == 0 {
		fmt.Println("this provision has no linked concepts, so any act read out of it would carry no participants")
	}
	return nil
}

// eventExtract runs the provision level read, one sighting file per document.
//
// A provision with neither a linked concept nor a trusted norm is skipped. It
// would still be read for acts, and the acts would have no parties in them and no
// norm to attach to, which is a node with a label and nothing to query it by at
// the price of a reasoning call.
func eventExtract(s *store.Store, dir string, b breadth, corrections int) error {
	in, err := eventLoad(s, dir)
	if err != nil {
		return err
	}
	docs, err := eventDocs(s, in)
	if err != nil {
		return err
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	x := &event.Extractor{
		Completer: eng.completer, Model: eng.model,
		Registry: in.registry, MaxCorrections: corrections,
	}
	fmt.Printf("events: reading with %s from %s\n", eng.model, eng.source)

	b.name, b.store = "events extract", s
	b.done = func(docID string) bool {
		_, err := os.Stat(event.SightingPath(dir, docID))
		return err == nil
	}
	summary, err := b.run(docs, func(ctx context.Context, docID string) (campaign.Outcome, error) {
		var out campaign.Outcome
		report, err := concept.ReadMentions(s.Concepts(), docID)
		if err != nil {
			return out, err
		}
		doc, err := loadDoc(s, docID)
		if err != nil {
			return out, err
		}
		texts := provisionTexts(doc)
		// A document with no mention report is read for the provisions its norms
		// reached. The acts come out of it with no parties, which is a thinner
		// read than the concept layer allows and still worth having, because the
		// norm it attaches to is what question 25 walks.
		byProvision := map[string][]event.Candidate{}
		if report != nil {
			byProvision = eventCandidates(report, in)
		}
		ids := make([]string, 0, len(texts))
		for id := range texts {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		sighting := event.Sighting{DocID: docID}
		for _, id := range ids {
			if texts[id] == "" || (len(byProvision[id]) == 0 && len(in.norms[id]) == 0) {
				continue
			}
			got, u, xerr := x.Extract(ctx, id, docID, texts[id], byProvision[id], in.norms[id])
			out.Calls++
			out.Usage = addAPIUsage(out.Usage, u)
			if xerr != nil {
				// The document fails whole. A file holding the provisions up to the
				// one that failed would take the rest out of the queue forever, and
				// the queue is the only record of what has been read.
				return out, fmt.Errorf("%s: %w", id, xerr)
			}
			sighting.Occurrences = append(sighting.Occurrences, got.Occurrences...)
			sighting.Chains = append(sighting.Chains, got.Chains...)
			sighting.Links = append(sighting.Links, got.Links...)
		}
		if out.Calls == 0 {
			out.Skipped = true
		}
		out.Produced = len(sighting.Occurrences)
		return out, event.WriteSighting(dir, sighting)
	})
	if err != nil {
		return err
	}
	reportRoutes(eng)
	if summary.Done > 0 {
		fmt.Println("nothing is folded yet, run luatdo events build")
	}
	return nil
}

// readSightings collects the raw provision level reads.
func readSightings(dir string) ([]event.Occurrence, []event.Chain, []event.Link, int, error) {
	var (
		occurrences []event.Occurrence
		chains      []event.Chain
		links       []event.Link
		docs        int
	)
	err := event.EachSighting(dir, func(s event.Sighting) error {
		docs++
		occurrences = append(occurrences, s.Occurrences...)
		chains = append(chains, s.Chains...)
		links = append(links, s.Links...)
		return nil
	})
	return occurrences, chains, links, docs, err
}

// eventBuild folds the sightings into the layer.
//
// The links are folded by dropping the ones whose act did not survive, because a
// norm pointing at a node that is not there is worse than a norm pointing at
// nothing: the first looks like an answer.
func eventBuild(s *store.Store, dir string) error {
	in, err := eventLoad(s, dir)
	if err != nil {
		return err
	}
	occurrences, rawChains, rawLinks, docs, err := readSightings(dir)
	if err != nil {
		return err
	}
	if len(occurrences) == 0 {
		return fmt.Errorf("no sightings, run luatdo events extract first")
	}
	events := event.Fold(occurrences, in.registry, event.DefaultThresholds)
	chains := event.FoldChains(rawChains, events, in.registry, event.DefaultThresholds)

	links, dropped := keepLinks(rawLinks, events)

	if err := event.WriteEvents(dir, events); err != nil {
		return err
	}
	if err := event.WriteChains(dir, chains); err != nil {
		return err
	}
	if err := event.WriteLinks(dir, links); err != nil {
		return err
	}
	if err := event.WriteRegistry(dir, in.registry); err != nil {
		return err
	}
	proposals := event.Propose(events, in.registry)
	if err := event.WriteProposals(dir, proposals); err != nil {
		return err
	}

	counts := event.Tally(events, chains, links, in.registry)
	summary := event.Summary{
		Documents: docs, Provisions: countProvisions(occurrences),
		Counts: counts, Direction: event.ScoreDirection(chains),
	}
	if err := event.WriteSummary(dir, summary); err != nil {
		return err
	}
	fmt.Printf("%d sightings over %d documents folded into %d acts and %d chains\n",
		len(occurrences), docs, len(events), len(chains))
	if dropped > 0 {
		fmt.Printf("%d norm links dropped, their act did not survive the fold\n", dropped)
	}
	fmt.Print(counts)
	fmt.Print(summary.Direction)
	if len(proposals) > 0 {
		fmt.Printf("%d proposed act types are waiting on a person, read them with luatdo events propose\n", len(proposals))
	}
	return nil
}

// keepLinks drops the norm slots whose act did not survive the fold, and returns
// the rest in a fixed order.
func keepLinks(in []event.Link, events []event.Event) ([]event.Link, int) {
	held := map[string]bool{}
	for _, e := range events {
		held[e.ID] = true
	}
	out := make([]event.Link, 0, len(in))
	dropped := 0
	for _, l := range in {
		if !held[l.EventID] {
			dropped++
			continue
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StatementID != out[j].StatementID {
			return out[i].StatementID < out[j].StatementID
		}
		return out[i].Kind < out[j].Kind
	})
	return out, dropped
}

func countProvisions(in []event.Occurrence) int {
	seen := map[string]bool{}
	for _, o := range in {
		seen[o.Evidence.ProvisionID] = true
	}
	return len(seen)
}

// eventVerify runs the blind second pass over the folded chains.
//
// It is blind for the reason the relation layer's is: M4 shipped 75252 backwards
// AMENDS edges, and a verifier shown the claim verifies the claim rather than the
// text, which is how a second pass agrees with the first for free.
func eventVerify(dir string, limit, corrections int) error {
	events, err := event.ReadEvents(dir)
	if err != nil {
		return err
	}
	chains, err := event.ReadChains(dir)
	if err != nil {
		return err
	}
	if len(chains) == 0 {
		return fmt.Errorf("no chains to verify, run luatdo events build first")
	}
	label := map[string]string{}
	for _, e := range events {
		label[e.ID] = e.LabelVI
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	v := &event.Verifier{Completer: eng.completer, Model: eng.model, MaxCorrections: corrections}
	fmt.Printf("events: %s from %s reads %d chains without being shown the claim\n", eng.model, eng.source, len(chains))

	ctx, stop := drainOnSignal(os.Stderr, "draining, finishing the chain in flight, signal again to abort")
	defer stop()

	var usage api.Usage
	checked := 0
	for i := range chains {
		if limit > 0 && checked >= limit {
			break
		}
		if len(chains[i].Evidence) == 0 {
			continue
		}
		checked++
		verdict, u, verr := v.Verify(ctx, chains[i], label[chains[i].FromID], label[chains[i].ToID])
		usage = addAPIUsage(usage, u)
		if verr != nil {
			if ctx.Err() != nil {
				break
			}
			fmt.Fprintf(os.Stderr, "  %s %s %s: %v\n", chains[i].FromID, chains[i].Type, chains[i].ToID, verr)
			continue
		}
		chains[i].Direction = verdict
	}
	// A chain the blind pass read the other way round is not canonical, whatever
	// its corroboration count says.
	demoted := 0
	for i := range chains {
		if chains[i].Status == event.StatusCanonical &&
			(chains[i].Direction == event.DirectionFlipped || chains[i].Direction == event.DirectionDisputed) {
			chains[i].Status = event.StatusProvisional
			chains[i].Why = event.WhyDirectionWrong
			demoted++
		}
	}
	if err := event.WriteChains(dir, chains); err != nil {
		return err
	}
	score := event.ScoreDirection(chains)
	if summary, serr := event.ReadSummary(dir); serr == nil && summary != nil {
		summary.Direction = score
		if err := event.WriteSummary(dir, *summary); err != nil {
			return err
		}
	}
	fmt.Print(score)
	if demoted > 0 {
		fmt.Printf("               %d chains lost canonical status on the reading, they are still in the layer and marked\n", demoted)
	}
	fmt.Printf("usage %d input, %d output, %d total tokens\n", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
	reportRoutes(eng)
	return nil
}

// openLayer reads the folded layer for the questions to run over.
func openLayer(dir string) (*event.Graph, error) {
	events, err := event.ReadEvents(dir)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("no act layer, run luatdo events build first")
	}
	chains, err := event.ReadChains(dir)
	if err != nil {
		return nil, err
	}
	links, err := event.ReadLinks(dir)
	if err != nil {
		return nil, err
	}
	return event.NewGraph(events, chains, links), nil
}

// eventAsk runs the competency questions over the layer alone, with no text read.
func eventAsk(dir, question, subject string, depth int) error {
	g, err := openLayer(dir)
	if err != nil {
		return err
	}
	switch question {
	case "24":
		if subject == "" {
			return fmt.Errorf("usage: luatdo events ask 24 <event-id>")
		}
		fmt.Print(g.AskQuestion24(subject, depth))
	case "25":
		fmt.Print(g.AskQuestion25())
	case "26":
		fmt.Print(g.AskQuestion26(2))
	default:
		return fmt.Errorf("usage: luatdo events ask 24|25|26 [event-id]")
	}
	return nil
}

// eventProposals prints the act types the model invented, for review.
func eventProposals(dir string) error {
	proposals, err := event.ReadProposals(dir)
	if err != nil {
		return err
	}
	if len(proposals) == 0 {
		fmt.Println("no invented act types, the registry covered everything the model read")
		return nil
	}
	for _, p := range proposals {
		fmt.Printf("  %-28s %d instances in %d documents\n", p.Class, p.Instances, p.Docs)
		fmt.Printf("    %s\n", p.Definition)
		fmt.Printf("    written as %s\n", strings.Join(p.AsWritten, ", "))
		for _, e := range p.Examples {
			fmt.Printf("    %s: %s\n", e.ProvisionID, e.Quote)
		}
	}
	fmt.Printf("%d proposed act types, none of them promotes an act on its own\n", len(proposals))
	return nil
}

// eventAblate takes out the two decisions this layer could have made the other
// way and reports what each of them is worth.
func eventAblate(dir string, depth int) error {
	occurrences, chains, _, _, err := readSightings(dir)
	if err != nil {
		return err
	}
	if len(occurrences) == 0 {
		return fmt.Errorf("no sightings, run luatdo events extract first")
	}
	reg, err := event.ReadRegistry(dir)
	if err != nil {
		return err
	}
	fmt.Print(event.AblateIdentity(occurrences, chains, reg, event.DefaultThresholds, depth))
	g, err := openLayer(dir)
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Print(event.AblateSanctionJoin(g))
	return nil
}

// eventReport prints everything this milestone produced, in one place.
func eventReport(dir string) error {
	summary, err := event.ReadSummary(dir)
	if err != nil {
		return err
	}
	if summary == nil {
		fmt.Println("nothing built yet, run luatdo events extract then build")
		return nil
	}
	fmt.Printf("%d documents read, %d provisions named an act\n", summary.Documents, summary.Provisions)
	fmt.Print(summary.Counts)
	fmt.Print(summary.Direction)
	proposals, err := event.ReadProposals(dir)
	if err != nil {
		return err
	}
	fmt.Printf("%d act types the registry does not hold are waiting on a person\n", len(proposals))
	return nil
}

// The gold set is drawn, checked and scored here. It is annotated before the
// pass runs over the same provisions, which is the only order in which the
// numbers mean anything.

const eventCandidateFile = "gold_event_candidates.jsonl"

func eventCandidatePath(scope string) string {
	if scope == "" {
		return eventCandidateFile
	}
	return "gold_event_candidates_" + scope + ".jsonl"
}

func eventGold(args []string) error {
	fs := flag.NewFlagSet("events gold", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	n := fs.Int("n", 100, "provisions to draw")
	seed := fs.String("seed", "m16", "sampling seed, so a draw is reproducible")
	scope := fs.String("campaign", "", "draw from and score against a named campaign")
	sub, _, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	switch sub {
	case "sample":
		return eventGoldSample(s, *scope, *n, *seed)
	case "check":
		return eventGoldCheck(s, *scope)
	case "score":
		return eventGoldScore(s, *scope)
	default:
		return fmt.Errorf("usage: luatdo events gold sample|check|score")
	}
}

// eventGoldSample draws the provisions to annotate, ranked by a hash of the seed
// so the draw is the same on every machine in the fleet without a random source.
//
// The draw is over the provisions the pass will read, which is the ones that
// mention a concept and the ones a trusted norm was read out of. A gold set
// drawn over every provision would put definition clauses and headings in front
// of an annotator, and the pass would score well on them by finding nothing,
// which is a number about the sample and not about the pass. Drawing over the
// mentions alone has the opposite fault and it is the worse one: on this corpus
// most resolved mentions sit in a handful of instruments, so the sample would
// have measured those instruments and been reported as a number about the
// corpus.
func eventGoldSample(s *store.Store, scope string, n int, seed string) error {
	type candidate struct {
		id, docID, text, hash string
		rank                  uint64
	}
	docs, err := loadDocs(s)
	if err != nil {
		return err
	}
	inScope := map[string]bool{}
	if scope != "" {
		sc, err := campaign.LookupScope(scope)
		if err != nil {
			return err
		}
		if _, inScope, err = campaignDocs(s, sc); err != nil {
			return err
		}
	}
	read := map[string]bool{}
	if err := eachMentionReport(s, "", func(r *concept.MentionReport) error {
		for _, m := range r.Mentions {
			if m.TermUseID != "" {
				read[m.ProvisionID] = true
			}
		}
		return nil
	}); err != nil {
		return err
	}
	// The same second half of the queue predicate the extractor uses. A store
	// with no norm layer still has acts in it, so a failure to load the
	// statements narrows the draw rather than stopping it.
	if in, err := eventLoad(s, s.Event()); err == nil {
		for id := range in.norms {
			read[id] = true
		}
	}

	var drawn []candidate
	for _, d := range docs {
		if scope != "" && !inScope[d.ID] {
			continue
		}
		for _, p := range coverage.Extractable(d) {
			if strings.TrimSpace(p.Text) == "" || !read[p.ID] {
				continue
			}
			sum := sha256.Sum256([]byte(seed + "\x00" + p.ID))
			drawn = append(drawn, candidate{
				id: p.ID, docID: d.ID, text: p.Text, hash: p.TextHash,
				rank: binary.BigEndian.Uint64(sum[:8]),
			})
		}
	}
	if len(drawn) == 0 {
		return fmt.Errorf("no provision has a linked concept or a trusted norm, run luatdo link or luatdo norms first")
	}
	population := len(drawn)
	sort.Slice(drawn, func(i, j int) bool {
		if drawn[i].rank != drawn[j].rank {
			return drawn[i].rank < drawn[j].rank
		}
		return drawn[i].id < drawn[j].id
	})
	drawn = drawn[:min(n, len(drawn))]
	sort.Slice(drawn, func(i, j int) bool { return drawn[i].id < drawn[j].id })

	var b strings.Builder
	enc := json.NewEncoder(&b)
	for _, c := range drawn {
		if err := enc.Encode(event.Gold{UnitID: c.id, DocID: c.docID, TextHash: c.hash, Text: c.text}); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(s.Event(), 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.Event(), eventCandidatePath(scope))
	if err := store.WriteFile(path, []byte(b.String())); err != nil {
		return err
	}
	where := "the corpus"
	if scope != "" {
		where = "campaign " + scope
	}
	fmt.Printf("drew %d provisions of the %d in %s the pass will read, seed %q\n", len(drawn), population, where, seed)
	fmt.Printf("wrote %s, annotate it by hand into %s before the pass runs over these provisions\n",
		path, event.GoldPath(s.Event(), scope))
	fmt.Println("a provision that names no act is annotated as such, it is not left out")
	return nil
}

func eventGoldCheck(s *store.Store, scope string) error {
	gold, err := event.ReadGold(s.Event(), scope)
	if err != nil {
		return err
	}
	if len(gold) == 0 {
		return fmt.Errorf("no gold set at %s, draw one with luatdo events gold sample and annotate it by hand",
			event.GoldPath(s.Event(), scope))
	}
	problems := event.CheckGold(gold)
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, "gold:", p)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d problems in the gold set, fix the annotations before scoring against them", len(problems))
	}
	fmt.Printf("%d annotated provisions, no problems\n", len(gold))
	return nil
}

// eventGoldScore scores the raw provision level reads rather than the folded
// layer, because the annotation is about one provision and the fold is about the
// corpus. Scoring the fold against a per provision annotation would count a
// correct read of a provision as a miss whenever corroboration held the act back.
func eventGoldScore(s *store.Store, scope string) error {
	gold, err := event.ReadGold(s.Event(), scope)
	if err != nil {
		return err
	}
	if len(gold) == 0 {
		return fmt.Errorf("no gold set at %s, draw one with luatdo events gold sample and annotate it by hand",
			event.GoldPath(s.Event(), scope))
	}
	if problems := event.CheckGold(gold); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "gold:", p)
		}
		return fmt.Errorf("%d problems in the gold set, fix the annotations before scoring against them", len(problems))
	}
	occurrences, chains, _, _, err := readSightings(s.Event())
	if err != nil {
		return err
	}
	if len(occurrences) == 0 {
		return fmt.Errorf("no sightings, run luatdo events extract first")
	}
	docs, err := loadDocs(s)
	if err != nil {
		return err
	}
	hash := map[string]string{}
	// A provision the pass read and found nothing in is covered too, and the
	// sighting file does not record it, so coverage is taken from the documents
	// the pass wrote a file for.
	seen := map[string]bool{}
	if err := event.EachSighting(s.Event(), func(sg event.Sighting) error {
		seen[sg.DocID] = true
		return nil
	}); err != nil {
		return err
	}
	for _, d := range docs {
		if !seen[d.ID] {
			continue
		}
		for i := range d.Provisions {
			hash[d.Provisions[i].ID] = d.Provisions[i].TextHash
		}
	}
	fmt.Print(event.Score(gold, occurrences, chains, hash))
	return nil
}
