package schema

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/eval"
)

// ParentEdge is one BROADER link as the relation layer holds it, reduced to the
// three fields a conflict turns on.
//
// The type is local rather than the relation layer's own so this package can be
// tested on a graph somebody wrote by hand. The command builds these from the
// canonical BROADER edges and nothing else: a proposed edge is not yet a claim
// about the taxonomy.
type ParentEdge struct {
	ChildID     string `json:"child_id"`
	ChildLabel  string `json:"child_label"`
	ParentID    string `json:"parent_id"`
	ParentLabel string `json:"parent_label"`
	Support     int    `json:"support"`
}

// ParentConflict is one concept the graph puts under two parents at once.
//
// Multiple inheritance is legitimate in a general ontology and is not
// legitimate here: the BROADER edges in this graph are induced one provision at
// a time, so a second parent is nearly always two readings of the same word
// rather than a concept that genuinely belongs in two places. Which of the two
// is a question about meaning, which is why the resolver asks the same question
// the induction pass asks rather than taking the higher support count.
type ParentConflict struct {
	ChildID    string       `json:"child_id"`
	ChildLabel string       `json:"child_label"`
	Parents    []ParentEdge `json:"parents"`
}

// FindParentConflicts groups edges by child and keeps the ones with more than
// one distinct parent.
func FindParentConflicts(edges []ParentEdge) []ParentConflict {
	by := map[string][]ParentEdge{}
	var order []string
	for _, e := range edges {
		if e.ChildID == "" || e.ParentID == "" {
			continue
		}
		if _, seen := by[e.ChildID]; !seen {
			order = append(order, e.ChildID)
		}
		by[e.ChildID] = append(by[e.ChildID], e)
	}
	var out []ParentConflict
	for _, id := range order {
		es := by[id]
		distinct := map[string]bool{}
		for _, e := range es {
			distinct[e.ParentID] = true
		}
		if len(distinct) < 2 {
			continue
		}
		sort.Slice(es, func(i, j int) bool {
			if es[i].Support != es[j].Support {
				return es[i].Support > es[j].Support
			}
			return es[i].ParentID < es[j].ParentID
		})
		out = append(out, ParentConflict{ChildID: id, ChildLabel: es[0].ChildLabel, Parents: es})
	}
	return out
}

// Resolution is one conflict decided.
//
// Support is carried beside the choice so a reader can see when the model went
// against the count. That disagreement is the interesting case: it is either
// the resolver doing the job the count cannot do, or the resolver being wrong
// where the count was right, and both are worth looking at by hand.
type Resolution struct {
	ChildID  string   `json:"child_id"`
	Kept     string   `json:"kept,omitempty"`
	Dropped  []string `json:"dropped,omitempty"`
	Support  int      `json:"support"`
	TopByFar bool     `json:"agreed_with_support"`
	Reason   string   `json:"reason,omitempty"`
}

// ResolveParents decides each conflict by asking the induction pass which
// parent the child belongs under.
//
// Nothing here edits the graph. The resolutions are returned so the caller can
// write them where decisions belong, which is the review queue, and so a run
// that goes wrong costs a file rather than a subtree.
func (in *Inducer) ResolveParents(ctx context.Context, cs []ParentConflict, onEach func(Resolution)) ([]Resolution, api.Usage, error) {
	var usage api.Usage
	var out []Resolution
	for _, c := range cs {
		child := Term{ID: c.ChildID, Label: labelOr(c.ChildLabel, c.ChildID)}
		var parents []Term
		seen := map[string]bool{}
		best, bestSupport := "", -1
		for _, p := range c.Parents {
			if seen[p.ParentID] {
				continue
			}
			seen[p.ParentID] = true
			parents = append(parents, Term{ID: p.ParentID, Label: labelOr(p.ParentLabel, p.ParentID)})
			if p.Support > bestSupport {
				best, bestSupport = p.ParentID, p.Support
			}
		}
		placement, u, err := in.Place(ctx, child, parents)
		usage = addUsage(usage, u)
		if err != nil {
			return out, usage, err
		}
		r := Resolution{ChildID: c.ChildID, Kept: placement.ParentID, Reason: placement.Rationale}
		for _, p := range parents {
			if p.ID != placement.ParentID {
				r.Dropped = append(r.Dropped, p.ID)
			}
		}
		if placement.ParentID != "" {
			r.Support = supportOf(c.Parents, placement.ParentID)
			r.TopByFar = placement.ParentID == best
		}
		out = append(out, r)
		if onEach != nil {
			onEach(r)
		}
	}
	return out, usage, nil
}

func labelOr(label, id string) string {
	if strings.TrimSpace(label) != "" {
		return label
	}
	return id
}

func supportOf(es []ParentEdge, parentID string) int {
	n := 0
	for _, e := range es {
		if e.ParentID == parentID {
			n += e.Support
		}
	}
	return n
}

// ConflictReport is the resolver's run over one graph.
//
// Edges and Broader are both counted so the report can tell a graph with no
// conflicts from a graph with no hierarchy at all. On this corpus it is the
// second, and a report that printed zero conflicts without saying so would be
// claiming a clean bill of health for a check that never ran.
type ConflictReport struct {
	// Source names where the conflicts came from, because they can come from the
	// stored graph or from the induction pass, and a resolution over induced
	// claims says nothing about the edges on disk.
	Source      string        `json:"source"`
	Edges       int           `json:"edges"`
	Broader     int           `json:"broader_edges"`
	Conflicts   int           `json:"conflicts"`
	Resolved    eval.Accuracy `json:"resolved"`
	AgreedCount int           `json:"agreed_with_support"`
	Resolutions []Resolution  `json:"resolutions,omitempty"`
	Usage       api.Usage     `json:"usage"`
}

// Report folds resolutions into a report over a graph of the given size.
func Report(source string, edges, broader int, cs []ParentConflict, rs []Resolution, usage api.Usage) ConflictReport {
	rep := ConflictReport{Source: source, Edges: edges, Broader: broader, Conflicts: len(cs), Resolutions: rs, Usage: usage}
	for _, r := range rs {
		rep.Resolved.Observe(r.Kept != "")
		if r.TopByFar {
			rep.AgreedCount++
		}
	}
	return rep
}

func (r ConflictReport) String() string {
	t := eval.NewTable("parent conflicts", fmt.Sprintf("%d relation edges", r.Edges))
	if r.Broader == 0 {
		t.Note("the graph holds no BROADER edge, so the stored hierarchy offered nothing to resolve, which is not the same as no conflicts")
	} else {
		t.Note("%d of the edges are BROADER", r.Broader)
	}
	if r.Conflicts == 0 {
		t.Note("no source offered a concept under two parents, so the resolver did not run")
		return t.String()
	}
	t.Note("the %d conflicts decided here came from the %s", r.Conflicts, r.Source)
	t.Rate("conflicts the resolver decided", r.Resolved)
	t.Note("%d of the decisions kept the parent with the most support, so the rest are where the resolver did something a count could not",
		r.AgreedCount)
	t.Note("%d tokens", r.Usage.TotalTokens)
	return t.String()
}
