// Package graph projects the canonical store into Neo4j.
//
// Neo4j is a projection, never the source of truth. A full export writes the
// CSV files and scripts for neo4j-admin database import, which is the fast
// path for a fresh database. An incremental export merges over Bolt and is
// idempotent, so it can run after every pipeline pass.
package graph

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/link"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/subject"
	"github.com/tamnd/luatdo/term"
)

// Schema is applied with cypher-shell after the imported database starts.
//
// The fulltext index over the text keeps the name provision_text even though
// the text now hangs off TextVersion. An index is addressed by name in every
// query that uses it, so renaming it would break the callers the Provision
// alias label exists to keep working, and both go in the same release.
const Schema = `CREATE CONSTRAINT doc_id IF NOT EXISTS FOR (d:Document) REQUIRE d.id IS UNIQUE;
CREATE CONSTRAINT component_id IF NOT EXISTS FOR (c:Component) REQUIRE c.id IS UNIQUE;
CREATE CONSTRAINT version_id IF NOT EXISTS FOR (v:TextVersion) REQUIRE v.id IS UNIQUE;
CREATE CONSTRAINT term_id IF NOT EXISTS FOR (t:Term) REQUIRE t.id IS UNIQUE;
CREATE CONSTRAINT concept_id IF NOT EXISTS FOR (c:LegalConcept) REQUIRE c.id IS UNIQUE;
CREATE CONSTRAINT subject_id IF NOT EXISTS FOR (s:Subject) REQUIRE s.id IS UNIQUE;
CREATE FULLTEXT INDEX provision_text IF NOT EXISTS FOR (v:TextVersion) ON EACH [v.text];
CREATE FULLTEXT INDEX component_heading IF NOT EXISTS FOR (c:Component) ON EACH [c.heading];
CREATE FULLTEXT INDEX document_title IF NOT EXISTS FOR (d:Document) ON EACH [d.title, d.title_en];
CREATE FULLTEXT INDEX term_text IF NOT EXISTS FOR (t:Term) ON EACH [t.text];
CREATE CONSTRAINT norm_id IF NOT EXISTS FOR (n:Norm) REQUIRE n.id IS UNIQUE;
CREATE INDEX norm_type IF NOT EXISTS FOR (n:Norm) ON (n.norm_type);
CREATE INDEX doc_effective IF NOT EXISTS FOR (d:Document) ON (d.effective_from);
CREATE INDEX version_from IF NOT EXISTS FOR (v:TextVersion) ON (v.from_date);
`

// componentLabels is what a component node carries. law.ProvisionAlias rides
// along until the release named beside it, so a query written against the
// earlier projection still matches.
const componentLabels = "Component;" + law.ProvisionAlias

// Input is everything the projection is built from. Registry, Definitions,
// Mentions, Statements, and the subject pair are optional; a corpus without
// the semantic layer projects the document graph alone.
type Input struct {
	Docs        []*law.Document
	Links       []cite.Link
	Registry    *ontology.Registry
	Definitions []term.Definition
	Mentions    []link.Resolution
	Statements  []norm.Record
	// Vocabulary and Subjects go together. The vocabulary supplies the nodes
	// and the records supply the edges, so a projection given the records
	// without the vocabulary would hang edges off subjects that do not exist.
	Vocabulary *subject.Vocabulary
	Subjects   []subject.Record
}

