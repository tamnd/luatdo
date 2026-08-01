package temporal

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// Resolution is deterministic on purpose.
//
// The model reads the instruction. Turning "khoản 2 Điều 15" into
// "vn:law:2019:45-2019-qh14:article-15:clause-2" is exact, and anything a
// grammar can get exactly is never asked of a model. It is also the step where
// a mistake is invisible: an amendment applied to the wrong clause produces a
// point in time text that is well formed and wrong.

var (
	articleRef = regexp.MustCompile(`(?i)đi[eề]u\s+([0-9]+[a-zđ]?)`)
	clauseRef  = regexp.MustCompile(`(?i)kho[aả]n\s+([0-9]+[a-zđ]?)`)
	pointRef   = regexp.MustCompile(`(?i)đi[eể]m\s+([a-zđ0-9]+)`)
	chapterRef = regexp.MustCompile(`(?i)ch[uư][oơ]ng\s+([IVXLC]+|[0-9]+)`)
)

// ParsePath turns a reference as a drafter wrote it into the structural path
// under a document identifier. It returns false when there is nothing
// structural in the reference at all, which is not a failure: "Luật này" names
// the whole instrument and resolves to the document rather than to a component.
//
// The order is deepest first in the output and does not depend on the order the
// drafter wrote them in. "Điều 20 khoản 1 điểm d" and "điểm d khoản 1 Điều 20"
// are the same component, and both forms appear in the corpus.
func ParsePath(ref string) (string, bool) {
	var parts []string
	// Every segment goes through the same spelling the parser used to build the
	// identifier. Resolution that spells a number differently from construction
	// misses a component that is right there.
	if m := chapterRef.FindStringSubmatch(ref); m != nil {
		parts = append(parts, "chapter-"+law.NumberSegment(law.RomanToArabic(m[1])))
	}
	if m := articleRef.FindStringSubmatch(ref); m != nil {
		parts = append(parts, "article-"+law.NumberSegment(m[1]))
	}
	if m := clauseRef.FindStringSubmatch(ref); m != nil {
		parts = append(parts, "clause-"+law.NumberSegment(m[1]))
	}
	if m := pointRef.FindStringSubmatch(ref); m != nil {
		parts = append(parts, "point-"+law.NumberSegment(m[1]))
	}
	if len(parts) == 0 {
		return "", false
	}
	// A chapter is only part of the path when nothing deeper was named. Article
	// identifiers in this corpus are unique within a document and do not carry
	// their chapter, so keeping it would name a component that does not exist.
	if len(parts) > 1 && strings.HasPrefix(parts[0], "chapter-") {
		parts = parts[1:]
	}
	return strings.Join(parts, ":"), true
}

// Compound reports whether a reference names more than one component.
//
// "Sửa đổi, bổ sung điểm a và điểm b khoản 1 Điều 5 như sau:" is one sentence
// and two amendments. ParsePath takes the first number at each level, so it
// reads that reference as điểm a alone, and the replacement text quoted for
// both points lands entirely on the first while the second silently keeps the
// wording it is meant to lose. That answer is well formed and wrong, which is
// the one outcome this layer is built to avoid.
//
// Splitting the quoted text in code is not on offer either, because deciding
// where điểm a ends and điểm b begins is reading. So a compound reference is
// quarantined, and the fix is in the reading: one operation per component.
func Compound(ref string) bool {
	for _, re := range []*regexp.Regexp{chapterRef, articleRef, clauseRef, pointRef} {
		seen := map[string]bool{}
		for _, m := range re.FindAllStringSubmatch(ref, -1) {
			seen[law.NumberSegment(m[1])] = true
		}
		if len(seen) > 1 {
			return true
		}
	}
	return false
}

// Corpus is what resolution needs from the store: the documents the operations
// point at, indexed by identifier, with the components each one holds.
type Corpus struct {
	docs       map[string]*law.Document
	components map[string]bool
	numbers    map[string]string // official number to document identifier
	effective  map[string]string // document identifier to a date that sorts
	colliding  map[string]int    // document identifier to repeated component identifiers
}

// NewCorpus indexes documents for resolution.
func NewCorpus(docs []*law.Document) *Corpus {
	c := &Corpus{
		docs:       map[string]*law.Document{},
		components: map[string]bool{},
		numbers:    map[string]string{},
		effective:  map[string]string{},
		colliding:  map[string]int{},
	}
	for _, d := range docs {
		if d == nil {
			continue
		}
		c.docs[d.ID] = d
		if d.OfficialNumber != "" {
			c.numbers[normalizeNumber(d.OfficialNumber)] = d.ID
		}
		// The datasets write dates as 17/08/2007 and this layer compares them as
		// text, so they are converted once here rather than at every comparison.
		c.effective[d.ID] = law.ISODate(d.EffectiveFrom)

		seen := map[string]bool{}
		for i := range d.Provisions {
			id := d.Provisions[i].ID
			if seen[id] {
				c.colliding[d.ID]++
				continue
			}
			seen[id] = true
			c.components[id] = true
		}
	}
	return c
}

