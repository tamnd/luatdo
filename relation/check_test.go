package relation

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/concept"
)

// corpus is the provision text Check reads, because invariant 3 is not about
// the quote being well formed, it is about the quote being in the provision it
// names, and only the corpus can settle that.
func corpus(m map[string]string) Texts {
	return func(id string) (string, bool) {
		text, ok := m[id]
		return text, ok
	}
}

func canonical(from, typ, to, provision string) Edge {
	return Edge{
		FromID: from, ToID: to, Type: typ,
		Status: StatusCanonical, Source: SourceCorpus, Confidence: 0.9,
		Evidence:     []Evidence{{ProvisionID: provision, DocID: "d1"}},
		SupportCount: 2, SupportDocs: 2,
	}
}

func TestCheckPassesALayerWithNothingWrongWithIt(t *testing.T) {
	text := "Giấy phép xây dựng được cấp khi đã có giấy chứng nhận."
	e := canonical("c1", Requires, "c2", "p1")
	e.Evidence[0].Quote = "Giấy phép xây dựng"
	e.Evidence[0].CharEnd = len("Giấy phép xây dựng")

	l := &Layer{
		Registry: SeedRegistry(1),
		Edges:    []Edge{e},
		Kinds:    Kinds{"c1": concept.KindArtifact, "c2": concept.KindArtifact},
		Labels:   map[string]string{"c1": "giấy phép xây dựng", "c2": "giấy chứng nhận"},
	}
	if problems := l.Check(corpus(map[string]string{"p1": text})); len(problems) != 0 {
		t.Errorf("problems = %v, want none", problems)
	}
}

func TestCheckRefusesAnEdgeWhoseEndpointsAreNowhereNearTheTextItCites(t *testing.T) {
	// This is the one that earns its keep. A model asserting a plausible
	// relation between two concepts that never appear in the same provision has
	// nothing to cite, it kills the commonest relation hallucination outright,
	// and it costs a substring search.
	text := "Giấy phép xây dựng được cấp theo quy định."
	e := canonical("c1", Requires, "c9", "p1")
	e.Evidence[0].Quote = "Giấy phép xây dựng"
	e.Evidence[0].CharEnd = len("Giấy phép xây dựng")

	l := &Layer{
		Registry: SeedRegistry(1),
		Edges:    []Edge{e},
		Labels:   map[string]string{"c1": "giấy phép xây dựng", "c9": "quyền sử dụng đất"},
	}
	problems := l.Check(corpus(map[string]string{"p1": text}))
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want the endpoint that is not in the provision", problems)
	}
	if !strings.Contains(problems[0], "quyền sử dụng đất") {
		t.Errorf("problem = %q, it does not name what is missing", problems[0])
	}
}

func TestCheckFindsAQuoteThatMovedOrWasNeverThere(t *testing.T) {
	text := "Giấy phép xây dựng được cấp theo quy định."
	for name, ev := range map[string]Evidence{
		"never there":    {ProvisionID: "p1", Quote: "hoàn toàn khác", CharStart: 0, CharEnd: 5},
		"wrong offsets":  {ProvisionID: "p1", Quote: "Giấy phép xây dựng", CharStart: 20, CharEnd: 38},
		"absent clause":  {ProvisionID: "p404", Quote: "Giấy phép xây dựng", CharStart: 0, CharEnd: 24},
		"nothing quoted": {ProvisionID: "p1", Quote: "", CharStart: 0, CharEnd: 0},
	} {
		e := canonical("c1", Requires, "c2", "p1")
		e.Evidence = []Evidence{ev}
		l := &Layer{Registry: SeedRegistry(1), Edges: []Edge{e}}
		if problems := l.Check(corpus(map[string]string{"p1": text})); len(problems) == 0 {
			t.Errorf("%s: passed the check", name)
		}
	}
}

func TestCheckReportsTheSameEdgeTwiceAsADoubleCount(t *testing.T) {
	// Its support count would be a sum of two counts, and the corroboration gate
	// reads that count.
	e := canonical("c1", Requires, "c2", "p1")
	l := &Layer{Registry: SeedRegistry(1), Edges: []Edge{e, e}}
	problems := l.Check(nil)
	if len(problems) != 1 || !strings.Contains(problems[0], "twice") {
		t.Errorf("problems = %v, want the duplicate named", problems)
	}
}

func TestCheckPrintsABroaderCycleWhole(t *testing.T) {
	// A cycle almost always means two concepts should have been merged and the
	// merge queue missed them, so naming one edge would send a reviewer to fix
	// the wrong link.
	l := &Layer{Registry: SeedRegistry(1), Edges: []Edge{
		canonical("a", Broader, "b", "p1"),
		canonical("b", Broader, "c", "p2"),
		canonical("c", Broader, "a", "p3"),
	}}
	problems := l.Check(nil)
	var cycle string
	for _, p := range problems {
		if strings.HasPrefix(p, "BROADER cycle") {
			cycle = p
		}
	}
	if cycle == "" {
		t.Fatalf("no cycle was reported, problems = %v", problems)
	}
	for _, node := range []string{"a", "b", "c"} {
		if !strings.Contains(cycle, node) {
			t.Errorf("the path does not name %q: %s", node, cycle)
		}
	}
	if strings.Count(cycle, "->") != 3 {
		t.Errorf("the path is not closed: %s", cycle)
	}
}

