package temporal

import (
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/parse"
)

// The version graph is built by aggregation rather than by composition, which
// is the one design decision in this package worth arguing about.
//
// When an amendment changes clause 2 of article 15, the naive model creates a
// new version of the whole document. That duplicates every unchanged article
// and turns "what changed" into a diff problem rather than a graph query. The
// aggregation model creates a new version only for the changed component and
// for its ancestors, and the ancestors' new versions reuse the unchanged
// siblings' existing versions rather than copying them.
//
//	Before                        After amending clause 2 of article 15
//
//	Document v1                   Document v2
//	 |- Article 14 v1              |- Article 14 v1   (reused, same node)
//	 |- Article 15 v1              |- Article 15 v2   (new)
//	 |   |- Clause 1 v1            |   |- Clause 1 v1 (reused)
//	 |   |- Clause 2 v1            |   |- Clause 2 v2 (new)
//	 |- Article 16 v1              |- Article 16 v1   (reused)
//
// Three consequences earn the complexity. "Which parts of this law have never
// been amended" is a query for components with one version. "What changed on
// this date" is the set of versions one event produced. And storage stays
// proportional to the amount of change rather than to the number of
// amendments, which matters when a code has been amended twenty times.

type builder struct {
	corpus *Corpus

	versions map[string]*Version // version identifier to version
	order    []string            // version identifiers in creation order
	current  map[string]string   // component identifier to its open version
	seq      map[string]int      // component identifier to the last sequence used
	parent   map[string]string   // component identifier to its parent component
	kind     map[string]string
	number   map[string]string

	events      []Event
	quarantined []Operation
	dropped     bool // the operation being applied has already been quarantined
}

// Build constructs the version graph from the corpus and the operations.
//
// It returns the layer and the ties that could not be broken. A tie is two
// instruments changing the same component on the same day with neither
// outranking the other, and it is reported rather than resolved, because
// picking one silently is how a graph acquires an answer nobody can defend.
func Build(c *Corpus, ops []Operation) (*Layer, []string) {
	b := &builder{
		corpus:   c,
		versions: map[string]*Version{},
		current:  map[string]string{},
		seq:      map[string]int{},
		parent:   map[string]string{},
		kind:     map[string]string{},
		number:   map[string]string{},
	}

	// Every document an operation touches is enacted first, so there is
	// something for the amendments to change. A document nobody amends is not
	// versioned here: an unversioned document reports as unversioned rather
	// than as unamended, because "nothing changed" and "we did not look" are
	// different claims.
	for _, doc := range b.targets(ops) {
		b.enact(doc)
	}

	ordered, ties := Order(ops)
	for _, op := range ordered {
		if op.Quarantine != "" {
			b.quarantined = append(b.quarantined, op)
			continue
		}
		b.dropped = false
		if reason := b.refuse(op.TargetDoc); reason != "" {
			b.drop(op, reason)
			continue
		}
		b.apply(op)
	}

	layer := &Layer{Events: b.events, Quarantined: b.quarantined}
	for _, id := range b.order {
		layer.Versions = append(layer.Versions, *b.versions[id])
	}
	SortEvents(layer.Events)
	SortVersions(layer.Versions)
	return layer, ties
}

// refuse reports why a document cannot be versioned, or empty when it can.
//
// Both reasons come from the real corpus rather than from theory. Almost a
// third of parsed documents repeat a component identifier, because an amending
// law quotes the clauses it inserts and the parser numbers the quotation as
// structure; versioning one of those answers "what did clause 2 say" with text
// from whichever clause 2 was written last. And a document whose date this code
// cannot read has no place on a timeline at all, since every interval in this
// layer is compared as text and an empty date compares as the beginning of
// time. Refusing is loud: the operations are listed with the reason, so the
// document reports as not versioned instead of quietly answering wrongly.
func (b *builder) refuse(docID string) string {
	if docID == "" {
		return ""
	}
	if b.corpus.Colliding(docID) > 0 {
		return QuarantineCollidingParse
	}
	if doc := b.corpus.Document(docID); doc != nil && b.corpus.EffectiveFrom(docID) == "" {
		return QuarantineUndatedDocument
	}
	return ""
}

