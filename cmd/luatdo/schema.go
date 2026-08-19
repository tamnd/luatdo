package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/eval"
	"github.com/tamnd/luatdo/extract"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/relation"
	"github.com/tamnd/luatdo/schema"
	"github.com/tamnd/luatdo/store"
	"github.com/tamnd/luatdo/subject"
)

func init() {
	commands = append(commands,
		command{"schema", "find out what the closed registry is missing, and measure the finding out", cmdSchema},
	)
}

func cmdSchema(args []string) error {
	fs := flag.NewFlagSet("schema", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	limit := fs.Int("limit", 0, "stop after this many items, 0 for all")
	corrections := fs.Int("max-corrections", 2, "bounded retries on invalid model output")
	roundsFlag := fs.Int("rounds", schema.DefaultRounds, "how many times a broken record goes back to the model")
	direction := fs.String("direction", "both", "taxonomy induction direction: bottom-up, top-down or both")
	judge := fs.Bool("judge", false, "ask a second call whether each repair is grounded in the provision")
	dryRun := fs.Bool("dry-run", false, "print what would be asked, call no model")
	write := fs.Bool("write", false, "append unmatched proposals to the candidates queue")
	sub, _, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	switch sub {
	case "invariants":
		return schemaInvariants(s)
	case "blindspots":
		return schemaBlindspots(s)
	case "define":
		return schemaDefine(s, *limit, *corrections, *dryRun, *write)
	case "roundtrip":
		return schemaRoundTrip(s, *limit, *corrections, *dryRun)
	case "taxonomy":
		return schemaTaxonomy(s, *direction, *limit, *corrections, *dryRun)
	case "repair":
		return schemaRepair(s, *limit, *roundsFlag, *judge, *dryRun)
	case "conflicts":
		return schemaConflicts(s, *corrections, *dryRun)
	default:
		return fmt.Errorf("usage: luatdo schema invariants|blindspots|define|roundtrip|taxonomy|repair|conflicts")
	}
}

// schemaItems is every stored statement with the text its evidence has to
// quote.
//
// The text is the extraction window and not the provision's own Text field,
// which is the whole difference between a report about the corpus and a report
// about this function. A clause that enumerates points holds only its lead in
// sentence in Text, the points are separate provisions, and the extractor was
// shown and validated against the window that includes them. Checking against
// the bare clause marks three hundred verified records as quoting something
// that is not there, and every one of those is the checker being wrong.
//
// A record whose provision is no longer in the corpus is dropped rather than
// checked against an empty string, because every quote invariant would fire on
// it and the report would blame the extractor for a missing document.
func schemaItems(s *store.Store, limit int) ([]schema.Item, int, error) {
	records, err := campaignRecords(s)
	if err != nil {
		return nil, 0, err
	}
	docs, err := loadDocs(s)
	if err != nil {
		return nil, 0, err
	}
	owner := map[string]*law.Document{}
	for _, d := range docs {
		for i := range d.Provisions {
			owner[d.Provisions[i].ID] = d
		}
	}
	// Only the provisions somebody extracted from get a window. Building one for
	// every provision in the corpus is a minute of walking parent links for
	// fifteen thousand provisions to answer a question about fifteen hundred.
	texts := map[string]string{}
	for i := range records {
		id := records[i].ProvisionID
		if _, done := texts[id]; done {
			continue
		}
		d := owner[id]
		if d == nil {
			continue
		}
		w, err := extract.BuildWindow(d, id)
		if err != nil {
			continue
		}
		texts[id] = w.Text
	}
	var out []schema.Item
	orphans := 0
	for i := range records {
		rec := &records[i]
		text, ok := texts[rec.ProvisionID]
		if !ok {
			orphans++
			continue
		}
		out = append(out, schema.Item{
			RecordID: rec.ID, ProvisionID: rec.ProvisionID, DocID: rec.DocID,
			Statement: &rec.Statement, Text: text,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, orphans, nil
}

func schemaInvariants(s *store.Store) error {
	reg, err := ontology.Load(s.Ontology())
	if err != nil {
		return err
	}
	items, orphans, err := schemaItems(s, 0)
	if err != nil {
		return err
	}
	inv := schema.CountInvariants(reg, items)
	fmt.Print(inv)
	if orphans > 0 {
		fmt.Printf("  note: %d records cite a provision no longer in the corpus and were left out\n", orphans)
	}
	fmt.Println("  note: this pass calls no model, it re-runs the invariants over what is on disk")
	path := filepath.Join(s.Eval(), "schema_invariants.json")
	if err := store.WriteJSON(path, inv); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}

func schemaBlindspots(s *store.Store) error {
	reg, err := ontology.Load(s.Ontology())
	if err != nil {
		return err
	}
	items, _, err := schemaItems(s, 0)
	if err != nil {
		return err
	}
	b := schema.FindBlindspots(reg, items, nil)
	fmt.Print(b)
	fmt.Println("  note: this pass calls no model, an unplaced reference is the extractor declining to guess a class")
	path := filepath.Join(s.Eval(), "schema_blindspots.json")
	if err := store.WriteJSON(path, b); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}

// proposals reads the bootstrap queue and folds it.
//
// The blind spot report feeds this too: a surface form the corpus asked for
// often enough is a proposal in exactly the sense the define pass means, and it
// arrives with counts the bootstrap rows do not have.
func proposals(s *store.Store, reg *ontology.Registry, items []schema.Item) ([]schema.Proposal, error) {
	cs, err := ontology.ReadCandidates(s.Ontology())
	if err != nil {
		return nil, err
	}
	docOf := map[string]string{}
	for _, it := range items {
		docOf[it.ProvisionID] = it.DocID
	}
	out := schema.FoldProposals(ontology.Pending(cs), func(p string) string { return docOf[p] })
	seen := map[string]bool{}
	for _, p := range out {
		seen[p.Slug] = true
	}
	for _, a := range schema.FindBlindspots(reg, items, nil).Recurring() {
		if seen[a.Slug] {
			continue
		}
		out = append(out, schema.Proposal{
			Slug: a.Slug, Label: a.Text, Count: a.Count, Docs: a.Docs, Provisions: a.Examples,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out, nil
}

func schemaDefine(s *store.Store, limit, corrections int, dryRun, write bool) error {
	reg, err := ontology.Load(s.Ontology())
	if err != nil {
		return err
	}
	items, _, err := schemaItems(s, 0)
	if err != nil {
		return err
	}
	ps, err := proposals(s, reg, items)
	if err != nil {
		return err
	}
	if limit > 0 && len(ps) > limit {
		fmt.Printf("note: %d proposals folded, the run is capped at %d, so the rest are untried rather than matched\n", len(ps), limit)
		ps = ps[:limit]
	}
	undefined := 0
	for _, c := range reg.Classes {
		if strings.TrimSpace(c.DefinitionVI) == "" {
			undefined++
		}
	}
	fmt.Printf("define: %d proposals, %d registry classes of %d carry no definition and are defined first\n",
		len(ps), undefined, len(reg.Classes))
	if dryRun {
		for i, p := range ps {
			if i >= 10 {
				fmt.Printf("  ... %d more\n", len(ps)-i)
				break
			}
			fmt.Printf("  %4d in %2d docs  %s\n", p.Count, p.Docs, p.Label)
		}
		fmt.Printf("  this run would cost about %d calls: %d to define the registry, %d to define proposals, up to %d to canonicalize\n",
			undefined+2*len(ps), undefined, len(ps), len(ps))
		return nil
	}

	eng, err := openEngine()
	if err != nil {
		return err
	}
	ctx, stop := drainOnSignal(os.Stderr, "draining, finishing the proposal in flight, signal again to abort")
	defer stop()
	d := &schema.Definer{Completer: eng.completer, Model: eng.model, MaxCorrections: corrections}

	defs, usage, err := d.DefineRegistry(ctx, reg, func(id, def string) {
		fmt.Printf("  registry %-32s %s\n", id, def)
	})
	if err != nil {
		return err
	}
	score := schema.DefineScore{Proposals: len(ps), Usage: usage, Calls: undefined}
	matches := map[string]schema.Match{}
	for i := range ps {
		def, u, err := d.Define(ctx, ps[i].Label, ps[i].Quotes)
		score.Usage = addUsage(score.Usage, u)
		score.Calls++
		if err != nil {
			return err
		}
		ps[i].Definition = def
		score.Defined.Observe(def != "")
		if def == "" {
			continue
		}
		short := schema.Shortlist(reg, defs, def, schema.ShortlistSize)
		if len(short) == 0 {
			score.NoShort++
		}
		m, u, err := d.Canonicalize(ctx, ps[i], short, defs)
		score.Usage = addUsage(score.Usage, u)
		if len(short) > 0 {
			score.Calls++
		}
		if err != nil {
			return err
		}
		matches[ps[i].Slug] = m
		score.Matched.Observe(m.ClassID != "")
		fmt.Printf("  %-40s %s\n", ps[i].Label, matchLine(m))
	}
	queue := schema.Queue(ps, matches, time.Now().UTC().Format(time.RFC3339))
	score.Queued = len(queue)
	fmt.Print(score)
	reportRoutes(eng)

	if write {
		if err := ontology.AppendCandidates(s.Ontology(), queue); err != nil {
			return err
		}
		fmt.Printf("appended %d candidates to the queue, all proposed, none promoted\n", len(queue))
	} else {
		fmt.Printf("note: %d rows were not written, rerun with -write to append them to the queue\n", len(queue))
	}
	path := filepath.Join(s.Eval(), "schema_define.json")
	if err := store.WriteJSON(path, struct {
		Score      schema.DefineScore      `json:"score"`
		Registry   map[string]string       `json:"registry_definitions"`
		Proposals  []schema.Proposal       `json:"proposals"`
		Matches    map[string]schema.Match `json:"matches"`
		Candidates []ontology.Candidate    `json:"candidates"`
	}{score, defs, ps, matches, queue}); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}

func matchLine(m schema.Match) string {
	if m.ClassID != "" {
		return "matched " + m.ClassID
	}
	if m.Nearest != "" {
		return "new, nearest " + m.Nearest + ": " + m.Reason
	}
	return "new: " + m.Reason
}

func schemaRoundTrip(s *store.Store, limit, corrections int, dryRun bool) error {
	reg, err := ontology.Load(s.Ontology())
	if err != nil {
		return err
	}
	defs := map[string]string{}
	var saved struct {
		Registry map[string]string `json:"registry_definitions"`
	}
	if err := store.ReadJSON(filepath.Join(s.Eval(), "schema_define.json"), &saved); err == nil {
		defs = saved.Registry
	}
	have := 0
	for _, c := range reg.Classes {
		if defs[c.ID] != "" || strings.TrimSpace(c.DefinitionVI) != "" {
			have++
		}
	}
	if have == 0 {
		return fmt.Errorf("no class carries a definition, run luatdo schema define first")
	}
	if limit > 0 && limit < have {
		trimmed := map[string]string{}
		n := 0
		for _, c := range reg.Classes {
			if defs[c.ID] == "" && strings.TrimSpace(c.DefinitionVI) == "" {
				continue
			}
			if n >= limit {
				break
			}
			trimmed[c.ID] = defs[c.ID]
			n++
		}
		fmt.Printf("note: %d classes carry a definition, the run is capped at %d\n", have, limit)
		defs, have = trimmed, limit
	}
	fmt.Printf("roundtrip: %d defined classes, two calls each, one with the class in the shortlist and one without\n", have)
	if dryRun {
		return nil
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	ctx, stop := drainOnSignal(os.Stderr, "draining, finishing the class in flight, signal again to abort")
	defer stop()
	d := &schema.Definer{Completer: eng.completer, Model: eng.model, MaxCorrections: corrections}
	score, err := d.RoundTrip(ctx, reg, defs, func(id string, got schema.Match, held bool) {
		half := "withheld"
		if held {
			half = "held   "
		}
		fmt.Printf("  %s %-32s %s\n", half, id, matchLine(got))
	})
	if err != nil {
		return err
	}
	fmt.Print(score)
	reportRoutes(eng)
	path := filepath.Join(s.Eval(), "schema_roundtrip.json")
	if err := store.WriteJSON(path, score); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}

// taxonomyTerms is the held out gold: the hand written subject vocabulary, with
// every subdomain as a child and every domain as a candidate parent.
//
// It is the right test set for this because nobody wrote it for this. It was
// written to classify documents two milestones ago, the parent links are a
// person's opinion rather than a model's, and no pass in the pipeline has ever
// been scored against it.
func taxonomyTerms() ([]schema.Term, []schema.Term, error) {
	v, err := subject.Load()
	if err != nil {
		return nil, nil, err
	}
	var children, parents []schema.Term
	for _, d := range v.Domains() {
		parents = append(parents, schema.Term{ID: d.ID, Label: d.LabelVI})
	}
	for _, s := range v.Subjects {
		if s.IsDomain() {
			continue
		}
		children = append(children, schema.Term{ID: s.ID, Label: s.LabelVI, Parent: s.Parent})
	}
	return children, parents, nil
}

func schemaTaxonomy(s *store.Store, direction string, limit, corrections int, dryRun bool) error {
	children, parents, err := taxonomyTerms()
	if err != nil {
		return err
	}
	if limit > 0 && len(children) > limit {
		fmt.Printf("note: %d subdomains in the vocabulary, the run is capped at %d, so the rest are untried\n", len(children), limit)
		children = children[:limit]
	}
	runBottom := direction == "both" || direction == schema.BottomUp
	runTop := direction == "both" || direction == schema.TopDown
	if !runBottom && !runTop {
		return fmt.Errorf("direction %q is not bottom-up, top-down or both", direction)
	}
	calls := 0
	if runBottom {
		calls += len(children)
	}
	if runTop {
		calls += len(parents)
	}
	fmt.Printf("taxonomy: %d subdomains under %d domains, about %d calls\n", len(children), len(parents), calls)
	if dryRun {
		return nil
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	ctx, stop := drainOnSignal(os.Stderr, "draining, finishing the term in flight, signal again to abort")
	defer stop()
	in := &schema.Inducer{Completer: eng.completer, Model: eng.model, MaxCorrections: corrections}

	var scores []schema.TaxonomyScore
	var bottom, top *schema.Induced
	if runBottom {
		bottom, err = in.InduceBottomUp(ctx, children, parents, func(p schema.Placement) {
			fmt.Printf("  bottom-up %-36s %s\n", p.ChildID, orNone(p.ParentID))
		})
		if err != nil {
			return err
		}
		scores = append(scores, schema.ScoreTaxonomy(bottom, children))
	}
	if runTop {
		top, err = in.InduceTopDown(ctx, children, parents, func(p schema.Term, got []string) {
			fmt.Printf("  top-down  %-36s claims %d\n", p.ID, len(got))
		})
		if err != nil {
			return err
		}
		scores = append(scores, schema.ScoreTaxonomy(top, children))
	}
	for _, sc := range scores {
		fmt.Print(sc)
	}
	var agree eval.Accuracy
	if bottom != nil && top != nil {
		agree = schema.Agreement(bottom, top, children)
		fmt.Printf("the two directions agree on %s of the terms both of them placed\n", agree)
		if len(scores) == 2 && eval.Separates(scores[0].Overall.Right, scores[0].Overall.Of, scores[1].Overall.Right, scores[1].Overall.Of) {
			fmt.Println("the intervals do not overlap, so the two directions differ on this vocabulary")
		} else {
			fmt.Println("the intervals overlap, so this vocabulary does not separate the two directions")
		}
	} else {
		fmt.Println("note: one direction was run, so the two cannot be compared on this run")
	}
	reportRoutes(eng)
	path := filepath.Join(s.Eval(), "schema_taxonomy.json")
	if err := store.WriteJSON(path, struct {
		Scores    []schema.TaxonomyScore `json:"scores"`
		Agreement eval.Accuracy          `json:"agreement"`
		BottomUp  *schema.Induced        `json:"bottom_up,omitempty"`
		TopDown   *schema.Induced        `json:"top_down,omitempty"`
	}{scores, agree, bottom, top}); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}

// inducedConflicts reads the last taxonomy run and returns the children top
// down induction left contested, shaped as parent conflicts.
//
// Support is 1 for every claim on purpose. A top down claim is one parent
// saying yes once, so there is no count here to prefer, and inventing one would
// hand the resolver a tie break the data does not contain.
func inducedConflicts(s *store.Store) ([]schema.ParentConflict, error) {
	var saved struct {
		TopDown *schema.Induced `json:"top_down"`
	}
	if err := store.ReadJSON(filepath.Join(s.Eval(), "schema_taxonomy.json"), &saved); err != nil {
		return nil, nil // No taxonomy run yet is not an error, it is one less source.
	}
	if saved.TopDown == nil {
		return nil, nil
	}
	children, parents, err := taxonomyTerms()
	if err != nil {
		return nil, err
	}
	label := map[string]string{}
	for _, t := range append(append([]schema.Term{}, children...), parents...) {
		label[t.ID] = t.Label
	}
	var edges []schema.ParentEdge
	for _, id := range saved.TopDown.Contested() {
		for _, p := range saved.TopDown.Claims[id] {
			edges = append(edges, schema.ParentEdge{
				ChildID: id, ChildLabel: label[id],
				ParentID: p, ParentLabel: label[p], Support: 1,
			})
		}
	}
	return schema.FindParentConflicts(edges), nil
}

func orNone(id string) string {
	if id == "" {
		return "none"
	}
	return id
}

func schemaRepair(s *store.Store, limit, maxRounds int, judge, dryRun bool) error {
	reg, err := ontology.Load(s.Ontology())
	if err != nil {
		return err
	}
	items, _, err := schemaItems(s, 0)
	if err != nil {
		return err
	}
	var broken []schema.Item
	for _, it := range items {
		if len(norm.Violations(it.Statement, reg, it.Text)) > 0 {
			broken = append(broken, it)
		}
	}
	fmt.Printf("repair: %d of %d stored records break an invariant\n", len(broken), len(items))
	if limit > 0 && len(broken) > limit {
		fmt.Printf("note: the run is capped at %d, so %d broken records are untried rather than unfixable\n",
			limit, len(broken)-limit)
		broken = broken[:limit]
	}
	if dryRun {
		fmt.Printf("  this run would cost up to %d repair calls", len(broken)*rounds(maxRounds))
		if judge {
			fmt.Printf(" and up to %d judge calls", len(broken))
		}
		fmt.Println()
		return nil
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	ctx, stop := drainOnSignal(os.Stderr, "draining, finishing the record in flight, signal again to abort")
	defer stop()
	r := &schema.Repairer{Completer: eng.completer, Model: eng.model, MaxRounds: maxRounds}

	var reps []schema.Repair
	var grounded eval.Accuracy
	for _, it := range broken {
		rep, err := r.Fix(ctx, it, reg)
		if err != nil {
			return err
		}
		if judge && rep.Statement != nil && !rep.Declined {
			ok, reason, u, err := r.Judge(ctx, it, rep)
			if err != nil {
				return err
			}
			rep.Usage = addUsage(rep.Usage, u)
			rep.Calls++
			grounded.Observe(ok)
			if !ok {
				fmt.Printf("  ungrounded %-28s %s\n", it.ProvisionID, reason)
			}
		}
		reps = append(reps, rep)
		fmt.Printf("  %-28s %v -> %v\n", it.ProvisionID, rep.Before, rep.After)
	}
	score := schema.ScoreRepairs(reps, grounded)
	fmt.Print(score)
	reportRoutes(eng)
	path := filepath.Join(s.Eval(), "schema_repair.json")
	if err := store.WriteJSON(path, struct {
		Score   schema.RepairScore `json:"score"`
		Repairs []schema.Repair    `json:"repairs"`
	}{score, reps}); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	fmt.Println("note: nothing was written back to the norm store, a repair is a proposal like any other")
	return nil
}

func rounds(n int) int {
	if n <= 0 {
		return schema.DefaultRounds
	}
	return n
}

func schemaConflicts(s *store.Store, corrections int, dryRun bool) error {
	// The folded edge file is the layer as it is stored. The relation.Layer
	// value the checker uses is assembled in memory and never written, so
	// reading it back from disk is not an option.
	dir := s.Relation()
	all, err := relation.ReadEdges(dir)
	if err != nil {
		return fmt.Errorf("no folded relation edges in %s, run luatdo relations build first: %w", dir, err)
	}
	in, err := relationInputs(s, dir)
	if err != nil {
		return err
	}
	var edges []schema.ParentEdge
	broader := 0
	for _, e := range all {
		if e.Type != relation.Broader || e.Status != relation.StatusCanonical {
			continue
		}
		broader++
		edges = append(edges, schema.ParentEdge{
			ChildID: e.FromID, ChildLabel: in.labels[e.FromID],
			ParentID: e.ToID, ParentLabel: in.labels[e.ToID],
			Support: e.SupportCount,
		})
	}
	cs := schema.FindParentConflicts(edges)
	fmt.Printf("conflicts: %d edges in the layer, %d of them canonical BROADER, %d concepts under more than one parent\n",
		len(all), broader, len(cs))
	// The relation layer on this corpus carries no hierarchy, so the resolver
	// would never run against it. The top down induction pass produces the same
	// shape of problem and produces it for real: a child two parents both
	// claimed. Falling back to that is not a substitute for the graph, and the
	// report says which source it decided, so nobody reads an induced result as
	// a statement about the stored edges.
	source := "relation layer"
	if len(cs) == 0 {
		induced, err := inducedConflicts(s)
		if err != nil {
			return err
		}
		if len(induced) > 0 {
			cs, source = induced, "induced taxonomy"
			fmt.Printf("note: the relation layer offers no conflict, so the resolver runs over the %d children top down induction left contested\n", len(cs))
		}
	}
	if dryRun || len(cs) == 0 {
		rep := schema.Report(source, len(all), broader, cs, nil, api.Usage{})
		fmt.Print(rep)
		out := filepath.Join(s.Eval(), "schema_conflicts.json")
		if err := store.WriteJSON(out, rep); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", out)
		return nil
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	ctx, stop := drainOnSignal(os.Stderr, "draining, finishing the conflict in flight, signal again to abort")
	defer stop()
	inducer := &schema.Inducer{Completer: eng.completer, Model: eng.model, MaxCorrections: corrections}
	rs, usage, err := inducer.ResolveParents(ctx, cs, func(r schema.Resolution) {
		fmt.Printf("  %-36s keeps %s, drops %s\n", r.ChildID, orNone(r.Kept), strings.Join(r.Dropped, ", "))
	})
	if err != nil {
		return err
	}
	rep := schema.Report(source, len(all), broader, cs, rs, usage)
	fmt.Print(rep)
	reportRoutes(eng)
	out := filepath.Join(s.Eval(), "schema_conflicts.json")
	if err := store.WriteJSON(out, rep); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", out)
	fmt.Println("note: no edge was changed, the resolutions are a proposal for the review queue")
	return nil
}
