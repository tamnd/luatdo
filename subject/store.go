package subject

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Encode serialises records one per line. The file is read whole by every
// consumer and appended to by none, so a line oriented form buys nothing over
// a JSON array except the ability to look at it with a text editor, which on a
// hundred and twenty thousand records is worth more than it sounds.
func Encode(records []Record) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i := range records {
		if err := enc.Encode(&records[i]); err != nil {
			return nil, fmt.Errorf("encode %s: %w", records[i].DocID, err)
		}
	}
	return buf.Bytes(), nil
}

// EncodeSelection serialises a sample the same way, one selection per line.
func EncodeSelection(selection []Selection) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i := range selection {
		if err := enc.Encode(&selection[i]); err != nil {
			return nil, fmt.Errorf("encode %s: %w", selection[i].DocID, err)
		}
	}
	return buf.Bytes(), nil
}

// ReadRecords loads an assignments file.
func ReadRecords(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []Record
	sc := bufio.NewScanner(f)
	// A record is a few hundred bytes, but a document filed under three
	// subdomains with every matched cue recorded can run long, so the buffer is
	// sized for the worst line rather than the usual one.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// Summary counts what one classification run did.
type Summary struct {
	Documents   int            `json:"documents"`
	Assigned    int            `json:"assigned"`
	Unassigned  int            `json:"unassigned"`
	ByDomain    map[string]int `json:"by_domain"`
	BySubdomain map[string]int `json:"by_subdomain"`
	// Labels counts documents by how many subdomains they landed in, so a run
	// that files everything under one subject and a run that files everything
	// under three are told apart. Both are failures and they look identical in
	// a total.
	Labels map[int]int `json:"labels"`
}

// NewSummary returns an empty summary.
func NewSummary() *Summary {
	return &Summary{ByDomain: map[string]int{}, BySubdomain: map[string]int{}, Labels: map[int]int{}}
}

// Add folds one classified document into the counts.
func (s *Summary) Add(r *Record) {
	s.Documents++
	subdomains := 0
	for _, a := range r.Subjects {
		if strings.Contains(a.SubjectID, "/") {
			s.BySubdomain[a.SubjectID]++
			subdomains++
			continue
		}
		s.ByDomain[a.SubjectID]++
	}
	s.Labels[subdomains]++
	if len(r.Subjects) == 0 {
		s.Unassigned++
		return
	}
	s.Assigned++
}

func (s *Summary) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "documents  %d classified, %d under nothing\n", s.Assigned, s.Unassigned)
	fmt.Fprintf(&b, "domains    %d of the vocabulary used\n", len(s.ByDomain))
	fmt.Fprintf(&b, "subdomains %d of the vocabulary used\n", len(s.BySubdomain))

	labels := make([]int, 0, len(s.Labels))
	for n := range s.Labels {
		labels = append(labels, n)
	}
	sort.Ints(labels)
	var parts []string
	for _, n := range labels {
		parts = append(parts, fmt.Sprintf("%d with %d", s.Labels[n], n))
	}
	fmt.Fprintf(&b, "labels     %s\n", strings.Join(parts, ", "))

	fmt.Fprintf(&b, "by domain\n")
	type row struct {
		id string
		n  int
	}
	rows := make([]row, 0, len(s.ByDomain))
	for id, n := range s.ByDomain {
		rows = append(rows, row{id, n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].id < rows[j].id
	})
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-26s %d\n", r.id, r.n)
	}
	return strings.TrimRight(b.String(), "\n")
}

// SummaryFile and SelectionFile sit beside the assignments in the store.
const (
	SummaryFile   = "summary.json"
	SelectionFile = "sample.jsonl"
)
