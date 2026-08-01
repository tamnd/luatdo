package graph

import (
	"context"
	"fmt"
	"os"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
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

	var docRows, provRows, containsRows, citesRows, amendsRows []map[string]any
	for _, d := range docs {
		docRows = append(docRows, map[string]any{
			"id": d.ID, "official_number": d.OfficialNumber, "title": d.Title,
			"title_en": d.TitleEN, "doc_type": d.DocType, "effective_from": d.EffectiveFrom,
			"source": d.Source, "source_url": d.SourceURL, "status": d.Status,
		})
		for i := range d.Provisions {
			p := &d.Provisions[i]
			provRows = append(provRows, map[string]any{
				"id": p.ID, "kind": p.Kind, "number": p.Number,
				"heading": p.Heading, "text": p.Text, "position": p.Position,
			})
			parent := p.ParentID
			if parent == "" {
				parent = d.ID
			}
			containsRows = append(containsRows, map[string]any{"from": parent, "to": p.ID})
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

	for _, statement := range []string{
		"CREATE CONSTRAINT doc_id IF NOT EXISTS FOR (d:Document) REQUIRE d.id IS UNIQUE",
		"CREATE CONSTRAINT prov_id IF NOT EXISTS FOR (p:Provision) REQUIRE p.id IS UNIQUE",
		"CREATE CONSTRAINT term_id IF NOT EXISTS FOR (t:Term) REQUIRE t.id IS UNIQUE",
		"CREATE CONSTRAINT concept_id IF NOT EXISTS FOR (c:LegalConcept) REQUIRE c.id IS UNIQUE",
	} {
		if _, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			return tx.Run(ctx, statement, nil)
		}); err != nil {
			return fmt.Errorf("apply constraint: %w", err)
		}
	}

	steps := []struct {
		query string
		rows  []map[string]any
	}{
		{"UNWIND $rows AS r MERGE (d:Document {id: r.id}) SET d += r", docRows},
		{"UNWIND $rows AS r MERGE (p:Provision {id: r.id}) SET p += r", provRows},
		{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[:CONTAINS]->(b)", containsRows},
		{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[c:CITES]->(b) SET c.method = r.method, c.snippet = r.snippet", citesRows},
		{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[c:AMENDS]->(b) SET c.method = r.method, c.snippet = r.snippet", amendsRows},
		{"UNWIND $rows AS r MERGE (t:Term {id: r.id}) SET t.text = r.text", termRows},
		{"UNWIND $rows AS r MERGE (c:LegalConcept {id: r.id}) SET c.label_vi = r.label_vi, c.parent = r.parent", conceptRows},
		{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[d:DEFINES]->(b) SET d.connective = r.connective", definesRows},
		{"UNWIND $rows AS r MATCH (a {id: r.from}), (b {id: r.to}) MERGE (a)-[m:MENTIONS]->(b) SET m.text = r.text, m.class_id = r.class_id, m.score = r.score, m.basis = r.basis", mentionsRows},
	}
	for _, step := range steps {
		if err := run(step.query, step.rows); err != nil {
			return fmt.Errorf("merge: %w", err)
		}
	}
	return nil
}
