// Package temporal turns amending instructions into events, and events into a
// version graph the corpus can be queried through at a date.
//
// The layer exists because a legal knowledge graph without time is a graph of
// what the law says today, and it is wrong about today the moment the corpus
// moves. It is silently wrong, which is the worse kind: a repealed provision
// looks exactly like a live one, and a norm read out of the 2012 Labour Code
// sits next to one from the 2019 Labour Code with nothing telling them apart.
//
// M4 produced 75,252 AMENDS edges between documents with their direction
// corrected. An edge between two documents is close to useless on its own. It
// says instrument A touched instrument B, not which article, not how, and not
// what B said before and after. This package is the interpretation of those
// edges, kept in separate files from them, so the raw fact stays checkable and
// a bad interpretation never destroys the evidence it was read from.
package temporal

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Event kinds, chosen to match how Vietnamese instruments describe what they
// are doing rather than to fit a general purpose versioning model.
//
// Suspend and resume exist because Vietnamese practice uses them and because a
// suspended provision is a third state. A two state model renders it as either
// in force or repealed, and both of those answers are wrong.
const (
	KindEnact       = "enact"       // ban hành, a component comes into existence
	KindAmend       = "amend"       // sửa đổi, existing text is changed
	KindSupplement  = "supplement"  // bổ sung, new text is added and nothing removed
	KindRepeal      = "repeal"      // bãi bỏ, hủy bỏ, a component ceases to have force
	KindReplace     = "replace"     // thay thế, a whole instrument supersedes another
	KindExpire      = "expire"      // hết hiệu lực, force ends with no act repealing it
	KindSuspend     = "suspend"     // ngưng hiệu lực, force is paused
	KindResume      = "resume"      // tiếp tục hiệu lực, force resumes after suspension
	KindRenumber    = "renumber"    // sắp xếp lại, identity is preserved and position changes
	KindConsolidate = "consolidate" // hợp nhất, a consolidated text is published
)

// Kinds lists the event kinds in the order they are documented.
var Kinds = []string{
	KindEnact, KindAmend, KindSupplement, KindRepeal, KindReplace,
	KindExpire, KindSuspend, KindResume, KindRenumber, KindConsolidate,
}

// KnownKind reports whether a kind is one of the ten.
func KnownKind(kind string) bool { return slices.Contains(Kinds, kind) }

// Scope is how much of the target one operation touches. It is separate from
// kind because "sửa đổi, bổ sung khoản 2" replaces a whole clause and "bổ sung
// điểm d vào sau điểm c" inserts one point beside its siblings, and those
// produce different version graphs from the same two words.
const (
	ScopeDocument = "document"
	ScopeArticle  = "article"
	ScopeClause   = "clause"
	ScopePoint    = "point"
	ScopePhrase   = "phrase"
)

// Quarantine reasons. A quarantined operation is listed with its instruction
// and never touches the version graph, because an amendment applied to the
// wrong component corrupts every point in time answer downstream and looks
// exactly like a correct one.
const (
	QuarantineUnresolvedTarget = "unresolved_target" // the target component is not in the corpus
	QuarantineMissingDocument  = "missing_document"  // the target document was never parsed
	QuarantineNoTargets        = "no_targets"        // a phrase edit that named no component
	QuarantineNoText           = "no_text"           // an amend that supplied no replacement text
	QuarantinePhraseNotFound   = "phrase_not_found"  // the phrase to replace is not in the target
	QuarantineNothingToChange  = "nothing_to_change" // the target had no version in force
	QuarantineUnknownKind      = "unknown_kind"      // a kind outside the ten
	QuarantineCollidingParse   = "colliding_parse"   // the target document repeats component identifiers
	QuarantineUndatedDocument  = "undated_document"  // the target document states no date this code can read
	QuarantineCompoundTarget   = "compound_target"   // one reference names more than one component
)

// Force is what a version's interval means. A version that exists is not the
// same as a version in force, and collapsing the two is how a suspended
// provision gets answered as live.
const (
	ForceInForce   = "in_force"
	ForceSuspended = "suspended"
)

