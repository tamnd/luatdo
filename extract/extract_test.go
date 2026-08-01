package extract

import (
	"context"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/ontology"
)

// scripted replays canned responses in order and records the inputs it saw, the
// same test discipline the api package uses: no network, no credentials.
type scripted struct {
	responses []string
	inputs    []string
}

func (s *scripted) Complete(_ context.Context, req api.Request) (api.Response, error) {
	if len(s.inputs) >= len(s.responses) {
		return api.Response{}, context.Canceled
	}
	s.inputs = append(s.inputs, req.Input)
	text := s.responses[len(s.inputs)-1]
	return api.Response{Text: text, Usage: api.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}}, nil
}

func testDoc() *law.Document {
	return &law.Document{
		ID:    "vn:law:2019:45-2019-qh14",
		Title: "Bộ luật Lao động",
		Provisions: []law.Provision{
			{ID: "vn:law:2019:45-2019-qh14:chapter-1", Kind: "chapter", Number: "1", Heading: "NHỮNG QUY ĐỊNH CHUNG"},
			{ID: "vn:law:2019:45-2019-qh14:article-3", ParentID: "vn:law:2019:45-2019-qh14:chapter-1", Kind: "article", Number: "3", Heading: "Giải thích từ ngữ"},
			{ID: "vn:law:2019:45-2019-qh14:article-3:clause-1", ParentID: "vn:law:2019:45-2019-qh14:article-3", Kind: "clause", Number: "1",
				Text: "Người lao động là người làm việc cho người sử dụng lao động theo thỏa thuận."},
		},
	}
}

func TestBuildWindow(t *testing.T) {
	doc := testDoc()
	w, err := BuildWindow(doc, "vn:law:2019:45-2019-qh14:article-3:clause-1")
	if err != nil {
		t.Fatalf("BuildWindow: %v", err)
	}
	if len(w.Path) != 3 || !strings.HasPrefix(w.Path[0], "chapter 1") {
		t.Errorf("path = %v, want chapter > article > clause", w.Path)
	}
	prompt := w.Prompt()
	for _, want := range []string{"Bộ luật Lao động", "Giải thích từ ngữ", w.Text} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt misses %q", want)
		}
	}
	if _, err := BuildWindow(doc, "vn:law:2019:45-2019-qh14:article-99"); err == nil {
		t.Error("unknown provision must error")
	}
}

func TestRunValid(t *testing.T) {
	c := &scripted{responses: []string{
		"```json\n{\"mentions\":[{\"text\":\"Người lao động\",\"class_id\":\"vn-legal:Employee\",\"quote\":\"Người lao động là người làm việc\"}],\"unresolved_mentions\":[{\"text\":\"thỏa thuận\",\"role\":\"object\",\"reason\":\"no matching class\"}]}\n```",
	}}
	e := &Extractor{Completer: c, Model: "m", Registry: ontology.Seed(), MaxCorrections: 2}
	job, err := e.Run(context.Background(), testDoc(), "vn:law:2019:45-2019-qh14:article-3:clause-1")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(job.Attempts) != 1 || len(job.Mentions) != 1 || len(job.Unresolved) != 1 {
		t.Fatalf("attempts=%d mentions=%d unresolved=%d", len(job.Attempts), len(job.Mentions), len(job.Unresolved))
	}
	if job.Mentions[0].ClassID != "vn-legal:Employee" {
		t.Errorf("class = %q", job.Mentions[0].ClassID)
	}
	if job.Usage.TotalTokens != 15 {
		t.Errorf("usage total = %d, want 15", job.Usage.TotalTokens)
	}
}

func TestRunCorrectsInvalidClass(t *testing.T) {
	c := &scripted{responses: []string{
		`{"mentions":[{"text":"Người lao động","class_id":"vn-legal:Invented","quote":"Người lao động"}]}`,
		`{"mentions":[{"text":"Người lao động","class_id":"vn-legal:Employee","quote":"Người lao động"}]}`,
	}}
	e := &Extractor{Completer: c, Model: "m", Registry: ontology.Seed(), MaxCorrections: 2}
	job, err := e.Run(context.Background(), testDoc(), "vn:law:2019:45-2019-qh14:article-3:clause-1")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(job.Attempts) != 2 || job.Attempts[0].Error == "" || job.Attempts[1].Error != "" {
		t.Fatalf("attempt history wrong: %+v", job.Attempts)
	}
	if !strings.Contains(c.inputs[1], "Lần trả lời trước bị lỗi") {
		t.Error("correction input must carry the validation error")
	}
	if job.Usage.TotalTokens != 30 {
		t.Errorf("usage total = %d, both attempts must be billed", job.Usage.TotalTokens)
	}
}

func TestRunRejectsNonVerbatimQuote(t *testing.T) {
	bad := `{"mentions":[{"text":"Người lao động","class_id":"vn-legal:Employee","quote":"nguoi lao dong lam viec"}]}`
	c := &scripted{responses: []string{bad, bad}}
	e := &Extractor{Completer: c, Model: "m", Registry: ontology.Seed(), MaxCorrections: 1}
	job, err := e.Run(context.Background(), testDoc(), "vn:law:2019:45-2019-qh14:article-3:clause-1")
	if err == nil {
		t.Fatal("run must fail when every attempt has a fabricated quote")
	}
	if len(job.Attempts) != 2 {
		t.Errorf("attempts = %d, want MaxCorrections+1", len(job.Attempts))
	}
	if len(job.Mentions) != 0 {
		t.Error("no mention may survive validation failure")
	}
}

func TestStripFences(t *testing.T) {
	cases := map[string]string{
		"{\"a\":1}":                    `{"a":1}`,
		"```json\n{\"a\":1}\n```":      `{"a":1}`,
		"```\n{\"a\":1}\n```":          `{"a":1}`,
		"  \n```json\n{\"a\":1}\n``` ": `{"a":1}`,
	}
	for in, want := range cases {
		if got := StripFences(in); got != want {
			t.Errorf("StripFences(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSample(t *testing.T) {
	var docs []*law.Document
	for range 3 {
		d := testDoc()
		d.Status = "parsed"
		docs = append(docs, d)
	}
	got := Sample(docs, 2)
	if len(got) != 2 {
		t.Fatalf("sample = %d, want 2", len(got))
	}
	again := Sample(docs, 2)
	for i := range got {
		if got[i].ProvisionID != again[i].ProvisionID {
			t.Error("sampling must be deterministic")
		}
	}
}
