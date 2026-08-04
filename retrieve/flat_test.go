package retrieve

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/law"
)

func TestChunksHoldTheSameWordsAndStraddleComponents(t *testing.T) {
	docs := []*law.Document{labourDoc(), taxDoc()}
	chunks := Chunks(docs, 12, 3)
	if len(chunks) < 3 {
		t.Fatalf("%d chunks, want the text cut into several windows", len(chunks))
	}
	straddling := 0
	for _, c := range chunks {
		if len(c.Covers) > 1 {
			straddling++
		}
	}
	if straddling == 0 {
		t.Error("no chunk crossed a component boundary, so this baseline is not the thing it is standing in for")
	}
	var words int
	for _, d := range docs {
		for i := range d.Provisions {
			words += len(strings.Fields(d.Provisions[i].Text))
		}
	}
	var seen int
	for _, c := range chunks {
		seen += len(strings.Fields(c.Text))
	}
	if seen < words {
		t.Errorf("the chunks hold %d words of the %d in the documents, so the two sides are not seeing the same text", seen, words)
	}
}

func TestTheFlatBaselineRanksChunksWithNoScopeAndNoAspects(t *testing.T) {
	f := BuildFlat([]*law.Document{labourDoc(), taxDoc()}, 12, 3)
	hits := f.Search("đền bù trả lương chậm", 3)
	if len(hits) == 0 {
		t.Fatal("the baseline found nothing at all, which would make every comparison against it meaningless")
	}
	if !strings.Contains(hits[0].Text, "đền bù") {
		t.Errorf("top chunk is %q", hits[0].Text)
	}
	// The employer duty is in the graph, not in the words, so the baseline has
	// no way to reach the clause through it.
	for _, h := range f.Search("người sử dụng", 5) {
		if strings.Contains(h.Text, "đền bù") && !strings.Contains(h.Text, "Người sử dụng") {
			t.Error("a chunk with no employer words was retrieved by an employer query")
		}
	}
	if _, ok := f.Chunk(hits[0].ID); !ok {
		t.Error("a returned chunk cannot be looked up by its own identifier")
	}
}
