package norm

import "testing"

const labourCode = "vn:law:2019:45-2019-qh14"
const penaltyDecree = "vn:decree:2022:12-2022-nd-cp"

func corpusIndex() map[string]string {
	return map[string]string{
		"45/2019/QH14":  labourCode,
		"12/2022/NĐ-CP": penaltyDecree,
	}
}

func sanctioned(docID, basis string) Record {
	return Record{
		DocID: docID, ProvisionID: docID + ":article-1", Status: "verified",
		Statement: Statement{
			Type:     "duty",
			Bearer:   &Ref{Text: "người sử dụng lao động", IsActor: true},
			Action:   Ref{Text: "trả lương"},
			Sanction: &Sanction{Text: "phạt tiền", LegalBasis: basis},
		},
	}
}

func TestResolveSanctionsFollowsABasisIntoAnotherInstrument(t *testing.T) {
	records := []Record{sanctioned(labourCode, "khoản 2 Điều 17 Nghị định số 12/2022/NĐ-CP")}
	cov := ResolveSanctions(records, corpusIndex())
	got := records[0].Statement.Sanction
	if got.BasisDoc != penaltyDecree {
		t.Errorf("basis doc = %q, the penalty for a labour duty lives in the decree", got.BasisDoc)
	}
	if got.BasisProvison != penaltyDecree+":article-17:clause-2" {
		t.Errorf("basis provision = %q, the article and the clause both count", got.BasisProvison)
	}
	if cov.CrossDoc != 1 || cov.Resolved != 1 {
		t.Errorf("coverage = %+v, this is the cross document case", cov)
	}
}

func TestResolveSanctionsReadsABareArticleAsTheDocumentsOwn(t *testing.T) {
	records := []Record{sanctioned(penaltyDecree, "Điều 17 của Nghị định này")}
	ResolveSanctions(records, corpusIndex())
	if got := records[0].Statement.Sanction.BasisProvison; got != penaltyDecree+":article-17" {
		t.Errorf("basis = %q, a drafter who meant another instrument would have named it", got)
	}
}

func TestResolveSanctionsPointsNowhereWhenTheCorpusLacksTheInstrument(t *testing.T) {
	records := []Record{sanctioned(labourCode, "Điều 5 Nghị định số 99/2099/NĐ-CP")}
	cov := ResolveSanctions(records, corpusIndex())
	if got := records[0].Statement.Sanction.BasisDoc; got != "" {
		t.Errorf("basis doc = %q, inventing a target is worse than leaving it empty", got)
	}
	if cov.External != 1 || cov.Resolved != 0 {
		t.Errorf("coverage = %+v, a basis outside the corpus is a fact about the corpus", cov)
	}
}

func TestUnsanctionedFindsTheProhibitionNothingPunishes(t *testing.T) {
	forbid := func(action, conceptID string) Record {
		return Record{
			DocID: labourCode, Status: "verified",
			Statement: Statement{
				Type:   "prohibition",
				Bearer: &Ref{Text: "người sử dụng lao động", IsActor: true},
				Action: Ref{Text: action, ConceptID: conceptID},
			},
		}
	}
	records := []Record{
		forbid("giữ bản chính giấy tờ tùy thân", "vn:concept:giu-giay-to"),
		forbid("ép buộc người lao động", "vn:concept:ep-buoc"),
		{
			DocID: penaltyDecree, Status: "verified",
			Statement: Statement{
				Type:   "sanction",
				Action: Ref{Text: "hành vi khác"},
				// The decree words the offence differently from the law, which is
				// the ordinary case and the reason the match runs on the concept.
				Sanction: &Sanction{Text: "phạt tiền", LegalBasis: "Điều 9", ConceptID: "vn:concept:giu-giay-to"},
			},
		},
	}
	got := Unsanctioned(records)
	if len(got) != 1 {
		t.Fatalf("unsanctioned = %d prohibitions, want the one the decree never mentions", len(got))
	}
	if got[0].Statement.Action.Text != "ép buộc người lao động" {
		t.Errorf("unsanctioned = %q, the other one is punished under different words", got[0].Statement.Action.Text)
	}
}
