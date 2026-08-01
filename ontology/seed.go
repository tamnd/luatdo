package ontology

// Seed returns the unfrozen v1 registry: the starting classes and predicates
// from the research spec. It is a starting point for human curation, not a
// finished ontology, which is why it is written unfrozen.
func Seed() *Registry {
	classes := []struct{ id, label, parent string }{
		{"vn-legal:LegalInstrument", "Văn bản quy phạm pháp luật", ""},
		{"vn-legal:Constitution", "Hiến pháp", "vn-legal:LegalInstrument"},
		{"vn-legal:Code", "Bộ luật", "vn-legal:LegalInstrument"},
		{"vn-legal:Law", "Luật", "vn-legal:LegalInstrument"},
		{"vn-legal:Resolution", "Nghị quyết", "vn-legal:LegalInstrument"},
		{"vn-legal:Ordinance", "Pháp lệnh", "vn-legal:LegalInstrument"},
		{"vn-legal:Decree", "Nghị định", "vn-legal:LegalInstrument"},
		{"vn-legal:Decision", "Quyết định", "vn-legal:LegalInstrument"},
		{"vn-legal:Circular", "Thông tư", "vn-legal:LegalInstrument"},
		{"vn-legal:LegalActor", "Chủ thể pháp luật", ""},
		{"vn-legal:NaturalPerson", "Cá nhân", "vn-legal:LegalActor"},
		{"vn-legal:Organization", "Tổ chức", "vn-legal:LegalActor"},
		{"vn-legal:Enterprise", "Doanh nghiệp", "vn-legal:Organization"},
		{"vn-legal:Household", "Hộ gia đình", "vn-legal:LegalActor"},
		{"vn-legal:StateAuthority", "Cơ quan nhà nước", "vn-legal:Organization"},
		{"vn-legal:Court", "Tòa án", "vn-legal:StateAuthority"},
		{"vn-legal:Procuracy", "Viện kiểm sát", "vn-legal:StateAuthority"},
		{"vn-legal:Ministry", "Bộ", "vn-legal:StateAuthority"},
		{"vn-legal:ProvincialAuthority", "Ủy ban nhân dân cấp tỉnh", "vn-legal:StateAuthority"},
		{"vn-legal:CompetentAuthority", "Cơ quan có thẩm quyền", "vn-legal:StateAuthority"},
		{"vn-legal:Employee", "Người lao động", "vn-legal:NaturalPerson"},
		{"vn-legal:Employer", "Người sử dụng lao động", "vn-legal:LegalActor"},
		{"vn-legal:Norm", "Quy phạm", ""},
		{"vn-legal:Duty", "Nghĩa vụ", "vn-legal:Norm"},
		{"vn-legal:Prohibition", "Điều cấm", "vn-legal:Norm"},
		{"vn-legal:Permission", "Sự cho phép", "vn-legal:Norm"},
		{"vn-legal:Right", "Quyền", "vn-legal:Norm"},
		{"vn-legal:Power", "Thẩm quyền", "vn-legal:Norm"},
		{"vn-legal:Definition", "Định nghĩa", "vn-legal:Norm"},
		{"vn-legal:Exception", "Ngoại lệ", "vn-legal:Norm"},
		{"vn-legal:Sanction", "Chế tài", "vn-legal:Norm"},
		{"vn-legal:Procedure", "Thủ tục", "vn-legal:Norm"},
		{"vn-legal:Condition", "Điều kiện", ""},
		{"vn-legal:Event", "Sự kiện pháp lý", ""},
		{"vn-legal:Action", "Hành vi", ""},
		{"vn-legal:LegalStatus", "Tình trạng pháp lý", ""},
		{"vn-legal:Location", "Địa điểm", ""},
		{"vn-legal:TimeInterval", "Khoảng thời gian", ""},
		{"vn-legal:Deadline", "Thời hạn", ""},
		{"vn-legal:Amount", "Mức tiền", ""},
		{"vn-legal:License", "Giấy phép", ""},
		{"vn-legal:Registration", "Đăng ký", ""},
		{"vn-legal:DocumentRequirement", "Hồ sơ, giấy tờ", ""},
	}
	predicates := []struct {
		id       string
		surfaces []string
	}{
		{"vn-legal:defines", nil},
		{"vn-legal:appliesTo", []string{"áp dụng đối với"}},
		{"vn-legal:imposesDuty", []string{"có trách nhiệm", "có nghĩa vụ", "phải"}},
		{"vn-legal:grantsRight", []string{"có quyền", "được quyền"}},
		{"vn-legal:prohibits", []string{"nghiêm cấm", "không được"}},
		{"vn-legal:permits", []string{"được", "được phép"}},
		{"vn-legal:authorizes", []string{"có thẩm quyền"}},
		{"vn-legal:requiresDocument", []string{"hồ sơ gồm", "kèm theo"}},
		{"vn-legal:performedBy", nil},
		{"vn-legal:decidedBy", nil},
		{"vn-legal:submittedTo", []string{"nộp cho", "gửi đến"}},
		{"vn-legal:issuedBy", []string{"do", "ban hành"}},
		{"vn-legal:hasCondition", []string{"trong trường hợp", "khi", "nếu"}},
		{"vn-legal:hasException", []string{"trừ trường hợp"}},
		{"vn-legal:hasSanction", []string{"bị xử phạt", "bị xử lý"}},
		{"vn-legal:hasDeadline", []string{"trong thời hạn"}},
	}

	r := &Registry{Version: 1}
	for _, c := range classes {
		r.Classes = append(r.Classes, Class{ID: c.id, LabelVI: c.label, Parent: c.parent})
	}
	for _, p := range predicates {
		r.Predicates = append(r.Predicates, Predicate{ID: p.id, SurfaceFormsVI: p.surfaces})
	}
	return r
}
