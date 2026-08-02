package norm

import (
	"strings"
	"testing"
)

func norm9(action string, sanction *Sanction) Record {
	r := Record{
		ID: "vn:norm:" + law9(action), DocID: labourCode,
		ProvisionID: labourCode + ":article-" + law9(action), Status: "verified",
		Statement: Statement{
			Type:     "duty",
			Bearer:   &Ref{Text: "người sử dụng lao động", ClassID: "vn-legal:Employer", IsActor: true},
			Action:   Ref{Text: action},
			Sanction: sanction,
		},
	}
	return r
}

func law9(action string) string {
	return strings.ReplaceAll(action, " ", "-")
}

func TestQuestion9SeparatesTheDutiesNothingBacks(t *testing.T) {
	records := []Record{
		norm9("trả lương", &Sanction{Text: "phạt tiền", BasisDoc: penaltyDecree}),
		norm9("giao kết hợp đồng", nil),
	}
	// A worker duty, to check the bearer filter keeps it out.
	other := norm9("làm việc", nil)
	other.Statement.Bearer = &Ref{Text: "người lao động", ClassID: "vn-legal:Employee", IsActor: true}
	records = append(records, other)

	q := AskQuestion9(records, labourCode, "vn-legal:Employer")
	if len(q.Rows) != 2 {
		t.Fatalf("rows = %d, the worker duty belongs to another bearer", len(q.Rows))
	}
	if q.Unsanctioned != 1 {
		t.Errorf("unsanctioned = %d, one of the two names no consequence", q.Unsanctioned)
	}
	if !strings.Contains(q.String(), penaltyDecree) {
		t.Errorf("rendering hides where the consequence lives:\n%s", q.String())
	}
}

func TestQuestion9AnswersFromWhatAReviewerKept(t *testing.T) {
	r := norm9("trả lương", nil)
	r.Status = StatusApproved
	if q := AskQuestion9([]Record{r}, labourCode, ""); len(q.Rows) != 1 {
		t.Errorf("rows = %d, a person looked at this one and kept it", len(q.Rows))
	}
}

func TestQuestion9MatchesTheBearerOnItsWordsWhenNoClassWasGiven(t *testing.T) {
	r := norm9("trả lương", nil)
	r.Statement.Bearer.ClassID = ""
	if q := AskQuestion9([]Record{r}, "", "người sử dụng lao động"); len(q.Rows) != 1 {
		t.Errorf("rows = %d, a bearer the registry never placed is still a bearer", len(q.Rows))
	}
}

func TestQuestion10TellsADraftingChoiceFromAnExtractionFailure(t *testing.T) {
	silent := norm9("bảo đảm an toàn", nil)
	silent.Statement.Bearer = nil
	silent.ProvisionID = labourCode + ":article-1"
	missed := norm9("trả trợ cấp", nil)
	missed.Statement.Bearer = nil
	missed.ProvisionID = labourCode + ":article-2"

	text := map[string]string{
		labourCode + ":article-1": "Phải bảo đảm an toàn tại nơi làm việc.",
		labourCode + ":article-2": "Người sử dụng lao động phải trả trợ cấp thôi việc.",
	}
	q := AskQuestion10([]Record{silent, missed}, text)
	if len(q.Rows) != 2 {
		t.Fatalf("rows = %d", len(q.Rows))
	}
	if q.Rows[0].Cause != CauseDrafting {
		t.Errorf("cause = %q, the provision names nobody in its own words", q.Rows[0].Cause)
	}
	if q.Rows[1].Cause != CauseExtraction {
		t.Errorf("cause = %q, the actor is right there in the sentence and was not taken", q.Rows[1].Cause)
	}
}

func TestQuestion10FallsBackToTheWeakerClaimWithNoText(t *testing.T) {
	r := norm9("trả trợ cấp", nil)
	r.Statement.Bearer = nil
	q := AskQuestion10([]Record{r}, nil)
	if q.Rows[0].Cause != CauseExtraction && q.Rows[0].Cause != CauseDrafting {
		t.Fatalf("cause = %q", q.Rows[0].Cause)
	}
	if q.Rows[0].Cause != CauseDrafting {
		t.Errorf("cause = %q, with no text to read the accusation is the one that blames nobody", q.Rows[0].Cause)
	}
}

