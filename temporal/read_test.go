package temporal

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/api"
)

// answers is a Completer that reads from a script, so a reading test is about
// the reading rather than about a network.
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

const instruction = `1. Sửa đổi, bổ sung khoản 2 Điều 15 như sau: "2. Tự nguyện, bình đẳng, thiện chí."` + "\n" +
	`2. Bổ sung điểm d vào sau điểm c khoản 1 Điều 20.`

// quoteAt returns a quote and its real byte offsets, so a test never hand counts
// bytes in a Vietnamese sentence and gets them wrong.
func quoteAt(t *testing.T, text, quote string) (int, int) {
	t.Helper()
	i := strings.Index(text, quote)
	if i < 0 {
		t.Fatalf("the test quote is not in the test text: %q", quote)
	}
	return i, i + len(quote)
}

func reply(t *testing.T, body string) string {
	t.Helper()
	return `{"operations":[` + body + `]}`
}

func amend(t *testing.T) string {
	t.Helper()
	start, end := quoteAt(t, instruction, "Sửa đổi, bổ sung khoản 2 Điều 15")
	return `{"operation":"amend","target_doc":"45/2019/QH14","target_component":"khoản 2 Điều 15",` +
		`"scope":"clause","new_text":"2. Tự nguyện, bình đẳng, thiện chí.",` +
		`"instruction_quote":"Sửa đổi, bổ sung khoản 2 Điều 15","char_start":` + strconv.Itoa(start) +
		`,"char_end":` + strconv.Itoa(end) + `,"confidence":0.9}`
}

func newReader(replies ...string) (*Reader, *answers) {
	m := &answers{replies: replies}
	return &Reader{Completer: m, Model: "test-model", MaxCorrections: 2}, m
}

func TestReadOneAmendment(t *testing.T) {
	r, m := newReader(reply(t, amend(t)))
	ops, usage, err := r.Read(context.Background(), amendDoc, amendDoc+":article-1", instruction, "2022-06-20")
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("read %d operations", len(ops))
	}
	got := ops[0]
	if got.Kind != KindAmend {
		t.Errorf("kind is %q", got.Kind)
	}
	if got.TargetRef != "khoản 2 Điều 15" {
		t.Errorf("the target reference came back as %q, it must stay as the drafter wrote it", got.TargetRef)
	}
	if got.TargetComponent != "" {
		t.Errorf("the reading resolved a component identifier, which is code's job: %q", got.TargetComponent)
	}
	if got.TargetNumber != "45/2019/QH14" {
		t.Errorf("the stated number is %q", got.TargetNumber)
	}
	if got.InstrumentFrom != "2022-06-20" {
		t.Errorf("the commencement of the amending instrument is %q", got.InstrumentFrom)
	}
	if got.EffectiveFrom != "" {
		t.Errorf("the instruction stated no effective date and one was invented: %q", got.EffectiveFrom)
	}
	// The amendment states no date of its own, so it takes effect when its
	// instrument does, which the instrument states.
	if got.Date() != "2022-06-20" {
		t.Errorf("the operation takes effect on %q", got.Date())
	}
	if got.Model != "test-model" {
		t.Errorf("the model is not recorded: %q", got.Model)
	}
	if usage.TotalTokens == 0 || m.calls != 1 {
		t.Errorf("%d calls, %d tokens", m.calls, usage.TotalTokens)
	}
}

func TestReadAnInsertionKeepsTheAnchor(t *testing.T) {
	start, end := quoteAt(t, instruction, "Bổ sung điểm d vào sau điểm c khoản 1 Điều 20")
	r, _ := newReader(reply(t, `{"operation":"supplement","target_component":"điểm d khoản 1 Điều 20",`+
		`"scope":"point","anchor":{"position":"after","sibling":"điểm c"},"new_text":"d) ...",`+
		`"instruction_quote":"Bổ sung điểm d vào sau điểm c khoản 1 Điều 20","char_start":`+strconv.Itoa(start)+
		`,"char_end":`+strconv.Itoa(end)+`,"confidence":0.8}`))

	ops, _, err := r.Read(context.Background(), amendDoc, amendDoc+":article-1", instruction, "")
	if err != nil {
		t.Fatal(err)
	}
	if ops[0].Anchor == nil {
		t.Fatal("an insertion with no anchor goes to the end of its parent, which is usually wrong and always unverifiable")
	}
	if ops[0].Anchor.Position != "after" || ops[0].Anchor.Sibling != "điểm c" {
		t.Errorf("the anchor is %+v", ops[0].Anchor)
	}
}

