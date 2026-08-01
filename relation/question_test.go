package relation

import (
	"strings"
	"testing"
)

// asked builds an edge with the support counts every answer row carries, since
// an answer that prints a forty provision edge and a one circular edge alike has
// thrown away the only thing that lets a reader judge it.
func asked(from, typ, to string, provisions, docs int) Edge {
	return Edge{
		FromID: from, ToID: to, Type: typ,
		Status: StatusCanonical, Source: SourceCorpus,
		SupportCount: provisions, SupportDocs: docs,
		Evidence: []Evidence{{ProvisionID: "p1", DocID: "d1"}},
	}
}

func TestQuestion7WalksTheHierarchyTheCorpusActuallyUses(t *testing.T) {
	g := NewGraph([]Edge{
		asked("gpxd", Broader, "giay-phep", 12, 4),
		asked("gp-moi-truong", Broader, "giay-phep", 3, 2),
		asked("gpxd-nha-o", Broader, "gpxd", 2, 2),
		asked("khong-lien-quan", Requires, "giay-phep", 9, 3),
	}, map[string]string{
		"giay-phep":     "giấy phép",
		"gpxd":          "giấy phép xây dựng",
		"gpxd-nha-o":    "giấy phép xây dựng nhà ở riêng lẻ",
		"gp-moi-truong": "giấy phép môi trường",
	}, nil)

	q := g.AskQuestion7("giay-phep", 0)
	if q.MaxDepth != 5 {
		t.Errorf("depth = %d, an unbounded walk needs a default", q.MaxDepth)
	}
	if len(q.Children) != 3 {
		t.Fatalf("children = %+v, want the three BROADER edges and not the REQUIRES one", q.Children)
	}
	if q.Children[0].Depth != 1 || q.Children[2].Depth != 2 {
		t.Errorf("depths = %d %d %d", q.Children[0].Depth, q.Children[1].Depth, q.Children[2].Depth)
	}
	if q.Children[2].FromLabel != "giấy phép xây dựng nhà ở riêng lẻ" {
		t.Errorf("grandchild = %q, the walk did not go past depth 1", q.Children[2].FromLabel)
	}
	if q.Children[0].SupportCount != 3 || q.Children[0].SupportDocs != 2 {
		t.Errorf("row = %+v, an answer without its support counts cannot be judged", q.Children[0])
	}
}

func TestQuestion7StopsAtTheDepthItWasGiven(t *testing.T) {
	g := NewGraph([]Edge{
		asked("b", Broader, "a", 2, 2),
		asked("c", Broader, "b", 2, 2),
		asked("d", Broader, "c", 2, 2),
	}, nil, nil)
	if got := len(g.AskQuestion7("a", 2).Children); got != 2 {
		t.Errorf("children = %d, want the walk bounded at two levels", got)
	}
}

func TestQuestion7ReportsTwoParentsAsTheCorpusDisagreeingRatherThanAnError(t *testing.T) {
	// Two instruments putting the same thing under different parents is a real
	// fact about Vietnamese drafting, and the answer says so rather than picking
	// one and hiding the other.
	g := NewGraph([]Edge{
		asked("thua-dat", Broader, "bat-dong-san", 8, 3),
		asked("thua-dat", Broader, "tai-san", 5, 2),
	}, map[string]string{
		"thua-dat":     "thửa đất",
		"bat-dong-san": "bất động sản",
		"tai-san":      "tài sản",
	}, nil)

	q := g.AskQuestion7("bat-dong-san", 3)
	if len(q.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want the one concept with two parents", q.Conflicts)
	}
	if len(q.Conflicts[0].Parents) != 2 {
		t.Errorf("parents = %+v, want both of them named", q.Conflicts[0].Parents)
	}
	out := q.String()
	for _, want := range []string{"bất động sản", "tài sản", "corpus disagreeing rather than an error"} {
		if !strings.Contains(out, want) {
			t.Errorf("the answer does not say %q:\n%s", want, out)
		}
	}
}

func TestQuestion7ReportsOneConflictedConceptOnce(t *testing.T) {
	// It is reachable from the root once per parent, and listing it twice would
	// read as two separate drafting disagreements.
	g := NewGraph([]Edge{
		asked("x", Broader, "root", 2, 2),
		asked("x", Broader, "other", 2, 2),
		asked("other", Broader, "root", 2, 2),
	}, nil, nil)
	q := g.AskQuestion7("root", 3)
	if len(q.Conflicts) != 1 {
		t.Errorf("conflicts = %+v, want one row for one concept", q.Conflicts)
	}
}

