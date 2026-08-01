package concept

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/law"
)

// The provision the discovery tests read. It is an ordinary operative clause
// rather than a definition, which is the whole point of pass C: nothing in it
// marks any phrase as a concept, so only reading tells them apart.
const provisionText = "Người sử dụng lao động phải bảo đảm thời giờ làm việc bình thường không quá 08 giờ trong 01 ngày."

func provision() *law.Provision {
	return &law.Provision{
		ID:       "vn:law:2019:45-2019-qh14:article-105:clause-1",
		Kind:     "clause",
		Text:     provisionText,
		TextHash: "8c1d4e2f",
	}
}

func document() *law.Document {
	return &law.Document{
		ID:      "vn:law:2019:45-2019-qh14",
		Title:   "Bộ luật Lao động",
		DocType: "Bộ luật",
	}
}

func discovery(t *testing.T, cs ...wireCandidate) string {
	t.Helper()
	data, err := json.Marshal(wireDiscovery{Concepts: cs})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

func goodCandidate() wireCandidate {
	quote := "thời giờ làm việc bình thường không quá 08 giờ"
	start := strings.Index(provisionText, quote)
	return wireCandidate{
		LabelVI:    "thời giờ làm việc bình thường",
		Kind:       KindAmount,
		Quote:      quote,
		CharStart:  start,
		CharEnd:    start + len(quote),
		Shows:      "điều khoản đặt mức trần cho khái niệm này",
		Confidence: 0.9,
	}
}

func TestDiscoverReadsCandidatesOutOfAnUnanchoredProvision(t *testing.T) {
	s := &scripted{replies: []string{discovery(t, goodCandidate())}}
	d := &Discoverer{Completer: s, Model: "test"}

	sight, err := d.Discover(context.Background(), document(), provision())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if sight.Err != "" {
		t.Fatalf("sighting carries an error: %s", sight.Err)
	}
	if len(sight.Candidates) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(sight.Candidates))
	}
	c := sight.Candidates[0]
	if c.ProvisionID != provision().ID || c.DocID != document().ID {
		t.Errorf("the caller fills in the identifiers, got %q and %q", c.ProvisionID, c.DocID)
	}
	if c.Model != "test" {
		t.Errorf("model not recorded on the candidate, got %q", c.Model)
	}
	if sight.TextHash != "8c1d4e2f" {
		t.Errorf("text hash not carried onto the sighting, got %q", sight.TextHash)
	}
	if sight.Usage.TotalTokens == 0 {
		t.Error("usage not accumulated")
	}
}

func TestDiscoverKeepsTheEmptyAnswer(t *testing.T) {
	// A provision that operates on no concept is a fact worth storing. Without
	// it the denominator of every discovery rate is the provisions that
	// happened to succeed.
	s := &scripted{replies: []string{discovery(t)}}
	d := &Discoverer{Completer: s, Model: "test"}

	sight, err := d.Discover(context.Background(), document(), provision())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if sight.Err != "" {
		t.Fatalf("an empty list is a valid answer, got error %q", sight.Err)
	}
	if len(sight.Candidates) != 0 {
		t.Fatalf("want no candidates, got %d", len(sight.Candidates))
	}
	if s.calls != 1 {
		t.Errorf("an empty answer was corrected, %d calls", s.calls)
	}
}

func TestDiscoverRejectsAQuoteThatIsNotInTheProvision(t *testing.T) {
	bad := goodCandidate()
	bad.Quote = "thời giờ làm việc không quá 10 giờ"
	s := &scripted{replies: []string{discovery(t, bad), discovery(t, goodCandidate())}}
	d := &Discoverer{Completer: s, Model: "test", MaxCorrections: 2}

	sight, err := d.Discover(context.Background(), document(), provision())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(sight.Attempts) != 2 {
		t.Fatalf("want two attempts, got %d", len(sight.Attempts))
	}
	if sight.Attempts[0].Error == "" {
		t.Error("the rejected attempt was not recorded with its reason")
	}
	if !strings.Contains(s.inputs[1], "thời giờ làm việc không quá 10 giờ") &&
		!strings.Contains(s.inputs[1], "không khớp") && !strings.Contains(s.inputs[1], "quote") {
		t.Errorf("the correction did not say what was wrong: %q", s.inputs[1])
	}
	if len(sight.Candidates) != 1 {
		t.Fatalf("the corrected reading was not accepted, got %d candidates", len(sight.Candidates))
	}
}

