package campaign

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/review"
	"github.com/tamnd/luatdo/route"
	"github.com/tamnd/luatdo/store"
)

const quote = "làm việc theo hợp đồng lao động"

// scripted answers by the kind of call rather than by position, because a
// campaign with several workers has no single call order to script against.
type scripted struct {
	mu     sync.Mutex
	calls  int
	fail   error
	before func(call int)
}

func (s *scripted) Complete(ctx context.Context, req api.Request) (api.Response, error) {
	s.mu.Lock()
	s.calls++
	n, hook, fail := s.calls, s.before, s.fail
	s.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	// A cancelled context here means the campaign tore up work already in
	// flight instead of draining it.
	if err := ctx.Err(); err != nil {
		return api.Response{}, err
	}
	if fail != nil {
		return api.Response{}, fail
	}
	usage := api.Usage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}
	if strings.Contains(req.Instructions, "trích xuất các quy phạm") {
		return api.Response{Text: fmt.Sprintf(
			`{"statements":[{"statement_type":"duty","subject":{"text":"Người lao động","class_id":"vn-legal:Employee"},"modality":"obligation","action":{"text":"làm việc"},"evidence":{"quote":%q},"confidence":0.6}]}`,
			quote), Usage: usage}, nil
	}
	return api.Response{Text: `{"verdict":"` + norm.VerdictEntailed + `","rationale":"ok"}`, Usage: usage}, nil
}

func (s *scripted) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

const docID = "vn:law:2019:45-2019-qh14"

// fixture writes one document of n clauses and returns the store and the
// queue that a campaign would recompute from it.
func fixture(t *testing.T, n int) (*store.Store, []coverage.Task) {
	t.Helper()
	s := &store.Store{Root: t.TempDir()}
	doc := &law.Document{
		ID: docID, Title: "Bộ luật Lao động", DocType: "code", Status: "parsed",
		Provisions: []law.Provision{
			{ID: docID + ":article-3", Kind: "article", Number: "3", Heading: "Giải thích từ ngữ"},
		},
	}
	for i := 1; i <= n; i++ {
		doc.Provisions = append(doc.Provisions, law.Provision{
			ID: fmt.Sprintf("%s:article-3:clause-%d", docID, i), ParentID: docID + ":article-3",
			Kind: "clause", Number: fmt.Sprint(i),
			Text: fmt.Sprintf("Khoản %d. Người lao động là người %s.", i, quote),
		})
	}
	if err := store.WriteJSON(filepath.Join(s.Docs(), law.FileName(doc.ID)), doc); err != nil {
		t.Fatal(err)
	}
	tasks, err := coverage.Queue(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != n {
		t.Fatalf("queue = %d tasks, want %d", len(tasks), n)
	}
	return s, tasks
}

func runner(s *store.Store, c api.Completer, workers int) *Runner {
	return &Runner{
		Store: s, Registry: ontology.Seed(), Completer: c,
		Pricing: &route.Pricing{InputPerM: 1, OutputPerM: 2},
		Model:   "m", Workers: workers,
	}
}

func jobFiles(t *testing.T, s *store.Store) []string {
	t.Helper()
	entries, err := os.ReadDir(s.Norms())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestRunWorksTheWholeQueue(t *testing.T) {
	s, tasks := fixture(t, 6)
	c := &scripted{}
	r := runner(s, c, 3)
	var mu sync.Mutex
	var lines []string
	r.Report = func(res Result) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, res.String())
	}

	summary, err := r.Run(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Done != 6 || summary.Failed != 0 || summary.Skipped != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.Statements != 6 || summary.Entailed != 6 {
		t.Errorf("statements = %d, entailed = %d, want 6 and 6", summary.Statements, summary.Entailed)
	}
	// Confidence 0.6 is under the review gate, so every statement is queued
	// for a human even though both the validator and the judge accepted it.
	if summary.Review != 6 {
		t.Errorf("review = %d, want 6", summary.Review)
	}
	if got := len(jobFiles(t, s)); got != 6 {
		t.Errorf("job artifacts = %d, want one per provision", got)
	}
	items, err := review.ReadQueue(s.Review())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 6 {
		t.Fatalf("review queue = %d items, want 6", len(items))
	}
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it.StatementID] {
			t.Errorf("%s queued twice, the shared queue file lost an append", it.StatementID)
		}
		seen[it.StatementID] = true
	}
	if len(lines) != 6 {
		t.Fatalf("reported lines = %d", len(lines))
	}
	if !strings.Contains(lines[0], "statements=1 entailed=1 review=1") {
		t.Errorf("reporting line = %q", lines[0])
	}

	// Two calls per provision, extraction and the entailment judge, at 1000
	// input and 100 output tokens each.
	if summary.Usage.TotalTokens != 12*1100 {
		t.Errorf("tokens = %d", summary.Usage.TotalTokens)
	}
	want := 12 * (1000.0/1e6*1 + 100.0/1e6*2)
	if !summary.Cost.Available || summary.Cost.USD < want*0.999 || summary.Cost.USD > want*1.001 {
		t.Errorf("cost = %v, want about %v", summary.Cost, want)
	}

	// The queue is recomputed from the artifacts, so a second run has nothing
	// left to do.
	again, err := coverage.Queue(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("%d provisions still queued after a clean campaign", len(again))
	}
}