func TestQuestion11ReturnsTheStepsInAnOrderSomebodyCouldFollow(t *testing.T) {
	records := []Record{
		step("2", 2, "nhận giấy phép", "cấp giấy phép xây dựng"),
		step("1", 1, "nộp hồ sơ", "cấp giấy phép xây dựng"),
		step("1", 1, "đăng ký kinh doanh", "thành lập doanh nghiệp"),
	}
	q := AskQuestion11(records, map[string]int{}, "giấy phép xây dựng")
	if len(q.Procedures) != 1 {
		t.Fatalf("procedures = %d, the other procedure does not match the query", len(q.Procedures))
	}
	out := q.String()
	if strings.Index(out, "nộp hồ sơ") > strings.Index(out, "nhận giấy phép") {
		t.Errorf("steps came out in the wrong order:\n%s", out)
	}
}

func timed(article, phrase string) Record {
	r := Record{
		DocID: labourCode, ProvisionID: labourCode + ":article-" + article, Status: "verified",
		Statement: Statement{
			Type:     "duty",
			Bearer:   &Ref{Text: "người sử dụng lao động", IsActor: true},
			Action:   Ref{Text: "trả lương"},
			Deadline: &Deadline{Text: phrase},
		},
	}
	Normalize(&r.Statement)
	return r
}

func TestQuestion12CountsWorkingDaysAgainstCalendarDays(t *testing.T) {
	records := []Record{
		timed("1", "trong thời hạn 03 ngày làm việc"),
		timed("2", "trong thời hạn 30 ngày"),
		// Six calendar days is shorter than five working days, which span seven.
		timed("3", "trong thời hạn 06 ngày"),
		timed("4", "trong thời hạn hợp lý"),
	}
	q := AskQuestion12(records, 5)
	if len(q.Rows) != 2 {
		t.Fatalf("rows = %+v, six calendar days falls inside five working days", q.Rows)
	}
	if q.Unparsed != 1 {
		t.Errorf("unparsed = %d, the phrase with no number is missing from the answer and has to be said so", q.Unparsed)
	}
	if !strings.Contains(q.String(), "could not be taken apart") {
		t.Errorf("rendering does not admit what it skipped:\n%s", q.String())
	}
}

func TestQuestion12NamesTheActorWhoHasToMeetTheDeadline(t *testing.T) {
	q := AskQuestion12([]Record{timed("1", "trong thời hạn 03 ngày làm việc")}, 5)
	if q.Rows[0].Bearer != "người sử dụng lao động" {
		t.Errorf("bearer = %q, a deadline nobody owes is not actionable", q.Rows[0].Bearer)
	}
}

func TestQuestion13ReportsTheShareAsWellAsTheList(t *testing.T) {
	forbid := func(action, conceptID string) Record {
		return Record{
			DocID: labourCode, ProvisionID: labourCode + ":article-1", Status: "verified",
			Statement: Statement{
				Type:   "prohibition",
				Bearer: &Ref{Text: "người sử dụng lao động", IsActor: true},
				Action: Ref{Text: action, ConceptID: conceptID},
			},
		}
	}
	records := []Record{
		forbid("giữ giấy tờ tùy thân", "vn:concept:giu-giay-to"),
		forbid("ép buộc người lao động", "vn:concept:ep-buoc"),
		{
			DocID: penaltyDecree, Status: "verified",
			Statement: Statement{
				Type:     "sanction",
				Action:   Ref{Text: "hành vi khác"},
				Sanction: &Sanction{Text: "phạt tiền", LegalBasis: "Điều 9", ConceptID: "vn:concept:giu-giay-to"},
			},
		},
	}
	q := AskQuestion13(records)
	if q.Prohibited != 2 || len(q.Rows) != 1 {
		t.Errorf("question 13 = %d of %d, one prohibition of the two goes unpunished", len(q.Rows), q.Prohibited)
	}
}

