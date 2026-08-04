// Package retrieve finds the components of the corpus a question is about.
//
// Two things make this different from a search box over the same text. The
// first is that scope is applied before ranking rather than after it: a caller
// says which documents, which subject, which subtree of the hierarchy and which
// day it is asking about, the corpus is cut down to that, and only then does
// anything get scored. A filter applied afterwards is a different operation
// wearing the same name, because the ranker has already spent its top places on
// material the caller had ruled out.
//
// The second is that a component is indexed under more than its own words. The
// graph knows who a provision puts a duty on, what act it names, what it makes
// conditional, what deadline it sets, which term it defines and which
// instrument it cites, and none of that is reliably in the words of the
// component itself. A duty on the employer written as an enumerated clause
// under an article stem says "employer" nowhere. Those aspects come from the
// extracted statements and the citation and term layers, so a question about
// employer duties reaches the clause through the graph rather than through a
// word match that was never going to happen.
//
// There is no vector search here and nothing in this repository produces
// embeddings. Ranking inside the scope is BM25 over folded Vietnamese
// syllables and adjacent syllable pairs. It is worth saying plainly rather
// than calling it semantic search: the retrieval numbers in the benchmark are
// what a lexical ranker over graph derived aspects achieves, and an embedding
// model might do better or worse than that on the same questions.
package retrieve

import (
	"sort"
	"strings"

	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/subject"
	"github.com/tamnd/luatdo/temporal"
	"github.com/tamnd/luatdo/term"
)

// The aspects a component is indexed under. Text and heading are the words the
// component carries; the rest are derived from the graph, which is the point of
// having them.
const (
	AspectText      = "text"      // the component's own words
	AspectHeading   = "heading"   // its heading and the headings above it
	AspectBearer    = "bearer"    // who the extracted statements put the duty or right on
	AspectAction    = "action"    // what those statements say is done, and to what
	AspectCondition = "condition" // the conditions and exceptions they carry
	AspectDeadline  = "deadline"  // the deadlines they set, as the text words them
	AspectSanction  = "sanction"  // the consequence they attach
	AspectTerm      = "term"      // terms this component defines
	AspectCitation  = "citation"  // the instruments it cites, by number and identifier
)

// Aspects in index order, so reports list them the same way every time.
var Aspects = []string{
	AspectText, AspectHeading, AspectBearer, AspectAction,
	AspectCondition, AspectDeadline, AspectSanction, AspectTerm, AspectCitation,
}

// DefaultWeights is what each aspect contributes to the combined score.
//
// The numbers are not tuned against the benchmark. They were set once by what
// each aspect is for, and tuning them on the same questions the benchmark
// scores would turn a measurement into a fit. A caller that wants different
// weights passes them in Query.
var DefaultWeights = map[string]float64{
	AspectText:      1.0,
	AspectHeading:   0.6,
	AspectBearer:    0.8,
	AspectAction:    0.8,
	AspectCondition: 0.5,
	AspectDeadline:  0.6,
	AspectSanction:  0.6,
	AspectTerm:      0.9,
	AspectCitation:  0.4,
}

// Interval is one stretch of time a component's wording was good for, copied
// from the temporal layer so that this package does not have to reason about
// versions.
type Interval struct {
	From   string `json:"from"`
	To     string `json:"to,omitempty"`
	Force  string `json:"force"`
	Source string `json:"source"`
}

// Unit is one retrievable component: an article, a clause or a point, with the
// text it carries and everything the graph knows about it.
type Unit struct {
	ComponentID string   `json:"component_id"`
	DocID       string   `json:"doc_id"`
	Kind        string   `json:"kind"`
	Number      string   `json:"number"`
	Heading     string   `json:"heading,omitempty"`
	Text        string   `json:"text"`
	Position    int      `json:"position"`
	Subjects    []string `json:"subjects,omitempty"`
	Statements  []string `json:"statements,omitempty"`

	// Span is the component's own words followed by the words of everything
	// nested under it, and it is what may be quoted from the component.
	//
	// The two differ for the shape that runs through the whole corpus: a clause
	// that is a stem and four lettered points. The clause's own text is "Khi làm
	// thủ tục hải quan, công chức hải quan phải:" and the four duties live in
	// its points, so a statement read from the clause is a statement about words
	// the clause does not contain. Ranking uses Text, because a stem and its
	// points are separately retrievable; quoting uses Span, because an answerer
	// asked to quote the clause it was handed and then told those words are not
	// in it has been set an impossible task.
	Span string `json:"span,omitempty"`

	// Intervals is what the temporal layer stamped on records read out of this
	// component. A component with none is not undated, it is unstamped, and the
	// date filter treats those two the same way on purpose: it drops them and
	// says how many it dropped.
	Intervals []Interval `json:"intervals,omitempty"`

	aspects map[string][]string
}