// Anchor is where an inserted component goes. Insertion without an anchor is a
// component appended to the end of its parent, which is usually wrong and
// always unverifiable.
type Anchor struct {
	Position string `json:"position"` // after or before
	Sibling  string `json:"sibling"`  // the component it sits next to, as written
}

// PhraseEdit is a substitution across a named set of components.
//
// Targets is explicit and an empty list is a validation failure rather than a
// wildcard. "Thay thế cụm từ X bằng cụm từ Y" scoped to three articles and
// applied corpus wide silently corrupts everything it touches.
type PhraseEdit struct {
	Find    string   `json:"find"`
	Replace string   `json:"replace"`
	Targets []string `json:"targets"`
}

// Operation is one amending instruction as the model read it.
//
// It is the reading and not yet the change. The target reference is resolved
// through the citation index rather than by the model, the version graph is
// built in code, and the resulting text is assembled in code. The model reads
// the sentence. It does not edit the corpus.
type Operation struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	AmendingDoc string `json:"amending_doc"`
	CausedBy    string `json:"caused_by"` // the provision holding the instruction

	TargetDoc       string  `json:"target_doc,omitempty"`       // resolved document identifier
	TargetNumber    string  `json:"target_number,omitempty"`    // the official number as written
	TargetComponent string  `json:"target_component,omitempty"` // resolved identifier
	TargetRef       string  `json:"target_ref,omitempty"`       // as the drafter wrote it
	Scope           string  `json:"scope,omitempty"`
	Anchor          *Anchor `json:"anchor,omitempty"`

	NewText string      `json:"new_text,omitempty"`
	Phrase  *PhraseEdit `json:"phrase_edit,omitempty"`

	// EffectiveFrom is empty when nothing stated it and metadata does not have
	// it. It is never guessed. A guessed date propagates into every interval
	// downstream and is indistinguishable from a real one afterwards.
	EffectiveFrom  string `json:"effective_from,omitempty"`
	InstrumentFrom string `json:"instrument_from,omitempty"`

	Instruction string  `json:"instruction"`
	CharStart   int     `json:"char_start"`
	CharEnd     int     `json:"char_end"`
	Confidence  float64 `json:"confidence,omitempty"`
	Model       string  `json:"model,omitempty"`

	// Quarantine is why this operation was not applied. An operation carrying
	// one is kept, listed and queryable, and it changes nothing.
	Quarantine string `json:"quarantine,omitempty"`
}

// Date is the day the operation takes effect.
//
// A drafter writes a date on an individual amendment only when it differs from
// the instrument's own commencement. The usual case states nothing, and the
// date is then the day the amending instrument took effect, which the
// instrument itself states. Reading it off the instrument is not the guess the
// specification warns about: that is inventing a date for an amendment whose
// instrument gives none either, and those stay undated here.
//
// The first real instrument this pass read is the whole argument. Both of its
// amendments state no date of their own, and without this fallback the layer
// recorded two events, changed nothing, and reported itself as consistent.
func (o Operation) Date() string {
	if d := strings.TrimSpace(o.EffectiveFrom); d != "" {
		return d
	}
	return strings.TrimSpace(o.InstrumentFrom)
}

// Undated reports whether an operation has no date at all. Undated operations
// are excluded from point in time queries rather than given a date.
func (o Operation) Undated() bool { return o.Date() == "" }

// OperationID builds the stable identifier of an operation. Sequence is the
// index of the instruction within its own provision, so two instructions in one
// clause do not collide and the identifier is reproducible from the reading.
func OperationID(amendingDoc, target string, seq int) string {
	if target == "" {
		target = "unresolved"
	}
	return fmt.Sprintf("vn:event:%s:%s:%d", amendingDoc, target, seq)
}