func TestDiscoverRejectsALabelTheProvisionDoesNotContain(t *testing.T) {
	// A normalised or translated label breaks the mention linking that comes
	// later, so it is rejected here where the text is still in hand.
	bad := goodCandidate()
	bad.LabelVI = "thời giờ làm việc tiêu chuẩn"
	s := &scripted{replies: []string{discovery(t, bad)}}
	d := &Discoverer{Completer: s, Model: "test", MaxCorrections: 1}

	sight, err := d.Discover(context.Background(), document(), provision())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if sight.Err == "" {
		t.Fatal("an invented label was accepted")
	}
	if len(sight.Candidates) != 0 {
		t.Errorf("candidates survived a failed reading: %d", len(sight.Candidates))
	}
}

func TestDiscoverRejectsAKindOutsideTheEnum(t *testing.T) {
	bad := goodCandidate()
	bad.Kind = "concept"
	s := &scripted{replies: []string{discovery(t, bad)}}
	d := &Discoverer{Completer: s, Model: "test", MaxCorrections: 0}

	sight, _ := d.Discover(context.Background(), document(), provision())
	if sight.Err == "" {
		t.Fatal("a kind outside the enum was accepted")
	}
}

func TestDiscoverCountsOneProvisionNamingOneConceptTwiceAsOneSighting(t *testing.T) {
	// Counting it twice would let a single provision push a candidate over a
	// promotion threshold on its own.
	again := goodCandidate()
	again.Shows = "nhắc lại"
	s := &scripted{replies: []string{discovery(t, goodCandidate(), again)}}
	d := &Discoverer{Completer: s, Model: "test"}

	sight, err := d.Discover(context.Background(), document(), provision())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(sight.Candidates) != 1 {
		t.Fatalf("want 1 candidate after the fold, got %d", len(sight.Candidates))
	}
}

func TestDiscoverOrdersCandidatesByPosition(t *testing.T) {
	first := goodCandidate()
	second := wireCandidate{
		LabelVI:    "Người sử dụng lao động",
		Kind:       KindActor,
		Quote:      "Người sử dụng lao động phải bảo đảm",
		CharStart:  0,
		CharEnd:    len("Người sử dụng lao động phải bảo đảm"),
		Shows:      "điều khoản đặt nghĩa vụ lên chủ thể này",
		Confidence: 0.95,
	}
	s := &scripted{replies: []string{discovery(t, first, second)}}
	d := &Discoverer{Completer: s, Model: "test"}

	sight, err := d.Discover(context.Background(), document(), provision())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(sight.Candidates) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(sight.Candidates))
	}
	if sight.Candidates[0].LabelVI != "Người sử dụng lao động" {
		t.Errorf("candidates are not in provision order, first is %q", sight.Candidates[0].LabelVI)
	}
}

func TestCandidateKeyFoldsTheSameWayTheRestOfTheProjectDoes(t *testing.T) {
	a := Candidate{LabelVI: "Thời giờ làm việc"}
	b := Candidate{LabelVI: "thời giờ  làm việc"}
	if a.Key() != b.Key() {
		t.Errorf("two spellings of one phrase fold apart: %q and %q", a.Key(), b.Key())
	}
	if a.Key() != law.Slug("Thời giờ làm việc") {
		t.Errorf("the key is not law.Slug, got %q", a.Key())
	}
}

func TestDiscoveryPromptCarriesTheProvisionAndNotTheAnswer(t *testing.T) {
	prompt := DiscoveryPrompt(document(), provision())
	for _, want := range []string{"Bộ luật Lao động", provision().ID, provisionText} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	if strings.Contains(prompt, KindAmount) {
		t.Error("the prompt names a kind, which is a hint at the answer")
	}
}

func TestDiscoveryInstructionsListEveryKind(t *testing.T) {
	instructions := (&Discoverer{}).Instructions()
	for _, k := range Kinds {
		if !strings.Contains(instructions, k) {
			t.Errorf("kind %q is not offered to the model", k)
		}
	}
}
