package relation

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// The relation layer is stored as one file of raw sightings per document plus
// three derived files rewritten whole.
//
// Per document because extraction is a long job against a metered service and a
// document that fails has to leave no artifact, which is exactly what puts it
// back in the queue next time. Derived files rewritten whole because folding is
// cheap and a stale fold is worse than no fold: it looks like a result somebody
// computed.

// File names inside the relation directory.
const (
	SightingPrefix = "rel_"
	// EdgesFile holds the folded layer.
	EdgesFile = "edges.jsonl"
	// ProposalsFile holds the invented relation types with their definitions
	// and their canonicalization outcomes. It is the review queue.
	ProposalsFile = "proposals.jsonl"
	// RegistryFile pins the relation vocabulary the run cited.
	RegistryFile = "registry.json"
	SummaryFile  = "summary.json"
)

// SightingPath is where one document's raw edges live.
func SightingPath(dir, docID string) string {
	return filepath.Join(dir, SightingPrefix+law.FileName(docID))
}

// WriteSightings replaces one document's raw edges. Writing nothing writes an
// empty file rather than removing it, because a document read and found to hold
// no relations is a result, and deleting the file would put it back in the queue
// forever.
func WriteSightings(dir, docID string, edges []Edge) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	for _, e := range edges {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return os.WriteFile(SightingPath(dir, docID), []byte(b.String()), 0o644)
}

// ReadSightings returns one document's raw edges. A document nobody read is not
// an error.
func ReadSightings(dir, docID string) ([]Edge, error) {
	return readJSONL[Edge](SightingPath(dir, docID))
}

// EachSighting visits every document's raw edges. It reads only sighting files,
// because one directory holds the sightings, the folded layer and the review
// queue, and a file that is not a sighting unmarshalled as one would be read as
// silence rather than as an error.
func EachSighting(dir string, visit func(docID string, edges []Edge) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), SightingPrefix) {
			continue
		}
		edges, err := readJSONL[Edge](filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		docID := ""
		if len(edges) > 0 && len(edges[0].Evidence) > 0 {
			docID = edges[0].Evidence[0].DocID
		}
		if err := visit(docID, edges); err != nil {
			return err
		}
	}
	return nil
}

// WriteEdges replaces the folded layer.
func WriteEdges(dir string, edges []Edge) error { return replaceJSONL(dir, EdgesFile, edges) }

// ReadEdges returns the folded layer.
func ReadEdges(dir string) ([]Edge, error) { return readJSONL[Edge](filepath.Join(dir, EdgesFile)) }

// WriteProposals replaces the review queue.
func WriteProposals(dir string, ps []Proposal) error { return replaceJSONL(dir, ProposalsFile, ps) }

// ReadProposals returns the review queue.
func ReadProposals(dir string) ([]Proposal, error) {
	return readJSONL[Proposal](filepath.Join(dir, ProposalsFile))
}

// WriteRegistry pins the relation vocabulary a run cited, so a layer built last
// month can still say which definitions its canonical edges were matched
// against.
func WriteRegistry(dir string, r *Registry) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, RegistryFile), append(data, '\n'), 0o644)
}

// ReadRegistry loads the pinned vocabulary, falling back to the seed when a
// store has never run the pass.
func ReadRegistry(dir string) (*Registry, error) {
	data, err := os.ReadFile(filepath.Join(dir, RegistryFile))
	if err != nil {
		if os.IsNotExist(err) {
			return SeedRegistry(1), nil
		}
		return nil, err
	}
	var r Registry
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("read %s: %w", RegistryFile, err)
	}
	return &r, nil
}

// Summary is what one relation run produced, for coverage.
type Summary struct {
	Documents int            `json:"documents"`
	Counts    Counts         `json:"counts"`
	Direction DirectionScore `json:"direction"`
	Densest   []Density      `json:"densest,omitempty"`
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
// nothing to write. An empty derived file sitting on disk pretends to be a
// result somebody computed.
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
