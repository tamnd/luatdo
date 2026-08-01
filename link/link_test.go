package link

import (
	"testing"

	"github.com/tamnd/luatdo/extract"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/term"
)

func job(mentions ...extract.Mention) *extract.Job {
	return &extract.Job{
		ProvisionID: "vn:law:2019:45-2019-qh14:article-3:clause-1",
		DocID:       "vn:law:2019:45-2019-qh14",
		Mentions:    mentions,
	}
}

func linker() *Linker {
	defs := []term.Definition{
		{TermID: "vn:term:hop-dong-lao-dong", Term: "Hợp đồng lao động", DocID: "vn:law:2019:45-2019-qh14"},
		{TermID: "vn:term:to-chuc-tin-dung", Term: "Tổ chức tín dụng", DocID: "vn:law:2010:47-2010-qh12"},
	}
	return New(ontology.Seed(), defs)
}

func TestResolvePrecedence(t *testing.T) {
	l := linker()
	got := l.Resolve(job(
		extract.Mention{Text: "Hợp đồng lao động", ClassID: "vn-legal:Event"},
		extract.Mention{Text: "Người sử dụng lao động", ClassID: "vn-legal:Employer"},
		extract.Mention{Text: "Tổ chức tín dụng", ClassID: "vn-legal:Organization"},
		extract.Mention{Text: "khái niệm chưa từng thấy", ClassID: "vn-legal:Organization"},
	))
	if len(got) != 4 {
		t.Fatalf("resolutions = %d, want 4", len(got))
	}
	sameDoc := got[0]
	if sameDoc.TargetKind != "term" || sameDoc.Basis != "term-same-doc" || sameDoc.Score != 1.0 {
		t.Errorf("same-doc term = %+v", sameDoc)
	}
	if sameDoc.TargetID != "vn:term:hop-dong-lao-dong" {
		t.Errorf("target = %q", sameDoc.TargetID)
	}
	label := got[1]
	if label.TargetKind != "class" || label.TargetID != "vn-legal:Employer" || label.Basis != "label" || label.Score != 1.0 {
		t.Errorf("class label = %+v", label)
	}
	corpus := got[2]
	if corpus.TargetKind != "term" || corpus.Basis != "term-corpus" || corpus.Score != 0.8 {
		t.Errorf("corpus term = %+v", corpus)
	}
	unresolved := got[3]
	if unresolved.TargetKind != "unresolved" || unresolved.Score != 0 {
		t.Errorf("unresolved = %+v", unresolved)
	}
	if unresolved.ClassID != "vn-legal:Organization" {
		t.Error("the extractor class assignment must survive on unresolved mentions")
	}
}

func TestResolveAliasAndLabelPrecedence(t *testing.T) {
	reg := &ontology.Registry{Version: 1, Classes: []ontology.Class{
		{ID: "vn-legal:Employer", LabelVI: "Người sử dụng lao động", Aliases: []string{"bên sử dụng lao động"}},
		{ID: "vn-legal:Enterprise", LabelVI: "Doanh nghiệp", Aliases: []string{"người sử dụng lao động"}},
	}}
	got := New(reg, nil).Resolve(job(
		extract.Mention{Text: "bên sử dụng lao động", ClassID: "vn-legal:Employer"},
		extract.Mention{Text: "Người sử dụng lao động", ClassID: "vn-legal:Employer"},
	))
	alias := got[0]
	if alias.TargetKind != "class" || alias.TargetID != "vn-legal:Employer" || alias.Basis != "alias" || alias.Score != 0.9 {
		t.Errorf("alias resolution = %+v", alias)
	}
	label := got[1]
	if label.TargetID != "vn-legal:Employer" || label.Basis != "label" {
		t.Errorf("label must win over a colliding alias, got %+v", label)
	}
}

func TestResolveCarriesUnresolvedRoles(t *testing.T) {
	j := job()
	j.Unresolved = []extract.Unresolved{{Text: "quỹ tương lai", Role: "subject", Reason: "no class"}}
	got := linker().Resolve(j)
	if len(got) != 1 || got[0].TargetKind != "unresolved" || got[0].Role != "subject" {
		t.Errorf("unresolved roles = %+v", got)
	}
}
