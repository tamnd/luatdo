package concept

import (
	"strings"
	"testing"
)

func TestScoringSeparatesAMissedTermFromAnInventedOne(t *testing.T) {
	gold := []Gold{{
		UnitID: "u1", DocID: "d", ScopeID: "d", TextHash: "aa",
		Terms: []GoldTerm{
			{LabelVI: "Người lao động", Genus: "người làm việc", Kind: KindActor},
			{LabelVI: "Người sử dụng lao động", Genus: "doanh nghiệp", Kind: KindActor},
		},
		AnnotatedBy: "tamnd",
	}}
	jobs := []Job{{
		UnitID: "u1", DocID: "d", ScopeID: "d", TextHash: "aa",
		TermUses: []TermUse{
			{LabelVI: "Người lao động", Genus: "người làm việc cho người sử dụng lao động", Kind: KindActor},
			{LabelVI: "Hợp đồng lao động", Genus: "thỏa thuận", Kind: KindArtifact},
		},
	}}

	m := Score(gold, jobs)
	if m.Definitions != (Count{TP: 1, FP: 1, FN: 1}) {
		t.Fatalf("definitions = %+v", m.Definitions)
	}
	if got := m.Definitions.Precision(); got != 0.5 {
		t.Errorf("precision = %v", got)
	}
	// Genus is scored on containment, because an annotator writes the shortest
	// phrase that names the category and a longer span of the same phrase is
	// not a mistake.
	if m.Genus != (Accuracy{Right: 1, Of: 1}) {
		t.Errorf("genus = %+v", m.Genus)
	}
	if m.Kind != (Accuracy{Right: 1, Of: 1}) {
		t.Errorf("kind = %+v", m.Kind)
	}
}

func TestAnAliasCountsAsTheTermItNames(t *testing.T) {
	gold := []Gold{{
		UnitID: "u1", TextHash: "aa",
		Terms: []GoldTerm{{
			LabelVI: "Ủy ban nhân dân tỉnh, thành phố trực thuộc trung ương",
			Aliases: []string{"Ủy ban nhân dân cấp tỉnh"},
			Kind:    KindBody,
		}},
	}}
	jobs := []Job{{
		UnitID: "u1", TextHash: "aa",
		TermUses: []TermUse{{LabelVI: "Ủy ban nhân dân cấp tỉnh", Kind: KindBody}},
	}}
	m := Score(gold, jobs)
	// Taking the drafter's short form as the label is one debatable choice, not
	// two errors, and scoring it as a miss plus an invention would make the
	// numbers unreadable.
	if m.Definitions != (Count{TP: 1}) {
		t.Errorf("definitions = %+v", m.Definitions)
	}
}

func TestRoleErrorsAreCountedInBothDirections(t *testing.T) {
	gold := []Gold{{
		UnitID: "u1", TextHash: "aa",
		Terms: []GoldTerm{
			{LabelVI: "Cơ quan có thẩm quyền", Kind: KindActor, IsRole: true},
			{LabelVI: "Bộ Tài chính", Kind: KindBody},
		},
	}}
	jobs := []Job{{
		UnitID: "u1", TextHash: "aa",
		TermUses: []TermUse{
			{LabelVI: "Cơ quan có thẩm quyền", Kind: KindActor},
			{LabelVI: "Bộ Tài chính", Kind: KindBody, IsRole: true},
		},
	}}
	m := Score(gold, jobs)
	if m.Role != (Accuracy{Of: 2}) {
		t.Errorf("role = %+v", m.Role)
	}
	// A missed role resolves globally and puts one ministry in every provision
	// that says co quan co tham quyen. An invented role does the opposite and
	// makes a named body unresolvable. They are different failures and the
	// report keeps them apart.
	if m.RolesMissed != 1 || m.RolesInvented != 1 {
		t.Errorf("missed %d, invented %d", m.RolesMissed, m.RolesInvented)
	}
}

func TestAnnotationsAgainstAmendedTextAreNotScored(t *testing.T) {
	gold := []Gold{
		{UnitID: "u1", TextHash: "aa", Terms: []GoldTerm{{LabelVI: "A", Kind: KindActor}}},
		{UnitID: "u2", TextHash: "bb", Terms: []GoldTerm{{LabelVI: "B", Kind: KindActor}}},
	}
	jobs := []Job{{UnitID: "u1", TextHash: "cc", TermUses: []TermUse{{LabelVI: "A", Kind: KindActor}}}}

	m := Score(gold, jobs)
	if m.Scored != 0 || len(m.Stale) != 1 || len(m.Missing) != 1 {
		t.Fatalf("metrics = %+v", m)
	}
	// Scoring a reading of amended text against an annotation of the old text
	// produces a number that is about neither, so it is refused and said out
	// loud in the report.
	if !strings.Contains(m.String(), "since changed") {
		t.Errorf("the report hides the stale annotation:\n%s", m)
	}
	if !strings.Contains(m.String(), "never read") {
		t.Errorf("the report hides the unread unit:\n%s", m)
	}
}

