package relation

import (
	"fmt"
	"sort"
	"strings"
)

// The competency questions this layer is answerable for. They are the test of
// whether the layer works, and they run over the edges alone with no text
// reading, because a question that has to fall back on searching the corpus is a
// question the graph did not answer.
//
// Every answer carries its support counts. An edge supported by forty provisions
// in nine instruments and an edge supported by one provincial circular are
// different kinds of claim, and an answer that prints them alike has thrown away
// the only thing that lets a reader judge it.

// Graph is an index over the edges, built once and asked many times.
type Graph struct {
	edges  []Edge
	out    map[string][]int
	in     map[string][]int
	labels map[string]string
	scope  map[string]string // endpoint id to the instrument that defines it
}

// NewGraph indexes edges for traversal. Labels and scopes are optional: without
// them the answers carry identifiers instead of phrases, which is less readable
// and no less correct.
func NewGraph(edges []Edge, labels, scope map[string]string) *Graph {
	g := &Graph{
		edges:  edges,
		out:    map[string][]int{},
		in:     map[string][]int{},
		labels: labels,
		scope:  scope,
	}
	for i, e := range edges {
		g.out[e.FromID] = append(g.out[e.FromID], i)
		g.in[e.ToID] = append(g.in[e.ToID], i)
	}
	return g
}

// Label returns the readable form of an endpoint, falling back to its
// identifier.
func (g *Graph) Label(id string) string {
	if l := g.labels[id]; l != "" {
		return l
	}
	return id
}

// Answer is one row of an answer with the evidence that makes it arguable.
type Answer struct {
	FromID       string `json:"from_id"`
	FromLabel    string `json:"from_label"`
	Type         string `json:"type"`
	ToID         string `json:"to_id"`
	ToLabel      string `json:"to_label"`
	Status       string `json:"status"`
	Source       string `json:"source"`
	SupportCount int    `json:"support_count"`
	SupportDocs  int    `json:"support_docs"`
	Depth        int    `json:"depth,omitempty"`
	Note         string `json:"note,omitempty"`
}

func (g *Graph) answer(e Edge) Answer {
	return Answer{
		FromID: e.FromID, FromLabel: g.Label(e.FromID),
		Type: e.Type,
		ToID: e.ToID, ToLabel: g.Label(e.ToID),
		Status: e.Status, Source: e.Source,
		SupportCount: e.SupportCount, SupportDocs: e.SupportDocs,
	}
}

// Question7 is the hierarchy under a concept as the corpus actually uses it.
//
// The inconsistencies are part of the answer rather than noise to be cleaned
// out of it. Two instruments putting the same thing under different parents is a
// real fact about Vietnamese drafting, and it is visible here only because every
// BROADER edge carries its support count and its source.
type Question7 struct {
	Root     string   `json:"root"`
	Children []Answer `json:"children"`
	// Conflicts are concepts the corpus files under more than one parent.
	Conflicts []Conflict `json:"conflicts,omitempty"`
	MaxDepth  int        `json:"max_depth"`
}

// Conflict is one concept with more than one parent in the corpus.
type Conflict struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Parents []Answer `json:"parents"`
}

// AskQuestion7 walks BROADER downwards from a root.
//
// The traversal is at query time and the closure is never materialised, because
// a stored transitive closure hides which link is weak and lets one bad edge
// poison a whole subtree without leaving a trace of where it entered.
func (g *Graph) AskQuestion7(root string, maxDepth int) Question7 {
	q := Question7{Root: root, MaxDepth: maxDepth}
	if maxDepth <= 0 {
		maxDepth = 5
		q.MaxDepth = maxDepth
	}
	seen := map[string]bool{root: true}
	frontier := []string{root}
	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		var nextFrontier []string
		for _, node := range frontier {
			for _, i := range g.in[node] {
				e := g.edges[i]
				if e.Type != Broader {
					continue
				}
				a := g.answer(e)
				a.Depth = depth
				q.Children = append(q.Children, a)
				if !seen[e.FromID] {
					seen[e.FromID] = true
					nextFrontier = append(nextFrontier, e.FromID)
				}
			}
		}
		sort.Strings(nextFrontier)
		frontier = nextFrontier
	}
	sort.SliceStable(q.Children, func(i, j int) bool {
		if q.Children[i].Depth != q.Children[j].Depth {
			return q.Children[i].Depth < q.Children[j].Depth
		}
		return q.Children[i].FromID < q.Children[j].FromID
	})

	for _, child := range q.Children {
		var parents []Answer
		for _, i := range g.out[child.FromID] {
			if g.edges[i].Type == Broader {
				parents = append(parents, g.answer(g.edges[i]))
			}
		}
		if len(parents) > 1 {
			q.Conflicts = append(q.Conflicts, Conflict{ID: child.FromID, Label: child.FromLabel, Parents: parents})
		}
	}
	sort.Slice(q.Conflicts, func(i, j int) bool { return q.Conflicts[i].ID < q.Conflicts[j].ID })
	q.Conflicts = dedupeConflicts(q.Conflicts)
	return q
}

