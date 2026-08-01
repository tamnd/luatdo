package concept

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const (
	usageA = "Người sử dụng lao động phải trả lương làm thêm giờ cho thời gian làm việc vượt quá thời giờ làm việc bình thường."
	usageB = "Thời giờ làm việc bình thường không quá 48 giờ trong 01 tuần."
)

func usageTexts() (texts, hashes map[string]string) {
	return map[string]string{
			"vn:law:2019:45-2019-qh14:article-98:clause-1":  usageA,
			"vn:law:2019:45-2019-qh14:article-105:clause-2": usageB,
		}, map[string]string{
			"vn:law:2019:45-2019-qh14:article-98:clause-1":  "aaa",
			"vn:law:2019:45-2019-qh14:article-105:clause-2": "bbb",
		}
}

func promoted() *TermUse {
	return &TermUse{
		ID:      TermUseID(UndefinedScope, "thời giờ làm việc bình thường"),
		LabelVI: "thời giờ làm việc bình thường",
		ScopeID: UndefinedScope,
		Origin:  OriginUndefinedUsage,
	}
}

func usageAgg() *Aggregation {
	return &Aggregation{
		Key:     "thoi-gio-lam-viec-binh-thuong",
		LabelVI: "thời giờ làm việc bình thường",
		Provisions: []string{
			"vn:law:2019:45-2019-qh14:article-98:clause-1",
			"vn:law:2019:45-2019-qh14:article-105:clause-2",
		},
	}
}

