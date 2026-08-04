package event

import (
	"fmt"
	"sort"
	"strings"
)

// The competency questions this layer is answerable for.
//
// They run over the folded layer alone with no text read, because a question
// that has to fall back on searching the corpus is a question the graph did not
// answer. Every row carries its support counts and its status, since an act
// chain corroborated across nine instruments and one read off a single
// provincial circular are different kinds of claim and an answer that prints
// them alike has thrown away what lets a reader judge it.

// Graph is an index over the folded layer, built once and asked many times.
type Graph struct {
	events map[string]Event
	chains []Chain
	out    map[string][]int
	in     map[string][]int
	links  []Link
}

// NewGraph indexes the layer for traversal.
func NewGraph(events []Event, chains []Chain, links []Link) *Graph {
	g := &Graph{
		events: map[string]Event{},
		chains: chains,
		out:    map[string][]int{},
		in:     map[string][]int{},
		links:  links,
	}
	for _, e := range events {
		g.events[e.ID] = e
	}
	for i, c := range chains {
		g.out[c.FromID] = append(g.out[c.FromID], i)
		g.in[c.ToID] = append(g.in[c.ToID], i)
	}
	return g
}

// Label returns the readable form of an act, falling back to its identifier.
func (g *Graph) Label(id string) string {
	if e, ok := g.events[id]; ok && e.LabelVI != "" {
		return e.LabelVI
	}
	return id
}

// Docs returns the instruments that name an act, sorted.
func (g *Graph) Docs(id string) []string {
	seen := map[string]bool{}
	for _, ev := range g.events[id].Evidence {
		if ev.DocID != "" {
			seen[ev.DocID] = true
		}
	}
	return sortedKeys(seen)
}

// Step is one act reached from another, with the evidence that makes it
// arguable.
type Step struct {
	FromID       string `json:"from_id"`
	FromLabel    string `json:"from_label"`
	Type         string `json:"type"`
	ToID         string `json:"to_id"`
	ToLabel      string `json:"to_label"`
	Status       string `json:"status"`
	Direction    string `json:"direction,omitempty"`
	SupportCount int    `json:"support_count"`
	SupportDocs  int    `json:"support_docs"`
	Depth        int    `json:"depth,omitempty"`
	ProvisionID  string `json:"provision_id,omitempty"`
}

func (g *Graph) step(c Chain) Step {
	s := Step{
		FromID: c.FromID, FromLabel: g.Label(c.FromID),
		Type: c.Type,
		ToID: c.ToID, ToLabel: g.Label(c.ToID),
		Status: c.Status, Direction: c.Direction,
		SupportCount: c.SupportCount, SupportDocs: c.SupportDocs,
	}
	if len(c.Evidence) > 0 {
		s.ProvisionID = c.Evidence[0].ProvisionID
	}
	return s
}

// Question24 is what follows from an act and what has to happen before it.
//
// This is the question the layer exists for. Before M16 a consequence had to be
// found by reading, because the graph held norms about acts and nothing joining
// one act to the next, and the answer to "what happens if this is not done" was
// a full text search.
type Question24 struct {
	Act      string `json:"act"`
	Label    string `json:"label"`
	Follows  []Step `json:"follows,omitempty"`
	Precedes []Step `json:"precedes,omitempty"`
	MaxDepth int    `json:"max_depth"`
	// Backwards counts the steps on the answer whose direction the blind pass
	// read the other way round. They are shown rather than dropped, and counted
	// so nobody reads a chain of eleven steps as eleven checked steps.
	Backwards int `json:"backwards"`
}

// AskQuestion24 walks the chains outward from an act and inward to it.
//
// The traversal happens at query time and no closure is stored, because a
// materialised closure hides which link is weak and lets one backwards chain
// poison a whole procedure without leaving a trace of where it entered.
func (g *Graph) AskQuestion24(act string, maxDepth int) Question24 {
	if maxDepth <= 0 {
		maxDepth = 3
	}
	q := Question24{Act: act, Label: g.Label(act), MaxDepth: maxDepth}
	q.Follows = g.walk(act, maxDepth, true)
	q.Precedes = g.walk(act, maxDepth, false)
	for _, s := range append(append([]Step{}, q.Follows...), q.Precedes...) {
		if s.Direction == DirectionFlipped || s.Direction == DirectionDisputed {
			q.Backwards++
		}
	}
	return q
}

