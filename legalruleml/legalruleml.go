// Package legalruleml writes the norm layer as LegalRuleML.
//
// LegalRuleML is the OASIS interchange format for legal rules, and it is the
// format a person asks for when they want to reason with the law rather than
// read about it. That is exactly why this package is gated and why the export
// is narrower than the format allows.
//
// A deontic operator in LegalRuleML is a claim that a rule engine can act on
// this. Every statement here was read out of Vietnamese prose by a language
// model and checked by another one, the pass reports precision rather than
// proof, and wrapping that in an XML element named Obligation does not make it
// any more certain than it was in the JSON it came from. It does make it look
// more certain, which is the failure this package is written against: a
// formalism is read as a guarantee by people who never saw how it was produced.
//
// So three things hold. Only the campaign that was measured is exported, and
// the release gates decide, with no flag to override them. Only records the
// judge entailed or a person approved are written, so the file holds what the
// trusted store holds and nothing else. And what the pipeline did not formalise
// is carried as text under a named relation rather than invented as logic: a
// condition is an atom whose argument is the words of the condition, and a
// consumer can see there is a condition, read it, and see plainly that nothing
// has resolved it into a predicate. An engine that runs this file gets the
// modalities, the parties, the actions and the deadlines, which is what was
// actually extracted, and it gets no false precision about the rest.
package legalruleml

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/store"
)

// Namespaces. The luatdo one carries the relations the pipeline named itself,
// which is everything that is not part of RuleML's own vocabulary.
const (
	nsLRML   = "http://docs.oasis-open.org/legalruleml/ns/v1.0/"
	nsRuleML = "http://ruleml.org/spec"
	nsLuatdo = "https://luatdo.dev/ns#"
	nsID     = "https://luatdo.dev/id/"
)

// deontic maps a statement type onto the LegalRuleML operator that states it.
//
// Four types map and the rest do not. A definition is constitutive rather than
// prescriptive and gets its own statement element. A sanction, a procedure step
// and an exception are all parts of some other norm rather than norms in their
// own right, and giving each of them a deontic operator would put a duty in the
// file that the provision never stated.
var deontic = map[string]string{
	"duty":        "Obligation",
	"prohibition": "Prohibition",
	"permission":  "Permission",
	"right":       "Right",
}

// skipReason says why a type is not exported, in words a reader can check
// against the provision rather than a code they have to look up.
var skipReason = map[string]string{
	"definition": "",
	"sanction":   "a sanction attaches to the norm it punishes and is written on that statement, not as one",
	"procedure":  "a procedure step is a step of a procedure and the procedure is not a deontic statement",
	"exception":  "an exception qualifies another norm and is written on that norm",
}

// Input is one campaign's trusted output.
type Input struct {
	Campaign string
	Note     string
	Records  []norm.Record
}

// Summary is what one export wrote.
type Summary struct {
	Statements   int            `json:"statements"`
	Prescriptive int            `json:"prescriptive"`
	Constitutive int            `json:"constitutive"`
	Sources      int            `json:"sources"`
	Conditions   int            `json:"conditions"`
	Exceptions   int            `json:"exceptions"`
	Deadlines    int            `json:"deadlines"`
	Skipped      map[string]int `json:"skipped,omitempty"`
}

