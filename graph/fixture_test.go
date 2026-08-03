package graph

import (
	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/conflict"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/relation"
	"github.com/tamnd/luatdo/temporal"
	"github.com/tamnd/luatdo/term"
)

// The fixture corpus, built so that every one of the twenty three competency
// questions has a known answer.
//
// It is six invented documents with real shapes: a code and the code it
// replaced, a law from another field that defines one of the same words
// differently, a decree that amends the code, a provincial decision that cites
// the decree, and the 1994 code at the far end of the amendment chain. The text
// is Vietnamese because the queries match on Vietnamese strings and a fixture in
// English would let a broken accent through.
//
// Nothing here is copied from a real instrument. The point is not to be right
// about the Labour Code, it is to have a graph small enough that the expected
// answer to each question can be worked out by reading the fixture and stated as
// a number, which is what makes a failure mean something.
const (
	fxCode     = "vn:law:2019:45-2019-qh14"      // the code, in force
	fxOldCode  = "vn:law:2012:10-2012-qh13"      // the code it replaced, repealed
	fxSocial   = "vn:law:2014:58-2014-qh13"      // another field, same word, other definition
	fxDecree   = "vn:decree:2021:145-2021-nd-cp" // central, amends the code
	fxDecision = "vn:decision:2020:12-2020-qd-ubnd"
	fxAncient  = "vn:law:1994:35-l-ctn" // the far end of the amendment chain
)

// The concepts. Only the ones a question walks through are here.
const (
	fxNLD   = "vn:concept:nguoi-lao-dong"
	fxCQCTQ = "vn:concept:co-quan-co-tham-quyen"
	fxCQNN  = "vn:concept:co-quan-nha-nuoc"
	fxUBND  = "vn:concept:uy-ban-nhan-dan"
	fxTL    = "vn:concept:tien-luong"
	fxBHXH  = "vn:concept:bao-hiem-xa-hoi"
	fxGPXD  = "vn:concept:giay-phep-xay-dung"
	fxHSTK  = "vn:concept:ho-so-thiet-ke"
	fxGCN   = "vn:concept:giay-chung-nhan-quyen-su-dung-dat"
	fxHDLD  = "vn:concept:hop-dong-lao-dong"
	fxHDLV  = "vn:concept:hop-dong-lam-viec"
)

