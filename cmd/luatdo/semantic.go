package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/extract"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/link"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/store"
	"github.com/tamnd/luatdo/term"
)

func init() {
	commands = append(commands,
		command{"terms", "extract defined terms from interpretation articles, no model", cmdTerms},
		command{"ontology", "manage the versioned class and predicate registry", cmdOntology},
		command{"extract", "LLM mention extraction under the closed registry", cmdExtract},
		command{"link", "resolve extracted mentions against registry and terms", cmdLink},
		command{"prompt", "print the exact extraction prompt, no model call", cmdPrompt},
	)
}

func completerFromEnv() (api.Completer, string, error) {
	base := os.Getenv("LUATDO_BASE_URL")
	if base == "" {
		return nil, "", fmt.Errorf("LUATDO_BASE_URL is not set")
	}
	key := os.Getenv("LUATDO_API_KEY")
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	model := os.Getenv("LUATDO_MODEL")
	if model == "" {
		return nil, "", fmt.Errorf("LUATDO_MODEL is not set")
	}
	return &api.Client{URL: base, APIKey: key, MaxRetries: 4}, model, nil
}

func cmdTerms(args []string) error {
	fs := flag.NewFlagSet("terms", flag.ContinueOnError)
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
	total, docsWithTerms := 0, 0
	for _, doc := range docs {
		defs := term.Extract(doc)
		if len(defs) == 0 {
			continue
		}
		docsWithTerms++
		total += len(defs)
		if err := store.WriteJSON(filepath.Join(s.Terms(), law.FileName(doc.ID)), defs); err != nil {
			return err
		}
	}
	fmt.Printf("terms: %d definitions across %d documents\n", total, docsWithTerms)
	return nil
}

func loadDefinitions(s *store.Store) ([]term.Definition, error) {
	entries, err := os.ReadDir(s.Terms())
	if err != nil {
		return nil, err
	}
	var defs []term.Definition
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var fileDefs []term.Definition
		if err := store.ReadJSON(filepath.Join(s.Terms(), e.Name()), &fileDefs); err != nil {
			return nil, err
		}
		defs = append(defs, fileDefs...)
	}
	return defs, nil
}

func loadResolutions(s *store.Store) ([]link.Resolution, error) {
	entries, err := os.ReadDir(s.Links())
	if err != nil {
		return nil, err
	}
	var out []link.Resolution
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var rs []link.Resolution
		if err := store.ReadJSON(filepath.Join(s.Links(), e.Name()), &rs); err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	return out, nil
}

func cmdOntology(args []string) error {
	fs := flag.NewFlagSet("ontology", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	sample := fs.Int("sample", 50, "provisions to sample for bootstrap")
	sub, rest, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	switch sub {
	case "init":
		reg := ontology.Seed()
		if err := ontology.Save(s.Ontology(), reg); err != nil {
			return err
		}
		fmt.Printf("ontology v%d written with %d classes, %d predicates, unfrozen\n",
			reg.Version, len(reg.Classes), len(reg.Predicates))
		return nil
	case "freeze":
		reg, err := ontology.Freeze(s.Ontology(), time.Now())
		if err != nil {
			return err
		}
		fmt.Printf("ontology v%d frozen at %s\n", reg.Version, reg.FrozenAt)
		return nil
	case "candidates":
		all, err := ontology.ReadCandidates(s.Ontology())
		if err != nil {
			return err
		}
		pending := ontology.Pending(all)
		for _, c := range pending {
			fmt.Printf("%-9s %-40s %s\n", c.Kind, c.Label, c.Provision)
		}
		fmt.Printf("%d pending of %d recorded\n", len(pending), len(all))
		return nil
	case "approve", "reject", "merge":
		label := arg(rest, 0)
		if label == "" {
			return fmt.Errorf("usage: luatdo ontology %s <label> [merged-to]", sub)
		}
		c := ontology.Candidate{
			Kind:   "class",
			Label:  label,
			Source: "review",
			Status: sub + "d",
			At:     time.Now().UTC().Format(time.RFC3339),
		}
		if sub == "reject" {
			c.Status = "rejected"
		}
		if sub == "merge" {
			c.Status = "merged"
			c.MergedTo = arg(rest, 1)
			if c.MergedTo == "" {
				return fmt.Errorf("usage: luatdo ontology merge <label> <merged-to>")
			}
		}
		if err := ontology.AppendCandidates(s.Ontology(), []ontology.Candidate{c}); err != nil {
			return err
		}
		fmt.Printf("%s %s\n", c.Status, label)
		return nil
	case "bootstrap":
		return runBootstrap(s, *sample)
	default:
		return fmt.Errorf("usage: luatdo ontology init|bootstrap|candidates|approve|reject|merge|freeze")
	}
}

func runBootstrap(s *store.Store, sampleSize int) error {
	completer, model, err := completerFromEnv()
	if err != nil {
		return err
	}
	docs, err := loadDocs(s)
	if err != nil {
		return err
	}
	sampled := extract.Sample(docs, sampleSize)
	var usage api.Usage
	found := 0
	for i, item := range sampled {
		cs, u, err := extract.Bootstrap(context.Background(), completer, model, item.Doc, item.ProvisionID)
		usage = addUsage(usage, u)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bootstrap %s: %v\n", item.ProvisionID, err)
			continue
		}
		if err := ontology.AppendCandidates(s.Ontology(), cs); err != nil {
			return err
		}
		found += len(cs)
		fmt.Printf("bootstrap %d/%d %s: %d candidates\n", i+1, len(sampled), item.ProvisionID, len(cs))
	}
	fmt.Printf("bootstrap: %d candidates from %d provisions, %d tokens\n", found, len(sampled), usage.TotalTokens)
	return nil
}

