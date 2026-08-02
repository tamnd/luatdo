package norm

import "testing"

const unit = "vn:law:2019:45-2019-qh14:article-97:clause-1"

func annotation() Gold {
	return Gold{
		UnitID: unit, DocID: labourCode, TextHash: "h1",
		Text:        "Người lao động được trả lương trực tiếp, đầy đủ, đúng hạn.",
		Duoc:        SensePassiveRight,
		Statements:  []GoldStatement{{Type: "right", Bearer: "người lao động", Action: "được trả lương đúng hạn"}},
		AnnotatedBy: "tamnd", AnnotatedAt: "2026-08-01",
	}
}

func extracted(statementType, bearer, action string) Record {
	return Record{
		DocID: labourCode, ProvisionID: unit, Status: "verified",
		Statement: Statement{
			Type:     statementType,
			Bearer:   &Ref{Text: bearer, IsActor: true},
			Action:   Ref{Text: action},
			Evidence: Evidence{Quote: "được trả lương trực tiếp, đầy đủ, đúng hạn"},
		},
	}
}

func hashes() map[string]string { return map[string]string{unit: "h1"} }

func TestScoreCountsARightReadAsARight(t *testing.T) {
	m := Score([]Gold{annotation()}, []Record{extracted("right", "người lao động", "được trả lương đúng hạn")}, hashes())
	if m.Scored != 1 || m.Statements.TP != 1 {
		t.Fatalf("metrics = %+v", m)
	}
	if m.Types.Right != 1 || m.Bearers.Right != 1 {
		t.Errorf("type and bearer both agree, got %+v and %+v", m.Types, m.Bearers)
	}
	if m.Duoc.Right != 1 {
		t.Errorf("được scored %+v, the passive reading is the annotated one", m.Duoc)
	}
}

// The trap, as a test. "Người lao động được trả lương" is a right of the worker
// stated in the passive voice. Reading it as a permission moves the obligation
// off the employer, and the point of the confusion table is that this shows up
// as a named swap rather than as a rate that stays high.
func TestScoreCatchesAPassiveRightReadAsAPermission(t *testing.T) {
	m := Score([]Gold{annotation()}, []Record{extracted("permission", "người lao động", "được trả lương đúng hạn")}, hashes())
	if m.Duoc.Right != 0 || m.Duoc.Of != 1 {
		t.Errorf("được scored %+v, want one decided and none right", m.Duoc)
	}
	if m.DuocConfusion[SensePassiveRight][SensePermission] != 1 {
		t.Errorf("confusion = %v, the swap has to be nameable", m.DuocConfusion)
	}
	if m.Statements.TP != 1 || m.Types.Right != 0 {
		t.Errorf("the statement was found and its type is wrong, got %+v and %+v", m.Statements, m.Types)
	}
}

func TestScoreSeparatesAMissingBearerFromAWrongOne(t *testing.T) {
	r := extracted("right", "", "được trả lương đúng hạn")
	r.Statement.Bearer = nil
	m := Score([]Gold{annotation()}, []Record{r}, hashes())
	if m.BearersMissed != 1 || m.Bearers.Of != 0 {
		t.Errorf("metrics = %+v, no bearer and the wrong bearer are different failures", m)
	}
}

func TestScoreWillNotScoreAgainstTextThatChanged(t *testing.T) {
	m := Score([]Gold{annotation()}, nil, map[string]string{unit: "h2"})
	if len(m.Stale) != 1 || m.Scored != 0 {
		t.Errorf("metrics = %+v, an annotation of amended words is not evidence about this corpus", m)
	}
}

func TestScoreTellsAClauseNobodyReadFromOneWithNothingInIt(t *testing.T) {
	m := Score([]Gold{annotation()}, nil, nil)
	if len(m.Missing) != 1 {
		t.Fatalf("metrics = %+v, a clause the pass never reached is missing", m)
	}
	m = Score([]Gold{annotation()}, nil, hashes())
	if len(m.Missing) != 0 || m.Statements.FN != 1 {
		t.Errorf("metrics = %+v, a clause the pass read and found nothing in is a miss, not a gap", m)
	}
}

func TestCheckGoldRefusesAnAnnotationNobodyCouldScoreAgainst(t *testing.T) {
	cases := map[string]func(*Gold){
		"no unit":             func(g *Gold) { g.UnitID = "" },
		"no hash":             func(g *Gold) { g.TextHash = "" },
		"no annotator":        func(g *Gold) { g.AnnotatedBy = "" },
		"invented sense":      func(g *Gold) { g.Duoc = "vibes" },
		"invented type":       func(g *Gold) { g.Statements[0].Type = "wish" },
		"silent":              func(g *Gold) { g.Statements = nil },
		"both and neither":    func(g *Gold) { g.StatesNoNorm = true },
		"statement no action": func(g *Gold) { g.Statements[0].Action = "" },
	}
	for name, corrupt := range cases {
		g := annotation()
		corrupt(&g)
		if problems := CheckGold([]Gold{g}); len(problems) == 0 {
			t.Errorf("%s passed the check on the ruler itself", name)
		}
	}
	if problems := CheckGold([]Gold{annotation()}); len(problems) != 0 {
		t.Errorf("a sound annotation was rejected: %v", problems)
	}
}

func TestGoldRoundTrips(t *testing.T) {
	dir := t.TempDir()
	if err := WriteGold(dir, []Gold{annotation()}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadGold(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 || got[0].Duoc != SensePassiveRight || len(got[0].Statements) != 1 {
		t.Errorf("annotation did not come back as it went in: %+v", got)
	}
}
