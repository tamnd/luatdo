// Package norm defines the statement schema for pass 3 and its hard
// invariants.
//
// Norms are n-ary statements, not triples, so conditions, exceptions,
// deadlines, and sanctions survive extraction intact. The invariants run
// before any judge does: a statement that fails them never costs a judge
// call, and a statement that passes them still needs entailment before it is
// trusted.
package norm

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/store"
)

// Types is the closed set of statement types.
var Types = []string{
	"duty", "right", "prohibition", "permission",
	"sanction", "procedure", "definition", "exception",
}

// The verdict enum of pass 4. Only entailed enters the trusted store.
const (
	VerdictEntailed           = "entailed"
	VerdictContradicted       = "contradicted"
	VerdictPartiallySupported = "partially_supported"
	VerdictNotEnoughInfo      = "not_enough_information"
)

// Verdicts lists every legal judge verdict.
var Verdicts = []string{
	VerdictEntailed, VerdictContradicted, VerdictPartiallySupported, VerdictNotEnoughInfo,
}

// bearerRequired lists the types whose subject is a legal actor that must be
// present: a duty without a bearer is not a duty.
var bearerRequired = map[string]bool{
	"duty": true, "right": true, "prohibition": true, "permission": true,
}

// Ref is one participant of a statement: the surface text, the registry class
// where the extractor could place it, and the concept where the concept layer
// can resolve it.
//
// IsActor is explicit rather than inferred from which field the reference sits
// in. An object is often an actor too, as in "người sử dụng lao động phải thông
// báo cho người lao động", where the employee is the object of the notifying
// and a legal actor all the same. A layer that infers actorhood from position
// cannot answer question 10, which asks which duties have no identified bearer
// and whether that is a drafting defect or an extraction failure, because it
// cannot tell a missing actor from a thing.
type Ref struct {
	Text      string `json:"text"`
	ClassID   string `json:"class_id,omitempty"`
	ConceptID string `json:"concept_id,omitempty"`
	IsActor   bool   `json:"is_actor,omitempty"`
}

// Clause is one condition or one exception, with the words that carry it.
//
// A bare string cannot be checked against the provision and cannot be grouped,
// and a duty whose condition was dropped is not a weaker fact but a false one.
// The kind is coarse on purpose: a finer vocabulary would be a taxonomy nobody
// validated, and these four distinctions are the ones question 14 turns on.
type Clause struct {
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Quote string `json:"quote"`
}

// The coarse kinds a condition may take.
const (
	CondPrecondition = "precondition" // something that must already be true
	CondTemporal     = "temporal"     // a time or period the duty is confined to
	CondThreshold    = "threshold"    // a quantity that triggers the duty
	CondQualifying   = "qualifying"   // a property the bearer must have
)

// The coarse kinds an exception may take.
const (
	ExcCarveOut  = "carve_out" // a case the rule does not reach
	ExcOverride  = "override"  // another instrument governs instead
	ExcConsented = "consented" // the affected party agreed otherwise
	ExcForce     = "force"     // circumstances outside anybody's control
)

// ConditionKinds and ExceptionKinds are the closed sets.
var (
	ConditionKinds = []string{CondPrecondition, CondTemporal, CondThreshold, CondQualifying}
	ExceptionKinds = []string{ExcCarveOut, ExcOverride, ExcConsented, ExcForce}
)

// Deadline is a time limit with its parts separated.
//
// A deadline as a string is not queryable. Question 12 asks for every deadline
// shorter than five working days with the actor who must meet it, and that is
// one line of Cypher when the number, the unit and the calendar are fields and
// an unbounded scan of Vietnamese prose when they are not.
//
// Calendar is the field that changes outcomes. Vietnamese law distinguishes
// ngày làm việc from ngày, and five working days is nine calendar days across a
// public holiday. Reporting one as the other is the kind of error that looks
// like rounding and is not.
type Deadline struct {
	Text     string `json:"text"`               // the phrase as the provision writes it
	Value    int    `json:"value,omitempty"`    // 05 in "05 ngày làm việc"
	Unit     string `json:"unit,omitempty"`     // hour, day, month, year
	Calendar string `json:"calendar,omitempty"` // working or calendar
	Anchor   string `json:"anchor,omitempty"`   // the event it counts from or to, as written
	AnchorAt string `json:"anchor_at,omitempty"`
}

