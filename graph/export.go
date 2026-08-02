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
	"strings"

	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/concept"
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
CREATE CONSTRAINT term_use_id IF NOT EXISTS FOR (t:TermUse) REQUIRE t.id IS UNIQUE;
CREATE CONSTRAINT merged_concept_id IF NOT EXISTS FOR (c:Concept) REQUIRE c.id IS UNIQUE;
CREATE INDEX norm_type IF NOT EXISTS FOR (n:Norm) ON (n.norm_type);
CREATE INDEX norm_deadline IF NOT EXISTS FOR (n:Norm) ON (n.deadline_value, n.deadline_calendar);
CREATE INDEX norm_procedure IF NOT EXISTS FOR (n:Norm) ON (n.procedure_id);
CREATE INDEX term_use_kind IF NOT EXISTS FOR (t:TermUse) ON (t.kind);
CREATE INDEX term_use_role IF NOT EXISTS FOR (t:TermUse) ON (t.is_role);
CREATE INDEX doc_effective IF NOT EXISTS FOR (d:Document) ON (d.effective_from);
CREATE INDEX version_from IF NOT EXISTS FOR (v:TextVersion) ON (v.from_date);
CREATE FULLTEXT INDEX term_use_text IF NOT EXISTS FOR (t:TermUse) ON EACH [t.label_vi, t.definition_vi];
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
	// Layer is the concept layer: terms as each instrument uses them, the
	// corpus wide concepts a person merged them into, and the differences a
	// person recorded instead of merging. It is separate from Registry, which
	// holds the designed vocabulary. Registry says what kinds of thing exist,
	// the layer says which ones the corpus actually names.
	Layer *concept.Layer
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
	if err := writeCSV(filepath.Join(dir, "norms.csv"), normHeader(),
		func(w *csv.Writer) error {
			for i := range in.Statements {
				r := &in.Statements[i]
				row := append([]string{r.ID}, normValues(r)...)
				if err := w.Write(append(row, "Norm")); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "norm_details.csv"),
		[]string{"id:ID", "text", "kind", "quote", "legal_basis", ":LABEL"},
		func(w *csv.Writer) error {
			return eachNormDetail(in.Statements, func(d normDetail) error {
				return w.Write([]string{d.ID, d.Text, d.Kind, d.Quote, d.Basis, d.Label})
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
				if err := eachNormClass(r, func(classID, relType string) error {
					return w.Write([]string{r.ID, classID, relType})
				}); err != nil {
					return err
				}
				if err := w.Write([]string{r.ID, r.ProvisionID, "HAS_LEGAL_BASIS"}); err != nil {
					return err
				}
			}
			return eachNormDetail(in.Statements, func(d normDetail) error {
				return w.Write([]string{d.NormID, d.ID, d.RelType})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "term_uses.csv"),
		[]string{"id:ID", "label_vi", "scope_id", "doc_id", "definition_vi", "genus", "kind",
			"aliases:string[]", "differentiae:string[]", "enumerated_subtypes:string[]", "is_role:boolean", "by_reference",
			"origin", "defined_by", "quote", "char_start:int", "char_end:int", "confidence:float",
			"model", ":LABEL"},
		func(w *csv.Writer) error {
			return eachTermUse(in.Layer, func(t *concept.TermUse) error {
				differentiae := make([]string, 0, len(t.Differentiae))
				for _, d := range t.Differentiae {
					differentiae = append(differentiae, d.Text)
				}
				byReference := ""
				if t.DefinesByReference != nil {
					byReference = t.DefinesByReference.Instrument
				}
				return w.Write([]string{t.ID, t.LabelVI, t.ScopeID, t.DocID, t.DefinitionVI, t.Genus, t.Kind,
					join(t.Aliases), join(differentiae), join(t.EnumeratedSubtypes),
					strconv.FormatBool(t.IsRole), byReference,
					t.Origin, t.DefinedBy, t.Quote, strconv.Itoa(t.CharStart), strconv.Itoa(t.CharEnd),
					strconv.FormatFloat(t.Confidence, 'f', 2, 64), t.Model, "TermUse"})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "merged_concepts.csv"),
		[]string{"id:ID", "label_vi", "label_en", "kind", "disambiguator", "registry_class", ":LABEL"},
		func(w *csv.Writer) error {
			if in.Layer == nil {
				return nil
			}
			for _, c := range in.Layer.Concepts {
				if err := w.Write([]string{c.ID, c.LabelVI, c.LabelEN, c.Kind, c.Disambiguator, c.RegistryClass, "Concept"}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "instance_of.csv"),
		[]string{":START_ID", ":END_ID", "relation", "decided_by", "decided_at", "rationale", ":TYPE"},
		func(w *csv.Writer) error {
			return eachMembership(in.Layer, func(m *concept.Membership) error {
				return w.Write([]string{m.TermUseID, m.ConceptID, m.Relation, m.DecidedBy, m.DecidedAt, m.Rationale, "INSTANCE_OF"})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "differs_from.csv"),
		[]string{":START_ID", ":END_ID", "decided_by", "decided_at", "rationale", "basis:string[]", ":TYPE"},
		func(w *csv.Writer) error {
			return eachDifference(in.Layer, func(d *concept.Difference) error {
				return w.Write([]string{d.FromID, d.ToID, d.DecidedBy, d.DecidedAt, d.Rationale, join(d.Basis), "DIFFERS_FROM"})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "term_use_edges.csv"),
		[]string{":START_ID", ":END_ID", ":TYPE"},
		func(w *csv.Writer) error {
			return eachTermUseEdge(in, func(from, to, relType string) error {
				return w.Write([]string{from, to, relType})
			})
		}); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "schema.cypher"), []byte(Schema), 0o644); err != nil {
		return err
	}
	return writeImportScripts(dir)
}

// arrayDelimiter separates the elements of an array column. The importer's
// default is a semicolon, which Vietnamese legal drafting uses inside almost
// every enumerated definition, so one differentia would arrive as four. The
// pipe is passed to neo4j-admin explicitly by the import scripts.
const arrayDelimiter = "|"

// join packs a list into one CSV cell.
func join(values []string) string { return strings.Join(values, arrayDelimiter) }

// eachTermUse, eachMembership and eachDifference walk the concept layer,
// skipping nothing: the layer was already checked by concept.Layer.Check before
// it was written, and a projection is not the place to discover an invariant.
func eachTermUse(l *concept.Layer, visit func(*concept.TermUse) error) error {
	if l == nil {
		return nil
	}
	for i := range l.TermUses {
		if err := visit(&l.TermUses[i]); err != nil {
			return err
		}
	}
	return nil
}

func eachMembership(l *concept.Layer, visit func(*concept.Membership) error) error {
	if l == nil {
		return nil
	}
	for i := range l.Memberships {
		if err := visit(&l.Memberships[i]); err != nil {
			return err
		}
	}
	return nil
}

func eachDifference(l *concept.Layer, visit func(*concept.Difference) error) error {
	if l == nil {
		return nil
	}
	for i := range l.Differences {
		if err := visit(&l.Differences[i]); err != nil {
			return err
		}
	}
	return nil
}

// eachTermUseEdge visits the edges that tie a term use to the rest of the
// graph: the provision that defines it, the instrument it is scoped to, and the
// other terms its definition leans on.
//
// Every one of those is checked against the nodes this export actually writes.
// A term use recovered from a clause the corpus no longer carries, or a
// referenced term that was never itself defined, would otherwise put a dangling
// identifier in a relationship file, and neo4j-admin refuses the entire import
// over a single one of those rather than skipping the row.
func eachTermUseEdge(in Input, visit func(from, to, relType string) error) error {
	if in.Layer == nil {
		return nil
	}
	components := map[string]bool{}
	for _, d := range in.Docs {
		components[d.ID] = true
	}
	if err := eachComponent(in.Docs, func(c *law.Component) error {
		components[c.ID] = true
		return nil
	}); err != nil {
		return err
	}
	terms := map[string]bool{}
	for i := range in.Layer.TermUses {
		terms[in.Layer.TermUses[i].ID] = true
	}
	for i := range in.Layer.TermUses {
		t := &in.Layer.TermUses[i]
		if components[t.DefinedBy] {
			if err := visit(t.DefinedBy, t.ID, "DEFINES_TERM"); err != nil {
				return err
			}
		}
		if components[t.ScopeID] {
			if err := visit(t.ID, t.ScopeID, "IN_SCOPE"); err != nil {
				return err
			}
		}
		// A referenced term is a label, and a label is not an identity, so it
		// resolves only inside the instrument that wrote it. Reaching across
		// instruments here would be the string match merge the whole layer
		// exists to refuse.
		for _, label := range t.ReferencedTerms {
			id := concept.TermUseID(t.ScopeID, label)
			if id == t.ID || !terms[id] {
				continue
			}
			if err := visit(t.ID, id, "REFERS_TO"); err != nil {
				return err
			}
		}
	}
	return nil
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

// normDetail is one condition, exception, or sanction as a node.
//
// Kind and Quote are on the node rather than only in the statement JSON,
// because the questions that ask about a condition ask about a kind of
// condition, and every claim in this projection has to be checkable against the
// words that licensed it without leaving the graph.
type normDetail struct {
	ID      string
	Text    string
	Kind    string
	Quote   string
	Basis   string // the sanction's legal basis, as the provision writes it
	Label   string
	NormID  string
	RelType string
}

// eachNormDetail visits every condition, exception, and sanction node of the
// statement set. Detail node IDs hang off the norm ID, so they are as
// deterministic as everything else.
func eachNormDetail(records []norm.Record, visit func(normDetail) error) error {
	for i := range records {
		r := &records[i]
		for j, c := range r.Statement.Conditions {
			if err := visit(normDetail{
				ID: fmt.Sprintf("%s:condition-%d", r.ID, j+1), Text: c.Text, Kind: c.Kind, Quote: c.Quote,
				Label: "Condition", NormID: r.ID, RelType: "HAS_CONDITION",
			}); err != nil {
				return err
			}
		}
		for j, e := range r.Statement.Exceptions {
			if err := visit(normDetail{
				ID: fmt.Sprintf("%s:exception-%d", r.ID, j+1), Text: e.Text, Kind: e.Kind, Quote: e.Quote,
				Label: "Exception", NormID: r.ID, RelType: "HAS_EXCEPTION",
			}); err != nil {
				return err
			}
		}
		if s := r.Statement.Sanction; s != nil {
			if err := visit(normDetail{
				ID: r.ID + ":sanction", Text: s.Text, Quote: s.Quote, Basis: s.LegalBasis,
				Label: "Sanction", NormID: r.ID, RelType: "HAS_SANCTION",
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// eachNormClass visits the registry class each participant was placed in, with
// the edge type that participant earns.
//
// The counterparty gets its own edge type. "Bên A phải thông báo cho bên B"
// puts two actors in one provision and only one of them owes the duty, and a
// projection that hangs both off HAS_BEARER answers question 9, which duties
// the Labour Code places on employers, with the employees as well.
func eachNormClass(r *norm.Record, visit func(classID, relType string) error) error {
	s := &r.Statement
	for _, p := range []struct {
		ref     *norm.Ref
		relType string
	}{
		{s.Bearer, "HAS_BEARER"},
		{s.Counterparty, "HAS_COUNTERPARTY"},
		{s.Object, "HAS_OBJECT"},
	} {
		if p.ref == nil || p.ref.ClassID == "" {
			continue
		}
		if err := visit(p.ref.ClassID, p.relType); err != nil {
			return err
		}
	}
	return nil
}

// normFields flattens one statement into the scalar properties a Norm node
// carries. Both writers call it, so the CSV columns and the Bolt properties
// cannot drift apart, which they did the last time the schema moved.
func normFields(r *norm.Record) map[string]any {
	s := &r.Statement
	text := func(ref *norm.Ref) string {
		if ref == nil {
			return ""
		}
		return ref.Text
	}
	out := map[string]any{
		"norm_type": s.Type, "modality": s.Modality,
		"bearer": text(s.Bearer), "counterparty": text(s.Counterparty),
		"action": s.Action.Text, "object": text(s.Object),
		"procedure_id": s.ProcedureID, "step": s.Step,
		"deadline_text": "", "deadline_value": 0, "deadline_unit": "",
		"deadline_calendar": "", "deadline_anchor": "", "deadline_anchor_at": "",
		"evidence_quote": s.Evidence.Quote, "evidence_start": s.Evidence.Start,
		"evidence_end": s.Evidence.End, "confidence": s.Confidence,
		"verdict": "", "model": r.Model, "ontology_version": r.OntologyVersion,
	}
	if d := s.Deadline; d != nil {
		out["deadline_text"], out["deadline_value"] = d.Text, d.Value
		out["deadline_unit"], out["deadline_calendar"] = d.Unit, d.Calendar
		out["deadline_anchor"], out["deadline_anchor_at"] = d.Anchor, d.AnchorAt
	}
	if r.Entailment != nil {
		out["verdict"] = r.Entailment.Verdict
	}
	return out
}

// normColumns is the CSV column order, and the order the values come back in.
// The header the export writes is built from it, so a field added to normFields
// and forgotten here shows up as a missing column rather than a shifted one.
var normColumns = []string{
	"norm_type", "modality", "bearer", "counterparty", "action", "object",
	"deadline_text", "deadline_value", "deadline_unit", "deadline_calendar",
	"deadline_anchor", "deadline_anchor_at", "procedure_id", "step",
	"evidence_quote", "evidence_start", "evidence_end", "confidence",
	"verdict", "model", "ontology_version",
}

// normColumnTypes gives the neo4j-admin type suffix of every column that is
// not a string.
var normColumnTypes = map[string]string{
	"deadline_value": "int", "step": "int", "evidence_start": "int",
	"evidence_end": "int", "confidence": "float", "ontology_version": "int",
}

// normHeader is the norms.csv header, built from the column list so the two
// cannot disagree.
func normHeader() []string {
	out := []string{"id:ID"}
	for _, name := range normColumns {
		if t, ok := normColumnTypes[name]; ok {
			name += ":" + t
		}
		out = append(out, name)
	}
	return append(out, ":LABEL")
}

// normValues renders the fields in column order.
func normValues(r *norm.Record) []string {
	f := normFields(r)
	out := make([]string, 0, len(normColumns))
	for _, name := range normColumns {
		switch v := f[name].(type) {
		case string:
			out = append(out, v)
		case int:
			out = append(out, strconv.Itoa(v))
		case float64:
			out = append(out, strconv.FormatFloat(v, 'f', 2, 64))
		default:
			out = append(out, fmt.Sprint(v))
		}
	}
	return out
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
	args := `database import full luatdo --overwrite-destination --array-delimiter="` + arrayDelimiter + `" ` +
		`--nodes=documents.csv --nodes=components.csv --nodes=text_versions.csv ` +
		`--nodes=terms.csv --nodes=concepts.csv --nodes=subjects.csv ` +
		`--nodes=norms.csv --nodes=norm_details.csv ` +
		`--nodes=term_uses.csv --nodes=merged_concepts.csv ` +
		`--relationships=contains.csv --relationships=has_version.csv --relationships=cites.csv ` +
		`--relationships=defines.csv --relationships=mentions.csv ` +
		`--relationships=subject_parents.csv --relationships=about_subject.csv ` +
		`--relationships=has_norm.csv --relationships=norm_edges.csv ` +
		`--relationships=instance_of.csv --relationships=differs_from.csv ` +
		`--relationships=term_use_edges.csv`
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
	// TermUses and MergedConcepts are counted apart on purpose. The gap between
	// them is the work: every term use is discovered, every concept was created
	// by somebody deciding two of them are the same thing, and a report that
	// added them together would hide how much of the corpus nobody has read yet.
	TermUses       int `json:"term_uses"`
	MergedConcepts int `json:"merged_concepts"`
	InstanceOf     int `json:"instance_of"`
	DiffersFrom    int `json:"differs_from"`
	TermUseEdges   int `json:"term_use_edges"`
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
	if in.Layer != nil {
		s.TermUses = len(in.Layer.TermUses)
		s.MergedConcepts = len(in.Layer.Concepts)
		s.InstanceOf = len(in.Layer.Memberships)
		s.DiffersFrom = len(in.Layer.Differences)
	}
	_ = eachTermUseEdge(in, func(string, string, string) error {
		s.TermUseEdges++
		return nil
	})
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
	if s.TermUses > 0 {
		out += fmt.Sprintf(", term uses %d, merged concepts %d, instance of %d, differs from %d",
			s.TermUses, s.MergedConcepts, s.InstanceOf, s.DiffersFrom)
	}
	return out
}
