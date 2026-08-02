package campaign

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/route"
)

// A pool works a list of documents, which is the shape every pass except the
// norm pass has.
//
// The norm pass got a Runner in M11 because it was the one that had to run at
// scale, and the concept, relation and temporal passes were left as they were
// written: one document at a time, from the top of the corpus, with no way to
// say which part of it to read and no memory of what the last run managed
// before somebody closed the laptop. That is fine for a trial run over one law
// and it is the reason none of the three has ever covered anything. Reading
// forty nine thousand definition units one at a time, at a minute each, is a
// month of wall clock during which any interruption starts it again.
//
// The three passes differ in what they ask a model and agree on everything
// else: the unit of work is a document, the artifact is one file per document,
// and a document with no artifact is a document to do. So the driver is shared
// and the passes supply a function.

// Outcome is what one document cost and produced. Produced is the pass's own
// unit, term uses or edges or operations, and is only used for reporting.
type Outcome struct {
	Produced int
	Calls    int
	Usage    api.Usage
	// Skipped marks a document that carried no content for this pass. It is
	// coverage rather than failure: an instrument with no definitions article
	// has nothing to read and is not a gap in the reading.
	Skipped bool
}

// Work runs one document. Returning an error fails that document and no other,
// because a pass over a thousand instruments should not end on the one the
// model timed out on.
type Work func(ctx context.Context, docID string) (Outcome, error)

// Progress is one finished document, reported as it lands.
type Progress struct {
	DocID    string
	Done     int // documents finished so far, including this one
	Total    int
	Outcome  Outcome
	Duration time.Duration
	Err      string
}

func (p Progress) String() string {
	head := fmt.Sprintf("%5d/%d %-58s", p.Done, p.Total, p.DocID)
	switch {
	case p.Err != "":
		return fmt.Sprintf("%s failed after %s: %s", head, p.Duration.Round(time.Second), p.Err)
	case p.Outcome.Skipped:
		return fmt.Sprintf("%s nothing to read", head)
	default:
		return fmt.Sprintf("%s %4d found, %2d calls, %s", head, p.Outcome.Produced, p.Outcome.Calls, p.Duration.Round(time.Second))
	}
}

// PoolSummary is the whole pass.
type PoolSummary struct {
	Queued    int           `json:"queued"`
	Done      int           `json:"done"`
	Failed    int           `json:"failed"`
	Skipped   int           `json:"skipped"`
	Left      int           `json:"left"`
	Produced  int           `json:"produced"`
	Calls     int           `json:"calls"`
	Usage     api.Usage     `json:"usage"`
	Duration  time.Duration `json:"duration"`
	StartedAt time.Time     `json:"started_at"`
}

func (s PoolSummary) String() string {
	return fmt.Sprintf("%d read, %d with nothing to read, %d failed, %d left of %d queued, %d found, %d calls, %d tokens, %s",
		s.Done, s.Skipped, s.Failed, s.Left, s.Queued, s.Produced, s.Calls, s.Usage.TotalTokens,
		s.Duration.Round(time.Second))
}

// Pool runs Work over documents with a fixed number of workers.
type Pool struct {
	Workers int
	Report  func(Progress)
	Now     func() time.Time
}

func (p *Pool) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Pool) workers() int {
	if p.Workers > 0 {
		return p.Workers
	}
	return 1
}

// Run works docs until the list is empty or ctx is cancelled.
//
// Cancelling drains rather than aborts, on the same terms as the norm runner:
// no new document is started, the ones in flight finish against a context that
// is not cancelled, and they write their artifact. A pass that dropped work in
// flight on a signal would leave the document looking undone while the model
// calls that read it had already been spent.
func (p *Pool) Run(ctx context.Context, docs []string, work Work) PoolSummary {
	summary := PoolSummary{Queued: len(docs), StartedAt: p.now().UTC()}
	if len(docs) == 0 {
		return summary
	}
	commit := context.WithoutCancel(ctx)

	queue := make(chan string)
	results := make(chan Progress)
	var wg sync.WaitGroup
	for range p.workers() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range queue {
				started := p.now()
				out, err := work(commit, id)
				res := Progress{DocID: id, Total: len(docs), Outcome: out, Duration: p.now().Sub(started)}
				if err != nil {
					res.Err = err.Error()
				}
				results <- res
			}
		}()
	}
	go func() {
		defer close(queue)
		for _, id := range docs {
			select {
			case queue <- id:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	finished := 0
	for res := range results {
		finished++
		res.Done = finished
		if p.Report != nil {
			p.Report(res)
		}
		summary.Usage = route.AddUsage(summary.Usage, res.Outcome.Usage)
		summary.Calls += res.Outcome.Calls
		switch {
		case res.Err != "":
			summary.Failed++
		case res.Outcome.Skipped:
			summary.Skipped++
		default:
			summary.Done++
			summary.Produced += res.Outcome.Produced
		}
	}
	// Left is what the drain did not reach, and it is reported rather than
	// inferred because a pass that stopped early and a pass that finished print
	// the same summary otherwise.
	summary.Left = summary.Queued - summary.Done - summary.Failed - summary.Skipped
	summary.Duration = p.now().Sub(summary.StartedAt)
	return summary
}

// Todo drops the documents a pass has already written an artifact for.
//
// Resume is the whole reason these passes can be run at breadth. The artifact
// is the commit point, exactly as it is for norms: a document that has one is
// done, a document that does not is queued, and the queue is recomputed from
// disk on every run rather than remembered anywhere. Nothing needs to be
// checkpointed and an interrupted pass costs the documents that were in flight.
func Todo(docs []string, done func(docID string) bool) []string {
	out := make([]string, 0, len(docs))
	for _, id := range docs {
		if done(id) {
			continue
		}
		out = append(out, id)
	}
	return out
}