func TestCheckReportsOneCycleOnceHoweverItIsReached(t *testing.T) {
	l := &Layer{Registry: SeedRegistry(1), Edges: []Edge{
		canonical("a", Broader, "b", "p1"),
		canonical("b", Broader, "a", "p2"),
		canonical("z", Broader, "a", "p3"),
	}}
	cycles := 0
	for _, p := range l.Check(nil) {
		if strings.HasPrefix(p, "BROADER cycle") {
			cycles++
		}
	}
	if cycles != 1 {
		t.Errorf("cycles = %d, want the same cycle reported once", cycles)
	}
}

func TestCheckIgnoresACycleThroughAProvisionalEdge(t *testing.T) {
	// A provisional edge is not a claim the layer makes, so a cycle that only
	// exists because of one is not a finding about the hierarchy.
	weak := canonical("c", Broader, "a", "p3")
	weak.Status, weak.Why = StatusProvisional, WhySingleSupport
	l := &Layer{Registry: SeedRegistry(1), Edges: []Edge{
		canonical("a", Broader, "b", "p1"),
		canonical("b", Broader, "c", "p2"),
		weak,
	}}
	for _, p := range l.Check(nil) {
		if strings.HasPrefix(p, "BROADER cycle") {
			t.Errorf("a cycle was reported through an edge the layer does not assert: %s", p)
		}
	}
}

func TestCheckIsDeterministic(t *testing.T) {
	edges := []Edge{
		canonical("z", Requires, "y", "p9"),
		{FromID: "x", ToID: "x", Type: Requires, Status: StatusCanonical, Source: SourceCorpus,
			Evidence: []Evidence{{ProvisionID: "p1"}}, SupportCount: 2, SupportDocs: 2},
		canonical("a", "RELATED_TO", "b", "p2"),
	}
	l := &Layer{Registry: SeedRegistry(1), Edges: edges}
	first := strings.Join(l.Check(nil), "\n")
	reversed := &Layer{Registry: SeedRegistry(1), Edges: []Edge{edges[2], edges[0], edges[1]}}
	if second := strings.Join(reversed.Check(nil), "\n"); first != second {
		t.Errorf("two runs read differently:\n%s\n\n%s", first, second)
	}
}

func TestDensitiesCatchRelationInflation(t *testing.T) {
	// The failure is a graph where every co-occurring pair acquires an edge, so
	// traversal returns everything and answers nothing. It looks fine per edge
	// and only shows up in the ratio.
	var edges []Edge
	for _, pair := range [][2]string{{"a", "b"}, {"a", "c"}, {"b", "c"}, {"a", "d"}, {"b", "d"}, {"c", "d"}} {
		e := canonical(pair[0], Requires, pair[1], "p1")
		e.Evidence[0].DocID = "inflated"
		edges = append(edges, e)
	}
	sparse := canonical("m", Requires, "n", "p2")
	sparse.Evidence[0].DocID = "sane"
	edges = append(edges, sparse)

	got := Densities(edges)
	if len(got) != 2 {
		t.Fatalf("densities = %+v", got)
	}
	if got[0].DocID != "inflated" {
		t.Errorf("worst = %q, want the dense one first", got[0].DocID)
	}
	if got[0].Ratio != 1.5 {
		t.Errorf("ratio = %v, want 6 edges over 4 nodes", got[0].Ratio)
	}
	if got[1].Ratio != 0.5 {
		t.Errorf("ratio = %v, want 1 edge over 2 nodes", got[1].Ratio)
	}
}

func TestCountPutsTheReviewQueueSizeWherePeopleAlreadyLook(t *testing.T) {
	// A review queue nobody works is a slow way of dropping the tail, and the
	// only defence is reporting its size next to the numbers people read.
	unknownA := canonical("a", "DUOC_MIEN", "b", "p1")
	unknownA.Status, unknownA.Why = StatusProvisional, WhyUnknownType
	unknownB := canonical("c", "HIEM_KHI", "d", "p2")
	unknownB.Status, unknownB.Why = StatusProvisional, WhyUnknownType
	thin := canonical("e", Requires, "f", "p3")
	thin.Status, thin.Why = StatusProvisional, WhySingleSupport

	c := Count([]Edge{canonical("g", Requires, "h", "p4"), unknownA, unknownB, thin})
	if c.Edges != 4 || c.Canonical != 1 || c.Provisional != 3 {
		t.Fatalf("counts = %+v", c)
	}
	if c.ProvisionalTypes != 2 {
		t.Errorf("types waiting on a person = %d, want the two invented ones", c.ProvisionalTypes)
	}
	if c.ByWhy[WhyUnknownType] != 2 || c.ByWhy[WhySingleSupport] != 1 {
		t.Errorf("by why = %v", c.ByWhy)
	}
	out := c.String()
	for _, want := range []string{"1 canonical", "3 provisional", WhyUnknownType, "waiting on a person"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report does not say %q:\n%s", want, out)
		}
	}
}

func TestCountSaysNothingAboutAQueueThatIsEmpty(t *testing.T) {
	out := Count([]Edge{canonical("a", Requires, "b", "p1")}).String()
	if strings.Contains(out, "waiting on a person") {
		t.Errorf("an empty review queue was reported as one:\n%s", out)
	}
}
