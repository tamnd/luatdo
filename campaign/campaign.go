// Package campaign runs one pipeline stage over the whole corpus.
//
// A campaign is a long job against a metered service, so it is built to be
// interrupted. The queue is recomputed from disk, work is committed one
// provision at a time, and a job that fails leaves no artifact, which is
// exactly what puts it back in the queue on the next run. A signal drains:
// no new provision is started, the ones in flight finish and are written, and
// the accounting is reported for what actually ran.
package campaign

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/extract"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/review"
	"github.com/tamnd/luatdo/route"
	"github.com/tamnd/luatdo/store"
)

// Result is one provision's outcome, and one line of the campaign log.
type Result struct {
	Task       coverage.Task `json:"task"`
	Statements int           `json:"statements"`
	Entailed   int           `json:"entailed"`
	Review     int           `json:"review"`
	Invalid    int           `json:"invalid"`
	Routes     string        `json:"routes"`
	Usage      api.Usage     `json:"usage"`
	Cost       route.Cost    `json:"cost"`
	Duration   time.Duration `json:"duration"`
	Err        string        `json:"error,omitempty"`
}

// String is the reporting line: one provision, one line, every number a
// campaign is judged on.
func (r Result) String() string {
	if r.Err != "" {
		return fmt.Sprintf("norms %s route=%s failed after %s: %s",
			r.Task.ProvisionID, r.Routes, r.Duration.Round(time.Second), r.Err)
	}
	return fmt.Sprintf("norms %s route=%s statements=%d entailed=%d review=%d time=%s tokens=%d cost=%s",
		r.Task.ProvisionID, r.Routes, r.Statements, r.Entailed, r.Review,
		r.Duration.Round(time.Second), r.Usage.TotalTokens, r.Cost)
}

// Summary is the whole campaign.
type Summary struct {
	Queued     int           `json:"queued"`
	Done       int           `json:"done"`
	Failed     int           `json:"failed"`
	Skipped    int           `json:"skipped"`
	Statements int           `json:"statements"`
	Entailed   int           `json:"entailed"`
	Review     int           `json:"review"`
	Invalid    int           `json:"invalid"`
	Usage      api.Usage     `json:"usage"`
	Cost       route.Cost    `json:"cost"`
	Duration   time.Duration `json:"duration"`
	StartedAt  time.Time     `json:"started_at"`
}

func (s Summary) String() string {
	return fmt.Sprintf("campaign: %d done, %d failed, %d skipped of %d queued, %d statements, %d entailed, %d in review, %d tokens, cost %s, %s",
		s.Done, s.Failed, s.Skipped, s.Queued, s.Statements, s.Entailed, s.Review,
		s.Usage.TotalTokens, s.Cost, s.Duration.Round(time.Second))
}

// Runner works a queue of provisions with a pool of workers.
type Runner struct {
	Store          *store.Store
	Registry       *ontology.Registry
	Completer      api.Completer  // usually a *route.Router
	Pricing        *route.Pricing // rate card when there is no route file
	Model          string
	Mode           string
	Population     int
	MaxCorrections int
	Workers        int
	Report         func(Result)
	Now            func() time.Time
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Runner) workers() int {
	if r.Workers > 0 {
		return r.Workers
	}
	return 1
}

// meter returns the per job accounting wrapper. A router prices each call by
// the route that served it; anything else falls back to the campaign rate
// card, which may be nil, in which case the cost reports as unavailable.
func (r *Runner) meter() *route.Meter {
	if router, ok := r.Completer.(*route.Router); ok {
		return router.Meter()
	}
	return route.NewMeter(r.Completer, r.Pricing)
}

