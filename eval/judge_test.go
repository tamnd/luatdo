package eval

import (
	"path/filepath"
	"testing"
)

func corpus(n int) ([]Item, []Key) {
	var items []Item
	var keys []Key
	for i := 0; i < n; i++ {
		id := string(rune('a'+i%26)) + string(rune('0'+i/26))
		verdict := LabelEntailed
		if i%10 == 0 { // one rejection in ten, which is the real shape
			verdict = LabelNotEntailed
		}
		items = append(items, Item{ID: id, ProvisionID: "p" + id, Text: "t", Statement: "s", Quote: "q"})
		keys = append(keys, Key{ID: id, Machine: verdict})
	}
	return items, keys
}

func TestSampleOversamplesTheRejections(t *testing.T) {
	items, keys := corpus(200)
	got, gotKeys := Sample(items, keys, 40, 50, "m12")
	if len(got) != 40 {
		t.Fatalf("drew %d", len(got))
	}
	rejected := 0
	for _, k := range gotKeys {
		if k.Stratum == "rejected" {
			rejected++
		}
	}
	if rejected != 20 {
		t.Errorf("%d rejections of 40, a proportional sample spends the afternoon confirming the easy ones", rejected)
	}
}

func TestSampleIsBlind(t *testing.T) {
	items, keys := corpus(50)
	for i := range items {
		items[i].Human = "entailed" // whatever was lying in the input
	}
	got, _ := Sample(items, keys, 10, 50, "m12")
	for _, it := range got {
		if it.Human != "" {
			t.Fatal("a sample that shows the machine's answer measures willingness to agree with a machine")
		}
	}
}

func TestSampleIsReproducible(t *testing.T) {
	items, keys := corpus(100)
	a, _ := Sample(items, keys, 20, 50, "m12")
	b, _ := Sample(items, keys, 20, 50, "m12")
	c, _ := Sample(items, keys, 20, 50, "other")
	same := func(x, y []Item) bool {
		for i := range x {
			if x[i].ID != y[i].ID {
				return false
			}
		}
		return true
	}
	if !same(a, b) {
		t.Error("the same seed has to draw the same sample or a reported kappa is not checkable")
	}
	if same(a, c) {
		t.Error("a different seed draws a different sample")
	}
}

func TestSampleTakesWhatItCanWhenRejectionsAreScarce(t *testing.T) {
	items, keys := corpus(20) // two rejections exist
	got, gotKeys := Sample(items, keys, 10, 90, "m12")
	if len(got) != 10 {
		t.Fatalf("drew %d, the shortfall in one stratum is made up from the other", len(got))
	}
	rejected := 0
	for _, k := range gotKeys {
		if k.Stratum == "rejected" {
			rejected++
		}
	}
	if rejected != 2 {
		t.Errorf("%d rejections, there are only two in the corpus", rejected)
	}
}

func TestJudgedDropsUnsureRatherThanPickingASide(t *testing.T) {
	labels := []Item{
		{ID: "a", Human: LabelEntailed},
		{ID: "b", Human: LabelUnsure},
		{ID: "c", Human: LabelNotEntailed},
	}
	keys := []Key{{ID: "a", Machine: LabelEntailed}, {ID: "b", Machine: LabelEntailed}, {ID: "c", Machine: LabelEntailed}}
	a, unsure, disagreed := Judged(labels, keys)
	if a.Pairs != 2 || unsure != 1 {
		t.Errorf("pairs = %d unsure = %d, an annotator who cannot decide has said something real", a.Pairs, unsure)
	}
	if len(disagreed) != 1 || disagreed[0].ID != "c" {
		t.Errorf("disagreed = %+v", disagreed)
	}
}

func TestJudgedSkipsWhatNobodyLabelled(t *testing.T) {
	labels := []Item{{ID: "a", Human: LabelEntailed}, {ID: "b"}}
	keys := []Key{{ID: "a", Machine: LabelEntailed}, {ID: "b", Machine: LabelEntailed}}
	a, _, _ := Judged(labels, keys)
	if a.Pairs != 1 {
		t.Error("a half worked file scores the half that was worked")
	}
}

