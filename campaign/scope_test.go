package campaign

import (
	"testing"

	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/subject"
)

func doc(id, docType, body, effective string) *law.Document {
	return &law.Document{ID: id, DocType: docType, IssuingBody: body, EffectiveFrom: effective, Status: "parsed"}
}

func under(id string, subjects ...string) subject.Record {
	r := subject.Record{DocID: id}
	for _, s := range subjects {
		r.Subjects = append(r.Subjects, subject.Assignment{SubjectID: s, Confidence: 0.7})
	}
	return r
}

func TestScopeTakesTheSubdomainsOfItsSubject(t *testing.T) {
	docs := []*law.Document{
		doc("a", "Nghị định", "Chính phủ", "2021-01-01"),
		doc("b", "Thông tư", "Bộ Lao động - Thương binh và Xã hội", "2022-01-01"),
		doc("c", "Nghị định", "Chính phủ", "2021-01-01"),
	}
	records := []subject.Record{
		under("a", "lao-dong"),
		under("b", "lao-dong/hop-dong-lao-dong", "lao-dong"),
		under("c", "dat-dai"),
	}
	got := Scopes["labour-2025"].Documents(records, docs)
	if len(got) != 2 || !got["a"] || !got["b"] {
		t.Errorf("scope = %v, a subdomain of the subject is inside it and another domain is not", got)
	}
}

func TestScopeDropsWhatAProvinceIssued(t *testing.T) {
	docs := []*law.Document{
		doc("a", "Quyết định", "Bộ Lao động - Thương binh và Xã hội", "2021-01-01"),
		doc("b", "Quyết định", "UBND Tỉnh Lào Cai", "2021-01-01"),
		doc("c", "Nghị quyết", "Hội đồng nhân dân tỉnh Hưng Yên", "2021-01-01"),
	}
	records := []subject.Record{under("a", "lao-dong"), under("b", "lao-dong"), under("c", "lao-dong")}
	got := Scopes["labour-2025"].Documents(records, docs)
	if len(got) != 1 || !got["a"] {
		t.Errorf("scope = %v, the cut is on who signed it and a ministry decision is central", got)
	}
}

func TestScopeDropsWhatTakesEffectAfterIt(t *testing.T) {
	docs := []*law.Document{
		doc("a", "Nghị định", "Chính phủ", "2026-07-01"),
		doc("b", "Nghị định", "Chính phủ", ""),
	}
	records := []subject.Record{under("a", "lao-dong"), under("b", "lao-dong")}
	got := Scopes["labour-2025"].Documents(records, docs)
	if got["a"] {
		t.Error("a document that takes effect in 2026 is not part of a campaign named for 2025")
	}
	if !got["b"] {
		t.Error("a document with no effective date is kept, since dropping it would hide it rather than date it")
	}
}

func TestScopeDropsWhatNobodyClassified(t *testing.T) {
	docs := []*law.Document{doc("a", "Nghị định", "Chính phủ", "2021-01-01")}
	got := Scopes["labour-2025"].Documents(nil, docs)
	if len(got) != 0 {
		t.Errorf("scope = %v, a campaign that swept up the unclassified would be the whole corpus wearing a name", got)
	}
}

func TestScopeDropsWhatWasQuarantined(t *testing.T) {
	d := doc("a", "Nghị định", "Chính phủ", "2021-01-01")
	d.Status = "quarantined"
	got := Scopes["labour-2025"].Documents([]subject.Record{under("a", "lao-dong")}, []*law.Document{d})
	if len(got) != 0 {
		t.Errorf("scope = %v, a document the parser refused is not one to extract from", got)
	}
}

func TestInScopeKeepsTheQueueEntriesOfTheCampaign(t *testing.T) {
	tasks := []coverage.Task{
		{ProvisionID: "a:article-1", DocID: "a"},
		{ProvisionID: "c:article-1", DocID: "c"},
	}
	got := InScope(tasks, map[string]bool{"a": true})
	if len(got) != 1 || got[0].DocID != "a" {
		t.Errorf("queue = %+v", got)
	}
}

func TestLookupScopeNamesWhatItKnows(t *testing.T) {
	if _, err := LookupScope("nothing"); err == nil {
		t.Fatal("an unknown campaign is an error, not an empty scope that silently covers everything")
	} else if got := err.Error(); got == "" || !contains(got, "labour-2025") {
		t.Errorf("error = %q, it has to say what the alternatives are", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
