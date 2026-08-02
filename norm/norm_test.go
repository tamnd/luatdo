package norm

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/ontology"
)

const provisionText = "Người sử dụng lao động phải trả lương đầy đủ, đúng hạn cho người lao động, trừ trường hợp bất khả kháng."

func duty() Statement {
	return Statement{
		Type:         "duty",
		Bearer:       &Ref{Text: "người sử dụng lao động", ClassID: "vn-legal:Employer", IsActor: true},
		Counterparty: &Ref{Text: "người lao động", ClassID: "vn-legal:Employee", IsActor: true},
		Modality:     "obligation",
		Action:       Ref{Text: "trả lương đầy đủ, đúng hạn"},
		Object:       &Ref{Text: "lương"},
		Evidence:     Evidence{Quote: "phải trả lương đầy đủ, đúng hạn cho người lao động"},
		Confidence:   0.94,
	}
}

func TestValidate(t *testing.T) {
	reg := ontology.Seed()
	s := duty()
	if err := Validate(&s, reg, provisionText); err != nil {
		t.Fatalf("valid statement rejected: %v", err)
	}
	if s.Evidence.Start <= 0 || s.Evidence.End <= s.Evidence.Start {
		t.Errorf("offsets not filled: %d..%d", s.Evidence.Start, s.Evidence.End)
	}
	if provisionText[s.Evidence.Start:s.Evidence.End] != s.Evidence.Quote {
		t.Error("offsets do not slice back to the quote")
	}
}

func TestValidateRejects(t *testing.T) {
	reg := ontology.Seed()
	cases := map[string]func(*Statement){
		"unknown type":         func(s *Statement) { s.Type = "wish" },
		"empty quote":          func(s *Statement) { s.Evidence.Quote = "" },
		"fabricated quote":     func(s *Statement) { s.Evidence.Quote = "phải trả lương cao" },
		"duty without bearer":  func(s *Statement) { s.Bearer = nil },
		"unknown class":        func(s *Statement) { s.Bearer.ClassID = "vn-legal:Invented" },
		"confidence above one": func(s *Statement) { s.Confidence = 1.5 },
		"non-actor bearer":     func(s *Statement) { s.Bearer.ClassID = "vn-legal:Deadline" },
		"bearer not flagged as an actor": func(s *Statement) {
			s.Bearer.IsActor = false
		},
		"condition with no quote of its own": func(s *Statement) {
			s.Conditions = []Clause{{Kind: CondPrecondition, Text: "hợp đồng còn hiệu lực"}}
		},
		"condition quoting words the provision does not have": func(s *Statement) {
			s.Conditions = []Clause{{Kind: CondPrecondition, Text: "x", Quote: "khi trời mưa"}}
		},
		"exception of a kind nobody defined": func(s *Statement) {
			s.Exceptions = []Clause{{Kind: "vibes", Text: "bất khả kháng", Quote: "trừ trường hợp bất khả kháng"}}
		},
		"sanction with no legal basis": func(s *Statement) {
			s.Sanction = &Sanction{Text: "phạt tiền", Quote: "phải trả lương đầy đủ"}
		},
		"deadline with no words behind it": func(s *Statement) {
			s.Deadline = &Deadline{Value: 5, Unit: UnitDay, Calendar: CalendarWorking}
		},
		"unnamed sanction": func(s *Statement) {
			s.Type = "sanction"
			s.Bearer = nil
			s.Action.Text = ""
		},
	}
	for name, corrupt := range cases {
		s := duty()
		corrupt(&s)
		if err := Validate(&s, reg, provisionText); err == nil {
			t.Errorf("%s passed validation", name)
		}
	}
}

func TestIDDeterministic(t *testing.T) {
	a, b := duty(), duty()
	b.Confidence = 0.5
	b.Evidence.Quote = "phải trả lương"
	idA := ID("vn:law:2019:45-2019-qh14:article-94:clause-1", &a)
	idB := ID("vn:law:2019:45-2019-qh14:article-94:clause-1", &b)
	if idA != idB {
		t.Error("the same claim must keep the same identity across runs")
	}
	if !strings.HasPrefix(idA, "vn:norm:") {
		t.Errorf("id = %q", idA)
	}
	c := duty()
	c.Type = "right"
	if ID("vn:law:2019:45-2019-qh14:article-94:clause-1", &c) == idA {
		t.Error("a different claim must get a different identity")
	}
}

func TestRegistryIsA(t *testing.T) {
	reg := ontology.Seed()
	if !reg.IsA("vn-legal:Employee", "vn-legal:LegalActor") {
		t.Error("Employee descends from LegalActor through NaturalPerson")
	}
	if reg.IsA("vn-legal:Deadline", "vn-legal:LegalActor") {
		t.Error("Deadline is not an actor")
	}
	if !reg.IsA("vn-legal:LegalActor", "vn-legal:LegalActor") {
		t.Error("a class is itself")
	}
}