func TestQuestion7SaysPlainlyWhenNoBroaderEdgeReachesTheRoot(t *testing.T) {
	out := NewGraph(nil, nil, nil).AskQuestion7("giay-phep", 3).String()
	if !strings.Contains(out, "no BROADER edge reaches it") {
		t.Errorf("an empty hierarchy printed as though it were an answer:\n%s", out)
	}
}

func TestQuestion21IsTheDirectTestOfWhetherThisLayerWorks(t *testing.T) {
	// REQUIRES outward and PRODUCES inward, from the graph alone with no text
	// read. If the chain is not there the answer is empty and the graph is a
	// glossary.
	g := NewGraph([]Edge{
		asked("gpxd", Requires, "gcn-qsdd", 40, 9),
		asked("gpxd", Requires, "ban-ve", 6, 2),
		asked("gcn-qsdd", Requires, "ho-so-dia-chinh", 4, 2),
		asked("cap-gpxd", Produces, "gpxd", 11, 5),
		asked("gpxd", Broader, "giay-phep", 3, 2),
	}, map[string]string{
		"gpxd":            "giấy phép xây dựng",
		"gcn-qsdd":        "giấy chứng nhận quyền sử dụng đất",
		"ban-ve":          "bản vẽ thiết kế",
		"ho-so-dia-chinh": "hồ sơ địa chính",
		"cap-gpxd":        "cấp giấy phép xây dựng",
	}, nil)

	q := g.AskQuestion21("gpxd", 0)
	if len(q.Prerequisites) != 3 {
		t.Fatalf("prerequisites = %+v, want the chain and nothing that is not REQUIRES", q.Prerequisites)
	}
	if q.Prerequisites[0].Depth != 1 || q.Prerequisites[1].Depth != 1 || q.Prerequisites[2].Depth != 2 {
		t.Errorf("depths are wrong: %+v", q.Prerequisites)
	}
	if q.Prerequisites[2].ToLabel != "hồ sơ địa chính" {
		t.Errorf("second level = %q, the walk did not follow the chain", q.Prerequisites[2].ToLabel)
	}
	if len(q.Produced) != 1 || q.Produced[0].FromLabel != "cấp giấy phép xây dựng" {
		t.Errorf("produced by = %+v, PRODUCES is read inward", q.Produced)
	}

	out := q.String()
	for _, want := range []string{"giấy chứng nhận quyền sử dụng đất", "40 provisions in 9 documents", "produced by cấp giấy phép xây dựng"} {
		if !strings.Contains(out, want) {
			t.Errorf("the answer does not say %q:\n%s", want, out)
		}
	}
}

func TestQuestion21SaysAnEmptyAnswerMeansTheLayerDidNotWork(t *testing.T) {
	// The alternative is printing "nothing" and letting a broken run look like a
	// concept with no prerequisites.
	out := NewGraph(nil, nil, nil).AskQuestion21("gpxd", 3).String()
	if !strings.Contains(out, "an empty answer here means the layer did not work") {
		t.Errorf("an empty prerequisite answer read as a finding:\n%s", out)
	}
}

func TestQuestion21DoesNotWalkTheSameConceptTwice(t *testing.T) {
	g := NewGraph([]Edge{
		asked("a", Requires, "b", 2, 2),
		asked("b", Requires, "a", 2, 2),
	}, nil, nil)
	q := g.AskQuestion21("a", 5)
	if len(q.Prerequisites) != 2 {
		t.Errorf("prerequisites = %d, a cycle walked to depth five is not an answer", len(q.Prerequisites))
	}
}

