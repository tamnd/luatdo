package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/anchor"
	"github.com/tamnd/luatdo/campaign"
	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/fetch"
	"github.com/tamnd/luatdo/graph"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/parse"
	"github.com/tamnd/luatdo/relation"
	"github.com/tamnd/luatdo/store"
	"github.com/tamnd/luatdo/subject"
	"github.com/tamnd/luatdo/temporal"
)

func init() {
	commands = append(commands,
		command{"fetch", "download a pinned dataset revision into the raw store", cmdFetch},
		command{"parse", "parse raw documents into the canonical model", cmdParse},
		command{"cite", "resolve citation and amendment links", cmdCite},
		command{"anchor", "locate definitions articles and harvest alias declarations", cmdAnchor},
		command{"export", "project the store into Neo4j", cmdExport},
		command{"coverage", "report pipeline state recomputed from disk", cmdCoverage},
	)
}

func openStore(dataDir string) (*store.Store, error) {
	return store.Open(dataDir)
}

func cmdFetch(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory, default LUATDO_DATA or ~/data/luatdo")
	revision := fs.String("revision", "", "dataset revision, default the current commit")
	config := fs.String("config", "", "dataset config, for datasets published as several")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: luatdo fetch [--config <name>] <dataset>, one of: %s", strings.Join(datasetNames(), ", "))
	}
	ds, ok := fetch.Datasets[fs.Arg(0)]
	if !ok {
		return fmt.Errorf("unknown dataset %q, one of: %s", fs.Arg(0), strings.Join(datasetNames(), ", "))
	}
	ds, err := ds.Config(*config)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	client := &fetch.Client{}
	manifest, err := client.Fetch(context.Background(), s.Raw(), ds, *revision)
	if err != nil {
		return err
	}
	var total int64
	for _, f := range manifest.Files {
		total += f.Size
	}
	fmt.Printf("fetch %s revision %s: %d files, %d bytes\n", ds.Name, short(manifest.Revision), len(manifest.Files), total)
	return nil
}