func fxDocs() []*law.Document {
	article := func(docID, number, heading, text string, position int) law.Provision {
		return law.Provision{
			ID: docID + ":article-" + number, Kind: "article", Number: number,
			Heading: heading, Text: text, Position: position,
		}
	}
	code := &law.Document{
		ID: fxCode, OfficialNumber: "45/2019/QH14", Title: "Bộ luật Lao động",
		DocType: "code", IssuingBody: "Quốc hội", EffectiveFrom: "2021-01-01",
		Status: "in_force",
		Provisions: []law.Provision{
			article(fxCode, "1", "Phạm vi điều chỉnh", "Bộ luật này quy định tiêu chuẩn lao động.", 1),
			article(fxCode, "3", "Giải thích từ ngữ", "Người lao động là người làm việc cho người sử dụng lao động theo thỏa thuận.", 2),
			article(fxCode, "94", "Nguyên tắc trả lương", "Người sử dụng lao động phải trả lương trực tiếp cho người lao động.", 3),
			{
				ID: fxCode + ":article-94:clause-1", ParentID: fxCode + ":article-94",
				Kind: "clause", Number: "1", Text: "Tiền lương được trả bằng tiền mặt hoặc qua tài khoản.",
				Position: 4,
			},
			article(fxCode, "95", "Mức lương tối thiểu", "Mức lương tối thiểu là mức lương thấp nhất được trả cho người lao động.", 5),
			article(fxCode, "96", "Hình thức trả lương", "Tiền lương được trả theo thời gian hoặc theo sản phẩm.", 6),
			article(fxCode, "97", "Giữ giấy tờ", "Người sử dụng lao động không được giữ bản chính giấy tờ tùy thân của người lao động.", 7),
			article(fxCode, "98", "Thẩm quyền", "Cơ quan có thẩm quyền hướng dẫn thi hành Điều này.", 8),
			article(fxCode, "99", "Bảo hiểm", "Người sử dụng lao động phải đóng bảo hiểm xã hội cho người lao động.", 9),
		},
	}
	old := &law.Document{
		ID: fxOldCode, OfficialNumber: "10/2012/QH13", Title: "Bộ luật Lao động 2012",
		DocType: "code", IssuingBody: "Quốc hội", EffectiveFrom: "2013-05-01",
		Status: "repealed",
		Provisions: []law.Provision{
			article(fxOldCode, "96", "Nguyên tắc trả lương", "Người sử dụng lao động phải trả lương đúng hạn.", 1),
		},
	}
	social := &law.Document{
		ID: fxSocial, OfficialNumber: "58/2014/QH13", Title: "Luật Bảo hiểm xã hội",
		DocType: "law", IssuingBody: "Quốc hội", EffectiveFrom: "2016-01-01",
		Status: "in_force",
		Provisions: []law.Provision{
			article(fxSocial, "3", "Giải thích từ ngữ", "Người lao động là người tham gia bảo hiểm xã hội bắt buộc.", 1),
			article(fxSocial, "4", "Bảo hiểm xã hội", "Bảo hiểm xã hội là sự bảo đảm thay thế thu nhập của người lao động.", 2),
			article(fxSocial, "5", "Trách nhiệm đóng", "Người sử dụng lao động phải đóng bảo hiểm cho người lao động.", 3),
			article(fxSocial, "6", "Thẩm quyền", "Cơ quan có thẩm quyền kiểm tra việc đóng bảo hiểm.", 4),
		},
	}
	decree := &law.Document{
		ID: fxDecree, OfficialNumber: "145/2021/NĐ-CP", Title: "Nghị định quy định chi tiết",
		DocType: "decree", IssuingBody: "Chính phủ", EffectiveFrom: "2021-06-01",
		Status: "in_force",
		Provisions: []law.Provision{
			article(fxDecree, "1", "Hồ sơ", "Chủ đầu tư nộp hồ sơ đề nghị cấp giấy phép xây dựng.", 1),
			article(fxDecree, "2", "Thời hạn", "Cơ quan cấp phép trả kết quả trong thời hạn quy định.", 2),
		},
	}
	decision := &law.Document{
		ID: fxDecision, OfficialNumber: "12/2020/QĐ-UBND", Title: "Quyết định của tỉnh",
		DocType: "decision", IssuingBody: "UBND tỉnh Bình Dương", EffectiveFrom: "2020-06-01",
		Status: "in_force",
		Provisions: []law.Provision{
			article(fxDecision, "1", "Căn cứ", "Căn cứ Nghị định 145/2021/NĐ-CP.", 1),
		},
	}
	ancient := &law.Document{
		ID: fxAncient, OfficialNumber: "35-L/CTN", Title: "Bộ luật Lao động 1994",
		DocType: "code", IssuingBody: "Quốc hội", EffectiveFrom: "1995-01-01",
		Status: "repealed",
	}
	return []*law.Document{code, old, social, decree, decision, ancient}
}

func fxLinks() []cite.Link {
	return []cite.Link{
		{FromDoc: fxCode, FromProvision: fxCode + ":article-1", ToNumber: "10/2012/QH13", ToDoc: fxOldCode, Kind: "cites", Method: "pattern"},
		{FromDoc: fxCode, FromProvision: fxCode + ":article-1", ToNumber: "10/2012/QH13", ToDoc: fxOldCode, Kind: "amends", Method: "pattern"},
		{FromDoc: fxOldCode, FromProvision: fxOldCode + ":article-96", ToNumber: "35-L/CTN", ToDoc: fxAncient, Kind: "amends", Method: "pattern"},
		{FromDoc: fxDecision, FromProvision: fxDecision + ":article-1", ToNumber: "145/2021/NĐ-CP", ToDoc: fxDecree, Kind: "cites", Method: "pattern"},
	}
}

