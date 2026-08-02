package graph

import (
	"context"
	"fmt"
	"os"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/subject"
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
		docRows = append(docRows, map[string]any{
			"id": d.ID, "official_number": d.OfficialNumber, "issuing_body": d.IssuingBody,
			"title":    d.Title,
			"title_en": d.TitleEN, "doc_type": d.DocType, "effective_from": d.EffectiveFrom,
			"source": d.Source, "source_url": d.SourceURL, "status": d.Status,
		})
		components, versions := law.Split(d)
		for i := range components {
			c := &components[i]
			componentRows = append(componentRows, map[string]any{
				"id": c.ID, "kind": c.Kind, "number": c.Number,
				"heading": c.Heading, "position": c.Position, "renumbered_from": c.RenumberedFrom,
			})
			parent := c.ParentID
			if parent == "" {
				parent = d.ID
			}
			containsRows = append(containsRows, map[string]any{"from": parent, "to": c.ID})
		}
		for i := range versions {
			v := &versions[i]
			versionRows = append(versionRows, map[string]any{
				"id": v.ID, "text": v.Text, "text_hash": v.TextHash,
				"from_date": v.FromDate, "to_date": v.ToDate,
			})
			hasVersionRows = append(hasVersionRows, map[string]any{"from": v.ComponentID, "to": v.ID})
		}
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
	for i := range in.Statements {
		r := &in.Statements[i]
		row := normFields(r)
		row["id"] = r.ID
		normRows = append(normRows, row)
		hasNormRows = append(hasNormRows, map[string]any{"from": r.ProvisionID, "to": r.ID})
		normEdgeRows["HAS_LEGAL_BASIS"] = append(normEdgeRows["HAS_LEGAL_BASIS"], map[string]any{"from": r.ID, "to": r.ProvisionID})
		if err := eachNormClass(r, func(classID, relType string) error {
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

	for _, statement := range []string{
		"CREATE CONSTRAINT doc_id IF NOT EXISTS FOR (d:Document) REQUIRE d.id IS UNIQUE",
		"CREATE CONSTRAINT component_id IF NOT EXISTS FOR (c:Component) REQUIRE c.id IS UNIQUE",
		"CREATE CONSTRAINT version_id IF NOT EXISTS FOR (v:TextVersion) REQUIRE v.id IS UNIQUE",
		"CREATE CONSTRAINT term_id IF NOT EXISTS FOR (t:Term) REQUIRE t.id IS UNIQUE",
		"CREATE CONSTRAINT concept_id IF NOT EXISTS FOR (c:LegalConcept) REQUIRE c.id IS UNIQUE",
		"CREATE CONSTRAINT subject_id IF NOT EXISTS FOR (s:Subject) REQUIRE s.id IS UNIQUE",
		"CREATE CONSTRAINT norm_id IF NOT EXISTS FOR (n:Norm) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT term_use_id IF NOT EXISTS FOR (t:TermUse) REQUIRE t.id IS UNIQUE",
		"CREATE CONSTRAINT merged_concept_id IF NOT EXISTS FOR (c:Concept) REQUIRE c.id IS UNIQUE",
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
	steps := []step{
		{"UNWIND $rows AS r MERGE (d:Document {id: r.id}) SET d += r", docRows},
		// The alias label is set here as well as on the node created by a full
		// import, so a database built one way and topped up the other way carries
		// the same labels either way.
		{"UNWIND $rows AS r MERGE (c:Component {id: r.id}) SET c += r, c:" + law.ProvisionAlias, componentRows},
		{"UNWIND $rows AS r MERGE (v:TextVersion {id: r.id}) SET v += r", versionRows},
		{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[:CONTAINS]->(b)", containsRows},
		{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[:HAS_VERSION]->(b)", hasVersionRows},
		{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[c:CITES]->(b) SET c.method = r.method, c.snippet = r.snippet", citesRows},
		{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[c:AMENDS]->(b) SET c.method = r.method, c.snippet = r.snippet", amendsRows},
		{"UNWIND $rows AS r MERGE (t:Term {id: r.id}) SET t.text = r.text", termRows},
		{"UNWIND $rows AS r MERGE (c:LegalConcept {id: r.id}) SET c.label_vi = r.label_vi, c.parent = r.parent", conceptRows},
		{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[d:DEFINES]->(b) SET d.connective = r.connective", definesRows},
		{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[m:MENTIONS]->(b) SET m.text = r.text, m.class_id = r.class_id, m.score = r.score, m.basis = r.basis", mentionsRows},
		{"UNWIND $rows AS r MERGE (s:Subject {id: r.id}) SET s += r", subjectRows},
		{"UNWIND $rows AS r MATCH (a:Subject {id: r.from}), (b:Subject {id: r.to}) MERGE (a)-[:BROADER]->(b)", broaderRows},
		{"UNWIND $rows AS r MATCH (a {id: r.from}), (b:Subject {id: r.to}) MERGE (a)-[s:ABOUT_SUBJECT]->(b) SET s.confidence = r.confidence, s.method = r.method", aboutRows},
		{"UNWIND $rows AS r MERGE (n:Norm {id: r.id}) SET n += r", normRows},
	}
	// Detail node labels and norm relationship types are static per query, so
	// each group gets its own MERGE instead of a dynamic-label procedure.
	for _, label := range []string{"Condition", "Exception", "Sanction"} {
		steps = append(steps, step{"UNWIND $rows AS r MERGE (n:" + label + " {id: r.id}) SET n += r", detailRows[label]})
	}
	steps = append(steps, step{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[:HAS_NORM]->(b)", hasNormRows})
	for _, relType := range []string{"HAS_LEGAL_BASIS", "HAS_BEARER", "HAS_COUNTERPARTY", "HAS_OBJECT", "HAS_CONDITION", "HAS_EXCEPTION", "HAS_SANCTION"} {
		steps = append(steps, step{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[:" + relType + "]->(b)", normEdgeRows[relType]})
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
	for _, relType := range []string{"DEFINES_TERM", "IN_SCOPE", "REFERS_TO"} {
		steps = append(steps, step{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[:" + relType + "]->(b)", termUseEdgeRows[relType]})
	}
	for _, step := range steps {
		if err := run(step.query, step.rows); err != nil {
			return fmt.Errorf("merge: %w", err)
		}
	}
	return nil
}