// Run works the queue until it is empty or ctx is cancelled. Cancelling drains
// rather than aborts: workers stop taking new provisions, and the ones already
// running finish against a context that is not cancelled, so a provision is
// never half extracted and half billed.
func (r *Runner) Run(ctx context.Context, tasks []coverage.Task) (Summary, error) {
	summary := Summary{Queued: len(tasks), StartedAt: r.now().UTC(), Cost: route.Cost{Available: true}}
	if len(tasks) == 0 {
		return summary, nil
	}
	work := context.WithoutCancel(ctx)

	queue := make(chan coverage.Task)
	results := make(chan Result)
	var wg sync.WaitGroup
	var enqueueMu sync.Mutex

	for range r.workers() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var cached *law.Document
			for task := range queue {
				if cached == nil || cached.ID != task.DocID {
					doc, err := r.loadDoc(task.DocID)
					if err != nil {
						results <- Result{Task: task, Routes: "none", Err: err.Error()}
						continue
					}
					cached = doc
				}
				results <- r.one(work, cached, task, &enqueueMu)
			}
		}()
	}

	go func() {
		defer close(queue)
		for _, task := range tasks {
			select {
			case queue <- task:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		if r.Report != nil {
			r.Report(res)
		}
		summary.Usage = route.AddUsage(summary.Usage, res.Usage)
		summary.Cost = summary.Cost.Add(res.Cost)
		if res.Err != "" {
			summary.Failed++
			continue
		}
		summary.Done++
		summary.Statements += res.Statements
		summary.Entailed += res.Entailed
		summary.Review += res.Review
		summary.Invalid += res.Invalid
	}
	summary.Skipped = summary.Queued - summary.Done - summary.Failed
	summary.Duration = r.now().Sub(summary.StartedAt)
	return summary, nil
}

// loadDoc reads one parsed document. Workers cache the document they are on,
// and the queue is ordered by document, so a run over a whole law reads it
// once per worker rather than once per clause.
func (r *Runner) loadDoc(docID string) (*law.Document, error) {
	var doc law.Document
	if err := store.ReadJSON(filepath.Join(r.Store.Docs(), law.FileName(docID)), &doc); err != nil {
		return nil, fmt.Errorf("read document %s: %w", docID, err)
	}
	return &doc, nil
}

// one extracts a single provision and commits it. The job artifact is written
// only when the extraction produced something usable, because the artifact is
// what tells the next run this provision is done.
func (r *Runner) one(ctx context.Context, doc *law.Document, task coverage.Task, enqueueMu *sync.Mutex) Result {
	start := r.now()
	meter := r.meter()
	runner := &extract.NormRunner{
		Completer:      meter,
		Model:          r.Model,
		Registry:       r.Registry,
		MaxCorrections: r.MaxCorrections,
		Mode:           r.Mode,
		Population:     r.Population,
	}
	res := Result{Task: task}
	job, err := runner.Run(ctx, doc, task.ProvisionID)
	if err == nil && job != nil {
		err = usable(job)
	}
	res.Usage = meter.Usage()
	res.Cost = meter.Cost()
	res.Routes = meter.Routes()
	res.Duration = r.now().Sub(start)
	if err != nil {
		res.Err = err.Error()
		return res
	}

	var items []review.Item
	at := r.now().UTC().Format(time.RFC3339)
	for i := range job.Records {
		rec := &job.Records[i]
		res.Statements++
		switch rec.Status {
		case "verified":
			res.Entailed++
		case "invalid":
			res.Invalid++
		}
		reasons := review.Reasons(rec)
		if len(reasons) == 0 {
			continue
		}
		res.Review++
		items = append(items, review.Item{
			StatementID: rec.ID, ProvisionID: rec.ProvisionID, DocID: rec.DocID,
			Reasons: reasons, Statement: rec.Statement, At: at,
		})
	}

	// The queue file is one append-only file shared by every worker, so the
	// append is serialised. The job artifact is per provision and is written
	// last: it is the commit point, and it must not appear before the review
	// items the next stage will look for.
	enqueueMu.Lock()
	err = review.Enqueue(r.Store.Review(), items)
	enqueueMu.Unlock()
	if err != nil {
		res.Err = err.Error()
		return res
	}
	if err := store.WriteJSON(extract.JobPath(r.Store.Norms(), task.ProvisionID), job); err != nil {
		res.Err = err.Error()
	}
	return res
}

// usable reports whether a job is worth committing. A job whose candidates all
// failed carries no information, and writing it would mark the provision done
// forever on the strength of an outage.
func usable(job *extract.NormJob) error {
	for _, c := range job.Candidates {
		if c.Err == "" {
			return nil
		}
	}
	if len(job.Candidates) == 1 {
		return errors.New(job.Candidates[0].Err)
	}
	return fmt.Errorf("all %d candidates failed", len(job.Candidates))
}