// fxStatements are the norms. Identifiers are written out rather than minted,
// because a test that asks question 14 about a norm has to be able to name it.
func fxStatements() []norm.Record {
	rec := func(id, provision string, s norm.Statement) norm.Record {
		docID := provision
		for _, prefix := range []string{fxCode, fxSocial, fxDecree} {
			if len(provision) > len(prefix) && provision[:len(prefix)] == prefix {
				docID = prefix
			}
		}
		s.Evidence.Quote = s.Action.Text
		s.Confidence = 0.9
		return norm.Record{
			ID: id, DocID: docID, ProvisionID: provision, Statement: s,
			Status: norm.StatusVerified, Model: "fixture", OntologyVersion: 1,
		}
	}
	employer := func() *norm.Ref {
		return &norm.Ref{Text: "người sử dụng lao động", ClassID: "vn-legal:Employer", IsActor: true}
	}
	authority := func() *norm.Ref {
		return &norm.Ref{Text: "cơ quan có thẩm quyền", ClassID: "vn-legal:StateAgency", ConceptID: fxCQCTQ, IsActor: true}
	}
	return []norm.Record{
		// The paying duty, sanctioned, with a condition and an exception. It is
		// the norm question 14 is asked about.
		rec("vn:norm:fixture-pay", fxCode+":article-94", norm.Statement{
			Type: "duty", Modality: "phải", Bearer: employer(),
			Action:     norm.Ref{Text: "trả lương", ConceptID: fxTL},
			Object:     &norm.Ref{Text: "tiền lương"},
			Conditions: []norm.Clause{{Kind: "temporal", Text: "khi đến kỳ trả lương", Quote: "khi đến kỳ trả lương"}},
			Exceptions: []norm.Clause{{Kind: "scope", Text: "trừ trường hợp bất khả kháng", Quote: "trừ trường hợp bất khả kháng"}},
			Sanction: &norm.Sanction{
				Text:       "phạt tiền từ 5.000.000 đồng đến 10.000.000 đồng",
				Quote:      "phạt tiền từ 5.000.000 đồng đến 10.000.000 đồng",
				LegalBasis: "Điều 17 Nghị định 12/2022/NĐ-CP",
			},
		}),
		// The same actor and the same action, forbidden. Question 19 finds this
		// pair, and question 13 must not report it, because the duty beside it
		// carries a sanction and they are about the same concept.
		rec("vn:norm:fixture-nopay", fxCode+":article-94:clause-1", norm.Statement{
			Type: "prohibition", Modality: "không được", Bearer: employer(),
			Action: norm.Ref{Text: "trả lương", ConceptID: fxTL},
			Object: &norm.Ref{Text: "bằng hiện vật"},
		}),
		// A three working day deadline, for question 12.
		rec("vn:norm:fixture-publish", fxCode+":article-95", norm.Statement{
			Type: "duty", Modality: "phải", Bearer: employer(),
			Action: norm.Ref{Text: "công bố mức lương tối thiểu"},
			Deadline: &norm.Deadline{
				Text: "trong thời hạn 03 ngày làm việc", Value: 3,
				Unit: norm.UnitDay, Calendar: norm.CalendarWorking,
				Anchor: "kể từ ngày nhận được thông báo",
			},
		}),
		// A duty with nobody to owe it, for question 10.
		rec("vn:norm:fixture-orphan", fxCode+":article-96", norm.Statement{
			Type: "duty", Modality: "phải",
			Action: norm.Ref{Text: "trả lương theo thời gian"},
		}),
		// A prohibition nothing sanctions, anywhere, for question 13.
		rec("vn:norm:fixture-keepdocs", fxCode+":article-97", norm.Statement{
			Type: "prohibition", Modality: "không được", Bearer: employer(),
			Action: norm.Ref{Text: "giữ bản chính giấy tờ tùy thân"},
		}),
		// Two norms naming an authority nobody defines, for questions 6 and 15.
		rec("vn:norm:fixture-guide", fxCode+":article-98", norm.Statement{
			Type: "duty", Modality: "phải", Bearer: authority(),
			Action: norm.Ref{Text: "hướng dẫn thi hành"},
		}),
		rec("vn:norm:fixture-inspect", fxSocial+":article-6", norm.Statement{
			Type: "duty", Modality: "phải", Bearer: authority(),
			Action: norm.Ref{Text: "kiểm tra việc đóng bảo hiểm"},
		}),
		// A norm in the older instrument about the word the newer one redefined,
		// for question 20.
		rec("vn:norm:fixture-contribute", fxSocial+":article-5", norm.Statement{
			Type: "duty", Modality: "phải", Bearer: employer(),
			Action: norm.Ref{Text: "đóng bảo hiểm xã hội"},
			Object: &norm.Ref{Text: "người lao động", ConceptID: fxNLD, IsActor: true},
		}),
		// A norm using a concept the other instrument defines, for question 22.
		rec("vn:norm:fixture-insure", fxCode+":article-99", norm.Statement{
			Type: "duty", Modality: "phải", Bearer: employer(),
			Action: norm.Ref{Text: "đóng bảo hiểm xã hội", ConceptID: fxBHXH},
		}),
		// The two steps of a procedure, for question 11.
		rec("vn:norm:fixture-apply", fxDecree+":article-1", norm.Statement{
			Type: "duty", Modality: "phải",
			Bearer:      &norm.Ref{Text: "chủ đầu tư", ClassID: "vn-legal:Business", IsActor: true},
			Action:      norm.Ref{Text: "nộp hồ sơ"},
			Object:      &norm.Ref{Text: "giấy phép xây dựng", ConceptID: fxGPXD},
			ProcedureID: "cấp giấy phép xây dựng", Step: 1,
			Conditions: []norm.Clause{{Kind: "document", Text: "hồ sơ thiết kế đã được thẩm định", Quote: "hồ sơ thiết kế đã được thẩm định"}},
		}),
		rec("vn:norm:fixture-issue", fxDecree+":article-2", norm.Statement{
			Type: "duty", Modality: "phải",
			Bearer:      authority(),
			Action:      norm.Ref{Text: "trả kết quả"},
			ProcedureID: "cấp giấy phép xây dựng", Step: 2,
			Deadline: &norm.Deadline{
				Text: "trong thời hạn 20 ngày", Value: 20,
				Unit: norm.UnitDay, Calendar: norm.CalendarNormal,
			},
		}),
	}
}

