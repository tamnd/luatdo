package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/relation"
	"github.com/tamnd/luatdo/store"
)

func init() {
	commands = append(commands,
		command{"relations", "read concept to concept relations, fold them, and answer with them", cmdRelations},
	)
}

func cmdRelations(args []string) error {
	fs := flag.NewFlagSet("relations", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	limit := fs.Int("limit", 0, "stop after this many provisions or edges, 0 for all")
	only := fs.String("doc", "", "one document, for a trial run before a campaign")
	corrections := fs.Int("max-corrections", 2, "bounded retries on invalid model output")
	depth := fs.Int("depth", 0, "how far a hierarchy or prerequisite walk goes")
	sub, rest, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	dir := s.Relation()

	switch sub {
	case "prompt":
		return relationPrompt(s, arg(rest, 0))
	case "extract":
		return relationExtract(s, dir, *only, *limit, *corrections)
	case "define":
		return relationDefine(s, dir, *corrections)
	case "verify":
		return relationVerify(s, dir, *limit, *corrections)
	case "build":
		return relationBuild(s, dir)
	case "ask":
		return relationAsk(s, dir, arg(rest, 0), arg(rest, 1), *depth)
	default:
		return fmt.Errorf("usage: luatdo relations prompt|extract|define|verify|build|ask")
	}
}

// inputs is everything the relation passes need that earlier milestones already
// built: the concepts, the labels and kinds the checker enforces domain and
// range with, and the resolver that turns a phrase into an identifier.
type inputs struct {
	terms    []concept.TermUse
	resolver *relation.Resolver
	kinds    relation.Kinds
	labels   map[string]string
	scope    map[string]string
	registry *relation.Registry
}

func relationInputs(s *store.Store, dir string) (*inputs, error) {
	terms, err := loadTermUses(s)
	if err != nil {
		return nil, err
	}
	if len(terms) == 0 {
		return nil, fmt.Errorf("no concepts to relate, run luatdo concepts read first")
	}
	reg, err := relation.ReadRegistry(dir)
	if err != nil {
		return nil, err
	}
	in := &inputs{
		terms: terms, resolver: relation.NewResolver(terms), registry: reg,
		kinds:  relation.Kinds{},
		labels: map[string]string{},
		scope:  map[string]string{},
	}
	for _, t := range terms {
		in.kinds[t.ID] = t.Kind
		in.labels[t.ID] = t.LabelVI
		in.scope[t.ID] = t.ScopeID
	}
	return in, nil
}

// candidates groups the resolved mentions of one document by provision. The
// candidate set is bounded on purpose: the model is asked what relations a
// provision shows between concepts somebody else already found, which is a
// comprehension task, rather than to find concepts and relations at once, which
// is two tasks and fails at both.
func candidates(report *concept.MentionReport, in *inputs) map[string][]relation.Candidate {
	out := map[string][]relation.Candidate{}
	seen := map[string]bool{}
	for _, m := range report.Mentions {
		if m.TermUseID == "" || seen[m.ProvisionID+"|"+m.TermUseID] {
			continue
		}
		seen[m.ProvisionID+"|"+m.TermUseID] = true
		out[m.ProvisionID] = append(out[m.ProvisionID], relation.Candidate{
			ID: m.TermUseID, LabelVI: in.labels[m.TermUseID], Kind: in.kinds[m.TermUseID],
		})
	}
	for id := range out {
		sort.Slice(out[id], func(i, j int) bool { return out[id][i].ID < out[id][j].ID })
	}
	return out
}

// eachMentionReport visits the linking reports. One directory holds the reading
// jobs, the discovery sightings, the mention reports and the built layer, and
// the prefix is how they are told apart.
func eachMentionReport(s *store.Store, only string, visit func(*concept.MentionReport) error) error {
	entries, err := os.ReadDir(s.Concepts())
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no linked mentions, run luatdo link first")
		}
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), concept.MentionPrefix) && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("no linked mentions, run luatdo link first")
	}
	for _, name := range names {
		var r concept.MentionReport
		if err := store.ReadJSON(filepath.Join(s.Concepts(), name), &r); err != nil {
			return err
		}
		if only != "" && r.DocID != only {
			continue
		}
		if err := visit(&r); err != nil {
			return err
		}
	}
	return nil
}

// provisionTexts indexes one document's extractable provisions by identifier.
func provisionTexts(doc *law.Document) map[string]string {
	out := map[string]string{}
	for _, p := range coverage.Extractable(doc) {
		out[p.ID] = p.Text
	}
	return out
}

