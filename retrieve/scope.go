package retrieve

import (
	"fmt"
	"strings"
)

// Scope is the part of the corpus a question is allowed to be answered from.
//
// Every field is a constraint the caller states, and an empty field constrains
// nothing. They are applied in the order the fields are listed, cheapest and
// coarsest first, and each one reports what it removed. A caller who gets no
// answer can then see which of their own constraints emptied the corpus, which
// is nearly always the real question when a retrieval returns nothing.
type Scope struct {
	// Docs restricts to whole instruments by identifier.
	Docs []string
	// Subjects restricts to the documents the subject layer put under these
	// subjects. A subject identifier matches its children, so "thue" reaches
	// "thue/thue-gia-tri-gia-tang".
	Subjects []string
	// Components restricts to subtrees of the hierarchy. A component
	// identifier matches itself and everything under it, so an article
	// identifier reaches its clauses and their points.
	Components []string
	// Kinds restricts by structural kind: article, clause, point.
	Kinds []string
	// Date restricts to components whose wording was in force on that day, in
	// ISO form. A component the temporal layer never stamped is dropped and
	// counted separately, because "no interval was recorded" and "the wording
	// had ended" are different facts and neither of them is "in force".
	Date string
	// Statements restricts to components that carry at least one trusted
	// statement. It is what the answerer uses, since a component with no
	// statement gives it nothing it is allowed to assert.
	Statements bool
	// Unread admits components whose only intervals rest on an amendment
	// nobody has read, treating the original wording as still current. It is
	// off by default because it is a guess, and it exists because on a corpus
	// where almost nothing has been read the strict filter answers nothing at
	// all, and a caller is owed the choice between a guess they were told about
	// and no answer.
	Unread bool
}

// Empty reports whether the scope constrains anything at all.
func (s Scope) Empty() bool {
	return len(s.Docs) == 0 && len(s.Subjects) == 0 && len(s.Components) == 0 &&
		len(s.Kinds) == 0 && s.Date == "" && !s.Statements
}

// Step is one filter's effect: what it was, how many components it was handed
// and how many it passed on.
type Step struct {
	Filter string `json:"filter"`
	Detail string `json:"detail,omitempty"`
	Before int    `json:"before"`
	After  int    `json:"after"`
	Note   string `json:"note,omitempty"`
}

// String renders one step for a terminal.
func (s Step) String() string {
	line := fmt.Sprintf("%-12s %6d to %6d", s.Filter, s.Before, s.After)
	if s.Detail != "" {
		line += "  " + s.Detail
	}
	if s.Note != "" {
		line += "  " + s.Note
	}
	return line
}

// Select applies the scope and returns which components survived, with the
// arithmetic of each filter.
func (ix *Index) Select(s Scope) ([]bool, []Step) {
	keep := make([]bool, len(ix.units))
	for i := range keep {
		keep[i] = true
	}
	var steps []Step
	apply := func(filter, detail string, pass func(*Unit) bool) {
		before, after := 0, 0
		for i, u := range ix.units {
			if !keep[i] {
				continue
			}
			before++
			if pass(u) {
				after++
				continue
			}
			keep[i] = false
		}
		steps = append(steps, Step{Filter: filter, Detail: detail, Before: before, After: after})
	}

	if len(s.Docs) > 0 {
		want := set(s.Docs)
		apply("doc", strings.Join(s.Docs, ","), func(u *Unit) bool { return want[u.DocID] })
	}
	if len(s.Subjects) > 0 {
		apply("subject", strings.Join(s.Subjects, ","), func(u *Unit) bool {
			for _, have := range u.Subjects {
				for _, want := range s.Subjects {
					if have == want || strings.HasPrefix(have, want+"/") {
						return true
					}
				}
			}
			return false
		})
	}
	if len(s.Components) > 0 {
		apply("component", strings.Join(s.Components, ","), func(u *Unit) bool {
			for _, want := range s.Components {
				if u.ComponentID == want || strings.HasPrefix(u.ComponentID, want+":") {
					return true
				}
			}
			return false
		})
	}
	if len(s.Kinds) > 0 {
		want := set(s.Kinds)
		apply("kind", strings.Join(s.Kinds, ","), func(u *Unit) bool { return want[u.Kind] })
	}
	if s.Statements {
		apply("statements", "components with a trusted statement", func(u *Unit) bool {
			return len(u.Statements) > 0
		})
	}
	if s.Date != "" {
		unstamped, unread, assumed := 0, 0, 0
		before := len(steps)
		detail := s.Date
		if s.Unread {
			detail += ", assuming an unread amendment left the wording alone"
		}
		apply("date", detail, func(u *Unit) bool {
			if len(u.Intervals) == 0 {
				unstamped++
				return false
			}
			if u.InForceAt(s.Date) {
				return true
			}
			if !u.Unread() {
				return false
			}
			unread++
			if s.Unread && u.startedBy(s.Date) {
				assumed++
				return true
			}
			return false
		})
		var notes []string
		if unstamped > 0 {
			notes = append(notes, fmt.Sprintf("%d of those had no recorded interval and were dropped for that rather than for being out of force", unstamped))
		}
		if unread > 0 {
			notes = append(notes, fmt.Sprintf("%d rest on an amendment nobody has read, which is the temporal layer saying it does not know rather than saying no, and %d of those were admitted", unread, assumed))
		}
		steps[before].Note = strings.Join(notes, "; ")
	}
	return keep, steps
}

func set(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}