func fxLayer() *concept.Layer {
	return &concept.Layer{
		TermUses: []concept.TermUse{
			{
				ID: "vn:term:" + fxCode + ":nguoi-lao-dong", LabelVI: "người lao động",
				ScopeID: fxCode, DocID: fxCode, Kind: concept.KindActor,
				DefinitionVI: "người làm việc cho người sử dụng lao động theo thỏa thuận",
				Origin:       concept.OriginDefined, DefinedBy: fxCode + ":article-3",
				Quote: "Người lao động", Confidence: 0.9,
			},
			{
				ID: "vn:term:" + fxSocial + ":nguoi-lao-dong", LabelVI: "người lao động",
				ScopeID: fxSocial, DocID: fxSocial, Kind: concept.KindActor,
				DefinitionVI: "người tham gia bảo hiểm xã hội bắt buộc",
				Origin:       concept.OriginDefined, DefinedBy: fxSocial + ":article-3",
				Quote: "Người lao động", Confidence: 0.9,
			},
			{
				// Nobody defines this one, which is the whole point of it.
				ID: "vn:term:" + fxCode + ":co-quan-co-tham-quyen", LabelVI: "cơ quan có thẩm quyền",
				ScopeID: fxCode, DocID: fxCode, Kind: concept.KindActor, IsRole: true,
				Origin: concept.OriginUndefinedUsage, Quote: "Cơ quan có thẩm quyền", Confidence: 0.8,
			},
			{
				ID: "vn:term:" + fxSocial + ":bao-hiem-xa-hoi", LabelVI: "bảo hiểm xã hội",
				ScopeID: fxSocial, DocID: fxSocial, Kind: concept.KindThing,
				DefinitionVI: "sự bảo đảm thay thế thu nhập của người lao động",
				Origin:       concept.OriginDefined, DefinedBy: fxSocial + ":article-4",
				Quote: "Bảo hiểm xã hội", Confidence: 0.9,
			},
			{
				// Supplemented into article 95 by the decree, for question 8.
				ID: "vn:term:" + fxCode + ":muc-luong-toi-thieu", LabelVI: "mức lương tối thiểu",
				ScopeID: fxCode, DocID: fxCode, Kind: concept.KindThing,
				DefinitionVI: "mức lương thấp nhất được trả cho người lao động",
				Origin:       concept.OriginDefined, DefinedBy: fxCode + ":article-95",
				Quote: "Mức lương tối thiểu", Confidence: 0.9,
			},
		},
		Concepts: []concept.Concept{
			{ID: fxNLD, LabelVI: "người lao động", Kind: concept.KindActor},
			{ID: fxCQCTQ, LabelVI: "cơ quan có thẩm quyền", Kind: concept.KindActor},
			{ID: fxCQNN, LabelVI: "cơ quan nhà nước", Kind: concept.KindActor},
			{ID: fxUBND, LabelVI: "ủy ban nhân dân", Kind: concept.KindActor},
			{ID: fxTL, LabelVI: "tiền lương", Kind: concept.KindThing},
			{ID: fxBHXH, LabelVI: "bảo hiểm xã hội", Kind: concept.KindThing},
			{ID: fxGPXD, LabelVI: "giấy phép xây dựng", Kind: concept.KindThing},
			{ID: fxHSTK, LabelVI: "hồ sơ thiết kế", Kind: concept.KindThing},
			{ID: fxGCN, LabelVI: "giấy chứng nhận quyền sử dụng đất", Kind: concept.KindThing},
			{ID: fxHDLD, LabelVI: "hợp đồng lao động", Kind: concept.KindThing},
			{ID: fxHDLV, LabelVI: "hợp đồng làm việc", Kind: concept.KindThing},
		},
		Memberships: []concept.Membership{
			{TermUseID: "vn:term:" + fxCode + ":nguoi-lao-dong", ConceptID: fxNLD, Relation: concept.RelationSame, DecidedBy: "fixture", DecidedAt: "2026-01-01T00:00:00Z"},
			{TermUseID: "vn:term:" + fxSocial + ":nguoi-lao-dong", ConceptID: fxNLD, Relation: concept.RelationSame, DecidedBy: "fixture", DecidedAt: "2026-01-01T00:00:00Z"},
			{TermUseID: "vn:term:" + fxCode + ":co-quan-co-tham-quyen", ConceptID: fxCQCTQ, Relation: concept.RelationSame, DecidedBy: "fixture", DecidedAt: "2026-01-01T00:00:00Z"},
			{TermUseID: "vn:term:" + fxSocial + ":bao-hiem-xa-hoi", ConceptID: fxBHXH, Relation: concept.RelationSame, DecidedBy: "fixture", DecidedAt: "2026-01-01T00:00:00Z"},
			{TermUseID: "vn:term:" + fxCode + ":muc-luong-toi-thieu", ConceptID: fxTL, Relation: concept.RelationSame, DecidedBy: "fixture", DecidedAt: "2026-01-01T00:00:00Z"},
		},
		Differences: []concept.Difference{{
			FromID: "vn:term:" + fxSocial + ":nguoi-lao-dong", ToID: "vn:term:" + fxCode + ":nguoi-lao-dong",
			DecidedBy: "fixture", DecidedAt: "2026-01-01T00:00:00Z",
			Rationale: "một bên định nghĩa theo thỏa thuận, một bên theo việc tham gia bảo hiểm",
			Basis:     []string{"phạm vi"},
		}},
	}
}

