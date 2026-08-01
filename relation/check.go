package relation

import (
	"fmt"
	"sort"
	"strings"
)

// Layer is the relation layer in one value, so the invariants can be checked
// over the whole of it rather than a piece at a time. A cycle is not visible
// from one edge and neither is a document whose edge count says the extractor
// went into a loop.
type Layer struct {
	Registry *Registry         `json:"-"`
	Edges    []Edge            `json:"edges"`
	Kinds    Kinds             `json:"-"`
	Labels   map[string]string `json:"-"`
}

// Texts looks up the text of a provision. Check needs it because invariant 3 is
// not about the quote being well formed, it is about the quote being in the
// provision it names, and only the corpus can settle that.
type Texts func(provisionID string) (string, bool)

// Check returns every invariant violation, sorted so two runs read the same. A
// non empty result fails the build rather than printing a report: the survey's
// finding is that missing mandatory attributes are the commonest schema
// violation in model built ontologies, and a warning nobody reads is not a
// defence.
func (l *Layer) Check(texts Texts) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	r := l.Registry
	if r == nil {
		r = SeedRegistry(1)
	}
	seen := map[string]bool{}
	for _, e := range l.Edges {
		if seen[e.Key()] {
			add("%s %s %s appears twice, so its support count is a sum of two counts", e.FromID, e.Type, e.ToID)
			continue
		}
		seen[e.Key()] = true

		if err := e.Validate(r, l.Kinds); err != nil {
			add("%s %s %s: %v", e.FromID, e.Type, e.ToID, err)
		}
		l.checkEvidence(e, texts, add)
	}

	for _, path := range cycles(l.Edges) {
		// A BROADER cycle almost always means two concepts should have been
		// merged and the merge queue missed them, so the path is printed whole:
		// naming one edge would send a reviewer to fix the wrong link.
		add("BROADER cycle: %s", strings.Join(path, " -> "))
	}

	sort.Strings(out)
	return out
}

// checkEvidence enforces invariant 3. The last clause is the one that earns its
// keep: a model asserting a plausible relation between two concepts that never
// appear in the same provision has nothing to cite, and the edge does not build.
// It kills the commonest relation hallucination outright and it costs a
// substring search.
func (l *Layer) checkEvidence(e Edge, texts Texts, add func(string, ...any)) {
	if texts == nil {
		return
	}
	fromLabel, toLabel := l.Labels[e.FromID], l.Labels[e.ToID]
	for _, ev := range e.Evidence {
		text, ok := texts(ev.ProvisionID)
		if !ok {
			add("%s %s %s cites %s, which is not in the corpus", e.FromID, e.Type, e.ToID, ev.ProvisionID)
			continue
		}
		if err := checkQuote(text, ev.Quote, ev.CharStart, ev.CharEnd); err != nil {
			add("%s %s %s quotes %s: %v", e.FromID, e.Type, e.ToID, ev.ProvisionID, err)
			continue
		}
		if fromLabel != "" && !mentions(text, fromLabel) {
			add("%s %s %s cites %s, which does not mention %q", e.FromID, e.Type, e.ToID, ev.ProvisionID, fromLabel)
		}
		if toLabel != "" && !mentions(text, toLabel) {
			add("%s %s %s cites %s, which does not mention %q", e.FromID, e.Type, e.ToID, ev.ProvisionID, toLabel)
		}
	}
}

// mentions is a case folded substring test. It is deliberately loose: the point
// is to catch an edge whose endpoints are nowhere near the text it cites, not to
// adjudicate morphology, and a tighter test would reject correct edges over
// capitalisation.
func mentions(text, label string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(label))
}

