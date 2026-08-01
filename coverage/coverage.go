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
	"sort"
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
	Extractable int            `json:"extractable_provisions"`
	Extracted   int            `json:"extracted_provisions"`
	Quarantines []string       `json:"quarantines,omitempty"`
}

// Compute walks the docs and cite directories.
func Compute(s *store.Store) (*Report, error) {
	r := &Report{Provisions: map[string]int{}}
	done, err := extracted(s)
	if err != nil {
		return nil, err
	}

	if err := eachDoc(s, func(doc *law.Document) error {
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
		for _, p := range Extractable(doc) {
			r.Extractable++
			if done[law.FileName(p.ID)] {
				r.Extracted++
			}
		}
		return nil
	}); err != nil {
		return nil, err
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

// Extractable returns the provisions of a document a norm pass should work on.
//
// The unit is the clause, because that is where a Vietnamese law states one
// rule. An article that was never divided into clauses is a unit of its own.
// Points are never units: they are enumerated fragments of their clause and
// travel inside its window, so extracting them separately would cover the same
// sentence twice and strip the fragment of the sentence that governs it.
func Extractable(doc *law.Document) []*law.Provision {
	hasChild := map[string]bool{}
	for i := range doc.Provisions {
		hasChild[doc.Provisions[i].ParentID] = true
	}
	var out []*law.Provision
	for i := range doc.Provisions {
		p := &doc.Provisions[i]
		if strings.TrimSpace(p.Text) == "" {
			continue
		}
		switch p.Kind {
		case "clause":
			out = append(out, p)
		case "article":
			if !hasChild[p.ID] {
				out = append(out, p)
			}
		}
	}
	return out
}

// Task is one unit of campaign work.
type Task struct {
	ProvisionID string `json:"provision_id"`
	DocID       string `json:"doc_id"`
	DocType     string `json:"doc_type"`
	Priority    int    `json:"priority"`
}

// priorities orders the campaign. The constitution and the codes come first
// because everything else cites them, so the terms and classes they define are
// in the graph before the documents that lean on them arrive. Both the English
// type names UTS_VLC uses and the Vietnamese ones vbpl.vn uses are recognised.
var priorities = map[string]int{
	"constitution": 0, "hiến pháp": 0,
	"code": 1, "bộ luật": 1,
	"law": 2, "luật": 2,
	"ordinance": 3, "pháp lệnh": 3,
	"decree": 4, "nghị định": 4,
	"decision": 5, "quyết định": 5,
	"circular": 6, "thông tư": 6,
}

// Priority ranks a document type. Unknown types sort last but are still
// queued; nothing is dropped for being unfamiliar.
func Priority(docType string) int {
	if p, ok := priorities[strings.ToLower(strings.TrimSpace(docType))]; ok {
		return p
	}
	return len(priorities)
}

// Queue recomputes the outstanding norm extraction work from disk: every
// extractable provision that has no job artifact yet, ordered by document
// priority and then by identifier so two runs agree on what comes next.
func Queue(s *store.Store) ([]Task, error) {
	done, err := extracted(s)
	if err != nil {
		return nil, err
	}
	var tasks []Task
	if err := eachDoc(s, func(doc *law.Document) error {
		if doc.Status == "quarantined" {
			return nil
		}
		priority := Priority(doc.DocType)
		for _, p := range Extractable(doc) {
			if done[law.FileName(p.ID)] {
				continue
			}
			tasks = append(tasks, Task{ProvisionID: p.ID, DocID: doc.ID, DocType: doc.DocType, Priority: priority})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority < tasks[j].Priority
		}
		return tasks[i].ProvisionID < tasks[j].ProvisionID
	})
	return tasks, nil
}

// extracted lists the norm job artifacts already on disk, by file name. One
// directory listing answers the whole queue; the job files themselves are
// never opened, so recomputing the queue stays cheap on a large corpus.
func extracted(s *store.Store) (map[string]bool, error) {
	entries, err := os.ReadDir(s.Norms())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	done := make(map[string]bool, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			done[e.Name()] = true
		}
	}
	return done, nil
}

func eachDoc(s *store.Store, visit func(*law.Document) error) error {
	entries, err := os.ReadDir(s.Docs())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var doc law.Document
		if err := store.ReadJSON(filepath.Join(s.Docs(), e.Name()), &doc); err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		if err := visit(&doc); err != nil {
			return err
		}
	}
	return nil
}

func (r *Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "documents  %d parsed, %d quarantined, %d total\n", r.Parsed, r.Quarantined, r.Documents)
	for _, kind := range []string{"chapter", "section", "article", "clause", "point"} {
		if n := r.Provisions[kind]; n > 0 {
			fmt.Fprintf(&b, "%-10s %d\n", kind+"s", n)
		}
	}
	fmt.Fprintf(&b, "cited      %d documents scanned\n", r.Cited)
	fmt.Fprintf(&b, "norms      %d of %d units extracted, %d pending",
		r.Extracted, r.Extractable, r.Extractable-r.Extracted)
	return b.String()
}