func TestAClauseThatDefinesNothingIsScoredOnItsOwn(t *testing.T) {
	gold := []Gold{
		{UnitID: "u1", TextHash: "aa", DefinesNothing: true},
		{UnitID: "u2", TextHash: "aa", DefinesNothing: true},
		{UnitID: "u3", TextHash: "aa", Terms: []GoldTerm{{LabelVI: "A", Kind: KindActor}}},
	}
	jobs := []Job{
		{UnitID: "u1", TextHash: "aa", DefinesNo: true},
		{UnitID: "u2", TextHash: "aa", TermUses: []TermUse{{LabelVI: "X", Kind: KindActor}}},
		{UnitID: "u3", TextHash: "aa", DefinesNo: true},
	}
	m := Score(gold, jobs)
	if m.DefinesNothing != (Count{TP: 1, FP: 1, FN: 1}) {
		t.Errorf("defines nothing = %+v", m.DefinesNothing)
	}
	if m.Definitions.FP != 1 || m.Definitions.FN != 1 {
		t.Errorf("definitions = %+v", m.Definitions)
	}
}

func TestByReferenceAndEnumerationsAreScored(t *testing.T) {
	gold := []Gold{{
		UnitID: "u1", TextHash: "aa",
		Terms: []GoldTerm{
			{LabelVI: "Các từ ngữ khác", Kind: KindStatus, DefinesByReference: "Luật Bảo hiểm xã hội"},
			{LabelVI: "Phương tiện giao thông", Kind: KindArtifact,
				EnumeratedSubtypes: []string{"xe ô tô", "xe mô tô", "xe gắn máy"}},
		},
	}}
	jobs := []Job{{
		UnitID: "u1", TextHash: "aa",
		TermUses: []TermUse{
			{LabelVI: "Các từ ngữ khác", Kind: KindStatus,
				DefinesByReference: &Reference{Instrument: "Luật Bảo hiểm xã hội"}},
			{LabelVI: "Phương tiện giao thông", Kind: KindArtifact,
				EnumeratedSubtypes: []string{"xe ô tô", "xe mô tô", "xe đạp điện"}},
		},
	}}
	m := Score(gold, jobs)
	if m.ByReference != (Count{TP: 1}) {
		t.Errorf("by reference = %+v", m.ByReference)
	}
	if m.Enumerations != (Count{TP: 2, FP: 1, FN: 1}) {
		t.Errorf("enumerations = %+v", m.Enumerations)
	}
}

func TestOverMergeAndUnderMergeAreCountedApart(t *testing.T) {
	gold := []GoldPair{
		{A: "t:a", B: "t:b", Verdict: RelationDiffers},
		{A: "t:c", B: "t:d", Verdict: RelationSame},
		{A: "t:e", B: "t:f", Verdict: RelationSame},
		{A: "t:g", B: "t:h", Verdict: RelationBroader},
		{A: "t:i", B: "t:j", Verdict: RelationSame},
	}
	comparisons := []Comparison{
		{A: "t:a", B: "t:b", Relation: RelationSame},     // over merge
		{A: "t:c", B: "t:d", Relation: RelationDiffers},  // under merge
		{A: "t:e", B: "t:f", Relation: RelationSame},     // agreed
		{A: "t:h", B: "t:g", Relation: RelationNarrower}, // agreed, asked the other way round
		{A: "t:i", B: "t:j", Relation: RelationUnclear},  // no opinion
	}
	m := ScoreMerges(gold, comparisons)
	if m.OverMerge != 1 || m.UnderMerge != 1 || m.Agreed != 2 || m.Unclear != 1 {
		t.Fatalf("merge metrics = %+v", m)
	}
	// An over merge destroys a distinction the corpus contains and an under
	// merge leaves two nodes a later pass can still join, so a single accuracy
	// number would hide the difference that matters.
	report := m.String()
	if !strings.Contains(report, "1 over merge") || !strings.Contains(report, "1 under merge") {
		t.Errorf("report = %s", report)
	}
}

func TestTheGoldSetSurvivesTheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := []Gold{{
		UnitID: "vn:doc:a:article-3:clause-1", DocID: "vn:doc:a", ScopeID: "vn:doc:a",
		TextHash: "aa", Text: clause,
		Terms:       []GoldTerm{{LabelVI: "Người lao động", Genus: "người làm việc", Kind: KindActor}},
		AnnotatedBy: "tamnd", AnnotatedAt: "2026-08-01T00:00:00Z",
	}}
	if err := WriteGold(dir, want); err != nil {
		t.Fatalf("WriteGold: %v", err)
	}
	got, err := ReadGold(dir)
	if err != nil || len(got) != 1 || got[0].Terms[0].LabelVI != want[0].Terms[0].LabelVI {
		t.Fatalf("ReadGold = %+v, %v", got, err)
	}
	if got[0].Text != clause {
		t.Error("the annotated text was not stored, so a later run cannot tell the clause changed under it")
	}

	pairs := []GoldPair{{A: "t:a", B: "t:b", Verdict: RelationDiffers, Rationale: "phạm vi khác nhau", AnnotatedBy: "tamnd"}}
	if err := WriteGoldPairs(dir, pairs); err != nil {
		t.Fatalf("WriteGoldPairs: %v", err)
	}
	gotPairs, err := ReadGoldPairs(dir)
	if err != nil || len(gotPairs) != 1 {
		t.Fatalf("ReadGoldPairs = %+v, %v", gotPairs, err)
	}
	if empty, err := ReadGold(t.TempDir()); err != nil || empty != nil {
		t.Errorf("an unannotated directory read as %+v, %v", empty, err)
	}
}
