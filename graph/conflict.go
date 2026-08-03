package graph

import (
	"sort"
	"strings"

	"github.com/tamnd/luatdo/conflict"
)

// The conflict layer, projected.
//
// Question 19 used to be a query: find two norms with the same bearer and the
// same action whose modalities cannot both hold. The query is in the history and
// its own comment said what was wrong with it, that two norms agreeing on bearer
// and action are usually told apart by a condition the comparison ignores, so
// the rows were candidates rather than findings.
//
// A pair of norms is not the sort of thing Cypher can decide. Deciding it needs
// the bearer and the act in canonical form, the validity intervals intersected,
// the conditions compared for containment and the deferrals honoured, and each
// of those is a step the detector takes in Go with a reason it can state. So the
// detector runs outside the database and its findings are projected as nodes,
// which turns question 19 from a heuristic into a lookup of work already done
// and checked.
const (
	// ConflictLabel is one pair the detector could not reconcile.
	ConflictLabel = "Conflict"
	// Involves points from a conflict to each of the two norms in it. The side
	// is a property rather than two relationship types, because which of the
	// two is called a is an artefact of sorting and no query should turn on it.
	Involves = "INVOLVES"
)

// conflictNode is one finding as the projection carries it.
//
// The slots are flattened into strings. A graph is queried by property and a
// nested list of name and value pairs is neither queryable nor readable in the
// browser, so the matched slots become one string a person can read and the
// clashing slot becomes the three fields a query filters on.
type conflictNode struct {
	ID            string
	Rule          string
	Circumstances string
	Party         string
	Act           string
	Object        string
	Matched       string
	ClashingSlot  string
	ClashingA     string
	ClashingB     string
	Rank          string
	Explanation   string
	NormA         string
	NormB         string
}

// conflictColumns is the CSV header order, which is also the Bolt property set.
var conflictColumns = []string{
	"rule", "circumstances", "party", "act", "object",
	"matched", "clashing_slot", "clashing_a", "clashing_b",
	"rank", "explanation", "norm_a", "norm_b",
}

func (c conflictNode) values() []string {
	return []string{
		c.Rule, c.Circumstances, c.Party, c.Act, c.Object,
		c.Matched, c.ClashingSlot, c.ClashingA, c.ClashingB,
		c.Rank, c.Explanation, c.NormA, c.NormB,
	}
}

func (c conflictNode) fields() map[string]any {
	out := map[string]any{"id": c.ID}
	for i, name := range conflictColumns {
		out[name] = c.values()[i]
	}
	return out
}

// eachConflict walks the findings in a fixed order, skipping any whose norms the
// projection did not write. A conflict node pointing at a statement that is not
// in the graph is an edge nothing can traverse and a count nobody can explain.
func eachConflict(in Input, visit func(conflictNode) error) error {
	if len(in.Conflicts) == 0 {
		return nil
	}
	norms := make(map[string]bool, len(in.Statements))
	for i := range in.Statements {
		norms[in.Statements[i].ID] = true
	}
	findings := make([]conflict.Finding, len(in.Conflicts))
	copy(findings, in.Conflicts)
	sort.Slice(findings, func(i, j int) bool { return findings[i].ID() < findings[j].ID() })

	for i := range findings {
		f := &findings[i]
		if f.A == nil || f.B == nil || !norms[f.A.StatementID] || !norms[f.B.StatementID] {
			continue
		}
		party, act, object := f.A.Words()
		node := conflictNode{
			ID: "vn:conflict:" + shortID(f.ID()), Rule: f.Rule, Circumstances: f.Circumstances,
			Party: party, Act: act, Object: object,
			Matched:     slotList(f.Matched),
			Rank:        conflict.Describe(f.Rank, f.A, f.B),
			Explanation: f.Explanation,
			NormA:       f.A.StatementID, NormB: f.B.StatementID,
		}
		if len(f.Clashing) > 0 {
			node.ClashingSlot = f.Clashing[0].Name
			node.ClashingA = f.Clashing[0].A
			node.ClashingB = f.Clashing[0].B
		}
		if err := visit(node); err != nil {
			return err
		}
	}
	return nil
}

// shortID hashes nothing. The finding identifier is already stable and already
// unique, and it is made into a node identifier by replacing the separator the
// rest of the identifiers in this graph use for their own segments.
func shortID(id string) string {
	return strings.ReplaceAll(strings.ReplaceAll(id, "|", "+"), "vn:norm:", "")
}

func slotList(slots []conflict.Slot) string {
	parts := make([]string, 0, len(slots))
	for _, s := range slots {
		parts = append(parts, s.Name+": "+s.A)
	}
	return strings.Join(parts, "; ")
}