// targets returns the documents the operations change, in identifier order so
// the build is reproducible.
func (b *builder) targets(ops []Operation) []*law.Document {
	want := map[string]bool{}
	for _, op := range ops {
		if op.Quarantine == "" && op.TargetDoc != "" && b.refuse(op.TargetDoc) == "" {
			want[op.TargetDoc] = true
		}
	}
	ids := make([]string, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []*law.Document
	for _, id := range ids {
		if doc := b.corpus.Document(id); doc != nil {
			out = append(out, doc)
		}
	}
	return out
}

// enact writes the first version of every component of a document, plus the
// document component itself, which is what a point in time walk starts from.
func (b *builder) enact(doc *law.Document) {
	if _, done := b.current[doc.ID]; done {
		return
	}
	// The date the corpus states, in the form this layer compares. The corpus
	// writes 17/08/2007 and every interval here is compared as text.
	from := b.corpus.EffectiveFrom(doc.ID)
	event := Event{
		ID: OperationID(doc.ID, doc.ID, 0), Kind: KindEnact, Date: from,
		InstrumentFrom: from, CausedByDoc: doc.ID, Targets: doc.ID,
	}

	children := map[string][]string{}
	for i := range doc.Provisions {
		p := &doc.Provisions[i]
		parent := p.ParentID
		if parent == "" {
			parent = doc.ID
		}
		b.parent[p.ID] = parent
		b.kind[p.ID] = p.Kind
		b.number[p.ID] = p.Number
		children[parent] = append(children[parent], p.ID)
	}
	// Deepest first, so a parent's children already have versions when the
	// parent is written and the child version identifiers are known.
	for _, id := range depthOrder(doc, children) {
		v := b.newVersion(id, doc.ID, from, textOf(doc, id), event.ID)
		for _, child := range children[id] {
			v.Children = append(v.Children, b.current[child])
		}
		event.Produces = append(event.Produces, v.ID)
	}
	root := b.newVersion(doc.ID, doc.ID, from, "", event.ID)
	root.Kind = "document"
	for _, child := range children[doc.ID] {
		root.Children = append(root.Children, b.current[child])
	}
	event.Produces = append(event.Produces, root.ID)
	b.events = append(b.events, event)
}

// depthOrder returns component identifiers deepest first, so a parent is
// written after its children and can hold their version identifiers.
func depthOrder(doc *law.Document, _ map[string][]string) []string {
	depth := map[string]int{}
	for i := range doc.Provisions {
		p := &doc.Provisions[i]
		// Depth is the number of structural segments after the document
		// identifier, which the identifier already states.
		depth[p.ID] = strings.Count(strings.TrimPrefix(p.ID, doc.ID), ":")
	}
	ids := make([]string, 0, len(depth))
	for id := range depth {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if depth[ids[i]] != depth[ids[j]] {
			return depth[ids[i]] > depth[ids[j]]
		}
		return ids[i] < ids[j]
	})
	return ids
}

func textOf(doc *law.Document, id string) string {
	for i := range doc.Provisions {
		if doc.Provisions[i].ID == id {
			return doc.Provisions[i].Text
		}
	}
	return ""
}

// newVersion opens a version of a component and makes it the current one.
func (b *builder) newVersion(componentID, docID, from, text, producedBy string) *Version {
	b.seq[componentID]++
	v := &Version{
		ID: VersionID(componentID, b.seq[componentID]), ComponentID: componentID,
		DocID: docID, Kind: b.kind[componentID], Number: b.number[componentID],
		Seq: b.seq[componentID], Text: text, From: from, Force: ForceInForce,
		ProducedBy: producedBy,
	}
	b.versions[v.ID] = v
	b.order = append(b.order, v.ID)
	b.current[componentID] = v.ID
	return v
}

