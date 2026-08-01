package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/fetch"
	"github.com/tamnd/luatdo/graph"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/parse"
	"github.com/tamnd/luatdo/store"
)

func init() {
	commands = append(commands,
		command{"fetch", "download a pinned dataset revision into the raw store", cmdFetch},
		command{"parse", "parse raw documents into the canonical model", cmdParse},
		command{"cite", "resolve citation and amendment links", cmdCite},
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: luatdo fetch <dataset>, one of: %s", strings.Join(datasetNames(), ", "))
	}
	ds, ok := fetch.Datasets[fs.Arg(0)]
	if !ok {
		return fmt.Errorf("unknown dataset %q, one of: %s", fs.Arg(0), strings.Join(datasetNames(), ", "))
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	revisionDir, manifest, err := fetch.Latest(s.Raw(), "uts_vlc")
	if err != nil {
		return err
	}
	inputs, err := parse.LoadUTSVLC(revisionDir, manifest.Revision)
	if err != nil {
		return err
	}
	only := fs.Arg(0)
	parsed, quarantined := 0, 0
	for _, in := range inputs {
		doc, err := parse.Parse(in)
		if err != nil {
			return fmt.Errorf("parse %s: %w", in.OfficialNumber, err)
		}
		if only != "" && doc.ID != only && doc.OfficialNumber != only {
			continue
		}
		if doc.Status == "quarantined" {
			quarantined++
			fmt.Printf("quarantine %s: %s\n", doc.ID, doc.Quarantine)
		} else {
			parsed++
		}
		if err := store.WriteJSON(filepath.Join(s.Docs(), law.FileName(doc.ID)), doc); err != nil {
			return err
		}
	}
	fmt.Printf("parse: %d parsed, %d quarantined\n", parsed, quarantined)
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
	index := cite.Index(docs)
	resolved, unresolved := 0, 0
	for _, doc := range docs {
		links := cite.Resolve(doc, index)
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
	return nil
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || fs.Arg(0) != "neo4j" {
		return fmt.Errorf("usage: luatdo export neo4j [--merge]")
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
	summary := graph.Summarize(in)
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
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