// The units a deadline may be stated in, and the two calendars.
const (
	UnitHour  = "hour"
	UnitDay   = "day"
	UnitMonth = "month"
	UnitYear  = "year"

	CalendarWorking = "working"
	CalendarNormal  = "calendar"
)

// How a deadline is anchored.
const (
	AnchorFrom   = "from"   // counted forward from an event
	AnchorBefore = "before" // counted back from an event
	AnchorBy     = "by"     // a fixed date the act must be done by
)

// Days is the deadline in days, and whether it could be expressed in days at
// all. It exists so question 12 can compare a deadline in months against one in
// days without every caller reinventing the conversion.
//
// A month is 30 days and a year is 365 here. Neither is exact and neither is
// meant to be: this is for ordering and thresholding, and any answer that turns
// on the difference between 30 and 31 needs the real calendar rather than this.
func (d Deadline) Days() (int, bool) {
	switch d.Unit {
	case UnitDay:
		return d.Value, true
	case UnitMonth:
		return d.Value * 30, true
	case UnitYear:
		return d.Value * 365, true
	}
	return 0, false
}

// Sanction is a consequence, with the provision that imposes it.
//
// LegalBasis is required. A sanction with no basis is somebody's summary of
// what probably happens, and this graph does not carry those. Where the basis
// names another instrument the edge crosses documents, which is what makes
// question 13, prohibitions with no sanction anywhere in the corpus, a query
// rather than a research project.
type Sanction struct {
	Text          string `json:"text"`
	Quote         string `json:"quote"`
	LegalBasis    string `json:"legal_basis"` // the reference as the provision writes it
	BasisDoc      string `json:"basis_doc,omitempty"`
	BasisProvison string `json:"basis_provision,omitempty"`
	ConceptID     string `json:"concept_id,omitempty"`
}

// Evidence is the byte-for-byte span that licenses the statement. Start and
// End are byte offsets into the provision text, computed here rather than
// trusted from the model.
type Evidence struct {
	Quote string `json:"quote"`
	Start int    `json:"start_char"`
	End   int    `json:"end_char"`
}

// Statement is one extracted norm.
//
// Bearer and Counterparty replace the single subject the first pass had. "Bên
// A phải thông báo cho bên B" has two actors in it and one of them owes the
// duty, and a schema with one slot forces the extractor to drop the other or to
// put it somewhere it does not belong. Which of the two owes the duty is the
// most consequential fact in the provision.
type Statement struct {
	Type         string    `json:"statement_type"`
	Bearer       *Ref      `json:"bearer,omitempty"`
	Counterparty *Ref      `json:"counterparty,omitempty"`
	Modality     string    `json:"modality,omitempty"`
	Action       Ref       `json:"action"`
	Object       *Ref      `json:"object,omitempty"`
	Conditions   []Clause  `json:"conditions,omitempty"`
	Exceptions   []Clause  `json:"exceptions,omitempty"`
	Deadline     *Deadline `json:"deadline,omitempty"`
	Sanction     *Sanction `json:"sanction,omitempty"`
	ProcedureID  string    `json:"procedure_id,omitempty"`
	Step         int       `json:"step,omitempty"`
	Evidence     Evidence  `json:"evidence"`
	Confidence   float64   `json:"confidence"`
}

// ID returns the deterministic statement identifier. It hashes the fields
// that make two statements the same claim, so re-running extraction never
// mints a second identity for the same norm.
func ID(provisionID string, s *Statement) string {
	return "vn:norm:" + store.HashBytes([]byte(provisionID + "|" + Key(s)))[:16]
}

// Key is the dedup key used by the slow mode selector: two candidates that
// agree on it are the same claim and their extractions are merged.
//
// The counterparty is in the key. Two duties on the same bearer to do the same
// thing towards different parties are two duties, and folding them together
// loses the one people ask about.
func Key(s *Statement) string {
	return strings.Join([]string{
		s.Type, slugOf(s.Bearer), law.Slug(s.Action.Text), slugOf(s.Object), slugOf(s.Counterparty),
	}, "|")
}

func slugOf(r *Ref) string {
	if r == nil {
		return ""
	}
	return law.Slug(r.Text)
}

