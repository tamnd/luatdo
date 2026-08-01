package distill

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// Score is span level precision and recall against one reference. The
// reference is named in the struct rather than left to the caller's memory,
// because the same student measured against the teacher and against the gold
// set produces two numbers that mean completely different things and get
// confused the moment they are printed without their labels.
type Score struct {
	Against   string  `json:"against"` // teacher or gold
	Examples  int     `json:"examples"`
	Predicted int     `json:"predicted"`
	Reference int     `json:"reference"`
	Exact     int     `json:"exact"`
	Overlap   int     `json:"overlap"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
	// KindRight counts spans that matched exactly and also carried the right
	// kind. It is reported over the exact matches rather than over everything,
	// since a kind on a span that is not a concept is not wrong about the kind.
	KindRight int `json:"kind_right"`
	KindOf    int `json:"kind_of"`
}

// Evaluate runs the student over the examples and scores it against their
// labels. Matching is by folded surface form within one provision rather than
// by byte offsets: a teacher and a student that both found thoi gio lam viec
// binh thuong agree, and making them disagree because one of them included a
// trailing comma would measure tokenisation instead of tagging.
func Evaluate(t *Tagger, examples []Example, against string) Score {
	s := Score{Against: against, Examples: len(examples)}
	for _, e := range examples {
		predicted := keySet(t.Tag(e.Text))
		reference := keySet(e.Spans)
		s.Predicted += len(predicted)
		s.Reference += len(reference)
		for key, pk := range predicted {
			rk, ok := reference[key]
			if !ok {
				continue
			}
			s.Exact++
			if rk != "" {
				s.KindOf++
				if pk == rk {
					s.KindRight++
				}
			}
		}
		s.Overlap += countOverlap(t.Tag(e.Text), e.Spans)
	}
	if s.Predicted > 0 {
		s.Precision = float64(s.Exact) / float64(s.Predicted)
	}
	if s.Reference > 0 {
		s.Recall = float64(s.Exact) / float64(s.Reference)
	}
	if s.Precision+s.Recall > 0 {
		s.F1 = 2 * s.Precision * s.Recall / (s.Precision + s.Recall)
	}
	return s
}

// keySet folds a span list to phrase key against kind. A provision naming one
// concept twice counts once, the same way aggregation counts it once.
func keySet(spans []Span) map[string]string {
	out := map[string]string{}
	for _, s := range spans {
		key := law.Slug(s.Text)
		if key == "" {
			continue
		}
		if _, seen := out[key]; !seen || s.Kind != "" {
			out[key] = s.Kind
		}
	}
	return out
}

// countOverlap counts predicted spans that touch a reference span without
// matching it. It is reported separately because a student that finds thoi gio
// lam viec where the teacher said thoi gio lam viec binh thuong has found the
// concept and got the boundary wrong, which is a different failure from
// finding nothing, and the fix for it is a different change.
func countOverlap(predicted, reference []Span) int {
	n := 0
	for _, p := range predicted {
		for _, r := range reference {
			if p.Start < r.End && r.Start < p.End && law.Slug(p.Text) != law.Slug(r.Text) {
				n++
				break
			}
		}
	}
	return n
}

func (s Score) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "against %-8s %d provisions, %d predicted, %d reference\n",
		s.Against, s.Examples, s.Predicted, s.Reference)
	fmt.Fprintf(&b, "           precision %.1f%% recall %.1f%% f1 %.1f%%, %d exact, %d boundary only\n",
		100*s.Precision, 100*s.Recall, 100*s.F1, s.Exact, s.Overlap)
	if s.KindOf > 0 {
		fmt.Fprintf(&b, "           kind %.1f%% of %d exact matches that carried one\n",
			100*float64(s.KindRight)/float64(s.KindOf), s.KindOf)
	}
	return b.String()
}

// Split divides examples into a training part and a held out part by a stable
// hash of the provision identifier. There is no random source: the same corpus
// splits the same way on every machine, so a number reported here can be
// reproduced by somebody else.
func Split(examples []Example, holdout float64) (train, test []Example) {
	sorted := append([]Example(nil), examples...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ProvisionID < sorted[j].ProvisionID })
	for _, e := range sorted {
		if bucket(e.ProvisionID) < holdout {
			test = append(test, e)
		} else {
			train = append(train, e)
		}
	}
	return train, test
}

// bucket maps an identifier into [0,1) deterministically.
func bucket(id string) float64 {
	h := fnv64a(id)
	return float64(h%10000) / 10000
}

func fnv64a(s string) uint64 {
	const offset, prime = uint64(14695981039346656037), uint64(1099511628211)
	h := offset
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	return h
}
