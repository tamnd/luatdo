// Package concept holds the concept layer: what a term means inside one
// instrument, and which of those readings are the same thing.
//
// The layer has two node types and the difference between them is the whole
// design. A TermUse is a term as one instrument defines and uses it, and it is
// discovered. A Concept is corpus wide and exists only because a person decided
// a merge and wrote down why. Nothing in an automated pass may create a
// Concept, because a label is not an identity: nguoi lao dong is a phrase that
// the Labour Code, the Social Insurance Law and several hundred provincial
// decisions all use, and a pipeline that string matches its way to one node for
// it destroys the fact that two of them disagree.
//
// The model does the reading and never touches identity. Identifiers are minted
// here from the document and the label through law.Slug, so a rebuild of a
// pinned revision produces byte identical identifiers even though a model
// produced the content inside the node.
package concept

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/luatdo/law"
)

// Kind is the closed enum a term is filed under. It is deliberately coarse,
// because a model assigns it and the choices have to be ones two careful
// readers agree on. It is not decoration: the norm layer expresses its role
// constraints over these, so a duty borne by an action fails validation instead
// of being quietly accepted.
//
// The list started at eight, covering the subjects, acts, documents, states,
// thresholds and places a norm is made of. Annotating two hundred real
// definition clauses by hand before running anything showed that most of the
// corpus does not define those. It defines chemicals, cables, vehicles,
// software, forests and water, it defines periods and deadlines, and it defines
// standards and methods. Forcing those into artifact or status would have made
// the kind number meaningless while looking fine, so thing, time and rule were
// added and other was added with them. Other is a residual and its share is a
// measurement: if it grows past a few percent the enum is wrong again and the
// gold set will say so before a campaign does.
const (
	KindActor     = "actor"     // a legal subject that can bear duties or hold rights
	KindBody      = "body"      // an identified state organ
	KindAction    = "action"    // a regulated act, activity or service
	KindArtifact  = "artifact"  // a document, licence, registration, record, dataset
	KindThing     = "thing"     // a physical or technical object, material, works or system
	KindPlace     = "place"     // a jurisdiction, area, site or facility
	KindTime      = "time"      // a point in time, a deadline or a period
	KindAmount    = "amount"    // a sum, rate, coefficient or quantitative threshold
	KindStatus    = "status"    // a legal state a subject or thing can be in
	KindCondition = "condition" // a state of affairs used as a precondition
	KindRule      = "rule"      // a standard, method, criterion or algorithm
	KindOther     = "other"     // none of the above, kept honest rather than forced
)

// Kinds is the enum in a fixed order, for prompts and for validation.
var Kinds = []string{
	KindActor, KindBody, KindAction, KindArtifact, KindThing, KindPlace,
	KindTime, KindAmount, KindStatus, KindCondition, KindRule, KindOther,
}

// KindLabels are the Vietnamese glosses the prompt shows, so the model is
// choosing between described options rather than between bare English words.
var KindLabels = map[string]string{
	KindActor:     "chủ thể pháp lý có thể mang nghĩa vụ hoặc có quyền, nêu theo loại chứ không đích danh",
	KindBody:      "cơ quan nhà nước xác định, gọi đích danh",
	KindAction:    "hành vi, hoạt động hoặc dịch vụ được điều chỉnh",
	KindArtifact:  "giấy tờ, giấy phép, đăng ký, hồ sơ, tài liệu, dữ liệu",
	KindThing:     "vật, vật liệu, thiết bị, công trình, hệ thống kỹ thuật, tài nguyên",
	KindPlace:     "địa bàn, khu vực, vị trí, cơ sở hoặc phạm vi lãnh thổ",
	KindTime:      "thời điểm, thời hạn hoặc khoảng thời gian",
	KindAmount:    "mức tiền, tỷ lệ, hệ số hoặc ngưỡng định lượng",
	KindStatus:    "trạng thái pháp lý của một chủ thể hoặc một vật",
	KindCondition: "tình trạng dùng làm điều kiện",
	KindRule:      "quy tắc, tiêu chuẩn, tiêu chí, phương pháp hoặc thuật toán",
	KindOther:     "không thuộc các loại trên, chỉ dùng khi thực sự không xếp được",
}

// ValidKind reports whether k is in the enum.
func ValidKind(k string) bool {
	_, ok := KindLabels[k]
	return ok
}