// Export writes the CSV projection into dir.
func Export(dir string, in Input) error {
	docs, links := in.Docs, in.Links
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// An export directory is read as a whole, and a file an earlier export wrote
	// looks as current as the ones this one writes. Leaving provisions.csv beside
	// components.csv would give a reader two answers to the same question, one of
	// them from before the split, so the retired file goes.
	for _, name := range []string{"provisions.csv"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := writeCSV(filepath.Join(dir, "documents.csv"),
		[]string{"id:ID", "official_number", "issuing_body", "title", "title_en", "doc_type", "effective_from", "source", "source_url", "status", ":LABEL"},
		func(w *csv.Writer) error {
			for _, d := range docs {
				if err := w.Write([]string{d.ID, d.OfficialNumber, d.IssuingBody, d.Title, d.TitleEN, d.DocType, d.EffectiveFrom, d.Source, d.SourceURL, d.Status, "Document"}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "components.csv"),
		[]string{"id:ID", "kind", "number", "heading", "position:int", "renumbered_from", ":LABEL"},
		func(w *csv.Writer) error {
			return eachComponent(docs, func(c *law.Component) error {
				return w.Write([]string{c.ID, c.Kind, c.Number, c.Heading, strconv.Itoa(c.Position), c.RenumberedFrom, componentLabels})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "text_versions.csv"),
		[]string{"id:ID", "text", "text_hash", "from_date", "to_date", ":LABEL"},
		func(w *csv.Writer) error {
			return eachVersion(docs, func(v *law.TextVersion) error {
				return w.Write([]string{v.ID, v.Text, v.TextHash, v.FromDate, v.ToDate, "TextVersion"})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "has_version.csv"),
		[]string{":START_ID", ":END_ID", ":TYPE"},
		func(w *csv.Writer) error {
			return eachVersion(docs, func(v *law.TextVersion) error {
				return w.Write([]string{v.ComponentID, v.ID, "HAS_VERSION"})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "contains.csv"),
		[]string{":START_ID", ":END_ID", ":TYPE"},
		func(w *csv.Writer) error {
			for _, d := range docs {
				for i := range d.Provisions {
					p := &d.Provisions[i]
					parent := p.ParentID
					if parent == "" {
						parent = d.ID
					}
					if err := w.Write([]string{parent, p.ID, "CONTAINS"}); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "cites.csv"),
		[]string{":START_ID", ":END_ID", "method", "snippet", ":TYPE"},
		func(w *csv.Writer) error {
			for _, l := range links {
				if l.ToDoc == "" {
					continue
				}
				relType := "CITES"
				if l.Kind == "amends" {
					relType = "AMENDS"
				}
				from := l.FromProvision
				if from == "" {
					from = l.FromDoc
				}
				if err := w.Write([]string{from, l.ToDoc, l.Method, l.Snippet, relType}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "terms.csv"),
		[]string{"id:ID", "text", ":LABEL"},
		func(w *csv.Writer) error {
			seen := map[string]bool{}
			for _, d := range in.Definitions {
				if seen[d.TermID] {
					continue
				}
				seen[d.TermID] = true
				if err := w.Write([]string{d.TermID, d.Term, "Term"}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "concepts.csv"),
		[]string{"id:ID", "label_vi", "parent", ":LABEL"},
		func(w *csv.Writer) error {
			if in.Registry == nil {
				return nil
			}
			for _, c := range in.Registry.Classes {
				if err := w.Write([]string{c.ID, c.LabelVI, c.Parent, "LegalConcept"}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "subjects.csv"),
		[]string{"id:ID", "label_vi", "label_en", "parent", ":LABEL"},
		func(w *csv.Writer) error {
			if in.Vocabulary == nil {
				return nil
			}
			for _, s := range in.Vocabulary.Subjects {
				if err := w.Write([]string{s.ID, s.LabelVI, s.LabelEN, s.Parent, "Subject"}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "subject_parents.csv"),
		[]string{":START_ID", ":END_ID", ":TYPE"},
		func(w *csv.Writer) error {
			if in.Vocabulary == nil {
				return nil
			}
			for _, s := range in.Vocabulary.Subjects {
				if s.Parent == "" {
					continue
				}
				if err := w.Write([]string{s.ID, s.Parent, "BROADER"}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "about_subject.csv"),
		[]string{":START_ID", ":END_ID", "confidence:float", "method", ":TYPE"},
		func(w *csv.Writer) error {
			return eachAssignment(in, func(docID string, a *subject.Assignment) error {
				confidence := strconv.FormatFloat(a.Confidence, 'f', 2, 64)
				return w.Write([]string{docID, a.SubjectID, confidence, a.Method, "ABOUT_SUBJECT"})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "defines.csv"),
		[]string{":START_ID", ":END_ID", "connective", ":TYPE"},
		func(w *csv.Writer) error {
			for _, d := range in.Definitions {
				if err := w.Write([]string{d.ProvisionID, d.TermID, d.Connective, "DEFINES"}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "mentions.csv"),
		[]string{":START_ID", ":END_ID", "text", "class_id", "score:float", "basis", ":TYPE"},
		func(w *csv.Writer) error {
			for _, m := range in.Mentions {
				if m.TargetKind == "unresolved" || m.TargetID == "" {
					continue
				}
				score := strconv.FormatFloat(m.Score, 'f', 2, 64)
				if err := w.Write([]string{m.ProvisionID, m.TargetID, m.Text, m.ClassID, score, m.Basis, "MENTIONS"}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "norms.csv"),
		[]string{"id:ID", "norm_type", "modality", "subject", "action", "object", "deadline", "sanction",
			"evidence_quote", "evidence_start:int", "evidence_end:int", "confidence:float", "verdict",
			"model", "ontology_version:int", ":LABEL"},
		func(w *csv.Writer) error {
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
				if err := w.Write([]string{r.ID, s.Type, s.Modality, subject, s.Action.Text, object,
					s.Deadline, s.Sanction, s.Evidence.Quote, strconv.Itoa(s.Evidence.Start),
					strconv.Itoa(s.Evidence.End), strconv.FormatFloat(s.Confidence, 'f', 2, 64),
					verdict, r.Model, strconv.Itoa(r.OntologyVersion), "Norm"}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "norm_details.csv"),
		[]string{"id:ID", "text", ":LABEL"},
		func(w *csv.Writer) error {
			return eachNormDetail(in.Statements, func(id, text, label, _, _ string) error {
				return w.Write([]string{id, text, label})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "has_norm.csv"),
		[]string{":START_ID", ":END_ID", ":TYPE"},
		func(w *csv.Writer) error {
			for i := range in.Statements {
				r := &in.Statements[i]
				if err := w.Write([]string{r.ProvisionID, r.ID, "HAS_NORM"}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "norm_edges.csv"),
		[]string{":START_ID", ":END_ID", ":TYPE"},
		func(w *csv.Writer) error {
			for i := range in.Statements {
				r := &in.Statements[i]
				s := &r.Statement
				if s.Subject != nil && s.Subject.ClassID != "" {
					if err := w.Write([]string{r.ID, s.Subject.ClassID, "HAS_BEARER"}); err != nil {
						return err
					}
				}
				if s.Object != nil && s.Object.ClassID != "" {
					if err := w.Write([]string{r.ID, s.Object.ClassID, "HAS_OBJECT"}); err != nil {
						return err
					}
				}
				if err := w.Write([]string{r.ID, r.ProvisionID, "HAS_LEGAL_BASIS"}); err != nil {
					return err
				}
			}
			return eachNormDetail(in.Statements, func(id, _, _, normID, relType string) error {
				return w.Write([]string{normID, id, relType})
			})
		}); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "schema.cypher"), []byte(Schema), 0o644); err != nil {
		return err
	}
	return writeImportScripts(dir)
}

// eachComponent and eachVersion walk the split form of the corpus. Both call
// law.Split rather than reading the provisions directly, so the projection and
// the split cannot disagree about which provisions earn a version.
func eachComponent(docs []*law.Document, visit func(*law.Component) error) error {
	for _, d := range docs {
		components, _ := law.Split(d)
		for i := range components {
			if err := visit(&components[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func eachVersion(docs []*law.Document, visit func(*law.TextVersion) error) error {
	for _, d := range docs {
		_, versions := law.Split(d)
		for i := range versions {
			if err := visit(&versions[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// eachAssignment visits every subject a document was filed under, skipping any
// subject the vocabulary does not hold. An assignment file written against an
// older vocabulary would otherwise point an edge at a node that is not in the
// import, and neo4j-admin refuses the whole import over one such row.
func eachAssignment(in Input, visit func(docID string, a *subject.Assignment) error) error {
	if in.Vocabulary == nil {
		return nil
	}
	for i := range in.Subjects {
		r := &in.Subjects[i]
		for j := range r.Subjects {
			a := &r.Subjects[j]
			if in.Vocabulary.Get(a.SubjectID) == nil {
				continue
			}
			if err := visit(r.DocID, a); err != nil {
				return err
			}
		}
	}
	return nil
}

// eachNormDetail visits every condition, exception, and sanction node of the
// statement set. Detail node IDs hang off the norm ID, so they are as
// deterministic as everything else.
func eachNormDetail(records []norm.Record, visit func(id, text, label, normID, relType string) error) error {
	for i := range records {
		r := &records[i]
		for j, c := range r.Statement.Conditions {
			id := fmt.Sprintf("%s:condition-%d", r.ID, j+1)
			if err := visit(id, c, "Condition", r.ID, "HAS_CONDITION"); err != nil {
				return err
			}
		}
		for j, e := range r.Statement.Exceptions {
			id := fmt.Sprintf("%s:exception-%d", r.ID, j+1)
			if err := visit(id, e, "Exception", r.ID, "HAS_EXCEPTION"); err != nil {
				return err
			}
		}
		if r.Statement.Sanction != "" {
			id := r.ID + ":sanction"
			if err := visit(id, r.Statement.Sanction, "Sanction", r.ID, "HAS_SANCTION"); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeCSV(path string, header []string, body func(*csv.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return err
	}
	if err := body(w); err != nil {
		return err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return f.Close()
}

// writeImportScripts writes the neo4j-admin invocation for both shells, so
// the export directory works on the Linux servers and the Windows machine.
func writeImportScripts(dir string) error {
	args := `database import full luatdo --overwrite-destination ` +
		`--nodes=documents.csv --nodes=components.csv --nodes=text_versions.csv ` +
		`--nodes=terms.csv --nodes=concepts.csv --nodes=subjects.csv ` +
		`--nodes=norms.csv --nodes=norm_details.csv ` +
		`--relationships=contains.csv --relationships=has_version.csv --relationships=cites.csv ` +
		`--relationships=defines.csv --relationships=mentions.csv ` +
		`--relationships=subject_parents.csv --relationships=about_subject.csv ` +
		`--relationships=has_norm.csv --relationships=norm_edges.csv`
	sh := "#!/bin/sh\n# Run from this directory with the database stopped.\nneo4j-admin " + args + "\n"
	cmd := "@echo off\r\nrem Run from this directory with the database stopped.\r\nneo4j-admin " + args + "\r\n"
	if err := os.WriteFile(filepath.Join(dir, "import.sh"), []byte(sh), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "import.cmd"), []byte(cmd), 0o644)
}

// Summary says what an export contained.
type Summary struct {
	Documents    int `json:"documents"`
	Components   int `json:"components"`
	TextVersions int `json:"text_versions"`
	Contains     int `json:"contains"`
	Cites        int `json:"cites"`
	Unresolved   int `json:"unresolved"`
	Terms        int `json:"terms"`
	Defines      int `json:"defines"`
	Concepts     int `json:"concepts"`
	Mentions     int `json:"mentions"`
	Subjects     int `json:"subjects"`
	AboutSubject int `json:"about_subject"`
	Norms        int `json:"norms"`
}

// Summarize counts the projection without writing it.
func Summarize(in Input) Summary {
	s := Summary{Documents: len(in.Docs)}
	_ = eachComponent(in.Docs, func(*law.Component) error {
		s.Components++
		s.Contains++
		return nil
	})
	_ = eachVersion(in.Docs, func(*law.TextVersion) error {
		s.TextVersions++
		return nil
	})
	if in.Vocabulary != nil {
		s.Subjects = len(in.Vocabulary.Subjects)
	}
	_ = eachAssignment(in, func(string, *subject.Assignment) error {
		s.AboutSubject++
		return nil
	})
	for _, l := range in.Links {
		if l.ToDoc == "" {
			s.Unresolved++
		} else {
			s.Cites++
		}
	}
	terms := map[string]bool{}
	for _, d := range in.Definitions {
		terms[d.TermID] = true
		s.Defines++
	}
	s.Terms = len(terms)
	if in.Registry != nil {
		s.Concepts = len(in.Registry.Classes)
	}
	for _, m := range in.Mentions {
		if m.TargetKind != "unresolved" && m.TargetID != "" {
			s.Mentions++
		}
	}
	s.Norms = len(in.Statements)
	return s
}

func (s Summary) String() string {
	out := fmt.Sprintf("documents %d, components %d, versions %d, contains %d, cites %d, unresolved %d",
		s.Documents, s.Components, s.TextVersions, s.Contains, s.Cites, s.Unresolved)
	if s.Terms > 0 {
		out += fmt.Sprintf(", terms %d, defines %d", s.Terms, s.Defines)
	}
	if s.Concepts > 0 {
		out += fmt.Sprintf(", concepts %d", s.Concepts)
	}
	if s.Subjects > 0 {
		out += fmt.Sprintf(", subjects %d, about %d", s.Subjects, s.AboutSubject)
	}
	if s.Mentions > 0 {
		out += fmt.Sprintf(", mentions %d", s.Mentions)
	}
	if s.Norms > 0 {
		out += fmt.Sprintf(", norms %d", s.Norms)
	}
	return out
}
