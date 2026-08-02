package legalruleml

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/norm"
)

func duty() norm.Record {
	return norm.Record{
		ID: "vn:norm:aaaa1111", DocID: "vn:law:2019:45-2019-qh14",
		ProvisionID: "vn:law:2019:45-2019-qh14:article-97:clause-1",
		Status:      norm.StatusVerified,
		Statement: norm.Statement{
			Type:     "duty",
			Bearer:   &norm.Ref{Text: "người sử dụng lao động", ClassID: "employer", IsActor: true},
			Action:   norm.Ref{Text: "trả lương", ConceptID: "vn:concept:wage-payment"},
			Object:   &norm.Ref{Text: "tiền lương"},
			Evidence: norm.Evidence{Quote: "Người sử dụng lao động phải trả lương", Start: 0, End: 37},
		},
	}
}

func export(t *testing.T, records ...norm.Record) (string, Summary) {
	t.Helper()
	var buf bytes.Buffer
	s, err := Export(&buf, Input{Campaign: "labour-2025", Note: "the labour code", Records: records})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	return buf.String(), s
}

// The file has to parse. A format nobody can load is a format nobody can check,
// and hand written XML over Vietnamese prose full of quotation marks and
// ampersands is exactly where that goes wrong.
func TestTheFileIsWellFormedXML(t *testing.T) {
	r := duty()
	r.Statement.Object = &norm.Ref{Text: `lương & phụ cấp <đặc biệt> "theo hợp đồng"`}
	r.Statement.Conditions = []norm.Clause{{Kind: norm.CondTemporal, Text: "khi đến kỳ trả lương", Quote: "khi đến kỳ trả lương"}}
	out, _ := export(t, r)
	d := xml.NewDecoder(strings.NewReader(out))
	for {
		_, err := d.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("the export does not parse: %v\n%s", err, out)
		}
	}
	if !strings.Contains(out, "&amp;") || !strings.Contains(out, "&lt;đặc biệt&gt;") {
		t.Errorf("the prose was not escaped:\n%s", out)
	}
}

func TestEachStatementTypeGetsItsOwnOperator(t *testing.T) {
	for typ, want := range map[string]string{
		"duty":        "lrml:Obligation",
		"prohibition": "lrml:Prohibition",
		"permission":  "lrml:Permission",
		"right":       "lrml:Right",
	} {
		r := duty()
		r.Statement.Type = typ
		out, s := export(t, r)
		if !strings.Contains(out, "<"+want+">") {
			t.Errorf("a %s was not written as %s:\n%s", typ, want, out)
		}
		if s.Prescriptive != 1 {
			t.Errorf("a %s counted %d prescriptive statements", typ, s.Prescriptive)
		}
	}
}

// A definition states what a word means and does not place a duty on anybody.
// Giving it a deontic operator would put an obligation in the file that no
// provision states.
func TestADefinitionIsConstitutiveAndNotDeontic(t *testing.T) {
	r := duty()
	r.Statement.Type = "definition"
	out, s := export(t, r)
	if !strings.Contains(out, "<lrml:ConstitutiveStatement") {
		t.Errorf("a definition was not written as a constitutive statement:\n%s", out)
	}
	if strings.Contains(out, "lrml:Obligation") {
		t.Errorf("a definition was written as an obligation:\n%s", out)
	}
	if s.Constitutive != 1 || s.Prescriptive != 0 {
		t.Errorf("summary = %+v", s)
	}
}

// The types that are parts of other norms are counted and named rather than
// dropped quietly, because a reader comparing this file against the campaign
// report has to be able to account for the difference.
func TestThePartsOfOtherNormsAreSkippedAndCounted(t *testing.T) {
	var records []norm.Record
	for _, typ := range []string{"sanction", "procedure", "exception"} {
		r := duty()
		r.Statement.Type = typ
		records = append(records, r)
	}
	out, s := export(t, records...)
	if s.Statements != 0 {
		t.Errorf("%d statements were written from parts of other norms", s.Statements)
	}
	for _, typ := range []string{"sanction", "procedure", "exception"} {
		if s.Skipped[typ] != 1 {
			t.Errorf("%s was not counted as skipped: %+v", typ, s.Skipped)
		}
		if !strings.Contains(s.String(), skipReason[typ]) {
			t.Errorf("the summary does not say why %s was skipped: %s", typ, s.String())
		}
	}
	if strings.Contains(out, "PrescriptiveStatement") {
		t.Errorf("a part of another norm reached the statements:\n%s", out)
	}
}

