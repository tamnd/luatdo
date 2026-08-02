package norm

import (
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// Procedures are grouped after extraction rather than during it.
//
// A procedure is spread over several provisions and no single call sees more
// than one of them. An extractor asked to place a step in a procedure it cannot
// see will name one anyway, and the names it invents differ between calls, so
// the same procedure ends up as four procedures with one step each. Grouping
// afterwards, from the identifier the extractor wrote and the order of the
// provisions in the document, is the only place the whole thing is visible.

// Procedure is one ordered sequence of steps.
type Procedure struct {
	ID    string `json:"id"`
	DocID string `json:"doc_id"`
	Label string `json:"label"`
	Steps []Step `json:"steps"`
}

// Step is one statement in its place in a procedure.
type Step struct {
	Number      int    `json:"number"`
	StatementID string `json:"statement_id"`
	ProvisionID string `json:"provision_id"`
	Bearer      string `json:"bearer,omitempty"`
	Action      string `json:"action"`
	Deadline    string `json:"deadline,omitempty"`
}

// GroupProcedures collects the steps of every procedure in a record set.
//
// Steps are ordered by the step number the extractor gave, and ties are broken
// by where the provision sits in its document. The tiebreak carries most of the
// weight: numbers disagree across calls and document order does not, and a
// procedure whose steps come back in the wrong order answers question 11 with
// instructions that cannot be followed.
func GroupProcedures(records []Record, position map[string]int) []Procedure {
	byID := map[string]*Procedure{}
	for i := range records {
		r := &records[i]
		s := &r.Statement
		if s.ProcedureID == "" || !r.Trusted() {
			continue
		}
		// The identifier is scoped to the document. Two documents that both
		// call a procedure "cap-phep" are describing two different procedures,
		// and merging them across documents is how a construction permit
		// acquires a step from a fishing licence.
		id := r.DocID + ":procedure:" + law.Slug(s.ProcedureID)
		p, ok := byID[id]
		if !ok {
			p = &Procedure{ID: id, DocID: r.DocID, Label: strings.TrimSpace(s.ProcedureID)}
			byID[id] = p
		}
		step := Step{Number: s.Step, StatementID: r.ID, ProvisionID: r.ProvisionID, Action: s.Action.Text}
		if s.Bearer != nil {
			step.Bearer = s.Bearer.Text
		}
		if s.Deadline != nil {
			step.Deadline = s.Deadline.Text
		}
		p.Steps = append(p.Steps, step)
	}
	out := make([]Procedure, 0, len(byID))
	for _, p := range byID {
		sort.SliceStable(p.Steps, func(i, j int) bool {
			a, b := p.Steps[i], p.Steps[j]
			if a.Number != b.Number {
				return a.Number < b.Number
			}
			return position[a.ProvisionID] < position[b.ProvisionID]
		})
		for i := range p.Steps {
			p.Steps[i].Number = i + 1
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Positions numbers every provision of a document set in reading order, which
// is the tiebreak GroupProcedures needs.
func Positions(docs []*law.Document) map[string]int {
	out := map[string]int{}
	n := 0
	for _, d := range docs {
		for i := range d.Provisions {
			out[d.Provisions[i].ID] = n
			n++
		}
	}
	return out
}