// apply turns one operation into an event and whatever version changes it
// implies.
func (b *builder) apply(op Operation) {
	b.dropped = false
	event := Event{
		ID: op.ID, Kind: op.Kind, Date: op.Date(), InstrumentFrom: op.InstrumentFrom,
		CausedByDoc: op.AmendingDoc, CausedBy: op.CausedBy, Targets: op.TargetComponent,
		Instruction: op.Instruction, CharStart: op.CharStart, CharEnd: op.CharEnd,
		Confidence: op.Confidence, Model: op.Model,
	}
	if op.Undated() {
		// An undated amendment cannot be placed in time, so it changes nothing
		// and is listed. Giving it a guessed date would propagate that guess
		// into every interval downstream, where it is indistinguishable from a
		// date somebody read off the instrument.
		b.events = append(b.events, event)
		return
	}

	switch op.Kind {
	case KindConsolidate:
		// A consolidated text is published rather than enacted. It changes
		// nothing and it is the ground truth the verification pass reads.
		b.events = append(b.events, event)
		return
	case KindReplace, KindExpire:
		b.closeDocument(&event, op)
	case KindRepeal:
		b.repeal(&event, op)
	case KindSuspend:
		b.reforce(&event, op, ForceSuspended)
	case KindResume:
		b.reforce(&event, op, ForceInForce)
	case KindRenumber:
		b.renumber(&event, op)
	case KindSupplement:
		b.supplement(&event, op)
	case KindEnact, KindAmend:
		b.amend(&event, op)
	}
	if op.Phrase != nil {
		b.substitute(&event, op)
	}
	if event.Terminates == nil && event.Produces == nil {
		// An event that closed nothing and opened nothing did not happen, and
		// recording it as though it had would put a change in the history that
		// no version supports. If a step already said why, that reason stands:
		// one operation is quarantined once, with the reason nearest the cause.
		if !b.dropped {
			b.drop(op, QuarantineNothingToChange)
		}
		return
	}
	b.events = append(b.events, event)
}

// drop keeps an operation without applying it. Every quarantined operation is
// listed and queryable, and it changes nothing.
func (b *builder) drop(op Operation, reason string) {
	op.Quarantine = reason
	b.quarantined = append(b.quarantined, op)
	b.dropped = true
}

// open returns a component's current version if the date can close it.
func (b *builder) open(componentID, date string) *Version {
	id, ok := b.current[componentID]
	if !ok {
		return nil
	}
	v := b.versions[id]
	if v.To != "" {
		return nil
	}
	if v.From != "" && date < v.From {
		// Invariant 1: no event terminates a version that starts after the
		// event date. This is usually an amendment whose stated date is earlier
		// than the text it claims to change, which is a reading defect and not
		// a fact about the law.
		return nil
	}
	return v
}

func (b *builder) terminate(v *Version, event *Event, date string) {
	v.To = date
	v.TerminatedBy = event.ID
	event.Terminates = append(event.Terminates, v.ID)
}

// succeed is how a component moves to the state an event leaves it in.
//
// A version is identified by its component and the day it starts, so an event
// landing on the day the open version started rewrites that version instead of
// closing it and opening another. Two instruments amending the same code on the
// same day, or an instrument amended on the day it commenced, otherwise leave a
// version that starts and ends on one date, which under half open intervals is
// a version nothing was ever in force under. The trial run made three of them
// out of two amendments, and they came back as the answer to "which norms were
// in force for less than a year".
func (b *builder) succeed(event *Event, old *Version, date, text string) *Version {
	if old.From == date && old.To == "" {
		old.Text = text
		// The version now stands for the state after this event as well, and the
		// event that produced it last is the one to name.
		old.ProducedBy = event.ID
		event.Produces = append(event.Produces, old.ID)
		return old
	}
	b.terminate(old, event, date)
	v := b.newVersion(old.ComponentID, old.DocID, date, text, event.ID)
	v.Children = old.Children
	event.Produces = append(event.Produces, v.ID)
	return v
}

