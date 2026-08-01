package relation

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/concept"
)

// answers is a Completer that reads from a script, so an extraction test is
// about the extraction rather than about a network.
type answers struct {
	replies []string
	err     error
	inputs  []string
	calls   int
}

func (a *answers) Complete(_ context.Context, req api.Request) (api.Response, error) {
	a.inputs = append(a.inputs, req.Input)
	a.calls++
	if a.err != nil {
		return api.Response{}, a.err
	}
	i := min(a.calls-1, len(a.replies)-1)
	return api.Response{
		Text:  a.replies[i],
		Usage: api.Usage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110},
	}, nil
}

const clause = "Giấy phép xây dựng được cấp khi người đề nghị đã có giấy chứng nhận quyền sử dụng đất."

func cands() []Candidate {
	return []Candidate{
		{ID: "c1", LabelVI: "giấy phép xây dựng", Kind: concept.KindArtifact},
		{ID: "c2", LabelVI: "giấy chứng nhận quyền sử dụng đất", Kind: concept.KindArtifact},
	}
}

// quoteAt returns a quote and its real byte offsets, so a test never hand
// counts bytes in a Vietnamese sentence and gets them wrong.
func quoteAt(t *testing.T, text, quote string) (int, int) {
	t.Helper()
	i := strings.Index(text, quote)
	if i < 0 {
		t.Fatalf("the test quote is not in the test clause: %q", quote)
	}
	return i, i + len(quote)
}

func TestExtractReadsAProvision(t *testing.T) {
	start, end := quoteAt(t, clause, "đã có giấy chứng nhận quyền sử dụng đất")
	model := &answers{replies: []string{`{"relations":[{"subject_concept":"c1","relation":"REQUIRES",` +
		`"relation_as_written":"phải có trước khi được cấp","object_concept":"c2",` +
		`"quote":"đã có giấy chứng nhận quyền sử dụng đất","char_start":` + strconv.Itoa(start) + `,"char_end":` + strconv.Itoa(end) +
		`,"direction_check":"giấy phép cần giấy chứng nhận, không phải ngược lại","confidence":0.9}]}`}}

	x := &Extractor{Completer: model, Model: "m"}
	edges, usage, err := x.Extract(context.Background(), "p1", "d1", clause, cands())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("edges = %d", len(edges))
	}
	e := edges[0]
	if e.FromID != "c1" || e.ToID != "c2" || e.Type != Requires {
		t.Errorf("edge = %s %s %s", e.FromID, e.Type, e.ToID)
	}
	if e.Status != StatusProvisional || e.Why != WhySingleSupport {
		t.Errorf("status = %s why = %s, one sighting is one sentence", e.Status, e.Why)
	}
	if e.Source != SourceProvision || e.SupportCount != 1 || e.SupportDocs != 1 {
		t.Errorf("edge = %+v", e)
	}
	if len(e.Evidence) != 1 || e.Evidence[0].DirectionCheck == "" {
		t.Errorf("evidence = %+v, the direction check is read by a person when the blind pass disagrees", e.Evidence)
	}
	if e.Evidence[0].DocID != "d1" {
		t.Errorf("doc = %q, corroboration counts documents", e.Evidence[0].DocID)
	}
	if usage.TotalTokens != 110 {
		t.Errorf("usage = %+v, a campaign cannot report what it does not add up", usage)
	}
}

func TestExtractDoesNotCallTheModelForOneConcept(t *testing.T) {
	// One concept cannot relate to anything, and paying a model to be told so is
	// paying for a foregone conclusion.
	model := &answers{replies: []string{`{"relations":[]}`}}
	x := &Extractor{Completer: model, Model: "m"}
	edges, _, err := x.Extract(context.Background(), "p1", "d1", clause, cands()[:1])
	if err != nil || len(edges) != 0 {
		t.Fatalf("edges = %v err = %v", edges, err)
	}
	if model.calls != 0 {
		t.Errorf("the model was called %d times for a provision with one concept", model.calls)
	}
}

