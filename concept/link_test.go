package concept

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Two instruments define one phrase, and they do not mean the same thing by it.
// This is the whole reason pass D exists.
func twoSenses() []TermUse {
	return []TermUse{
		{
			ID: "vn:term:vn:law:2019:45-2019-qh14:nguoi-lao-dong", LabelVI: "người lao động",
			ScopeID: "vn:law:2019:45-2019-qh14", DocID: "vn:law:2019:45-2019-qh14",
			Origin: OriginDefined, DefinitionVI: "người làm việc cho người sử dụng lao động theo thỏa thuận",
		},
		{
			ID: "vn:term:vn:law:2014:58-2014-qh13:nguoi-lao-dong", LabelVI: "người lao động",
			ScopeID: "vn:law:2014:58-2014-qh13", DocID: "vn:law:2014:58-2014-qh13",
			Origin: OriginDefined, DefinitionVI: "người tham gia bảo hiểm xã hội bắt buộc",
		},
	}
}

func TestScanFindsTheLongerTermFirst(t *testing.T) {
	// A sentence containing người sử dụng lao động must not also produce a
	// mention of người lao động sitting inside it.
	ix := NewIndex([]TermUse{
		{ID: "a", LabelVI: "người lao động", ScopeID: "s", DocID: "d"},
		{ID: "b", LabelVI: "người sử dụng lao động", ScopeID: "s", DocID: "d"},
	})
	text := "Người sử dụng lao động phải trả lương cho người lao động."
	mentions := ix.Scan("d2", "d2:article-1", text)
	if len(mentions) != 1 {
		t.Fatalf("want one mention, got %d: %v", len(mentions), mentions)
	}
	if mentions[0].Surface != "người lao động" {
		t.Errorf("surface %q", mentions[0].Surface)
	}
	if mentions[0].CharStart == 0 {
		t.Error("the match inside the longer term was taken")
	}
}

func TestScanRespectsWordBoundaries(t *testing.T) {
	ix := NewIndex([]TermUse{{ID: "a", LabelVI: "lao động", ScopeID: "s", DocID: "d"}})
	if m := ix.Scan("d2", "p", "hợp đồng lao độngx là"); len(m) != 0 {
		t.Errorf("matched across a syllable boundary: %v", m)
	}
	if m := ix.Scan("d2", "p", "hợp đồng lao động là"); len(m) != 1 {
		t.Errorf("missed a real mention: %v", m)
	}
}

func TestScanFindsAliases(t *testing.T) {
	ix := NewIndex([]TermUse{{
		ID: "a", LabelVI: "người lao động", Aliases: []string{"NLĐ"}, ScopeID: "s", DocID: "d",
	}})
	mentions := ix.Scan("d2", "p", "NLĐ được nghỉ hằng năm")
	if len(mentions) != 1 || mentions[0].Surface != "NLĐ" {
		t.Fatalf("an alias was not scanned for: %v", mentions)
	}
}

func TestResolveInsideTheDefiningInstrumentIsSettledByCode(t *testing.T) {
	// Trong Luật này says so. There is nothing here to understand, so nothing
	// here is asked of a model.
	ix := NewIndex(twoSenses())
	m := Mention{DocID: "vn:law:2019:45-2019-qh14", ProvisionID: "p", Surface: "người lao động"}
	ix.Resolve(&m, "vn:law:2019:45-2019-qh14", Corpus{}, "2021-01-01")
	if m.Method != MethodInScope {
		t.Fatalf("method %q, want %q", m.Method, MethodInScope)
	}
	if m.TermUseID != "vn:term:vn:law:2019:45-2019-qh14:nguoi-lao-dong" {
		t.Errorf("linked to %q", m.TermUseID)
	}
}

func TestResolveFollowsTheCitationGraph(t *testing.T) {
	ix := NewIndex(twoSenses())
	c := Corpus{Cites: map[string]map[string]bool{
		"vn:decision:2022:15-2022-qd-ubnd": {"vn:law:2019:45-2019-qh14": true},
	}}
	m := Mention{DocID: "vn:decision:2022:15-2022-qd-ubnd", ProvisionID: "p", Surface: "người lao động"}
	ix.Resolve(&m, "vn:decision:2022:15-2022-qd-ubnd", c, "")
	if m.Method != MethodScored {
		t.Fatalf("method %q, want %q, candidates %v", m.Method, MethodScored, m.Candidates)
	}
	if m.TermUseID != "vn:term:vn:law:2019:45-2019-qh14:nguoi-lao-dong" {
		t.Errorf("the citation graph was not followed, linked to %q", m.TermUseID)
	}
	if len(m.Candidates) != 2 {
		t.Errorf("the runner up was dropped, and it is what a reviewer needs: %v", m.Candidates)
	}
}

