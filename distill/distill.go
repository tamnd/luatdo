// Package distill trains a cheap tagger on an expensive model's output.
//
// Pass C is a model reading a provision, and there are 1.9 million provisions
// in the corpus. Reading all of them is not a budget problem to be optimised
// later, it is a different project. So the model reads a stratified sample and
// becomes the teacher, its output becomes training data, and a tagger small
// enough to run over the whole corpus on a laptop learns to do the easy nine
// tenths of the job.
//
// The student is an averaged perceptron over hashed features. It is not chosen
// for accuracy. It is chosen because it trains in seconds, has no dependencies,
// produces the same weights on Linux, macOS and Windows given the same input,
// and can be read: a feature that gets a large weight is a feature somebody can
// look at and argue with. A neural tagger would score better and would be a
// black box in a project whose entire claim is that every fact in the graph can
// be traced to the thing that produced it.
//
// The student is measured against two different things and they are two
// different questions. Against the teacher's held out output it measures
// whether distillation worked, which is agreement. Against the hand annotated
// gold set it measures whether either of them is right, which is accuracy. A
// student that agrees with the teacher 0.95 of the time and is right 0.6 of the
// time has distilled a teacher that is wrong, and only reporting both says so.
package distill

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// Span is one labelled phrase inside a provision.
type Span struct {
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Kind  string `json:"kind,omitempty"`
}

// Example is one provision with the spans somebody or something said were
// legally operative concepts in it.
type Example struct {
	ProvisionID string `json:"provision_id"`
	Text        string `json:"text"`
	Spans       []Span `json:"spans"`
	// Source says where the labels came from: teacher or gold. It is carried
	// through so a training set can never be assembled by accident from a mix
	// of the two, which would make the evaluation meaningless.
	Source string `json:"source"`
}

// The sources a labelled example can have.
const (
	SourceTeacher = "teacher"
	SourceGold    = "gold"
)

// MaxSpanTokens is the longest candidate phrase considered, in whitespace
// tokens. Vietnamese legal terms are long: giay chung nhan quyen su dung dat is
// seven tokens and is one concept, so a limit tuned for English would miss the
// terms that matter most here.
const MaxSpanTokens = 8

// token is one whitespace separated word with its byte offsets.
type token struct {
	text  string
	start int
	end   int
}

