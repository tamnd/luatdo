// Package review is the human gate between verified statements and the
// trusted store.
//
// Routing is rule based and conservative: low confidence, sanctions,
// exceptions, and anything a judge did not fully entail go to a human. The
// queue and the decisions are both append-only JSONL, so the review history
// is an audit trail, not a mutable state file.
package review

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tamnd/luatdo/norm"
)

// ConfidenceGate is the threshold below which a statement needs human eyes.
const ConfidenceGate = 0.85

// Item is one statement queued for review with the reasons it was routed.
type Item struct {
	StatementID string         `json:"statement_id"`
	ProvisionID string         `json:"provision_id"`
	DocID       string         `json:"doc_id"`
	Reasons     []string       `json:"reasons"`
	Statement   norm.Statement `json:"statement"`
	At          string         `json:"at"`
}

// Decision is one human verdict on one queued statement.
type Decision struct {
	StatementID string `json:"statement_id"`
	Verdict     string `json:"verdict"` // approved or rejected
	Note        string `json:"note,omitempty"`
	At          string `json:"at"`
}

// Reasons returns why a record needs review, or nil when it can pass
// unattended. Invalid records never reach the queue; they failed the machine
// checks and a human cannot fix the extraction by approving it.
func Reasons(rec *norm.Record) []string {
	if rec.Status == "invalid" {
		return nil
	}
	var out []string
	if rec.Statement.Confidence < ConfidenceGate {
		out = append(out, fmt.Sprintf("confidence %.2f below %.2f", rec.Statement.Confidence, ConfidenceGate))
	}
	if rec.Statement.Type == "sanction" || rec.Statement.Sanction != nil {
		out = append(out, "sanction extracted")
	}
	// A sanction whose basis names another instrument is an edge across
	// documents, and a wrong one points at a penalty that does not exist. Those
	// go to a human whatever the confidence said.
	if s := rec.Statement.Sanction; s != nil && s.BasisDoc != "" && s.BasisDoc != rec.DocID {
		out = append(out, "sanction basis in another document")
	}
	// A bearer the extractor could not place in the registry is question 10's
	// exact case: nobody downstream can tell a drafting defect from a miss.
	if b := rec.Statement.Bearer; b != nil && b.ClassID == "" {
		out = append(out, "bearer not placed in the registry")
	}
	if len(rec.Statement.Exceptions) > 0 {
		out = append(out, "exception detected")
	}
	if rec.Entailment != nil && rec.Entailment.Verdict != norm.VerdictEntailed {
		out = append(out, "entailment verdict "+rec.Entailment.Verdict)
	}
	if rec.Falsification != nil && rec.Falsification.Verdict != norm.VerdictEntailed {
		out = append(out, "falsification verdict "+rec.Falsification.Verdict)
	}
	return out
}

func queuePath(dir string) string     { return filepath.Join(dir, "queue.jsonl") }
func decisionsPath(dir string) string { return filepath.Join(dir, "decisions.jsonl") }

// Enqueue appends items to the queue.
func Enqueue(dir string, items []Item) error {
	return appendJSONL(queuePath(dir), items)
}

// Decide appends one decision.
func Decide(dir string, d Decision) error {
	return appendJSONL(decisionsPath(dir), []Decision{d})
}

// ReadQueue returns every queued item in order.
func ReadQueue(dir string) ([]Item, error) {
	return readJSONL[Item](queuePath(dir))
}

// ReadDecisions returns every decision in order.
func ReadDecisions(dir string) ([]Decision, error) {
	return readJSONL[Decision](decisionsPath(dir))
}

// Pending folds queue and decisions: one entry per statement, in queue
// order, that no decision covers yet. Statement IDs are deterministic, so a
// re-run that queues the same statement again does not duplicate it.
func Pending(items []Item, decisions []Decision) []Item {
	decided := map[string]bool{}
	for _, d := range decisions {
		decided[d.StatementID] = true
	}
	var out []Item
	seen := map[string]bool{}
	for _, it := range items {
		if decided[it.StatementID] || seen[it.StatementID] {
			continue
		}
		seen[it.StatementID] = true
		out = append(out, it)
	}
	return out
}

// Approved reports whether the latest decision for a statement approved it.
func Approved(decisions []Decision, statementID string) bool {
	verdict := ""
	for _, d := range decisions {
		if d.StatementID == statementID {
			verdict = d.Verdict
		}
	}
	return verdict == "approved"
}

func appendJSONL[T any](path string, rows []T) error {
	if len(rows) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return f.Close()
}

func readJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []T
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for scanner.Scan() {
		var row T
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, scanner.Err()
}