func datasetNames() []string {
	names := make([]string, 0, len(fetch.Datasets))
	for name := range fetch.Datasets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func short(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

func cmdParse(args []string) error {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	dataset := fs.String("dataset", "uts_vlc", "dataset to parse, uts_vlc or th1nhng0")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	revisionDir, manifest, err := fetch.Latest(s.Raw(), *dataset)
	if err != nil {
		return err
	}

	only := fs.Arg(0)
	parsed, quarantined, metadata, skipped, merged := 0, 0, 0, 0, 0
	write := func(in parse.Input) error {
		doc, err := parse.Parse(in)
		if err != nil {
			// An official number with no year has no stable identifier. That
			// is a property of the source, not a failure of this run, so it
			// is counted and reported rather than aborting a 171k row pass.
			skipped++
			return nil
		}
		if only != "" && doc.ID != only && doc.OfficialNumber != only {
			return nil
		}
		switch doc.Status {
		case "quarantined":
			quarantined++
			if *dataset == "uts_vlc" {
				fmt.Printf("quarantine %s: %s\n", doc.ID, doc.Quarantine)
			}
		case "metadata":
			metadata++
		default:
			parsed++
		}
		// The same instrument is published in more than one dataset and the
		// publications do not carry the same fields, so what is already on disk
		// fills what this parse leaves empty rather than being overwritten by it.
		path := filepath.Join(s.Docs(), law.FileName(doc.ID))
		var existing law.Document
		if err := store.ReadJSON(path, &existing); err == nil {
			merged++
			doc = parse.Merge(&existing, doc)
		} else if !os.IsNotExist(err) {
			return err
		}
		return store.WriteJSON(path, doc)
	}

	switch *dataset {
	case "uts_vlc":
		inputs, err := parse.LoadUTSVLC(revisionDir, manifest.Revision)
		if err != nil {
			return err
		}
		for _, in := range inputs {
			if err := write(in); err != nil {
				return err
			}
		}
	case "th1nhng0":
		stats, err := parse.EachTh1nhng0(revisionDir, manifest.Revision, write)
		if err != nil {
			return err
		}
		fmt.Printf("th1nhng0: %d metadata rows, %d with content\n", stats.Metadata, stats.Content)
		fmt.Printf("th1nhng0 rows not taken: %d without a usable official number, %d local without an issuing body, %d translations, %d duplicates of a document already taken\n",
			stats.Unnumbered, stats.Unattributed, stats.Translation, stats.Duplicate)
	default:
		return fmt.Errorf("unknown dataset %q, one of: %s", *dataset, strings.Join(datasetNames(), ", "))
	}
	fmt.Printf("parse: %d parsed, %d quarantined, %d metadata only, %d without a usable official number\n",
		parsed, quarantined, metadata, skipped)
	if merged > 0 {
		fmt.Printf("parse: %d documents another dataset had already published, merged field by field rather than overwritten\n", merged)
	}
	return nil
}

func cmdAnchor(args []string) error {
	fs := flag.NewFlagSet("anchor", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}

	only := fs.Arg(0)
	sum := anchor.NewSummary()
	if err := eachDoc(s, func(doc *law.Document) error {
		if only != "" && doc.ID != only && doc.OfficialNumber != only {
			return nil
		}
		r := anchor.Anchor(doc)
		sum.Add(doc, r)
		if len(doc.Provisions) == 0 {
			// A document with no text was never a candidate, so it gets no
			// artifact. Absence here means the text is missing, not that the
			// stage skipped it, and the two are told apart in the report.
			return nil
		}
		return store.WriteJSON(filepath.Join(s.Anchor(), law.FileName(doc.ID)), r)
	}); err != nil {
		return err
	}

	if err := store.WriteJSON(filepath.Join(s.Anchor(), anchor.SummaryFile), sum); err != nil {
		return err
	}
	residue := strings.Join(sum.Unanchored, "\n")
	if residue != "" {
		residue += "\n"
	}
	if err := store.WriteFile(filepath.Join(s.Anchor(), anchor.ResidueFile), []byte(residue)); err != nil {
		return err
	}
	fmt.Println(sum)
	fmt.Printf("wrote %s\n", filepath.Join(s.Anchor(), anchor.ResidueFile))
	return nil
}

// eachDoc streams the parsed documents one at a time. The corpus is 128,094
// documents with full text, so a stage that walks it holds one document rather
// than all of them.
func eachDoc(s *store.Store, visit func(*law.Document) error) error {
	entries, err := os.ReadDir(s.Docs())
	if err != nil {
		return fmt.Errorf("no parsed documents, run luatdo parse first: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var doc law.Document
		if err := store.ReadJSON(filepath.Join(s.Docs(), name), &doc); err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := visit(&doc); err != nil {
			return err
		}
	}
	return nil
}

func loadDocs(s *store.Store) ([]*law.Document, error) {
	entries, err := os.ReadDir(s.Docs())
	if err != nil {
		return nil, fmt.Errorf("no parsed documents, run luatdo parse first: %w", err)
	}
	var docs []*law.Document
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var doc law.Document
		if err := store.ReadJSON(filepath.Join(s.Docs(), e.Name()), &doc); err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		docs = append(docs, &doc)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	return docs, nil
}

func cmdCite(args []string) error {
	fs := flag.NewFlagSet("cite", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	docs, err := loadDocs(s)
	if err != nil {
		return err
	}
	official, dropped, err := officialRelations(s)
	if err != nil {
		return err
	}
	index := cite.Index(docs)
	resolved, unresolved := 0, 0
	for _, doc := range docs {
		links := cite.Merge(cite.Resolve(doc, index), official[doc.ID])
		for _, l := range links {
			if l.ToDoc == "" {
				unresolved++
			} else {
				resolved++
			}
		}
		if err := store.WriteJSON(filepath.Join(s.Cite(), law.FileName(doc.ID)), links); err != nil {
			return err
		}
	}
	fmt.Printf("cite: %d resolved, %d unresolved across %d documents\n", resolved, unresolved, len(docs))
	if dropped > 0 {
		fmt.Printf("cite: %d official relations dropped, they name documents outside the corpus\n", dropped)
	}
	return nil
}

// officialRelations reads the th1nhng0 relationship graph when that dataset
// has been fetched, grouped by source document. Official links are dataset
// metadata rather than pattern matches, so they are authoritative wherever
// they exist and the in-text scan only fills the gaps.
func officialRelations(s *store.Store) (map[string][]cite.Link, int, error) {
	revisionDir, _, err := fetch.Latest(s.Raw(), "th1nhng0")
	if err != nil {
		return nil, 0, nil
	}
	relations, dropped, err := parse.Th1nhng0Relations(revisionDir)
	if err != nil {
		return nil, 0, err
	}
	out := map[string][]cite.Link{}
	for _, r := range relations {
		out[r.FromDoc] = append(out[r.FromDoc], cite.Link{
			FromDoc: r.FromDoc,
			ToDoc:   r.ToDoc,
			Kind:    r.Kind,
			Method:  "official",
			Snippet: r.Label,
		})
	}
	return out, dropped, nil
}

func loadLinks(s *store.Store) ([]cite.Link, error) {
	entries, err := os.ReadDir(s.Cite())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var links []cite.Link
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var fileLinks []cite.Link
		if err := store.ReadJSON(filepath.Join(s.Cite(), e.Name()), &fileLinks); err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		links = append(links, fileLinks...)
	}
	return links, nil
}

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	merge := fs.Bool("merge", false, "merge incrementally over Bolt instead of writing CSVs")
	check := fs.Bool("check", false, "compare the live database against the store and report drift")
	scope := fs.String("campaign", "", "run the release gates over a named campaign before exporting")
	force := fs.Bool("force", false, "export even though the release gates failed, and say so in the output")
	// parseSub rather than fs.Parse, because flag stops at the first argument
	// that is not a flag and "luatdo export neo4j --campaign labour-2025" would
	// otherwise dump the whole corpus without a word about the flag it ignored.
	sub, rest, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	if sub != "neo4j" || len(rest) > 0 {
		return fmt.Errorf("usage: luatdo export neo4j [--merge|--check] [--campaign <name>]")
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	docs, err := loadDocs(s)
	if err != nil {
		return err
	}
	links, err := loadLinks(s)
	if err != nil {
		return err
	}
	in := graph.Input{Docs: docs, Links: links}
	in.Definitions, _ = loadDefinitions(s)
	in.Registry, _ = ontology.Load(s.Ontology())
	in.Mentions, _ = loadResolutions(s)
	// The subject layer only reaches the projection when both halves are there.
	// A store where luatdo subjects has never run exports the document graph
	// alone, the same way it does without the ontology.
	if records, err := subject.ReadRecords(filepath.Join(s.Subject(), subject.AssignmentsFile)); err == nil {
		in.Subjects = records
		in.Vocabulary, _ = subject.Load()
	}
	_ = store.ReadJSON(filepath.Join(s.Trusted(), "statements.json"), &in.Statements)
	in.Layer, _ = concept.ReadLayer(s.Concepts())
	// The relation layer and the temporal layer are loaded on the same terms as
	// everything else here: if the pass has not been run the projection goes
	// without them. Both were built milestones ago and neither was ever handed
	// to the exporter, so the database held a document graph with a norm layer
	// bolted on and none of the concept relations or amendment history that half
	// the competency questions are asked of. Nothing failed, because nothing
	// asked.
	in.Relations, _ = relation.ReadEdges(s.Relation())
	in.Temporal, _ = temporal.ReadLayer(s.Temporal())

	// A campaign scopes the dump as well as the gates. The whole corpus is not
	// the unit anybody works with: a person asking about labour law wants the
	// labour campaign, and a dump of everything is slower to load and harder to
	// read a query result out of. Naming the campaign twice, once to gate and
	// once to cut, would be two chances to name a different one.
	if *scope != "" {
		sc, err := campaign.LookupScope(*scope)
		if err != nil {
			return err
		}
		_, keep, err := campaignDocs(s, sc)
		if err != nil {
			return err
		}
		in = graph.Restrict(in, keep)
		fmt.Printf("scoped to campaign %s: %d documents\n", sc.Name, len(in.Docs))
	}
	summary := graph.Summarize(in)

	// The release gates sit on the road out. A checklist beside the road is
	// worked by whoever is shipping, at the moment they most want to ship, and
	// the export is the last point where a campaign that should not leave the
	// building still has not left it. A check run reads the live database and
	// changes nothing, so it is not gated.
	if !*check {
		v, err := gateVerdict(s, *scope)
		if err != nil {
			return err
		}
		if ok, reasons := v.Ship(); !ok {
			fmt.Print(v)
			if !*force {
				return fmt.Errorf("release gates failed on %d of %d checks, rerun with --force to export anyway", len(reasons), len(v.Results))
			}
			fmt.Println("exporting anyway because --force was passed, and this graph is not one to publish numbers from")
		}
	}
	if *check {
		counts, err := graph.Live(context.Background(), graph.TargetFromEnv())
		if err != nil {
			return err
		}
		drift := graph.Drift(summary, counts)
		for _, line := range drift {
			fmt.Println("drift", line)
		}
		if len(drift) > 0 {
			return fmt.Errorf("%d counters drifted, reimport rather than merge", len(drift))
		}
		fmt.Printf("export neo4j check: no drift, %s\n", summary)
		return nil
	}
	if *merge {
		if err := graph.Merge(context.Background(), graph.TargetFromEnv(), in); err != nil {
			return err
		}
		fmt.Printf("export neo4j merge: %s\n", summary)
		return nil
	}
	dir := filepath.Join(s.Export(), "neo4j")
	if err := graph.Export(dir, in); err != nil {
		return err
	}
	fmt.Printf("export neo4j: %s\n", summary)
	fmt.Printf("wrote %s, run import.sh or import.cmd there, then apply schema.cypher\n", dir)
	return nil
}

func cmdCoverage(args []string) error {
	fs := flag.NewFlagSet("coverage", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	verbose := fs.Bool("verbose", false, "list quarantined documents")
	missing := fs.Bool("missing", false, "list the outstanding extraction queue instead of the report")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	if *missing {
		tasks, err := coverage.Queue(s)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			fmt.Printf("%-12s %s\n", t.DocType, t.ProvisionID)
		}
		fmt.Printf("%d provisions outstanding\n", len(tasks))
		return nil
	}
	report, err := coverage.Compute(s)
	if err != nil {
		return err
	}
	fmt.Println(report)
	if *verbose {
		for _, q := range report.Quarantines {
			fmt.Println("quarantine", q)
		}
	}
	return nil
}
