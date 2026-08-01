package graph

import (
	"context"
	"fmt"
	"os"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/subject"
)

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
		s := &r.Statement
		subject, object := "", ""
		if s.Subject != nil {
			subject = s.Subject.Text
		}
		if s.Object != nil {
			object = s.Object.Text
		}
		verdict := ""
		if r.Entailment != nil {
			verdict = r.Entailment.Verdict
		}
		normRows = append(normRows, map[string]any{
			"id": r.ID, "norm_type": s.Type, "modality": s.Modality,
			"subject": subject, "action": s.Action.Text, "object": object,
			"deadline": s.Deadline, "sanction": s.Sanction,
			"evidence_quote": s.Evidence.Quote, "evidence_start": s.Evidence.Start,
			"evidence_end": s.Evidence.End, "confidence": s.Confidence,
			"verdict": verdict, "model": r.Model, "ontology_version": r.OntologyVersion,
		})
		hasNormRows = append(hasNormRows, map[string]any{"from": r.ProvisionID, "to": r.ID})
		normEdgeRows["HAS_LEGAL_BASIS"] = append(normEdgeRows["HAS_LEGAL_BASIS"], map[string]any{"from": r.ID, "to": r.ProvisionID})
		if s.Subject != nil && s.Subject.ClassID != "" {
			normEdgeRows["HAS_BEARER"] = append(normEdgeRows["HAS_BEARER"], map[string]any{"from": r.ID, "to": s.Subject.ClassID})
		}
		if s.Object != nil && s.Object.ClassID != "" {
			normEdgeRows["HAS_OBJECT"] = append(normEdgeRows["HAS_OBJECT"], map[string]any{"from": r.ID, "to": s.Object.ClassID})
		}
	}
	if err := eachNormDetail(in.Statements, func(id, text, label, normID, relType string) error {
		detailRows[label] = append(detailRows[label], map[string]any{"id": id, "text": text})
		normEdgeRows[relType] = append(normEdgeRows[relType], map[string]any{"from": normID, "to": id})
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
		steps = append(steps, step{"UNWIND $rows AS r MERGE (n:" + label + " {id: r.id}) SET n.text = r.text", detailRows[label]})
	}
	steps = append(steps, step{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[:HAS_NORM]->(b)", hasNormRows})
	for _, relType := range []string{"HAS_LEGAL_BASIS", "HAS_BEARER", "HAS_OBJECT", "HAS_CONDITION", "HAS_EXCEPTION", "HAS_SANCTION"} {
		steps = append(steps, step{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[:" + relType + "]->(b)", normEdgeRows[relType]})
	}
	for _, step := range steps {
		if err := run(step.query, step.rows); err != nil {
			return fmt.Errorf("merge: %w", err)
		}
	}
	return nil
}
