package norm

import (
	"slices"
	"testing"

	"github.com/tamnd/luatdo/ontology"
)

func TestEveryInvariantHasACodeAndFiresUnderIt(t *testing.T) {
	reg := ontology.Seed()
	cases := map[string]func(*Statement){
		ViolationType:           func(s *Statement) { s.Type = "wish" },
		ViolationEvidenceEmpty:  func(s *Statement) { s.Evidence.Quote = "" },
		ViolationEvidenceQuote:  func(s *Statement) { s.Evidence.Quote = "phải trả lương cao" },
		ViolationBearerMissing:  func(s *Statement) { s.Bearer = nil },
		ViolationBearerNotActor: func(s *Statement) { s.Bearer.IsActor = false },
		ViolationBearerClass:    func(s *Statement) { s.Bearer.ClassID = "vn-legal:Deadline" },
		ViolationClassUnknown:   func(s *Statement) { s.Bearer.ClassID = "vn-legal:Invented" },
		ViolationConfidence:     func(s *Statement) { s.Confidence = 1.5 },
		ViolationConditionKind: func(s *Statement) {
			s.Conditions = []Clause{{Kind: "vibes", Text: "x", Quote: "trừ trường hợp bất khả kháng"}}
		},
		ViolationConditionQuote: func(s *Statement) {
			s.Conditions = []Clause{{Kind: CondPrecondition, Text: "x", Quote: "khi trời mưa"}}
		},
		ViolationExceptionKind: func(s *Statement) {
			s.Exceptions = []Clause{{Kind: "vibes", Text: "x", Quote: "trừ trường hợp bất khả kháng"}}
		},
		ViolationExceptionQuote: func(s *Statement) {
			s.Exceptions = []Clause{{Kind: ExcCarveOut, Text: "x", Quote: "khi trời mưa"}}
		},
		ViolationSanctionEmpty: func(s *Statement) { s.Type = "sanction"; s.Sanction = nil },
		ViolationSanctionText: func(s *Statement) {
			s.Sanction = &Sanction{LegalBasis: "Điều 1", Quote: "phải trả lương đầy đủ"}
		},
		ViolationSanctionBasis: func(s *Statement) {
			s.Sanction = &Sanction{Text: "phạt tiền", Quote: "phải trả lương đầy đủ"}
		},
		ViolationSanctionQuote: func(s *Statement) {
			s.Sanction = &Sanction{Text: "phạt tiền", LegalBasis: "Điều 1", Quote: "phạt thật nặng"}
		},
		ViolationDeadlineEmpty: func(s *Statement) {
			s.Deadline = &Deadline{Value: 5, Unit: UnitDay, Calendar: CalendarWorking}
		},
	}
	for code, corrupt := range cases {
		if !slices.Contains(Codes, code) {
			t.Errorf("%s is not listed in Codes, so a report would never show it", code)
		}
		s := duty()
		corrupt(&s)
		vs := Violations(&s, reg, provisionText)
		if !hasCode(vs, code) {
			t.Errorf("%s did not fire, got %v", code, vs)
		}
	}
	if len(cases) != len(Codes) {
		t.Errorf("%d codes are exercised and %d exist, so one invariant is untested", len(cases), len(Codes))
	}
}

// A statement broken in two places has to report both, because the whole point
// of counting invariants is to know which one to fix first, and a first
// violation only ever reports the invariant that happens to be checked earliest.
func TestViolationsReportsEveryBreakAndValidateReportsTheFirst(t *testing.T) {
	reg := ontology.Seed()
	s := duty()
	s.Bearer = nil
	s.Confidence = 3
	vs := Violations(&s, reg, provisionText)
	if len(vs) != 2 || !hasCode(vs, ViolationBearerMissing) || !hasCode(vs, ViolationConfidence) {
		t.Fatalf("violations are %v, want both breaks", vs)
	}
	err := Validate(&s, reg, provisionText)
	if err == nil || err.Error() != vs[0].Detail {
		t.Errorf("Validate returned %v, want the first violation", err)
	}
}

// The offsets are filled by the same walk that checks the quote, and a caller
// that switched from Validate to Violations must still get them.
func TestViolationsFillsTheEvidenceOffsets(t *testing.T) {
	s := duty()
	if vs := Violations(&s, ontology.Seed(), provisionText); len(vs) != 0 {
		t.Fatalf("a valid statement reported %v", vs)
	}
	if provisionText[s.Evidence.Start:s.Evidence.End] != s.Evidence.Quote {
		t.Error("the offsets do not slice back to the quote")
	}
}

// A missing part and a wrong part are different failures with different
// repairs, and the split has to be declared rather than read off the wording.
func TestTheMandatorySetIsNamedRatherThanGuessed(t *testing.T) {
	if !Mandatory(ViolationBearerMissing) || !Mandatory(ViolationEvidenceEmpty) {
		t.Error("an absent required part is a missing mandatory attribute")
	}
	if Mandatory(ViolationBearerNotActor) || Mandatory(ViolationEvidenceQuote) {
		t.Error("a part that is present and wrong is not a missing attribute")
	}
	for _, code := range Codes {
		if Mandatory(code) && !slices.Contains(Codes, code) {
			t.Errorf("%s is mandatory but not a code", code)
		}
	}
}

func hasCode(vs []Violation, code string) bool {
	for _, v := range vs {
		if v.Code == code {
			return true
		}
	}
	return false
}
