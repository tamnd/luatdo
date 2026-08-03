package temporal

import (
	"fmt"
	"sort"
	"strings"
)

// The nine invariants from the specification, run as code against the built
// layer.
//
// They are checks and not assertions. A layer that violates one is still
// written, because a graph that refuses to exist teaches nobody what went
// wrong, and the violations are the map of where the reading is weak. What the
// checks must never do is pass quietly on a layer that has a hole in it: an
// interval gap answers a point in time query with "no version" and that reads
// exactly like a repeal.

// Report is what a check run found.
type Report struct {
	Versions    int       `json:"versions"`
	Events      int       `json:"events"`
	Components  int       `json:"components"`
	Quarantined int       `json:"quarantined"`
	Undated     int       `json:"undated"`
	Problems    []Problem `json:"problems,omitempty"`
}

// Add records one violation.
func (r *Report) Add(invariant int, subject, format string, args ...any) {
	r.Problems = append(r.Problems, Problem{
		Invariant: invariant, Subject: subject, Detail: fmt.Sprintf(format, args...),
	})
}

// OK reports whether the layer violated nothing.
func (r *Report) OK() bool { return len(r.Problems) == 0 }

func (r *Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d versions of %d components, %d events, %d quarantined, %d undated\n",
		r.Versions, r.Components, r.Events, r.Quarantined, r.Undated)
	if r.OK() {
		b.WriteString("all nine invariants hold\n")
		return b.String()
	}
	byInvariant := map[int]int{}
	for _, p := range r.Problems {
		byInvariant[p.Invariant]++
	}
	nums := make([]int, 0, len(byInvariant))
	for n := range byInvariant {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, n := range nums {
		fmt.Fprintf(&b, "invariant %d broken %d times\n", n, byInvariant[n])
	}
	return b.String()
}

// Check runs the invariants over a built layer.
//
// Invariant 9, the consolidated text comparison, needs a consolidated text to
// compare against and lives in its own pass, because it is the only check that
// depends on a document outside the layer.
func Check(l *Layer) *Report {
	v := NewView(l)
	r := &Report{
		Versions: len(l.Versions), Events: len(l.Events),
		Components: len(v.byPart), Quarantined: len(l.Quarantined),
	}
	for _, e := range l.Events {
		if e.Date == "" {
			r.Undated++
		}
	}

	checkOne(v, r)
	checkTwo(v, r)
	checkThree(v, r)
	checkFour(v, r)
	checkSix(v, r)
	return r
}

// Invariant 1: no event terminates a version that starts after the event date.
// An amendment dated before the text it changes is a reading defect, not a fact
// about the law, and applying it puts a negative interval in the graph.
func checkOne(v *View, r *Report) {
	for _, e := range v.eventList {
		if e.Date == "" {
			continue
		}
		for _, id := range e.Terminates {
			ver := v.versions[id]
			if ver == nil {
				r.Add(1, e.ID, "terminates %s, which is not in the layer", id)
				continue
			}
			if ver.From != "" && ver.From > e.Date {
				r.Add(1, e.ID, "dated %s terminates %s which starts %s", e.Date, id, ver.From)
			}
		}
	}
}

// Invariant 2: every version has a start, and every version except the current
// one has an end.
func checkTwo(v *View, r *Report) {
	for component, list := range v.byPart {
		for i, ver := range list {
			if ver.From == "" {
				r.Add(2, ver.ID, "has no start date")
			}
			if i < len(list)-1 && ver.To == "" {
				r.Add(2, ver.ID, "is not the last version of %s and has no end date", component)
			}
		}
	}
}

// Invariant 3: intervals for one component are contiguous and do not overlap.
//
// A gap usually means a repeal with no successor was recorded as an amendment,
// and it is worth naming because a gap and a repeal answer a point in time query
// the same way while meaning opposite things.
func checkThree(v *View, r *Report) {
	for component, list := range v.byPart {
		for i := 1; i < len(list); i++ {
			prev, cur := list[i-1], list[i]
			if prev.To == "" || cur.From == "" {
				continue
			}
			switch {
			case prev.To > cur.From:
				r.Add(3, component, "%s ends %s and %s starts %s, which overlap",
					prev.ID, prev.To, cur.ID, cur.From)
			case prev.To < cur.From:
				r.Add(3, component, "%s ends %s and %s starts %s, so nothing is in force between them",
					prev.ID, prev.To, cur.ID, cur.From)
			}
		}
	}
}

// Invariant 4: every event has a cause and a target that resolved, or it is
// quarantined. Also invariant 5 in effect: an event whose amending instrument is
// empty has no instrument in the corpus to point at.
func checkFour(v *View, r *Report) {
	for _, e := range v.eventList {
		if e.CausedByDoc == "" {
			r.Add(5, e.ID, "names no amending instrument")
		}
		if e.Targets == "" {
			r.Add(4, e.ID, "resolved no target and was applied anyway")
		}
		if e.Kind == KindEnact || e.Kind == KindConsolidate {
			continue
		}
		if e.CausedBy == "" {
			r.Add(4, e.ID, "names no provision holding the instruction")
		}
	}
}