// InForceAt reports whether any stamped interval on this component covers the
// date. A component with no interval answers false, and the caller is expected
// to report that separately from a component whose wording had ended.
func (u *Unit) InForceAt(date string) bool {
	for _, iv := range u.Intervals {
		v := temporal.Validity{From: iv.From, To: iv.To, Force: iv.Force, Source: iv.Source}
		if v.InForceAt(date) {
			return true
		}
	}
	return false
}

// Unread reports whether every interval on this component rests on a document
// that something amends and nobody has read the amendment yet.
//
// The temporal layer stamps those and then refuses to call them in force, which
// is the honest answer to "was this the wording on that day" when the amending
// instruction has not been read. It matters here because it is the difference
// between a date filter that dropped a component and a date filter that does
// not know, and a retrieval report that shows one figure for both is telling
// the reader the corpus had ended when it had only gone unread.
func (u *Unit) Unread() bool {
	if len(u.Intervals) == 0 {
		return false
	}
	for _, iv := range u.Intervals {
		if iv.Source != temporal.SourceCommencementAmended {
			return false
		}
	}
	return true
}

// startedBy reports whether any interval had begun and not ended by the date,
// ignoring the temporal layer's refusal to call an unread wording current. It
// is the arithmetic behind Scope.Unread and nothing else, which is why it is
// not exported: on its own it is a claim the layer declined to make.
func (u *Unit) startedBy(date string) bool {
	for _, iv := range u.Intervals {
		if date >= iv.From && (iv.To == "" || date < iv.To) {
			return true
		}
	}
	return false
}

// Aspect returns the strings indexed under one aspect, which is what a report
// shows when it explains why a component was retrieved.
func (u *Unit) Aspect(name string) []string { return u.aspects[name] }

// Input is everything the index is built from. Every field but Docs is
// optional, and an index built without them is a plain text index over the same
// components, which is exactly the comparison the benchmark wants to make.
type Input struct {
	Docs     []*law.Document
	Records  []norm.Record // trusted statements only, the caller filters
	Subjects []subject.Record
	Terms    []term.Definition
	Links    []cite.Link
	Validity []temporal.Validity
}

// Index is the searchable corpus.
type Index struct {
	units  []*Unit
	byID   map[string]int
	fields map[string]*field
}

// Units returns the indexed components in index order.
func (ix *Index) Units() []*Unit { return ix.units }

// Unit returns one component by identifier.
func (ix *Index) Unit(id string) *Unit {
	i, ok := ix.byID[id]
	if !ok {
		return nil
	}
	return ix.units[i]
}

// Len is how many components are indexed.
func (ix *Index) Len() int { return len(ix.units) }

// Build indexes every component that carries text.
//
// A chapter with a heading and no words of its own is not a unit. Nothing can
// be quoted from it, so an answerer could not cite it, and a retriever that
// returns it has spent one of the caller's slots on a signpost. Its heading
// still reaches the units underneath it through the heading aspect.
func Build(in Input) *Index {
	ix := &Index{byID: map[string]int{}, fields: map[string]*field{}}
	subjects := subjectsByDoc(in.Subjects)
	statements := statementsByComponent(in.Records)
	terms := termsByComponent(in.Terms)
	cites := citationsByComponent(in.Links)
	intervals := intervalsByComponent(in.Validity)

	for _, doc := range in.Docs {
		headings := headingChain(doc)
		spans := spanByComponent(doc)
		for i := range doc.Provisions {
			p := &doc.Provisions[i]
			if strings.TrimSpace(p.Text) == "" {
				continue
			}
			u := &Unit{
				ComponentID: p.ID, DocID: doc.ID, Kind: p.Kind, Number: p.Number,
				Heading: p.Heading, Text: p.Text, Position: p.Position,
				Span:      spans[p.ID],
				Subjects:  subjects[doc.ID],
				Intervals: intervals[p.ID],
				aspects:   map[string][]string{},
			}
			u.aspects[AspectText] = []string{p.Text}
			u.aspects[AspectHeading] = headings[p.ID]
			for _, rec := range statements[p.ID] {
				u.Statements = append(u.Statements, rec.ID)
				addStatementAspects(u, &rec.Statement)
			}
			u.aspects[AspectTerm] = terms[p.ID]
			u.aspects[AspectCitation] = cites[p.ID]
			ix.byID[p.ID] = len(ix.units)
			ix.units = append(ix.units, u)
		}
	}
	for _, name := range Aspects {
		ix.fields[name] = buildField(ix.units, name)
	}
	return ix
}

