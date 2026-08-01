package concept

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func termIn(scope, label string, opts ...func(*TermUse)) TermUse {
	t := TermUse{
		ID:        TermUseID(scope, label),
		LabelVI:   label,
		ScopeID:   scope,
		DocID:     scope,
		Kind:      KindActor,
		Origin:    OriginDefined,
		DefinedBy: scope + ":article-3:clause-1",
		Quote:     label,
	}
	for _, o := range opts {
		o(&t)
	}
	return t
}

func with(def string) func(*TermUse) {
	return func(t *TermUse) { t.DefinitionVI = def }
}

func aliased(a ...string) func(*TermUse) {
	return func(t *TermUse) { t.Aliases = a }
}

func TestClusteringProposesAcrossInstrumentsAndNeverWithinOne(t *testing.T) {
	terms := []TermUse{
		termIn("vn:law:2019:45-2019-qh14", "Người lao động"),
		termIn("vn:law:2014:58-2014-qh13", "Người lao động"),
		// Two readings of one phrase inside one instrument are a reading
		// problem, not a merge candidate, so nothing pairs them.
		termIn("vn:law:2014:58-2014-qh13", "Người lao động "),
		termIn("vn:law:2013:45-2013-qh13", "Thửa đất"),
	}
	links := Links(terms)
	for _, l := range links {
		if strings.Contains(l.A, "58-2014") && strings.Contains(l.B, "58-2014") {
			t.Errorf("two terms in one instrument were paired: %+v", l)
		}
	}
	clusters := Clusters(terms, links)
	if len(clusters) != 1 {
		t.Fatalf("got %d clusters, want the one worth asking about: %+v", len(clusters), clusters)
	}
	c := clusters[0]
	if len(c.Members) != 2 || c.Anchor != c.Members[0] {
		t.Errorf("cluster = %+v", c)
	}
	if len(c.Pairs()) != len(c.Members)-1 {
		t.Errorf("a cluster of %d members asks %d questions, want %d", len(c.Members), len(c.Pairs()), len(c.Members)-1)
	}
}

func TestAnAliasAndAWordingFindWhatALabelMatchMisses(t *testing.T) {
	terms := []TermUse{
		termIn("vn:doc:a", "Ủy ban nhân dân cấp tỉnh"),
		termIn("vn:doc:b", "Ủy ban nhân dân tỉnh, thành phố trực thuộc trung ương",
			aliased("Ủy ban nhân dân cấp tỉnh")),
		termIn("vn:doc:c", "Bên nhận bảo đảm", with("tổ chức tín dụng nhận tài sản bảo đảm để bảo đảm nghĩa vụ trả nợ")),
		termIn("vn:doc:d", "Bên nhận thế chấp", with("tổ chức tín dụng nhận tài sản bảo đảm để bảo đảm nghĩa vụ trả nợ vay")),
	}
	found := map[string]bool{}
	for _, l := range Links(terms) {
		found[l.Basis] = true
	}
	if !found["alias"] {
		t.Error("the drafter's own alias declaration did not link the two labels")
	}
	if !found["definition"] {
		t.Error("two near identical definitions under different labels were never proposed")
	}
	if len(Clusters(terms, Links(terms))) != 2 {
		t.Errorf("clusters = %+v", Clusters(terms, Links(terms)))
	}
}

func TestNothingButAHumanAnswerCreatesAConcept(t *testing.T) {
	terms := []TermUse{
		termIn("vn:law:2014:58-2014-qh13", "Người lao động"),
		termIn("vn:law:2019:45-2019-qh14", "Người lao động"),
	}
	clusters := Clusters(terms, Links(terms))
	if len(clusters) != 1 {
		t.Fatalf("clusters = %+v", clusters)
	}

	// Clustering and comparison together produce no concepts at all. That is
	// the point: the pipeline can propose all day and the graph stays silent.
	if layer := Apply(terms, nil); len(layer.Concepts) != 0 || len(layer.Memberships) != 0 {
		t.Fatalf("a run with no answers created %+v", layer)
	}

	pair := clusters[0].Pairs()[0]
	layer := Apply(terms, []Answer{{
		ClusterID: clusters[0].ID, A: pair[0], B: pair[1],
		Verdict: RelationSame, Rationale: "cùng phạm vi chủ thể, cùng điều kiện làm việc theo thỏa thuận",
		DecidedBy: "tamnd", DecidedAt: "2026-08-01T00:00:00Z",
	}})
	if len(layer.Concepts) != 1 || len(layer.Memberships) != 2 {
		t.Fatalf("one merge produced %d concepts and %d memberships", len(layer.Concepts), len(layer.Memberships))
	}
	for _, m := range layer.Memberships {
		if m.DecidedBy != "tamnd" || m.Rationale == "" {
			t.Errorf("membership %+v lost its provenance", m)
		}
	}
	if problems := layer.Check(); len(problems) != 0 {
		t.Errorf("a properly decided merge failed the invariants:\n%s", strings.Join(problems, "\n"))
	}
}

