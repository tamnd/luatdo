package eval

import (
	"fmt"
	"sort"
	"strings"
)

// An ablation is the full system with one thing taken out, asked the same 23
// questions.
//
// The difference between an ablation and a baseline is who it argues against. A
// baseline argues against other people's designs. An ablation argues against
// the parts of this one, and it is the only evidence that a layer earned its
// cost rather than being built because it was interesting. A layer whose
// removal changes nothing is a layer to delete, and this table is where that
// would show up.
type Ablation struct {
	Name    string   `json:"name"`
	Removes string   `json:"removes"`
	Layers  []string `json:"layers"`
	// Why records what the removal is meant to simulate, so a reader can argue
	// with the setup rather than only with the number.
	Why string `json:"why"`
}

// Ablations is the set from the milestone: the concept layer, the conditions on
// a norm, and the temporal layer.
var Ablations = []Ablation{
	{
		Name:    "no-concepts",
		Removes: LayerConcept,
		Layers:  without(Full.Layers, LayerConcept, LayerRelation),
		Why:     "norms extracted but every reference left as a string, which is the shadow ontology this project set out to avoid",
	},
	{
		Name:    "no-conditions",
		Removes: "conditions",
		Layers:  Full.Layers,
		Why:     "the full system with conditions and exceptions dropped from each statement, which is what flattening a norm to a triple costs",
	},
	{
		Name:    "no-temporal",
		Removes: LayerTemporal,
		Layers:  without(Full.Layers, LayerTemporal),
		Why:     "a graph with no notion of later, which is how most legal knowledge graph papers are built",
	},
}

// Affected returns the questions an ablation breaks.
//
// The conditions ablation is the one the layer table cannot answer on its own,
// because dropping conditions does not remove the norm layer, it removes part
// of a statement. Those questions are named here rather than derived, and the
// naming is the claim: question 14 asks for the conditions directly, and
// questions 9 and 19 are wrong rather than incomplete without them, because a
// duty whose condition was dropped is a different duty and two provisions that
// look incompatible may simply hold under different conditions.
func (a Ablation) Affected() []int {
	if a.Removes == "conditions" {
		return []int{9, 14, 19}
	}
	var out []int
	for _, q := range Questions {
		if ok, _ := q.Expressible(a.Layers...); !ok {
			out = append(out, q.N)
		}
	}
	return out
}

// Report renders every ablation with what it costs.
func AblationReport() string {
	var b strings.Builder
	for _, a := range Ablations {
		lost := a.Affected()
		sort.Ints(lost)
		fmt.Fprintf(&b, "%-14s loses %d of %d questions: %v\n", a.Name, len(lost), len(Questions), lost)
		fmt.Fprintf(&b, "               %s\n", a.Why)
	}
	return b.String()
}

func without(layers []string, drop ...string) []string {
	out := make([]string, 0, len(layers))
	for _, l := range layers {
		keep := true
		for _, d := range drop {
			if l == d {
				keep = false
			}
		}
		if keep {
			out = append(out, l)
		}
	}
	return out
}