func dedupeConflicts(cs []Conflict) []Conflict {
	out := cs[:0]
	var last string
	for i, c := range cs {
		if i == 0 || c.ID != last {
			out = append(out, c)
		}
		last = c.ID
	}
	return out
}

// Question21 is the prerequisites for something, at concept level, with no text
// read at answer time. It is the direct test of whether this layer works: if the
// REQUIRES chain is not there, the answer is empty and the graph is a glossary.
type Question21 struct {
	Target        string   `json:"target"`
	Prerequisites []Answer `json:"prerequisites"`
	Produced      []Answer `json:"produced_by,omitempty"`
}

// AskQuestion21 walks REQUIRES outward and PRODUCES inward.
func (g *Graph) AskQuestion21(target string, maxDepth int) Question21 {
	q := Question21{Target: target}
	if maxDepth <= 0 {
		maxDepth = 3
	}
	seen := map[string]bool{target: true}
	frontier := []string{target}
	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		var nextFrontier []string
		for _, node := range frontier {
			for _, i := range g.out[node] {
				e := g.edges[i]
				if e.Type != Requires {
					continue
				}
				a := g.answer(e)
				a.Depth = depth
				q.Prerequisites = append(q.Prerequisites, a)
				if !seen[e.ToID] {
					seen[e.ToID] = true
					nextFrontier = append(nextFrontier, e.ToID)
				}
			}
		}
		sort.Strings(nextFrontier)
		frontier = nextFrontier
	}
	for _, i := range g.in[target] {
		if g.edges[i].Type == Produces {
			q.Produced = append(q.Produced, g.answer(g.edges[i]))
		}
	}
	sort.SliceStable(q.Prerequisites, func(i, j int) bool {
		if q.Prerequisites[i].Depth != q.Prerequisites[j].Depth {
			return q.Prerequisites[i].Depth < q.Prerequisites[j].Depth
		}
		return q.Prerequisites[i].ToID < q.Prerequisites[j].ToID
	})
	return q
}

// Question22 finds concepts one instrument requires and another defines, and
// says whether the two instruments cite each other at all.
//
// When they do not, that is a drafting observation worth surfacing rather than a
// bug in the graph: it is the concept level analogue of legislative void
// detection, and it is the sort of thing only a corpus wide view can see.
type Question22 struct {
	Rows []CrossReference `json:"rows"`
}

// CrossReference is one concept used in one instrument and defined in another.
type CrossReference struct {
	ConceptID    string `json:"concept_id"`
	Label        string `json:"label"`
	UsedIn       string `json:"used_in"`
	DefinedIn    string `json:"defined_in"`
	Type         string `json:"type"`
	SupportCount int    `json:"support_count"`
	// Cited says whether the using instrument cites the defining one anywhere.
	// A false here is the finding.
	Cited bool `json:"cited"`
}

// Cites reports whether one document cites another, from the citation graph M1
// already built. It is passed in rather than recomputed, because the citation
// graph is the largest thing on disk and this question needs a lookup rather
// than a copy.
type Cites func(from, to string) bool

// AskQuestion22 walks every edge whose endpoints are scoped to different
// instruments.
func (g *Graph) AskQuestion22(cites Cites) Question22 {
	var q Question22
	seen := map[string]bool{}
	for _, e := range g.edges {
		if e.Type != Requires && e.Type != RegulatedBy && e.Type != EvidencedBy {
			continue
		}
		usedIn := g.scope[e.FromID]
		definedIn := g.scope[e.ToID]
		if usedIn == "" || definedIn == "" || usedIn == definedIn {
			continue
		}
		key := e.Key()
		if seen[key] {
			continue
		}
		seen[key] = true
		row := CrossReference{
			ConceptID: e.ToID, Label: g.Label(e.ToID),
			UsedIn: usedIn, DefinedIn: definedIn,
			Type: e.Type, SupportCount: e.SupportCount,
		}
		if cites != nil {
			row.Cited = cites(usedIn, definedIn)
		}
		q.Rows = append(q.Rows, row)
	}
	sort.Slice(q.Rows, func(i, j int) bool {
		// Uncited first, because those are the finding and the rest is context.
		if q.Rows[i].Cited != q.Rows[j].Cited {
			return !q.Rows[i].Cited
		}
		if q.Rows[i].UsedIn != q.Rows[j].UsedIn {
			return q.Rows[i].UsedIn < q.Rows[j].UsedIn
		}
		return q.Rows[i].ConceptID < q.Rows[j].ConceptID
	})
	return q
}