func fxRelations() []relation.Edge {
	edge := func(from, to, relType string, support, docs int) relation.Edge {
		return relation.Edge{
			FromID: from, ToID: to, Type: relType,
			Status: relation.StatusCanonical, Source: "fixture",
			SupportCount: support, SupportDocs: docs, Confidence: 0.9,
			Evidence: []relation.Evidence{{ProvisionID: fxCode + ":article-1", Quote: "fixture"}},
		}
	}
	return []relation.Edge{
		edge(fxCQCTQ, fxCQNN, relation.Broader, 4, 2),
		edge(fxUBND, fxCQCTQ, relation.Broader, 3, 2),
		// The second parent. Question 7 flags it rather than resolving it.
		edge(fxUBND, fxCQNN, relation.Broader, 2, 2),
		edge(fxGPXD, fxHSTK, relation.Requires, 5, 3),
		edge(fxHSTK, fxGCN, relation.Requires, 3, 2),
		edge(fxHDLD, fxHDLV, relation.AlternativeTo, 3, 2),
		// Provisional, and so not exported at all.
		{
			FromID: fxTL, ToID: fxBHXH, Type: relation.RegulatedBy,
			Status: relation.StatusProvisional, Source: "fixture", Why: "seen once",
			SupportCount: 1, SupportDocs: 1,
			Evidence: []relation.Evidence{{ProvisionID: fxCode + ":article-99", Quote: "fixture"}},
		},
	}
}

