package retrieve

import (
	"math"
	"sort"
	"strings"
)

// BM25 constants. They are the usual ones and nothing here tuned them.
const (
	k1 = 1.2
	b  = 0.75
)

// field is one aspect's inverted index.
type field struct {
	postings map[string][]posting
	length   []int
	average  float64
}

type posting struct {
	unit int
	freq int
}

func buildField(units []*Unit, aspect string) *field {
	f := &field{postings: map[string][]posting{}, length: make([]int, len(units))}
	total := 0
	for i, u := range units {
		counts := map[string]int{}
		for _, s := range u.aspects[aspect] {
			for _, t := range Tokens(s) {
				counts[t]++
				f.length[i]++
			}
		}
		total += f.length[i]
		for t, n := range counts {
			f.postings[t] = append(f.postings[t], posting{unit: i, freq: n})
		}
	}
	if len(units) > 0 {
		f.average = float64(total) / float64(len(units))
	}
	return f
}

// idf is computed over the whole index rather than over the scope subset.
//
// A term's rarity is a property of the corpus, and recomputing it inside every
// scope would make the same word worth a different amount depending on which
// filter the caller happened to apply. The subset decides what may be returned;
// it does not decide what "rare" means.
func (f *field) idf(term string, units int) float64 {
	df := len(f.postings[term])
	if df == 0 {
		return 0
	}
	return math.Log(1 + (float64(units)-float64(df)+0.5)/(float64(df)+0.5))
}

// Query is one search: what to look for, where it may be looked for, and how
// many components to bring back.
type Query struct {
	Text    string
	K       int
	Scope   Scope
	Weights map[string]float64

	// Duplicates, when set, is the Jaccard overlap above which a lower ranked
	// component counts as a restatement of a higher ranked one. Zero uses the
	// default; a negative number turns suppression off, which is how the
	// benchmark measures what suppression is worth.
	Duplicates float64
}

// DefaultDuplicates is the overlap at which two components are the same words.
//
// It is set high on purpose. Two clauses of the same article about the same
// deadline overlap a lot without being the same provision, and suppressing one
// of those loses an answer. What this is for is the snapshot problem: an
// amending law restates the article it amends, the corpus holds both wordings,
// and a search returns the same words twice under two identifiers.
const DefaultDuplicates = 0.85

// Hit is one retrieved component with the arithmetic that put it there.
type Hit struct {
	Unit       *Unit              `json:"-"`
	ID         string             `json:"component_id"`
	DocID      string             `json:"doc_id"`
	Score      float64            `json:"score"`
	ByAspect   map[string]float64 `json:"by_aspect,omitempty"`
	Duplicates []string           `json:"duplicates,omitempty"`
}

// Result is what a search did, not only what it found. The counts are here so
// that a caller can tell an empty answer caused by a scope that kept nothing
// from one caused by a query that matched nothing, which are different
// failures with different fixes.
type Result struct {
	Hits       []Hit   `json:"hits"`
	Indexed    int     `json:"indexed"`
	InScope    int     `json:"in_scope"`
	Matched    int     `json:"matched"`
	Suppressed int     `json:"suppressed"`
	Steps      []Step  `json:"scope_steps,omitempty"`
	Weights    []Aspec `json:"weights,omitempty"`
}

// Aspec is one aspect and the weight the query gave it, written out so a stored
// result says what produced it.
type Aspec struct {
	Aspect string  `json:"aspect"`
	Weight float64 `json:"weight"`
}