// relationPrompt prints the exact prompt for one provision and calls nothing.
func relationPrompt(s *store.Store, provisionID string) error {
	if provisionID == "" {
		return fmt.Errorf("usage: luatdo relations prompt <provision-id>")
	}
	in, err := relationInputs(s, s.Relation())
	if err != nil {
		return err
	}
	doc, err := loadDoc(s, provisionID)
	if err != nil {
		return err
	}
	report, err := concept.ReadMentions(s.Concepts(), doc.ID)
	if err != nil {
		return err
	}
	if report == nil {
		return fmt.Errorf("%s has no linked mentions, run luatdo link first", doc.ID)
	}
	cands := candidates(report, in)[provisionID]
	text := provisionTexts(doc)[provisionID]
	if text == "" {
		return fmt.Errorf("provision %s has no extractable text", provisionID)
	}
	x := &relation.Extractor{Registry: in.registry}
	fmt.Println(x.Instructions())
	fmt.Println(relation.Prompt(provisionID, text, cands))
	if len(cands) < 2 {
		fmt.Printf("this provision has %d linked concepts, so no model would be called for it\n", len(cands))
	}
	return nil
}

// relationExtract runs the provision level read, one file of raw sightings per
// document. A document that fails leaves no artifact, which is exactly what puts
// it back in the queue next time.
func relationExtract(s *store.Store, dir, only string, limit, corrections int) error {
	in, err := relationInputs(s, dir)
	if err != nil {
		return err
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	x := &relation.Extractor{
		Completer: eng.completer, Model: eng.model,
		Registry: in.registry, MaxCorrections: corrections,
	}
	fmt.Printf("relations: reading with %s from %s\n", eng.model, eng.source)

	var usage api.Usage
	docs, asked, skipped, edges, failed := 0, 0, 0, 0, 0
	ctx := context.Background()
	err = eachMentionReport(s, only, func(report *concept.MentionReport) error {
		if limit > 0 && asked >= limit {
			return nil
		}
		doc, derr := loadDoc(s, report.DocID)
		if derr != nil {
			return derr
		}
		texts := provisionTexts(doc)
		byProvision := candidates(report, in)
		ids := make([]string, 0, len(byProvision))
		for id := range byProvision {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		var found []relation.Edge
		cut := false
		for _, id := range ids {
			if limit > 0 && asked >= limit {
				cut = true
				break
			}
			text := texts[id]
			if text == "" || len(byProvision[id]) < 2 {
				// One concept cannot relate to anything, and calling a model to
				// be told so is paying for a foregone conclusion.
				skipped++
				continue
			}
			asked++
			got, u, xerr := x.Extract(ctx, id, report.DocID, text, byProvision[id])
			usage = addAPIUsage(usage, u)
			if xerr != nil {
				fmt.Fprintf(os.Stderr, "  %s: %v\n", id, xerr)
				failed++
				continue
			}
			found = append(found, got...)
		}
		if cut {
			// A limit stopped this document part way, so it was not read and it
			// leaves no file. Writing one would say it was read and found to hold
			// what it held so far, which is how a trial run quietly takes the rest
			// of a document out of the queue forever.
			return nil
		}
		docs++
		edges += len(found)
		relation.Sort(found)
		return relation.WriteSightings(dir, report.DocID, found)
	})
	fmt.Printf("read %d documents, asked about %d provisions, skipped %d with fewer than two concepts\n", docs, asked, skipped)
	fmt.Printf("%d raw sightings, %d provisions the model could not get right\n", edges, failed)
	fmt.Printf("usage %d input, %d output, %d total tokens\n", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
	reportRoutes(eng)
	if err != nil {
		return err
	}
	fmt.Println("nothing is folded yet, run luatdo relations define then build")
	return nil
}

// relationDefine is the Define and Canonicalize steps. It folds the invented
// labels first, because a label proposed once is a guess and a label proposed
// across forty documents is the registry being incomplete, and the two get told
// apart by counting rather than by reading the label and forming an impression.
func relationDefine(s *store.Store, dir string, corrections int) error {
	in, err := relationInputs(s, dir)
	if err != nil {
		return err
	}
	var raw []relation.Edge
	if err := relation.EachSighting(dir, func(_ string, edges []relation.Edge) error {
		raw = append(raw, edges...)
		return nil
	}); err != nil {
		return err
	}
	proposals := relation.Propose(raw)
	if len(proposals) == 0 {
		fmt.Println("no invented relation types, the registry covered everything the model read")
		return relation.WriteProposals(dir, nil)
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	definer := &relation.Definer{Completer: eng.completer, Model: eng.model, MaxCorrections: corrections}
	canon := &relation.Canonicalizer{
		Completer: eng.completer, Model: eng.model,
		Registry: in.registry, MaxCorrections: corrections,
	}

	var usage api.Usage
	matched := 0
	ctx := context.Background()
	for i := range proposals {
		def, u, derr := definer.Define(ctx, proposals[i])
		usage = addAPIUsage(usage, u)
		if derr != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", proposals[i].Label, derr)
			continue
		}
		proposals[i].Definition = def
		to, why, u2, cerr := canon.Canonicalize(ctx, proposals[i])
		usage = addAPIUsage(usage, u2)
		if cerr != nil {
			return cerr
		}
		proposals[i].MatchedTo, proposals[i].Rationale = to, why
		if to != "" {
			matched++
		}
	}
	if err := relation.WriteProposals(dir, proposals); err != nil {
		return err
	}
	for _, p := range proposals {
		outcome := "no match, it stays provisional and stays in the queue"
		if p.MatchedTo != "" {
			outcome = "matches " + p.MatchedTo
		}
		fmt.Printf("  %-24s %d instances in %d documents, %s\n", p.Label, p.Instances, p.Docs, outcome)
	}
	fmt.Printf("%d proposed types, %d matched an existing relation, %d waiting on a person\n",
		len(proposals), matched, len(proposals)-matched)
	fmt.Printf("usage %d input, %d output, %d total tokens\n", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
	reportRoutes(eng)
	return nil
}

// relationVerify runs the blind second pass over the folded layer.
//
// It is blind on purpose. M4 shipped 75252 backwards AMENDS edges, and a
// verifier shown the extractor's claim verifies the claim rather than the text,
// which is how a second pass ends up agreeing with the first for free.
func relationVerify(s *store.Store, dir string, limit, corrections int) error {
	in, err := relationInputs(s, dir)
	if err != nil {
		return err
	}
	edges, err := relation.ReadEdges(dir)
	if err != nil {
		return err
	}
	if len(edges) == 0 {
		return fmt.Errorf("no folded layer to verify, run luatdo relations build first")
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	v := &relation.Verifier{Completer: eng.completer, Model: eng.model, MaxCorrections: corrections}

	var usage api.Usage
	checked := 0
	ctx := context.Background()
	for i := range edges {
		if limit > 0 && checked >= limit {
			break
		}
		if len(edges[i].Evidence) == 0 {
			continue
		}
		checked++
		verdict, u, verr := v.Verify(ctx, edges[i], in.labels[edges[i].FromID], in.labels[edges[i].ToID])
		usage = addAPIUsage(usage, u)
		if verr != nil {
			fmt.Fprintf(os.Stderr, "  %s %s %s: %v\n", edges[i].FromID, edges[i].Type, edges[i].ToID, verr)
			continue
		}
		edges[i].Direction = verdict
	}
	// An edge the blind pass read the other way round is not canonical, whatever
	// the corroboration count says, and Validate refuses one that claims to be.
	for i := range edges {
		if edges[i].Status == relation.StatusCanonical &&
			(edges[i].Direction == relation.DirectionFlipped || edges[i].Direction == relation.DirectionDisputed) {
			edges[i].Status = relation.StatusProvisional
			edges[i].Why = relation.WhyDirectionWrong
		}
	}
	if err := relation.WriteEdges(dir, edges); err != nil {
		return err
	}
	score := relation.ScoreDirection(edges)
	fmt.Print(score)
	fmt.Printf("usage %d input, %d output, %d total tokens\n", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
	reportRoutes(eng)
	return nil
}

// relationBuild folds the sightings into the layer and refuses to write one that
// fails its invariants. A relation layer that is nearly consistent is a relation
// layer nobody can query.
func relationBuild(s *store.Store, dir string) error {
	in, err := relationInputs(s, dir)
	if err != nil {
		return err
	}
	proposals, err := relation.ReadProposals(dir)
	if err != nil {
		return err
	}
	var raw []relation.Edge
	docs := 0
	if err := relation.EachSighting(dir, func(_ string, edges []relation.Edge) error {
		docs++
		raw = append(raw, edges...)
		return nil
	}); err != nil {
		return err
	}
	// The definitional edges are derived rather than stored, so a term use whose
	// definition was corrected gets its edges rebuilt from the new reading
	// instead of outliving the mistake they came from.
	raw = append(raw, relation.Definitional(in.terms, in.resolver)...)
	raw = relation.Apply(raw, proposals, in.registry)
	edges := relation.Fold(raw, in.registry, relation.DefaultThresholds)

	texts := relationTexts(s, edges)
	layer := &relation.Layer{
		Registry: in.registry, Edges: edges,
		Kinds: in.kinds, Labels: in.labels,
	}
	if problems := layer.Check(texts); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "  "+p)
		}
		return fmt.Errorf("%d relation layer invariants failed", len(problems))
	}
	if err := relation.WriteEdges(dir, edges); err != nil {
		return err
	}
	if err := relation.WriteRegistry(dir, in.registry); err != nil {
		return err
	}

	counts := relation.Count(edges)
	densest := relation.Densities(edges)
	if len(densest) > 5 {
		densest = densest[:5]
	}
	summary := relation.Summary{
		Documents: docs, Counts: counts,
		Direction: relation.ScoreDirection(edges), Densest: densest,
	}
	if err := relation.WriteSummary(dir, summary); err != nil {
		return err
	}
	fmt.Printf("%d raw sightings over %d documents folded into %d edges\n", len(raw), docs, len(edges))
	fmt.Print(counts)
	fmt.Print(summary.Direction)
	for _, d := range densest {
		fmt.Printf("density        %-28s %d edges over %d concepts, %.2f\n", d.DocID, d.Edges, d.Nodes, d.Ratio)
	}
	return nil
}

// relationTexts loads the provision text of every document the layer cites, so
// Check can enforce that a quote is in the provision it names. It reads only the
// documents the edges touch, because the corpus is a hundred thousand files and
// the layer is not.
func relationTexts(s *store.Store, edges []relation.Edge) relation.Texts {
	wanted := map[string]bool{}
	for _, e := range edges {
		for _, ev := range e.Evidence {
			if ev.DocID != "" {
				wanted[ev.DocID] = true
			}
		}
	}
	texts := map[string]string{}
	for docID := range wanted {
		doc, err := loadDoc(s, docID)
		if err != nil {
			continue
		}
		for id, text := range provisionTexts(doc) {
			texts[id] = text
		}
	}
	if len(texts) == 0 {
		// Nothing to read against is not the same as everything checking out, so
		// the caller gets nothing and Check skips invariant 3 rather than passing
		// it on an empty corpus.
		return nil
	}
	return func(id string) (string, bool) {
		text, ok := texts[id]
		return text, ok
	}
}

// citationLookup builds the from document to to document test question 22 asks.
// The citation graph is the largest thing on disk and pass L1 already built it,
// so this is a lookup rather than a recomputation.
func citationLookup(s *store.Store) (relation.Cites, error) {
	links, err := loadLinks(s)
	if err != nil {
		return nil, err
	}
	cited := map[string]map[string]bool{}
	for _, l := range links {
		if l.ToDoc == "" {
			continue
		}
		if cited[l.FromDoc] == nil {
			cited[l.FromDoc] = map[string]bool{}
		}
		cited[l.FromDoc][l.ToDoc] = true
	}
	return func(from, to string) bool { return cited[from][to] }, nil
}

// relationAsk runs the competency questions over the edges alone, with no text
// read. A question that has to fall back on searching the corpus is a question
// the graph did not answer.
func relationAsk(s *store.Store, dir, question, subject string, depth int) error {
	in, err := relationInputs(s, dir)
	if err != nil {
		return err
	}
	edges, err := relation.ReadEdges(dir)
	if err != nil {
		return err
	}
	if len(edges) == 0 {
		return fmt.Errorf("no relation layer, run luatdo relations build first")
	}
	g := relation.NewGraph(edges, in.labels, in.scope)

	switch question {
	case "7":
		if subject == "" {
			return fmt.Errorf("usage: luatdo relations ask 7 <concept-id>")
		}
		fmt.Print(g.AskQuestion7(subject, depth))
	case "21":
		if subject == "" {
			return fmt.Errorf("usage: luatdo relations ask 21 <concept-id>")
		}
		fmt.Print(g.AskQuestion21(subject, depth))
	case "22":
		cites, err := citationLookup(s)
		if err != nil {
			return err
		}
		fmt.Print(g.AskQuestion22(cites))
	case "23":
		if subject == "" {
			return fmt.Errorf("usage: luatdo relations ask 23 <concept-id>")
		}
		fmt.Print(g.AskQuestion23(subject))
	default:
		return fmt.Errorf("usage: luatdo relations ask 7|21|22|23 [concept-id]")
	}
	return nil
}
