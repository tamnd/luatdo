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

// Ref is one participant of a statement: the surface text and, when the
// extractor could place it, a registry class.
type Ref struct {
	Text    string `json:"text"`
	ClassID string `json:"class_id,omitempty"`
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
type Statement struct {
	Type       string   `json:"statement_type"`
	Subject    *Ref     `json:"subject,omitempty"`
	Modality   string   `json:"modality,omitempty"`
	Action     Ref      `json:"action"`
	Object     *Ref     `json:"object,omitempty"`
	Conditions []string `json:"conditions,omitempty"`
	Exceptions []string `json:"exceptions,omitempty"`
	Deadline   string   `json:"deadline,omitempty"`
	Sanction   string   `json:"sanction,omitempty"`
	Evidence   Evidence `json:"evidence"`
	Confidence float64  `json:"confidence"`
}

// ID returns the deterministic statement identifier. It hashes the fields
// that make two statements the same claim, so re-running extraction never
// mints a second identity for the same norm.
func ID(provisionID string, s *Statement) string {
	subject := ""
	if s.Subject != nil {
		subject = law.Slug(s.Subject.Text)
	}
	object := ""
	if s.Object != nil {
		object = law.Slug(s.Object.Text)
	}
	key := strings.Join([]string{provisionID, s.Type, subject, law.Slug(s.Action.Text), object}, "|")
	return "vn:norm:" + store.HashBytes([]byte(key))[:16]
}

// Key is the dedup key used by the slow mode selector: two candidates that
// agree on it are the same claim and their extractions are merged.
func Key(s *Statement) string {
	subject := ""
	if s.Subject != nil {
		subject = law.Slug(s.Subject.Text)
	}
	object := ""
	if s.Object != nil {
		object = law.Slug(s.Object.Text)
	}
	return strings.Join([]string{s.Type, subject, law.Slug(s.Action.Text), object}, "|")
}

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
	if bearerRequired[s.Type] && (s.Subject == nil || strings.TrimSpace(s.Subject.Text) == "") {
		return fmt.Errorf("a %s needs a bearer and this one has no subject", s.Type)
	}
	if s.Type == "sanction" && strings.TrimSpace(s.Sanction) == "" && strings.TrimSpace(s.Action.Text) == "" {
		return fmt.Errorf("a sanction statement must name the sanction")
	}
	if s.Confidence < 0 || s.Confidence > 1 {
		return fmt.Errorf("confidence %v is outside [0,1]", s.Confidence)
	}
	for _, ref := range []*Ref{s.Subject, &s.Action, s.Object} {
		if ref == nil || ref.ClassID == "" {
			continue
		}
		if reg.Class(ref.ClassID) == nil {
			return fmt.Errorf("class %q is not in ontology v%d", ref.ClassID, reg.Version)
		}
	}
	if s.Subject != nil && s.Subject.ClassID != "" && bearerRequired[s.Type] {
		if !reg.IsA(s.Subject.ClassID, "vn-legal:LegalActor") {
			return fmt.Errorf("subject class %q of a %s must be a legal actor", s.Subject.ClassID, s.Type)
		}
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
	Status          string    `json:"status"` // verified, rejected, invalid
	Invalid         string    `json:"invalid,omitempty"`
	Entailment      *Judgment `json:"entailment,omitempty"`
	Falsification   *Judgment `json:"falsification,omitempty"`
	Model           string    `json:"model,omitempty"`
	OntologyVersion int       `json:"ontology_version"`
}