// Invariant 6: a component with a repeal has no later version unless an enact
// or a resume explains it.
//
// The subtlety is that a repeal terminates more versions than it repeals. When
// one clause goes, every component above it gets a new version with that child
// removed, and the old version of each of them is terminated by the same repeal
// event. Read naively, an article that lost a clause looks like an article that
// was repealed and then came back the same day.
//
// The corpus says so out loud. Quyết định 06/2001 had provisions repealed out
// of two of its articles by two later decisions, and the article that lost a
// clause of Nghị định 03/2003 did the same thing, and all nine violations on the
// labour campaign were of this shape. None of them was a provision coming back
// from the dead, which is the only thing this check is for, and nine false ones
// are how a person learns to stop reading the check.
//
// So the terminating event has to have targeted this component or something
// above it. A repeal aimed at a descendant is a component being re-issued
// smaller, and that is ordinary.
func checkSix(v *View, r *Report) {
	under := descendants(v)
	for component, list := range v.byPart {
		repealedAt := ""
		for _, ver := range list {
			if repealedAt != "" && ver.From != "" && ver.From >= repealedAt {
				kind := ""
				if e := v.events[ver.ProducedBy]; e != nil {
					kind = e.Kind
				}
				if kind != KindEnact && kind != KindResume {
					r.Add(6, component, "was repealed on %s and %s starts %s by %s",
						repealedAt, ver.ID, ver.From, orNone(kind))
				}
				repealedAt = ""
			}
			e := v.events[ver.TerminatedBy]
			if e == nil || (e.Kind != KindRepeal && e.Kind != KindReplace && e.Kind != KindExpire) {
				continue
			}
			if under[component][e.Targets] {
				continue
			}
			repealedAt = ver.To
		}
	}
}

// descendants maps each component onto every component beneath it.
//
// It is built from the children the versions carry rather than from the
// identifiers, because an identifier does not hold the whole path: an article
// inside a chapter is numbered against its document, so the chapter's
// identifier is not a prefix of the article's and a string test misses exactly
// the case that matters.
func descendants(v *View) map[string]map[string]bool {
	parent := map[string]string{}
	for _, ver := range v.versions {
		for _, id := range ver.Children {
			if child := v.versions[id]; child != nil && child.ComponentID != ver.ComponentID {
				parent[child.ComponentID] = ver.ComponentID
			}
		}
	}
	under := map[string]map[string]bool{}
	for c := range parent {
		seen := map[string]bool{c: true}
		for p := parent[c]; p != "" && !seen[p]; p = parent[p] {
			seen[p] = true
			if under[p] == nil {
				under[p] = map[string]bool{}
			}
			under[p][c] = true
		}
	}
	return under
}

func orNone(kind string) string {
	if kind == "" {
		return "no event at all"
	}
	return "an event of kind " + kind
}

// CheckNorms runs invariant 7: every norm's interval sits inside the interval of
// the version it was read from. Norms arrive in M11, so this takes the intervals
// rather than the norms and is called from there.
func CheckNorms(v *View, intervals []Interval, r *Report) {
	for _, in := range intervals {
		ver := v.versions[in.VersionID]
		if ver == nil {
			r.Add(7, in.ID, "cites version %s, which is not in the layer", in.VersionID)
			continue
		}
		if !within(in.From, in.To, ver.From, ver.To) {
			r.Add(7, in.ID, "runs %s to %s, outside %s which runs %s to %s",
				orOpen(in.From), orOpen(in.To), ver.ID, orOpen(ver.From), orOpen(ver.To))
		}
	}
}

// CheckTermUses runs invariant 8: a term use cannot outlive the component that
// defines it. It is the same containment as invariant 7 against a different
// citing thing, and it is separate because the two are reported separately.
func CheckTermUses(v *View, intervals []Interval, r *Report) {
	for _, in := range intervals {
		ver := v.versions[in.VersionID]
		if ver == nil {
			r.Add(8, in.ID, "cites version %s, which is not in the layer", in.VersionID)
			continue
		}
		if !within(in.From, in.To, ver.From, ver.To) {
			r.Add(8, in.ID, "runs %s to %s, outside %s which runs %s to %s",
				orOpen(in.From), orOpen(in.To), ver.ID, orOpen(ver.From), orOpen(ver.To))
		}
	}
}

// Interval is a dated thing that hangs off a version: a norm, a term use, a
// relation edge. Containment is checked the same way for all three, so they are
// checked through one type rather than three near identical loops.
type Interval struct {
	ID        string `json:"id"`
	VersionID string `json:"version_id"`
	From      string `json:"from_date,omitempty"`
	To        string `json:"to_date,omitempty"`
}

// within reports whether one half open interval sits inside another. An open end
// is the end of time, so an open inner end is contained only by an open outer
// end.
func within(from, to, outerFrom, outerTo string) bool {
	if from == "" || outerFrom == "" {
		return false
	}
	if from < outerFrom {
		return false
	}
	if outerTo == "" {
		return true
	}
	if to == "" {
		return false
	}
	return to <= outerTo
}

func orOpen(date string) string {
	if date == "" {
		return "open"
	}
	return date
}
