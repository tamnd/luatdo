package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/anchor"
	"github.com/tamnd/luatdo/campaign"
	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/conflict"
	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/deploy"
	"github.com/tamnd/luatdo/event"
	"github.com/tamnd/luatdo/fetch"
	"github.com/tamnd/luatdo/graph"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/legalruleml"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/parse"
	"github.com/tamnd/luatdo/rdf"
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
		command{"export", "project the store into Neo4j, and the dump into RDF", cmdExport},
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
	// Documents that number two of their own provisions the same, and how many
	// provisions that came to. The parser gives the later ones their own
	// identifier rather than letting one answer for both, and the count is
	// printed because it is a property of the corpus worth watching: a jump in
	// it is a structure the walk stopped recognising.
	repeatDocs, repeatProvisions := 0, 0
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
			repeats := 0
			for i := range doc.Provisions {
				if law.Repeated(doc.Provisions[i].ID) {
					repeats++
				}
			}
			if repeats > 0 {
				repeatDocs++
				repeatProvisions += repeats
			}
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
	if repeatDocs > 0 {
		fmt.Printf("parse: %d provisions across %d documents carry a number the document had already used, and each of them has an occurrence index so no identifier answers for two provisions\n",
			repeatProvisions, repeatDocs)
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

// loadExportInput reads every layer the store holds into one projection input.
//
// It is a function rather than a block inside the export command because
// forgetting a layer here is silent. The relation layer and the temporal layer
// were both built, tested and left out of this assembly for a milestone each,
// and the act layer went the same way after them: the projection wrote acts.csv
// with nothing under the header, no gate noticed, and the questions that ask
// about acts came back empty rather than wrong. A named function has a test
// against it, which is the only thing that catches an omission.
//
// Only the documents and the citations are required. Every other layer loads on
// the same terms: if the pass has not been run in this store the projection goes
// without it, because a store part way through the pipeline is the normal case
// and not an error.
func loadExportInput(s *store.Store) (graph.Input, error) {
	docs, err := loadDocs(s)
	if err != nil {
		return graph.Input{}, err
	}
	links, err := loadLinks(s)
	if err != nil {
		return graph.Input{}, err
	}
	in := graph.Input{Docs: docs, Links: links}
	in.Definitions, _ = loadDefinitions(s)
	in.Registry, _ = ontology.Load(s.Ontology())
	in.Mentions, _ = loadResolutions(s)
	// The subject layer needs both halves. Assignments without the vocabulary
	// are subject identifiers with nothing to name them.
	if records, err := subject.ReadRecords(filepath.Join(s.Subject(), subject.AssignmentsFile)); err == nil {
		in.Subjects = records
		in.Vocabulary, _ = subject.Load()
	}
	_ = store.ReadJSON(filepath.Join(s.Trusted(), "statements.json"), &in.Statements)
	in.Layer, _ = concept.ReadLayer(s.Concepts())
	in.Relations, _ = relation.ReadEdges(s.Relation())
	in.Temporal, _ = temporal.ReadLayer(s.Temporal())
	if report, err := conflict.ReadReport(s.Conflict()); err == nil && report != nil {
		in.Conflicts = report.Findings
	}
	// The act layer is three fields because the projection checks them against
	// different things, and handing over the chains without the acts writes
	// edges into nodes that are not there.
	in.Acts, _ = event.ReadEvents(s.Event())
	in.Chains, _ = event.ReadChains(s.Event())
	in.NormActs, _ = event.ReadLinks(s.Event())
	return in, nil
}

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	merge := fs.Bool("merge", false, "merge incrementally over Bolt instead of writing CSVs")
	check := fs.Bool("check", false, "compare the live database against the store and report drift")
	scope := fs.String("campaign", "", "run the release gates over a named campaign before exporting")
	force := fs.Bool("force", false, "export even though the release gates failed, and say so in the output")
	shard := fs.Int("shard", 1_000_000, "rows per Parquet file")
	// parseSub rather than fs.Parse, because flag stops at the first argument
	// that is not a flag and "luatdo export neo4j --campaign labour-2025" would
	// otherwise dump the whole corpus without a word about the flag it ignored.
	sub, rest, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	switch sub {
	case "neo4j", "rdf", "legalruleml", "parquet":
	default:
		sub = ""
	}
	if sub == "" || len(rest) > 0 {
		return fmt.Errorf("usage: luatdo export neo4j [--merge|--check] [--campaign <name>], luatdo export rdf, luatdo export parquet, or luatdo export legalruleml --campaign <name>")
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	switch sub {
	case "rdf":
		return exportRDF(s)
	case "parquet":
		return exportParquet(s, *shard)
	case "legalruleml":
		return exportLegalRuleML(s, *scope, *force)
	}
	in, err := loadExportInput(s)
	if err != nil {
		return err
	}

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

// exportRDF projects the CSV dump into N-Triples.
//
// It reads the dump and not the store, which is the whole point of the rdf
// package and is worth restating at the call site: whatever the last neo4j
// export wrote is what this projects, so the RDF cannot hold a node the graph
// does not. If the dump is stale the RDF is stale in exactly the same way, and
// that is better than being fresh in a way nothing else agrees with.
//
// There is no campaign flag here for the same reason. The scoping decision is
// made one step earlier by the export that wrote the dump, and a flag here
// would let somebody ask for a labour RDF over a corpus dump and get a file
// that says labour on the command line and holds everything.
func exportRDF(s *store.Store) error {
	dump := filepath.Join(s.Export(), "neo4j")
	out := filepath.Join(s.Export(), "rdf")
	summary, err := rdf.Export(dump, out)
	if err != nil {
		return err
	}
	fmt.Printf("export rdf: %s\n", summary)
	fmt.Printf("wrote %s, graph.nt is the data and vocabulary.ttl is the alignment you can decline to load\n", out)
	return nil
}

// exportParquet converts the Neo4j dump into the shape everything that is not
// Neo4j wants to read.
//
// It reads the dump for the same reason exportRDF does, and it takes no campaign
// flag for the same reason. What is in the dump is what gets published, and a
// second scoping decision here would let somebody publish a file that says
// labour and holds the corpus.
func exportParquet(s *store.Store, shard int) error {
	dump := filepath.Join(s.Export(), "neo4j")
	out := filepath.Join(s.Export(), "parquet")
	// Cleared rather than written over. The shard file names carry the total
	// number of shards, so a table that shrinks leaves files behind under their
	// old names, and the card would name a config whose directory holds two
	// copies of half the rows.
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	tables, err := graph.ToParquet(dump, out, shard)
	if err != nil {
		return err
	}
	unpacked, err := dirSize(dump)
	if err != nil {
		return err
	}
	d := deploy.PublishedDataset()
	card := graph.CardInput{
		Repo:          deploy.DatasetRepo,
		Archive:       filepath.Base(d.URL),
		ArchiveBytes:  d.Bytes,
		UnpackedBytes: unpacked,
	}
	if err := graph.WriteCard(out, tables, card); err != nil {
		return err
	}

	var rows, bytes, files int64
	for _, t := range tables {
		rows += t.Rows
		bytes += t.Bytes
		files += int64(len(t.Files))
	}
	fmt.Printf("export parquet: %d tables, %d rows, %d files, %.1fGB from %.1fGB of CSV\n",
		len(tables), rows, files, float64(bytes)/(1<<30), float64(unpacked)/(1<<30))
	fmt.Printf("wrote %s, README.md there is the dataset card and names one config per table\n", out)
	return nil
}

func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// exportLegalRuleML writes one campaign's trusted norms as LegalRuleML.
//
// It takes --force and refuses it. The neo4j export has one because a graph
// that fails a gate is still worth loading locally to see what is wrong with
// it, and the CSVs say nothing about how good they are. A LegalRuleML file says
// something about how good it is on every line: an lrml:Obligation is a claim
// that a rule engine may act on this, the file is meant to leave the building,
// and a person who receives one has no way of knowing that whoever produced it
// passed a flag. So the gate is the whole feature, and a flag that turned it
// off would be the feature removed.
//
// The refusal is not a placeholder either. On the corpus as it stands the judge
// agreement gate fails, which means the second opinion behind every entailed
// verdict has not been shown to agree with a person, and a rule base built on
// that would be exactly the false certainty this format invites.
func exportLegalRuleML(s *store.Store, scope string, force bool) error {
	if scope == "" {
		return fmt.Errorf("usage: luatdo export legalruleml --campaign <name>, one of: %s",
			strings.Join(campaign.ScopeNames(), ", "))
	}
	sc, err := campaign.LookupScope(scope)
	if err != nil {
		return err
	}
	v, err := gateVerdict(s, scope)
	if err != nil {
		return err
	}
	if ok, reasons := v.Ship(); !ok {
		fmt.Print(v)
		if force {
			fmt.Println("--force does nothing here, a rule base is read as a guarantee and there is nobody downstream to tell")
		}
		return fmt.Errorf("release gates failed on %d of %d checks, so this campaign does not get a formalism", len(reasons), len(v.Results))
	}
	records, err := loadTrusted(s)
	if err != nil {
		return err
	}
	_, inScope, err := campaignDocs(s, sc)
	if err != nil {
		return err
	}
	var kept []norm.Record
	for _, r := range records {
		if inScope[r.DocID] && r.Trusted() {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		return fmt.Errorf("campaign %s has no trusted statements to formalise, run luatdo run --campaign %s then luatdo build", sc.Name, sc.Name)
	}
	dir := filepath.Join(s.Export(), "legalruleml")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, sc.Name+".xml")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	summary, err := legalruleml.Export(f, legalruleml.Input{
		Campaign: sc.Name, Note: sc.Note, Records: kept,
	})
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Print(summary)
	fmt.Printf("wrote %s\n", path)
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