// amend replaces a component's text.
func (b *builder) amend(event *Event, op Operation) {
	if strings.TrimSpace(op.NewText) == "" && op.Phrase == nil {
		b.drop(op, QuarantineNoText)
		return
	}
	if op.Phrase != nil {
		return // handled by substitute
	}
	old := b.open(op.TargetComponent, op.Date())
	if old == nil {
		b.drop(op, QuarantineNothingToChange)
		return
	}
	// "Điều 73 được sửa đổi, bổ sung như sau:" quotes the whole article,
	// numbered clauses and all, and the quotation is the article from that day
	// on. Keeping the old clauses under the new wording is how article 73 of the
	// anti corruption law came back with its 2005 clauses appended to its 2007
	// text: the same rule stated twice, in two different forms, in one answer.
	// So a replacement replaces the subtree, and a change that is meant to leave
	// the subtree alone is a supplement or a phrase edit rather than this.
	for _, child := range old.Children {
		if cv := b.versions[child]; cv != nil && cv.To == "" {
			b.closeSubtree(event, cv, op.Date())
		}
	}
	text, under := restructure(op)
	v := b.succeed(event, old, op.Date(), text)
	v.Children = nil
	b.regrow(event, v, op, under)
	b.reparent(event, op.TargetComponent, old.ID, v.ID, op.Date())
}

// restructure reads the replacement text as the component it replaces.
//
// The quotation an amending instrument carries is a component, not a paragraph.
// Where it states structure the structure is kept, and where it states a
// sentence the sentence is stored the way the parser would have stored it.
func restructure(op Operation) (string, []law.Provision) {
	kind, number := kindOf(op.TargetComponent), numberOf(op.TargetComponent)
	parsed := parse.Fragment(op.TargetDoc, op.NewText)
	for i := range parsed {
		if parsed[i].ID != op.TargetComponent {
			continue
		}
		var under []law.Provision
		for j := range parsed {
			if strings.HasPrefix(parsed[j].ID, op.TargetComponent+":") {
				under = append(under, parsed[j])
			}
		}
		return parsed[i].Text, under
	}
	return parse.TrimMarker(kind, number, op.NewText), nil
}

// regrow writes a version of every component the replacement text states under
// the one it replaces, and hangs them off it.
//
// A component the old subtree had and the replacement does not stays closed,
// which is the whole point: the replacement is the component from that day on,
// and what it leaves out is gone.
func (b *builder) regrow(event *Event, v *Version, op Operation, under []law.Provision) {
	if len(under) == 0 {
		return
	}
	children := map[string][]string{}
	for i := range under {
		p := &under[i]
		b.parent[p.ID] = p.ParentID
		b.kind[p.ID] = p.Kind
		b.number[p.ID] = p.Number
		children[p.ParentID] = append(children[p.ParentID], p.ID)
	}
	// Deepest first, so a parent is written after its children and can hold
	// their version identifiers. This is the rule enact walks by.
	ids := make([]string, 0, len(under))
	text := map[string]string{}
	for i := range under {
		ids = append(ids, under[i].ID)
		text[under[i].ID] = under[i].Text
	}
	sort.Slice(ids, func(i, j int) bool {
		di, dj := strings.Count(ids[i], ":"), strings.Count(ids[j], ":")
		if di != dj {
			return di > dj
		}
		return ids[i] < ids[j]
	})
	for _, id := range ids {
		nv := b.newVersion(id, op.TargetDoc, op.Date(), text[id], event.ID)
		for _, child := range children[id] {
			nv.Children = append(nv.Children, b.current[child])
		}
		event.Produces = append(event.Produces, nv.ID)
	}
	for _, child := range children[op.TargetComponent] {
		v.Children = append(v.Children, b.current[child])
	}
}