// Origin says how a term use came to be a node. A definition read out of a
// definitions article and a concept recovered from how the corpus uses it are
// different kinds of fact, and the graph never lets one pass for the other.
const (
	OriginDefined        = "defined"         // read from a definitions article
	OriginRecovered      = "recovered"       // read from a definition stated outside one
	OriginUndefinedUsage = "undefined_usage" // never defined, promoted from usage
)

// TermUseID mints the identifier of a term as used inside one document.
//
// The scope goes in the identifier rather than beside it, because two
// instruments defining the same phrase have to land on two nodes and an
// identifier built from the label alone would collide them.
func TermUseID(scopeID, label string) string {
	return "vn:term:" + scopeID + ":" + law.Slug(label)
}

// ConceptID mints a corpus wide concept identifier. The disambiguator exists
// only when the plain form is genuinely ambiguous, and its presence is the
// signal that a person looked at it, the same way the issuing body suffix works
// on a provincial document identifier.
func ConceptID(label, disambiguator string) string {
	id := "vn:concept:" + law.Slug(label)
	if disambiguator != "" {
		id += ":" + law.Slug(disambiguator)
	}
	return id
}

// Differentia is one distinguishing feature of a definition, with the span of
// the clause it was read out of. The quote is what makes it checkable: a
// differentia the clause does not contain is a fabrication, and code catches it
// rather than a reviewer noticing.
type Differentia struct {
	Text  string `json:"text"`
	Quote string `json:"quote"`
}

// Reference is a definition that points at another instrument instead of
// stating anything: khai niem X duoc hieu theo quy dinh tai Luat Y. The target
// is recorded as written and resolved through the citation graph later. The
// definition text stays empty on purpose, because paraphrasing a definition
// from a document that is not in front of the reader is an unfalsifiable claim.
type Reference struct {
	Instrument string `json:"instrument"`
	DocID      string `json:"doc_id,omitempty"`
	Quote      string `json:"quote"`
}

// TermUse is a term as one instrument defines and uses it.
type TermUse struct {
	ID      string `json:"id"`
	LabelVI string `json:"label_vi"`
	// ScopeID is the instrument the definition belongs to, which is the
	// document for a law and the annex for a regulation issued under a
	// decision. Flattening the annex onto its parent would claim a definition
	// the decision never made.
	ScopeID string `json:"scope_id"`
	DocID   string `json:"doc_id"`

	DefinitionVI string        `json:"definition_vi,omitempty"`
	Genus        string        `json:"genus,omitempty"`
	Differentiae []Differentia `json:"differentiae,omitempty"`
	Kind         string        `json:"kind"`
	Aliases      []string      `json:"aliases,omitempty"`
	// IsRole marks a term that names a position rather than an organisation.
	// Co quan co tham quyen resolves per document and never to one ministry,
	// and a pipeline that resolves it globally has fabricated the most
	// consequential fact in the provision. It is asked as a direct question
	// while the clause is in view, never inferred from the label afterwards.
	IsRole             bool       `json:"is_role"`
	DefinesByReference *Reference `json:"defines_by_reference,omitempty"`
	EnumeratedSubtypes []string   `json:"enumerated_subtypes,omitempty"`
	ReferencedTerms    []string   `json:"referenced_terms,omitempty"`

	Origin    string `json:"origin"`
	DefinedBy string `json:"defined_by"` // the clause identifier
	Quote     string `json:"quote"`
	CharStart int    `json:"char_start"`
	CharEnd   int    `json:"char_end"`

	Confidence float64 `json:"confidence"`
	Model      string  `json:"model,omitempty"`
}