// Event is an operation that was applied, with what it closed and what it
// opened. It is derived from the operations and the corpus, never edited by
// hand, and rebuilt whole whenever either moves.
type Event struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Date           string `json:"date,omitempty"`
	InstrumentFrom string `json:"instrument_from,omitempty"`
	CausedByDoc    string `json:"caused_by_doc"`
	CausedBy       string `json:"caused_by,omitempty"`
	Targets        string `json:"targets,omitempty"`
	Instruction    string `json:"instruction,omitempty"`
	CharStart      int    `json:"char_start,omitempty"`
	CharEnd        int    `json:"char_end,omitempty"`

	Terminates []string `json:"terminates,omitempty"`
	Produces   []string `json:"produces,omitempty"`

	Confidence float64 `json:"confidence,omitempty"`
	Model      string  `json:"model,omitempty"`
}

// Version is what a component said over one interval.
//
// Component identity is stable and survives its own amendment: article 94 of
// the Labour Code is one component forever, and what it says is a sequence of
// versions. Children hold version identifiers rather than component
// identifiers, which is the aggregation model: a new version of an article
// reuses the existing versions of the clauses that did not change.
type Version struct {
	ID          string   `json:"id"`
	ComponentID string   `json:"component_id"`
	DocID       string   `json:"doc_id"`
	Kind        string   `json:"kind"`             // chapter, article, clause, point
	Number      string   `json:"number,omitempty"` // renumbering changes this and not the identity
	Seq         int      `json:"seq"`
	Text        string   `json:"text,omitempty"`
	Children    []string `json:"children,omitempty"`

	From  string `json:"from_date,omitempty"`
	To    string `json:"to_date,omitempty"`
	Force string `json:"force"`

	ProducedBy   string `json:"produced_by,omitempty"`
	TerminatedBy string `json:"terminated_by,omitempty"`
}

// VersionID builds a version identifier from its component and sequence.
func VersionID(componentID string, seq int) string {
	return fmt.Sprintf("%s@v%d", componentID, seq)
}

// Covers reports whether a version's interval contains a date. The interval is
// half open: a version that ends on the day its successor starts is not in
// force on that day, because otherwise the same date has two answers.
func (v Version) Covers(date string) bool {
	if v.From == "" || date == "" {
		return false
	}
	if date < v.From {
		return false
	}
	return v.To == "" || date < v.To
}

// InForceAt reports whether a version is both current and not suspended.
func (v Version) InForceAt(date string) bool {
	return v.Covers(date) && v.Force == ForceInForce
}

// Layer is the built temporal layer.
type Layer struct {
	Events      []Event     `json:"events"`
	Versions    []Version   `json:"versions"`
	Quarantined []Operation `json:"quarantined,omitempty"`
}

// SortEvents orders events for storage: by date, then by the instrument, then
// by identifier, so a rebuild writes the same file.
func SortEvents(events []Event) {
	sort.SliceStable(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		if a.CausedByDoc != b.CausedByDoc {
			return a.CausedByDoc < b.CausedByDoc
		}
		return a.ID < b.ID
	})
}

// SortVersions orders versions by component and then by sequence.
func SortVersions(versions []Version) {
	sort.SliceStable(versions, func(i, j int) bool {
		if versions[i].ComponentID != versions[j].ComponentID {
			return versions[i].ComponentID < versions[j].ComponentID
		}
		return versions[i].Seq < versions[j].Seq
	})
}

// Order sorts operations into the order they are applied.
//
// Effective date first, because that is the order the law took effect in. Ties
// are broken by the hierarchy of the announcing instrument and then by its
// identifier, deterministically. A tie that survives both is reported rather
// than resolved silently, because two instruments changing the same component
// on the same day is a fact somebody should look at.
func Order(ops []Operation) ([]Operation, []string) {
	out := make([]Operation, len(ops))
	copy(out, ops)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Date() != b.Date() {
			return a.Date() < b.Date()
		}
		ra, rb := rank(a.AmendingDoc), rank(b.AmendingDoc)
		if ra != rb {
			return ra < rb
		}
		if a.AmendingDoc != b.AmendingDoc {
			return a.AmendingDoc < b.AmendingDoc
		}
		return a.ID < b.ID
	})

	var ties []string
	for i := 1; i < len(out); i++ {
		a, b := out[i-1], out[i]
		if a.Date() == b.Date() && a.AmendingDoc != b.AmendingDoc &&
			rank(a.AmendingDoc) == rank(b.AmendingDoc) &&
			a.TargetComponent != "" && a.TargetComponent == b.TargetComponent {
			ties = append(ties, fmt.Sprintf("%s and %s both change %s on %s and neither outranks the other",
				a.AmendingDoc, b.AmendingDoc, a.TargetComponent, a.Date()))
		}
	}
	return out, ties
}

