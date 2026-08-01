// Package ontology holds the closed, versioned registry of classes and
// predicates the extractor is allowed to emit.
//
// The registry is the contract between the model and the graph. A version is
// frozen the first time an extraction job cites it and never edited again;
// changes go into the next version. Anything the model proposes beyond the
// registry lands in the candidates queue for a human decision, never in a
// statement.
package ontology

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tamnd/luatdo/store"
)

// Class is one entity class the extractor may assign.
type Class struct {
	ID           string   `json:"id"` // vn-legal:Employer
	LabelVI      string   `json:"label_vi"`
	Parent       string   `json:"parent,omitempty"`
	DefinitionVI string   `json:"definition_vi,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
	Examples     []string `json:"examples,omitempty"` // provision IDs
}

// Predicate is one relation the extractor may assert.
type Predicate struct {
	ID             string   `json:"id"`
	Domain         []string `json:"domain,omitempty"`
	Range          []string `json:"range,omitempty"`
	SurfaceFormsVI []string `json:"surface_forms_vi,omitempty"`
}

// Registry is one ontology version.
type Registry struct {
	Version    int         `json:"version"`
	FrozenAt   string      `json:"frozen_at,omitempty"`
	Classes    []Class     `json:"classes"`
	Predicates []Predicate `json:"predicates"`
}

// Frozen reports whether this version is immutable.
func (r *Registry) Frozen() bool { return r.FrozenAt != "" }

// Class returns the class with the given ID, or nil.
func (r *Registry) Class(id string) *Class {
	for i := range r.Classes {
		if r.Classes[i].ID == id {
			return &r.Classes[i]
		}
	}
	return nil
}

func path(dir string, version int) string {
	return filepath.Join(dir, fmt.Sprintf("v%d.json", version))
}

// Load returns the highest version present in dir.
func Load(dir string) (*Registry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("no ontology directory, run luatdo ontology init first: %w", err)
	}
	best := 0
	for _, e := range entries {
		var v int
		if _, err := fmt.Sscanf(e.Name(), "v%d.json", &v); err == nil && v > best {
			best = v
		}
	}
	if best == 0 {
		return nil, fmt.Errorf("no ontology versions under %s, run luatdo ontology init first", dir)
	}
	var r Registry
	if err := store.ReadJSON(path(dir, best), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Save writes the registry as its version file. A frozen version on disk is
// never overwritten.
func Save(dir string, r *Registry) error {
	var existing Registry
	if err := store.ReadJSON(path(dir, r.Version), &existing); err == nil && existing.Frozen() {
		return fmt.Errorf("ontology v%d is frozen, bump the version instead", r.Version)
	}
	return store.WriteJSON(path(dir, r.Version), r)
}

// Freeze stamps the current version immutable and returns it.
func Freeze(dir string, now time.Time) (*Registry, error) {
	r, err := Load(dir)
	if err != nil {
		return nil, err
	}
	if r.Frozen() {
		return r, nil
	}
	r.FrozenAt = now.UTC().Format(time.RFC3339)
	return r, store.WriteJSON(path(dir, r.Version), r)
}

// Candidate is one proposal awaiting a human decision. Decisions are appended,
// never rewritten, so the queue doubles as the audit trail.
type Candidate struct {
	Kind      string   `json:"kind"` // class or predicate
	Label     string   `json:"label"`
	Aliases   []string `json:"aliases,omitempty"`
	Provision string   `json:"provision,omitempty"`
	Quote     string   `json:"quote,omitempty"`
	Source    string   `json:"source"` // bootstrap or extract
	Status    string   `json:"status"` // proposed, approved, rejected, merged
	MergedTo  string   `json:"merged_to,omitempty"`
	Rationale string   `json:"rationale,omitempty"`
	At        string   `json:"at"`
}

func candidatesPath(dir string) string { return filepath.Join(dir, "candidates.jsonl") }

// AppendCandidates appends proposals to the queue file.
func AppendCandidates(dir string, cs []Candidate) error {
	if len(cs) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(candidatesPath(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for _, c := range cs {
		if err := enc.Encode(c); err != nil {
			return err
		}
	}
	return f.Close()
}

// ReadCandidates returns every line of the queue in order.
func ReadCandidates(dir string) ([]Candidate, error) {
	f, err := os.Open(candidatesPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []Candidate
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for scanner.Scan() {
		var c Candidate
		if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, scanner.Err()
}

// Pending folds the append-only queue into the open proposals: the latest
// entry per label wins, and only proposed ones remain.
func Pending(cs []Candidate) []Candidate {
	latest := map[string]Candidate{}
	var order []string
	for _, c := range cs {
		key := c.Kind + "|" + c.Label
		if _, seen := latest[key]; !seen {
			order = append(order, key)
		}
		latest[key] = c
	}
	var out []Candidate
	for _, key := range order {
		if latest[key].Status == "proposed" {
			out = append(out, latest[key])
		}
	}
	return out
}