// supplement inserts a component that does not exist yet, or adds text to one
// that does. The distinction is the whole reason the reading has an anchor:
// "bổ sung điểm d vào sau điểm c" inserts one point and leaves its siblings
// alone, and treating it as a replacement of the clause would delete them.
func (b *builder) supplement(event *Event, op Operation) {
	if _, exists := b.current[op.TargetComponent]; exists {
		old := b.open(op.TargetComponent, op.Date())
		if old == nil {
			b.drop(op, QuarantineNothingToChange)
			return
		}
		text := old.Text
		if strings.TrimSpace(op.NewText) != "" {
			text = strings.TrimSpace(old.Text + "\n" + op.NewText)
		}
		v := b.succeed(event, old, op.Date(), text)
		b.reparent(event, op.TargetComponent, old.ID, v.ID, op.Date())
		return
	}

	parent := parentOf(op.TargetComponent)
	if parent == "" || b.open(parent, op.Date()) == nil {
		b.drop(op, QuarantineUnresolvedTarget)
		return
	}
	b.parent[op.TargetComponent] = parent
	b.kind[op.TargetComponent] = kindOf(op.TargetComponent)
	b.number[op.TargetComponent] = numberOf(op.TargetComponent)
	v := b.newVersion(op.TargetComponent, op.TargetDoc, op.Date(), op.NewText, event.ID)
	event.Produces = append(event.Produces, v.ID)
	b.insert(event, parent, v.ID, op)
}

// insert opens a new version of the parent holding the new child at the
// position the anchor names.
func (b *builder) insert(event *Event, parent, childVersion string, op Operation) {
	old := b.open(parent, op.Date())
	if old == nil {
		return
	}
	children := append([]string(nil), old.Children...)
	at := len(children)
	if op.Anchor != nil {
		if path, ok := ParsePath(op.Anchor.Sibling); ok {
			sibling := op.TargetDoc + ":" + path
			if !strings.Contains(op.Anchor.Sibling, "Điều") && !strings.Contains(op.Anchor.Sibling, "điều") {
				// A sibling written as "điểm c" carries no article, so it is
				// read relative to the component being inserted.
				sibling = siblingOf(op.TargetComponent, path)
			}
			for i, id := range children {
				if b.versions[id].ComponentID == sibling {
					at = i
					if op.Anchor.Position == "after" {
						at = i + 1
					}
					break
				}
			}
		}
	}
	children = append(children[:at], append([]string{childVersion}, children[at:]...)...)

	v := b.succeed(event, old, op.Date(), old.Text)
	v.Children = children
	b.reparent(event, parent, old.ID, v.ID, op.Date())
}

// repeal closes a component and every component under it.
func (b *builder) repeal(event *Event, op Operation) {
	old := b.open(op.TargetComponent, op.Date())
	if old == nil {
		b.drop(op, QuarantineNothingToChange)
		return
	}
	b.closeSubtree(event, old, op.Date())
	b.reparent(event, op.TargetComponent, old.ID, "", op.Date())
}

func (b *builder) closeSubtree(event *Event, v *Version, date string) {
	for _, child := range v.Children {
		cv := b.versions[child]
		if cv != nil && cv.To == "" {
			b.closeSubtree(event, cv, date)
		}
	}
	if v.To == "" {
		b.terminate(v, event, date)
	}
}

// closeDocument ends a whole instrument, which is what replace and expire do.
func (b *builder) closeDocument(event *Event, op Operation) {
	root := op.TargetDoc
	v := b.open(root, op.Date())
	if v == nil {
		b.drop(op, QuarantineNothingToChange)
		return
	}
	event.Targets = root
	b.closeSubtree(event, v, op.Date())
}

// reforce moves a component between in force and suspended without changing a
// word of it. A suspended provision is neither live nor repealed, and a model
// with two states answers one of those two, both wrong.
func (b *builder) reforce(event *Event, op Operation, force string) {
	old := b.open(op.TargetComponent, op.Date())
	if old == nil {
		b.drop(op, QuarantineNothingToChange)
		return
	}
	if old.Force == force {
		b.drop(op, QuarantineNothingToChange)
		return
	}
	v := b.succeed(event, old, op.Date(), old.Text)
	v.Force = force
	b.reparent(event, op.TargetComponent, old.ID, v.ID, op.Date())
}