func working(t *testing.T, w wireWorking) string {
	t.Helper()
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

func goodWorking() wireWorking {
	return wireWorking{
		Text: "cách dùng trong các điều khoản cho thấy đây là mức thời gian làm việc chuẩn, phần vượt quá được trả lương làm thêm giờ",
		Claims: []wireClaim{
			{
				Text:        "phần vượt quá được trả lương làm thêm giờ",
				ProvisionID: "vn:law:2019:45-2019-qh14:article-98:clause-1",
				Quote:       "trả lương làm thêm giờ cho thời gian làm việc vượt quá",
			},
			{
				Text:        "mức trần theo tuần là 48 giờ",
				ProvisionID: "vn:law:2019:45-2019-qh14:article-105:clause-2",
				Quote:       "không quá 48 giờ trong 01 tuần",
			},
		},
	}
}

func TestDefineWritesAWorkingDefinitionWithCheckedQuotes(t *testing.T) {
	texts, hashes := usageTexts()
	s := &scripted{replies: []string{working(t, goodWorking())}}
	d := &Definer{Completer: s, Model: "test"}

	w, usage, err := d.Define(context.Background(), promoted(), usageAgg(), texts, hashes)
	if err != nil {
		t.Fatalf("define: %v", err)
	}
	if w == nil {
		t.Fatal("no working definition written")
	}
	if len(w.Claims) != 2 {
		t.Fatalf("want two claims, got %d", len(w.Claims))
	}
	if len(w.Sources) != 2 || w.Sources[0].TextHash != "aaa" {
		t.Errorf("sources are missing their hashes: %v", w.Sources)
	}
	if usage.TotalTokens == 0 {
		t.Error("usage not returned")
	}
}

func TestDefineRefusesATermThatHasADefinition(t *testing.T) {
	// This is the fence, enforced at the only place that can create one of
	// these. A concept with a statutory definition does not get a second
	// definition written by us sitting next to it.
	texts, hashes := usageTexts()
	s := &scripted{replies: []string{working(t, goodWorking())}}
	d := &Definer{Completer: s, Model: "test"}

	term := promoted()
	term.Origin = OriginDefined
	w, _, err := d.Define(context.Background(), term, usageAgg(), texts, hashes)
	if err == nil {
		t.Fatal("a defined term was given a working definition")
	}
	if w != nil {
		t.Error("a working definition came back with the error")
	}
	if s.calls != 0 {
		t.Errorf("the model was called before the fence was checked, %d calls", s.calls)
	}
}

func TestDefineRejectsAClaimWithNoSupportingQuote(t *testing.T) {
	// Without this the working definition is a summary, and a summary about
	// legal text is a recollection with a citation stapled to it.
	texts, hashes := usageTexts()
	bad := goodWorking()
	bad.Claims[0].Quote = "được trả lương gấp ba lần"
	s := &scripted{replies: []string{working(t, bad), working(t, goodWorking())}}
	d := &Definer{Completer: s, Model: "test", MaxCorrections: 2}

	w, _, err := d.Define(context.Background(), promoted(), usageAgg(), texts, hashes)
	if err != nil {
		t.Fatalf("define: %v", err)
	}
	if s.calls != 2 {
		t.Fatalf("the unsupported claim was not corrected, %d calls", s.calls)
	}
	if !strings.Contains(s.inputs[1], "được trả lương gấp ba lần") {
		t.Errorf("the correction did not name the invented quote: %q", s.inputs[1])
	}
	if w == nil {
		t.Fatal("the corrected definition was not accepted")
	}
}

func TestDefineRejectsAClaimAgainstAProvisionItWasNotGiven(t *testing.T) {
	texts, hashes := usageTexts()
	bad := goodWorking()
	bad.Claims[0].ProvisionID = "vn:law:2012:10-2012-qh13:article-104"
	s := &scripted{replies: []string{working(t, bad)}}
	d := &Definer{Completer: s, Model: "test", MaxCorrections: 0}

	w, _, err := d.Define(context.Background(), promoted(), usageAgg(), texts, hashes)
	if err != nil {
		t.Fatalf("define: %v", err)
	}
	if w != nil {
		t.Error("a claim citing a provision the model never saw was accepted")
	}
}

func TestDefineAcceptsThatTheProvisionsDoNotSettleIt(t *testing.T) {
	texts, hashes := usageTexts()
	s := &scripted{replies: []string{working(t, wireWorking{})}}
	d := &Definer{Completer: s, Model: "test"}

	w, _, err := d.Define(context.Background(), promoted(), usageAgg(), texts, hashes)
	if err != nil {
		t.Fatalf("an empty answer is valid and should not be an error: %v", err)
	}
	if w != nil {
		t.Error("an empty answer produced a definition")
	}
	if s.calls != 1 {
		t.Errorf("an empty answer was corrected, %d calls", s.calls)
	}
}

func TestDefineCapsTheProvisionsItReads(t *testing.T) {
	// Forty provisions of context makes this a summarisation task, where the
	// quotes stop being checkable by anyone reading the output.
	texts, hashes := usageTexts()
	agg := usageAgg()
	for i := range 20 {
		id := "vn:law:2019:45-2019-qh14:article-" + itoa(i)
		agg.Provisions = append(agg.Provisions, id)
		texts[id] = usageB
		hashes[id] = "ccc"
	}
	s := &scripted{replies: []string{working(t, goodWorking())}}
	d := &Definer{Completer: s, Model: "test", MaxProvisions: 3}

	w, _, err := d.Define(context.Background(), promoted(), agg, texts, hashes)
	if err != nil {
		t.Fatalf("define: %v", err)
	}
	if w == nil {
		t.Fatal("no definition")
	}
	if len(w.Sources) != 3 {
		t.Errorf("read %d provisions, want the cap of 3", len(w.Sources))
	}
}

func TestStaleWhenASourceProvisionChanged(t *testing.T) {
	w := &WorkingDefinition{Sources: []Source{
		{ProvisionID: "a", TextHash: "aaa"},
		{ProvisionID: "b", TextHash: "bbb"},
	}}
	if w.Stale(map[string]string{"a": "aaa", "b": "bbb"}) {
		t.Error("unchanged sources reported as stale")
	}
	if !w.Stale(map[string]string{"a": "aaa", "b": "zzz"}) {
		t.Error("an amended source was not noticed")
	}
	if w.Stale(map[string]string{"a": "aaa"}) {
		t.Error("a provision the caller did not ask about was treated as changed")
	}
}

func TestCheckWorkingIsTheBuildFailure(t *testing.T) {
	terms := []TermUse{
		{ID: "vn:term:vn:usage:a", LabelVI: "a", Origin: OriginUndefinedUsage},
		{ID: "vn:term:vn:law:2019:45-2019-qh14:b", LabelVI: "b", Origin: OriginDefined, DefinitionVI: "..."},
	}
	defs := []WorkingDefinition{
		{TermUseID: "vn:term:vn:usage:a", Claims: []Claim{{Text: "x"}}},
		{TermUseID: "vn:term:vn:usage:a", Claims: []Claim{{Text: "y"}}},
		{TermUseID: "vn:term:vn:law:2019:45-2019-qh14:b", Claims: []Claim{{Text: "z"}}},
		{TermUseID: "vn:term:vn:usage:missing", Claims: []Claim{{Text: "w"}}},
		{TermUseID: "vn:term:vn:usage:a"},
	}
	problems := CheckWorking(defs, terms)
	if len(problems) < 4 {
		t.Fatalf("want a problem for each of the four faults, got %d: %v", len(problems), problems)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"does not exist", "two working definitions", "no checkable claim", "origin"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no problem mentions %q:\n%s", want, joined)
		}
	}
}

func TestCheckWorkingPassesACleanLayer(t *testing.T) {
	terms := []TermUse{{ID: "vn:term:vn:usage:a", LabelVI: "a", Origin: OriginUndefinedUsage}}
	defs := []WorkingDefinition{{TermUseID: "vn:term:vn:usage:a", Claims: []Claim{{Text: "x"}}}}
	if problems := CheckWorking(defs, terms); len(problems) != 0 {
		t.Errorf("a clean layer reported problems: %v", problems)
	}
}
