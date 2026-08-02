package norm

import "testing"

func index() map[string]string {
	return map[string]string{
		"nguoi-lao-dong":     "vn:concept:nguoi-lao-dong",
		"co-so-day-nghe":     "vn:concept:co-so-day-nghe",
		"hoat-dong-day-nghe": "vn:concept:hoat-dong-day-nghe",
	}
}

func TestLinkConceptsPlacesThePhrasesTheConceptLayerDecided(t *testing.T) {
	records := []Record{{Statement: Statement{
		Bearer: &Ref{Text: "người lao động"},
		Action: Ref{Text: "nộp hồ sơ"},
	}}}
	linked, total := LinkConcepts(records, index())
	if linked != 1 || total != 2 {
		t.Errorf("linked %d of %d, the bearer is a concept and the action is not", linked, total)
	}
	if records[0].Statement.Bearer.ConceptID != "vn:concept:nguoi-lao-dong" {
		t.Errorf("bearer = %+v", records[0].Statement.Bearer)
	}
	if records[0].Statement.Action.ConceptID != "" {
		t.Error("a phrase nothing matches keeps no concept rather than the nearest one")
	}
}

func TestLinkConceptsSeesThroughATrailingQualifier(t *testing.T) {
	records := []Record{{Statement: Statement{
		Bearer: &Ref{Text: "cơ sở dạy nghề quy định tại khoản 3 Điều 15 của Luật này"},
		Action: Ref{Text: "tiếp tục hoạt động dạy nghề"},
	}}}
	if linked, _ := LinkConcepts(records, index()); linked != 1 {
		t.Errorf("linked %d, a pointer stapled to an actor does not make it a different actor", linked)
	}
	if records[0].Statement.Bearer.ConceptID != "vn:concept:co-so-day-nghe" {
		t.Errorf("bearer = %+v", records[0].Statement.Bearer)
	}
	if records[0].Statement.Action.ConceptID != "" {
		t.Error("hoạt động dạy nghề is the tail of tiếp tục hoạt động dạy nghề, and a tail is not the phrase")
	}
}

func TestLinkConceptsCountsTheSanctionAsAReference(t *testing.T) {
	records := []Record{{Statement: Statement{
		Action:   Ref{Text: "trả lương"},
		Sanction: &Sanction{Text: "người lao động", LegalBasis: "Điều 17"},
	}}}
	linked, total := LinkConcepts(records, index())
	if linked != 1 || total != 2 {
		t.Errorf("linked %d of %d, a sanction names a thing and is counted with the rest", linked, total)
	}
}