func fxTemporal() *temporal.Layer {
	v94a := temporal.VersionID(fxCode+":article-94", 1)
	v94b := temporal.VersionID(fxCode+":article-94", 2)
	v94c := temporal.VersionID(fxCode+":article-94:clause-1", 1)
	v95 := temporal.VersionID(fxCode+":article-95", 1)
	v96 := temporal.VersionID(fxOldCode+":article-96", 1)
	vSocial := temporal.VersionID(fxSocial+":article-3", 1)
	return &temporal.Layer{
		Events: []temporal.Event{
			{
				ID: "vn:event:fixture-enact", Kind: temporal.KindEnact, Date: "2021-01-01",
				CausedByDoc: fxCode, CausedBy: fxCode + ":article-1", Confidence: 1,
				Produces: []string{v94a, v94c},
			},
			{
				ID: "vn:event:fixture-amend", Kind: temporal.KindAmend, Date: "2021-06-01",
				CausedByDoc: fxDecree, CausedBy: fxDecree + ":article-1",
				Instruction: "sửa đổi Điều 94", Confidence: 0.9,
				Terminates: []string{v94a}, Produces: []string{v94b},
			},
			{
				ID: "vn:event:fixture-supplement", Kind: temporal.KindSupplement, Date: "2021-06-01",
				CausedByDoc: fxDecree, CausedBy: fxDecree + ":article-1",
				Instruction: "bổ sung Điều 95", Confidence: 0.9,
				Produces: []string{v95},
			},
			{
				ID: "vn:event:fixture-repeal", Kind: temporal.KindRepeal, Date: "2021-01-01",
				CausedByDoc: fxCode, CausedBy: fxCode + ":article-1",
				Instruction: "bãi bỏ Bộ luật Lao động 2012", Confidence: 1,
				Terminates: []string{v96},
			},
			{
				ID: "vn:event:fixture-enact-social", Kind: temporal.KindEnact, Date: "2016-01-01",
				CausedByDoc: fxSocial, Confidence: 1, Produces: []string{vSocial},
			},
		},
		Versions: []temporal.Version{
			{
				ID: v94a, ComponentID: fxCode + ":article-94", DocID: fxCode, Kind: "article",
				Number: "94", Seq: 1, Text: "Người sử dụng lao động phải trả lương trực tiếp.",
				From: "2021-01-01", To: "2021-06-01", Force: temporal.ForceInForce,
				ProducedBy: "vn:event:fixture-enact", TerminatedBy: "vn:event:fixture-amend",
				Children: []string{v94c},
			},
			{
				ID: v94b, ComponentID: fxCode + ":article-94", DocID: fxCode, Kind: "article",
				Number: "94", Seq: 2, Text: "Người sử dụng lao động phải trả lương trực tiếp, đầy đủ và đúng hạn.",
				From: "2021-06-01", Force: temporal.ForceInForce,
				ProducedBy: "vn:event:fixture-amend", Children: []string{v94c},
			},
			{
				ID: v94c, ComponentID: fxCode + ":article-94:clause-1", DocID: fxCode, Kind: "clause",
				Number: "1", Seq: 1, Text: "Tiền lương được trả bằng tiền mặt hoặc qua tài khoản.",
				From: "2021-01-01", Force: temporal.ForceInForce,
				ProducedBy: "vn:event:fixture-enact",
			},
			{
				ID: v95, ComponentID: fxCode + ":article-95", DocID: fxCode, Kind: "article",
				Number: "95", Seq: 1, Text: "Mức lương tối thiểu là mức lương thấp nhất.",
				From: "2021-06-01", Force: temporal.ForceInForce,
				ProducedBy: "vn:event:fixture-supplement",
			},
			{
				ID: v96, ComponentID: fxOldCode + ":article-96", DocID: fxOldCode, Kind: "article",
				Number: "96", Seq: 1, Text: "Người sử dụng lao động phải trả lương đúng hạn.",
				From: "2013-05-01", To: "2021-01-01", Force: temporal.ForceInForce,
				TerminatedBy: "vn:event:fixture-repeal",
			},
			{
				ID: vSocial, ComponentID: fxSocial + ":article-3", DocID: fxSocial, Kind: "article",
				Number: "3", Seq: 1, Text: "Người lao động là người tham gia bảo hiểm xã hội bắt buộc.",
				From: "2016-01-01", Force: temporal.ForceInForce,
				ProducedBy: "vn:event:fixture-enact-social",
			},
		},
	}
}

