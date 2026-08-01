// Package coverage reports pipeline state, recomputed from disk on every run.
//
// The inputs are directories. There is no state file and no cursor, so the
// report can never drift from the truth, and the same counts drive both the
// human report and the work queue.
package coverage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/store"
)

// Report is the state of one data directory.
type Report struct {
	Documents   int            `json:"documents"`
	Parsed      int            `json:"parsed"`
	Quarantined int            `json:"quarantined"`
	Provisions  map[string]int `json:"provisions"`
	Cited       int            `json:"cited_documents"`
	Quarantines []string       `json:"quarantines,omitempty"`
}

// Compute walks the docs and cite directories.
func Compute(s *store.Store) (*Report, error) {
	r := &Report{Provisions: map[string]int{}}

	docsDir := s.Docs()
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var doc law.Document
		if err := store.ReadJSON(filepath.Join(docsDir, e.Name()), &doc); err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		r.Documents++
		if doc.Status == "quarantined" {
			r.Quarantined++
			r.Quarantines = append(r.Quarantines, doc.ID+": "+doc.Quarantine)
		} else {
			r.Parsed++
		}
		for i := range doc.Provisions {
			r.Provisions[doc.Provisions[i].Kind]++
		}
	}

	citeEntries, err := os.ReadDir(s.Cite())
	if err == nil {
		for _, e := range citeEntries {
			if strings.HasSuffix(e.Name(), ".json") {
				r.Cited++
			}
		}
	}
	return r, nil
}

func (r *Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "documents  %d parsed, %d quarantined, %d total\n", r.Parsed, r.Quarantined, r.Documents)
	for _, kind := range []string{"chapter", "section", "article", "clause", "point"} {
		if n := r.Provisions[kind]; n > 0 {
			fmt.Fprintf(&b, "%-10s %d\n", kind+"s", n)
		}
	}
	fmt.Fprintf(&b, "cited      %d documents scanned", r.Cited)
	return b.String()
}