func TestDisagreementIsRecordedRatherThanResolved(t *testing.T) {
	a := termIn("vn:law:2019:45-2019-qh14", "Người lao động",
		with("người làm việc theo hợp đồng lao động"))
	a.Differentiae = []Differentia{{Text: "làm việc theo hợp đồng lao động", Quote: "hợp đồng lao động"}}
	b := termIn("vn:law:2014:58-2014-qh13", "Người lao động",
		with("người tham gia bảo hiểm xã hội bắt buộc"))
	b.Differentiae = []Differentia{{Text: "tham gia bảo hiểm xã hội bắt buộc", Quote: "bảo hiểm xã hội"}}
	terms := []TermUse{a, b}

	layer := Apply(terms, []Answer{{
		A: a.ID, B: b.ID, Verdict: RelationDiffers,
		Rationale: "phạm vi bên bảo hiểm xã hội gồm cả người không có hợp đồng lao động",
		DecidedBy: "tamnd", DecidedAt: "2026-08-01T00:00:00Z",
	}})
	if len(layer.Concepts) != 0 {
		t.Fatalf("a difference minted a concept: %+v", layer.Concepts)
	}
	if len(layer.Differences) != 1 {
		t.Fatalf("differences = %+v", layer.Differences)
	}
	// The edge carries what the two readings disagree about, computed from the
	// differentiae rather than asked for, so the disagreement is evidence and
	// not an opinion.
	if len(layer.Differences[0].Basis) != 2 {
		t.Errorf("basis = %v, want both distinguishing features", layer.Differences[0].Basis)
	}
	if problems := layer.Check(); len(problems) != 0 {
		t.Errorf("a recorded disagreement failed the invariants:\n%s", strings.Join(problems, "\n"))
	}
}

func TestBroaderIsRecordedFromTheConceptsSide(t *testing.T) {
	a := termIn("vn:doc:a", "Phương tiện giao thông")
	b := termIn("vn:doc:b", "Phương tiện giao thông")
	// The answer says A is broader than B. Seen from the concept A named, B is
	// the narrower member, and getting this backwards inverts every hierarchy
	// query over the layer.
	layer := Apply([]TermUse{a, b}, []Answer{{
		A: a.ID, B: b.ID, Verdict: RelationBroader,
		Rationale: "định nghĩa A gồm cả phương tiện thủy", DecidedBy: "tamnd", DecidedAt: "2026-08-01T00:00:00Z",
	}})
	var got string
	for _, m := range layer.Memberships {
		if m.TermUseID == b.ID {
			got = m.Relation
		}
	}
	if got != RelationNarrower {
		t.Errorf("B joined the concept as %q, want %q", got, RelationNarrower)
	}
}

func TestDeferLeavesTheGraphSayingNothing(t *testing.T) {
	a := termIn("vn:doc:a", "Cơ quan có thẩm quyền")
	b := termIn("vn:doc:b", "Cơ quan có thẩm quyền")
	layer := Apply([]TermUse{a, b}, []Answer{{
		A: a.ID, B: b.ID, Verdict: VerdictDefer,
		Rationale: "cần đọc thêm điều 4 của cả hai văn bản", DecidedBy: "tamnd",
	}})
	if len(layer.Concepts) != 0 || len(layer.Memberships) != 0 || len(layer.Differences) != 0 {
		t.Errorf("a deferred question wrote %+v", layer)
	}
}

func TestALaterAnswerCorrectsAnEarlierOne(t *testing.T) {
	a := termIn("vn:doc:a", "Thửa đất")
	b := termIn("vn:doc:b", "Thửa đất")
	answers := []Answer{
		{A: a.ID, B: b.ID, Verdict: RelationSame, Rationale: "giống nhau", DecidedBy: "tamnd", DecidedAt: "2026-08-01T00:00:00Z"},
		{A: a.ID, B: b.ID, Verdict: RelationDiffers, Rationale: "đọc lại thì phạm vi khác nhau", DecidedBy: "tamnd", DecidedAt: "2026-08-02T00:00:00Z"},
	}
	layer := Apply([]TermUse{a, b}, answers)
	if len(layer.Memberships) != 0 || len(layer.Differences) != 1 {
		t.Errorf("the correction did not take: %+v", layer)
	}
}

