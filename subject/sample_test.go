package subject

import (
	"fmt"
	"testing"
)

// corpus builds a lopsided record set: one huge stratum, one middling one and
// one that is a handful of documents. That shape is the corpus. Sixty five
// thousand provincial decisions sit alongside eight codes, and a sampler that
// behaves well on an even spread tells you nothing about how it behaves here.
func corpus() []Record {
	var out []Record
	add := func(n int, subject, docType, prefix string) {
		for i := range n {
			out = append(out, Record{
				DocID:    fmt.Sprintf("vn:law:2020:%s-%d", prefix, i),
				DocType:  docType,
				Subjects: []Assignment{{SubjectID: subject, Method: MethodLexical}},
			})
		}
	}
	add(600, "dat-dai/gia-dat", "quyết định", "gd")
	add(90, "y-te/duoc-my-pham", "thông tư", "dm")
	add(6, "khoa-hoc-cong-nghe/nang-luong-nguyen-tu", "nghị định", "nlnt")
	return out
}

func TestSampleIsReproducible(t *testing.T) {
	first := Sample(corpus(), 60, "m6")
	second := Sample(corpus(), 60, "m6")
	if len(first) != len(second) {
		t.Fatalf("two runs drew %d and %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("two runs disagree at %d: %v and %v", i, first[i], second[i])
		}
	}
}

func TestSampleFollowsTheSeed(t *testing.T) {
	a := Sample(corpus(), 60, "m6")
	b := Sample(corpus(), 60, "m7")
	same := 0
	for i := range a {
		if a[i].DocID == b[i].DocID {
			same++
		}
	}
	if same == len(a) {
		t.Errorf("two seeds drew the same sixty documents")
	}
}

func TestSampleDrawsExactlyWhatWasAskedFor(t *testing.T) {
	for _, n := range []int{1, 5, 60, 200, 696} {
		got := Sample(corpus(), n, "m6")
		if len(got) != n {
			t.Errorf("asked for %d, got %d", n, len(got))
		}
	}
}

func TestSampleCannotDrawMoreThanExists(t *testing.T) {
	got := Sample(corpus(), 5000, "m6")
	if len(got) != len(corpus()) {
		t.Errorf("asked for five thousand from a corpus of %d, got %d", len(corpus()), len(got))
	}
	seen := map[string]bool{}
	for _, s := range got {
		if seen[s.DocID] {
			t.Fatalf("document %s drawn twice", s.DocID)
		}
		seen[s.DocID] = true
	}
}

func TestSampleReachesTheSmallStratum(t *testing.T) {
	counts := map[Stratum]int{}
	for _, s := range Sample(corpus(), 60, "m6") {
		counts[s.Stratum]++
	}
	if len(counts) != 3 {
		t.Fatalf("got %d strata, want all three", len(counts))
	}
	small := Stratum{Subject: "khoa-hoc-cong-nghe/nang-luong-nguyen-tu", DocType: "nghị định"}
	if counts[small] == 0 {
		t.Errorf("the six document stratum was never drawn from, which is what proportional sampling does and stratified sampling exists to stop")
	}
	// The big stratum still dominates, because a sample that ignored corpus
	// shape would be as wrong in the other direction.
	big := Stratum{Subject: "dat-dai/gia-dat", DocType: "quyết định"}
	if counts[big] <= counts[small] {
		t.Errorf("the six hundred document stratum got %d places and the six document one got %d", counts[big], counts[small])
	}
}

func TestSampleGrowsWithoutRedrawingTheSample(t *testing.T) {
	before := Sample(corpus(), 60, "m6")
	grown := append(corpus(), Record{
		DocID:    "vn:law:2021:new-1",
		DocType:  "quyết định",
		Subjects: []Assignment{{SubjectID: "dat-dai/gia-dat", Method: MethodLexical}},
	})
	after := Sample(grown, 60, "m6")

	kept := map[string]bool{}
	for _, s := range after {
		kept[s.DocID] = true
	}
	lost := 0
	for _, s := range before {
		if !kept[s.DocID] {
			lost++
		}
	}
	// A rank derived from the identifier means one new document can displace at
	// most a few picks. A counter based sampler would reshuffle everything.
	if lost > 2 {
		t.Errorf("one new document cost %d of the previous sixty picks", lost)
	}
}

func TestSampleSeparatesDocumentTypesWithinASubject(t *testing.T) {
	records := []Record{
		{DocID: "a", DocType: "luật", Subjects: []Assignment{{SubjectID: "dat-dai", Method: MethodLexical}}},
		{DocID: "b", DocType: "nghị định", Subjects: []Assignment{{SubjectID: "dat-dai", Method: MethodLexical}}},
		{DocID: "c", DocType: "quyết định", Subjects: []Assignment{{SubjectID: "dat-dai", Method: MethodLexical}}},
	}
	got := Sample(records, 3, "m6")
	seen := map[string]bool{}
	for _, s := range got {
		seen[s.Stratum.DocType] = true
	}
	if len(seen) != 3 {
		t.Errorf("one subject with three instrument types gave %d strata, want 3", len(seen))
	}
}

func TestSampleGivesUnclassifiedDocumentsAStratumOfTheirOwn(t *testing.T) {
	records := append(corpus(),
		Record{DocID: "unknown-1", DocType: "quyết định"},
		Record{DocID: "unknown-2", DocType: "quyết định"},
	)
	counts := map[Stratum]int{}
	for _, s := range Sample(records, 60, "m6") {
		counts[s.Stratum]++
	}
	if counts[Stratum{Subject: "", DocType: "quyết định"}] == 0 {
		t.Errorf("the documents the classifier could not read were never sampled, and they are the ones worth reading")
	}
}

func TestSampleOfNothingIsNothing(t *testing.T) {
	if got := Sample(nil, 10, "m6"); got != nil {
		t.Errorf("got %v from an empty corpus", got)
	}
	if got := Sample(corpus(), 0, "m6"); got != nil {
		t.Errorf("got %v when asked for none", got)
	}
}
