package distill

import (
	"bytes"
	"strings"
	"testing"
)

// The teacher's output on a handful of provisions, written out by hand. It is
// small enough to reason about and large enough that the student has to
// generalise rather than memorise one sentence.
func teacherOutput() []Example {
	texts := []struct {
		id    string
		text  string
		spans []string
		kinds []string
	}{
		{
			"vn:law:2019:45-2019-qh14:article-105:clause-1",
			"Thời giờ làm việc bình thường không quá 08 giờ trong 01 ngày.",
			[]string{"Thời giờ làm việc bình thường"},
			[]string{"amount"},
		},
		{
			"vn:law:2019:45-2019-qh14:article-106:clause-1",
			"Người sử dụng lao động phải bố trí thời giờ làm việc bình thường theo tuần.",
			[]string{"Người sử dụng lao động", "thời giờ làm việc bình thường"},
			[]string{"actor", "amount"},
		},
		{
			"vn:decree:2020:145-2020-nd-cp:article-58:clause-1",
			"Người sử dụng lao động phải thông báo thời giờ làm việc bình thường cho người lao động.",
			[]string{"Người sử dụng lao động", "thời giờ làm việc bình thường", "người lao động"},
			[]string{"actor", "amount", "actor"},
		},
		{
			"vn:decree:2020:145-2020-nd-cp:article-59:clause-2",
			"Người lao động được nghỉ hằng năm theo quy định.",
			[]string{"Người lao động"},
			[]string{"actor"},
		},
	}
	var out []Example
	for _, r := range texts {
		e := Example{ProvisionID: r.id, Text: r.text, Source: SourceTeacher}
		for i, s := range r.spans {
			at := strings.Index(r.text, s)
			e.Spans = append(e.Spans, Span{Text: s, Start: at, End: at + len(s), Kind: r.kinds[i]})
		}
		out = append(out, e)
	}
	return out
}

func TestTokenizeDoesNotLetAPhraseCrossPunctuation(t *testing.T) {
	// Letting candidates cross a comma multiplies the candidate count by ten
	// and adds nothing but noise.
	groups := tokenize("Người lao động, người sử dụng lao động.")
	if len(groups) != 2 {
		t.Fatalf("want two groups, got %d: %v", len(groups), groups)
	}
	if groups[0][len(groups[0])-1].text != "động" {
		t.Errorf("the comma did not break the group: %v", groups[0])
	}
}

func TestTokenOffsetsPointBackAtTheText(t *testing.T) {
	text := "Người sử dụng lao động phải trả lương."
	for _, group := range tokenize(text) {
		for _, tok := range group {
			if text[tok.start:tok.end] != tok.text {
				t.Errorf("token %q does not sit at %d:%d, which holds %q", tok.text, tok.start, tok.end, text[tok.start:tok.end])
			}
		}
	}
}

func TestCandidatesAreDeterministicAndReachTheLongTerms(t *testing.T) {
	// Recall lost here can never be recovered by the classifier, and a
	// candidate set that changed between runs makes every measurement
	// unrepeatable.
	text := "Giấy chứng nhận quyền sử dụng đất được cấp cho người sử dụng đất."
	a, b := Candidates(text), Candidates(text)
	if len(a) != len(b) {
		t.Fatalf("two runs produced %d and %d candidates", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("candidate %d differs between runs: %v and %v", i, a[i], b[i])
		}
	}
	found := false
	for _, c := range a {
		if c.Text == "Giấy chứng nhận quyền sử dụng đất" {
			found = true
		}
	}
	if !found {
		t.Error("a seven token term was not among the candidates, and a limit tuned for English would miss exactly these")
	}
}

func TestTrainLearnsWhatTheTeacherLabelled(t *testing.T) {
	examples := teacherOutput()
	tagger := Train(examples, 8)
	tagged := tagger.Tag(examples[0].Text)
	if len(tagged) == 0 {
		t.Fatal("the student tagged nothing it was trained on")
	}
	want := "Thời giờ làm việc bình thường"
	for _, s := range tagged {
		if s.Text == want {
			return
		}
	}
	t.Errorf("the student missed %q, tagged %v", want, tagged)
}

