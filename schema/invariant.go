// Package schema gives the closed registry a way to find out what it is
// missing, and measures the finding out.
//
// The registry is frozen on first use and a model may not extend it, which is
// the right contract and has one cost: the registry stays wrong in ways nobody
// can see from inside it. Everything here is an instrument pointed at that
// blind spot. Nothing in this package edits a registry version, and nothing
// here promotes a candidate; both of those are a person's decision and the
// queue is where they make it.
package schema

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/eval"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
)

// Item is one stored statement with the provision text its evidence has to
// quote. It is the input to every check in this file.
type Item struct {
	RecordID    string
	ProvisionID string
	DocID       string
	Statement   *norm.Statement
	Text        string
}

// Firing is what one invariant did over a corpus.
//
// Records and Breaks are both here because they answer different questions. A
// statement with three broken conditions breaks one invariant three times, and
// counting only breaks makes a rare defect in a verbose statement look common.
type Firing struct {
	Code      string   `json:"code"`
	Records   int      `json:"records"`
	Breaks    int      `json:"breaks"`
	First     int      `json:"first"`
	Mandatory bool     `json:"mandatory"`
	Examples  []string `json:"examples,omitempty"`
}

// MaxExamples is how many provision identifiers a firing carries. Enough to go
// and look, not enough to make the report a list.
const MaxExamples = 3

// Invariants is the firing distribution over a set of stored statements.
type Invariants struct {
	Records   int      `json:"records"`
	Broken    int      `json:"broken"`
	Breaks    int      `json:"breaks"`
	Firings   []Firing `json:"firings"`
	Mandatory int      `json:"mandatory_records"`
}

// CountInvariants re-runs the invariants over stored statements.
//
// Re-running rather than reading the stored message is the point. The message
// is prose that was formatted for a person, it interpolates the bearer text, so
// counting distinct messages reports one common defect as fifty rare ones. The
// statement and the provision are both on disk, so the check is exact and free.
func CountInvariants(reg *ontology.Registry, items []Item) Invariants {
	inv := Invariants{Records: len(items)}
	by := map[string]*Firing{}
	for _, code := range norm.Codes {
		by[code] = &Firing{Code: code, Mandatory: norm.Mandatory(code)}
	}
	for _, it := range items {
		vs := norm.Violations(it.Statement, reg, it.Text)
		if len(vs) == 0 {
			continue
		}
		inv.Broken++
		inv.Breaks += len(vs)
		seen := map[string]bool{}
		mandatory := false
		for i, v := range vs {
			f := by[v.Code]
			if f == nil {
				// An invariant that fires under a code the list does not have is
				// a code somebody forgot to register, and it is louder here than
				// in a report that quietly drops it.
				f = &Firing{Code: v.Code, Mandatory: norm.Mandatory(v.Code)}
				by[v.Code] = f
			}
			f.Breaks++
			if i == 0 {
				f.First++
			}
			if !seen[v.Code] {
				seen[v.Code] = true
				f.Records++
				if len(f.Examples) < MaxExamples {
					f.Examples = append(f.Examples, it.ProvisionID)
				}
			}
			if norm.Mandatory(v.Code) {
				mandatory = true
			}
		}
		if mandatory {
			inv.Mandatory++
		}
	}
	for _, f := range by {
		inv.Firings = append(inv.Firings, *f)
	}
	sort.Slice(inv.Firings, func(i, j int) bool {
		if inv.Firings[i].Records != inv.Firings[j].Records {
			return inv.Firings[i].Records > inv.Firings[j].Records
		}
		return inv.Firings[i].Code < inv.Firings[j].Code
	})
	return inv
}

// MandatoryShare is the share of broken statements that are broken because a
// required part is absent. It is the number the literature's prediction is
// about, so it is computed once and reported as a rate with its denominator.
func (inv Invariants) MandatoryShare() eval.Accuracy {
	return eval.Accuracy{Right: inv.Mandatory, Of: inv.Broken}
}

// Silent lists the invariants that never fired. They are part of the result:
// an invariant with no firings is either a defect the extractor does not have
// or a check that cannot fire, and only a person reading the list can tell.
func (inv Invariants) Silent() []string {
	var out []string
	for _, f := range inv.Firings {
		if f.Breaks == 0 {
			out = append(out, f.Code)
		}
	}
	sort.Strings(out)
	return out
}

func (inv Invariants) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "invariants over %d stored statements, %d of them broken in %d places\n",
		inv.Records, inv.Broken, inv.Breaks)
	width := 0
	for _, f := range inv.Firings {
		width = max(width, len(f.Code))
	}
	for _, f := range inv.Firings {
		if f.Breaks == 0 {
			continue
		}
		mark := " "
		if f.Mandatory {
			mark = "*"
		}
		fmt.Fprintf(&b, "  %s %-*s  %s  %d breaks, first violation on %d\n",
			mark, width, f.Code, eval.Accuracy{Right: f.Records, Of: inv.Broken}, f.Breaks, f.First)
	}
	fmt.Fprintf(&b, "  note: * marks a missing mandatory attribute, and those are %s of the broken statements\n",
		inv.MandatoryShare())
	if silent := inv.Silent(); len(silent) > 0 {
		fmt.Fprintf(&b, "  note: %d invariants never fired: %s\n", len(silent), strings.Join(silent, ", "))
	}
	return b.String()
}
