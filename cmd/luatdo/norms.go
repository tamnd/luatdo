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
	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/extract"
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
	)
}

func cmdNorms(args []string) error {
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
	if err := fs.Parse(args); err != nil {
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
	switch fs.Arg(0) {
	case "", "list":
		pending := review.Pending(items, decisions)
		for _, it := range pending {
			subject := ""
			if it.Statement.Subject != nil {
				subject = it.Statement.Subject.Text
			}
			fmt.Printf("%s\n  %s: %s / %s\n  quote: %s\n  reasons: %s\n",
				it.StatementID, it.Statement.Type, subject, it.Statement.Action.Text,
				it.Statement.Evidence.Quote, strings.Join(it.Reasons, "; "))
		}
		fmt.Printf("%d pending of %d queued\n", len(pending), len(items))
		return nil
	case "approve", "reject":
		id := fs.Arg(1)
		if id == "" {
			return fmt.Errorf("usage: luatdo review %s <statement-id> [note]", fs.Arg(0))
		}
		d := review.Decision{
			StatementID: id,
			Verdict:     fs.Arg(0) + "d",
			Note:        strings.Join(fs.Args()[2:], " "),
			At:          time.Now().UTC().Format(time.RFC3339),
		}
		if fs.Arg(0) == "reject" {
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
				trusted = append(trusted, *rec)
				kept++
			}
		}
	}
	if err := store.WriteJSON(filepath.Join(s.Trusted(), "statements.json"), trusted); err != nil {
		return err
	}
	fmt.Printf("build: %d trusted, %d awaiting review, %d dropped\n", kept, waiting, dropped)
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