func TestExtractAcceptsAnEmptyAnswer(t *testing.T) {
	// A provision showing no specific relation is supposed to return nothing.
	// Treating that as a failure is what teaches a pipeline to accept RELATED_TO.
	model := &answers{replies: []string{`{"relations":[]}`}}
	x := &Extractor{Completer: model, Model: "m"}
	edges, _, err := x.Extract(context.Background(), "p1", "d1", clause, cands())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("edges = %v, an empty list is a correct answer", edges)
	}
	if model.calls != 1 {
		t.Errorf("calls = %d, an empty list is not something to correct", model.calls)
	}
}

func TestExtractCorrectsRatherThanAskingAgainIdentically(t *testing.T) {
	start, end := quoteAt(t, clause, "Giấy phép xây dựng")
	good := `{"relations":[{"subject_concept":"c1","relation":"REQUIRES","relation_as_written":"phải có",` +
		`"object_concept":"c2","quote":"Giấy phép xây dựng","char_start":` + strconv.Itoa(start) + `,"char_end":` + strconv.Itoa(end) +
		`,"direction_check":"c1 cần c2","confidence":0.8}]}`

	for name, tc := range map[string]struct {
		bad  string
		want string
	}{
		"not json": {`xin lỗi, tôi không thể`, "JSON"},
		"a concept nobody offered": {`{"relations":[{"subject_concept":"c9","relation":"REQUIRES","object_concept":"c2",` +
			`"quote":"Giấy phép xây dựng","char_start":0,"char_end":24,"direction_check":"x","confidence":0.5}]}`, "c9"},
		"the generic relation": {`{"relations":[{"subject_concept":"c1","relation":"RELATED_TO","object_concept":"c2",` +
			`"quote":"Giấy phép xây dựng","char_start":0,"char_end":24,"direction_check":"x","confidence":0.5}]}`, "RELATED_TO"},
		"a quote that is not there": {`{"relations":[{"subject_concept":"c1","relation":"REQUIRES","object_concept":"c2",` +
			`"quote":"điều này không có trong điều khoản","char_start":0,"char_end":10,"direction_check":"x","confidence":0.5}]}`, "không có trong nội dung"},
		"a quote at the wrong offsets": {`{"relations":[{"subject_concept":"c1","relation":"REQUIRES","object_concept":"c2",` +
			`"quote":"Giấy phép xây dựng","char_start":40,"char_end":58,"direction_check":"x","confidence":0.5}]}`, "không nằm ở vị trí"},
		"no direction check": {`{"relations":[{"subject_concept":"c1","relation":"REQUIRES","object_concept":"c2",` +
			`"quote":"Giấy phép xây dựng","char_start":0,"char_end":24,"direction_check":"  ","confidence":0.5}]}`, "direction_check"},
		"an unknown type with nothing said about it": {`{"relations":[{"subject_concept":"c1","relation":"DUOC_MIEN",` +
			`"object_concept":"c2","quote":"Giấy phép xây dựng","char_start":0,"char_end":24,"direction_check":"x","confidence":0.5}]}`,
			"relation_as_written"},
	} {
		model := &answers{replies: []string{tc.bad, good}}
		x := &Extractor{Completer: model, Model: "m", MaxCorrections: 1}
		edges, _, err := x.Extract(context.Background(), "p1", "d1", clause, cands())
		if err != nil {
			t.Errorf("%s: Extract: %v", name, err)
			continue
		}
		if len(edges) != 1 {
			t.Errorf("%s: edges = %d, want the corrected answer", name, len(edges))
			continue
		}
		if model.calls != 2 {
			t.Errorf("%s: calls = %d, want one correction", name, model.calls)
			continue
		}
		second := model.inputs[1]
		if !strings.Contains(second, "bị từ chối") {
			t.Errorf("%s: the second prompt did not say the first was rejected", name)
		}
		if !strings.Contains(second, tc.want) {
			t.Errorf("%s: the correction does not name the problem, want it to mention %q", name, tc.want)
		}
	}
}

func TestExtractGivesUpRatherThanReturningRubbish(t *testing.T) {
	model := &answers{replies: []string{"không phải JSON"}}
	x := &Extractor{Completer: model, Model: "m", MaxCorrections: 2}
	if _, _, err := x.Extract(context.Background(), "p1", "d1", clause, cands()); err == nil {
		t.Fatal("an answer that never parsed was returned as edges")
	}
	if model.calls != 3 {
		t.Errorf("calls = %d, want the first try and two corrections", model.calls)
	}
}