func TestRejectionCheckOnlyLooksAtWhatTheGateThrewAway(t *testing.T) {
	labels := []Item{
		{ID: "a", Human: LabelNotEntailed}, // gate was right
		{ID: "b", Human: LabelEntailed},    // gate deleted a correct extraction
		{ID: "c", Human: LabelEntailed},    // accepted, not this check's business
	}
	keys := []Key{
		{ID: "a", Machine: LabelNotEntailed, Stratum: "rejected"},
		{ID: "b", Machine: LabelNotEntailed, Stratum: "rejected"},
		{ID: "c", Machine: LabelEntailed, Stratum: "accepted"},
	}
	c := RejectionCheck(labels, keys)
	if c.TP != 1 || c.FP != 1 || c.Decided() != 2 {
		t.Errorf("count = %+v, half the rejections took a correct statement with them", c)
	}
}

func TestLabelFilesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	items := []Item{{ID: "a", Quote: "phải trả lương", Human: LabelEntailed, Why: "the words are there"}}
	keys := []Key{{ID: "a", Machine: LabelNotEntailed, Stratum: "rejected"}}
	labelPath, keyPath := filepath.Join(dir, LabelFile), filepath.Join(dir, KeyFile)
	if err := WriteItems(labelPath, items); err != nil {
		t.Fatal(err)
	}
	if err := WriteKeys(keyPath, keys); err != nil {
		t.Fatal(err)
	}
	gotItems, err := ReadItems(labelPath)
	if err != nil {
		t.Fatal(err)
	}
	gotKeys, err := ReadKeys(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotItems) != 1 || gotItems[0].Why != "the words are there" {
		t.Errorf("items = %+v, the reason is the most valuable field in the file", gotItems)
	}
	if len(gotKeys) != 1 || gotKeys[0].Stratum != "rejected" {
		t.Errorf("keys = %+v", gotKeys)
	}
}

func TestReadItemsNamesTheLineItCouldNotParse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LabelFile)
	if err := writeRaw(path, "{\"id\":\"a\"}\nnot json\n"); err != nil {
		t.Fatal(err)
	}
	_, err := ReadItems(path)
	if err == nil || !contains(err.Error(), "line 2") {
		t.Errorf("err = %v, a person editing this file by hand needs the line number", err)
	}
}

func TestMovesReportTheDirectionAPromptChangeMovedVerdicts(t *testing.T) {
	before := []Key{
		{ID: "a", Machine: LabelNotEntailed}, {ID: "b", Machine: LabelNotEntailed},
		{ID: "c", Machine: LabelEntailed}, {ID: "d", Machine: LabelEntailed},
	}
	after := []Key{
		{ID: "a", Machine: LabelEntailed}, {ID: "b", Machine: LabelEntailed},
		{ID: "c", Machine: LabelNotEntailed}, {ID: "d", Machine: LabelEntailed},
	}
	moves := Moves(before, after)
	if len(moves) != 2 {
		t.Fatalf("moves = %+v, two directions were travelled", moves)
	}
	if moves[0].From != LabelNotEntailed || moves[0].To != LabelEntailed || moves[0].N != 2 {
		t.Errorf("moves[0] = %+v, the larger movement is reported first", moves[0])
	}
	if moves[1].N != 1 {
		t.Errorf("moves[1] = %+v, a prompt that only ever loosens is worth knowing about", moves[1])
	}
}

func TestMovesIgnoreItemsTheRerunDidNotCover(t *testing.T) {
	before := []Key{{ID: "a", Machine: LabelNotEntailed}, {ID: "b", Machine: LabelNotEntailed}}
	after := []Key{{ID: "a", Machine: LabelEntailed}, {ID: "z", Machine: LabelEntailed}}
	moves := Moves(before, after)
	if len(moves) != 1 || moves[0].N != 1 {
		t.Errorf("moves = %+v, an item with no earlier verdict has not moved", moves)
	}
}
