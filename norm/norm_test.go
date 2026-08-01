package norm

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/ontology"
)

const provisionText = "Người sử dụng lao động phải trả lương đầy đủ, đúng hạn cho người lao động, trừ trường hợp bất khả kháng."

func duty() Statement {
	return Statement{
		Type:       "duty",
		Subject:    &Ref{Text: "người sử dụng lao động", ClassID: "vn-legal:Employer"},
		Modality:   "obligation",
		Action:     Ref{Text: "trả lương đầy đủ, đúng hạn"},
		Object:     &Ref{Text: "người lao động", ClassID: "vn-legal:Employee"},
		Evidence:   Evidence{Quote: "phải trả lương đầy đủ, đúng hạn cho người lao động"},
		Confidence: 0.94,
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
		"duty without bearer":  func(s *Statement) { s.Subject = nil },
		"unknown class":        func(s *Statement) { s.Subject.ClassID = "vn-legal:Invented" },
		"confidence above one": func(s *Statement) { s.Confidence = 1.5 },
		"non-actor bearer":     func(s *Statement) { s.Subject.ClassID = "vn-legal:Deadline" },
		"unnamed sanction": func(s *Statement) {
			s.Type = "sanction"
			s.Subject = nil
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
