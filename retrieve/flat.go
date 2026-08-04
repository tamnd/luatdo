package retrieve

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// Flat is the baseline this project has to beat: the same text cut into fixed
// windows, ranked by the same BM25, with no scope and no graph derived aspects.
//
// It is here rather than described in a document because a baseline nobody runs
// is a claim. Almost every retrieval augmented system in production is this,
// and if structure aware retrieval does not beat it on the benchmark then the
// graph is not paying for itself on retrieval and the honest thing is to say so
// and defend the graph on the questions a chunk index cannot express at all.
//
// The chunks are made from the same provisions in document order, so both sides
// see exactly the same words. What the baseline does not get is the component
// boundary: a chunk can straddle two articles, and when it does, the identifier
// it reports is the article the chunk started in. That is not a handicap this
// package invented, it is the thing chunking does.
type Flat struct {
	chunks []Chunk
	field  *field
	units  []*Unit
}

// Chunk is one window of text with the component it started in.
type Chunk struct {
	ID      string   `json:"id"`
	DocID   string   `json:"doc_id"`
	StartID string   `json:"start_component"`
	Covers  []string `json:"covers"`
	Text    string   `json:"text"`
}

// DefaultChunk is the window size in syllables, with the overlap that keeps a
// sentence from being cut in half at every boundary. Both are the numbers most
// pipelines use.
const (
	DefaultChunk   = 250
	DefaultOverlap = 50
)

// Chunks cuts documents into overlapping windows.
func Chunks(docs []*law.Document, size, overlap int) []Chunk {
	if size <= 0 {
		size = DefaultChunk
	}
	if overlap < 0 || overlap >= size {
		overlap = DefaultOverlap
	}
	var out []Chunk
	for _, doc := range docs {
		var words []string
		var owner []string
		for i := range doc.Provisions {
			p := &doc.Provisions[i]
			if strings.TrimSpace(p.Text) == "" {
				continue
			}
			for w := range strings.FieldsSeq(p.Text) {
				words = append(words, w)
				owner = append(owner, p.ID)
			}
		}
		for start := 0; start < len(words); start += size - overlap {
			end := min(start+size, len(words))
			covers := []string{}
			seen := map[string]bool{}
			for _, id := range owner[start:end] {
				if !seen[id] {
					seen[id] = true
					covers = append(covers, id)
				}
			}
			out = append(out, Chunk{
				ID:      fmt.Sprintf("%s#chunk-%d", doc.ID, len(out)),
				DocID:   doc.ID,
				StartID: owner[start],
				Covers:  covers,
				Text:    strings.Join(words[start:end], " "),
			})
			if end == len(words) {
				break
			}
		}
	}
	return out
}

// BuildFlat indexes chunks and nothing else.
func BuildFlat(docs []*law.Document, size, overlap int) *Flat {
	chunks := Chunks(docs, size, overlap)
	f := &Flat{chunks: chunks}
	f.units = make([]*Unit, len(chunks))
	for i := range chunks {
		f.units[i] = &Unit{
			ComponentID: chunks[i].ID, DocID: chunks[i].DocID, Kind: "chunk",
			Text: chunks[i].Text, aspects: map[string][]string{AspectText: {chunks[i].Text}},
		}
	}
	f.field = buildField(f.units, AspectText)
	return f
}

// Len is how many chunks the baseline holds.
func (f *Flat) Len() int { return len(f.chunks) }

// Chunk returns one chunk by position in the ranking's identifier space.
func (f *Flat) Chunk(id string) (Chunk, bool) {
	for _, c := range f.chunks {
		if c.ID == id {
			return c, true
		}
	}
	return Chunk{}, false
}

// Search ranks chunks. There is no scope argument because the baseline has no
// scope, which is the difference being measured.
func (f *Flat) Search(query string, k int) []Chunk {
	if k <= 0 {
		k = 10
	}
	scores := map[int]float64{}
	for _, t := range Tokens(query) {
		idf := f.field.idf(t, len(f.units))
		if idf == 0 {
			continue
		}
		for _, p := range f.field.postings[t] {
			length := float64(f.field.length[p.unit])
			tf := float64(p.freq) * (k1 + 1) /
				(float64(p.freq) + k1*(1-b+b*length/maxFloat(f.field.average, 1)))
			scores[p.unit] += idf * tf
		}
	}
	order := make([]int, 0, len(scores))
	for i := range scores {
		order = append(order, i)
	}
	sort.Slice(order, func(i, j int) bool {
		if scores[order[i]] != scores[order[j]] {
			return scores[order[i]] > scores[order[j]]
		}
		return order[i] < order[j]
	})
	if len(order) > k {
		order = order[:k]
	}
	out := make([]Chunk, 0, len(order))
	for _, i := range order {
		out = append(out, f.chunks[i])
	}
	return out
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
