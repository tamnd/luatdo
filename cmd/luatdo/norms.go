package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/luatdo/campaign"
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/extract"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/review"
	"github.com/tamnd/luatdo/store"
)

func init() {
	commands = append(commands,
		command{"norms", "LLM norm extraction with entailment verification", cmdNorms},
		command{"review", "human review queue for gated statements", cmdReview},
		command{"build", "assemble verified statements into the trusted store", cmdBuild},
		command{"ask", "answer the norm competency questions over the trusted store", cmdAsk},
	)
}

func cmdAsk(args []string) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	doc := fs.String("doc", "", "restrict question 9 to one document")
	days := fs.Int("days", 5, "the working day threshold of question 12")
	sub, rest, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	records, err := loadTrusted(s)
	if err != nil {
		return err
	}
	switch sub {
	case "9":
		fmt.Print(norm.AskQuestion9(records, *doc, arg(rest, 0)))
	case "10":
		docs, err := loadDocs(s)
		if err != nil {
			return err
		}
		fmt.Print(norm.AskQuestion10(records, provisionText(docs)))
	case "11":
		docs, err := loadDocs(s)
		if err != nil {
			return err
		}
		fmt.Print(norm.AskQuestion11(records, norm.Positions(docs), strings.Join(rest, " ")))
	case "12":
		fmt.Print(norm.AskQuestion12(records, *days))
	case "13":
		fmt.Print(norm.AskQuestion13(records))
	case "14":
		id := arg(rest, 0)
		if id == "" {
			return fmt.Errorf("usage: luatdo ask 14 <statement-id>")
		}
		fmt.Print(norm.AskQuestion14(records, id))
	case "15":
		terms, err := loadTermUses(s)
		if err != nil {
			return err
		}
		fmt.Print(norm.AskQuestion15(records, concept.DefinedLabels(terms)))
	default:
		return fmt.Errorf("usage: luatdo ask 9|10|11|12|13|14|15 [argument]")
	}
	return nil
}

// provisionText is the words of every provision, which question 10 needs to
// tell a provision that names no actor from one whose actor the extraction
// dropped.
func provisionText(docs []*law.Document) map[string]string {
	out := map[string]string{}
	for _, d := range docs {
		for i := range d.Provisions {
			out[d.Provisions[i].ID] = d.Provisions[i].Text
		}
	}
	return out
}

func loadTrusted(s *store.Store) ([]norm.Record, error) {
	var records []norm.Record
	path := filepath.Join(s.Trusted(), "statements.json")
	if err := store.ReadJSON(path, &records); err != nil {
		return nil, fmt.Errorf("no trusted statements, run luatdo build first: %w", err)
	}
	return records, nil
}