func TestTheQueueDoesNotReAskWhatWasSettled(t *testing.T) {
	dir := t.TempDir()
	a := termIn("vn:doc:a", "Thửa đất")
	b := termIn("vn:doc:b", "Thửa đất")
	c := termIn("vn:doc:c", "Thửa đất")
	qs := []Question{
		{ClusterID: "vn:cluster:thua-dat", A: a, B: b, At: "2026-08-01T00:00:00Z"},
		{ClusterID: "vn:cluster:thua-dat", A: a, B: c, At: "2026-08-01T00:00:00Z"},
	}
	if err := AskQuestions(dir, qs); err != nil {
		t.Fatalf("AskQuestions: %v", err)
	}
	if err := RecordAnswers(dir, []Answer{{A: a.ID, B: b.ID, Verdict: RelationSame, Rationale: "cùng nghĩa", DecidedBy: "tamnd"}}); err != nil {
		t.Fatalf("RecordAnswers: %v", err)
	}
	// A second clustering run over a corpus that gained a document re-proposes
	// everything, and the queue has to survive that without asking a reviewer
	// the same question twice.
	if err := AskQuestions(dir, qs); err != nil {
		t.Fatalf("AskQuestions: %v", err)
	}

	questions, err := ReadQuestions(dir)
	if err != nil {
		t.Fatalf("ReadQuestions: %v", err)
	}
	answers, err := ReadAnswers(dir)
	if err != nil {
		t.Fatalf("ReadAnswers: %v", err)
	}
	pending := Pending(questions, answers)
	if len(pending) != 1 || pending[0].B.ID != c.ID {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestClustersAndJobsSurviveTheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	terms := []TermUse{termIn("vn:doc:a", "Thửa đất"), termIn("vn:doc:b", "Thửa đất")}
	clusters := Clusters(terms, Links(terms))
	if err := WriteClusters(dir, clusters); err != nil {
		t.Fatalf("WriteClusters: %v", err)
	}
	got, err := ReadClusters(dir)
	if err != nil || len(got) != 1 || got[0].Anchor != clusters[0].Anchor {
		t.Fatalf("ReadClusters = %+v, %v", got, err)
	}

	job := Job{UnitID: "vn:doc:a:article-3:clause-1", DocID: "vn:doc:a", ScopeID: "vn:doc:a", TermUses: terms[:1]}
	if err := WriteJob(dir, []Job{job}); err != nil {
		t.Fatalf("WriteJob: %v", err)
	}
	jobs, err := ReadJob(dir, "vn:doc:a")
	if err != nil || len(jobs) != 1 || len(TermUses(jobs)) != 1 {
		t.Fatalf("ReadJob = %+v, %v", jobs, err)
	}
	if strings.Contains(filepath.Base(JobPath(dir, "vn:doc:a")), ":") {
		t.Error("the job file name carries a colon, which is not a legal file name on Windows")
	}
	// A document nobody has read is not an error. Most of the corpus has no
	// definitions article at all.
	if jobs, err := ReadJob(dir, "vn:doc:never-read"); err != nil || jobs != nil {
		t.Errorf("ReadJob on an unread document = %+v, %v", jobs, err)
	}
}

func TestTheComparerAdvisesAndDecidesNothing(t *testing.T) {
	a := termIn("vn:doc:a", "Người lao động", with("người làm việc theo hợp đồng"))
	b := termIn("vn:doc:b", "Người lao động", with("người tham gia bảo hiểm xã hội"))
	model := &scripted{replies: []string{
		`{"relation":"maybe","rationale":"x"}`,
		`{"relation":"differs","rationale":"phạm vi khác nhau","differing_features":["hợp đồng lao động"],"confidence":0.7}`,
	}}
	c := &Comparer{Completer: model, Model: "test", MaxCorrections: 2}

	got, usage, err := c.Compare(context.Background(), &a, &b)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if model.calls != 2 {
		t.Errorf("a relation outside the enum was accepted, calls = %d", model.calls)
	}
	if got.Relation != RelationDiffers || got.A != a.ID || got.B != b.ID {
		t.Errorf("comparison = %+v", got)
	}
	if usage.TotalTokens == 0 {
		t.Error("the comparison did not carry its cost")
	}

	// A comparison is advice. Feeding it straight into Apply is impossible:
	// Apply reads answers, and an answer needs a decider and a rationale that
	// only a person supplies.
	if layer := Apply([]TermUse{a, b}, nil); len(layer.Memberships) != 0 {
		t.Error("a comparison reached the graph without a person")
	}
}

func TestTheComparerIsAllowedToSayItDoesNotKnow(t *testing.T) {
	a := termIn("vn:doc:a", "Đơn vị")
	b := termIn("vn:doc:b", "Đơn vị")
	model := &scripted{replies: []string{"nonsense"}}
	c := &Comparer{Completer: model, Model: "test", MaxCorrections: 1}

	got, _, err := c.Compare(context.Background(), &a, &b)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	// A model that cannot answer must not stall the queue, and it must not be
	// read as agreement either. The pair goes to a person with no advice.
	if got.Relation != RelationUnclear {
		t.Errorf("comparison = %+v", got)
	}
}
