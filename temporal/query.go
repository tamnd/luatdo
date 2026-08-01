package temporal

import (
	"fmt"
	"sort"
	"strings"
)

// Every query in this file takes a date.
//
// That is the whole discipline. A query with no date parameter answers "as of
// whenever the corpus was built", which is an answer nobody asked for and which
// changes under the caller without warning. Making the date required means a
// caller who does not care has to write down the date they do not care about,
// and that is the point: they usually do care and had not noticed.

// View is the layer indexed for reading. It is built once and queried many
// times, because walking a version graph by scanning a slice is quadratic and
// the graph is the size of the corpus.
type View struct {
	versions   map[string]*Version   // version identifier
	byPart     map[string][]*Version // component identifier, in sequence order
	byDoc      map[string][]*Version // document identifier
	events     map[string]*Event
	eventList  []Event
	quarantine []Operation
}

// NewView indexes a built layer.
func NewView(l *Layer) *View {
	v := &View{
		versions: map[string]*Version{},
		byPart:   map[string][]*Version{},
		byDoc:    map[string][]*Version{},
		events:   map[string]*Event{},
	}
	for i := range l.Versions {
		ver := &l.Versions[i]
		v.versions[ver.ID] = ver
		v.byPart[ver.ComponentID] = append(v.byPart[ver.ComponentID], ver)
		v.byDoc[ver.DocID] = append(v.byDoc[ver.DocID], ver)
	}
	for _, list := range v.byPart {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Seq < list[j].Seq })
	}
	for i := range l.Events {
		v.events[l.Events[i].ID] = &l.Events[i]
	}
	v.eventList = l.Events
	v.quarantine = l.Quarantined
	return v
}

// Version returns one version by identifier.
func (v *View) Version(id string) *Version { return v.versions[id] }

// Event returns one event by identifier.
func (v *View) Event(id string) *Event { return v.events[id] }

// Versions returns every version of a component, oldest first.
func (v *View) Versions(componentID string) []*Version { return v.byPart[componentID] }

// Quarantined returns the operations that were read and not applied.
func (v *View) Quarantined() []Operation { return v.quarantine }

// VersionAt returns the version of a component covering a date, or nil. A
// suspended version is returned, because a caller asking what a component was
// on a date is owed the suspension rather than a silent nil.
func (v *View) VersionAt(componentID, date string) *Version {
	for _, ver := range v.byPart[componentID] {
		if ver.Covers(date) {
			return ver
		}
	}
	return nil
}

// TextAt returns what a component said on a date, with its children assembled
// in order underneath it. It reports false when the component had no version on
// that date, which is a different answer from the empty string.
func (v *View) TextAt(componentID, date string) (string, bool) {
	ver := v.VersionAt(componentID, date)
	if ver == nil {
		return "", false
	}
	return v.assemble(ver, map[string]bool{}), true
}

func (v *View) assemble(ver *Version, seen map[string]bool) string {
	if seen[ver.ID] {
		return ""
	}
	seen[ver.ID] = true
	var b strings.Builder
	if t := strings.TrimSpace(ver.Text); t != "" {
		b.WriteString(t)
	}
	for _, id := range ver.Children {
		child := v.versions[id]
		if child == nil {
			continue
		}
		part := v.assemble(child, seen)
		if part == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(part)
	}
	return b.String()
}

// InForceAt returns every component of a document in force on a date, as
// version pointers, in identifier order.
func (v *View) InForceAt(docID, date string) []*Version {
	var out []*Version
	for _, ver := range v.byDoc[docID] {
		if ver.InForceAt(date) {
			out = append(out, ver)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ComponentID < out[j].ComponentID })
	return out
}

// LastChange is the day the last dated version of a document begins, or an
// empty string if the document has none.
//
// It exists for one caller. A văn bản hợp nhất often states no date of its own,
// and comparing against it needs a day to compute the text at. The day the
// instrument last changed is the only day the drafter could have been
// consolidating, because a consolidation is published after the amendment it
// folds in and nothing has happened since.
func (v *View) LastChange(docID string) string {
	last := ""
	for _, ver := range v.byDoc[docID] {
		if ver.From > last {
			last = ver.From
		}
	}
	return last
}

// EventsBetween returns the events dated within a half open interval, oldest
// first. Undated events are never returned: an event with no date cannot be
// inside or outside an interval, and putting it in one silently invents a date.
func (v *View) EventsBetween(from, to string) []Event {
	var out []Event
	for _, e := range v.eventList {
		if e.Date == "" {
			continue
		}
		if from != "" && e.Date < from {
			continue
		}
		if to != "" && e.Date >= to {
			continue
		}
		out = append(out, e)
	}
	SortEvents(out)
	return out
}

// UndatedEvents returns the events excluded from every point in time query, so
// a caller can see what the answer did not cover.
func (v *View) UndatedEvents() []Event {
	var out []Event
	for _, e := range v.eventList {
		if e.Date == "" {
			out = append(out, e)
		}
	}
	SortEvents(out)
	return out
}

// Comparison is what one component said on two dates and whether it moved.
type Comparison struct {
	ComponentID string `json:"component_id"`
	EarlyDate   string `json:"early_date"`
	LateDate    string `json:"late_date"`
	EarlyText   string `json:"early_text,omitempty"`
	LateText    string `json:"late_text,omitempty"`
	EarlyForce  string `json:"early_force,omitempty"`
	LateForce   string `json:"late_force,omitempty"`
	Changed     bool   `json:"changed"`
	// Events are the events between the two dates that touched this component,
	// which is the answer to "why is it different" and not a second query.
	Events []Event `json:"events,omitempty"`
}