// EffectiveFrom is the date a document took effect, in a form that sorts. It is
// empty where the corpus states no date or states one this code cannot read,
// and an empty date is never filled in.
func (c *Corpus) EffectiveFrom(docID string) string { return c.effective[docID] }

// Colliding reports how many component identifiers a document repeats.
//
// Almost a third of the parsed corpus repeats at least one, because an amending
// law quotes the text it inserts and the parser numbers the quoted clauses as
// though they were the amending law's own. A version graph built on colliding
// identifiers answers a point in time question with text from the wrong
// provision, so those documents are not versioned and are counted instead.
func (c *Corpus) Colliding(docID string) int { return c.colliding[docID] }

// Document returns a parsed document, or nil.
func (c *Corpus) Document(id string) *law.Document { return c.docs[id] }

// Documents returns every indexed document identifier.
func (c *Corpus) Documents() []*law.Document {
	out := make([]*law.Document, 0, len(c.docs))
	for _, d := range c.docs {
		out = append(out, d)
	}
	return out
}

// Has reports whether a component identifier exists in the corpus.
func (c *Corpus) Has(componentID string) bool { return c.components[componentID] }

// DocByNumber resolves an official number to a document identifier.
func (c *Corpus) DocByNumber(number string) (string, bool) {
	id, ok := c.numbers[normalizeNumber(number)]
	return id, ok
}

func normalizeNumber(n string) string { return strings.ToUpper(strings.TrimSpace(n)) }

// Resolve fills in the target document and component of every operation, and
// quarantines the ones that cannot be placed.
//
// amends is the document to document amendment graph already in the store,
// which answers the common case where the instruction names no number at all
// because the instrument's title already said what it amends.
func Resolve(ops []Operation, c *Corpus, amends map[string][]string) []Operation {
	out := make([]Operation, len(ops))
	copy(out, ops)
	for i := range out {
		resolveOne(&out[i], c, amends)
	}
	return out
}

func resolveOne(op *Operation, c *Corpus, amends map[string][]string) {
	if !KnownKind(op.Kind) {
		op.Quarantine = QuarantineUnknownKind
		return
	}
	doc, ok := targetDoc(op, c, amends)
	if !ok {
		op.Quarantine = QuarantineMissingDocument
		return
	}
	op.TargetDoc = doc

	if op.Phrase != nil {
		resolvePhrase(op, c)
		return
	}
	if Compound(op.TargetRef) {
		op.Quarantine = QuarantineCompoundTarget
		return
	}
	path, ok := ParsePath(op.TargetRef)
	if !ok {
		// No component named means the whole instrument, which is what replace
		// and expire do, and what nothing else may do.
		if op.Kind == KindReplace || op.Kind == KindExpire || op.Kind == KindConsolidate {
			op.TargetComponent = doc
			op.Scope = ScopeDocument
			return
		}
		op.Quarantine = QuarantineUnresolvedTarget
		return
	}
	id := doc + ":" + path
	if !c.Has(id) {
		// A supplement creates the component it names, so a target that does not
		// exist yet is the normal case for one and a defect for everything else.
		if op.Kind == KindSupplement {
			op.TargetComponent = id
			return
		}
		op.Quarantine = QuarantineUnresolvedTarget
		return
	}
	op.TargetComponent = id
}

func resolvePhrase(op *Operation, c *Corpus) {
	if len(op.Phrase.Targets) == 0 {
		op.Quarantine = QuarantineNoTargets
		return
	}
	var resolved []string
	for _, t := range op.Phrase.Targets {
		path, ok := ParsePath(t)
		if !ok {
			continue
		}
		id := op.TargetDoc + ":" + path
		if !c.Has(id) {
			continue
		}
		resolved = append(resolved, id)
	}
	if len(resolved) == 0 {
		// Nothing resolved is not a licence to apply the substitution corpus
		// wide. It is a quarantine.
		op.Quarantine = QuarantineNoTargets
		return
	}
	op.Phrase.Targets = resolved
	op.TargetComponent = resolved[0]
}

// targetDoc picks the document an operation changes: the number the instruction
// stated, or the single document this instrument is already known to amend.
// Two candidates with nothing to choose between them is unresolved rather than
// a coin flip.
func targetDoc(op *Operation, c *Corpus, amends map[string][]string) (string, bool) {
	if op.TargetNumber != "" {
		if id, ok := c.DocByNumber(op.TargetNumber); ok {
			return id, true
		}
		if id, err := law.DocID(op.TargetNumber); err == nil && c.Document(id) != nil {
			return id, true
		}
		return "", false
	}
	targets := amends[op.AmendingDoc]
	if len(targets) == 1 && c.Document(targets[0]) != nil {
		return targets[0], true
	}
	return "", false
}

// Problem is one thing a check found, named so it can be printed and counted.
type Problem struct {
	Invariant int    `json:"invariant"`
	Subject   string `json:"subject"`
	Detail    string `json:"detail"`
}

func (p Problem) String() string {
	return fmt.Sprintf("invariant %d  %s: %s", p.Invariant, p.Subject, p.Detail)
}
