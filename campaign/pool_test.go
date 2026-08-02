package campaign

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tamnd/luatdo/api"
)

func TestThePoolReadsEveryDocumentOnceAndCountsWhatEachOneCost(t *testing.T) {
	docs := make([]string, 50)
	for i := range docs {
		docs[i] = fmt.Sprintf("doc-%02d", i)
	}
	var mu sync.Mutex
	seen := map[string]int{}
	p := &Pool{Workers: 8}
	summary := p.Run(context.Background(), docs, func(_ context.Context, id string) (Outcome, error) {
		mu.Lock()
		seen[id]++
		mu.Unlock()
		return Outcome{Produced: 2, Calls: 1, Usage: api.Usage{TotalTokens: 10}}, nil
	})

	if len(seen) != len(docs) {
		t.Errorf("read %d distinct documents, want %d", len(seen), len(docs))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s read %d times, want once", id, n)
		}
	}
	if summary.Done != 50 || summary.Failed != 0 || summary.Left != 0 {
		t.Errorf("summary = %+v, want 50 done and nothing left", summary)
	}
	if summary.Produced != 100 || summary.Calls != 50 || summary.Usage.TotalTokens != 500 {
		t.Errorf("produced %d, calls %d, tokens %d", summary.Produced, summary.Calls, summary.Usage.TotalTokens)
	}
}

// A document that fails takes itself out of the pass and nothing else. The
// alternative, ending the run on the first error, means one model timeout
// eleven hours into a corpus pass throws away the eleven hours.
func TestOneFailedDocumentDoesNotEndThePass(t *testing.T) {
	docs := []string{"a", "b", "c", "d"}
	p := &Pool{Workers: 2}
	summary := p.Run(context.Background(), docs, func(_ context.Context, id string) (Outcome, error) {
		if id == "b" {
			return Outcome{}, errors.New("model said no")
		}
		if id == "c" {
			return Outcome{Skipped: true}, nil
		}
		return Outcome{Produced: 1}, nil
	})
	if summary.Done != 2 || summary.Failed != 1 || summary.Skipped != 1 {
		t.Errorf("summary = %+v, want 2 done, 1 failed, 1 skipped", summary)
	}
	if summary.Left != 0 {
		t.Errorf("left = %d, want 0, every document was reached", summary.Left)
	}
}

// Cancelling stops the queue and lets the work in flight commit. The document
// that was running has already cost the model calls that read it, so throwing
// its result away would mean paying for it twice.
func TestCancellingDrainsRatherThanAborts(t *testing.T) {
	docs := make([]string, 100)
	for i := range docs {
		docs[i] = fmt.Sprintf("doc-%03d", i)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	committed := 0
	p := &Pool{Workers: 2}
	summary := p.Run(ctx, docs, func(work context.Context, _ string) (Outcome, error) {
		if work.Err() != nil {
			return Outcome{}, errors.New("the work context was cancelled, so this document could not commit")
		}
		mu.Lock()
		committed++
		n := committed
		mu.Unlock()
		if n == 3 {
			cancel()
		}
		time.Sleep(time.Millisecond)
		return Outcome{Produced: 1}, nil
	})
	defer cancel()

	if summary.Failed != 0 {
		t.Errorf("%d documents failed, so the drain cancelled work that was already running", summary.Failed)
	}
	if summary.Done == 0 {
		t.Error("the drain committed nothing, so the work in flight was thrown away")
	}
	if summary.Done == len(docs) {
		t.Error("the whole queue ran, so cancelling did not stop new work being started")
	}
	if summary.Left != len(docs)-summary.Done {
		t.Errorf("left = %d, want %d, the summary has to say what the drain did not reach", summary.Left, len(docs)-summary.Done)
	}
}

func TestTodoKeepsTheDocumentsWithNoArtifact(t *testing.T) {
	done := map[string]bool{"a": true, "c": true}
	got := Todo([]string{"a", "b", "c", "d"}, func(id string) bool { return done[id] })
	if len(got) != 2 || got[0] != "b" || got[1] != "d" {
		t.Errorf("Todo = %v, want [b d]", got)
	}
}