// rank orders instruments by the force of the body that issued them, read off
// the identifier suffix. A lower number outranks a higher one. Anything not
// recognised sorts last rather than in the middle, because guessing at the
// hierarchy of an unknown instrument is worse than admitting it is unknown.
func rank(docID string) int {
	switch {
	case strings.HasPrefix(docID, "vn:constitution:"):
		return 0
	case strings.Contains(docID, "-qh"):
		return 1
	case strings.Contains(docID, "-ubtvqh"), strings.Contains(docID, "-pl-"):
		return 2
	case strings.Contains(docID, "-nd-cp"):
		return 3
	case strings.Contains(docID, "-qd-ttg"):
		return 4
	case strings.Contains(docID, "-tt-"):
		return 5
	default:
		return 9
	}
}

// Counts is what one built layer holds, for coverage and for the progress
// report a campaign prints.
type Counts struct {
	Events      int            `json:"events"`
	Versions    int            `json:"versions"`
	Components  int            `json:"components"`
	Amended     int            `json:"amended"`   // components with more than one version
	Repealed    int            `json:"repealed"`  // components with no version in force
	Suspended   int            `json:"suspended"` // components whose current version is paused
	Undated     int            `json:"undated"`
	Quarantined int            `json:"quarantined"`
	ByKind      map[string]int `json:"by_kind,omitempty"`
	ByReason    map[string]int `json:"by_reason,omitempty"`
}

// Count summarises a layer.
func Count(l *Layer) Counts {
	c := Counts{ByKind: map[string]int{}, ByReason: map[string]int{}}
	c.Events = len(l.Events)
	c.Versions = len(l.Versions)
	c.Quarantined = len(l.Quarantined)
	for _, e := range l.Events {
		c.ByKind[e.Kind]++
		if e.Date == "" {
			c.Undated++
		}
	}
	for _, q := range l.Quarantined {
		c.ByReason[q.Quarantine]++
	}
	byComponent := map[string][]Version{}
	for _, v := range l.Versions {
		byComponent[v.ComponentID] = append(byComponent[v.ComponentID], v)
	}
	c.Components = len(byComponent)
	for _, vs := range byComponent {
		if len(vs) > 1 {
			c.Amended++
		}
		open := 0
		suspended := 0
		for _, v := range vs {
			if v.To == "" {
				open++
				if v.Force == ForceSuspended {
					suspended++
				}
			}
		}
		if open == 0 {
			c.Repealed++
		}
		if suspended > 0 {
			c.Suspended++
		}
	}
	return c
}

// String prints the counts with the denominators visible.
func (c Counts) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "temporal       %d events over %d components, %d versions\n", c.Events, c.Components, c.Versions)
	fmt.Fprintf(&b, "               %d components amended, %d no longer in force, %d suspended\n", c.Amended, c.Repealed, c.Suspended)
	if c.Undated > 0 {
		fmt.Fprintf(&b, "               %d events have no date and are out of every point in time answer\n", c.Undated)
	}
	if c.Quarantined > 0 {
		fmt.Fprintf(&b, "               %d instructions quarantined and applied to nothing\n", c.Quarantined)
		for _, reason := range sortedKeys(c.ByReason) {
			fmt.Fprintf(&b, "                 %-20s %d\n", reason, c.ByReason[reason])
		}
	}
	for _, kind := range Kinds {
		if n := c.ByKind[kind]; n > 0 {
			fmt.Fprintf(&b, "               %-12s %d\n", kind, n)
		}
	}
	return b.String()
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