func TestTrainIsReproducibleAcrossOrderings(t *testing.T) {
	// Two trainings over the same teacher output have to produce the same model
	// file, on Linux, macOS and Windows alike. The order of examples decides
	// the weights, so Train fixes it rather than trusting the caller.
	examples := teacherOutput()
	shuffled := []Example{examples[3], examples[1], examples[0], examples[2]}

	var a, b bytes.Buffer
	if err := Train(examples, 5).Write(&a); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := Train(shuffled, 5).Write(&b); err != nil {
		t.Fatalf("write: %v", err)
	}
	if a.String() != b.String() {
		t.Error("the model depends on the order the examples arrived in")
	}
}

func TestTaggerRemembersKindsRatherThanGuessingThem(t *testing.T) {
	// A kind is a judgement about a concept and not about a span, so guessing
	// one for a phrase the teacher never saw would be inventing rather than
	// tagging.
	tagger := Train(teacherOutput(), 8)
	if tagger.Kinds["thoi-gio-lam-viec-binh-thuong"] != "amount" {
		t.Errorf("kind not remembered: %v", tagger.Kinds)
	}
	if _, ok := tagger.Kinds["giay-chung-nhan"]; ok {
		t.Error("a kind was invented for a phrase the teacher never saw")
	}
}

func TestTagDoesNotReturnEveryPrefixOfOnePhrase(t *testing.T) {
	tagger := Train(teacherOutput(), 8)
	tagged := tagger.Tag(teacherOutput()[2].Text)
	for i := 1; i < len(tagged); i++ {
		if tagged[i].Start < tagged[i-1].End {
			t.Errorf("spans overlap: %v and %v", tagged[i-1], tagged[i])
		}
	}
}

func TestTaggerSurvivesTheRoundTrip(t *testing.T) {
	tagger := Train(teacherOutput(), 5)
	tagger.TeacherHash = Fingerprint(teacherOutput())

	var buf bytes.Buffer
	if err := tagger.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	back, err := Read(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(back.Weights) != len(tagger.Weights) {
		t.Errorf("weights came back as %d of %d", len(back.Weights), len(tagger.Weights))
	}
	if back.TeacherHash != tagger.TeacherHash {
		t.Errorf("teacher hash %q, want %q", back.TeacherHash, tagger.TeacherHash)
	}
	text := teacherOutput()[0].Text
	if len(back.Tag(text)) != len(tagger.Tag(text)) {
		t.Error("the loaded model tags differently from the trained one")
	}
}

func TestFingerprintChangesWithTheTrainingSet(t *testing.T) {
	examples := teacherOutput()
	before := Fingerprint(examples)
	if before != Fingerprint(examples) {
		t.Fatal("the fingerprint is not stable")
	}
	examples[0].Spans[0].End--
	if Fingerprint(examples) == before {
		t.Error("a changed span did not change the fingerprint")
	}
}

func TestFingerprintIgnoresTheOrderExamplesArriveIn(t *testing.T) {
	examples := teacherOutput()
	shuffled := []Example{examples[2], examples[0], examples[3], examples[1]}
	if Fingerprint(examples) != Fingerprint(shuffled) {
		t.Error("the fingerprint depends on the order")
	}
}

func TestFeaturesMarkTheGazetteer(t *testing.T) {
	// The single strongest feature, and the one that carries most of what the
	// teacher knew.
	text := "Người sử dụng lao động phải trả lương."
	group := tokenize(text)[0]
	gaz := map[string]bool{"nguoi-su-dung-lao-dong": true}
	withGaz := strings.Join(features(text, group, 0, 5, gaz), " ")
	without := strings.Join(features(text, group, 0, 5, nil), " ")
	if !strings.Contains(withGaz, "in_gazetteer") {
		t.Errorf("a known phrase did not get the feature: %s", withGaz)
	}
	if strings.Contains(without, "in_gazetteer") {
		t.Errorf("an unknown phrase got the feature: %s", without)
	}
}

func TestFeaturesMarkANumberedPhrase(t *testing.T) {
	text := "không quá 08 giờ trong 01 ngày"
	group := tokenize(text)[0]
	f := strings.Join(features(text, group, 2, 1, nil), " ")
	if !strings.Contains(f, "has_number") {
		t.Errorf("a phrase that is a number did not get the feature: %s", f)
	}
}