func TestReadRejectsAnAnchorPositionThatIsNeither(t *testing.T) {
	start, end := quoteAt(t, instruction, "Bổ sung điểm d")
	bad := reply(t, `{"operation":"supplement","target_component":"điểm d khoản 1 Điều 20","scope":"point",`+
		`"anchor":{"position":"beside","sibling":"điểm c"},"instruction_quote":"Bổ sung điểm d",`+
		`"char_start":`+strconv.Itoa(start)+`,"char_end":`+strconv.Itoa(end)+`,"confidence":0.8}`)
	r, m := newReader(bad)
	if _, _, err := r.Read(context.Background(), amendDoc, amendDoc+":article-1", instruction, ""); err == nil {
		t.Fatal("an anchor position outside after and before was accepted")
	}
	if m.calls != 3 {
		t.Errorf("the reader made %d calls, want the first plus two corrections", m.calls)
	}
	if !strings.Contains(m.inputs[1], "after hoặc before") {
		t.Errorf("the correction does not say what was wrong:\n%s", m.inputs[1])
	}
}

func TestReadRejectsAPhraseEditWithNoTargets(t *testing.T) {
	start, end := quoteAt(t, instruction, "Sửa đổi")
	bad := reply(t, `{"operation":"amend","target_component":"","scope":"phrase",`+
		`"phrase_edit":{"find":"cơ quan quản lý nhà nước","replace":"cơ quan nhà nước có thẩm quyền","targets":[]},`+
		`"instruction_quote":"Sửa đổi","char_start":`+strconv.Itoa(start)+`,"char_end":`+strconv.Itoa(end)+
		`,"confidence":0.9}`)
	r, m := newReader(bad)
	if _, _, err := r.Read(context.Background(), amendDoc, amendDoc+":article-1", instruction, ""); err == nil {
		t.Fatal("a phrase edit with an empty target list was accepted, and an empty list is not a wildcard")
	}
	if !strings.Contains(strings.Join(m.inputs, "\n"), "targets") {
		t.Error("the correction does not mention the target list")
	}
}

func TestReadRejectsAQuoteThatIsNotThere(t *testing.T) {
	bad := reply(t, `{"operation":"amend","target_component":"khoản 2 Điều 15","scope":"clause",`+
		`"instruction_quote":"một câu không có trong điều khoản","char_start":0,"char_end":10,"confidence":0.9}`)
	r, m := newReader(bad)
	if _, _, err := r.Read(context.Background(), amendDoc, amendDoc+":article-1", instruction, ""); err == nil {
		t.Fatal("a quote that is not in the provision was accepted")
	}
	if !strings.Contains(strings.Join(m.inputs, "\n"), "nguyên văn") {
		t.Error("the correction does not ask for a verbatim quote")
	}
}

func TestReadCorrectsOffsetsWhenTheQuoteItselfIsRight(t *testing.T) {
	// This is the failure that cost the first real decree all three of its
	// amendments: a verbatim quote reported one byte early, because the
	// provision text opens with a newline the model did not count.
	off := reply(t, `{"operation":"amend","target_doc":"45/2019/QH14","target_component":"khoản 2 Điều 15",`+
		`"scope":"clause","new_text":"2. Tự nguyện, bình đẳng, thiện chí.",`+
		`"instruction_quote":"Sửa đổi, bổ sung khoản 2 Điều 15","char_start":0,"char_end":41,"confidence":0.9}`)
	r, m := newReader(off)
	ops, _, err := r.Read(context.Background(), amendDoc, amendDoc+":article-1", instruction, "")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if m.calls != 1 {
		t.Errorf("the model was called %d times, a repairable offset should cost no correction", m.calls)
	}
	start, end := quoteAt(t, instruction, "Sửa đổi, bổ sung khoản 2 Điều 15")
	if len(ops) != 1 || ops[0].CharStart != start || ops[0].CharEnd != end {
		t.Errorf("offsets are %d to %d, want the real %d to %d", ops[0].CharStart, ops[0].CharEnd, start, end)
	}
}

func TestLocateQuote(t *testing.T) {
	text := "một hai ba hai"
	start, end := quoteAt(t, text, "ba")
	cases := []struct {
		name             string
		quote            string
		start, end       int
		wantStart, wantE int
		wantErr          bool
	}{
		{"đúng vị trí", "ba", start, end, start, end, false},
		{"lệch một byte", "ba", start - 1, end - 1, start, end, false},
		{"ngoài đoạn", "ba", 900, 902, start, end, false},
		{"rỗng", "", 0, 0, 0, 0, true},
		{"không có", "bốn", 0, 3, 0, 0, true},
		{"nhiều lần", "hai", 0, 3, 0, 0, true},
	}
	for _, c := range cases {
		gotStart, gotEnd, err := locateQuote(text, c.quote, c.start, c.end)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: error is %v, want an error %v", c.name, err, c.wantErr)
			continue
		}
		if err == nil && (gotStart != c.wantStart || gotEnd != c.wantE) {
			t.Errorf("%s: offsets are %d to %d, want %d to %d", c.name, gotStart, gotEnd, c.wantStart, c.wantE)
		}
	}
}