// cycles returns every BROADER cycle as a full path.
//
// Transitivity is a query and never an edge, so the closure is not materialised
// and cycles are not harmless: a materialised closure would hide which link is
// weak and let one bad edge poison a subtree invisibly, and an unmaterialised
// one turns a cycle into a traversal that does not terminate.
func cycles(edges []Edge) [][]string {
	next := map[string][]string{}
	for _, e := range edges {
		if e.Type != Broader || e.Status != StatusCanonical {
			continue
		}
		next[e.FromID] = append(next[e.FromID], e.ToID)
	}
	nodes := make([]string, 0, len(next))
	for n := range next {
		sort.Strings(next[n])
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	const (
		white = 0
		grey  = 1
		black = 2
	)
	colour := map[string]int{}
	var stack []string
	var found [][]string
	reported := map[string]bool{}

	var walk func(string)
	walk = func(n string) {
		colour[n] = grey
		stack = append(stack, n)
		for _, m := range next[n] {
			switch colour[m] {
			case white:
				walk(m)
			case grey:
				at := 0
				for i, s := range stack {
					if s == m {
						at = i
						break
					}
				}
				path := append(append([]string{}, stack[at:]...), m)
				key := strings.Join(canonicalRotation(path[:len(path)-1]), "\x00")
				if !reported[key] {
					reported[key] = true
					found = append(found, path)
				}
			}
		}
		stack = stack[:len(stack)-1]
		colour[n] = black
	}
	for _, n := range nodes {
		if colour[n] == white {
			walk(n)
		}
	}
	sort.Slice(found, func(i, j int) bool {
		return strings.Join(found[i], "\x00") < strings.Join(found[j], "\x00")
	})
	return found
}

// canonicalRotation rotates a cycle to start at its smallest member, so the same
// cycle reached from two entry points is reported once.
func canonicalRotation(cycle []string) []string {
	if len(cycle) == 0 {
		return cycle
	}
	at := 0
	for i, s := range cycle {
		if s < cycle[at] {
			at = i
		}
	}
	return append(append([]string{}, cycle[at:]...), cycle[:at]...)
}

// Density is the edge to node ratio for one document, which is how relation
// inflation gets caught.
//
// The failure it watches for is a graph where every co-occurring concept pair
// acquires an edge, so traversal returns everything and answers nothing. It
// looks fine per edge and only shows up in the ratio, which is why the ratio is
// reported next to the numbers people already look at rather than kept in a
// diagnostic nobody runs.
type Density struct {
	DocID string  `json:"doc_id"`
	Nodes int     `json:"nodes"`
	Edges int     `json:"edges"`
	Ratio float64 `json:"ratio"`
}

// Densities reports the ratio per document, worst first.
func Densities(edges []Edge) []Density {
	nodes := map[string]map[string]bool{}
	count := map[string]int{}
	var order []string
	for _, e := range edges {
		for _, ev := range e.Evidence {
			doc := ev.DocID
			if doc == "" {
				continue
			}
			if nodes[doc] == nil {
				nodes[doc] = map[string]bool{}
				order = append(order, doc)
			}
			nodes[doc][e.FromID] = true
			nodes[doc][e.ToID] = true
			count[doc]++
		}
	}
	out := make([]Density, 0, len(order))
	for _, doc := range order {
		d := Density{DocID: doc, Nodes: len(nodes[doc]), Edges: count[doc]}
		if d.Nodes > 0 {
			d.Ratio = float64(d.Edges) / float64(d.Nodes)
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ratio != out[j].Ratio {
			return out[i].Ratio > out[j].Ratio
		}
		return out[i].DocID < out[j].DocID
	})
	return out
}

// Counts is what a relation run produced, for coverage.
type Counts struct {
	Edges            int            `json:"edges"`
	Canonical        int            `json:"canonical"`
	Provisional      int            `json:"provisional"`
	ByType           map[string]int `json:"by_type"`
	BySource         map[string]int `json:"by_source"`
	ByWhy            map[string]int `json:"by_why"`
	ProvisionalTypes int            `json:"provisional_types"`
}

// Count folds a layer into the numbers coverage reports. The provisional counts
// are next to the canonical ones on purpose: a review queue nobody works is a
// slow way of dropping the tail, and the only defence is putting its size where
// people already look.
func Count(edges []Edge) Counts {
	c := Counts{ByType: map[string]int{}, BySource: map[string]int{}, ByWhy: map[string]int{}}
	types := map[string]bool{}
	for _, e := range edges {
		c.Edges++
		c.ByType[e.Type]++
		c.BySource[e.Source]++
		if e.Status == StatusCanonical {
			c.Canonical++
			continue
		}
		c.Provisional++
		c.ByWhy[e.Why]++
		if e.Why == WhyUnknownType {
			types[e.Type] = true
		}
	}
	c.ProvisionalTypes = len(types)
	return c
}

// String prints the counts.
func (c Counts) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "relations      %d edges: %d canonical, %d provisional\n", c.Edges, c.Canonical, c.Provisional)
	if c.Provisional > 0 {
		reasons := make([]string, 0, len(c.ByWhy))
		for why := range c.ByWhy {
			reasons = append(reasons, why)
		}
		sort.Strings(reasons)
		for _, why := range reasons {
			fmt.Fprintf(&b, "               %-16s %d\n", why, c.ByWhy[why])
		}
		fmt.Fprintf(&b, "               %d distinct types waiting on a person\n", c.ProvisionalTypes)
	}
	return b.String()
}