// Search ranks the components the scope kept.
//
// The order of the two halves is the whole design. Select runs first and
// returns a subset plus the arithmetic of how it shrank; ranking never sees a
// component the caller ruled out. Doing it the other way, ranking the corpus
// and filtering the top of the list, gives a caller who asked for one document
// a page of results from other documents that happened to score well.
func (ix *Index) Search(q Query) Result {
	keep, steps := ix.Select(q.Scope)
	inScope := 0
	for _, ok := range keep {
		if ok {
			inScope++
		}
	}
	res := Result{Indexed: len(ix.units), InScope: inScope, Steps: steps}

	weights := q.Weights
	if weights == nil {
		weights = DefaultWeights
	}
	for _, name := range Aspects {
		if w := weights[name]; w != 0 {
			res.Weights = append(res.Weights, Aspec{Aspect: name, Weight: w})
		}
	}

	terms := Tokens(q.Text)
	scores := map[int]float64{}
	byAspect := map[int]map[string]float64{}
	for _, name := range Aspects {
		w := weights[name]
		if w == 0 {
			continue
		}
		f := ix.fields[name]
		if f == nil {
			continue
		}
		for _, t := range terms {
			idf := f.idf(t, len(ix.units))
			if idf == 0 {
				continue
			}
			for _, p := range f.postings[t] {
				if !keep[p.unit] {
					continue
				}
				length := float64(f.length[p.unit])
				tf := float64(p.freq) * (k1 + 1) /
					(float64(p.freq) + k1*(1-b+b*length/math.Max(f.average, 1)))
				s := w * idf * tf
				scores[p.unit] += s
				if byAspect[p.unit] == nil {
					byAspect[p.unit] = map[string]float64{}
				}
				byAspect[p.unit][name] += s
			}
		}
	}
	res.Matched = len(scores)

	ranked := make([]Hit, 0, len(scores))
	for i, s := range scores {
		u := ix.units[i]
		ranked = append(ranked, Hit{Unit: u, ID: u.ComponentID, DocID: u.DocID, Score: s, ByAspect: byAspect[i]})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].ID < ranked[j].ID
	})

	k := q.K
	if k <= 0 {
		k = 10
	}
	res.Hits, res.Suppressed = suppress(ranked, k, q.Duplicates)
	return res
}

// suppress walks the ranking and folds a component into a higher ranked one
// when they are the same words under two identifiers.
//
// The suppressed identifiers are kept on the surviving hit rather than
// discarded. A user who asked what the law says wants one answer; a user
// checking whether the corpus holds a duplicate wants to know it was there, and
// throwing the list away serves the first at the cost of the second.
func suppress(ranked []Hit, k int, threshold float64) ([]Hit, int) {
	if threshold == 0 {
		threshold = DefaultDuplicates
	}
	var out []Hit
	var shingles []map[string]bool
	dropped := 0
	// Only the region of the ranking that can reach the answer is compared,
	// because shingling is quadratic and the tail of a BM25 ranking is noise.
	limit := min(len(ranked), max(k*5, 50))
	for i := 0; i < limit && len(out) < k; i++ {
		if threshold < 0 {
			out = append(out, ranked[i])
			continue
		}
		sh := shingle(ranked[i].Unit.Text)
		duplicate := -1
		for j := range out {
			if jaccard(sh, shingles[j]) >= threshold {
				duplicate = j
				break
			}
		}
		if duplicate >= 0 {
			out[duplicate].Duplicates = append(out[duplicate].Duplicates, ranked[i].ID)
			dropped++
			continue
		}
		out = append(out, ranked[i])
		shingles = append(shingles, sh)
	}
	return out, dropped
}

// shingle is the set of five syllable windows of a text, which is enough to
// tell a restatement from a different provision on the same topic.
func shingle(text string) map[string]bool {
	s := Syllables(text)
	out := map[string]bool{}
	const n = 5
	if len(s) < n {
		if len(s) > 0 {
			out[joinSyllables(s)] = true
		}
		return out
	}
	for i := 0; i+n <= len(s); i++ {
		out[joinSyllables(s[i:i+n])] = true
	}
	return out
}

func joinSyllables(s []string) string { return strings.Join(s, " ") }

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	small, large := a, b
	if len(large) < len(small) {
		small, large = large, small
	}
	for k := range small {
		if large[k] {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}