func (g *Graph) walk(from string, maxDepth int, forward bool) []Step {
	var out []Step
	seen := map[string]bool{from: true}
	frontier := []string{from}
	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, node := range frontier {
			index := g.out[node]
			if !forward {
				index = g.in[node]
			}
			for _, i := range index {
				c := g.chains[i]
				s := g.step(c)
				s.Depth = depth
				out = append(out, s)
				other := c.ToID
				if !forward {
					other = c.FromID
				}
				if !seen[other] {
					seen[other] = true
					next = append(next, other)
				}
			}
		}
		sort.Strings(next)
		frontier = next
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		if out[i].FromID != out[j].FromID {
			return out[i].FromID < out[j].FromID
		}
		return out[i].ToID < out[j].ToID
	})
	return out
}

// Sanctioned is one act with the penalty a norm attaches to it.
type Sanctioned struct {
	ActID       string `json:"act_id"`
	ActLabel    string `json:"act_label"`
	StatementID string `json:"statement_id"`
	ProvisionID string `json:"provision_id"`
	DocID       string `json:"doc_id,omitempty"`
	SanctionID  string `json:"sanction_id"`
	Sanction    string `json:"sanction_label"`
}

// Question25 is which acts carry a penalty and what the penalty is.
type Question25 struct {
	Rows []Sanctioned `json:"rows"`
	// Unlinked counts the norms whose action reached an act and whose sanction
	// reached none. It is printed, because a table of forty sanctioned acts
	// above a silence about the two hundred that did not link reads as coverage.
	Unlinked int `json:"unlinked"`
}

// AskQuestion25 joins the act to the penalty through the norm rather than by
// comparing labels.
//
// The join is the statement identifier. Both links were written while one
// provision was in view, so the act and the penalty on this row came out of one
// paragraph, and nothing here matched trả lương in a labour code against trả
// lương in a tax decree and called it the same duty.
func (g *Graph) AskQuestion25() Question25 {
	var q Question25
	actions := map[string]Link{}
	sanctions := map[string]Link{}
	for _, l := range g.links {
		switch l.Kind {
		case LinkAction:
			actions[l.StatementID] = l
		case LinkSanction:
			sanctions[l.StatementID] = l
		}
	}
	for id, action := range actions {
		penalty, ok := sanctions[id]
		if !ok {
			q.Unlinked++
			continue
		}
		q.Rows = append(q.Rows, Sanctioned{
			ActID: action.EventID, ActLabel: g.Label(action.EventID),
			StatementID: id, ProvisionID: action.ProvisionID, DocID: action.DocID,
			SanctionID: penalty.EventID, Sanction: g.Label(penalty.EventID),
		})
	}
	sort.Slice(q.Rows, func(i, j int) bool {
		if q.Rows[i].ActID != q.Rows[j].ActID {
			return q.Rows[i].ActID < q.Rows[j].ActID
		}
		return q.Rows[i].StatementID < q.Rows[j].StatementID
	})
	return q
}

// Shared is one act named by more than one instrument.
type Shared struct {
	ActID    string   `json:"act_id"`
	Label    string   `json:"label"`
	Class    string   `json:"class"`
	Status   string   `json:"status"`
	Docs     []string `json:"docs"`
	Chains   int      `json:"chains"`
	Evidence int      `json:"evidence"`
}

// Question26 is the acts the corpus names in more than one instrument, and the
// chains that run through them.
//
// This is the question that says whether corpus wide act identity paid for
// itself or misfired. A row joining a law to the decree that guides it is the
// layer working. A row joining two unrelated fields is two different acts merged
// under one phrase, which is the risk this identifier takes, and it is visible
// here rather than hidden inside a traversal.
type Question26 struct {
	Rows []Shared `json:"rows"`
}

