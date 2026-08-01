package law

import "testing"

func labourCode() *Document {
	return &Document{
		ID:            "vn:law:2019:45-2019-qh14",
		EffectiveFrom: "2021-01-01",
		Provisions: []Provision{
			{ID: "vn:law:2019:45-2019-qh14:chapter-VII", Kind: "chapter", Number: "VII", Heading: "Tiền lương", Position: 0},
			{ID: "vn:law:2019:45-2019-qh14:article-94", ParentID: "vn:law:2019:45-2019-qh14:chapter-VII", Kind: "article", Number: "94", Heading: "Nguyên tắc trả lương", Position: 1},
			{ID: "vn:law:2019:45-2019-qh14:article-94:clause-1", ParentID: "vn:law:2019:45-2019-qh14:article-94", Kind: "clause", Number: "1", Text: "Người sử dụng lao động phải trả lương trực tiếp, đầy đủ, đúng hạn cho người lao động.", TextHash: "9f2c4a1b7d3e5608aa11", Position: 2},
		},
	}
}

func TestSplitGivesEveryProvisionAComponent(t *testing.T) {
	doc := labourCode()
	components, _ := Split(doc)
	if len(components) != len(doc.Provisions) {
		t.Fatalf("got %d components, want %d", len(components), len(doc.Provisions))
	}
	for i, c := range components {
		if c.ID != doc.Provisions[i].ID {
			t.Errorf("component %d has id %q, want %q", i, c.ID, doc.Provisions[i].ID)
		}
		if c.DocID != doc.ID {
			t.Errorf("component %s has doc %q, want %q", c.ID, c.DocID, doc.ID)
		}
	}
}

func TestSplitGivesNoVersionToAProvisionWithNoText(t *testing.T) {
	doc := labourCode()
	_, versions := Split(doc)
	if len(versions) != 1 {
		t.Fatalf("got %d versions, want 1", len(versions))
	}
	if versions[0].ComponentID != "vn:law:2019:45-2019-qh14:article-94:clause-1" {
		t.Errorf("version belongs to %q", versions[0].ComponentID)
	}
	// The chapter and the article carry a heading and no text. Giving them a
	// version holding the empty string would record that they said nothing,
	// which is a different claim from having no wording of their own.
	for _, v := range versions {
		if v.Text == "" {
			t.Errorf("version %s carries no text", v.ID)
		}
	}
}

func TestSplitDatesVersionsFromTheDocument(t *testing.T) {
	doc := labourCode()
	_, versions := Split(doc)
	if versions[0].FromDate != "2021-01-01" {
		t.Errorf("version starts %q, want 2021-01-01", versions[0].FromDate)
	}
	if versions[0].ToDate != "" {
		t.Errorf("version ends %q, want an open interval", versions[0].ToDate)
	}
}

func TestTextVersionIDFollowsTheContent(t *testing.T) {
	const component = "vn:law:2019:45-2019-qh14:article-94:clause-1"
	first := TextVersionID(component, "9f2c4a1b7d3e5608aa11")
	again := TextVersionID(component, "9f2c4a1b7d3e5608aa11")
	amended := TextVersionID(component, "0011223344556677aabb")
	if first != again {
		t.Errorf("the same text minted %q then %q", first, again)
	}
	if first == amended {
		t.Errorf("amended text kept the identifier %q", first)
	}
	if want := component + ":text-9f2c4a1b7d3e"; first != want {
		t.Errorf("got %q, want %q", first, want)
	}
}

func TestTextVersionIDSurvivesAMissingHash(t *testing.T) {
	const component = "vn:law:2019:45-2019-qh14:article-94:clause-1"
	if got, want := TextVersionID(component, ""), component+":text"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenumberKeepsTheIdentifierAndTheFirstNumber(t *testing.T) {
	doc := labourCode()
	components, _ := Split(doc)
	c := &components[1]
	id := c.ID

	c.Renumber("95")
	if c.ID != id {
		t.Errorf("renumbering moved the identifier to %q", c.ID)
	}
	if c.Number != "95" {
		t.Errorf("number is %q, want 95", c.Number)
	}
	if c.RenumberedFrom != "94" {
		t.Errorf("renumbered from %q, want 94", c.RenumberedFrom)
	}

	// A second move must not overwrite the first. The identifier says
	// article-94 and RenumberedFrom is the only field that agrees with it.
	c.Renumber("96")
	if c.RenumberedFrom != "94" {
		t.Errorf("a second move rewrote the origin to %q", c.RenumberedFrom)
	}
	if c.Number != "96" {
		t.Errorf("number is %q, want 96", c.Number)
	}
}

func TestRenumberToTheSameNumberChangesNothing(t *testing.T) {
	c := Component{ID: "vn:law:2019:45-2019-qh14:article-94", Kind: "article", Number: "94"}
	c.Renumber("94")
	if c.RenumberedFrom != "" {
		t.Errorf("renumbered from %q after no move", c.RenumberedFrom)
	}
}

func TestSplitLosesNothingFromTheProvisions(t *testing.T) {
	doc := labourCode()
	components, versions := Split(doc)

	text := map[string]TextVersion{}
	for _, v := range versions {
		text[v.ComponentID] = v
	}
	for i, p := range doc.Provisions {
		c := components[i]
		if c.Kind != p.Kind || c.Number != p.Number || c.Heading != p.Heading || c.Position != p.Position || c.ParentID != p.ParentID {
			t.Errorf("component %s does not carry the identity of its provision", c.ID)
		}
		v, ok := text[p.ID]
		if p.Text == "" {
			if ok {
				t.Errorf("component %s gained a version out of nowhere", c.ID)
			}
			continue
		}
		if !ok {
			t.Fatalf("component %s lost its text", c.ID)
		}
		if v.Text != p.Text || v.TextHash != p.TextHash {
			t.Errorf("component %s does not carry the content of its provision", c.ID)
		}
	}
}
