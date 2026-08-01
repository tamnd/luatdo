package concept

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	// The discovery pass keeps its own files. Sightings live one file per
	// document beside the reading jobs, under a prefix, because a document has
	// both and one directory listing has to be able to tell them apart.
	SightingPrefix   = "sight_"
	AggregateFile    = "aggregate.jsonl"
	PromotionFile    = "promotions.jsonl"
	WorkingFile      = "working.jsonl"
	MentionPrefix    = "mentions_"
	TaggerFile       = "tagger.json"
	TeacherFile      = "teacher.jsonl"
	DiscoverySummary = "discovery.json"
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

// SightingPath is where one document's discovery output lives.
func SightingPath(dir, docID string) string {
	return filepath.Join(dir, SightingPrefix+law.FileName(docID))
}

// WriteSightings stores one document's discovery run.
func WriteSightings(dir string, ss []Sighting) error {
	if len(ss) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeJSON(SightingPath(dir, ss[0].DocID), ss)
}

// ReadSightings returns one document's discovery output, or nil when the
// document has not been read for concepts.
func ReadSightings(dir, docID string) ([]Sighting, error) {
	data, err := os.ReadFile(SightingPath(dir, docID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ss []Sighting
	if err := json.Unmarshal(data, &ss); err != nil {
		return nil, err
	}
	return ss, nil
}

// EachSighting streams every stored sighting file. The corpus has more
// documents than fit in memory at once, and the aggregation only needs the
// candidates, so the files go past one at a time.
func EachSighting(dir string, visit func([]Sighting) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), SightingPrefix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		var ss []Sighting
		if err := json.Unmarshal(data, &ss); err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := visit(ss); err != nil {
			return err
		}
	}
	return nil
}

// WriteAggregations replaces the aggregation file. Like the clusters it is
// derived and rebuilt whole, so nothing is lost by rewriting it.
func WriteAggregations(dir string, aggs []Aggregation) error {
	return replaceJSONL(dir, AggregateFile, aggs)
}

// ReadAggregations returns the aggregation output.
func ReadAggregations(dir string) ([]Aggregation, error) {
	return readJSONL[Aggregation](filepath.Join(dir, AggregateFile))
}

// WritePromotions replaces the promotion file.
func WritePromotions(dir string, ps []Promotion) error {
	return replaceJSONL(dir, PromotionFile, ps)
}

// ReadPromotions returns what was promoted.
func ReadPromotions(dir string) ([]Promotion, error) {
	return readJSONL[Promotion](filepath.Join(dir, PromotionFile))
}

// WriteWorkingDefinitions replaces the working definition file. It is rewritten
// rather than appended because a working definition is regenerated when its
// sources change, and keeping the superseded one would leave two readings of
// one concept in the store with nothing to say which is current.
func WriteWorkingDefinitions(dir string, ws []WorkingDefinition) error {
	return replaceJSONL(dir, WorkingFile, ws)
}

// ReadWorkingDefinitions returns the working definitions.
func ReadWorkingDefinitions(dir string) ([]WorkingDefinition, error) {
	return readJSONL[WorkingDefinition](filepath.Join(dir, WorkingFile))
}

// WriteMentions stores one document's mention report.
func WriteMentions(dir string, r *MentionReport) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, MentionPrefix+law.FileName(r.DocID)), r)
}

// ReadMentions returns one document's mention report, or nil.
func ReadMentions(dir, docID string) (*MentionReport, error) {
	data, err := os.ReadFile(filepath.Join(dir, MentionPrefix+law.FileName(docID)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var r MentionReport
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// replaceJSONL writes rows to a file atomically, removing it when there are no
// rows so that an empty derived file never sits on disk pretending to be a
// result somebody computed.
func replaceJSONL[T any](dir, name string, rows []T) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	if len(rows) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	f, err := os.CreateTemp(dir, "."+name+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}()
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