// Normalize fills the fields this package can derive from the words the
// extractor returned, before anything is validated or judged.
//
// The deadline is derived here rather than asked for. A model asked for a
// number returns a number for every phrase including the ones that have none,
// and a deadline of five that came from "trong thời hạn hợp lý" is worse than
// no deadline at all because it answers question 12. The grammar in
// deadline.go either takes the phrase apart or leaves it as text.
//
// The empty objects go first. A model handed a schema with eight optional
// fields fills all eight, and the ones it has nothing for come back as
// {"text": ""} rather than as nothing at all. An empty string is not a claim
// about the provision, so it is dropped here rather than failing validation as
// an unnamed bearer, which is a real defect this would otherwise drown.
// Rederive reruns the parts of Normalize that are grammar over stored words,
// discarding what an earlier version of that grammar decided.
//
// This is what makes the trusted store a projection rather than a record. The
// deadline fields are derived from the phrase, so a fix to the grammar should
// reach the store by rebuilding it, not by paying a model to read the same
// provisions again. What a reviewer edits is the phrase, and the phrase is the
// input here, so their correction survives the rebuild and the parse of it
// improves along with everything else.
func Rederive(s *Statement) {
	if s.Deadline != nil {
		s.Deadline.Value, s.Deadline.Unit = 0, ""
		s.Deadline.Calendar, s.Deadline.Anchor, s.Deadline.AnchorAt = "", "", ""
	}
	Normalize(s)
}

func Normalize(s *Statement) {
	for _, ref := range []**Ref{&s.Bearer, &s.Counterparty, &s.Object} {
		if *ref != nil && blank((*ref).Text) {
			*ref = nil
		}
	}
	if s.Sanction != nil && blank(s.Sanction.Text) && blank(s.Sanction.Quote) {
		s.Sanction = nil
	}
	if s.Deadline != nil && blank(s.Deadline.Text) {
		s.Deadline = nil
	}
	s.Conditions = keepClauses(s.Conditions)
	s.Exceptions = keepClauses(s.Exceptions)

	if s.Deadline == nil {
		return
	}
	if s.Deadline.Unit != "" || s.Deadline.AnchorAt != "" {
		return
	}
	if parsed, ok := ParseDeadline(s.Deadline.Text); ok {
		*s.Deadline = *parsed
	}
}