// addStatementAspects is where the graph earns its place in the index. Every
// string here comes from a statement a judge accepted, not from the words of
// the component, and for enumerated clauses the two are usually different.
func addStatementAspects(u *Unit, s *norm.Statement) {
	add := func(aspect string, values ...string) {
		for _, v := range values {
			if strings.TrimSpace(v) == "" {
				continue
			}
			u.aspects[aspect] = append(u.aspects[aspect], v)
		}
	}
	if s.Bearer != nil {
		add(AspectBearer, s.Bearer.Text, s.Bearer.ClassID)
	}
	if s.Counterparty != nil {
		add(AspectBearer, s.Counterparty.Text)
	}
	add(AspectAction, s.Action.Text, s.Action.ConceptID)
	if s.Object != nil {
		add(AspectAction, s.Object.Text)
	}
	for _, c := range s.Conditions {
		add(AspectCondition, c.Text)
	}
	for _, c := range s.Exceptions {
		add(AspectCondition, c.Text)
	}
	if s.Deadline != nil {
		add(AspectDeadline, s.Deadline.Text)
	}
	if s.Sanction != nil {
		add(AspectSanction, s.Sanction.Text)
	}
}

// headingChain gives every component the headings above it, nearest first.
//
// An article's heading is the one line of a Vietnamese instrument that was
// written to be read on its own, and a clause four levels down inherits the
// subject matter from it without repeating a word of it.
func headingChain(doc *law.Document) map[string][]string {
	parent := map[string]string{}
	heading := map[string]string{}
	for i := range doc.Provisions {
		p := &doc.Provisions[i]
		parent[p.ID] = p.ParentID
		if h := strings.TrimSpace(p.Heading); h != "" {
			heading[p.ID] = h
		}
	}
	out := map[string][]string{}
	for id := range parent {
		var chain []string
		if h := heading[id]; h != "" {
			chain = append(chain, h)
		}
		for up, guard := parent[id], 0; up != "" && guard < 12; up, guard = parent[up], guard+1 {
			if h := heading[up]; h != "" {
				chain = append(chain, h)
			}
		}
		if doc.Title != "" {
			chain = append(chain, doc.Title)
		}
		out[id] = chain
	}
	return out
}

func subjectsByDoc(records []subject.Record) map[string][]string {
	out := map[string][]string{}
	for i := range records {
		r := &records[i]
		for _, a := range r.Subjects {
			out[r.DocID] = append(out[r.DocID], a.SubjectID)
		}
	}
	return out
}

func statementsByComponent(records []norm.Record) map[string][]norm.Record {
	out := map[string][]norm.Record{}
	for _, r := range records {
		if !r.Trusted() {
			continue
		}
		out[r.ProvisionID] = append(out[r.ProvisionID], r)
	}
	return out
}

func termsByComponent(defs []term.Definition) map[string][]string {
	out := map[string][]string{}
	for i := range defs {
		d := &defs[i]
		out[d.ProvisionID] = append(out[d.ProvisionID], d.Term)
	}
	return out
}

// citationsByComponent indexes both the number as the text writes it and the
// identifier it resolved to, because people ask both ways and only one of the
// two is in the words of the provision.
func citationsByComponent(links []cite.Link) map[string][]string {
	out := map[string][]string{}
	for _, l := range links {
		if l.FromProvision == "" {
			continue
		}
		out[l.FromProvision] = append(out[l.FromProvision], l.ToNumber)
		if l.ToDoc != "" {
			out[l.FromProvision] = append(out[l.FromProvision], l.ToDoc)
		}
	}
	return out
}

// spanByComponent joins each component's words to the words of everything
// nested under it, in document order.
//
// The nesting is read from the identifier rather than from ParentID because the
// identifier is what every other layer keys on, and a point whose parent link
// went missing would otherwise drop out of the span its statements were read
// from without anything saying so.
func spanByComponent(doc *law.Document) map[string]string {
	parts := map[string][]string{}
	for i := range doc.Provisions {
		p := &doc.Provisions[i]
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		// Every ancestor of this component gets these words, and the walk up is
		// over the identifier's segments. It also produces keys above the
		// document, such as the year, which nothing ever looks up.
		for id := p.ID; ; {
			parts[id] = append(parts[id], text)
			cut := strings.LastIndex(id, ":")
			if cut < 0 {
				break
			}
			id = id[:cut]
		}
	}
	out := make(map[string]string, len(parts))
	for id, texts := range parts {
		out[id] = strings.Join(texts, "\n")
	}
	return out
}

// intervalsByComponent collapses the stamps on a component's records into the
// distinct intervals the component's wording had. Fifty statements read out of
// one article share one interval and storing it fifty times would make the date
// filter look better informed than it is.
func intervalsByComponent(stamps []temporal.Validity) map[string][]Interval {
	out := map[string][]Interval{}
	seen := map[string]bool{}
	for _, v := range stamps {
		iv := Interval{From: v.From, To: v.To, Force: v.Force, Source: v.Source}
		key := v.ProvisionID + "\x00" + iv.From + "\x00" + iv.To + "\x00" + iv.Force + "\x00" + iv.Source
		if seen[key] {
			continue
		}
		seen[key] = true
		out[v.ProvisionID] = append(out[v.ProvisionID], iv)
	}
	for id := range out {
		sort.Slice(out[id], func(i, j int) bool { return out[id][i].From < out[id][j].From })
	}
	return out
}
