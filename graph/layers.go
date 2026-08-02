package graph

import (
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/relation"
	"github.com/tamnd/luatdo/temporal"
)

// The relation layer and the temporal layer, projected.
//
// Both were built in earlier milestones and neither reached the database, which
// meant the graph could not be asked seven of the twenty three questions no
// matter how good the extraction behind it was. A layer that exists only as
// JSON on one machine is a layer nobody has.

// Two layers that both name a thing a version, and two that both call an edge
// BROADER, are the collisions this projection has to keep apart. Neo4j has no
// namespaces: a query is a label and a relationship type, and a type that means
// two things means neither.
//
// The rule is that the name goes to whoever shipped it first, and the layer
// arriving later takes a qualified one. That is not a claim about which layer
// matters more. It is a claim that renaming an edge type somebody has already
// written a query against costs more than naming a new one carefully.
const (
	// EventLabel is one applied amendment operation, with what it closed and
	// what it opened.
	EventLabel = "Event"
	// TemporalVersionLabel is what a component said over one interval, derived
	// from the amendment chain.
	//
	// TextVersion, which the document layer writes, is the text as one
	// instrument states it, keyed by its hash. This one is keyed by sequence and
	// carries an interval and a force state. Article 94 of the Labour Code has
	// one TextVersion per instrument that printed it and one of these per period
	// during which it said a particular thing, and answering question 16 with
	// the wrong one of those gives a confident answer about the wrong date.
	TemporalVersionLabel = "TemporalVersion"

	// HasTemporalVersion hangs an interval off the component it belongs to.
	// HAS_VERSION already points at TextVersion.
	HasTemporalVersion = "HAS_TEMPORAL_VERSION"
	// Includes is the aggregation edge: an article version holds the clause
	// versions in force under it, which is how an unchanged clause is reused
	// rather than copied.
	Includes = "INCLUDES"
	// CausedBy points from an event to the instrument that performed it.
	CausedBy = "CAUSED_BY"
	// Terminates and ProducesVersion are the two halves of an event. An event
	// with neither changed nothing and is not written.
	//
	// The version half is qualified because the relation registry already holds
	// PRODUCES, meaning one concept produces another: an application produces a
	// permit. An amendment producing a version of a clause is a different claim
	// over different nodes, and both layers land in the database in the same
	// milestone, so the tie goes to the layer built first.
	Terminates      = "TERMINATES"
	ProducesVersion = "PRODUCES_VERSION"

	// AboutConcept joins a norm to the concept one of its participants resolves
	// to.
	//
	// Without it the norm layer and the concept layer were two graphs sharing a
	// database and touching nowhere, and every question of the form "which norms
	// are about X" had to be asked with a string match on the bearer text, which
	// is the thing this project exists to stop doing. Six of the twenty three
	// questions cross that join.
	//
	// The role is a property rather than four relationship types, because a
	// concept is regulated in all four roles and almost every question about it
	// wants all four.
	AboutConcept = "ABOUT_CONCEPT"
)

// relationTypeRenames maps a relation layer type onto the name it takes in the
// database, for the ones another layer already owns.
//
// BROADER belongs to the subject taxonomy, which the document layer has been
// writing since M6. A concept being broader than another concept and a filing
// category being broader than another filing category are different claims over
// different nodes, and a reader who writes MATCH ()-[:BROADER]->() expecting one
// of them should not silently get both.
var relationTypeRenames = map[string]string{
	relation.Broader: "CONCEPT_BROADER",
}

// RelationType is the relationship type an edge of this relation type is
// written under.
func RelationType(t string) string {
	if renamed, ok := relationTypeRenames[t]; ok {
		return renamed
	}
	return t
}

// RelationTypes lists every relationship type the relation layer can write, in
// the registry's order, under the names the database uses. The merge path needs
// the list up front because each type gets its own statement.
func RelationTypes() []string {
	seed := []string{
		relation.Broader, relation.PartOf, relation.Requires, relation.Produces,
		relation.Grants, relation.IssuedBy, relation.RegulatedBy, relation.EvidencedBy,
		relation.MeasuredIn, relation.AlternativeTo, relation.Excludes, relation.Replaces,
	}
	out := make([]string, 0, len(seed))
	for _, t := range seed {
		out = append(out, RelationType(t))
	}
	return out
}

// relationRow is one concept to concept edge as the database holds it.
type relationRow struct {
	From, To, Type string
	Status         string
	Source         string
	Support        int
	SupportDocs    int
	Confidence     float64
	Direction      string
	Quote          string
	ProvisionID    string
}

