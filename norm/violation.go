package norm

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tamnd/luatdo/ontology"
)

// Violation is one broken invariant.
//
// The code is the reason a Violation exists at all. Validate has always
// returned an error whose text names the problem, and a distribution counted
// over those texts is a distribution over English: "the bearer %q is not marked
// as an actor" produces a distinct string per bearer, so counting them says
// there are fifty rare problems where there is one common one. The code is
// stable, the detail keeps the words a person needs to see the case.
type Violation struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (v Violation) Error() string { return v.Detail }

// The invariant codes, one per way a statement can fail.
const (
	ViolationType           = "type-not-in-closed-set"
	ViolationEvidenceEmpty  = "evidence-quote-missing"
	ViolationEvidenceQuote  = "evidence-quote-not-verbatim"
	ViolationBearerMissing  = "bearer-missing"
	ViolationBearerNotActor = "bearer-not-marked-actor"
	ViolationBearerClass    = "bearer-class-not-legal-actor"
	ViolationConditionKind  = "condition-kind-not-in-closed-set"
	ViolationConditionQuote = "condition-quote"
	ViolationExceptionKind  = "exception-kind-not-in-closed-set"
	ViolationExceptionQuote = "exception-quote"
	ViolationSanctionEmpty  = "sanction-missing"
	ViolationSanctionText   = "sanction-text-missing"
	ViolationSanctionBasis  = "sanction-legal-basis-missing"
	ViolationSanctionQuote  = "sanction-quote"
	ViolationDeadlineEmpty  = "deadline-phrase-missing"
	ViolationConfidence     = "confidence-out-of-range"
	ViolationClassUnknown   = "class-not-in-registry"
)

// Codes lists every invariant in a fixed order, so a report shows the ones that
// never fired as zero rather than omitting them. An invariant that never fires
// is either dead code or a problem the extractor does not have, and those two
// look identical in a report that only lists what happened.
var Codes = []string{
	ViolationType,
	ViolationEvidenceEmpty,
	ViolationEvidenceQuote,
	ViolationBearerMissing,
	ViolationBearerNotActor,
	ViolationBearerClass,
	ViolationConditionKind,
	ViolationConditionQuote,
	ViolationExceptionKind,
	ViolationExceptionQuote,
	ViolationSanctionEmpty,
	ViolationSanctionText,
	ViolationSanctionBasis,
	ViolationSanctionQuote,
	ViolationDeadlineEmpty,
	ViolationConfidence,
	ViolationClassUnknown,
}

// mandatory is the set of invariants that fire because a required part of the
// statement is absent, as opposed to present and wrong.
//
// The distinction is not cosmetic. The extraction literature predicts that
// missing mandatory attributes are the most common schema violation a model
// produces, and that prediction is only testable against a set somebody wrote
// down before counting.
var mandatory = map[string]bool{
	ViolationEvidenceEmpty: true,
	ViolationBearerMissing: true,
	ViolationSanctionEmpty: true,
	ViolationSanctionText:  true,
	ViolationSanctionBasis: true,
	ViolationDeadlineEmpty: true,
}

// Mandatory reports whether a code is a missing mandatory attribute.
func Mandatory(code string) bool { return mandatory[code] }