func TestNormalizeTakesTheDeadlineApartFromThePhrase(t *testing.T) {
	s := duty()
	s.Deadline = &Deadline{Text: "trong thời hạn 05 ngày làm việc kể từ ngày nhận đủ hồ sơ"}
	Normalize(&s)
	if s.Deadline.Value != 5 || s.Deadline.Calendar != CalendarWorking {
		t.Errorf("deadline = %+v, the fields are derived from the words rather than asked for", s.Deadline)
	}
	if s.Deadline.Anchor != "nhận đủ hồ sơ" || s.Deadline.AnchorAt != AnchorFrom {
		t.Errorf("anchor = %+v, a deadline with nothing to count from cannot be checked", s.Deadline)
	}
}

func TestNormalizeLeavesAPhraseWithNoNumberWithoutOne(t *testing.T) {
	s := duty()
	s.Deadline = &Deadline{Text: "trong thời hạn hợp lý"}
	Normalize(&s)
	if _, ok := s.Deadline.Days(); ok {
		t.Errorf("deadline = %+v, a number here would answer question 12 with a rule nobody wrote", s.Deadline)
	}
}

func TestNormalizeDropsTheShapesTheModelFilledWithNothing(t *testing.T) {
	s := duty()
	s.Type = "definition"
	s.Bearer = &Ref{Text: ""}
	s.Counterparty = &Ref{Text: "   "}
	s.Object = &Ref{Text: ""}
	s.Sanction = &Sanction{}
	s.Deadline = &Deadline{Text: ""}
	s.Conditions = []Clause{{Kind: CondPrecondition}}
	Normalize(&s)
	if s.Bearer != nil || s.Counterparty != nil || s.Object != nil {
		t.Errorf("statement = %+v, an empty string is not a claim about the provision", s)
	}
	if s.Sanction != nil || s.Deadline != nil || s.Conditions != nil {
		t.Errorf("statement = %+v, the empty optional objects go before validation sees them", s)
	}
	if err := Validate(&s, ontology.Seed(), provisionText); err != nil {
		t.Errorf("a definition with nothing but empty shapes around it was rejected: %v", err)
	}
}

func TestNormalizeKeepsTheClaimThatHasWordsButNoEvidence(t *testing.T) {
	s := duty()
	s.Conditions = []Clause{{Kind: CondPrecondition, Text: "hợp đồng còn hiệu lực"}}
	Normalize(&s)
	if len(s.Conditions) != 1 {
		t.Fatal("a condition with words in it is a claim, and dropping it would hide the failure")
	}
	if err := Validate(&s, ontology.Seed(), provisionText); err == nil {
		t.Error("a claim with no evidence is validation's to refuse, and it did not")
	}
}

func TestNormalizeKeepsWhatSomebodyAlreadyDecided(t *testing.T) {
	s := duty()
	s.Deadline = &Deadline{Text: "trong thời hạn 05 ngày", Value: 3, Unit: UnitDay, Calendar: CalendarWorking}
	Normalize(&s)
	if s.Deadline.Value != 3 {
		t.Error("a deadline that already carries fields was corrected by a person, and the grammar does not overrule them")
	}
}

func TestRederiveTakesTheDeadlineApartAgainWithTodaysGrammar(t *testing.T) {
	s := duty()
	// What an older grammar left behind: it found the anchor and gave up on
	// the length, because it only read digits.
	s.Deadline = &Deadline{
		Text:     "Trong thời hạn mười lăm ngày, kể từ ngày nhận đủ hồ sơ",
		AnchorAt: AnchorFrom, Anchor: "nhận đủ hồ sơ",
	}
	Rederive(&s)
	if s.Deadline.Value != 15 || s.Deadline.Unit != UnitDay {
		t.Errorf("deadline = %+v, a fix to the grammar has to reach the store without paying a model again", s.Deadline)
	}
	if s.Deadline.Anchor != "nhận đủ hồ sơ" {
		t.Errorf("deadline = %+v, the anchor is derived from the same words and comes back with them", s.Deadline)
	}
}

func TestNearDuplicatesGroupsOneClaimWrittenWithTwoBearers(t *testing.T) {
	bare, qualified := Record{ProvisionID: "p1", Statement: duty()}, Record{ProvisionID: "p1", Statement: duty()}
	qualified.Statement.Bearer = &Ref{Text: "người sử dụng lao động là doanh nghiệp"}
	groups := NearDuplicates([]Record{bare, qualified})
	if len(groups) != 1 || len(groups[0]) != 2 {
		t.Fatalf("groups = %v, one norm written two ways answers the same question twice", groups)
	}
}

func TestNearDuplicatesLeavesTwoClaimsThatShareAVerbAlone(t *testing.T) {
	wages, notice := Record{ProvisionID: "p1", Statement: duty()}, Record{ProvisionID: "p1", Statement: duty()}
	notice.Statement.Object = &Ref{Text: "tiền thưởng"}
	if groups := NearDuplicates([]Record{wages, notice}); len(groups) != 0 {
		t.Errorf("groups = %v, paying wages and paying a bonus are two duties and a key that folds them overcounts", groups)
	}
}

func TestNearDuplicatesKeepsProvisionsApart(t *testing.T) {
	here, there := Record{ProvisionID: "p1", Statement: duty()}, Record{ProvisionID: "p2", Statement: duty()}
	if groups := NearDuplicates([]Record{here, there}); len(groups) != 0 {
		t.Errorf("groups = %v, the same duty stated in two articles is stated in two articles", groups)
	}
}