// keepClauses drops the clauses that say nothing. A clause with no words and no
// quote is the same empty shape as an empty reference; one with words but no
// quote is a claim with no evidence, and that one is validation's to refuse.
func keepClauses(in []Clause) []Clause {
	out := in[:0]
	for _, c := range in {
		if blank(c.Text) && blank(c.Quote) {
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

// Validate enforces the hard invariants against the registry version the
// extraction cited and the provision text the evidence must quote. It returns
// the first violation, and it fills in the evidence byte offsets as a side
// effect when the quote is genuine.
func Validate(s *Statement, reg *ontology.Registry, provisionText string) error {
	if !slices.Contains(Types, s.Type) {
		return fmt.Errorf("statement type %q is not in the closed set", s.Type)
	}
	if s.Evidence.Quote == "" {
		return fmt.Errorf("statement has no evidence quote")
	}
	start := strings.Index(provisionText, s.Evidence.Quote)
	if start < 0 {
		return fmt.Errorf("evidence quote does not appear verbatim in the provision")
	}
	s.Evidence.Start = start
	s.Evidence.End = start + len(s.Evidence.Quote)
	if bearerRequired[s.Type] && (s.Bearer == nil || strings.TrimSpace(s.Bearer.Text) == "") {
		return fmt.Errorf("a %s needs a bearer and this one has none", s.Type)
	}
	if s.Bearer != nil && !s.Bearer.IsActor {
		// The flag is not decoration. A bearer that the extractor did not call
		// an actor is a bearer it did not believe in, and question 10 counts on
		// telling that apart from a bearer nobody wrote down.
		return fmt.Errorf("the bearer %q is not marked as an actor", s.Bearer.Text)
	}
	if err := validateClauses(s, provisionText); err != nil {
		return err
	}
	if err := validateSanction(s, provisionText); err != nil {
		return err
	}
	if s.Deadline != nil && strings.TrimSpace(s.Deadline.Text) == "" {
		return fmt.Errorf("a deadline with no phrase behind it is a number somebody chose")
	}
	if s.Confidence < 0 || s.Confidence > 1 {
		return fmt.Errorf("confidence %v is outside [0,1]", s.Confidence)
	}
	for _, ref := range []*Ref{s.Bearer, s.Counterparty, &s.Action, s.Object} {
		if ref == nil || ref.ClassID == "" {
			continue
		}
		if reg.Class(ref.ClassID) == nil {
			return fmt.Errorf("class %q is not in ontology v%d", ref.ClassID, reg.Version)
		}
	}
	if s.Bearer != nil && s.Bearer.ClassID != "" && bearerRequired[s.Type] {
		if !reg.IsA(s.Bearer.ClassID, "vn-legal:LegalActor") {
			return fmt.Errorf("bearer class %q of a %s must be a legal actor", s.Bearer.ClassID, s.Type)
		}
	}
	return nil
}

// validateClauses checks that every condition and exception is a kind from its
// closed set and quotes the provision. The quote is the point: a condition
// nobody can locate in the text is a condition nobody can check.
func validateClauses(s *Statement, provisionText string) error {
	for _, c := range s.Conditions {
		if !slices.Contains(ConditionKinds, c.Kind) {
			return fmt.Errorf("condition kind %q is not in the closed set", c.Kind)
		}
		if err := quotes(provisionText, c.Quote, "condition"); err != nil {
			return err
		}
	}
	for _, e := range s.Exceptions {
		if !slices.Contains(ExceptionKinds, e.Kind) {
			return fmt.Errorf("exception kind %q is not in the closed set", e.Kind)
		}
		if err := quotes(provisionText, e.Quote, "exception"); err != nil {
			return err
		}
	}
	return nil
}

// validateSanction enforces the one rule that makes a sanction a fact rather
// than a summary: it names the provision that imposes it.
func validateSanction(s *Statement, provisionText string) error {
	if s.Type == "sanction" && s.Sanction == nil {
		return fmt.Errorf("a sanction statement must name the sanction")
	}
	if s.Sanction == nil {
		return nil
	}
	if strings.TrimSpace(s.Sanction.Text) == "" {
		return fmt.Errorf("a sanction with no text is not a sanction")
	}
	if strings.TrimSpace(s.Sanction.LegalBasis) == "" {
		return fmt.Errorf("the sanction %q cites no legal basis", s.Sanction.Text)
	}
	return quotes(provisionText, s.Sanction.Quote, "sanction")
}

func quotes(text, quote, what string) error {
	if strings.TrimSpace(quote) == "" {
		return fmt.Errorf("a %s with no quote cannot be checked against the provision", what)
	}
	if !strings.Contains(text, quote) {
		return fmt.Errorf("the %s quote %q does not appear verbatim in the provision", what, quote)
	}
	return nil
}

// Judgment is one judge's verdict on one statement.
type Judgment struct {
	Verdict   string `json:"verdict"` // entailed, contradicted, partially_supported, not_enough_information
	Rationale string `json:"rationale,omitempty"`
}

// Record is one statement with its full verification state, the unit stored
// in job artifacts and, when verified, in the trusted store.
type Record struct {
	ID              string    `json:"id"`
	DocID           string    `json:"doc_id"`
	ProvisionID     string    `json:"provision_id"`
	Statement       Statement `json:"statement"`
	Status          string    `json:"status"` // verified, approved, rejected, invalid
	Invalid         string    `json:"invalid,omitempty"`
	Entailment      *Judgment `json:"entailment,omitempty"`
	Falsification   *Judgment `json:"falsification,omitempty"`
	Model           string    `json:"model,omitempty"`
	OntologyVersion int       `json:"ontology_version"`
}

// The statuses a record carries.
//
// Approved is what a human review decision leaves behind and it stands beside
// verified rather than under it. A reviewer who keeps a statement the judge
// rejected has looked at the words, and a record the person kept that every
// later query then skips would make the review queue decorative.
const (
	StatusVerified = "verified"
	StatusApproved = "approved"
	StatusRejected = "rejected"
	StatusInvalid  = "invalid"
)

// Trusted reports whether anything downstream may answer from this record.
func (r *Record) Trusted() bool {
	return r.Status == StatusVerified || r.Status == StatusApproved
}
