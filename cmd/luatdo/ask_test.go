package main

import (
	"flag"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/answer"
	"github.com/tamnd/luatdo/eval"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/retrieve"
)

func askCorpus() *corpus {
	doc := &law.Document{
		ID: "vn:law:2006:72-2006-qh11", Title: "Luật người lao động đi làm việc ở nước ngoài",
		Provisions: []law.Provision{
			{ID: "vn:law:2006:72-2006-qh11:article-10:clause-1", Kind: "clause", Number: "1",
				Text: "Hồ sơ cấp Giấy phép bao gồm văn bản đề nghị của doanh nghiệp."},
			{ID: "vn:law:2006:72-2006-qh11:article-15:clause-3", Kind: "clause", Number: "3",
				Text: "Bộ trưởng quyết định thu hồi Giấy phép."},
		},
	}
	rec := norm.Record{
		ID: "vn:norm:aaa", DocID: doc.ID, ProvisionID: doc.Provisions[1].ID, Status: norm.StatusVerified,
		Statement: norm.Statement{Type: "duty", Bearer: &norm.Ref{Text: "Bộ trưởng", IsActor: true},
			Action: norm.Ref{Text: "quyết định thu hồi"}},
	}
	c := &corpus{
		docs:   []*law.Document{doc},
		titles: map[string]string{doc.ID: doc.Title},
		byComp: map[string][]norm.Record{rec.ProvisionID: {rec}},
	}
	c.index = retrieve.Build(retrieve.Input{Docs: c.docs, Records: []norm.Record{rec}})
	return c
}

func TestTheAnswererIsHandedTheStatementsOfTheComponentsItWasGiven(t *testing.T) {
	c := askCorpus()
	res := c.index.Search(retrieve.Query{Text: "thu hồi Giấy phép", K: 2})
	sources := c.sources(res.Hits)
	if len(sources) == 0 {
		t.Fatal("nothing was retrieved, so there is nothing to hand over")
	}
	var found bool
	for _, s := range sources {
		if s.ComponentID != "vn:law:2006:72-2006-qh11:article-15:clause-3" {
			continue
		}
		found = true
		if len(s.Statements) != 1 || s.Statements[0].ID != "vn:norm:aaa" {
			t.Errorf("the component travelled without the statement that licenses a claim from it: %+v", s.Statements)
		}
		if s.Text == "" || s.Title == "" {
			t.Error("the component travelled without the words the answerer has to quote")
		}
	}
	if !found {
		t.Error("the clause the query names was not retrieved")
	}
}

// The three modes exist to be compared, so the only thing that may differ
// between them is what they retrieve.
func TestTheThreeModesDifferOnlyInWhatTheyRetrieve(t *testing.T) {
	c := askCorpus()
	flat := retrieve.BuildFlat(c.docs, retrieve.DefaultChunk, retrieve.DefaultOverlap)
	q := eval.BenchQuestion{N: 1, Kind: eval.KindAnswerable, Question: "Ai quyết định thu hồi Giấy phép?"}

	graphRanked, graphSources, _ := retrieveFor(c, flat, q, 5, "graph", false)
	if len(graphRanked) == 0 || len(graphSources) == 0 {
		t.Error("graph retrieval returned nothing for a question its own words answer")
	}
	flatRanked, flatSources, _ := retrieveFor(c, flat, q, 5, "flat", false)
	if len(flatRanked) == 0 || len(flatSources) == 0 {
		t.Error("the flat baseline returned nothing, so a comparison against it would be empty")
	}
	noneRanked, noneSources, _ := retrieveFor(c, flat, q, 5, "none", false)
	if len(noneRanked) != 0 || len(noneSources) != 0 {
		t.Error("the no retrieval baseline retrieved something")
	}
}

func TestScoringAnAnswerSeparatesAnInventedCitationFromAWeakQuote(t *testing.T) {
	q := eval.BenchQuestion{N: 1, Kind: eval.KindAnswerable, Gold: []string{"c1"}}
	a := answer.Answer{
		Claims: []answer.Claim{{Text: "một", ComponentID: "c1"}, {Text: "hai", ComponentID: "c2"}},
		Dropped: []answer.Dropped{
			{Reason: answer.DropUnknownComponent, Claim: answer.Claim{ComponentID: "c9"}},
			{Reason: answer.DropQuoteNotFound, Claim: answer.Claim{ComponentID: "c1"}},
		},
	}
	g := scoreAnswer(q, a)
	if g.Claims != 4 || g.Grounded != 2 {
		t.Errorf("%d claims, %d grounded, want the dropped ones counted as made", g.Claims, g.Grounded)
	}
	if g.OnGold != 1 {
		t.Errorf("%d on gold, want the one claim that cited the right component", g.OnGold)
	}
	// A paraphrased quote is the model being loose with evidence it really had.
	// An unknown component is the model inventing a citation. Only the second is
	// counted here.
	if g.Invented != 1 {
		t.Errorf("%d invented, want only the unknown component", g.Invented)
	}
}

func TestAnUnanswerableQuestionThatWasAnsweredIsNamedAsSuch(t *testing.T) {
	q := eval.BenchQuestion{N: 23, Kind: eval.KindUnanswerable}
	spoke := verdict(q, answer.Answer{Claims: []answer.Claim{{Text: "một"}}})
	if !strings.Contains(spoke, "cannot answer") {
		t.Errorf("verdict is %q", spoke)
	}
	if got := verdict(q, answer.Answer{Refused: true}); !strings.Contains(got, "right") {
		t.Errorf("verdict is %q", got)
	}
}

func TestScopeFlagsCarryEveryRetrievalPrimitive(t *testing.T) {
	var sf scopeFlags
	fs := flag.NewFlagSet("scope", flag.ContinueOnError)
	sf.bind(fs)
	if err := fs.Parse([]string{
		"--doc", "d1", "--subject", "thue", "--component", "d1:article-1",
		"--kind", "clause", "--date", "2006-06-01", "--statements", "--assume-unread",
	}); err != nil {
		t.Fatal(err)
	}
	s := sf.scope()
	if s.Empty() {
		t.Fatal("a scope with every flag set reads as empty")
	}
	if len(s.Docs) != 1 || len(s.Subjects) != 1 || len(s.Components) != 1 || len(s.Kinds) != 1 {
		t.Errorf("scope is %+v", s)
	}
	if s.Date != "2006-06-01" || !s.Statements || !s.Unread {
		t.Errorf("scope is %+v", s)
	}
	if sf.docs.String() != "d1" {
		t.Errorf("repeatable flag renders as %q", sf.docs.String())
	}
}