func TestFailedProvisionLeavesNoArtifact(t *testing.T) {
	s, tasks := fixture(t, 3)
	c := &scripted{fail: errors.New("responses API returned 503: upstream down")}
	summary, err := runner(s, c, 2).Run(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Failed != 3 || summary.Done != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if got := jobFiles(t, s); len(got) != 0 {
		t.Errorf("artifacts = %v, an outage must not mark a provision done", got)
	}
	again, err := coverage.Queue(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 3 {
		t.Errorf("queue after a failed campaign = %d, want all 3 back", len(again))
	}
}

func TestMissingDocumentIsReportedNotFatal(t *testing.T) {
	s, tasks := fixture(t, 1)
	tasks = append(tasks, coverage.Task{ProvisionID: "vn:law:2000:1-2000-qh10:article-1", DocID: "vn:law:2000:1-2000-qh10"})
	summary, err := runner(s, &scripted{}, 1).Run(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Done != 1 || summary.Failed != 1 {
		t.Errorf("summary = %+v, one absent document must not stop the campaign", summary)
	}
}

func TestCancelDrainsRatherThanAborts(t *testing.T) {
	s, tasks := fixture(t, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	release := make(chan struct{})
	c := &scripted{}
	c.before = func(call int) {
		if call != 1 {
			return
		}
		// Hold the first extraction open, cancel while it is in flight, then
		// let it go. The feeder sees the cancellation before any worker is
		// free to take another provision.
		close(started)
		<-release
	}

	r := runner(s, c, 1)
	type outcome struct {
		summary Summary
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		summary, err := r.Run(ctx, tasks)
		done <- outcome{summary, err}
	}()

	<-started
	cancel()
	close(release)
	got := <-done

	if got.err != nil {
		t.Fatalf("Run: %v", got.err)
	}
	if got.summary.Failed != 0 {
		t.Errorf("failed = %d, a drain must not break the provisions in flight", got.summary.Failed)
	}
	if got.summary.Done != 1 || got.summary.Skipped != 3 {
		t.Errorf("summary = %+v, want the in flight provision finished and the rest left queued", got.summary)
	}
	if got := jobFiles(t, s); len(got) != 1 {
		t.Errorf("artifacts = %v, want the drained provision committed", got)
	}
	if c.count() != 2 {
		t.Errorf("model calls = %d, want the extraction and its judge and nothing more", c.count())
	}
	left, err := coverage.Queue(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 3 {
		t.Errorf("queue after the drain = %d, want the 3 untouched provisions", len(left))
	}
}

func TestRunOnAnEmptyQueue(t *testing.T) {
	s, _ := fixture(t, 1)
	summary, err := runner(s, &scripted{}, 4).Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Queued != 0 || summary.Done != 0 {
		t.Errorf("summary = %+v", summary)
	}
}
