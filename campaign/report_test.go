package campaign

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/route"
)

const code = "vn:law:2019:45-2019-qh14"
const decree = "vn:decree:2022:12-2022-nd-cp"

func record(status, statementType string, fill func(*norm.Statement)) norm.Record {
	r := norm.Record{
		DocID: code, ProvisionID: code + ":article-1", Status: status,
		Statement: norm.Statement{
			Type:   statementType,
			Bearer: &norm.Ref{Text: "người sử dụng lao động", ClassID: "vn-legal:Employer", IsActor: true},
			Action: norm.Ref{Text: "trả lương"},
		},
	}
	if fill != nil {
		fill(&r.Statement)
	}
	return r
}

func scope() Scope { return Scope{Name: "test", Subject: "lao-dong"} }

func TestReportCountsWhatTheCampaignHasNotReached(t *testing.T) {
	inScope := map[string]bool{code: true, decree: true}
	r := Compile(scope(), inScope,
		map[string]int{code: 40, decree: 20}, map[string]int{code: 12},
		nil, nil, nil)
	if r.Documents != 2 || r.Reached != 1 {
		t.Errorf("report = %+v, one of the two documents has a job and the other has none", r)
	}
	if r.Extractable != 60 || r.Extracted != 12 {
		t.Errorf("report = %+v", r)
	}
	out := r.String()
	if !strings.Contains(out, "48 left in the queue") || !strings.Contains(out, "1 untouched") {
		t.Errorf("the report has to lead with the gap:\n%s", out)
	}
}

func TestReportSeparatesTheJudgeFromTheReviewer(t *testing.T) {
	records := []norm.Record{
		record(norm.StatusVerified, "duty", nil),
		record(norm.StatusApproved, "duty", nil),
		record(norm.StatusRejected, "duty", nil),
	}
	r := Compile(scope(), map[string]bool{code: true}, nil, nil, records, nil, nil)
	if r.Statements != 3 || r.Verified != 1 || r.Approved != 1 || r.Rejected != 1 {
		t.Errorf("report = %+v, a statement a person kept is not one the judge passed", r)
	}
	if r.ByType["duty"] != 3 {
		t.Errorf("by type = %v, the breakdown is over everything extracted", r.ByType)
	}
}

func TestReportCountsOnlyTheDocumentsInScope(t *testing.T) {
	out := record(norm.StatusVerified, "duty", nil)
	out.DocID = "vn:law:2015:91-2015-qh13"
	r := Compile(scope(), map[string]bool{code: true}, nil, nil,
		[]norm.Record{record(norm.StatusVerified, "duty", nil), out}, nil, nil)
	if r.Statements != 1 {
		t.Errorf("statements = %d, a record from another area of law is not this campaign's", r.Statements)
	}
}

func TestReportTellsAParsedDeadlineFromAPhrase(t *testing.T) {
	records := []norm.Record{
		record(norm.StatusVerified, "duty", func(s *norm.Statement) {
			s.Deadline = &norm.Deadline{Text: "trong thời hạn 03 ngày làm việc"}
			norm.Normalize(s)
		}),
		record(norm.StatusVerified, "duty", func(s *norm.Statement) {
			s.Deadline = &norm.Deadline{Text: "trong thời hạn hợp lý"}
			norm.Normalize(s)
		}),
	}
	r := Compile(scope(), map[string]bool{code: true}, nil, nil, records, nil, nil)
	if r.Deadlines != 2 || r.Parsed != 1 || r.Short != 1 {
		t.Errorf("report = %+v, a phrase with no number in it is a deadline nobody can check", r)
	}
}

func TestReportAnswersTheOpenQuestionsFromTheTrustedRecordsOnly(t *testing.T) {
	forbidden := record(norm.StatusVerified, "prohibition", func(s *norm.Statement) {
		s.Action = norm.Ref{Text: "giữ giấy tờ tùy thân"}
	})
	rejected := record(norm.StatusRejected, "prohibition", func(s *norm.Statement) {
		s.Action = norm.Ref{Text: "ép buộc người lao động"}
	})
	r := Compile(scope(), map[string]bool{code: true}, nil, nil,
		[]norm.Record{forbidden, rejected}, nil, nil)
	if r.Unsanctioned != 1 {
		t.Errorf("unsanctioned = %d, the rejected prohibition is not in the graph to be unpunished", r.Unsanctioned)
	}
}