// renumber keeps the identity and changes the position. It is an event kind
// because an amendment that renumbers is why a later amendment's "khoản 2" can
// name something that no longer exists.
func (b *builder) renumber(event *Event, op Operation) {
	old := b.open(op.TargetComponent, op.Date())
	if old == nil {
		b.drop(op, QuarantineNothingToChange)
		return
	}
	v := b.succeed(event, old, op.Date(), old.Text)
	if n := strings.TrimSpace(op.NewText); n != "" {
		v.Number = n
	}
	b.reparent(event, op.TargetComponent, old.ID, v.ID, op.Date())
}

// substitute applies a phrase edit to every component it named, and to nothing
// else. A find that is in none of them changes nothing and is quarantined,
// because a substitution that matched nowhere is a reading that was wrong about
// where the phrase is.
func (b *builder) substitute(event *Event, op Operation) {
	hit := false
	for _, target := range op.Phrase.Targets {
		old := b.open(target, op.Date())
		if old == nil || !strings.Contains(old.Text, op.Phrase.Find) {
			continue
		}
		hit = true
		text := strings.ReplaceAll(old.Text, op.Phrase.Find, op.Phrase.Replace)
		v := b.succeed(event, old, op.Date(), text)
		b.reparent(event, target, old.ID, v.ID, op.Date())
	}
	if !hit {
		b.drop(op, QuarantinePhraseNotFound)
	}
}

// reparent is the aggregation step. Every ancestor of a changed component gets
// a new version whose children are the old ones with a single identifier
// swapped, so the siblings that did not change are reused rather than copied.
func (b *builder) reparent(event *Event, componentID, oldVersion, newVersion, date string) {
	child := componentID
	oldID, newID := oldVersion, newVersion
	for {
		parent, ok := b.parent[child]
		if !ok || parent == "" {
			return
		}
		old := b.open(parent, date)
		if old == nil {
			return
		}
		var children []string
		for _, id := range old.Children {
			switch {
			case id != oldID:
				children = append(children, id)
			case newID != "":
				children = append(children, newID)
			}
		}
		oldParentID := old.ID
		v := b.succeed(event, old, date, old.Text)
		v.Children = children

		child, oldID, newID = parent, oldParentID, v.ID
	}
}

// parentOf returns the component one level up, or the document for a top level
// component.
func parentOf(componentID string) string {
	i := strings.LastIndex(componentID, ":")
	if i < 0 {
		return ""
	}
	last := componentID[i+1:]
	if !isStructural(last) {
		return ""
	}
	return componentID[:i]
}

func isStructural(segment string) bool {
	for _, prefix := range []string{"chapter-", "section-", "article-", "clause-", "point-"} {
		if strings.HasPrefix(segment, prefix) {
			return true
		}
	}
	return false
}

func kindOf(componentID string) string {
	i := strings.LastIndex(componentID, ":")
	if i < 0 {
		return ""
	}
	segment := componentID[i+1:]
	if j := strings.Index(segment, "-"); j > 0 {
		return segment[:j]
	}
	return ""
}

func numberOf(componentID string) string {
	i := strings.LastIndex(componentID, ":")
	if i < 0 {
		return ""
	}
	segment := componentID[i+1:]
	if j := strings.Index(segment, "-"); j > 0 {
		return segment[j+1:]
	}
	return ""
}

// siblingOf rewrites a relative reference such as "điểm c" against the
// component being inserted, so it names a sibling rather than a component of
// the document root.
func siblingOf(componentID, path string) string {
	parent := parentOf(componentID)
	if parent == "" {
		return path
	}
	if i := strings.LastIndex(path, ":"); i >= 0 {
		path = path[i+1:]
	}
	return parent + ":" + path
}