func violate(code, format string, args ...any) Violation {
	return Violation{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// Violations returns every invariant a statement breaks, in the order the
// invariants are checked.
//
// A check that depends on an earlier one is skipped when the earlier one
// failed, because a condition quote cannot be located in a provision whose own
// evidence quote was never found, and reporting both would count one defect
// twice. It fills in the evidence byte offsets when the quote is genuine, which
// is the same side effect Validate has always had.
func Violations(s *Statement, reg *ontology.Registry, provisionText string) []Violation {
	var out []Violation
	if !slices.Contains(Types, s.Type) {
		out = append(out, violate(ViolationType, "statement type %q is not in the closed set", s.Type))
	}
	if s.Evidence.Quote == "" {
		out = append(out, violate(ViolationEvidenceEmpty, "statement has no evidence quote"))
	} else if start := strings.Index(provisionText, s.Evidence.Quote); start < 0 {
		out = append(out, violate(ViolationEvidenceQuote, "evidence quote does not appear verbatim in the provision"))
	} else {
		s.Evidence.Start = start
		s.Evidence.End = start + len(s.Evidence.Quote)
	}
	if bearerRequired[s.Type] && (s.Bearer == nil || strings.TrimSpace(s.Bearer.Text) == "") {
		out = append(out, violate(ViolationBearerMissing, "a %s needs a bearer and this one has none", s.Type))
	}
	if s.Bearer != nil && !s.Bearer.IsActor {
		out = append(out, violate(ViolationBearerNotActor, "the bearer %q is not marked as an actor", s.Bearer.Text))
	}
	out = append(out, clauseViolations(s, provisionText)...)
	out = append(out, sanctionViolations(s, provisionText)...)
	if s.Deadline != nil && strings.TrimSpace(s.Deadline.Text) == "" {
		out = append(out, violate(ViolationDeadlineEmpty, "a deadline with no phrase behind it is a number somebody chose"))
	}
	if s.Confidence < 0 || s.Confidence > 1 {
		out = append(out, violate(ViolationConfidence, "confidence %v is outside [0,1]", s.Confidence))
	}
	for _, ref := range []*Ref{s.Bearer, s.Counterparty, &s.Action, s.Object} {
		if ref == nil || ref.ClassID == "" {
			continue
		}
		if reg.Class(ref.ClassID) == nil {
			out = append(out, violate(ViolationClassUnknown, "class %q is not in ontology v%d", ref.ClassID, reg.Version))
		}
	}
	if s.Bearer != nil && s.Bearer.ClassID != "" && bearerRequired[s.Type] && reg.Class(s.Bearer.ClassID) != nil {
		if !reg.IsA(s.Bearer.ClassID, "vn-legal:LegalActor") {
			out = append(out, violate(ViolationBearerClass,
				"bearer class %q of a %s must be a legal actor", s.Bearer.ClassID, s.Type))
		}
	}
	return out
}

func clauseViolations(s *Statement, provisionText string) []Violation {
	var out []Violation
	for _, c := range s.Conditions {
		if !slices.Contains(ConditionKinds, c.Kind) {
			out = append(out, violate(ViolationConditionKind, "condition kind %q is not in the closed set", c.Kind))
		}
		if err := quotes(provisionText, c.Quote, "condition"); err != nil {
			out = append(out, violate(ViolationConditionQuote, "%s", err))
		}
	}
	for _, e := range s.Exceptions {
		if !slices.Contains(ExceptionKinds, e.Kind) {
			out = append(out, violate(ViolationExceptionKind, "exception kind %q is not in the closed set", e.Kind))
		}
		if err := quotes(provisionText, e.Quote, "exception"); err != nil {
			out = append(out, violate(ViolationExceptionQuote, "%s", err))
		}
	}
	return out
}

func sanctionViolations(s *Statement, provisionText string) []Violation {
	if s.Type == "sanction" && s.Sanction == nil {
		return []Violation{violate(ViolationSanctionEmpty, "a sanction statement must name the sanction")}
	}
	if s.Sanction == nil {
		return nil
	}
	var out []Violation
	if strings.TrimSpace(s.Sanction.Text) == "" {
		out = append(out, violate(ViolationSanctionText, "a sanction with no text is not a sanction"))
	}
	if strings.TrimSpace(s.Sanction.LegalBasis) == "" {
		out = append(out, violate(ViolationSanctionBasis, "the sanction %q cites no legal basis", s.Sanction.Text))
	}
	if err := quotes(provisionText, s.Sanction.Quote, "sanction"); err != nil {
		out = append(out, violate(ViolationSanctionQuote, "%s", err))
	}
	return out
}
