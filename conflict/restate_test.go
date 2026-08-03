package conflict

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/norm"
)

func TestRestateSaysTheModality(t *testing.T) {
	cases := []struct {
		op, want string
	}{
		{Obligation, "phải thông báo"},
		{Prohibition, "không được thông báo"},
		{Permission, "được thông báo"},
		{Right, "có quyền thông báo"},
	}
	for _, c := range cases {
		got := Restate(form("a", c.op))
		if !strings.Contains(got, c.want) {
			t.Errorf("a %s reads %q, want it to contain %q", c.op, got, c.want)
		}
	}
}

func TestRestateCarriesEverythingThatDecidesAConflict(t *testing.T) {
	f := form("a", Obligation)
	f.Canon.Object = "quyết định"
	f.Canon.Toward = "người lao động"
	f.Deadline = &norm.Deadline{Text: "30 ngày", Value: 30, Unit: norm.UnitDay, Anchor: "ngày ký hợp đồng"}
	f.Sanction = "phat-tien"
	f.Scope.Conditions = []string{"trong-truong-hop-khan-cap"}
	f.Scope.Exceptions = []string{"bat-kha-khang"}
	f.Scope.Defers = []string{"vn:doc:nghi-dinh-145"}
	f.Scope.From, f.Scope.To = "2021-01-01", "2025-01-01"

	got := Restate(f)
	for _, want := range []string{
		"Người sử dụng lao động phải thông báo quyết định cho người lao động",
		"trong thời hạn 30 ngày kể từ ngày ký hợp đồng",
		"khi trong truong hop khan cap",
		"trừ trường hợp bat kha khang",
		"Vi phạm thì bị phat tien",
		"thực hiện theo vn:doc:nghi-dinh-145",
		"từ ngày 2021-01-01 đến trước ngày 2025-01-01",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the restatement is missing %q:\n%s", want, got)
		}
	}
}

// Provisions often write the event into the deadline phrase, and the anchor is
// then the same words again.
func TestRestateDoesNotSayTheAnchorTwice(t *testing.T) {
	f := form("a", Obligation)
	f.Deadline = &norm.Deadline{
		Text:   "chậm nhất là năm ngày trước khi có những thay đổi trên",
		Anchor: "có những thay đổi trên",
	}
	got := Restate(f)
	if strings.Count(got, "có những thay đổi trên") != 1 {
		t.Errorf("the anchor is repeated: %s", got)
	}
	if strings.Contains(got, "trong thời hạn trong thời hạn") {
		t.Errorf("the lead phrase is repeated: %s", got)
	}
}

// A norm that was always in force says nothing about dates, because a line about
// it on every one of the 671 forms in the corpus would be noise.
func TestRestateIsQuietAboutAnOpenInterval(t *testing.T) {
	got := Restate(form("a", Obligation))
	if strings.Contains(got, "hiệu lực") {
		t.Errorf("an open interval was written out anyway: %s", got)
	}
	if want := "Người sử dụng lao động phải thông báo."; got != want {
		t.Errorf("restated %q, want %q", got, want)
	}
}

func TestRestateSurvivesAnEmptyForm(t *testing.T) {
	if got := Restate(nil); got != "" {
		t.Errorf("a missing form restated to %q", got)
	}
	if got := Restate(&Form{}); got != "." {
		t.Errorf("an empty form restated to %q", got)
	}
}

// The generated side of every case has to state its own mutation. It has no
// sentence in the corpus, and a case whose second norm reads as a placeholder
// cannot be answered by anybody, which is what made the first baseline run score
// zero on all sixty conflicting pairs.
func TestEveryGeneratedCaseStatesBothNorms(t *testing.T) {
	for _, c := range Build(seeds(), 0) {
		for _, side := range []*Form{c.A, c.B} {
			quote := side.Source.Quote
			if quote != Restate(side) {
				t.Errorf("%s: the stored sentence is not the norm:\n%s\n%s", c.ID, quote, Restate(side))
			}
			party, act, _ := side.Words()
			lower := strings.ToLower(quote)
			if !strings.Contains(lower, strings.ToLower(unslug(party))) ||
				!strings.Contains(lower, strings.ToLower(unslug(act))) {
				t.Errorf("%s: %q states neither the party nor the act", c.ID, quote)
			}
			if !strings.Contains(quote, operatorWord(side.Operator)) {
				t.Errorf("%s: %q does not say the modality", c.ID, quote)
			}
		}
		if c.A.Source.Quote == c.B.Source.Quote && c.Mutation != MutRestated {
			t.Errorf("%s: both sides read the same, so the mutation is invisible:\n%s", c.ID, c.A.Source.Quote)
		}
	}
}

func TestUpperFirstLeavesTheRestAlone(t *testing.T) {
	cases := map[string]string{
		"":                    "",
		"đơn vị":              "Đơn vị",
		"người lao động":      "Người lao động",
		"Đã viết hoa sẵn rồi": "Đã viết hoa sẵn rồi",
	}
	for in, want := range cases {
		if got := upperFirst(in); got != want {
			t.Errorf("upperFirst(%q) = %q, want %q", in, got, want)
		}
	}
}