func TestQuestion14ReadsBackWhatHoldsAndWhatReleases(t *testing.T) {
	r := Record{
		ID: "vn:norm:abc", DocID: labourCode, Status: "verified",
		Statement: Statement{
			Type:       "duty",
			Bearer:     &Ref{Text: "người sử dụng lao động", IsActor: true},
			Action:     Ref{Text: "trả lương đúng hạn"},
			Conditions: []Clause{{Kind: CondPrecondition, Text: "hợp đồng còn hiệu lực", Quote: "khi hợp đồng còn hiệu lực"}},
			Exceptions: []Clause{{Kind: ExcForce, Text: "bất khả kháng", Quote: "trừ trường hợp bất khả kháng"}},
		},
	}
	q := AskQuestion14([]Record{r}, "vn:norm:abc")
	if len(q.Conditions) != 1 || len(q.Exceptions) != 1 {
		t.Fatalf("question 14 = %+v", q)
	}
	out := q.String()
	if !strings.Contains(out, ExcForce) || !strings.Contains(out, CondPrecondition) {
		t.Errorf("the kinds are what make the two lists usable, and they are not in:\n%s", out)
	}
	if missing := AskQuestion14([]Record{r}, "vn:norm:nothing"); missing.Statement != nil {
		t.Error("a statement nobody stored came back with a body")
	}
}

func TestQuestion15SkipsTheAuthorityTheCorpusDefines(t *testing.T) {
	vagueOne := norm9("cấp giấy phép", nil)
	vagueOne.Statement.Bearer = &Ref{Text: "cơ quan có thẩm quyền", IsActor: true}
	vagueOne.ProvisionID = labourCode + ":article-1"

	defined := norm9("thanh tra", nil)
	defined.Statement.Bearer = &Ref{Text: "cơ quan nhà nước có thẩm quyền", IsActor: true}
	defined.ProvisionID = labourCode + ":article-2"

	named := norm9("ban hành", nil)
	named.ProvisionID = labourCode + ":article-3"

	q := AskQuestion15([]Record{vagueOne, defined, named}, map[string]bool{"co-quan-nha-nuoc-co-tham-quyen": true})
	if len(q.Rows) != 1 {
		t.Fatalf("rows = %+v, a phrase some instrument defines is one a reader can follow", q.Rows)
	}
	if q.Rows[0].Text != "cơ quan có thẩm quyền" {
		t.Errorf("row = %+v", q.Rows[0])
	}
}

func TestQuestion15SkipsTheReferenceTheConceptLayerResolved(t *testing.T) {
	r := norm9("cấp giấy phép", nil)
	r.Statement.Bearer = &Ref{Text: "cơ quan có thẩm quyền", ConceptID: "vn:concept:so-xay-dung", IsActor: true}
	if q := AskQuestion15([]Record{r}, nil); len(q.Rows) != 0 {
		t.Errorf("rows = %+v, this one was resolved and is not a hole", q.Rows)
	}
}

func TestQuestionsIgnoreWhatWasNotVerified(t *testing.T) {
	r := norm9("trả lương", nil)
	r.Status = "rejected"
	r.Statement.Deadline = &Deadline{Text: "trong thời hạn 01 ngày", Value: 1, Unit: UnitDay, Calendar: CalendarNormal}
	r.Statement.Bearer = &Ref{Text: "cơ quan có thẩm quyền", IsActor: true}
	records := []Record{r}
	if q := AskQuestion9(records, labourCode, ""); len(q.Rows) != 0 {
		t.Errorf("question 9 answered from a rejected statement: %+v", q.Rows)
	}
	if q := AskQuestion12(records, 5); len(q.Rows) != 0 {
		t.Errorf("question 12 answered from a rejected statement: %+v", q.Rows)
	}
	if q := AskQuestion15(records, nil); len(q.Rows) != 0 {
		t.Errorf("question 15 answered from a rejected statement: %+v", q.Rows)
	}
	r.Statement.Bearer = nil
	if q := AskQuestion10(records, nil); len(q.Rows) != 0 {
		t.Errorf("question 10 answered from a rejected statement: %+v", q.Rows)
	}
}