// fxConflicts runs the real detector over the fixture's own statements, so the
// projected findings are the ones the checker produces rather than ones written
// out by hand here.
//
// The pay and no-pay pair is the one it fires on: the same employer, the same
// act, one duty and one prohibition. The parse pass is stood in for, because it
// calls a model and a fixture does not, and standing it in is exactly what it
// does in production, namely fill the canonical party and act.
func fxConflicts() []conflict.Finding {
	var forms []*conflict.Form
	for _, r := range fxStatements() {
		f, ok := conflict.Draft(&r)
		if !ok {
			continue
		}
		if r.Statement.Bearer != nil {
			f.Party = law.Slug(r.Statement.Bearer.Text)
			f.Canon.Party = r.Statement.Bearer.Text
		}
		f.Act = law.Slug(r.Statement.Action.Text)
		f.Canon.Act = r.Statement.Action.Text
		forms = append(forms, f)
	}
	return conflict.Check(forms, nil).Findings
}

// competencyFixture is the whole projection the competency tests run against.
func competencyFixture() Input {
	return Input{
		Docs:       fxDocs(),
		Links:      fxLinks(),
		Registry:   ontology.Seed(),
		Statements: fxStatements(),
		Layer:      fxLayer(),
		Relations:  fxRelations(),
		Temporal:   fxTemporal(),
		Conflicts:  fxConflicts(),
		Definitions: []term.Definition{{
			ProvisionID: fxCode + ":article-3", TermID: "vn:term:nguoi-lao-dong",
			Term: "người lao động", Connective: "là",
		}},
	}
}
