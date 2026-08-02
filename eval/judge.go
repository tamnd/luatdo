package eval

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Validating the judge.
//
// Every precision figure this project has reported was produced by an
// entailment judge, and an unvalidated instrument produces numbers of unknown
// meaning. The specific failure is silent: a judge that agrees with the
// extractor because both are the same model reading the same words will pass
// everything the extractor produces and report excellent precision on a
// pipeline that is wrong.
//
// So a sample of its decisions goes to a person, and the agreement is reported
// next to every precision figure the judge produced, not in a separate document
// nobody opens.
//
// The sample file withholds the judge's verdict. That is not politeness, it is
// the difference between measuring a person's reading of a provision and
// measuring their willingness to agree with a machine, and the two produce the
// same file at the end.

// LabelFile is where the blind sample is written for a person to work through.
const LabelFile = "judge_labels.jsonl"

// KeyFile holds the judge's verdicts for the same items, joined by identifier
// at scoring time.
const KeyFile = "judge_key.jsonl"

// RerunFile holds a second set of machine verdicts over the same items, from a
// judge whose prompt has since been changed.
//
// It is a separate file rather than an overwrite because the labels were drawn
// blind against the first key, and a prompt that is tuned until the kappa comes
// out well is measured by nothing. The labels stay fixed, the key moves, and
// the two numbers are reported side by side with the count of items that
// changed verdict.
const RerunFile = "judge_rerun.jsonl"

// The verdicts a person or a judge can give. They are the norm layer's own
// words so nothing has to be translated at the join.
const (
	LabelEntailed    = "entailed"
	LabelNotEntailed = "not_entailed"
	LabelUnsure      = "unsure"
)

// Item is one statement put to a person, with everything needed to decide and
// nothing that says what the machine thought.
type Item struct {
	ID          string `json:"id"`
	ProvisionID string `json:"provision_id"`
	// Text is the provision as the judge saw it, so the person reads the same
	// words. A sample that shows a person a shorter window than the judge got
	// measures the window rather than the judge.
	Text      string `json:"text"`
	Statement string `json:"statement"`
	Quote     string `json:"quote"`
	// Human is what the annotator fills in: entailed, not_entailed, or unsure.
	Human string `json:"human"`
	// Why is optional and is the most valuable field in the file, because the
	// disagreements are where the judge's prompt gets fixed.
	Why string `json:"why,omitempty"`
}

// Key is the machine's verdict for one item.
type Key struct {
	ID      string `json:"id"`
	Machine string `json:"machine"`
	// Stratum records which side of the gate the item came from, so a sample
	// that oversampled rejections can say so rather than reporting a kappa over
	// a distribution nobody would recognise.
	Stratum string `json:"stratum"`
}

// Sample draws a reproducible stratified sample of judged items.
//
// Rejections are oversampled on purpose. They are rare, they are the decisions
// nobody sees again because the record never reaches the trusted store, and
// they are the ones where a wrong judge silently deletes correct extractions.
// A proportional sample of a pass that rejects one statement in ten spends
// ninety percent of a person's afternoon confirming the easy ones.
func Sample(items []Item, keys []Key, n, rejectShare int, seed string) ([]Item, []Key) {
	byID := map[string]Key{}
	for _, k := range keys {
		byID[k.ID] = k
	}
	var rejected, rest []Item
	for _, it := range items {
		if byID[it.ID].Machine == LabelNotEntailed {
			rejected = append(rejected, it)
			continue
		}
		rest = append(rest, it)
	}
	order := func(in []Item) {
		sort.Slice(in, func(i, j int) bool { return rank(seed, in[i].ID) < rank(seed, in[j].ID) })
	}
	order(rejected)
	order(rest)

	wantRejected := min(n*rejectShare/100, len(rejected))
	out := append([]Item{}, rejected[:wantRejected]...)
	out = append(out, rest[:min(n-wantRejected, len(rest))]...)

	outKeys := make([]Key, 0, len(out))
	for i := range out {
		k := byID[out[i].ID]
		k.Stratum = "accepted"
		if k.Machine == LabelNotEntailed {
			k.Stratum = "rejected"
		}
		outKeys = append(outKeys, k)
		out[i].Human = "" // blind, whatever was in the input
	}
	return out, outKeys
}