func cmdNorms(args []string) error {
	if len(args) > 0 && args[0] == "gold" {
		return normGold(args[1:])
	}
	fs := flag.NewFlagSet("norms", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	mode := fs.String("mode", "fast", "fast or slow")
	population := fs.Int("population", 3, "independent candidates in slow mode")
	corrections := fs.Int("max-corrections", 2, "bounded retries on invalid model output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: luatdo norms [--mode fast|slow] <provision-id or article-id>")
	}
	target := fs.Arg(0)
	s, err := openStore(*dataDir)
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
	var tasks []coverage.Task
	for _, p := range coverage.Extractable(doc) {
		if p.ID == target || strings.HasPrefix(p.ID, target+":") {
			tasks = append(tasks, coverage.Task{
				ProvisionID: p.ID, DocID: doc.ID, DocType: doc.DocType,
				Priority: coverage.Priority(doc.DocType),
			})
		}
	}
	if len(tasks) == 0 {
		return fmt.Errorf("no extractable provisions under %s", target)
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	runner := &campaign.Runner{
		Store: s, Registry: reg, Completer: eng.completer, Pricing: eng.pricing,
		Model: eng.model, Mode: *mode, Population: *population,
		MaxCorrections: *corrections, Workers: 1,
		Report: func(res campaign.Result) { fmt.Println(res) },
	}
	summary, err := runner.Run(context.Background(), tasks)
	if err != nil {
		return err
	}
	fmt.Println(summary)
	if summary.Failed > 0 {
		return fmt.Errorf("%d provisions failed", summary.Failed)
	}
	return nil
}

func cmdReview(args []string) error {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	sub, rest, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	items, err := review.ReadQueue(s.Review())
	if err != nil {
		return err
	}
	decisions, err := review.ReadDecisions(s.Review())
	if err != nil {
		return err
	}
	switch sub {
	case "", "list":
		pending := review.Pending(items, decisions)
		for _, it := range pending {
			bearer := "(nobody)"
			if it.Statement.Bearer != nil {
				bearer = it.Statement.Bearer.Text
			}
			fmt.Printf("%s\n  %s: %s / %s\n  quote: %s\n  reasons: %s\n",
				it.StatementID, it.Statement.Type, bearer, it.Statement.Action.Text,
				it.Statement.Evidence.Quote, strings.Join(it.Reasons, "; "))
		}
		fmt.Printf("%d pending of %d queued\n", len(pending), len(items))
		return nil
	case "approve", "reject":
		id := arg(rest, 0)
		if id == "" {
			return fmt.Errorf("usage: luatdo review %s <statement-id> [note]", sub)
		}
		d := review.Decision{
			StatementID: id,
			Verdict:     sub + "d",
			Note:        strings.Join(rest[1:], " "),
			At:          time.Now().UTC().Format(time.RFC3339),
		}
		if sub == "reject" {
			d.Verdict = "rejected"
		}
		if err := review.Decide(s.Review(), d); err != nil {
			return err
		}
		fmt.Printf("%s %s\n", d.Verdict, id)
		return nil
	default:
		return fmt.Errorf("usage: luatdo review [list|approve|reject]")
	}
}

func cmdBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	jobs, err := loadNormJobs(s)
	if err != nil {
		return fmt.Errorf("no norm jobs, run luatdo norms first: %w", err)
	}
	items, err := review.ReadQueue(s.Review())
	if err != nil {
		return err
	}
	decisions, err := review.ReadDecisions(s.Review())
	if err != nil {
		return err
	}
	queued := map[string]bool{}
	for _, it := range items {
		queued[it.StatementID] = true
	}
	pending := map[string]bool{}
	for _, it := range review.Pending(items, decisions) {
		pending[it.StatementID] = true
	}

	var trusted []norm.Record
	kept, waiting, dropped := 0, 0, 0
	seen := map[string]bool{}
	for _, job := range jobs {
		for i := range job.Records {
			rec := &job.Records[i]
			if seen[rec.ID] {
				continue
			}
			seen[rec.ID] = true
			switch {
			case pending[rec.ID]:
				waiting++
			case rec.Status != "verified" && !review.Approved(decisions, rec.ID):
				dropped++
			case queued[rec.ID] && !review.Approved(decisions, rec.ID):
				dropped++
			default:
				out := *rec
				// A statement the judge rejected and a person kept goes in saying
				// so, rather than borrowing the word the judge would not give it.
				if out.Status != norm.StatusVerified {
					out.Status = norm.StatusApproved
				}
				norm.Rederive(&out.Statement)
				trusted = append(trusted, out)
				kept++
			}
		}
	}
	docs, err := loadDocs(s)
	if err != nil {
		return err
	}
	// Both of these run over the trusted set rather than over each extraction
	// job, because both are corpus wide. A sanction basis points at a document
	// the job that produced it never saw, and a procedure's steps are extracted
	// one provision at a time by calls that cannot know how many there are.
	// The concept join comes before the sanction resolution, because a sanction
	// matched through a concept is the whole point of the concept layer being
	// under this one. A store where the concept build has never run links
	// nothing and says so, rather than failing.
	layer, err := concept.ReadLayer(s.Concepts())
	if err != nil {
		return err
	}
	linked, refs := 0, 0
	if layer != nil {
		linked, refs = norm.LinkConcepts(trusted, concept.LabelIndex(layer.TermUses, layer.Memberships))
	}
	cov := norm.ResolveSanctions(trusted, norm.Index(docs))
	procedures := norm.GroupProcedures(trusted, norm.Positions(docs))

	if err := store.WriteJSON(filepath.Join(s.Trusted(), "statements.json"), trusted); err != nil {
		return err
	}
	if err := store.WriteJSON(filepath.Join(s.Trusted(), "procedures.json"), procedures); err != nil {
		return err
	}
	fmt.Printf("build: %d trusted, %d awaiting review, %d dropped\n", kept, waiting, dropped)
	if layer == nil {
		fmt.Println("concepts: no concept layer in this store, references carry their registry class alone")
	} else {
		fmt.Printf("concepts: %d of %d references resolved to a concept\n", linked, refs)
	}
	fmt.Printf("sanctions: %d, %d resolved (%d into another instrument), %d outside the corpus, %d unreadable\n",
		cov.Sanctions, cov.Resolved, cov.CrossDoc, cov.External, cov.Unresolved)
	steps := 0
	for _, p := range procedures {
		steps += len(p.Steps)
	}
	fmt.Printf("procedures: %d with %d steps\n", len(procedures), steps)
	return nil
}

func loadNormJobs(s *store.Store) ([]*extract.NormJob, error) {
	entries, err := os.ReadDir(s.Norms())
	if err != nil {
		return nil, err
	}
	var jobs []*extract.NormJob
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var job extract.NormJob
		if err := store.ReadJSON(filepath.Join(s.Norms(), e.Name()), &job); err != nil {
			return nil, err
		}
		jobs = append(jobs, &job)
	}
	return jobs, nil
}