func TestQuestion22PutsTheUncitedPairsFirstBecauseThoseAreTheFinding(t *testing.T) {
	scope := map[string]string{
		"gpxd":     "luat-xay-dung",
		"gcn-qsdd": "luat-dat-dai",
		"phi":      "thong-tu-phi",
		"ho-so":    "luat-xay-dung",
	}
	g := NewGraph([]Edge{
		asked("gpxd", Requires, "gcn-qsdd", 40, 9),
		asked("gpxd", EvidencedBy, "phi", 3, 2),
		asked("gpxd", Requires, "ho-so", 12, 4),
		asked("gpxd", Broader, "gcn-qsdd", 2, 2),
	}, map[string]string{"gcn-qsdd": "giấy chứng nhận quyền sử dụng đất", "phi": "phí thẩm định"}, scope)

	cites := func(from, to string) bool { return from == "luat-xay-dung" && to == "luat-dat-dai" }
	q := g.AskQuestion22(cites)
	if len(q.Rows) != 2 {
		t.Fatalf("rows = %+v, want the two cross instrument edges of a type this question reads", q.Rows)
	}
	if q.Rows[0].Cited || !q.Rows[1].Cited {
		t.Errorf("rows = %+v, the uncited pair is the finding and belongs first", q.Rows)
	}
	if q.Rows[0].ConceptID != "phi" {
		t.Errorf("first = %q", q.Rows[0].ConceptID)
	}
	out := q.String()
	for _, want := range []string{"1 of them between instruments that do not cite each other", "no citation either way", "cites it"} {
		if !strings.Contains(out, want) {
			t.Errorf("the answer does not say %q:\n%s", want, out)
		}
	}
}

func TestQuestion22IgnoresWhatIsNotCrossInstrument(t *testing.T) {
	g := NewGraph([]Edge{asked("a", Requires, "b", 2, 2)},
		nil, map[string]string{"a": "luat-xay-dung", "b": "luat-xay-dung"})
	if rows := g.AskQuestion22(nil).Rows; len(rows) != 0 {
		t.Errorf("rows = %+v, both endpoints are defined in the same instrument", rows)
	}

	unscoped := NewGraph([]Edge{asked("a", Requires, "b", 2, 2)},
		nil, map[string]string{"a": "luat-xay-dung"})
	if rows := unscoped.AskQuestion22(nil).Rows; len(rows) != 0 {
		t.Errorf("rows = %+v, an endpoint nobody scoped is not evidence of a gap", rows)
	}
}

func TestQuestion23ReadsAlternativeToBothWays(t *testing.T) {
	// It is symmetric, so the direction an extractor happened to write it in is
	// not a fact about anything, and an answer that reads one way round would
	// depend on extraction order.
	g := NewGraph([]Edge{
		asked("so-do", AlternativeTo, "gcn-qsdd", 4, 2),
		asked("so-hong", AlternativeTo, "so-do", 30, 8),
		asked("so-do", Requires, "ho-so", 5, 3),
	}, map[string]string{
		"so-do":    "sổ đỏ",
		"so-hong":  "sổ hồng",
		"gcn-qsdd": "giấy chứng nhận quyền sử dụng đất",
	}, nil)

	q := g.AskQuestion23("so-do")
	if len(q.Alternatives) != 2 {
		t.Fatalf("alternatives = %+v, want both directions and nothing else", q.Alternatives)
	}
	if q.Alternatives[0].ToLabel != "sổ hồng" {
		t.Errorf("first = %q, want the one more of the corpus treats as interchangeable", q.Alternatives[0].ToLabel)
	}
	if q.Alternatives[0].FromID != "so-do" {
		t.Errorf("row reads from %q, an answer about sổ đỏ has sổ đỏ on the left", q.Alternatives[0].FromID)
	}
	if q.Alternatives[0].FromLabel != "sổ đỏ" {
		t.Errorf("labels were swapped without their identifiers: %+v", q.Alternatives[0])
	}
	if !strings.Contains(q.String(), "30 provisions in 8 documents") {
		t.Errorf("the answer dropped its support counts:\n%s", q.String())
	}
}

func TestQuestion23ReportsOnePairOnce(t *testing.T) {
	g := NewGraph([]Edge{
		asked("a", AlternativeTo, "b", 4, 2),
		asked("b", AlternativeTo, "a", 3, 2),
	}, nil, nil)
	if got := g.AskQuestion23("a").Alternatives; len(got) != 1 {
		t.Errorf("alternatives = %+v, the same pair written both ways is one alternative", got)
	}
}

func TestGraphFallsBackToIdentifiersWhenNobodyPassedLabels(t *testing.T) {
	g := NewGraph([]Edge{asked("a", Broader, "b", 2, 2)}, nil, nil)
	if got := g.Label("a"); got != "a" {
		t.Errorf("label = %q, an answer with no labels is less readable and no less correct", got)
	}
}