// Validate checks a reading against the clause it claims to have read. It runs
// after every model call and a failure rejects the extraction rather than
// logging a warning, because the model does not get to be trusted about its own
// evidence.
func (t *TermUse) Validate(clause string) error {
	if strings.TrimSpace(t.LabelVI) == "" {
		return fmt.Errorf("no label")
	}
	if law.Slug(t.LabelVI) == "" {
		return fmt.Errorf("label %q slugs to nothing, so it cannot carry an identifier", t.LabelVI)
	}
	if !ValidKind(t.Kind) {
		return fmt.Errorf("kind %q is not one of %s", t.Kind, strings.Join(Kinds, ", "))
	}
	if t.Origin != OriginDefined && t.Origin != OriginRecovered && t.Origin != OriginUndefinedUsage {
		return fmt.Errorf("origin %q is not a known origin", t.Origin)
	}
	if err := checkQuote(clause, t.Quote, t.CharStart, t.CharEnd); err != nil {
		return fmt.Errorf("quote: %w", err)
	}
	for i, d := range t.Differentiae {
		if strings.TrimSpace(d.Text) == "" {
			return fmt.Errorf("differentia %d has no text", i+1)
		}
		if !strings.Contains(clause, d.Quote) {
			return fmt.Errorf("differentia %d quotes %q, which is not in the clause", i+1, d.Quote)
		}
	}
	if t.Genus != "" && !strings.Contains(clause, t.Genus) {
		return fmt.Errorf("genus %q is not in the clause", t.Genus)
	}
	if t.DefinesByReference != nil {
		if !strings.Contains(clause, t.DefinesByReference.Quote) {
			return fmt.Errorf("reference quotes %q, which is not in the clause", t.DefinesByReference.Quote)
		}
		// A definition that points elsewhere states nothing itself. Anything in
		// the definition field here is the model having paraphrased a document
		// it was never shown.
		if t.DefinitionVI != "" || t.Genus != "" || len(t.Differentiae) > 0 {
			return fmt.Errorf("definition by reference to %q also carries a definition, which can only be a paraphrase", t.DefinesByReference.Instrument)
		}
	}
	for _, s := range t.EnumeratedSubtypes {
		if !strings.Contains(clause, s) {
			return fmt.Errorf("enumerated subtype %q is not in the clause", s)
		}
	}
	if t.Confidence < 0 || t.Confidence > 1 {
		return fmt.Errorf("confidence %v is outside 0 to 1", t.Confidence)
	}
	return nil
}

// checkQuote verifies a span byte for byte at the offsets it claims. Offsets
// that are merely plausible are the easiest thing in the world for a model to
// produce, so the substring is compared rather than the length.
func checkQuote(clause, quote string, start, end int) error {
	if quote == "" {
		return fmt.Errorf("empty")
	}
	if start < 0 || end > len(clause) || start >= end {
		return fmt.Errorf("offsets %d to %d are outside a clause of %d bytes", start, end, len(clause))
	}
	if clause[start:end] != quote {
		if !strings.Contains(clause, quote) {
			return fmt.Errorf("%q does not occur in the clause", quote)
		}
		return fmt.Errorf("%q occurs in the clause but not at %d to %d", quote, start, end)
	}
	return nil
}

// Concept is a corpus wide object one or more term uses are instances of. It
// exists only because a person decided a merge.
type Concept struct {
	ID            string `json:"id"`
	LabelVI       string `json:"label_vi"`
	LabelEN       string `json:"label_en,omitempty"`
	Kind          string `json:"kind"`
	Disambiguator string `json:"disambiguator,omitempty"`
	// RegistryClass is the optional bridge to the designed vocabulary. Most
	// concepts never have one, because the registry will never hold a class for
	// giay chung nhan quyen su dung dat. The registry says what kind of thing a
	// concept is at the level a norm cares about; the concept says which one.
	RegistryClass string `json:"registry_class,omitempty"`
}

// The relations a merge decision can record. Same is the merge proper; broader
// and narrower record a hierarchy the reviewer saw rather than forcing one node.
const (
	RelationSame     = "same"
	RelationBroader  = "broader"
	RelationNarrower = "narrower"
)

// Membership is the INSTANCE_OF edge, which is the merge itself. It carries who
// decided and why, and the build refuses a membership without both.
type Membership struct {
	TermUseID string `json:"term_use_id"`
	ConceptID string `json:"concept_id"`
	Relation  string `json:"relation"`
	DecidedBy string `json:"decided_by"`
	DecidedAt string `json:"decided_at"`
	Rationale string `json:"rationale"`
}