func TestResolveLeavesACloseCallUnresolved(t *testing.T) {
	// Both definers are cited, so the signals do not settle it. This is the
	// pile that goes to the model, and keeping it small is what scoring is for.
	ix := NewIndex(twoSenses())
	c := Corpus{Cites: map[string]map[string]bool{
		"vn:decision:2022:15-2022-qd-ubnd": {
			"vn:law:2019:45-2019-qh14": true,
			"vn:law:2014:58-2014-qh13": true,
		},
	}}
	m := Mention{DocID: "vn:decision:2022:15-2022-qd-ubnd", ProvisionID: "p", Surface: "người lao động"}
	ix.Resolve(&m, "vn:decision:2022:15-2022-qd-ubnd", c, "")
	if m.Method != MethodUnresolved || m.TermUseID != "" {
		t.Fatalf("a close call was decided anyway: %s %s", m.Method, m.TermUseID)
	}
	if !NeedsAdjudication(&m) {
		t.Error("the close call was not offered for adjudication")
	}
}

func TestResolveLeavesAMentionWithNoSignalUnresolved(t *testing.T) {
	// An unresolved mention is correct output. A confidently wrong link is a
	// defect.
	ix := NewIndex(twoSenses())
	m := Mention{DocID: "vn:decision:2022:15-2022-qd-ubnd", ProvisionID: "p", Surface: "người lao động"}
	ix.Resolve(&m, "vn:decision:2022:15-2022-qd-ubnd", Corpus{}, "")
	if m.TermUseID != "" {
		t.Errorf("a mention with no evidence was linked to %q", m.TermUseID)
	}
	if m.Method != MethodUnresolved {
		t.Errorf("method %q", m.Method)
	}
}

func TestResolveRulesOutADefinitionThatWasNotYetInForce(t *testing.T) {
	ix := NewIndex(twoSenses())
	c := Corpus{
		Cites: map[string]map[string]bool{
			"vn:decision:2016:15-2016-qd-ubnd": {
				"vn:law:2019:45-2019-qh14": true,
				"vn:law:2014:58-2014-qh13": true,
			},
		},
		EffectiveFrom: map[string]string{
			"vn:law:2019:45-2019-qh14": "2021-01-01",
			"vn:law:2014:58-2014-qh13": "2016-01-01",
		},
	}
	m := Mention{DocID: "vn:decision:2016:15-2016-qd-ubnd", ProvisionID: "p", Surface: "người lao động"}
	ix.Resolve(&m, "vn:decision:2016:15-2016-qd-ubnd", c, "2016-06-01")
	if len(m.Candidates) != 1 {
		t.Fatalf("a definition that did not exist yet is still a candidate: %v", m.Candidates)
	}
	if m.TermUseID != "vn:term:vn:law:2014:58-2014-qh13:nguoi-lao-dong" {
		t.Errorf("linked to %q", m.TermUseID)
	}
}

func TestResolveSaysWhichSignalsFired(t *testing.T) {
	ix := NewIndex(twoSenses())
	c := Corpus{
		Cites:      map[string]map[string]bool{"d": {"vn:law:2019:45-2019-qh14": true}},
		Subdomains: map[string][]string{"d": {"lao-dong"}, "vn:law:2019:45-2019-qh14": {"lao-dong"}},
	}
	m := Mention{DocID: "d", ProvisionID: "p", Surface: "người lao động"}
	ix.Resolve(&m, "d", c, "")
	signals := strings.Join(m.Candidates[0].Signals, ",")
	for _, want := range []string{SignalCited, SignalSubject} {
		if !strings.Contains(signals, want) {
			t.Errorf("signal %q did not fire: %s", want, signals)
		}
	}
}

