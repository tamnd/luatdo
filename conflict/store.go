package conflict

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/store"
)

// The conflict directory holds one file of forms per document, plus the derived
// report and the benchmark.
//
// Per document for the reason every model pass in this repo is per document: the
// parse pass is a long job against a metered service, and a document that fails
// has to leave no artifact so that it comes back next run. The report is derived
// and rewritten whole, because checking is arithmetic over the forms and a stale
// report is worse than no report.
const (
	FormPrefix   = "form_"
	ReportFile   = "findings.json"
	BenchFile    = "bench.json"
	BaselineFile = "baseline.json"
	SummaryFile  = "summary.json"
)

// FormPath is where one document's forms live.
func FormPath(dir, docID string) string {
	return filepath.Join(dir, FormPrefix+law.FileName(docID))
}

// WriteForms replaces one document's forms. A document read and found to hold no
// comparable statement still gets a file: it is a result, and removing it would
// put the document back in the queue forever.
func WriteForms(dir, docID string, forms []*Form) error {
	SortForms(forms)
	if forms == nil {
		forms = []*Form{}
	}
	return store.WriteJSON(FormPath(dir, docID), forms)
}

// ReadForms returns one document's forms. A document nobody parsed is not an
// error.
func ReadForms(dir, docID string) ([]*Form, error) {
	var out []*Form
	err := store.ReadJSON(FormPath(dir, docID), &out)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}

// AllForms reads every document's forms, in a fixed order.
func AllForms(dir string) ([]*Form, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), FormPrefix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var out []*Form
	for _, name := range names {
		var forms []*Form
		if err := store.ReadJSON(filepath.Join(dir, name), &forms); err != nil {
			return nil, err
		}
		out = append(out, forms...)
	}
	SortForms(out)
	return out, nil
}

// WriteReport replaces the derived report.
func WriteReport(dir string, r *Report) error {
	return store.WriteJSON(filepath.Join(dir, ReportFile), r)
}

// ReadReport returns the last check, or nil where nothing has been checked.
func ReadReport(dir string) (*Report, error) {
	var r Report
	err := store.ReadJSON(filepath.Join(dir, ReportFile), &r)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Bench is a benchmark run: the pairs, and what the checker did with them.
//
// The pairs are stored and not only the score. A precision figure with no pairs
// behind it cannot be argued with, and the baseline has to be graded over the
// same list rather than over a fresh one, or the two numbers are answers to
// different questions.
type Bench struct {
	PerMutation int `json:"per_mutation"`
	// Judged says whether a model was asked about the pairs containment could
	// not place. Two runs of the same cases with and without it produce two
	// different precisions, and neither is readable without knowing which.
	Judged bool   `json:"judged"`
	Cases  []Case `json:"cases"`
	Grade  *Grade `json:"grade"`
}

// WriteBench records a benchmark run beside the report it grades, so a precision
// figure quoted anywhere can be traced to the pairs it was computed over.
func WriteBench(dir string, b *Bench) error {
	return store.WriteJSON(filepath.Join(dir, BenchFile), b)
}

// ReadBench returns the last benchmark, or nil where none was built.
func ReadBench(dir string) (*Bench, error) {
	var b Bench
	err := store.ReadJSON(filepath.Join(dir, BenchFile), &b)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// WriteBaseline records what direct prompting scored over the same pairs.
func WriteBaseline(dir string, g *BaselineGrade) error {
	return store.WriteJSON(filepath.Join(dir, BaselineFile), g)
}

// ReadBaseline returns the last baseline run, or nil where none was made.
func ReadBaseline(dir string) (*BaselineGrade, error) {
	var g BaselineGrade
	err := store.ReadJSON(filepath.Join(dir, BaselineFile), &g)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// Summary is the one file to read to know where the detector stands on the real
// corpus, as opposed to on the generated pairs.
//
// The noise floor is in it rather than beside it, because the finding count
// means nothing without it. Forty findings with a noise floor of zero is a
// detector, and forty findings with a noise floor of a third is a detector
// firing on the same provision read twice.
type Summary struct {
	Forms    int `json:"forms"`
	Pairs    int `json:"pairs"`
	Compared int `json:"compared"`
	Findings int `json:"findings"`
	Shared   int `json:"shared"`
	// Disjoint counts the pairs a judge took out because the two sets of
	// circumstances cannot both hold. They are not findings and they are not
	// nothing, so they are counted rather than folded into either.
	Disjoint int `json:"disjoint"`

	ByRule map[string]int `json:"by_rule,omitempty"`
	Noise  Noise          `json:"noise"`
	// Material is what the scope gave the rules to work with, which is how a
	// finding count of zero is told apart from a detector that could not have
	// fired. See Material.
	Material Material `json:"material"`
}

// Summarize folds a check and its noise measurement into the summary.
func Summarize(r *Report, n Noise, m Material) Summary {
	s := Summary{
		Forms: r.Forms, Pairs: r.Pairs, Compared: r.Compared,
		Findings: len(r.Findings), Shared: r.Shared(), Disjoint: len(r.Disjoint),
		ByRule: map[string]int{}, Noise: n, Material: m,
	}
	for i := range r.Findings {
		s.ByRule[r.Findings[i].Rule]++
	}
	return s
}

// WriteSummary replaces the summary.
func WriteSummary(dir string, s Summary) error {
	return store.WriteJSON(filepath.Join(dir, SummaryFile), s)
}