// eachRelation visits the concept relations that have both endpoints in this
// projection.
//
// Only canonical edges are written. A provisional edge is one the corpus stated
// once, or one whose type the registry does not hold, and both are review queue
// entries rather than claims about the law. Shipping them would put the queue in
// the graph and let a traversal walk over work nobody has done.
//
// The endpoint check is not defensive coding, it is the importer's contract:
// neo4j-admin refuses an entire import over one relationship row naming a node
// it does not have, so an edge into a concept this export left out has to be
// dropped here rather than discovered at three in the morning on a server.
func eachRelation(in Input, visit func(relationRow) error) error {
	if len(in.Relations) == 0 || in.Layer == nil {
		return nil
	}
	concepts := map[string]bool{}
	for _, c := range in.Layer.Concepts {
		concepts[c.ID] = true
	}
	for i := range in.Relations {
		e := &in.Relations[i]
		if e.Status != relation.StatusCanonical {
			continue
		}
		if !concepts[e.FromID] || !concepts[e.ToID] {
			continue
		}
		row := relationRow{
			From: e.FromID, To: e.ToID, Type: RelationType(e.Type),
			Status: e.Status, Source: e.Source,
			Support: e.SupportCount, SupportDocs: e.SupportDocs,
			Confidence: e.Confidence, Direction: e.Direction,
		}
		// One quote rides on the edge. The rest stay in the store: a graph is
		// for finding the claim and the store is for arguing with it, and an
		// edge carrying forty quotes is an edge nobody can read in a browser.
		if len(e.Evidence) > 0 {
			row.Quote, row.ProvisionID = e.Evidence[0].Quote, e.Evidence[0].ProvisionID
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return nil
}

// mergedConcepts is the set of concept IDs this export writes, which is what an
// edge into the concept layer has to land in.
func mergedConcepts(in Input) map[string]bool {
	out := map[string]bool{}
	if in.Layer == nil {
		return out
	}
	for _, c := range in.Layer.Concepts {
		out[c.ID] = true
	}
	return out
}

// eachNormConcept visits every ABOUT_CONCEPT edge the statement set contributes,
// as a norm, a concept and the role the concept plays in that norm.
//
// A participant with a concept the merge never produced is skipped rather than
// written, for the importer's reason again. It is also the honest count: the
// campaign report says 0 of 1798 participant references resolve today, and an
// edge into a concept that does not exist would turn that zero into a number
// without resolving anything.
func eachNormConcept(in Input, visit func(normID, conceptID, role string) error) error {
	concepts := mergedConcepts(in)
	if len(concepts) == 0 {
		return nil
	}
	for i := range in.Statements {
		r := &in.Statements[i]
		s := &r.Statement
		for _, p := range []struct {
			ref  *norm.Ref
			role string
		}{
			{s.Bearer, "bearer"},
			{s.Counterparty, "counterparty"},
			{&s.Action, "action"},
			{s.Object, "object"},
		} {
			if p.ref == nil || !concepts[p.ref.ConceptID] {
				continue
			}
			if err := visit(r.ID, p.ref.ConceptID, p.role); err != nil {
				return err
			}
		}
	}
	return nil
}

// eachEvent visits the amendment events, and eachTemporalVersion the intervals.
// Both skip nothing: the layer was checked when it was built.
func eachEvent(in Input, visit func(*temporal.Event) error) error {
	if in.Temporal == nil {
		return nil
	}
	for i := range in.Temporal.Events {
		if err := visit(&in.Temporal.Events[i]); err != nil {
			return err
		}
	}
	return nil
}

func eachTemporalVersion(in Input, visit func(*temporal.Version) error) error {
	if in.Temporal == nil {
		return nil
	}
	for i := range in.Temporal.Versions {
		if err := visit(&in.Temporal.Versions[i]); err != nil {
			return err
		}
	}
	return nil
}

// eachTemporalEdge visits every edge the temporal layer contributes, checked
// against the nodes this export writes, for the same reason as eachRelation.
func eachTemporalEdge(in Input, visit func(from, to, relType string) error) error {
	if in.Temporal == nil {
		return nil
	}
	components, docs := map[string]bool{}, map[string]bool{}
	for _, d := range in.Docs {
		docs[d.ID] = true
	}
	if err := eachComponent(in.Docs, func(_ *law.Document, c *law.Component) error {
		components[c.ID] = true
		return nil
	}); err != nil {
		return err
	}
	versions := map[string]bool{}
	for i := range in.Temporal.Versions {
		versions[in.Temporal.Versions[i].ID] = true
	}
	for i := range in.Temporal.Versions {
		v := &in.Temporal.Versions[i]
		if components[v.ComponentID] {
			if err := visit(v.ComponentID, v.ID, HasTemporalVersion); err != nil {
				return err
			}
		}
		for _, child := range v.Children {
			if !versions[child] {
				continue
			}
			if err := visit(v.ID, child, Includes); err != nil {
				return err
			}
		}
	}
	for i := range in.Temporal.Events {
		e := &in.Temporal.Events[i]
		if docs[e.CausedByDoc] {
			if err := visit(e.ID, e.CausedByDoc, CausedBy); err != nil {
				return err
			}
		}
		for _, id := range e.Terminates {
			if versions[id] {
				if err := visit(e.ID, id, Terminates); err != nil {
					return err
				}
			}
		}
		for _, id := range e.Produces {
			if versions[id] {
				if err := visit(e.ID, id, ProducesVersion); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// eventFields and temporalVersionFields flatten a node for both writers, the
// same way normFields does, so the CSV columns and the Bolt properties cannot
// drift apart.
func eventFields(e *temporal.Event) map[string]any {
	return map[string]any{
		"kind": e.Kind, "date": e.Date, "instrument_from": e.InstrumentFrom,
		"caused_by_doc": e.CausedByDoc, "caused_by": e.CausedBy,
		"targets": e.Targets, "instruction": e.Instruction,
		"confidence": e.Confidence, "model": e.Model,
	}
}

var eventColumns = []string{
	"kind", "date", "instrument_from", "caused_by_doc", "caused_by",
	"targets", "instruction", "confidence", "model",
}

var eventColumnTypes = map[string]string{"confidence": "float"}

func temporalVersionFields(v *temporal.Version) map[string]any {
	return map[string]any{
		"component_id": v.ComponentID, "doc_id": v.DocID, "kind": v.Kind,
		"number": v.Number, "seq": v.Seq, "text": v.Text,
		"from_date": v.From, "to_date": v.To, "force": v.Force,
		"produced_by": v.ProducedBy, "terminated_by": v.TerminatedBy,
	}
}

var temporalVersionColumns = []string{
	"component_id", "doc_id", "kind", "number", "seq", "text",
	"from_date", "to_date", "force", "produced_by", "terminated_by",
}

var temporalVersionColumnTypes = map[string]string{"seq": "int"}