// AskWhatItSaid answers competency question 16: what did this component require
// on one date, and on another.
//
// It returns the events in between as well, because the two texts on their own
// invite the reader to guess which instrument moved it, and the graph knows.
func (v *View) AskWhatItSaid(componentID, early, late string) Comparison {
	c := Comparison{ComponentID: componentID, EarlyDate: early, LateDate: late}
	if ver := v.VersionAt(componentID, early); ver != nil {
		c.EarlyText = v.assemble(ver, map[string]bool{})
		c.EarlyForce = ver.Force
	}
	if ver := v.VersionAt(componentID, late); ver != nil {
		c.LateText = v.assemble(ver, map[string]bool{})
		c.LateForce = ver.Force
	}
	c.Changed = c.EarlyText != c.LateText || c.EarlyForce != c.LateForce
	for _, e := range v.EventsBetween(early, late) {
		if v.touches(e, componentID) {
			c.Events = append(c.Events, e)
		}
	}
	return c
}

func (v *View) touches(e Event, componentID string) bool {
	for _, id := range e.Terminates {
		if ver := v.versions[id]; ver != nil && ver.ComponentID == componentID {
			return true
		}
	}
	for _, id := range e.Produces {
		if ver := v.versions[id]; ver != nil && ver.ComponentID == componentID {
			return true
		}
	}
	return false
}

// ShortLived is one version that was replaced sooner than a threshold.
type ShortLived struct {
	VersionID   string `json:"version_id"`
	ComponentID string `json:"component_id"`
	DocID       string `json:"doc_id"`
	From        string `json:"from_date"`
	To          string `json:"to_date"`
	Days        int    `json:"days"`
	EndedBy     string `json:"ended_by,omitempty"`     // the event identifier
	EndedByDoc  string `json:"ended_by_doc,omitempty"` // the instrument that ended it
}

// AskShortLived answers competency question 17: which versions were in force for
// less than a span before being amended.
//
// Only closed intervals count. A version still open has been in force for as
// long as the caller's clock says, which is not a property of the graph, and
// counting it would make the answer change every day without the corpus moving.
func (v *View) AskShortLived(maxDays int) []ShortLived {
	var out []ShortLived
	for _, ver := range v.versions {
		if ver.From == "" || ver.To == "" {
			continue
		}
		days, ok := daysBetween(ver.From, ver.To)
		if !ok || days >= maxDays {
			continue
		}
		s := ShortLived{
			VersionID: ver.ID, ComponentID: ver.ComponentID, DocID: ver.DocID,
			From: ver.From, To: ver.To, Days: days, EndedBy: ver.TerminatedBy,
		}
		if e := v.events[ver.TerminatedBy]; e != nil {
			s.EndedByDoc = e.CausedByDoc
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Days != out[j].Days {
			return out[i].Days < out[j].Days
		}
		return out[i].VersionID < out[j].VersionID
	})
	return out
}

// Step is one link in a component's history.
type Step struct {
	VersionID   string `json:"version_id"`
	Seq         int    `json:"seq"`
	From        string `json:"from_date,omitempty"`
	To          string `json:"to_date,omitempty"`
	Force       string `json:"force"`
	EventID     string `json:"event_id,omitempty"`
	EventKind   string `json:"event_kind,omitempty"`
	CausedBy    string `json:"caused_by,omitempty"`    // the amending instrument
	CausedByAt  string `json:"caused_by_at,omitempty"` // the provision holding the instruction
	Instruction string `json:"instruction,omitempty"`  // as the instrument wrote it
	Text        string `json:"text,omitempty"`
}

// AskHistory answers competency question 18: the amendment history of one
// component as a chain of events with the instrument that caused each.
//
// The chain is the version sequence rather than a walk of the event list,
// because the versions are what the events produced and a version with no
// producing event is a defect the chain should show rather than hide.
func (v *View) AskHistory(componentID string) []Step {
	var out []Step
	for _, ver := range v.byPart[componentID] {
		s := Step{
			VersionID: ver.ID, Seq: ver.Seq, From: ver.From, To: ver.To,
			Force: ver.Force, Text: ver.Text, EventID: ver.ProducedBy,
		}
		if e := v.events[ver.ProducedBy]; e != nil {
			s.EventKind = e.Kind
			s.CausedBy = e.CausedByDoc
			s.CausedByAt = e.CausedBy
			s.Instruction = e.Instruction
		}
		out = append(out, s)
	}
	return out
}

// daysBetween counts whole days between two YYYY-MM-DD dates. It returns false
// on anything it cannot parse rather than a zero, because a zero here reads as
// "replaced the same day", which is a strong claim to make out of a typo.
func daysBetween(from, to string) (int, bool) {
	a, ok := julian(from)
	if !ok {
		return 0, false
	}
	b, ok := julian(to)
	if !ok {
		return 0, false
	}
	return b - a, true
}

// julian converts a date to a day number. It is arithmetic on the proleptic
// Gregorian calendar rather than a time.Parse, so a date the corpus states out
// of range, such as a 31st of a 30 day month, counts rather than erroring out.
func julian(date string) (int, bool) {
	var y, m, d int
	if _, err := fmt.Sscanf(date, "%4d-%2d-%2d", &y, &m, &d); err != nil {
		return 0, false
	}
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return 0, false
	}
	a := (14 - m) / 12
	yy := y + 4800 - a
	mm := m + 12*a - 3
	return d + (153*mm+2)/5 + 365*yy + yy/4 - yy/100 + yy/400 - 32045, true
}
