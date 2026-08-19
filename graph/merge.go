package graph

import (
	"context"
	"fmt"
	"os"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/subject"
	"github.com/tamnd/luatdo/temporal"
)

// asAny hands a string list to the driver as a list rather than as a Go slice
// the parameter encoder would have to guess at. A nil list is sent as an empty
// one, so a term with no aliases sets the property to [] instead of leaving
// whatever an earlier merge put there.
func asAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}
	return out
}

// Target is a Bolt connection. Values default from the LUATDO_NEO4J
// environment variables, so the servers and the Windows machine configure
// themselves the same way.
type Target struct {
	URI      string
	User     string
	Password string
	Database string
}

// TargetFromEnv reads LUATDO_NEO4J_URI, LUATDO_NEO4J_USER,
// LUATDO_NEO4J_PASSWORD, and LUATDO_NEO4J_DATABASE.
func TargetFromEnv() Target {
	t := Target{
		URI:      os.Getenv("LUATDO_NEO4J_URI"),
		User:     os.Getenv("LUATDO_NEO4J_USER"),
		Password: os.Getenv("LUATDO_NEO4J_PASSWORD"),
		Database: os.Getenv("LUATDO_NEO4J_DATABASE"),
	}
	if t.URI == "" {
		t.URI = "neo4j://localhost:7687"
	}
	if t.User == "" {
		t.User = "neo4j"
	}
	if t.Database == "" {
		t.Database = "neo4j"
	}
	return t
}

const mergeBatch = 500