func TestReadRejectsAKindOutsideTheTen(t *testing.T) {
	start, end := quoteAt(t, instruction, "Sửa đổi")
	bad := reply(t, `{"operation":"modify","target_component":"khoản 2 Điều 15","scope":"clause",`+
		`"instruction_quote":"Sửa đổi","char_start":`+strconv.Itoa(start)+`,"char_end":`+strconv.Itoa(end)+
		`,"confidence":0.9}`)
	r, _ := newReader(bad)
	_, _, err := r.Read(context.Background(), amendDoc, amendDoc+":article-1", instruction, "")
	if err == nil {
		t.Fatal("a kind outside the ten was accepted")
	}
	// A failed provision that does not say what was wrong is a provision nobody
	// can fix, and the failures are the queue this pass leaves behind.
	if !strings.Contains(err.Error(), "modify") {
		t.Errorf("the error does not say what the model got wrong: %v", err)
	}
}

func TestReadRejectsADateThatIsNotADate(t *testing.T) {
	start, end := quoteAt(t, instruction, "Sửa đổi")
	bad := reply(t, `{"operation":"amend","target_component":"khoản 2 Điều 15","scope":"clause",`+
		`"effective_from":"ngày 1 tháng 1 năm 2025","instruction_quote":"Sửa đổi","char_start":`+
		strconv.Itoa(start)+`,"char_end":`+strconv.Itoa(end)+`,"confidence":0.9}`)
	r, _ := newReader(bad)
	if _, _, err := r.Read(context.Background(), amendDoc, amendDoc+":article-1", instruction, ""); err == nil {
		t.Fatal("a date the graph cannot compare was accepted")
	}
}

func TestReadRecoversAfterOneCorrection(t *testing.T) {
	r, m := newReader("khong phai JSON", reply(t, amend(t)))
	ops, _, err := r.Read(context.Background(), amendDoc, amendDoc+":article-1", instruction, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("read %d operations", len(ops))
	}
	if m.calls != 2 {
		t.Errorf("%d calls, want one correction", m.calls)
	}
}

func TestReadAcceptsAnEmptyAnswer(t *testing.T) {
	// A provision that amends nothing is the common case, and an empty list is
	// the right answer rather than a failure to correct.
	r, m := newReader(`{"operations":[]}`)
	ops, _, err := r.Read(context.Background(), amendDoc, amendDoc+":article-1", instruction, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 0 || m.calls != 1 {
		t.Errorf("read %d operations in %d calls", len(ops), m.calls)
	}
}

func TestReadStripsCodeFences(t *testing.T) {
	r, _ := newReader("```json\n" + reply(t, amend(t)) + "\n```")
	ops, _, err := r.Read(context.Background(), amendDoc, amendDoc+":article-1", instruction, "")
	if err != nil || len(ops) != 1 {
		t.Fatalf("read %d operations, %v", len(ops), err)
	}
}

func TestReadPassesTheErrorUp(t *testing.T) {
	m := &answers{err: errors.New("the service said no")}
	r := &Reader{Completer: m, Model: "test-model", MaxCorrections: 2}
	if _, _, err := r.Read(context.Background(), amendDoc, amendDoc+":article-1", instruction, ""); err == nil {
		t.Fatal("a transport failure was swallowed")
	}
	if m.calls != 1 {
		t.Errorf("a failing service was called %d times", m.calls)
	}
}

func TestOperationIDsDoNotCollideWithinAProvision(t *testing.T) {
	start, end := quoteAt(t, instruction, "Sửa đổi")
	one := `{"operation":"amend","target_component":"khoản 2 Điều 15","scope":"clause","new_text":"a",` +
		`"instruction_quote":"Sửa đổi","char_start":` + strconv.Itoa(start) + `,"char_end":` + strconv.Itoa(end) + `,"confidence":0.9}`
	r, _ := newReader(reply(t, one+","+one))
	ops, _, err := r.Read(context.Background(), amendDoc, amendDoc+":article-1", instruction, "")
	if err != nil {
		t.Fatal(err)
	}
	if ops[0].ID == ops[1].ID {
		t.Errorf("two instructions in one provision share the identifier %s", ops[0].ID)
	}
}

func TestInstructionsNameEveryKind(t *testing.T) {
	r := &Reader{}
	got := r.Instructions()
	for _, k := range Kinds {
		if !strings.Contains(got, k) {
			t.Errorf("the prompt does not offer the kind %q", k)
		}
	}
	if !strings.Contains(got, "Không được đoán") {
		t.Error("the prompt does not tell the model never to guess a date")
	}
}

func TestPromptCarriesTheProvision(t *testing.T) {
	got := Prompt("vn:law:2022:10-2022-qh15:article-1", instruction)
	if !strings.Contains(got, instruction) || !strings.Contains(got, "article-1") {
		t.Errorf("the prompt is missing the provision or its text:\n%s", got)
	}
}
