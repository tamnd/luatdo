package norm

import "testing"

func step(id string, n int, action, procedure string) Record {
	return Record{
		ID: id, DocID: labourCode, ProvisionID: labourCode + ":article-" + id, Status: "verified",
		Statement: Statement{
			Type:        "procedure",
			Bearer:      &Ref{Text: "chủ đầu tư", IsActor: true},
			Action:      Ref{Text: action},
			ProcedureID: procedure,
			Step:        n,
		},
	}
}

func TestGroupProceduresOrdersTheStepsAndRenumbersThem(t *testing.T) {
	records := []Record{
		step("3", 3, "nhận giấy phép", "cấp phép xây dựng"),
		step("1", 1, "nộp hồ sơ", "cấp phép xây dựng"),
		step("2", 2, "thẩm định hồ sơ", "cấp phép xây dựng"),
	}
	got := GroupProcedures(records, map[string]int{})
	if len(got) != 1 {
		t.Fatalf("procedures = %d, the three steps are one procedure", len(got))
	}
	p := got[0]
	if p.DocID != labourCode || p.Label != "cấp phép xây dựng" {
		t.Errorf("procedure = %+v", p)
	}
	want := []string{"nộp hồ sơ", "thẩm định hồ sơ", "nhận giấy phép"}
	for i, w := range want {
		if p.Steps[i].Action != w {
			t.Errorf("step %d = %q, want %q, an unfollowable order is not an answer", i+1, p.Steps[i].Action, w)
		}
		if p.Steps[i].Number != i+1 {
			t.Errorf("step %d numbered %d, the steps are renumbered from one", i+1, p.Steps[i].Number)
		}
	}
}

func TestGroupProceduresBreaksTiesByWhereTheProvisionSits(t *testing.T) {
	// Two calls both said step 1, which is what independent calls do, and the
	// document knows the order neither of them could see.
	records := []Record{step("9", 1, "nhận giấy phép", "cấp phép"), step("4", 1, "nộp hồ sơ", "cấp phép")}
	position := map[string]int{labourCode + ":article-4": 4, labourCode + ":article-9": 9}
	got := GroupProcedures(records, position)
	if got[0].Steps[0].Action != "nộp hồ sơ" {
		t.Errorf("first step = %q, the document order decides when the numbers do not", got[0].Steps[0].Action)
	}
}

func TestGroupProceduresKeepsTwoDocumentsApart(t *testing.T) {
	a := step("1", 1, "nộp hồ sơ", "cấp phép")
	b := step("1", 1, "nộp đơn", "cấp phép")
	b.DocID = penaltyDecree
	b.ProvisionID = penaltyDecree + ":article-1"
	got := GroupProcedures([]Record{a, b}, map[string]int{})
	if len(got) != 2 {
		t.Fatalf("procedures = %d, two documents that reuse a name describe two procedures", len(got))
	}
}

func TestGroupProceduresIgnoresWhatWasNotVerified(t *testing.T) {
	r := step("1", 1, "nộp hồ sơ", "cấp phép")
	r.Status = "rejected"
	if got := GroupProcedures([]Record{r}, map[string]int{}); len(got) != 0 {
		t.Errorf("procedures = %+v, a rejected statement is not a step anybody can follow", got)
	}
}