func addUsage(a, b api.Usage) api.Usage {
	a.InputTokens += b.InputTokens
	a.OutputTokens += b.OutputTokens
	a.TotalTokens += b.TotalTokens
	return a
}

func loadDoc(s *store.Store, provisionOrDocID string) (*law.Document, error) {
	docID := provisionOrDocID
	if i := strings.Index(docID, ":article-"); i >= 0 {
		docID = docID[:i]
	}
	if i := strings.Index(docID, ":chapter-"); i >= 0 {
		docID = docID[:i]
	}
	var doc law.Document
	if err := store.ReadJSON(filepath.Join(s.Docs(), law.FileName(docID)), &doc); err != nil {
		return nil, fmt.Errorf("document %s not parsed: %w", docID, err)
	}
	return &doc, nil
}

func cmdPrompt(args []string) error {
	fs := flag.NewFlagSet("prompt", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	pass := fs.String("pass", "mentions", "which prompt to print: mentions or norms")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: luatdo prompt [--pass mentions|norms] <provision-id>")
	}
	provID := fs.Arg(0)
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	doc, err := loadDoc(s, provID)
	if err != nil {
		return err
	}
	w, err := extract.BuildWindow(doc, provID)
	if err != nil {
		return err
	}
	reg, err := ontology.Load(s.Ontology())
	if err != nil {
		return err
	}
	var instructions string
	switch *pass {
	case "mentions":
		instructions = (&extract.Extractor{Registry: reg}).Instructions()
	case "norms":
		instructions = (&extract.NormRunner{Registry: reg}).Instructions()
	default:
		return fmt.Errorf("unknown pass %q, want mentions or norms", *pass)
	}
	fmt.Println("--- instructions ---")
	fmt.Println(instructions)
	fmt.Println("--- input ---")
	fmt.Println(w.Prompt())
	return nil
}

func cmdExtract(args []string) error {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	corrections := fs.Int("max-corrections", 2, "bounded retries on invalid model output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: luatdo extract <provision-id or article-id>")
	}
	target := fs.Arg(0)
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	completer, model, err := completerFromEnv()
	if err != nil {
		return err
	}
	reg, err := ontology.Load(s.Ontology())
	if err != nil {
		return err
	}
	doc, err := loadDoc(s, target)
	if err != nil {
		return err
	}
	e := &extract.Extractor{Completer: completer, Model: model, Registry: reg, MaxCorrections: *corrections}

	var provisions []string
	for i := range doc.Provisions {
		p := &doc.Provisions[i]
		if p.ID == target || (strings.HasPrefix(p.ID, target+":") && p.Kind == "clause") {
			if p.Text != "" {
				provisions = append(provisions, p.ID)
			}
		}
	}
	if len(provisions) == 0 {
		return fmt.Errorf("no extractable provisions under %s", target)
	}
	for _, provID := range provisions {
		job, err := e.Run(context.Background(), doc, provID)
		if job != nil {
			if werr := store.WriteJSON(extract.JobPath(s.Extracts(), provID), job); werr != nil {
				return werr
			}
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "extract %s: %v\n", provID, err)
			continue
		}
		fmt.Printf("extract %s: %d mentions, %d unresolved, %d tokens\n",
			provID, len(job.Mentions), len(job.Unresolved), job.Usage.TotalTokens)
	}
	return nil
}

func cmdLink(args []string) error {
	fs := flag.NewFlagSet("link", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	reg, err := ontology.Load(s.Ontology())
	if err != nil {
		return err
	}
	defs, err := loadDefinitions(s)
	if err != nil {
		return fmt.Errorf("no terms extracted, run luatdo terms first: %w", err)
	}
	entries, err := os.ReadDir(s.Extracts())
	if err != nil {
		return fmt.Errorf("nothing extracted, run luatdo extract first: %w", err)
	}
	linker := link.New(reg, defs)
	resolved, unresolved := 0, 0
	byDoc := map[string][]link.Resolution{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var job extract.Job
		if err := store.ReadJSON(filepath.Join(s.Extracts(), e.Name()), &job); err != nil {
			return err
		}
		for _, r := range linker.Resolve(&job) {
			if r.TargetKind == "unresolved" {
				unresolved++
			} else {
				resolved++
			}
			byDoc[job.DocID] = append(byDoc[job.DocID], r)
		}
	}
	for docID, rs := range byDoc {
		if err := store.WriteJSON(filepath.Join(s.Links(), law.FileName(docID)), rs); err != nil {
			return err
		}
	}
	fmt.Printf("link: %d resolved, %d unresolved across %d documents\n", resolved, unresolved, len(byDoc))
	return nil
}