// Question23 is the alternatives to a concept, ranked by how much of the corpus
// treats them as interchangeable.
type Question23 struct {
	Subject      string   `json:"subject"`
	Alternatives []Answer `json:"alternatives"`
}

// AskQuestion23 reads ALTERNATIVE_TO in both directions, since it is symmetric
// and the direction an extractor happened to write it in is not a fact about
// anything.
func (g *Graph) AskQuestion23(subject string) Question23 {
	q := Question23{Subject: subject}
	seen := map[string]bool{}
	collect := func(i int, other string) {
		e := g.edges[i]
		if e.Type != AlternativeTo || seen[other] {
			return
		}
		seen[other] = true
		a := g.answer(e)
		if a.ToID != other {
			a.FromID, a.ToID = a.ToID, a.FromID
			a.FromLabel, a.ToLabel = a.ToLabel, a.FromLabel
		}
		q.Alternatives = append(q.Alternatives, a)
	}
	for _, i := range g.out[subject] {
		collect(i, g.edges[i].ToID)
	}
	for _, i := range g.in[subject] {
		collect(i, g.edges[i].FromID)
	}
	sort.SliceStable(q.Alternatives, func(i, j int) bool {
		if q.Alternatives[i].SupportDocs != q.Alternatives[j].SupportDocs {
			return q.Alternatives[i].SupportDocs > q.Alternatives[j].SupportDocs
		}
		if q.Alternatives[i].SupportCount != q.Alternatives[j].SupportCount {
			return q.Alternatives[i].SupportCount > q.Alternatives[j].SupportCount
		}
		return q.Alternatives[i].ToID < q.Alternatives[j].ToID
	})
	return q
}

// support renders the counts every answer row carries.
func support(a Answer) string {
	return fmt.Sprintf("%d provisions in %d documents, %s, %s", a.SupportCount, a.SupportDocs, a.Source, a.Status)
}

// String prints question 7.
func (q Question7) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "question 7     what the corpus files under %s, to depth %d\n", q.Root, q.MaxDepth)
	if len(q.Children) == 0 {
		b.WriteString("               nothing, which means no BROADER edge reaches it\n")
		return b.String()
	}
	for _, c := range q.Children {
		fmt.Fprintf(&b, "               %s%s (%s)\n", strings.Repeat("  ", c.Depth-1), c.FromLabel, support(c))
	}
	if len(q.Conflicts) > 0 {
		fmt.Fprintf(&b, "               %d concepts sit under more than one parent, which is the corpus disagreeing rather than an error:\n", len(q.Conflicts))
		for _, c := range q.Conflicts {
			var parents []string
			for _, p := range c.Parents {
				parents = append(parents, fmt.Sprintf("%s (%d)", p.ToLabel, p.SupportCount))
			}
			fmt.Fprintf(&b, "                 %s under %s\n", c.Label, strings.Join(parents, " and "))
		}
	}
	return b.String()
}

// String prints question 21.
func (q Question21) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "question 21    what %s requires, from the graph alone with no text read\n", q.Target)
	if len(q.Prerequisites) == 0 {
		b.WriteString("               nothing, and an empty answer here means the layer did not work\n")
		return b.String()
	}
	for _, p := range q.Prerequisites {
		fmt.Fprintf(&b, "               %s%s (%s)\n", strings.Repeat("  ", p.Depth-1), p.ToLabel, support(p))
	}
	for _, p := range q.Produced {
		fmt.Fprintf(&b, "               produced by %s (%s)\n", p.FromLabel, support(p))
	}
	return b.String()
}

// String prints question 22.
func (q Question22) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "question 22    concepts required in one instrument and defined in another: %d\n", len(q.Rows))
	uncited := 0
	for _, r := range q.Rows {
		if !r.Cited {
			uncited++
		}
	}
	fmt.Fprintf(&b, "               %d of them between instruments that do not cite each other at all\n", uncited)
	for i, r := range q.Rows {
		if i >= 10 {
			fmt.Fprintf(&b, "               and %d more\n", len(q.Rows)-10)
			break
		}
		note := "cites it"
		if !r.Cited {
			note = "no citation either way"
		}
		fmt.Fprintf(&b, "               %s: %s needs it, %s defines it, %s\n", r.Label, r.UsedIn, r.DefinedIn, note)
	}
	return b.String()
}

// String prints question 23.
func (q Question23) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "question 23    what the corpus offers instead of %s\n", q.Subject)
	if len(q.Alternatives) == 0 {
		b.WriteString("               nothing\n")
		return b.String()
	}
	for _, a := range q.Alternatives {
		fmt.Fprintf(&b, "               %s (%s)\n", a.ToLabel, support(a))
	}
	return b.String()
}