func TestExtractReportsTheTransportFailure(t *testing.T) {
	// A route that is down is the router's problem, and swallowing it here would
	// look like a provision with nothing in it.
	down := errors.New("dial tcp: connection refused")
	x := &Extractor{Completer: &answers{err: down}, Model: "m"}
	if _, _, err := x.Extract(context.Background(), "p1", "d1", clause, cands()); !errors.Is(err, down) {
		t.Errorf("err = %v, want the transport error", err)
	}
}

func TestExtractKeepsTheTailWithTheModelsOwnWords(t *testing.T) {
	// The tail is where the interesting law is. A type the registry does not
	// hold is kept provisional and carries the definition Define needs.
	start, end := quoteAt(t, clause, "được cấp khi")
	model := &answers{replies: []string{`{"relations":[{"subject_concept":"c1","relation":"duoc_mien_giay_phep",` +
		`"relation_as_written":"X được miễn không cần có Y","object_concept":"c2","quote":"được cấp khi",` +
		`"char_start":` + strconv.Itoa(start) + `,"char_end":` + strconv.Itoa(end) + `,"direction_check":"c1 miễn c2","confidence":0.7}]}`}}
	x := &Extractor{Completer: model, Model: "m"}
	edges, _, err := x.Extract(context.Background(), "p1", "d1", clause, cands())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("edges = %d, a type outside the registry is not dropped", len(edges))
	}
	e := edges[0]
	if e.Type != "DUOC_MIEN_GIAY_PHEP" {
		t.Errorf("type = %q, want it upper cased", e.Type)
	}
	if e.Why != WhyUnknownType || e.Status != StatusProvisional {
		t.Errorf("why = %q status = %q", e.Why, e.Status)
	}
	if e.Definition == "" {
		t.Error("no definition was carried, and canonicalization has nothing to match on")
	}
}

func TestExtractDropsARepeatedEdgeWithinOneProvision(t *testing.T) {
	start, end := quoteAt(t, clause, "Giấy phép xây dựng")
	one := `{"subject_concept":"c1","relation":"REQUIRES","relation_as_written":"phải có","object_concept":"c2",` +
		`"quote":"Giấy phép xây dựng","char_start":` + strconv.Itoa(start) + `,"char_end":` + strconv.Itoa(end) +
		`,"direction_check":"c1 cần c2","confidence":0.8}`
	model := &answers{replies: []string{`{"relations":[` + one + `,` + one + `]}`}}
	x := &Extractor{Completer: model, Model: "m"}
	edges, _, err := x.Extract(context.Background(), "p1", "d1", clause, cands())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("edges = %d, one provision saying it twice is one sighting", len(edges))
	}
}

func TestStripFencesTakesTheJSONOutOfAMarkdownBlock(t *testing.T) {
	for name, in := range map[string]string{
		"fenced":           "```json\n{\"relations\":[]}\n```",
		"fenced unlabeled": "```\n{\"relations\":[]}\n```",
		"bare":             `{"relations":[]}`,
		"padded":           "\n  {\"relations\":[]}  \n",
	} {
		if got := stripFences(in); got != `{"relations":[]}` {
			t.Errorf("%s: stripFences = %q", name, got)
		}
	}
}

func TestInstructionsNameEveryRelationInTheRegistry(t *testing.T) {
	// The prompt is built from the registry, so a type added to the vocabulary
	// is a type the model is told about without anyone editing prose.
	x := &Extractor{Completer: &answers{}, Model: "m"}
	got := x.Instructions()
	for _, id := range SeedRegistry(1).IDs() {
		if !strings.Contains(got, id) {
			t.Errorf("the prompt does not offer %s", id)
		}
	}
	if !strings.Contains(got, "RELATED_TO") {
		t.Error("the prompt does not warn against the generic attractor, which is what a model reaches for under uncertainty")
	}
	if !strings.Contains(got, "rỗng") {
		t.Error("the prompt does not say an empty list is a correct answer")
	}
	// Byte identical between runs, so a cached prompt stays cached.
	if got != x.Instructions() {
		t.Error("the prompt is not stable between calls")
	}
}