// rank is the reproducible shuffle: the same seed and the same corpus draw the
// same sample on any machine, which is what makes a reported kappa checkable.
func rank(seed, id string) string {
	sum := sha256.Sum256([]byte(seed + "\x00" + id))
	return hex.EncodeToString(sum[:])
}

// Judged joins the human labels to the machine verdicts and reports agreement.
//
// Unsure is dropped from the agreement and counted separately. An annotator who
// cannot decide has said something real about the provision, and folding that
// into either bucket would put their uncertainty on the judge's side of the
// ledger.
func Judged(labels []Item, keys []Key) (*Agreement, int, []Item) {
	byID := map[string]Key{}
	for _, k := range keys {
		byID[k.ID] = k
	}
	a := NewAgreement(LabelEntailed, LabelNotEntailed)
	unsure := 0
	var disagreed []Item
	for _, it := range labels {
		k, ok := byID[it.ID]
		if !ok || it.Human == "" {
			continue
		}
		if it.Human == LabelUnsure {
			unsure++
			continue
		}
		a.Observe(it.Human, k.Machine)
		if it.Human != k.Machine {
			disagreed = append(disagreed, it)
		}
	}
	return a, unsure, disagreed
}

// RejectionCheck is the second validation the milestone asks for: of the
// statements the entailment gate threw away, how many should have been kept.
//
// This is reported as a rate over the rejected stratum alone, never blended
// into the overall agreement, because the sample oversampled it. A number that
// mixes a stratified sample back together without reweighting is a number about
// the sample and not about the pass.
func RejectionCheck(labels []Item, keys []Key) Count {
	byID := map[string]Key{}
	for _, k := range keys {
		byID[k.ID] = k
	}
	var c Count
	for _, it := range labels {
		k, ok := byID[it.ID]
		if !ok || k.Stratum != "rejected" || it.Human == "" || it.Human == LabelUnsure {
			continue
		}
		// The gate said not entailed. It was right when the human agrees.
		c.Observe(it.Human == LabelNotEntailed, true)
	}
	return c
}

// Move is a count of items whose machine verdict changed between two keys.
type Move struct {
	From, To string
	N        int
}

// Moves compares two sets of machine verdicts over the same items. It is how a
// prompt change is reported: not as an improved kappa on its own, which any
// change to a rejection rate produces, but as the verdicts that moved and the
// direction they moved in.
func Moves(before, after []Key) []Move {
	was := map[string]string{}
	for _, k := range before {
		was[k.ID] = k.Machine
	}
	counts := map[[2]string]int{}
	for _, k := range after {
		from, ok := was[k.ID]
		if !ok || from == k.Machine {
			continue
		}
		counts[[2]string{from, k.Machine}]++
	}
	out := make([]Move, 0, len(counts))
	for pair, n := range counts {
		out = append(out, Move{From: pair[0], To: pair[1], N: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N > out[j].N })
	return out
}

// WriteItems and the readers below keep the label file as JSON lines, so a
// person can work it in any editor and a half finished file still parses.
func WriteItems(path string, items []Item) error { return writeLines(path, items) }
func WriteKeys(path string, keys []Key) error    { return writeLines(path, keys) }

func writeLines[T any](path string, rows []T) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			_ = f.Close()
			return err
		}
	}
	// The close is returned rather than deferred and dropped. A label file that
	// failed to flush is a label file with the last few items missing, and the
	// only thing worse than losing them is not being told.
	return f.Close()
}

func ReadItems(path string) ([]Item, error) { return readLines[Item](path) }
func ReadKeys(path string) ([]Key, error)   { return readLines[Key](path) }

func readLines[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []T
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for line := 1; s.Scan(); line++ {
		text := strings.TrimSpace(s.Text())
		if text == "" {
			continue
		}
		var row T
		if err := json.Unmarshal([]byte(text), &row); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		out = append(out, row)
	}
	if err := s.Err(); err != nil && err != io.EOF {
		return nil, err
	}
	return out, nil
}