func (s Summary) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d statements over %d legal sources, %d prescriptive and %d constitutive\n",
		s.Statements, s.Sources, s.Prescriptive, s.Constitutive)
	fmt.Fprintf(&b, "%d conditions and %d exceptions carried as text, %d deadlines carried as numbers\n",
		s.Conditions, s.Exceptions, s.Deadlines)
	for _, t := range sortedKeys(s.Skipped) {
		fmt.Fprintf(&b, "%d %s statements were not written, %s\n", s.Skipped[t], t, skipReason[t])
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

// Export writes one campaign's trusted norms.
//
// Records that are not trusted are refused rather than skipped. A caller who
// hands over the whole store has made a mistake about what this file is for,
// and quietly dropping most of the input would produce a file that looks like a
// campaign and is a filtered view somebody else chose.
func Export(w io.Writer, in Input) (Summary, error) {
	s := Summary{Skipped: map[string]int{}}
	if in.Campaign == "" {
		return s, fmt.Errorf("legalruleml: no campaign named, this format is for measured output and a corpus wide export is not measured")
	}
	for i := range in.Records {
		if !in.Records[i].Trusted() {
			return s, fmt.Errorf("legalruleml: %s is %s, only entailed or approved records are exported",
				in.Records[i].ID, in.Records[i].Status)
		}
	}

	records := append([]norm.Record(nil), in.Records...)
	sort.Slice(records, func(i, j int) bool {
		if records[i].ProvisionID != records[j].ProvisionID {
			return records[i].ProvisionID < records[j].ProvisionID
		}
		return records[i].ID < records[j].ID
	})

	// The sources come first because everything else points at them. A source
	// is a provision and not a document: a duty is stated by a clause, and an
	// association that named the instrument would be true and useless.
	var sources []string
	seen := map[string]bool{}
	for i := range records {
		if id := records[i].ProvisionID; id != "" && !seen[id] {
			seen[id] = true
			sources = append(sources, id)
		}
	}
	s.Sources = len(sources)

	b := &writer{out: w}
	b.line(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.comment(header(in))
	b.open("lrml:LegalRuleML",
		attr{"xmlns:lrml", nsLRML}, attr{"xmlns:ruleml", nsRuleML}, attr{"xmlns:luatdo", nsLuatdo})

	b.open("lrml:LegalSources")
	for _, id := range sources {
		b.empty("lrml:LegalSource", attr{"key", sourceKey(id)}, attr{"sameAs", nsID + id})
	}
	b.close("lrml:LegalSources")

	b.open("lrml:Statements")
	for i := range records {
		r := &records[i]
		st := &r.Statement
		if _, ok := deontic[st.Type]; !ok && st.Type != "definition" {
			s.Skipped[st.Type]++
			continue
		}
		s.Statements++
		if st.Type == "definition" {
			s.Constitutive++
			b.open("lrml:ConstitutiveStatement", attr{"key", statementKey(r.ID)})
			b.atom(nsLuatdo+"defines", args(st))
			b.close("lrml:ConstitutiveStatement")
			continue
		}
		s.Prescriptive++
		b.open("lrml:PrescriptiveStatement", attr{"key", statementKey(r.ID)})
		conds, excs := len(st.Conditions), len(st.Exceptions)
		s.Conditions += conds
		s.Exceptions += excs
		if conds+excs > 0 {
			b.open("ruleml:Rule", attr{"closure", "universal"})
			b.open("ruleml:if")
			b.open("ruleml:And")
			for _, c := range st.Conditions {
				b.textAtom(nsLuatdo+"condition", c.Kind, c.Text)
			}
			for _, e := range st.Exceptions {
				// An exception is written as a negation as failure over the
				// words of the exception. That is the defeasible reading the
				// provision has: the duty holds unless the exception is shown.
				// It is not executable, because nothing has resolved the words
				// into a predicate, and it says so by carrying them.
				b.open("ruleml:Naf")
				b.textAtom(nsLuatdo+"exception", e.Kind, e.Text)
				b.close("ruleml:Naf")
			}
			b.close("ruleml:And")
			b.close("ruleml:if")
			b.open("ruleml:then")
			s.Deadlines += b.deontic(st)
			b.close("ruleml:then")
			b.close("ruleml:Rule")
		} else {
			s.Deadlines += b.deontic(st)
		}
		b.close("lrml:PrescriptiveStatement")
	}
	b.close("lrml:Statements")

	b.open("lrml:Associations")
	for i := range records {
		r := &records[i]
		if _, ok := deontic[r.Statement.Type]; !ok && r.Statement.Type != "definition" {
			continue
		}
		if r.ProvisionID == "" {
			continue
		}
		b.open("lrml:Association")
		b.empty("lrml:appliesSource", attr{"keyref", "#" + sourceKey(r.ProvisionID)})
		b.empty("lrml:toTarget", attr{"keyref", "#" + statementKey(r.ID)})
		b.close("lrml:Association")
	}
	b.close("lrml:Associations")
	b.close("lrml:LegalRuleML")
	return s, b.err
}

// header is the note at the top of the file. It is written into the document
// rather than into a README because a file like this is passed around on its
// own, and the person who has to decide whether to trust it is the one holding
// it.
func header(in Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  Campaign %s. %s\n\n", in.Campaign, in.Note)
	b.WriteString("  Every statement here was read out of Vietnamese legal prose by a language model\n")
	b.WriteString("  and checked by a second one against the same text. The deontic operators are what\n")
	b.WriteString("  that reading produced. They are not a proof and they are not a certification, and\n")
	b.WriteString("  the campaign report beside this file gives the precision they were measured at.\n\n")
	b.WriteString("  Conditions and exceptions are carried as text under luatdo:condition and\n")
	b.WriteString("  luatdo:exception. Nothing has resolved them into predicates, so an engine can see\n")
	b.WriteString("  that a rule is qualified and cannot evaluate the qualification. That is the state\n")
	b.WriteString("  of the extraction and not a limitation of the format.\n")
	return b.String()
}

// sourceKey and statementKey are the internal keys the associations join on.
// They are derived from the identifiers rather than counted, so two exports of
// one campaign produce the same file.
func sourceKey(provisionID string) string {
	return "ls-" + store.HashBytes([]byte(provisionID))[:12]
}

func statementKey(recordID string) string {
	return "st-" + store.HashBytes([]byte(recordID))[:12]
}

// args is the participants of a statement, in the order an atom takes them:
// who, then what it is done to, then who it is done towards.
func args(st *norm.Statement) []*norm.Ref {
	out := []*norm.Ref{}
	for _, r := range []*norm.Ref{st.Bearer, st.Object, st.Counterparty} {
		if r != nil && strings.TrimSpace(r.Text) != "" {
			out = append(out, r)
		}
	}
	return out
}

// attr is one XML attribute.
type attr struct{ name, value string }

// writer emits indented XML and remembers the first error.
type writer struct {
	out   io.Writer
	depth int
	err   error
}

func (w *writer) line(s string) {
	if w.err != nil {
		return
	}
	_, w.err = fmt.Fprintf(w.out, "%s%s\n", strings.Repeat("  ", w.depth), s)
}

func (w *writer) comment(s string) { w.line("<!--" + strings.ReplaceAll(s, "--", "- -") + "-->") }

func (w *writer) open(name string, as ...attr) {
	w.line("<" + name + render(as) + ">")
	w.depth++
}

func (w *writer) close(name string) {
	w.depth--
	w.line("</" + name + ">")
}

func (w *writer) empty(name string, as ...attr) { w.line("<" + name + render(as) + "/>") }

func render(as []attr) string {
	var b strings.Builder
	for _, a := range as {
		fmt.Fprintf(&b, " %s=\"%s\"", a.name, escapeAttr(a.value))
	}
	return b.String()
}

// deontic writes the operator and what it governs, and reports whether a
// deadline went with it.
func (w *writer) deontic(st *norm.Statement) int {
	name := "lrml:" + deontic[st.Type]
	w.open(name)
	deadlines := 0
	if st.Deadline != nil && st.Deadline.Value > 0 {
		deadlines = 1
		w.open("ruleml:And")
	}
	w.atom(actionIRI(st), args(st))
	if deadlines == 1 {
		w.open("ruleml:Atom")
		w.empty("ruleml:Rel", attr{"iri", nsLuatdo + "withinDeadline"})
		w.line(fmt.Sprintf("<ruleml:Data>%d</ruleml:Data>", st.Deadline.Value))
		w.value("ruleml:Ind", st.Deadline.Unit)
		// The calendar goes in because five working days is nine calendar days
		// across a holiday, and a file that dropped it would be wrong about
		// every deadline in it by an amount that looks like rounding.
		w.value("ruleml:Ind", st.Deadline.Calendar)
		w.close("ruleml:Atom")
		w.close("ruleml:And")
	}
	w.close(name)
	return deadlines
}

// atom writes one relation over its participants. A participant the pipeline
// placed in the registry or resolved to a concept gets an iri and is something
// a consumer can join on. One it did not gets its words and nothing else, which
// is what the pipeline knows about it.
func (w *writer) atom(rel string, refs []*norm.Ref) {
	w.open("ruleml:Atom")
	w.empty("ruleml:Rel", attr{"iri", rel})
	for _, r := range refs {
		switch {
		case r.ConceptID != "":
			w.valueWith("ruleml:Ind", attr{"iri", nsID + r.ConceptID}, r.Text)
		case r.ClassID != "":
			w.valueWith("ruleml:Ind", attr{"iri", nsID + r.ClassID}, r.Text)
		default:
			w.value("ruleml:Ind", r.Text)
		}
	}
	w.close("ruleml:Atom")
}

// textAtom writes a qualification that was never formalised: the kind the
// extractor gave it, and the words it was written in.
func (w *writer) textAtom(rel, kind, text string) {
	w.open("ruleml:Atom")
	w.empty("ruleml:Rel", attr{"iri", rel})
	if kind != "" {
		w.value("ruleml:Ind", kind)
	}
	w.value("ruleml:Ind", text)
	w.close("ruleml:Atom")
}

func (w *writer) value(name, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	w.line("<" + name + ">" + escapeText(text) + "</" + name + ">")
}

func (w *writer) valueWith(name string, a attr, text string) {
	w.line("<" + name + render([]attr{a}) + ">" + escapeText(text) + "</" + name + ">")
}

// actionIRI is the relation an action becomes. A concept the layer resolved
// gives a stable identifier, and an unresolved action gives a slug of its own
// words, which is not stable across rewordings and is still better than every
// action in the file sharing one relation.
func actionIRI(st *norm.Statement) string {
	if st.Action.ConceptID != "" {
		return nsID + st.Action.ConceptID
	}
	if slug := law.Slug(st.Action.Text); slug != "" {
		return nsLuatdo + "action/" + slug
	}
	return nsLuatdo + "action"
}

// escapeText and escapeAttr escape by hand rather than through encoding/xml,
// because the document is written element by element and xml.EscapeText would
// also turn the newlines inside a Vietnamese quotation into entities.
func escapeText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(strings.TrimSpace(s))
}

func escapeAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