// AskQuestion26 reports every act with evidence from more than one instrument.
func (g *Graph) AskQuestion26(minDocs int) Question26 {
	if minDocs < 2 {
		minDocs = 2
	}
	var q Question26
	for id, e := range g.events {
		docs := g.Docs(id)
		if len(docs) < minDocs {
			continue
		}
		q.Rows = append(q.Rows, Shared{
			ActID: id, Label: e.LabelVI, Class: e.Class, Status: e.Status,
			Docs: docs, Chains: len(g.out[id]) + len(g.in[id]), Evidence: len(e.Evidence),
		})
	}
	sort.Slice(q.Rows, func(i, j int) bool {
		if len(q.Rows[i].Docs) != len(q.Rows[j].Docs) {
			return len(q.Rows[i].Docs) > len(q.Rows[j].Docs)
		}
		if q.Rows[i].Chains != q.Rows[j].Chains {
			return q.Rows[i].Chains > q.Rows[j].Chains
		}
		return q.Rows[i].ActID < q.Rows[j].ActID
	})
	return q
}

func support(s Step) string {
	out := fmt.Sprintf("%d provisions in %d documents, %s", s.SupportCount, s.SupportDocs, s.Status)
	switch s.Direction {
	case DirectionFlipped, DirectionDisputed:
		out += ", the blind pass read it the other way round"
	case DirectionUnclear:
		out += ", direction unclear"
	case DirectionUnverified:
		out += ", direction unchecked"
	}
	return out
}

// String prints question 24.
func (q Question24) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "question 24    what follows from %s and what comes before it, to depth %d\n", q.Label, q.MaxDepth)
	if len(q.Follows) == 0 && len(q.Precedes) == 0 {
		b.WriteString("               nothing, and an empty answer here means no provision joined this act to another\n")
		return b.String()
	}
	for _, s := range q.Follows {
		fmt.Fprintf(&b, "               %s%s %s (%s)\n", strings.Repeat("  ", s.Depth-1), s.Type, s.ToLabel, support(s))
	}
	for _, s := range q.Precedes {
		fmt.Fprintf(&b, "               %sbefore it: %s %s (%s)\n", strings.Repeat("  ", s.Depth-1), s.FromLabel, s.Type, support(s))
	}
	if q.Backwards > 0 {
		fmt.Fprintf(&b, "               %d steps on this answer were read the other way round by the blind pass, so the order above is not settled\n", q.Backwards)
	}
	return b.String()
}

// String prints question 25.
func (q Question25) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "question 25    acts that carry a penalty, joined through the norm and not by matching labels: %d\n", len(q.Rows))
	fmt.Fprintf(&b, "               %d norms reached an act and no penalty\n", q.Unlinked)
	for i, r := range q.Rows {
		if i >= 20 {
			fmt.Fprintf(&b, "               and %d more\n", len(q.Rows)-20)
			break
		}
		fmt.Fprintf(&b, "               %s: %s (%s)\n", r.ActLabel, r.Sanction, r.ProvisionID)
	}
	return b.String()
}

// String prints question 26.
func (q Question26) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "question 26    acts named in more than one instrument: %d\n", len(q.Rows))
	if len(q.Rows) == 0 {
		b.WriteString("               none, so nothing was joined across documents and the corpus wide identifier bought nothing here\n")
		return b.String()
	}
	for i, r := range q.Rows {
		if i >= 20 {
			fmt.Fprintf(&b, "               and %d more\n", len(q.Rows)-20)
			break
		}
		fmt.Fprintf(&b, "               %-40s %d instruments, %d chains, %s\n", r.Label, len(r.Docs), r.Chains, r.Status)
	}
	b.WriteString("               read the two instrument rows before trusting them, since one phrase used by two fields merges two acts into one node\n")
	return b.String()
}
