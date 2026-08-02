// Package store resolves the on-disk data layout and provides atomic writes.
//
// Every pipeline stage reads and writes plain files under one data directory.
// There is no database and no cursor: queues and reports are recomputed from
// what is on disk, so an interrupted run needs no recovery.
package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Store is a data directory.
type Store struct {
	Root string
}

// Open resolves the data directory from the argument, the LUATDO_DATA
// environment variable, or the default ~/data/luatdo, in that order.
func Open(root string) (*Store, error) {
	if root == "" {
		root = os.Getenv("LUATDO_DATA")
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
		root = filepath.Join(home, "data", "luatdo")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	return &Store{Root: root}, nil
}

func (s *Store) Raw() string      { return filepath.Join(s.Root, "raw") }
func (s *Store) Docs() string     { return filepath.Join(s.Root, "docs") }
func (s *Store) Cite() string     { return filepath.Join(s.Root, "cite") }
func (s *Store) Anchor() string   { return filepath.Join(s.Root, "anchor") }
func (s *Store) Subject() string  { return filepath.Join(s.Root, "subject") }
func (s *Store) Terms() string    { return filepath.Join(s.Root, "terms") }
func (s *Store) Concepts() string { return filepath.Join(s.Root, "concept") }
func (s *Store) Ontology() string { return filepath.Join(s.Root, "ontology") }
func (s *Store) Extracts() string { return filepath.Join(s.Root, "extract") }
func (s *Store) Links() string    { return filepath.Join(s.Root, "link") }
func (s *Store) Relation() string { return filepath.Join(s.Root, "relation") }
func (s *Store) Temporal() string { return filepath.Join(s.Root, "temporal") }
func (s *Store) Norms() string    { return filepath.Join(s.Root, "norms") }
func (s *Store) Review() string   { return filepath.Join(s.Root, "review") }
func (s *Store) Trusted() string  { return filepath.Join(s.Root, "trusted") }
func (s *Store) Campaign() string { return filepath.Join(s.Root, "campaign") }
func (s *Store) Eval() string     { return filepath.Join(s.Root, "eval") }
func (s *Store) Export() string   { return filepath.Join(s.Root, "export") }

// WriteFile writes data atomically: a temp file in the same directory,
// fsynced, then renamed over the destination. os.Rename replaces the
// destination on every supported platform, including Windows.
func WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(name)
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// WriteJSON writes v as indented JSON, atomically.
func WriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, append(data, '\n'))
}

// ReadJSON reads path into v.
func ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// ReadJSONL reads a file of one JSON object per line. A file nobody has
// written is not an error: every stage that reads one is asking what an earlier
// stage produced, and "nothing yet" is an answer.
func ReadJSONL[T any](path string) ([]T, error) {
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
	// A provision with its full text on one line is the largest row any stage
	// writes, and the default 64 KB buffer is smaller than a long article.
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

// AppendJSONL appends rows to a file of one JSON object per line.
//
// This is the append-only half of the pair, for files that record what happened
// rather than what is currently true: annotations, decisions, queues. Nothing
// rewrites them, so a crash mid-run loses the row being written and not the
// ones before it.
func AppendJSONL[T any](path string, rows []T) error {
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

// HashFile returns the hex SHA-256 of the file at path.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashBytes returns the hex SHA-256 of data.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