func TestReportResolvesSanctionsAcrossTheScope(t *testing.T) {
	r := Compile(scope(), map[string]bool{code: true}, nil, nil,
		[]norm.Record{record(norm.StatusVerified, "duty", func(s *norm.Statement) {
			s.Sanction = &norm.Sanction{Text: "phạt tiền", LegalBasis: "khoản 2 Điều 17 Nghị định số 12/2022/NĐ-CP"}
		})}, nil,
		map[string]string{"12/2022/NĐ-CP": decree})
	if r.Sanctions.CrossDoc != 1 {
		t.Errorf("sanctions = %+v, the penalty for a labour duty lives in the decree", r.Sanctions)
	}
}

func TestAccountRefusesToPriceWhatHasNoRateCard(t *testing.T) {
	var r Report
	r.Account([]Summary{
		{Usage: api.Usage{TotalTokens: 100}, Cost: route.Cost{USD: 0.5, Available: true}},
		{Usage: api.Usage{TotalTokens: 50}, Cost: route.Cost{Available: false}},
	})
	if r.Runs != 2 || r.Tokens != 150 {
		t.Errorf("report = %+v", r)
	}
	if r.Cost != "unavailable" {
		t.Errorf("cost = %q, a total that drops the unpriced half is worse than no total", r.Cost)
	}
}

func TestReportNamesItsOwnDefectsWithTheirNumbers(t *testing.T) {
	records := []norm.Record{
		record(norm.StatusVerified, "duty", func(s *norm.Statement) {
			s.Deadline = &norm.Deadline{Text: "trong thời hạn hợp lý"}
			norm.Normalize(s)
		}),
	}
	r := Compile(scope(), map[string]bool{code: true}, nil, nil, records, nil, nil)
	out := r.String()
	if !strings.Contains(out, "unparsed-deadlines") || !strings.Contains(out, "no-concept-layer") {
		t.Errorf("a report over records with an unreadable deadline and no concept links knows two things are wrong:\n%s", out)
	}
	if !strings.Contains(out, "1 of 1 deadline phrases") {
		t.Errorf("a defect without its numbers is a sentence nobody can check:\n%s", out)
	}
}

func TestReportSaysAnEmptyDefectsSectionIsNotACleanBillOfHealth(t *testing.T) {
	clean := record(norm.StatusVerified, "duty", func(s *norm.Statement) {
		s.Bearer.ConceptID = "vn:concept:nguoi-su-dung-lao-dong"
		s.Action.ConceptID = "vn:concept:tra-luong"
	})
	r := Compile(scope(), map[string]bool{code: true}, nil, nil, []norm.Record{clean}, nil, nil)
	if len(r.Defects) != 0 {
		t.Fatalf("defects = %+v, nothing this report checks is wrong with that record", r.Defects)
	}
	if !strings.Contains(r.String(), "which is not the same as none") {
		t.Errorf("silence about defects reads as an absence of them:\n%s", r.String())
	}
}

func TestReportCountsAStatementTooBrokenToJudgeApartFromOneTheJudgeRejected(t *testing.T) {
	records := []norm.Record{
		record(norm.StatusRejected, "duty", nil),
		record(norm.StatusInvalid, "duty", nil),
	}
	r := Compile(scope(), map[string]bool{code: true}, nil, nil, records, nil, nil)
	if r.Rejected != 1 || r.Invalid != 1 {
		t.Errorf("report = %+v, a statement the validator threw out never reached the judge", r)
	}
	if !strings.Contains(r.String(), "1 never valid enough to judge") {
		t.Errorf("folding those two together flatters the judge:\n%s", r.String())
	}
}

func TestReportSaysHowManyReferencesTheConceptLayerCouldHavePlaced(t *testing.T) {
	linked := record(norm.StatusVerified, "duty", func(s *norm.Statement) {
		s.Bearer.ConceptID = "vn:concept:nguoi-su-dung-lao-dong"
	})
	r := Compile(scope(), map[string]bool{code: true}, nil, nil,
		[]norm.Record{linked}, nil, nil)
	if r.References != 2 || r.Conceptual != 1 {
		t.Errorf("report = %+v, a bare count of links reads the same whether nothing matched or nothing was tried", r)
	}
	if !strings.Contains(r.String(), "1 of 2 references") {
		t.Errorf("the denominator belongs in the line:\n%s", r.String())
	}
}