func TestResolverPrefersTheDefiningInstrument(t *testing.T) {
	// A phrase several instruments define means several things, and picking one
	// by string match is how a graph acquires confident wrong edges.
	terms := []concept.TermUse{
		{ID: "a1", LabelVI: "chủ đầu tư", ScopeID: "s1", Aliases: []string{"Chủ Đầu Tư"}},
		{ID: "a2", LabelVI: "chủ đầu tư", ScopeID: "s2"},
		{ID: "b1", LabelVI: "giấy phép xây dựng", ScopeID: "s1"},
	}
	r := NewResolver(terms)

	if id, ok := r.Resolve("s1", "chủ đầu tư"); !ok || id != "a1" {
		t.Errorf("in scope resolve = %q %v", id, ok)
	}
	if id, ok := r.Resolve("s2", "CHỦ ĐẦU TƯ"); !ok || id != "a2" {
		t.Errorf("the other instrument resolved to %q, and it defines its own", id)
	}
	if _, ok := r.Resolve("s3", "chủ đầu tư"); ok {
		t.Error("an ambiguous phrase resolved corpus wide, which is a confident wrong edge")
	}
	if id, ok := r.Resolve("s3", "giấy phép xây dựng"); !ok || id != "b1" {
		t.Errorf("a phrase only one instrument defines did not resolve corpus wide: %q %v", id, ok)
	}
	if id, ok := r.Resolve("s1", "Chủ Đầu Tư"); !ok || id != "a1" {
		t.Errorf("an alias did not resolve: %q %v", id, ok)
	}
	if _, ok := r.Resolve("s1", "  "); ok {
		t.Error("an empty phrase resolved to something")
	}
}

func TestDefinitionalDerivesWhatTheDrafterWrote(t *testing.T) {
	// A genus and an enumerated subtype are BROADER edges pointing opposite
	// ways, and getting them out costs one loop rather than a corpus of calls.
	terms := []concept.TermUse{
		{ID: "t1", LabelVI: "giấy phép xây dựng", ScopeID: "s1", DocID: "d1",
			Genus: "giấy phép", EnumeratedSubtypes: []string{"giấy phép xây dựng có thời hạn"},
			DefinedBy: "p1", Quote: "Giấy phép xây dựng là một loại giấy phép", Confidence: 0.95},
		{ID: "t2", LabelVI: "giấy phép", ScopeID: "s1", DocID: "d1"},
		{ID: "t3", LabelVI: "giấy phép xây dựng có thời hạn", ScopeID: "s1", DocID: "d1"},
	}
	edges := Definitional(terms, NewResolver(terms))
	if len(edges) != 2 {
		t.Fatalf("edges = %d, want the genus and the subtype", len(edges))
	}
	var up, down bool
	for _, e := range edges {
		if e.Type != Broader {
			t.Errorf("type = %q", e.Type)
		}
		if e.Status != StatusCanonical || e.Source != SourceDefinitional {
			t.Errorf("edge = %+v, a drafter wrote this one", e)
		}
		if len(e.Evidence) != 1 || e.Evidence[0].ProvisionID != "p1" {
			t.Errorf("evidence = %+v", e.Evidence)
		}
		if e.FromID == "t1" && e.ToID == "t2" {
			up = true
		}
		if e.FromID == "t3" && e.ToID == "t1" {
			down = true
		}
	}
	if !up {
		t.Error("the genus edge does not run from the term to its genus")
	}
	if !down {
		t.Error("the subtype edge does not run from the subtype to the term")
	}

	if err := edges[0].Validate(SeedRegistry(1), nil); err != nil {
		t.Errorf("a definitional edge does not validate: %v", err)
	}
}

func TestDefinitionalSkipsWhatItCannotGround(t *testing.T) {
	terms := []concept.TermUse{
		// No quote, so there is nothing to point at.
		{ID: "t1", LabelVI: "a", ScopeID: "s1", Genus: "b", DefinedBy: "p1"},
		// A genus nothing in the layer names.
		{ID: "t2", LabelVI: "b", ScopeID: "s1", Genus: "khái niệm không ai định nghĩa",
			DefinedBy: "p1", Quote: "b là..."},
		// A term whose genus is itself, which is not a hierarchy.
		{ID: "t3", LabelVI: "c", ScopeID: "s1", Genus: "c", DefinedBy: "p1", Quote: "c là c"},
	}
	if edges := Definitional(terms, NewResolver(terms)); len(edges) != 0 {
		t.Errorf("edges = %+v, want none of them", edges)
	}
}