// tokenize splits a provision into tokens and marks where a phrase may not
// cross. A concept does not span a comma, a semicolon or a full stop, and
// letting candidates cross them multiplies the candidate count by ten while
// adding nothing but noise.
func tokenize(text string) [][]token {
	var groups [][]token
	var cur []token
	i := 0
	for i < len(text) {
		for i < len(text) && isSpace(text[i]) {
			i++
		}
		if i >= len(text) {
			break
		}
		start := i
		for i < len(text) && !isSpace(text[i]) {
			i++
		}
		word := text[start:i]
		trimmed := strings.TrimRight(word, ".,;:)")
		broke := len(trimmed) != len(word)
		trimmed = strings.TrimLeft(trimmed, "(\"“”'")
		offset := start + (len(word) - len(strings.TrimLeft(word, "(\"“”'")))
		if trimmed != "" {
			cur = append(cur, token{text: trimmed, start: offset, end: offset + len(trimmed)})
		}
		if broke && len(cur) > 0 {
			groups = append(groups, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

// Candidates enumerates every phrase the student will decide about. It is
// deliberately generous and deliberately deterministic: recall lost here can
// never be recovered by the classifier, and a candidate set that changed
// between runs would make every measurement unrepeatable.
func Candidates(text string) []Span {
	var out []Span
	for _, group := range tokenize(text) {
		for i := range group {
			for n := 1; n <= MaxSpanTokens && i+n <= len(group); n++ {
				start, end := group[i].start, group[i+n-1].end
				out = append(out, Span{Text: text[start:end], Start: start, End: end})
			}
		}
	}
	return out
}

// features turns one candidate into the strings the model weighs. Everything
// here is computed from the provision alone, because the student has to run
// over documents nothing else has looked at.
func features(text string, group []token, i, n int, gazetteer map[string]bool) []string {
	phrase := group[i : i+n]
	key := law.Slug(text[phrase[0].start:phrase[n-1].end])
	head := law.Slug(phrase[0].text)
	tail := law.Slug(phrase[n-1].text)

	f := []string{
		"phrase=" + key,
		"head=" + head,
		"tail=" + tail,
		fmt.Sprintf("len=%d", n),
		"head+len=" + head + fmt.Sprintf("/%d", n),
	}
	if gazetteer[key] {
		// The single strongest feature, and the one that carries most of what
		// the teacher knew: a phrase the teacher named somewhere else is very
		// likely a concept here too.
		f = append(f, "in_gazetteer")
	}
	if i > 0 {
		f = append(f, "left="+law.Slug(group[i-1].text))
	} else {
		f = append(f, "left=<start>")
	}
	if i+n < len(group) {
		f = append(f, "right="+law.Slug(group[i+n].text))
	} else {
		f = append(f, "right=<end>")
	}
	if isDigits(phrase[0].text) || isDigits(phrase[n-1].text) {
		// A phrase that starts or ends in a number is usually a quantity or a
		// citation fragment rather than a concept.
		f = append(f, "has_number")
	}
	if startsUpper(phrase[0].text) {
		f = append(f, "starts_upper")
	}
	return f
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func startsUpper(s string) bool {
	if s == "" {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

// Tagger is the student. It is a weight per feature and a gazetteer, and that
// is the whole model.
type Tagger struct {
	Weights   map[string]float64 `json:"weights"`
	Gazetteer []string           `json:"gazetteer"`
	// Kinds maps a folded phrase to the kind the teacher gave it most often.
	// The student does not learn kinds, it remembers them: a kind is a
	// judgement about a concept and not about a span, so guessing one for a
	// phrase the teacher never saw would be inventing rather than tagging.
	Kinds map[string]string `json:"kinds"`
	// Epochs and Trained record how the weights came about, so a model file
	// found on disk can say what produced it.
	Epochs      int    `json:"epochs"`
	TrainedOn   int    `json:"trained_on"`
	TeacherHash string `json:"teacher_hash,omitempty"`
	// Source is what the weights were fitted to, teacher or gold. It is on the
	// model rather than remembered by whoever ran the training, because a
	// student trained on the gold set cannot then be scored against the gold
	// set as though it had never seen it, and a model file that does not say
	// what it learned from is one command away from being scored that way.
	Source string `json:"source,omitempty"`

	gaz map[string]bool
}

// Train fits the student on labelled examples. The order of examples decides
// the weights, so it is fixed here rather than left to the caller: two runs
// over the same teacher output produce the same model file, byte for byte.
func Train(examples []Example, epochs int) *Tagger {
	sorted := append([]Example(nil), examples...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ProvisionID < sorted[j].ProvisionID })

	t := &Tagger{Weights: map[string]float64{}, Kinds: map[string]string{}, Epochs: epochs, TrainedOn: len(sorted)}
	gaz := map[string]bool{}
	kindCounts := map[string]map[string]int{}
	for _, e := range sorted {
		for _, s := range e.Spans {
			key := law.Slug(s.Text)
			if key == "" {
				continue
			}
			gaz[key] = true
			if s.Kind != "" {
				if kindCounts[key] == nil {
					kindCounts[key] = map[string]int{}
				}
				kindCounts[key][s.Kind]++
			}
		}
	}
	t.gaz = gaz
	t.Gazetteer = append(t.Gazetteer, sortedKeys(gaz)...)
	for key, counts := range kindCounts {
		t.Kinds[key] = topKind(counts)
	}

	// The learning is in linear.go. What is here is the order the candidates
	// arrive in, which is the part that belongs to this task: every phrase of
	// every example, examples sorted, and the same walk on every machine.
	t.Weights = Fit(epochs, func(yield func([]string, bool) bool) {
		for _, e := range sorted {
			positive := map[[2]int]bool{}
			for _, s := range e.Spans {
				positive[[2]int{s.Start, s.End}] = true
			}
			for _, group := range tokenize(e.Text) {
				for i := range group {
					for n := 1; n <= MaxSpanTokens && i+n <= len(group); n++ {
						f := features(e.Text, group, i, n, gaz)
						if !yield(f, positive[[2]int{group[i].start, group[i+n-1].end}]) {
							return
						}
					}
				}
			}
		}
	})
	return t
}

// Tag runs the student over one provision. Overlapping accepted spans are
// resolved by score and then by length, so a sentence yields a set of phrases
// rather than every prefix of one phrase.
func (t *Tagger) Tag(text string) []Span {
	t.ensureGazetteer()
	type scored struct {
		span Span
		s    float64
	}
	var hits []scored
	for _, group := range tokenize(text) {
		for i := range group {
			for n := 1; n <= MaxSpanTokens && i+n <= len(group); n++ {
				f := features(text, group, i, n, t.gaz)
				s := Dot(t.Weights, f)
				if s <= 0 {
					continue
				}
				start, end := group[i].start, group[i+n-1].end
				hits = append(hits, scored{Span{Text: text[start:end], Start: start, End: end}, s})
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].s != hits[j].s {
			return hits[i].s > hits[j].s
		}
		if hits[i].span.End-hits[i].span.Start != hits[j].span.End-hits[j].span.Start {
			return hits[i].span.End-hits[i].span.Start > hits[j].span.End-hits[j].span.Start
		}
		return hits[i].span.Start < hits[j].span.Start
	})

	claimed := make([]bool, len(text)+1)
	var out []Span
	for _, h := range hits {
		taken := false
		for k := h.span.Start; k < h.span.End; k++ {
			if claimed[k] {
				taken = true
				break
			}
		}
		if taken {
			continue
		}
		for k := h.span.Start; k < h.span.End; k++ {
			claimed[k] = true
		}
		h.span.Kind = t.Kinds[law.Slug(h.span.Text)]
		out = append(out, h.span)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

func (t *Tagger) ensureGazetteer() {
	if t.gaz != nil {
		return
	}
	t.gaz = make(map[string]bool, len(t.Gazetteer))
	for _, g := range t.Gazetteer {
		t.gaz[g] = true
	}
}

// Write serialises the model. The weights are sorted on the way out so two
// trainings that produced the same model produce the same file.
func (t *Tagger) Write(w io.Writer) error {
	type pair struct {
		Feature string  `json:"f"`
		Weight  float64 `json:"w"`
	}
	out := struct {
		Weights     []pair            `json:"weights"`
		Gazetteer   []string          `json:"gazetteer"`
		Kinds       map[string]string `json:"kinds"`
		Epochs      int               `json:"epochs"`
		TrainedOn   int               `json:"trained_on"`
		TeacherHash string            `json:"teacher_hash,omitempty"`
		Source      string            `json:"source,omitempty"`
	}{
		Gazetteer: t.Gazetteer, Kinds: t.Kinds, Epochs: t.Epochs,
		TrainedOn: t.TrainedOn, TeacherHash: t.TeacherHash, Source: t.Source,
	}
	for _, f := range sortedKeys(t.Weights) {
		out.Weights = append(out.Weights, pair{f, t.Weights[f]})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// Read loads a model written by Write.
func Read(r io.Reader) (*Tagger, error) {
	var in struct {
		Weights []struct {
			Feature string  `json:"f"`
			Weight  float64 `json:"w"`
		} `json:"weights"`
		Gazetteer   []string          `json:"gazetteer"`
		Kinds       map[string]string `json:"kinds"`
		Epochs      int               `json:"epochs"`
		TrainedOn   int               `json:"trained_on"`
		TeacherHash string            `json:"teacher_hash,omitempty"`
		Source      string            `json:"source,omitempty"`
	}
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return nil, err
	}
	t := &Tagger{
		Weights: map[string]float64{}, Gazetteer: in.Gazetteer, Kinds: in.Kinds,
		Epochs: in.Epochs, TrainedOn: in.TrainedOn, TeacherHash: in.TeacherHash,
		Source: in.Source,
	}
	if t.Kinds == nil {
		t.Kinds = map[string]string{}
	}
	for _, p := range in.Weights {
		t.Weights[p.Feature] = p.Weight
	}
	t.ensureGazetteer()
	return t, nil
}

// Fingerprint is a stable hash of a training set, recorded on the model so a
// model file can say which teacher output it came from.
func Fingerprint(examples []Example) string {
	sorted := append([]Example(nil), examples...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ProvisionID < sorted[j].ProvisionID })
	h := fnv.New64a()
	for _, e := range sorted {
		// Hash writes never fail, and the alternative to ignoring the error is
		// threading one out of a fingerprint function that cannot fail.
		_, _ = io.WriteString(h, e.ProvisionID+"\x00")
		for _, s := range e.Spans {
			_, _ = io.WriteString(h, fmt.Sprintf("%d\x00%d\x00%s\x00", s.Start, s.End, s.Text))
		}
	}
	return fmt.Sprintf("%016x", h.Sum64())
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func topKind(counts map[string]int) string {
	best, bestN := "", -1
	for _, k := range sortedKeys(counts) {
		if counts[k] > bestN {
			best, bestN = k, counts[k]
		}
	}
	return best
}
