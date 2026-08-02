package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/tamnd/luatdo/campaign"
	"github.com/tamnd/luatdo/store"
)

// The front end the concept, relation and temporal passes share.
//
// All three read documents, write one artifact per document, and were written
// to be run over one law at a time. Giving each of them its own campaign flag,
// its own worker count and its own idea of what counts as already done would be
// three chances to get resume wrong, and resume is the part that has to be
// right: a pass that forgets what it read is a pass that cannot be interrupted,
// and a pass that cannot be interrupted never gets started on a corpus this
// size.

// breadth resolves which documents a pass should read and then reads them.
type breadth struct {
	// name is what the pass calls itself in its own output.
	name  string
	store *store.Store
	// scope is a campaign name, only is a single document, and they are
	// alternatives. A pass with neither reads the whole corpus, which is the
	// old behaviour and is still what somebody wants for the cheap passes.
	scope string
	only  string
	// limit is a number of documents rather than of provisions or units.
	//
	// It used to be the smaller thing, and it could not stay that way once the
	// document became the unit that commits. A limit that stops halfway through
	// a document either loses the provisions already paid for or writes an
	// artifact claiming the document was read, and the second is worse, because
	// it takes the rest of that document out of the queue forever.
	limit   int
	workers string
	dryRun  bool
	// done reports whether a document already carries this pass's artifact.
	done func(docID string) bool
}

// run works candidates, in order, after cutting them down to what this pass
// still has to do.
func (b breadth) run(candidates []string, work campaign.Work) (campaign.PoolSummary, error) {
	var summary campaign.PoolSummary
	docs := candidates
	if b.only != "" {
		docs = nil
		for _, id := range candidates {
			if id == b.only {
				docs = append(docs, id)
			}
		}
		if len(docs) == 0 {
			return summary, fmt.Errorf("%s: document %s has nothing for this pass to read", b.name, b.only)
		}
	}
	if b.scope != "" {
		sc, err := campaign.LookupScope(b.scope)
		if err != nil {
			return summary, err
		}
		_, inScope, err := campaignDocs(b.store, sc)
		if err != nil {
			return summary, err
		}
		kept := make([]string, 0, len(docs))
		for _, id := range docs {
			if inScope[id] {
				kept = append(kept, id)
			}
		}
		fmt.Printf("%s: campaign %s, %d of %d documents with content are in scope\n",
			b.name, sc.Name, len(kept), len(docs))
		docs = kept
	}

	queued := campaign.Todo(docs, b.done)
	fmt.Printf("%s: %d documents with content, %d already read, %d queued\n",
		b.name, len(docs), len(docs)-len(queued), len(queued))
	if b.limit > 0 && b.limit < len(queued) {
		queued = queued[:b.limit]
		fmt.Printf("%s: stopping after %d documents\n", b.name, b.limit)
	}
	if len(queued) == 0 {
		fmt.Printf("%s: nothing to read\n", b.name)
		return summary, nil
	}
	workers, err := parallelism(b.workers)
	if err != nil {
		return summary, err
	}
	if b.dryRun {
		fmt.Printf("%s: %d documents, %d workers, no model called\n", b.name, len(queued), workers)
		for i, id := range queued[:min(10, len(queued))] {
			fmt.Printf("  %2d %s\n", i+1, id)
		}
		if len(queued) > 10 {
			fmt.Printf("  ... %d more\n", len(queued)-10)
		}
		return summary, nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		// Hand the signal back so a second one kills the process. The first
		// only means stop starting documents.
		stop()
		fmt.Fprintln(os.Stderr, "draining, finishing the documents already in flight, signal again to abort")
	}()

	pool := &campaign.Pool{
		Workers: workers,
		// Progress goes to stderr and the summary to stdout, so a pass whose
		// output is being kept can be redirected without the running commentary
		// in it. A document takes a reasoning model minutes, and a pass that
		// says nothing for an hour looks exactly like a hang.
		Report: func(p campaign.Progress) { fmt.Fprintln(os.Stderr, p) },
	}
	fmt.Printf("%s: %d documents, %d workers\n", b.name, len(queued), workers)
	summary = pool.Run(ctx, queued, work)
	fmt.Printf("%s: %s\n", b.name, summary)
	return summary, nil
}
