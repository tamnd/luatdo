package concept

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tamnd/luatdo/law"
)

// The concept layer is stored as one job file per document and three append
// only logs. The logs are append only for the same reason the norm review is:
// a decision file that gets rewritten cannot be audited, and the question of
// who merged two terms and when is exactly the sort of thing somebody asks six
// months later.

// File names inside the concept directory.
const (
	QuestionsFile = "questions.jsonl"
	AnswersFile   = "answers.jsonl"
	ClustersFile  = "clusters.jsonl"
	// LayerFile holds the built layer. It is derived from the jobs and the
	// answers and is rewritten whole every build, so it is the one file here
	// that is safe to lose.
	LayerFile = "layer.json"
)

// ReadLayer loads a built layer. A store where the build has never run is not
// an error: the export projects the document graph alone, the same way it does
// without the ontology.
func ReadLayer(dir string) (*Layer, error) {
	data, err := os.ReadFile(filepath.Join(dir, LayerFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var l Layer
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("read %s: %w", LayerFile, err)
	}
	return &l, nil
}

// JobPath is where one document's reading job lives.
func JobPath(dir, docID string) string { return filepath.Join(dir, law.FileName(docID)) }

// WriteJob stores one document's reading.
func WriteJob(dir string, jobs []Job) error {
	if len(jobs) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeJSON(JobPath(dir, jobs[0].DocID), jobs)
}

// ReadJob returns one document's reading, or nil when the document has not
// been read. A missing file is not an error: most documents in the corpus have
// no definitions article at all, so absence is the normal case.
func ReadJob(dir, docID string) ([]Job, error) {
	data, err := os.ReadFile(JobPath(dir, docID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var jobs []Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

// TermUses gathers the accepted readings out of a set of jobs.
func TermUses(jobs []Job) []TermUse {
	var out []TermUse
	for i := range jobs {
		out = append(out, jobs[i].TermUses...)
	}
	return out
}

// AskQuestions appends questions to the queue.
func AskQuestions(dir string, qs []Question) error {
	return appendJSONL(filepath.Join(dir, QuestionsFile), qs)
}

// RecordAnswers appends answers.
func RecordAnswers(dir string, as []Answer) error {
	return appendJSONL(filepath.Join(dir, AnswersFile), as)
}

// WriteClusters replaces the cluster file. Clusters are derived from the
// readings and are rebuilt whenever the readings change, so this one is not
// append only: nothing depends on the history of a proposal.
func WriteClusters(dir string, cs []Cluster) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, ClustersFile)
	if len(cs) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	f, err := os.CreateTemp(dir, "."+ClustersFile+".tmp*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(name)
	}()
	enc := json.NewEncoder(f)
	for _, c := range cs {
		if err := enc.Encode(c); err != nil {
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// ReadQuestions returns every queued question in order.
func ReadQuestions(dir string) ([]Question, error) {
	return readJSONL[Question](filepath.Join(dir, QuestionsFile))
}

// ReadAnswers returns every answer in order.
func ReadAnswers(dir string) ([]Answer, error) {
	return readJSONL[Answer](filepath.Join(dir, AnswersFile))
}

// ReadClusters returns the current clusters.
func ReadClusters(dir string) ([]Cluster, error) {
	return readJSONL[Cluster](filepath.Join(dir, ClustersFile))
}

// Pending folds questions and answers: the questions nobody has answered yet,
// in queue order, with a question asked twice counted once. The pair is the
// key rather than a generated identifier, so re-running the clustering over a
// corpus that gained documents does not re-ask what has been settled.
func Pending(questions []Question, answers []Answer) []Question {
	answered := map[[2]string]bool{}
	for _, a := range answers {
		answered[[2]string{a.A, a.B}] = true
	}
	var out []Question
	seen := map[[2]string]bool{}
	for _, q := range questions {
		key := [2]string{q.A.ID, q.B.ID}
		if answered[key] || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, q)
	}
	return out
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
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
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var row T
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, scanner.Err()
}