// Difference is the DIFFERS_FROM edge: two instruments using one phrase for
// different things. It is a first class edge and not an error to be smoothed
// over, because it is one of the most useful facts a legal knowledge graph can
// hold, and any pipeline that merges by string match destroys it silently.
type Difference struct {
	FromID    string `json:"from_id"`
	ToID      string `json:"to_id"`
	DecidedBy string `json:"decided_by"`
	DecidedAt string `json:"decided_at"`
	Rationale string `json:"rationale"`
	// Basis is the specific differentiae the two readings disagree on. A
	// difference with no stated basis is an opinion, and this layer stores
	// evidence.
	Basis []string `json:"basis,omitempty"`
}

// Now returns the timestamp format the decisions use.
func Now(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Layer is everything the concept layer holds, in one value, so the invariants
// can be checked over the whole of it rather than a piece at a time.
type Layer struct {
	TermUses    []TermUse    `json:"term_uses"`
	Concepts    []Concept    `json:"concepts"`
	Memberships []Membership `json:"memberships"`
	Differences []Difference `json:"differences"`
}

// Check returns every invariant violation, sorted so two runs read the same.
// The caller fails the build on a non empty result: the survey's finding is
// that missing mandatory attributes are the commonest schema violation in
// LLM built ontologies, and the answer to that is a gate rather than a report
// nobody reads.
func (l *Layer) Check() []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	terms := map[string]*TermUse{}
	for i := range l.TermUses {
		t := &l.TermUses[i]
		if terms[t.ID] != nil {
			add("term use %s is defined twice", t.ID)
			continue
		}
		terms[t.ID] = t
		if want := TermUseID(t.ScopeID, t.LabelVI); t.ID != want {
			add("term use %s does not match its scope and label, which mint %s", t.ID, want)
		}
		if t.ScopeID == "" {
			add("term use %s has no scope, and a term use without one is a corpus wide claim", t.ID)
		}
		if !ValidKind(t.Kind) {
			add("term use %s has kind %q, which is not in the enum", t.ID, t.Kind)
		}
		if t.Quote == "" {
			add("term use %s carries no quote, so nothing about it can be checked", t.ID)
		}
		if t.Origin == OriginDefined && t.DefinedBy == "" {
			add("term use %s is marked defined and names no defining provision", t.ID)
		}
	}

	concepts := map[string]*Concept{}
	for i := range l.Concepts {
		c := &l.Concepts[i]
		if concepts[c.ID] != nil {
			add("concept %s is defined twice", c.ID)
			continue
		}
		concepts[c.ID] = c
		if want := ConceptID(c.LabelVI, c.Disambiguator); c.ID != want {
			add("concept %s does not match its label, which mints %s", c.ID, want)
		}
		if !ValidKind(c.Kind) {
			add("concept %s has kind %q, which is not in the enum", c.ID, c.Kind)
		}
	}

	merged := map[string]bool{}
	for _, m := range l.Memberships {
		switch {
		case terms[m.TermUseID] == nil:
			add("membership names term use %s, which does not exist", m.TermUseID)
		case concepts[m.ConceptID] == nil:
			add("membership of %s names concept %s, which does not exist", m.TermUseID, m.ConceptID)
		}
		if m.Relation != RelationSame && m.Relation != RelationBroader && m.Relation != RelationNarrower {
			add("membership of %s has relation %q", m.TermUseID, m.Relation)
		}
		if m.DecidedBy == "" || m.Rationale == "" {
			add("membership of %s in %s has no decider or no rationale, and identity is not a machine's call",
				m.TermUseID, m.ConceptID)
		}
		if m.Relation == RelationSame {
			if merged[m.TermUseID] {
				add("term use %s is merged into two concepts as the same thing", m.TermUseID)
			}
			merged[m.TermUseID] = true
		}
		if t, c := terms[m.TermUseID], concepts[m.ConceptID]; t != nil && c != nil && m.Relation == RelationSame && t.Kind != c.Kind {
			add("term use %s is a %s and is merged into concept %s, which is a %s", t.ID, t.Kind, c.ID, c.Kind)
		}
	}

	for _, d := range l.Differences {
		for _, id := range []string{d.FromID, d.ToID} {
			if terms[id] == nil && concepts[id] == nil {
				add("difference names %s, which is neither a term use nor a concept", id)
			}
		}
		if d.FromID == d.ToID {
			add("difference from %s to itself", d.FromID)
		}
		if d.DecidedBy == "" || d.Rationale == "" {
			add("difference between %s and %s has no decider or no rationale", d.FromID, d.ToID)
		}
	}

	sort.Strings(out)
	return out
}
