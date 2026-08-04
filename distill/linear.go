package distill

import "iter"

// The averaged perceptron itself, separated from the tagger that first used it.
//
// It is here rather than inside Train because a second student now learns from
// the same teacher on a different task: the entailment gate in entail/ decides
// whether a provision supports a statement, which is a decision about a pair and
// not about a span. The learning is the same and the features are not, so the
// learning moves out and each student keeps its own features.
//
// Nothing about this is clever and that is the point. It trains in seconds, it
// has no dependencies, it produces the same weights on Linux, macOS and Windows
// given the same sequence, and every weight belongs to a feature somebody can
// read and argue with.

// Fit runs the averaged perceptron over a sequence of labelled feature sets and
// returns the trained weights.
//
// The caller supplies the sequence and therefore the order, because the order
// decides the weights and a training set assembled from a map would produce a
// different model on every run. The sequence is walked once per epoch, so it has
// to be replayable rather than consumed.
//
// The average is what makes this stable. The last weight vector of a perceptron
// is whatever the last few examples happened to push it to, and averaging over
// every update is the standard fix.
func Fit(epochs int, examples iter.Seq2[[]string, bool]) map[string]float64 {
	weights := map[string]float64{}
	sum := map[string]float64{}
	step := 1.0
	for range epochs {
		for features, want := range examples {
			got := Dot(weights, features) > 0
			if got != want {
				delta := 1.0
				if got {
					delta = -1.0
				}
				for _, key := range features {
					weights[key] += delta
					sum[key] += delta * step
				}
			}
			step++
		}
	}
	for key := range weights {
		weights[key] -= sum[key] / step
		// A feature whose averaged weight lands exactly on zero says nothing and
		// is dropped, so a model file lists what it learned rather than every
		// feature it ever saw.
		if weights[key] == 0 {
			delete(weights, key)
		}
	}
	return weights
}

// Dot is the dot product of the weights with a feature set. Absent features
// contribute nothing, which is what lets a student run over documents whose
// words the training set never contained.
//
// The name is not Score because this package already has a Score, which is what
// a measurement of a student is called here.
func Dot(weights map[string]float64, features []string) float64 {
	var s float64
	for _, f := range features {
		s += weights[f]
	}
	return s
}