// The whole design point: an unformalised condition carries its words rather
// than becoming a predicate somebody would then try to evaluate.
func TestAConditionCarriesItsWordsAndClaimsNoPredicate(t *testing.T) {
	r := duty()
	r.Statement.Conditions = []norm.Clause{{Kind: norm.CondThreshold, Text: "nếu sử dụng từ 10 người lao động trở lên"}}
	r.Statement.Exceptions = []norm.Clause{{Kind: norm.ExcForce, Text: "trừ trường hợp bất khả kháng"}}
	out, s := export(t, r)
	if !strings.Contains(out, `iri="https://luatdo.dev/ns#condition"`) {
		t.Errorf("the condition did not become a named relation:\n%s", out)
	}
	if !strings.Contains(out, "nếu sử dụng từ 10 người lao động trở lên") {
		t.Errorf("the condition lost its words:\n%s", out)
	}
	if !strings.Contains(out, "<ruleml:Naf>") {
		t.Errorf("the exception was not written as a defeater:\n%s", out)
	}
	if s.Conditions != 1 || s.Exceptions != 1 {
		t.Errorf("summary = %+v", s)
	}
	// The header has to say this, because the file travels alone.
	if !strings.Contains(out, "cannot evaluate the qualification") {
		t.Errorf("the file does not tell its reader what the conditions are:\n%s", out)
	}
}

// A deadline is the one qualification the pipeline does parse into parts, and
// the calendar is the part that changes the answer.
func TestADeadlineKeepsItsUnitAndItsCalendar(t *testing.T) {
	r := duty()
	r.Statement.Deadline = &norm.Deadline{Text: "05 ngày làm việc", Value: 5, Unit: "day", Calendar: "working"}
	out, s := export(t, r)
	if !strings.Contains(out, "<ruleml:Data>5</ruleml:Data>") {
		t.Errorf("the deadline lost its number:\n%s", out)
	}
	if !strings.Contains(out, "<ruleml:Ind>working</ruleml:Ind>") {
		t.Errorf("the deadline lost its calendar, so five working days reads as five days:\n%s", out)
	}
	if s.Deadlines != 1 {
		t.Errorf("summary = %+v", s)
	}
}

// The association is what makes this a legal document rather than a rule base:
// every statement points at the provision that states it.
func TestEveryStatementIsAssociatedWithTheProvisionThatStatesIt(t *testing.T) {
	out, s := export(t, duty())
	if s.Sources != 1 {
		t.Fatalf("summary = %+v", s)
	}
	key := sourceKey("vn:law:2019:45-2019-qh14:article-97:clause-1")
	if !strings.Contains(out, `key="`+key+`"`) || !strings.Contains(out, `keyref="#`+key+`"`) {
		t.Errorf("the statement is not joined to its source:\n%s", out)
	}
	if !strings.Contains(out, `sameAs="https://luatdo.dev/id/vn:law:2019:45-2019-qh14:article-97:clause-1"`) {
		t.Errorf("the source does not name the provision:\n%s", out)
	}
	if !strings.Contains(out, `keyref="#`+statementKey("vn:norm:aaaa1111")+`"`) {
		t.Errorf("the association does not name the statement:\n%s", out)
	}
}

// A record the judge rejected has no place in a file that reads as a rule base.
// Skipping it quietly would be worse than refusing, because the caller would
// believe they exported their campaign.
func TestAnUntrustedRecordIsRefusedRatherThanDropped(t *testing.T) {
	r := duty()
	r.Status = norm.StatusRejected
	var buf bytes.Buffer
	_, err := Export(&buf, Input{Campaign: "labour-2025", Records: []norm.Record{duty(), r}})
	if err == nil {
		t.Fatal("a rejected record was exported")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("the error does not say what was wrong: %v", err)
	}
}

func TestACorpusWideExportIsRefused(t *testing.T) {
	var buf bytes.Buffer
	_, err := Export(&buf, Input{Records: []norm.Record{duty()}})
	if err == nil {
		t.Fatal("an export with no campaign succeeded")
	}
	if !strings.Contains(err.Error(), "measured") {
		t.Errorf("the error does not say why: %v", err)
	}
}

// Two exports of one campaign have to be the same bytes, or nobody can diff a
// release against the one before it.
func TestTheExportIsDeterministic(t *testing.T) {
	second := duty()
	second.ID = "vn:norm:bbbb2222"
	second.ProvisionID = "vn:law:2019:45-2019-qh14:article-13:clause-1"
	second.Statement.Type = "right"
	a, _ := export(t, duty(), second)
	b, _ := export(t, second, duty())
	if a != b {
		t.Error("the export depends on the order the records arrived in")
	}
}

// A participant the pipeline placed is something a consumer can join on, and
// one it did not is words. The difference has to survive into the file, or a
// consumer cannot tell which of the two they are holding.
func TestAPlacedParticipantGetsAnIRIAndAnUnplacedOneDoesNot(t *testing.T) {
	r := duty()
	r.Statement.Counterparty = &norm.Ref{Text: "người lao động"}
	out, _ := export(t, r)
	if !strings.Contains(out, `<ruleml:Ind iri="https://luatdo.dev/id/employer">người sử dụng lao động</ruleml:Ind>`) {
		t.Errorf("the placed bearer has no identifier:\n%s", out)
	}
	if !strings.Contains(out, `<ruleml:Ind>người lao động</ruleml:Ind>`) {
		t.Errorf("the unplaced counterparty was given one anyway:\n%s", out)
	}
	if !strings.Contains(out, `iri="https://luatdo.dev/id/vn:concept:wage-payment"`) {
		t.Errorf("the resolved action did not become the relation:\n%s", out)
	}
}