func adjudication(t *testing.T, w wireAdjudication) string {
	t.Helper()
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

func closeCall() *Mention {
	return &Mention{
		DocID: "d", ProvisionID: "p", Surface: "người lao động", Method: MethodUnresolved,
		Candidates: []MentionCandidate{
			{TermUseID: "vn:term:vn:law:2019:45-2019-qh14:nguoi-lao-dong", Score: 0.5},
			{TermUseID: "vn:term:vn:law:2014:58-2014-qh13:nguoi-lao-dong", Score: 0.5},
		},
	}
}

func TestAdjudicateDecidesACloseCall(t *testing.T) {
	ix := NewIndex(twoSenses())
	s := &scripted{replies: []string{adjudication(t, wireAdjudication{
		TermUseID:  "vn:term:vn:law:2014:58-2014-qh13:nguoi-lao-dong",
		Rationale:  "điều khoản nói về đóng bảo hiểm xã hội",
		Confidence: 0.8,
	})}}
	a := &Adjudicator{Completer: s, Model: "test"}

	m := closeCall()
	if _, err := a.Adjudicate(context.Background(), m, "mức đóng bảo hiểm xã hội của người lao động", ix); err != nil {
		t.Fatalf("adjudicate: %v", err)
	}
	if m.Method != MethodAdjudicated {
		t.Fatalf("method %q", m.Method)
	}
	if m.TermUseID != "vn:term:vn:law:2014:58-2014-qh13:nguoi-lao-dong" {
		t.Errorf("linked to %q", m.TermUseID)
	}
}

func TestAdjudicateHonoursNeither(t *testing.T) {
	// A provision using the phrase in a third sense is a real answer, and
	// forcing a pick would put a wrong edge in the graph.
	ix := NewIndex(twoSenses())
	s := &scripted{replies: []string{adjudication(t, wireAdjudication{Neither: true, Rationale: "nghĩa khác hẳn"})}}
	a := &Adjudicator{Completer: s, Model: "test"}

	m := closeCall()
	if _, err := a.Adjudicate(context.Background(), m, "văn bản dùng cụm từ theo nghĩa khác", ix); err != nil {
		t.Fatalf("adjudicate: %v", err)
	}
	if m.Method != MethodUnresolved || m.TermUseID != "" {
		t.Errorf("neither was overridden: %s %s", m.Method, m.TermUseID)
	}
	if s.calls != 1 {
		t.Errorf("neither was corrected, %d calls", s.calls)
	}
}

func TestAdjudicateRefusesATargetItNeverOffered(t *testing.T) {
	ix := NewIndex(twoSenses())
	s := &scripted{replies: []string{
		adjudication(t, wireAdjudication{TermUseID: "vn:term:vn:law:2015:invented:nguoi-lao-dong"}),
		adjudication(t, wireAdjudication{TermUseID: "vn:term:vn:law:2019:45-2019-qh14:nguoi-lao-dong", Confidence: 0.7}),
	}}
	a := &Adjudicator{Completer: s, Model: "test", MaxCorrections: 2}

	m := closeCall()
	if _, err := a.Adjudicate(context.Background(), m, "văn bản", ix); err != nil {
		t.Fatalf("adjudicate: %v", err)
	}
	if s.calls != 2 {
		t.Fatalf("an invented target was accepted, %d calls", s.calls)
	}
	if m.TermUseID != "vn:term:vn:law:2019:45-2019-qh14:nguoi-lao-dong" {
		t.Errorf("linked to %q", m.TermUseID)
	}
}

func TestAdjudicateGivesUpAsUnresolved(t *testing.T) {
	ix := NewIndex(twoSenses())
	s := &scripted{replies: []string{"không phải JSON"}}
	a := &Adjudicator{Completer: s, Model: "test", MaxCorrections: 1}

	m := closeCall()
	if _, err := a.Adjudicate(context.Background(), m, "văn bản", ix); err != nil {
		t.Fatalf("adjudicate: %v", err)
	}
	if m.Method != MethodUnresolved || m.TermUseID != "" {
		t.Errorf("a failed adjudication produced a link: %s %s", m.Method, m.TermUseID)
	}
}

func TestAdjudicationPromptOffersBothDefinitions(t *testing.T) {
	ix := NewIndex(twoSenses())
	prompt := AdjudicationPrompt(closeCall(), "mức đóng bảo hiểm xã hội", ix)
	for _, want := range []string{
		"người lao động",
		"người làm việc cho người sử dụng lao động theo thỏa thuận",
		"người tham gia bảo hiểm xã hội bắt buộc",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
}

func TestSummarizeCountsUnresolvedAsWork(t *testing.T) {
	r := &MentionReport{Mentions: []Mention{
		{TermUseID: "a"}, {TermUseID: ""}, {TermUseID: "b"}, {TermUseID: ""},
	}}
	Summarize(r)
	if r.Resolved != 2 || r.Unresolved != 2 {
		t.Errorf("resolved %d unresolved %d, want 2 and 2", r.Resolved, r.Unresolved)
	}
}
