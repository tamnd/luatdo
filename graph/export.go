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
	"github.com/tamnd/luatdo/conflict"
	"github.com/tamnd/luatdo/event"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/link"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/relation"
	"github.com/tamnd/luatdo/subject"
	"github.com/tamnd/luatdo/temporal"
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
CREATE CONSTRAINT condition_id IF NOT EXISTS FOR (n:Condition) REQUIRE n.id IS UNIQUE;
CREATE CONSTRAINT exception_id IF NOT EXISTS FOR (n:Exception) REQUIRE n.id IS UNIQUE;
CREATE CONSTRAINT sanction_id IF NOT EXISTS FOR (n:Sanction) REQUIRE n.id IS UNIQUE;
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
CREATE CONSTRAINT event_id IF NOT EXISTS FOR (e:Event) REQUIRE e.id IS UNIQUE;
CREATE CONSTRAINT temporal_version_id IF NOT EXISTS FOR (v:TemporalVersion) REQUIRE v.id IS UNIQUE;
CREATE INDEX event_date IF NOT EXISTS FOR (e:Event) ON (e.date);
CREATE INDEX event_kind IF NOT EXISTS FOR (e:Event) ON (e.kind);
CREATE INDEX temporal_version_component IF NOT EXISTS FOR (v:TemporalVersion) ON (v.component_id);
CREATE INDEX temporal_version_interval IF NOT EXISTS FOR (v:TemporalVersion) ON (v.from_date, v.to_date);
CREATE INDEX temporal_version_force IF NOT EXISTS FOR (v:TemporalVersion) ON (v.force);
CREATE CONSTRAINT conflict_id IF NOT EXISTS FOR (c:Conflict) REQUIRE c.id IS UNIQUE;
CREATE INDEX conflict_rule IF NOT EXISTS FOR (c:Conflict) ON (c.rule, c.circumstances);
CREATE CONSTRAINT act_id IF NOT EXISTS FOR (a:Act) REQUIRE a.id IS UNIQUE;
CREATE INDEX act_class IF NOT EXISTS FOR (a:Act) ON (a.class);
CREATE INDEX act_status IF NOT EXISTS FOR (a:Act) ON (a.status);
CREATE FULLTEXT INDEX act_text IF NOT EXISTS FOR (a:Act) ON EACH [a.label_vi, a.definition];
`

// componentLabels is what a component node carries. law.ProvisionAlias rides
// along until the release named beside it, so a query written against the
// earlier projection still matches.
//
// The separator is the array delimiter and not a semicolon, because the
// importer treats :LABEL as an array field and splits it with whatever
// --array-delimiter says. A semicolon here, against a dump that passes a pipe,
// imports one label literally named "Component;Provision", which is not a label
// anything queries and is not a node count anything notices. The merge path
// sets both labels in Cypher and was always right, so the two paths disagreed
// about the alias and nothing said so.
const componentLabels = "Component" + arrayDelimiter + law.ProvisionAlias

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
	// Relations are concept to concept edges. They need Layer, since a relation
	// between two concepts is nothing without the concepts, and the projection
	// drops any edge whose endpoints this export did not write.
	Relations []relation.Edge
	// Temporal is the version graph: what each component said over which
	// interval, and the events that closed one version and opened another.
	Temporal *temporal.Layer
	// Conflicts are the pairs the detector could not reconcile. They need
	// Statements, since a conflict between two norms is nothing without the
	// norms, and the projection drops any finding whose norms this export did
	// not write.
	Conflicts []conflict.Finding
	// Acts are the acts the corpus regulates, folded corpus wide. Chains are
	// the act to act edges, and NormActs joins a statement to the act its action
	// or its sanction slot names.
	//
	// The three are separate fields rather than one layer struct because they
	// are checked against different things: a chain needs both of its acts, a
	// participant needs the concept layer, and a norm slot link needs
	// Statements. A projection handed the chains without the acts writes edges
	// into nodes that are not there.
	Acts     []event.Event
	Chains   []event.Chain
	NormActs []event.Link
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
	if err := writeCSV(filepath.Join(dir, "documents.csv"), nodeHeader(docColumns, nil),
		func(w *csv.Writer) error {
			for _, d := range docs {
				row := nodeValues(docFields(d), docColumns)
				if err := w.Write(append(append([]string{d.ID}, row...), "Document")); err != nil {
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
			return eachComponent(docs, func(_ *law.Document, c *law.Component) error {
				return w.Write([]string{c.ID, c.Kind, c.Number, c.Heading, strconv.Itoa(c.Position), c.RenumberedFrom, componentLabels})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "text_versions.csv"),
		[]string{"id:ID", "text", "text_hash", "from_date", "to_date", "caption", ":LABEL"},
		func(w *csv.Writer) error {
			return eachVersion(docs, func(_ *law.Document, v *law.TextVersion) error {
				return w.Write([]string{v.ID, v.Text, v.TextHash, v.FromDate, v.ToDate, VersionCaption(v), "TextVersion"})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "has_version.csv"),
		[]string{":START_ID", ":END_ID", ":TYPE"},
		func(w *csv.Writer) error {
			return eachVersion(docs, func(_ *law.Document, v *law.TextVersion) error {
				return w.Write([]string{v.ComponentID, v.ID, "HAS_VERSION"})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "contains.csv"),
		[]string{":START_ID", ":END_ID", ":TYPE"},
		func(w *csv.Writer) error {
			// One edge per component rather than one per provision. The two are
			// the same list until a document numbers something twice, and then the
			// provisions carry an edge into a node components.csv did not write.
			return eachComponent(docs, func(d *law.Document, c *law.Component) error {
				return w.Write([]string{containerOf(d, c), c.ID, "CONTAINS"})
			})
		}); err != nil {
		return err
	}
	written, err := writtenNodes(docs)
	if err != nil {
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
				// Both ends are checked against the nodes this export wrote,
				// the same fence the term use edges get. A citation whose
				// source provision was folded away by splitOnce, or whose
				// target document is not in this export, is a row naming a node
				// that is not there, and neo4j-admin refuses the entire import
				// over one of those rather than skipping it. Campaign scope hid
				// this because it trims links to the scope already, so the full
				// corpus was the first export to carry one.
				if !written[from] || !written[l.ToDoc] {
					continue
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
			classes := registryClasses(in)
			for i := range in.Statements {
				r := &in.Statements[i]
				if err := eachNormClass(r, classes, func(classID, relType string) error {
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
	if err := writeCSV(filepath.Join(dir, "relations.csv"),
		[]string{":START_ID", ":END_ID", "status", "source", "support:int", "support_docs:int",
			"confidence:float", "direction", "quote", "provision_id", ":TYPE"},
		func(w *csv.Writer) error {
			return eachRelation(in, func(r relationRow) error {
				return w.Write([]string{r.From, r.To, r.Status, r.Source,
					strconv.Itoa(r.Support), strconv.Itoa(r.SupportDocs),
					strconv.FormatFloat(r.Confidence, 'f', 2, 64), r.Direction,
					r.Quote, r.ProvisionID, r.Type})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "about_concept.csv"),
		[]string{":START_ID", ":END_ID", "role", ":TYPE"},
		func(w *csv.Writer) error {
			return eachNormConcept(in, func(normID, conceptID, role string) error {
				return w.Write([]string{normID, conceptID, role, AboutConcept})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "events.csv"), nodeHeader(eventColumns, eventColumnTypes),
		func(w *csv.Writer) error {
			return eachEvent(in, func(e *temporal.Event) error {
				row := append([]string{e.ID}, nodeValues(eventFields(e), eventColumns)...)
				return w.Write(append(row, EventLabel))
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "temporal_versions.csv"),
		nodeHeader(temporalVersionColumns, temporalVersionColumnTypes),
		func(w *csv.Writer) error {
			return eachTemporalVersion(in, func(v *temporal.Version) error {
				row := append([]string{v.ID}, nodeValues(temporalVersionFields(v), temporalVersionColumns)...)
				return w.Write(append(row, TemporalVersionLabel))
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "temporal_edges.csv"),
		[]string{":START_ID", ":END_ID", ":TYPE"},
		func(w *csv.Writer) error {
			return eachTemporalEdge(in, func(from, to, relType string) error {
				return w.Write([]string{from, to, relType})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "conflicts.csv"),
		append(append([]string{"id:ID"}, conflictColumns...), ":LABEL"),
		func(w *csv.Writer) error {
			return eachConflict(in, func(c conflictNode) error {
				return w.Write(append(append([]string{c.ID}, c.values()...), ConflictLabel))
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "involves.csv"),
		[]string{":START_ID", ":END_ID", "side", ":TYPE"},
		func(w *csv.Writer) error {
			return eachConflict(in, func(c conflictNode) error {
				if err := w.Write([]string{c.ID, c.NormA, "a", Involves}); err != nil {
					return err
				}
				return w.Write([]string{c.ID, c.NormB, "b", Involves})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "acts.csv"), nodeHeader(actColumns, actColumnTypes),
		func(w *csv.Writer) error {
			return eachAct(in, func(a actNode) error {
				return w.Write(append(append([]string{a.ID}, a.values()...), ActLabel))
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "act_chains.csv"),
		[]string{":START_ID", ":END_ID", "status", "why", "support:int", "support_docs:int",
			"confidence:float", "direction", "quote", "provision_id", ":TYPE"},
		func(w *csv.Writer) error {
			return eachActChain(in, func(c chainEdge) error {
				return w.Write([]string{c.From, c.To, c.Status, c.Why,
					strconv.Itoa(c.Support), strconv.Itoa(c.SupportDocs),
					strconv.FormatFloat(c.Confidence, 'f', 2, 64), c.Direction,
					c.Quote, c.ProvisionID, c.Type})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "act_participants.csv"),
		[]string{":START_ID", ":END_ID", "role", "as_written", "support:int", ":TYPE"},
		func(w *csv.Writer) error {
			return eachActParticipant(in, func(p participantEdge) error {
				return w.Write([]string{p.ActID, p.ConceptID, p.Role, p.AsWritten,
					strconv.Itoa(p.Support), HasParticipant})
			})
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "about_act.csv"),
		[]string{":START_ID", ":END_ID", "slot", "provision_id", ":TYPE"},
		func(w *csv.Writer) error {
			return eachNormAct(in, func(l normActEdge) error {
				return w.Write([]string{l.NormID, l.ActID, l.Slot, l.ProvisionID, AboutAct})
			})
		}); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "schema.cypher"), []byte(Schema), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "style.grass"), []byte(Style), 0o644); err != nil {
		return err
	}
	if err := writeQueryScripts(dir); err != nil {
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
	if err := eachComponent(in.Docs, func(_ *law.Document, c *law.Component) error {
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

// splitOnce splits a document and keeps the first of any identifier the
// document uses twice.
//
// The corpus uses them twice. A decision that promulgates a regulation
// attached to it prints Điều 1 in the decision and Điều 1 again in the
// regulation, and an amending decree restates clause 1 of article 1 of every
// instrument it touches, which 140/2018/NĐ-CP does eleven times. An identifier
// is built from the numbering, so both readings land on the same one. Across
// the labour campaign that is 630 components in 94 of its 1239 documents.
//
// Folding them is the least bad of three options rather than a fix. MERGE
// folds them already, silently, so the incremental path has always done this.
// neo4j-admin does not fold them, it refuses the entire import over one
// repeated identifier, so the offline path was failing outright on the same
// corpus. Emitting each identifier once is what makes the two paths agree, and
// Summary reports how many were folded so the number stays in front of
// somebody. The fix is an identifier that tells an attached regulation apart
// from the instrument carrying it, and that belongs in the parser.
func splitOnce(d *law.Document) ([]law.Component, []law.TextVersion) {
	components, versions := law.Split(d)
	seen := make(map[string]bool, len(components))
	keptComponents := components[:0]
	for i := range components {
		if seen[components[i].ID] {
			continue
		}
		seen[components[i].ID] = true
		keptComponents = append(keptComponents, components[i])
	}
	// A version identifier carries the text hash, so two readings of one
	// provision that say different things keep both of their versions and hang
	// them off the surviving component. That is what TextVersion is for. Only an
	// exact repeat is dropped.
	seenVersions := make(map[string]bool, len(versions))
	keptVersions := versions[:0]
	for i := range versions {
		if seenVersions[versions[i].ID] {
			continue
		}
		seenVersions[versions[i].ID] = true
		keptVersions = append(keptVersions, versions[i])
	}
	return keptComponents, keptVersions
}

// eachComponent and eachVersion walk the split form of the corpus. Both call
// splitOnce rather than reading the provisions directly, so the projection and
// the split cannot disagree about which provisions earn a version, and neither
// writer can emit a node identifier the other one deduplicated.
//
// The document is handed to the callback because a top level component is
// contained by its document rather than by another component, and the walker
// is the only place that still knows which document a component came out of.
// writtenNodes is every identifier the document node files declare: the
// documents themselves and their components as splitOnce leaves them.
//
// It exists because the node files and the relationship files are built from
// different walks, and the only thing that keeps them agreeing is asking the
// same question both times.
func writtenNodes(docs []*law.Document) (map[string]bool, error) {
	out := make(map[string]bool, len(docs))
	for _, d := range docs {
		out[d.ID] = true
	}
	err := eachComponent(docs, func(_ *law.Document, c *law.Component) error {
		out[c.ID] = true
		return nil
	})
	return out, err
}

func eachComponent(docs []*law.Document, visit func(*law.Document, *law.Component) error) error {
	for _, d := range docs {
		components, _ := splitOnce(d)
		for i := range components {
			if err := visit(d, &components[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func eachVersion(docs []*law.Document, visit func(*law.Document, *law.TextVersion) error) error {
	for _, d := range docs {
		_, versions := splitOnce(d)
		for i := range versions {
			if err := visit(d, &versions[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// collisions counts the components and versions splitOnce folded away, which is
// a fact about the corpus rather than about the graph.
func collisions(docs []*law.Document) (components, versions int) {
	for _, d := range docs {
		allComponents, allVersions := law.Split(d)
		keptComponents, keptVersions := splitOnce(d)
		components += len(allComponents) - len(keptComponents)
		versions += len(allVersions) - len(keptVersions)
	}
	return components, versions
}

// containerOf is the node a component hangs under: the component above it, or
// the document when there is nothing above it.
func containerOf(d *law.Document, c *law.Component) string {
	if c.ParentID != "" {
		return c.ParentID
	}
	return d.ID
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

// registryClasses is the set of class IDs this export writes as LegalConcept
// nodes, which is what a participant edge has to land in.
func registryClasses(in Input) map[string]bool {
	out := map[string]bool{}
	if in.Registry == nil {
		return out
	}
	for _, c := range in.Registry.Classes {
		out[c.ID] = true
	}
	return out
}

// eachNormClass visits the registry class each participant was placed in, with
// the edge type that participant earns.
//
// The counterparty gets its own edge type. "Bên A phải thông báo cho bên B"
// puts two actors in one provision and only one of them owes the duty, and a
// projection that hangs both off HAS_BEARER answers question 9, which duties
// the Labour Code places on employers, with the employees as well.
//
// A participant placed in a class the registry no longer holds is skipped, for
// the same reason eachRelation drops an edge into a missing concept: the
// importer refuses the entire import over one row naming a node it does not
// have. A statement extracted against an older registry is a thing that
// happens, and it should cost that one edge rather than the whole graph.
func eachNormClass(r *norm.Record, classes map[string]bool, visit func(classID, relType string) error) error {
	s := &r.Statement
	for _, p := range []struct {
		ref     *norm.Ref
		relType string
	}{
		{s.Bearer, "HAS_BEARER"},
		{s.Counterparty, "HAS_COUNTERPARTY"},
		{s.Object, "HAS_OBJECT"},
	} {
		if p.ref == nil || p.ref.ClassID == "" || !classes[p.ref.ClassID] {
			continue
		}
		if err := visit(p.ref.ClassID, p.relType); err != nil {
			return err
		}
	}
	return nil
}

// docFields flattens one document into the properties a Document node carries,
// and docColumns is the CSV column order. Both writers call them, for the same
// reason normFields exists: the CSV columns and the Bolt properties drifted
// apart once already.
//
// The two dates come out ISO whatever the source wrote, because a graph
// compares dates as text and 17/08/2007 sorts before 01/06/2016. A date in
// neither form comes out empty rather than guessed. force_status is left in the
// source's own words, because it is a record of what the source says rather
// than a category this project invented.
func docFields(d *law.Document) map[string]any {
	return map[string]any{
		"official_number": d.OfficialNumber, "issuing_body": d.IssuingBody,
		"title": d.Title, "title_en": d.TitleEN, "doc_type": d.DocType,
		"effective_from": law.ISODate(d.EffectiveFrom),
		"signed_on":      law.ISODate(d.SignedOn),
		"expired_on":     law.ISODate(d.ExpiredOn),
		"force_status":   d.ForceStatus,
		"source":         d.Source, "source_url": d.SourceURL, "status": d.Status,
	}
}

var docColumns = []string{
	"official_number", "issuing_body", "title", "title_en", "doc_type",
	"effective_from", "signed_on", "expired_on", "force_status",
	"source", "source_url", "status",
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
		"norm_type": s.Type, "modality": s.Modality, "caption": normCaption(s),
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
	"caption", "norm_type", "modality", "bearer", "counterparty", "action", "object",
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

// nodeHeader builds a node file header from a column list and the type suffixes
// of whichever columns are not strings, so a field added to the flattener and
// forgotten in the column list shows up as a missing column rather than as
// every value after it landing one column to the left.
func nodeHeader(columns []string, types map[string]string) []string {
	out := []string{"id:ID"}
	for _, name := range columns {
		if t, ok := types[name]; ok {
			name += ":" + t
		}
		out = append(out, name)
	}
	return append(out, ":LABEL")
}

// nodeValues renders a flattened node in column order.
func nodeValues(fields map[string]any, columns []string) []string {
	out := make([]string, 0, len(columns))
	for _, name := range columns {
		switch v := fields[name].(type) {
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

// normHeader is the norms.csv header, built from the column list so the two
// cannot disagree.
func normHeader() []string { return nodeHeader(normColumns, normColumnTypes) }

// normValues renders the fields in column order.
func normValues(r *norm.Record) []string { return nodeValues(normFields(r), normColumns) }

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
//
// multiline-fields is not optional and not a tuning knob. A provision is a
// paragraph of Vietnamese and it contains newlines, so text_versions.csv has
// quoted fields that span lines, and the importer defaults to refusing those.
// Every real dump failed on it in under a second with a message about a field
// spanning multiple lines, while the fixture, whose text is one line, imported
// fine.
//
// The importer also writes import.report into the working directory, so by
// default the dump has to be run from a directory somebody can write to.
//
// Both scripts pass their own arguments through to neo4j-admin, and a repeated
// option there takes the last value given. That exists so a caller running this
// inside a container can move the report somewhere writable without this file
// having to know anything about containers, and it is not a hypothetical: a
// bind mounted export on a rootful docker host belongs to the host user and the
// image runs as neo4j, so the report is the one thing in the whole import that
// cannot be written.
func writeImportScripts(dir string) error {
	args := `database import full luatdo --overwrite-destination --multiline-fields=true --array-delimiter="` + arrayDelimiter + `" ` +
		`--nodes=documents.csv --nodes=components.csv --nodes=text_versions.csv ` +
		`--nodes=terms.csv --nodes=concepts.csv --nodes=subjects.csv ` +
		`--nodes=norms.csv --nodes=norm_details.csv ` +
		`--nodes=term_uses.csv --nodes=merged_concepts.csv ` +
		`--nodes=events.csv --nodes=temporal_versions.csv --nodes=conflicts.csv ` +
		`--nodes=acts.csv ` +
		`--relationships=contains.csv --relationships=has_version.csv --relationships=cites.csv ` +
		`--relationships=defines.csv --relationships=mentions.csv ` +
		`--relationships=subject_parents.csv --relationships=about_subject.csv ` +
		`--relationships=has_norm.csv --relationships=norm_edges.csv ` +
		`--relationships=instance_of.csv --relationships=differs_from.csv ` +
		`--relationships=term_use_edges.csv ` +
		`--relationships=relations.csv --relationships=about_concept.csv ` +
		`--relationships=temporal_edges.csv --relationships=involves.csv ` +
		`--relationships=act_chains.csv --relationships=act_participants.csv ` +
		`--relationships=about_act.csv`
	sh := "#!/bin/sh\n" +
		"# Run from this directory, which must be writable, with the database stopped.\n" +
		"# Anything passed to this script goes to neo4j-admin after the arguments below,\n" +
		"# so a repeated option here overrides the one set there.\n" +
		"neo4j-admin " + args + " \"$@\"\n"
	cmd := "@echo off\r\n" +
		"rem Run from this directory, which must be writable, with the database stopped.\r\n" +
		"rem Anything passed to this script goes to neo4j-admin after the arguments below,\r\n" +
		"rem so a repeated option here overrides the one set there.\r\n" +
		"neo4j-admin " + args + " %*\r\n"
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
	// FoldedComponents and FoldedVersions are what the projection dropped
	// because a document used one identifier for two things. They are facts
	// about the corpus, like Unresolved, and never counted against a database:
	// a folded component is a node nobody wrote, not a node that went missing.
	FoldedComponents int `json:"folded_components"`
	FoldedVersions   int `json:"folded_versions"`
	Contains         int `json:"contains"`
	Cites            int `json:"cites"`
	Unresolved       int `json:"unresolved"`
	// DanglingCites is the citations that resolved to a document and still
	// could not be written, because one end is a node this export does not
	// declare. It is reported apart from Unresolved because they are different
	// failures: unresolved is the linker not finding a target, dangling is the
	// projection folding a node the linker had already pointed at.
	DanglingCites int `json:"dangling_cites"`
	Terms         int `json:"terms"`
	Defines       int `json:"defines"`
	Concepts      int `json:"concepts"`
	Mentions      int `json:"mentions"`
	Subjects      int `json:"subjects"`
	AboutSubject  int `json:"about_subject"`
	Norms         int `json:"norms"`
	// NormEdges is every edge hanging off a Norm: the provision it came from in
	// both directions, the participants, and the conditions, exceptions and
	// sanctions. It is one number rather than eight because what it is for is
	// catching a merge that wrote the nodes and dropped the edges, and eight
	// counters would say the same thing eight times.
	NormEdges int `json:"norm_edges"`
	// TermUses and MergedConcepts are counted apart on purpose. The gap between
	// them is the work: every term use is discovered, every concept was created
	// by somebody deciding two of them are the same thing, and a report that
	// added them together would hide how much of the corpus nobody has read yet.
	TermUses       int `json:"term_uses"`
	MergedConcepts int `json:"merged_concepts"`
	InstanceOf     int `json:"instance_of"`
	DiffersFrom    int `json:"differs_from"`
	TermUseEdges   int `json:"term_use_edges"`

	// Relations counts only what reached the graph. Provisional counts what the
	// layer holds and the projection refused, and the two are printed together
	// because a graph with 40 canonical edges out of 900 is a different object
	// from one with 40 out of 41.
	Relations   int `json:"relations"`
	Provisional int `json:"provisional"`

	// AboutConcept is the join between the norm layer and the concept layer, and
	// the number to look at first. Zero means the two layers are in the same
	// database and share nothing, which is where this projection started.
	AboutConcept int `json:"about_concept"`

	Events           int `json:"events"`
	TemporalVersions int `json:"temporal_versions"`
	TemporalEdges    int `json:"temporal_edges"`

	// Conflicts is what the detector found and the projection wrote, and Shared
	// is the subset whose conditions one contains the other. The two are printed
	// together because they are different claims: a pair on shared circumstances
	// can really both be triggered, and a pair on unknown circumstances may
	// never meet.
	Conflicts int `json:"conflicts"`
	Shared    int `json:"conflicts_shared"`

	// Acts and ActChains are the consequence graph. ChainsOnOneProvision is the
	// part of it that rests on a single sentence, and it is reported beside the
	// total rather than subtracted from it, because this projection ships those
	// chains and a reader is owed the proportion before they walk one.
	Acts                 int `json:"acts"`
	ActChains            int `json:"act_chains"`
	ChainsOnOneProvision int `json:"act_chains_on_one_provision"`
	ActParticipants      int `json:"act_participants"`
	// NormActs is the join between the norm layer and the act layer, and it is
	// the number that says whether the action slot stopped being a dead end.
	NormActs int `json:"norm_acts"`
}

// Summarize counts the projection without writing it.
func Summarize(in Input) Summary {
	s := Summary{Documents: len(in.Docs)}
	_ = eachComponent(in.Docs, func(*law.Document, *law.Component) error {
		s.Components++
		s.Contains++
		return nil
	})
	_ = eachVersion(in.Docs, func(*law.Document, *law.TextVersion) error {
		s.TextVersions++
		return nil
	})
	s.FoldedComponents, s.FoldedVersions = collisions(in.Docs)
	if in.Vocabulary != nil {
		s.Subjects = len(in.Vocabulary.Subjects)
	}
	_ = eachAssignment(in, func(string, *subject.Assignment) error {
		s.AboutSubject++
		return nil
	})
	// The walk cannot fail here for the same reason the ones above it ignore
	// their errors: the visitor never returns one.
	written, _ := writtenNodes(in.Docs)
	for _, l := range in.Links {
		if l.ToDoc == "" {
			s.Unresolved++
			continue
		}
		from := l.FromProvision
		if from == "" {
			from = l.FromDoc
		}
		// Counted the way the CSV is written, because this number is what the
		// drift check compares a live database against, and a summary that
		// counted rows the export refuses to write would report drift on every
		// run of a database that is exactly right.
		if !written[from] || !written[l.ToDoc] {
			s.DanglingCites++
			continue
		}
		s.Cites++
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
	// Every statement earns a HAS_NORM from its provision and a HAS_LEGAL_BASIS
	// back to it. The rest depend on what the extractor found.
	s.NormEdges = 2 * len(in.Statements)
	classes := registryClasses(in)
	for i := range in.Statements {
		_ = eachNormClass(&in.Statements[i], classes, func(string, string) error {
			s.NormEdges++
			return nil
		})
	}
	_ = eachNormDetail(in.Statements, func(normDetail) error {
		s.NormEdges++
		return nil
	})
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
	_ = eachRelation(in, func(relationRow) error {
		s.Relations++
		return nil
	})
	s.Provisional = len(in.Relations) - s.Relations
	_ = eachNormConcept(in, func(string, string, string) error {
		s.AboutConcept++
		return nil
	})
	_ = eachEvent(in, func(*temporal.Event) error {
		s.Events++
		return nil
	})
	_ = eachTemporalVersion(in, func(*temporal.Version) error {
		s.TemporalVersions++
		return nil
	})
	_ = eachTemporalEdge(in, func(string, string, string) error {
		s.TemporalEdges++
		return nil
	})
	_ = eachConflict(in, func(c conflictNode) error {
		s.Conflicts++
		if c.Circumstances == conflict.CircumstancesShared {
			s.Shared++
		}
		return nil
	})
	_ = eachAct(in, func(actNode) error {
		s.Acts++
		return nil
	})
	_ = eachActChain(in, func(c chainEdge) error {
		s.ActChains++
		if c.Support <= 1 {
			s.ChainsOnOneProvision++
		}
		return nil
	})
	_ = eachActParticipant(in, func(participantEdge) error {
		s.ActParticipants++
		return nil
	})
	_ = eachNormAct(in, func(normActEdge) error {
		s.NormActs++
		return nil
	})
	return s
}

func (s Summary) String() string {
	out := fmt.Sprintf("documents %d, components %d, versions %d, contains %d, cites %d, unresolved %d",
		s.Documents, s.Components, s.TextVersions, s.Contains, s.Cites, s.Unresolved)
	if s.FoldedComponents > 0 || s.FoldedVersions > 0 {
		out += fmt.Sprintf(", folded %d components and %d versions onto identifiers their own document had already used",
			s.FoldedComponents, s.FoldedVersions)
	}
	if s.DanglingCites > 0 {
		out += fmt.Sprintf(", dropped %d citations with an end this export does not declare", s.DanglingCites)
	}
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
		out += fmt.Sprintf(", norms %d, norm edges %d", s.Norms, s.NormEdges)
	}
	if s.TermUses > 0 {
		out += fmt.Sprintf(", term uses %d, merged concepts %d, instance of %d, differs from %d",
			s.TermUses, s.MergedConcepts, s.InstanceOf, s.DiffersFrom)
	}
	if s.Relations > 0 || s.Provisional > 0 {
		out += fmt.Sprintf(", relations %d canonical, %d still provisional and not exported",
			s.Relations, s.Provisional)
	}
	if s.Norms > 0 && s.MergedConcepts > 0 {
		out += fmt.Sprintf(", norms joined to concepts %d", s.AboutConcept)
	}
	if s.Events > 0 || s.TemporalVersions > 0 {
		out += fmt.Sprintf(", events %d, temporal versions %d", s.Events, s.TemporalVersions)
	}
	if s.Conflicts > 0 {
		out += fmt.Sprintf(", conflicts %d of which %d on shared circumstances", s.Conflicts, s.Shared)
	}
	if s.Acts > 0 {
		out += fmt.Sprintf(", acts %d, chains %d of which %d rest on one provision, participants %d, norms joined to acts %d",
			s.Acts, s.ActChains, s.ChainsOnOneProvision, s.ActParticipants, s.NormActs)
	}
	return out
}
