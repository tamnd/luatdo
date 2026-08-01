package temporal

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/law"
)

// published is a văn bản hợp nhất of the target law with the 2022 amendment
// already applied, which is what the office of the drafter publishes and what
// the version graph has to reproduce.
func published(clause2Text string) *law.Document {
	d := &law.Document{
		ID: "vn:law:2022:1-2022-vbhn-vpqh", OfficialNumber: "01/2022/VBHN-VPQH",
		Title: "Văn bản hợp nhất Bộ luật Lao động", EffectiveFrom: "2022-07-01",
	}
	for _, p := range target().Provisions {
		p.ID = strings.Replace(p.ID, docID, d.ID, 1)
		if p.ParentID != "" {
			p.ParentID = strings.Replace(p.ParentID, docID, d.ID, 1)
		}
		if strings.HasSuffix(p.ID, "article-15:clause-2") {
			p.Text = clause2Text
		}
		d.Provisions = append(d.Provisions, p)
	}
	return d
}

func amendedView(t *testing.T) *View {
	t.Helper()
	o := op("a1", KindAmend, clause2, "2022-07-01")
	o.NewText = "2. Tự nguyện, bình đẳng, thiện chí."
	l, _ := Build(corpus(), []Operation{o})
	return NewView(l)
}

func TestIsConsolidated(t *testing.T) {
	if !IsConsolidated(published("x")) {
		t.Error("a document numbered VBHN with hợp nhất in its title was not recognised")
	}
	if IsConsolidated(target()) {
		t.Error("an ordinary law was read as a consolidated text")
	}
	if IsConsolidated(nil) {
		t.Error("nil is not a consolidated text")
	}
}

func TestCompareAgreesWithACorrectlyAppliedAmendment(t *testing.T) {
	m := Compare(amendedView(t), docID, published("Tự nguyện, bình đẳng, thiện chí."), "2022-07-01")

	if len(m.Divergences) != 0 {
		t.Fatalf("the computed text and the published text disagree:\n%s", m.String())
	}
	if m.Compared == 0 || m.Agreed != m.Compared {
		t.Errorf("compared %d and agreed %d", m.Compared, m.Agreed)
	}
	if m.Rate() != 1 {
		t.Errorf("rate is %v", m.Rate())
	}
}

// This is the check that can prove the layer wrong rather than merely
// inconsistent, so the test is about it catching something.
func TestCompareCatchesAWrongAmendment(t *testing.T) {
	m := Compare(amendedView(t), docID, published("Một nội dung hoàn toàn khác."), "2022-07-01")

	if len(m.Divergences) != 1 {
		t.Fatalf("want one divergence, got %d:\n%s", len(m.Divergences), m.String())
	}
	d := m.Divergences[0]
	if d.Path != "article-15:clause-2" {
		t.Errorf("the divergence names %q", d.Path)
	}
	if d.Reason != DivergeTextDiffers {
		t.Errorf("the reason is %q", d.Reason)
	}
	if d.Computed == "" || d.Published == "" {
		t.Error("a divergence that shows neither text cannot be reviewed")
	}
	if m.Rate() >= 1 {
		t.Errorf("the rate is %v with a divergence present", m.Rate())
	}
}

func TestCompareCatchesAMissingComponent(t *testing.T) {
	v := amendedView(t)
	pub := published("Tự nguyện, bình đẳng, thiện chí.")
	pub.Provisions = pub.Provisions[:len(pub.Provisions)-1] // drop point c

	m := Compare(v, docID, pub, "2022-07-01")
	found := false
	for _, d := range m.Divergences {
		if d.Reason == DivergeMissingInPublished {
			found = true
		}
	}
	if !found {
		t.Errorf("a component the consolidated text does not have was not reported:\n%s", m.String())
	}
}

func TestCompareIgnoresLineWrapping(t *testing.T) {
	m := Compare(amendedView(t), docID, published(" Tự nguyện,\n   bình đẳng, thiện chí."), "2022-07-01")
	if len(m.Divergences) != 0 {
		t.Errorf("two publishers wrapping lines differently is not a divergence:\n%s", m.String())
	}
}

func TestCompareDoesNotFoldDiacritics(t *testing.T) {
	// Folding diacritics would hide exactly the phrase edits this check exists
	// to catch.
	m := Compare(amendedView(t), docID, published("Tu nguyen, binh dang, thien chi."), "2022-07-01")
	if len(m.Divergences) != 1 {
		t.Errorf("a text without diacritics was read as the same text:\n%s", m.String())
	}
}

func TestCompareAtTheWrongDateShowsTheOldText(t *testing.T) {
	m := Compare(amendedView(t), docID, published("Tự nguyện, bình đẳng, thiện chí."), "2021-06-01")
	if len(m.Divergences) == 0 {
		t.Error("comparing the consolidated text against a date before the amendment must diverge")
	}
}
