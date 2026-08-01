package temporal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// The temporal layer is stored as one file of raw operations per amending
// instrument, plus the derived version graph rewritten whole.
//
// Per instrument for the same reason the relation layer stores sightings per
// document: reading amendments is a long job against a metered service, and an
// instrument that fails must leave no artifact so it comes back in the queue.
// The version graph is derived and is rebuilt whole from the operations, because
// applying amendments in date order is cheap and a half rebuilt graph is a graph
// that answers point in time questions wrongly while looking finished.

// File names inside the temporal directory.
const (
	OperationPrefix = "ops_"
	// EventsFile holds the applied events.
	EventsFile = "events.jsonl"
	// VersionsFile holds the version graph.
	VersionsFile = "versions.jsonl"
	// QuarantineFile holds the operations that were read and not applied. It is
	// the review queue and it is never empty on a real corpus.
	QuarantineFile = "quarantined.jsonl"
	SummaryFile    = "summary.json"
)

// OperationPath is where one instrument's read operations live.
func OperationPath(dir, docID string) string {
	return filepath.Join(dir, OperationPrefix+law.FileName(docID))
}

// WriteOperations replaces one instrument's operations. An instrument read and
// found to hold no amendments writes an empty file rather than none, because
// that is a result and deleting it would put the instrument back in the queue
// forever.
func WriteOperations(dir, docID string, ops []Operation) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	for _, op := range ops {
		if err := enc.Encode(op); err != nil {
			return err
		}
	}
	return os.WriteFile(OperationPath(dir, docID), []byte(b.String()), 0o644)
}

// ReadOperations returns one instrument's operations. An instrument nobody read
// is not an error.
func ReadOperations(dir, docID string) ([]Operation, error) {
	return readJSONL[Operation](OperationPath(dir, docID))
}

// AllOperations returns every read operation, in file order, which is the input
// to a build.
func AllOperations(dir string) ([]Operation, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Operation
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), OperationPrefix) {
			continue
		}
		ops, err := readJSONL[Operation](filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, ops...)
	}
	return out, nil
}

// WriteLayer replaces the derived version graph.
func WriteLayer(dir string, l *Layer) error {
	if err := replaceJSONL(dir, EventsFile, l.Events); err != nil {
		return err
	}
	if err := replaceJSONL(dir, VersionsFile, l.Versions); err != nil {
		return err
	}
	return replaceJSONL(dir, QuarantineFile, l.Quarantined)
}

// ReadLayer loads the derived version graph. A store that has never been built
// returns an empty layer rather than an error, which is what lets a query
// report "no versions" instead of failing.
func ReadLayer(dir string) (*Layer, error) {
	events, err := readJSONL[Event](filepath.Join(dir, EventsFile))
	if err != nil {
		return nil, err
	}
	versions, err := readJSONL[Version](filepath.Join(dir, VersionsFile))
	if err != nil {
		return nil, err
	}
	quarantined, err := readJSONL[Operation](filepath.Join(dir, QuarantineFile))
	if err != nil {
		return nil, err
	}
	return &Layer{Events: events, Versions: versions, Quarantined: quarantined}, nil
}

// Summary is what one temporal run produced, for coverage.
//
// Documents versioned is separate from documents in the corpus on purpose. A
// document whose amendments were never read reports as unversioned rather than
// as unamended, because the difference between nothing changed and we did not
// look is the difference between a graph and a claim.
type Summary struct {
	Instruments  int            `json:"instruments"`  // instruments whose amendments were read
	Operations   int            `json:"operations"`   // operations read
	Applied      int            `json:"applied"`      // operations that became events
	Quarantined  int            `json:"quarantined"`  // operations kept and not applied
	Undated      int            `json:"undated"`      // events with no date, excluded from queries
	Versioned    int            `json:"versioned"`    // documents with a version graph
	Refused      int            `json:"refused"`      // documents not versioned because the parse or the date is unusable
	Versions     int            `json:"versions"`     // versions written
	Components   int            `json:"components"`   // components with at least one version
	Ties         []string       `json:"ties"`         // same day changes with no rule to order them
	Reasons      map[string]int `json:"reasons"`      // quarantine reason to count
	Kinds        map[string]int `json:"kinds"`        // event kind to count
	Problems     int            `json:"problems"`     // invariant violations
	Consolidated []Match        `json:"consolidated"` // consolidated text comparisons
}

// WriteSummary records the run.
func WriteSummary(dir string, s Summary) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, SummaryFile), append(data, '\n'), 0o644)
}

// ReadSummary returns the last run's numbers, or nil.
func ReadSummary(dir string) (*Summary, error) {
	data, err := os.ReadFile(filepath.Join(dir, SummaryFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s Summary
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("read %s: %w", SummaryFile, err)
	}
	return &s, nil
}

// replaceJSONL rewrites a derived file whole, and removes it when there is
// nothing to write. An empty derived file on disk pretends to be a result
// somebody computed.
func replaceJSONL[T any](dir, name string, rows []T) error {
	path := filepath.Join(dir, name)
	if len(rows) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
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
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row T
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		out = append(out, row)
	}
	return out, scanner.Err()
}