// Merge upserts the projection over Bolt. Every statement is a MERGE keyed on
// the stable identifier, so running it twice changes nothing, and it is safe
// against a live database that already holds an earlier export.
func Merge(ctx context.Context, target Target, in Input) error {
	docs, links := in.Docs, in.Links
	driver, err := neo4j.NewDriverWithContext(target.URI, neo4j.BasicAuth(target.User, target.Password, ""))
	if err != nil {
		return fmt.Errorf("connect %s: %w", target.URI, err)
	}
	defer func() { _ = driver.Close(ctx) }()

	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: target.Database})
	defer func() { _ = session.Close(ctx) }()

	run := func(query string, rows []map[string]any) error {
		for start := 0; start < len(rows); start += mergeBatch {
			end := min(start+mergeBatch, len(rows))
			_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
				return tx.Run(ctx, query, map[string]any{"rows": rows[start:end]})
			})
			if err != nil {
				return err
			}
		}
		return nil
	}

	var docRows, componentRows, versionRows, containsRows, hasVersionRows, citesRows, amendsRows []map[string]any
	for _, d := range docs {
		row := docFields(d)
		row["id"] = d.ID
		docRows = append(docRows, row)
	}
	// The same walkers the CSV writer uses, so the two paths cannot disagree
	// about which components a document has.
	if err := eachComponent(docs, func(d *law.Document, c *law.Component) error {
		componentRows = append(componentRows, map[string]any{
			"id": c.ID, "kind": c.Kind, "number": c.Number,
			"heading": c.Heading, "position": c.Position, "renumbered_from": c.RenumberedFrom,
		})
		containsRows = append(containsRows, map[string]any{"from": containerOf(d, c), "to": c.ID})
		return nil
	}); err != nil {
		return err
	}
	if err := eachVersion(docs, func(_ *law.Document, v *law.TextVersion) error {
		versionRows = append(versionRows, map[string]any{
			"id": v.ID, "text": v.Text, "text_hash": v.TextHash,
			"from_date": v.FromDate, "to_date": v.ToDate,
			"caption": VersionCaption(v),
		})
		hasVersionRows = append(hasVersionRows, map[string]any{"from": v.ComponentID, "to": v.ID})
		return nil
	}); err != nil {
		return err
	}
	for _, l := range links {
		if l.ToDoc == "" {
			continue
		}
		from := l.FromProvision
		if from == "" {
			from = l.FromDoc
		}
		row := map[string]any{"from": from, "to": l.ToDoc, "method": l.Method, "snippet": l.Snippet}
		if l.Kind == "amends" {
			amendsRows = append(amendsRows, row)
		} else {
			citesRows = append(citesRows, row)
		}
	}

	var termRows, conceptRows, definesRows, mentionsRows []map[string]any
	var subjectRows, broaderRows, aboutRows []map[string]any
	if in.Vocabulary != nil {
		for _, s := range in.Vocabulary.Subjects {
			subjectRows = append(subjectRows, map[string]any{
				"id": s.ID, "label_vi": s.LabelVI, "label_en": s.LabelEN, "parent": s.Parent,
			})
			if s.Parent != "" {
				broaderRows = append(broaderRows, map[string]any{"from": s.ID, "to": s.Parent})
			}
		}
	}
	if err := eachAssignment(in, func(docID string, a *subject.Assignment) error {
		aboutRows = append(aboutRows, map[string]any{
			"from": docID, "to": a.SubjectID, "confidence": a.Confidence, "method": a.Method,
		})
		return nil
	}); err != nil {
		return err
	}
	seenTerms := map[string]bool{}
	for _, d := range in.Definitions {
		if !seenTerms[d.TermID] {
			seenTerms[d.TermID] = true
			termRows = append(termRows, map[string]any{"id": d.TermID, "text": d.Term})
		}
		definesRows = append(definesRows, map[string]any{"from": d.ProvisionID, "to": d.TermID, "connective": d.Connective})
	}
	if in.Registry != nil {
		for _, c := range in.Registry.Classes {
			conceptRows = append(conceptRows, map[string]any{"id": c.ID, "label_vi": c.LabelVI, "parent": c.Parent})
		}
	}
	for _, m := range in.Mentions {
		if m.TargetKind == "unresolved" || m.TargetID == "" {
			continue
		}
		mentionsRows = append(mentionsRows, map[string]any{
			"from": m.ProvisionID, "to": m.TargetID, "text": m.Text,
			"class_id": m.ClassID, "score": m.Score, "basis": m.Basis,
		})
	}

	var normRows, hasNormRows []map[string]any
	detailRows := map[string][]map[string]any{}
	normEdgeRows := map[string][]map[string]any{}
	classes := registryClasses(in)
	for i := range in.Statements {
		r := &in.Statements[i]
		row := normFields(r)
		row["id"] = r.ID
		normRows = append(normRows, row)
		hasNormRows = append(hasNormRows, map[string]any{"from": r.ProvisionID, "to": r.ID})
		normEdgeRows["HAS_LEGAL_BASIS"] = append(normEdgeRows["HAS_LEGAL_BASIS"], map[string]any{"from": r.ID, "to": r.ProvisionID})
		if err := eachNormClass(r, classes, func(classID, relType string) error {
			normEdgeRows[relType] = append(normEdgeRows[relType], map[string]any{"from": r.ID, "to": classID})
			return nil
		}); err != nil {
			return err
		}
	}
	if err := eachNormDetail(in.Statements, func(d normDetail) error {
		detailRows[d.Label] = append(detailRows[d.Label], map[string]any{
			"id": d.ID, "text": d.Text, "kind": d.Kind, "quote": d.Quote, "legal_basis": d.Basis,
		})
		normEdgeRows[d.RelType] = append(normEdgeRows[d.RelType], map[string]any{"from": d.NormID, "to": d.ID})
		return nil
	}); err != nil {
		return err
	}

	var termUseRows, mergedConceptRows, instanceOfRows, differsFromRows []map[string]any
	termUseEdgeRows := map[string][]map[string]any{}
	if err := eachTermUse(in.Layer, func(t *concept.TermUse) error {
		differentiae := make([]string, 0, len(t.Differentiae))
		for _, d := range t.Differentiae {
			differentiae = append(differentiae, d.Text)
		}
		byReference := ""
		if t.DefinesByReference != nil {
			byReference = t.DefinesByReference.Instrument
		}
		// Bolt carries a list as a list, so nothing is joined here. The CSV path
		// packs the same values into one cell only because that file format has
		// no other way to say it.
		termUseRows = append(termUseRows, map[string]any{
			"id": t.ID, "label_vi": t.LabelVI, "scope_id": t.ScopeID, "doc_id": t.DocID,
			"definition_vi": t.DefinitionVI, "genus": t.Genus, "kind": t.Kind,
			"aliases": asAny(t.Aliases), "differentiae": asAny(differentiae),
			"enumerated_subtypes": asAny(t.EnumeratedSubtypes),
			"is_role":             t.IsRole, "by_reference": byReference,
			"origin": t.Origin, "defined_by": t.DefinedBy, "quote": t.Quote,
			"char_start": t.CharStart, "char_end": t.CharEnd,
			"confidence": t.Confidence, "model": t.Model,
		})
		return nil
	}); err != nil {
		return err
	}
	if in.Layer != nil {
		for _, c := range in.Layer.Concepts {
			mergedConceptRows = append(mergedConceptRows, map[string]any{
				"id": c.ID, "label_vi": c.LabelVI, "label_en": c.LabelEN, "kind": c.Kind,
				"disambiguator": c.Disambiguator, "registry_class": c.RegistryClass,
			})
		}
	}
	if err := eachMembership(in.Layer, func(m *concept.Membership) error {
		instanceOfRows = append(instanceOfRows, map[string]any{
			"from": m.TermUseID, "to": m.ConceptID, "relation": m.Relation,
			"decided_by": m.DecidedBy, "decided_at": m.DecidedAt, "rationale": m.Rationale,
		})
		return nil
	}); err != nil {
		return err
	}
	if err := eachDifference(in.Layer, func(d *concept.Difference) error {
		differsFromRows = append(differsFromRows, map[string]any{
			"from": d.FromID, "to": d.ToID, "decided_by": d.DecidedBy,
			"decided_at": d.DecidedAt, "rationale": d.Rationale, "basis": asAny(d.Basis),
		})
		return nil
	}); err != nil {
		return err
	}
	if err := eachTermUseEdge(in, func(from, to, relType string) error {
		termUseEdgeRows[relType] = append(termUseEdgeRows[relType], map[string]any{"from": from, "to": to})
		return nil
	}); err != nil {
		return err
	}

	var eventRows, temporalVersionRows, aboutConceptRows []map[string]any
	relationRows := map[string][]map[string]any{}
	temporalEdgeRows := map[string][]map[string]any{}
	if err := eachRelation(in, func(r relationRow) error {
		relationRows[r.Type] = append(relationRows[r.Type], map[string]any{
			"from": r.From, "to": r.To, "status": r.Status, "source": r.Source,
			"support": r.Support, "support_docs": r.SupportDocs,
			"confidence": r.Confidence, "direction": r.Direction,
			"quote": r.Quote, "provision_id": r.ProvisionID,
		})
		return nil
	}); err != nil {
		return err
	}
	if err := eachNormConcept(in, func(normID, conceptID, role string) error {
		aboutConceptRows = append(aboutConceptRows, map[string]any{"from": normID, "to": conceptID, "role": role})
		return nil
	}); err != nil {
		return err
	}
	if err := eachEvent(in, func(e *temporal.Event) error {
		row := eventFields(e)
		row["id"] = e.ID
		eventRows = append(eventRows, row)
		return nil
	}); err != nil {
		return err
	}
	if err := eachTemporalVersion(in, func(v *temporal.Version) error {
		row := temporalVersionFields(v)
		row["id"] = v.ID
		temporalVersionRows = append(temporalVersionRows, row)
		return nil
	}); err != nil {
		return err
	}
	if err := eachTemporalEdge(in, func(from, to, relType string) error {
		temporalEdgeRows[relType] = append(temporalEdgeRows[relType], map[string]any{"from": from, "to": to})
		return nil
	}); err != nil {
		return err
	}

	var conflictRows, involvesRows []map[string]any
	if err := eachConflict(in, func(c conflictNode) error {
		conflictRows = append(conflictRows, c.fields())
		involvesRows = append(involvesRows,
			map[string]any{"from": c.ID, "to": c.NormA, "side": "a"},
			map[string]any{"from": c.ID, "to": c.NormB, "side": "b"})
		return nil
	}); err != nil {
		return err
	}

	var actRows, participantRows, aboutActRows []map[string]any
	chainRows := map[string][]map[string]any{}
	if err := eachAct(in, func(a actNode) error {
		actRows = append(actRows, a.fields())
		return nil
	}); err != nil {
		return err
	}
	if err := eachActChain(in, func(c chainEdge) error {
		chainRows[c.Type] = append(chainRows[c.Type], map[string]any{
			"from": c.From, "to": c.To, "status": c.Status, "why": c.Why,
			"support": c.Support, "support_docs": c.SupportDocs,
			"confidence": c.Confidence, "direction": c.Direction,
			"quote": c.Quote, "provision_id": c.ProvisionID,
		})
		return nil
	}); err != nil {
		return err
	}
	if err := eachActParticipant(in, func(p participantEdge) error {
		participantRows = append(participantRows, map[string]any{
			"from": p.ActID, "to": p.ConceptID, "role": p.Role,
			"as_written": p.AsWritten, "support": p.Support,
		})
		return nil
	}); err != nil {
		return err
	}
	if err := eachNormAct(in, func(l normActEdge) error {
		aboutActRows = append(aboutActRows, map[string]any{
			"from": l.NormID, "to": l.ActID, "slot": l.Slot, "provision_id": l.ProvisionID,
		})
		return nil
	}); err != nil {
		return err
	}

	for _, statement := range []string{
		"CREATE CONSTRAINT doc_id IF NOT EXISTS FOR (d:Document) REQUIRE d.id IS UNIQUE",
		"CREATE CONSTRAINT component_id IF NOT EXISTS FOR (c:Component) REQUIRE c.id IS UNIQUE",
		"CREATE CONSTRAINT version_id IF NOT EXISTS FOR (v:TextVersion) REQUIRE v.id IS UNIQUE",
		"CREATE CONSTRAINT term_id IF NOT EXISTS FOR (t:Term) REQUIRE t.id IS UNIQUE",
		"CREATE CONSTRAINT concept_id IF NOT EXISTS FOR (c:LegalConcept) REQUIRE c.id IS UNIQUE",
		"CREATE CONSTRAINT subject_id IF NOT EXISTS FOR (s:Subject) REQUIRE s.id IS UNIQUE",
		"CREATE CONSTRAINT norm_id IF NOT EXISTS FOR (n:Norm) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT condition_id IF NOT EXISTS FOR (n:Condition) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT exception_id IF NOT EXISTS FOR (n:Exception) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT sanction_id IF NOT EXISTS FOR (n:Sanction) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT term_use_id IF NOT EXISTS FOR (t:TermUse) REQUIRE t.id IS UNIQUE",
		"CREATE CONSTRAINT merged_concept_id IF NOT EXISTS FOR (c:Concept) REQUIRE c.id IS UNIQUE",
		"CREATE CONSTRAINT event_id IF NOT EXISTS FOR (e:" + EventLabel + ") REQUIRE e.id IS UNIQUE",
		"CREATE CONSTRAINT temporal_version_id IF NOT EXISTS FOR (v:" + TemporalVersionLabel + ") REQUIRE v.id IS UNIQUE",
		"CREATE CONSTRAINT conflict_id IF NOT EXISTS FOR (c:" + ConflictLabel + ") REQUIRE c.id IS UNIQUE",
		"CREATE CONSTRAINT act_id IF NOT EXISTS FOR (a:" + ActLabel + ") REQUIRE a.id IS UNIQUE",
	} {
		if _, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			return tx.Run(ctx, statement, nil)
		}); err != nil {
			return fmt.Errorf("apply constraint: %w", err)
		}
	}

	type step struct {
		query string
		rows  []map[string]any
	}
	// Every relationship step names the labels its endpoints carry, and the
	// reason is speed rather than tidiness. An unlabelled MATCH (a {id: r.from})
	// cannot use the uniqueness constraint, so the planner reads the whole node
	// store twice and takes the cartesian product, once per row. The fixture has
	// forty nodes and never noticed. The corpus has a hundred and thirty six
	// thousand, and the merge that found this had written eight thousand edges
	// in the time the labelled form takes to write all of them.
	//
	// Where an endpoint really can be either of two labels the pattern says so.
	// Neo4j plans a label disjunction as a union of two index seeks, so
	// Document|Component costs one extra seek rather than a scan. A container is
	// a document for a top level article and a component for a clause under one,
	// and a citation leaves from whichever of the two the extractor could pin
	// the reference to.
	steps := []step{
		{"UNWIND $rows AS r MERGE (d:Document {id: r.id}) SET d += r", docRows},
		// The alias label is set here as well as on the node created by a full
		// import, so a database built one way and topped up the other way carries
		// the same labels either way.
		{"UNWIND $rows AS r MERGE (c:Component {id: r.id}) SET c += r, c:" + law.ProvisionAlias, componentRows},
		{"UNWIND $rows AS r MERGE (v:TextVersion {id: r.id}) SET v += r", versionRows},
		{"UNWIND $rows AS r MATCH (a:Document|Component {id: r.from}), (b:Component {id: r.to}) MERGE (a)-[:CONTAINS]->(b)", containsRows},
		{"UNWIND $rows AS r MATCH (a:Component {id: r.from}), (b:TextVersion {id: r.to}) MERGE (a)-[:HAS_VERSION]->(b)", hasVersionRows},
		{"UNWIND $rows AS r MATCH (a:Document|Component {id: r.from}), (b:Document {id: r.to}) MERGE (a)-[c:CITES]->(b) SET c.method = r.method, c.snippet = r.snippet", citesRows},
		{"UNWIND $rows AS r MATCH (a:Document|Component {id: r.from}), (b:Document {id: r.to}) MERGE (a)-[c:AMENDS]->(b) SET c.method = r.method, c.snippet = r.snippet", amendsRows},
		{"UNWIND $rows AS r MERGE (t:Term {id: r.id}) SET t.text = r.text", termRows},
		{"UNWIND $rows AS r MERGE (c:LegalConcept {id: r.id}) SET c.label_vi = r.label_vi, c.parent = r.parent", conceptRows},
		{"UNWIND $rows AS r MATCH (a:Component {id: r.from}), (b:Term {id: r.to}) MERGE (a)-[d:DEFINES]->(b) SET d.connective = r.connective", definesRows},
		// A mention resolves to a term or to a registry class, and the linker
		// records which in the row it is not asked for here. Both labels are
		// listed rather than the step being split in two, because the two kinds
		// are one relationship type in every query that reads them.
		{"UNWIND $rows AS r MATCH (a:Component {id: r.from}), (b:Term|LegalConcept {id: r.to}) MERGE (a)-[m:MENTIONS]->(b) SET m.text = r.text, m.class_id = r.class_id, m.score = r.score, m.basis = r.basis", mentionsRows},
		{"UNWIND $rows AS r MERGE (s:Subject {id: r.id}) SET s += r", subjectRows},
		{"UNWIND $rows AS r MATCH (a:Subject {id: r.from}), (b:Subject {id: r.to}) MERGE (a)-[:BROADER]->(b)", broaderRows},
		{"UNWIND $rows AS r MATCH (a:Document {id: r.from}), (b:Subject {id: r.to}) MERGE (a)-[s:ABOUT_SUBJECT]->(b) SET s.confidence = r.confidence, s.method = r.method", aboutRows},
		{"UNWIND $rows AS r MERGE (n:Norm {id: r.id}) SET n += r", normRows},
	}
	// Detail node labels and norm relationship types are static per query, so
	// each group gets its own MERGE instead of a dynamic-label procedure.
	for _, label := range []string{"Condition", "Exception", "Sanction"} {
		steps = append(steps, step{"UNWIND $rows AS r MERGE (n:" + label + " {id: r.id}) SET n += r", detailRows[label]})
	}
	steps = append(steps, step{"UNWIND $rows AS r MATCH (a:Component {id: r.from}), (b:Norm {id: r.to}) MERGE (a)-[:HAS_NORM]->(b)", hasNormRows})
	// Each norm edge points at a different kind of thing, so the target label is
	// part of the table rather than something the loop can guess.
	for _, e := range []struct{ relType, to string }{
		{"HAS_LEGAL_BASIS", "Component"},
		{"HAS_BEARER", "LegalConcept"},
		{"HAS_COUNTERPARTY", "LegalConcept"},
		{"HAS_OBJECT", "LegalConcept"},
		{"HAS_CONDITION", "Condition"},
		{"HAS_EXCEPTION", "Exception"},
		{"HAS_SANCTION", "Sanction"},
	} {
		steps = append(steps, step{"UNWIND $rows AS r MATCH (a:Norm {id: r.from}), (b:" + e.to + " {id: r.to}) " +
			"MERGE (a)-[:" + e.relType + "]->(b)", normEdgeRows[e.relType]})
	}
	steps = append(steps,
		step{"UNWIND $rows AS r MERGE (t:TermUse {id: r.id}) SET t += r", termUseRows},
		step{"UNWIND $rows AS r MERGE (c:Concept {id: r.id}) SET c += r", mergedConceptRows},
		// The decision properties are set on the edge rather than the node
		// because the edge is the merge. Who joined these two readings and why
		// is the fact worth keeping, and it belongs where the join is.
		step{"UNWIND $rows AS r MATCH (a:TermUse {id: r.from}), (b:Concept {id: r.to}) " +
			"MERGE (a)-[e:INSTANCE_OF]->(b) " +
			"SET e.relation = r.relation, e.decided_by = r.decided_by, e.decided_at = r.decided_at, e.rationale = r.rationale",
			instanceOfRows},
		step{"UNWIND $rows AS r MATCH (a:TermUse {id: r.from}), (b:TermUse {id: r.to}) " +
			"MERGE (a)-[e:DIFFERS_FROM]->(b) " +
			"SET e.decided_by = r.decided_by, e.decided_at = r.decided_at, e.rationale = r.rationale, e.basis = r.basis",
			differsFromRows},
	)
	// A term use is defined by a provision and scoped to an instrument, and the
	// layer accepts a document at either end because a definition article
	// sometimes scopes to the whole law rather than to a clause of it.
	for _, e := range []struct{ relType, from, to string }{
		{"DEFINES_TERM", "Document|Component", "TermUse"},
		{"IN_SCOPE", "TermUse", "Document|Component"},
		{"REFERS_TO", "TermUse", "TermUse"},
	} {
		steps = append(steps, step{"UNWIND $rows AS r MATCH (a:" + e.from + " {id: r.from}), (b:" + e.to + " {id: r.to}) " +
			"MERGE (a)-[:" + e.relType + "]->(b)", termUseEdgeRows[e.relType]})
	}
	steps = append(steps,
		step{"UNWIND $rows AS r MERGE (e:" + EventLabel + " {id: r.id}) SET e += r", eventRows},
		step{"UNWIND $rows AS r MERGE (v:" + TemporalVersionLabel + " {id: r.id}) SET v += r", temporalVersionRows},
	)
	// A concept relation is matched between Concept nodes on both ends. Leaving
	// the labels off would let an identifier that happens to be shared with
	// another layer attach a REQUIRES edge to something that is not a concept,
	// which is the sort of thing a merge does quietly and a reader never finds.
	for _, relType := range RelationTypes() {
		steps = append(steps, step{"UNWIND $rows AS r MATCH (a:Concept {id: r.from}), (b:Concept {id: r.to}) " +
			"MERGE (a)-[e:" + relType + "]->(b) " +
			"SET e.status = r.status, e.source = r.source, e.support = r.support, e.support_docs = r.support_docs, " +
			"e.confidence = r.confidence, e.direction = r.direction, e.quote = r.quote, e.provision_id = r.provision_id",
			relationRows[relType]})
	}
	// The role is part of the merge key. A norm whose bearer and whose object
	// resolve to the same concept holds two different claims about it, and one
	// edge keyed on the pair alone would keep whichever role was written last.
	steps = append(steps, step{"UNWIND $rows AS r MATCH (a:Norm {id: r.from}), (b:Concept {id: r.to}) " +
		"MERGE (a)-[:" + AboutConcept + " {role: r.role}]->(b)", aboutConceptRows})
	for _, e := range []struct{ relType, from, to string }{
		{HasTemporalVersion, "Component", TemporalVersionLabel},
		{Includes, TemporalVersionLabel, TemporalVersionLabel},
		{CausedBy, EventLabel, "Document"},
		{Terminates, EventLabel, TemporalVersionLabel},
		{ProducesVersion, EventLabel, TemporalVersionLabel},
	} {
		steps = append(steps, step{"UNWIND $rows AS r MATCH (a:" + e.from + " {id: r.from}), (b:" + e.to + " {id: r.to}) " +
			"MERGE (a)-[:" + e.relType + "]->(b)", temporalEdgeRows[e.relType]})
	}
	// The side is a property of the edge and not part of its merge key. A
	// conflict has exactly two norms in it and which one is called a is decided
	// by sorting, so keying on the side would let a rerun that sorted the pair
	// the other way write four edges where there are two.
	steps = append(steps,
		step{"UNWIND $rows AS r MERGE (c:" + ConflictLabel + " {id: r.id}) SET c += r", conflictRows},
		step{"UNWIND $rows AS r MATCH (a:" + ConflictLabel + " {id: r.from}), (b:Norm {id: r.to}) " +
			"MERGE (a)-[e:" + Involves + "]->(b) SET e.side = r.side", involvesRows},
		step{"UNWIND $rows AS r MERGE (a:" + ActLabel + " {id: r.id}) SET a += r", actRows},
		// The role is part of the merge key, for the reason ABOUT_CONCEPT gives.
		// A body that both grants a licence and is notified about it holds two
		// claims, and one edge keyed on the pair alone would keep the last.
		step{"UNWIND $rows AS r MATCH (a:" + ActLabel + " {id: r.from}), (b:Concept {id: r.to}) " +
			"MERGE (a)-[e:" + HasParticipant + " {role: r.role}]->(b) " +
			"SET e.as_written = r.as_written, e.support = r.support", participantRows},
		// The slot is part of the key as well. A provision that requires a filing
		// and fines the failure to file names one act in both slots, and folding
		// the two into one edge would lose which of them the penalty hangs off,
		// which is the whole basis of question 25.
		step{"UNWIND $rows AS r MATCH (a:Norm {id: r.from}), (b:" + ActLabel + " {id: r.to}) " +
			"MERGE (a)-[e:" + AboutAct + " {slot: r.slot}]->(b) SET e.provision_id = r.provision_id", aboutActRows},
	)
	// One statement per chain type, the same way the concept relations are
	// written, and with both labels named so the merge uses the constraint rather
	// than reading every node twice per row.
	for _, relType := range ActChainTypes() {
		steps = append(steps, step{"UNWIND $rows AS r MATCH (a:" + ActLabel + " {id: r.from}), (b:" + ActLabel + " {id: r.to}) " +
			"MERGE (a)-[e:" + relType + "]->(b) " +
			"SET e.status = r.status, e.why = r.why, e.support = r.support, e.support_docs = r.support_docs, " +
			"e.confidence = r.confidence, e.direction = r.direction, e.quote = r.quote, e.provision_id = r.provision_id",
			chainRows[relType]})
	}
	for _, step := range steps {
		if err := run(step.query, step.rows); err != nil {
			return fmt.Errorf("merge: %w", err)
		}
	}
	return nil
}
