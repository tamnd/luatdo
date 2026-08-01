// Package cite finds citation and amendment links between documents.
//
// Links come from two methods, and every link records which one produced it.
// Official dataset metadata is authoritative when present. In-text patterns
// are the fallback, and a pattern hit that points at a document the corpus
// does not contain stays unresolved rather than inventing a target.
package cite

import (
	"regexp"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// Link is one directed edge between a provision and a document.
type Link struct {
	FromDoc       string `json:"from_doc"`
	FromProvision string `json:"from_provision,omitempty"`
	ToNumber      string `json:"to_number"`
	ToDoc         string `json:"to_doc,omitempty"` // empty when unresolved
	Kind          string `json:"kind"`             // cites or amends
	Method        string `json:"method"`           // official or pattern
	Snippet       string `json:"snippet,omitempty"`
}

// citation matches references such as "Luật số 32/2013/QH13" or
// "Nghị định số 15/2020/NĐ-CP". The document type word is optional because
// running text often says only "số 32/2013/QH13".
var citation = regexp.MustCompile(
	`(?:(?:Bộ luật|Luật|Nghị quyết|Nghị định|Pháp lệnh|Thông tư|Quyết định)\s+)?số\s+(\d+/\d{4}/[A-ZĐ][A-ZĐ0-9-]*)`)

// amendmentPhrase marks the standard amending formula. Case-insensitive
// because the formula opens headings as often as it appears in running text.
var amendmentPhrase = regexp.MustCompile(`(?i)sửa đổi, bổ sung một số điều`)

// Resolve scans every provision of doc for citations and resolves them
// against the corpus index, a map from official number to document ID.
func Resolve(doc *law.Document, index map[string]string) []Link {
	var links []Link
	seen := map[string]bool{}
	for i := range doc.Provisions {
		prov := &doc.Provisions[i]
		text := prov.Heading + "\n" + prov.Text
		for _, m := range citation.FindAllStringSubmatchIndex(text, -1) {
			number := text[m[2]:m[3]]
			if equalNumber(number, doc.OfficialNumber) {
				continue
			}
			kind := "cites"
			if amendmentPhrase.MatchString(before(text, m[0], 240)) {
				kind = "amends"
			}
			key := prov.ID + "|" + number + "|" + kind
			if seen[key] {
				continue
			}
			seen[key] = true
			links = append(links, Link{
				FromDoc:       doc.ID,
				FromProvision: prov.ID,
				ToNumber:      number,
				ToDoc:         index[normalizeNumber(number)],
				Kind:          kind,
				Method:        "pattern",
				Snippet:       snippet(text, m[0], 120),
			})
		}
	}
	return links
}

// Merge combines the in-text links of one document with its official links.
//
// Official metadata wins: where the dataset already states that A amends B,
// the pattern hit that says the same thing is dropped, so the graph carries
// one edge with the stronger provenance. A pattern hit the official graph does
// not cover survives, which is the whole reason both methods exist.
func Merge(pattern, official []Link) []Link {
	stated := map[string]bool{}
	for _, l := range official {
		stated[l.ToDoc] = true
	}
	out := append([]Link(nil), official...)
	for _, l := range pattern {
		if l.ToDoc != "" && stated[l.ToDoc] {
			continue
		}
		out = append(out, l)
	}
	return out
}

// Index builds the official number to document ID map used for resolution.
//
// A number carried by more than one document resolves to nothing. Across the
// full corpus that is the ordinary case for a local number: sixty provinces
// issue their own "01/2024/QĐ-UBND", and text citing that number without naming
// the province has not said which one it means. Leaving the citation
// unresolved states that honestly, where picking one province would invent an
// edge between two documents that have nothing to do with each other.
func Index(docs []*law.Document) map[string]string {
	index := make(map[string]string, len(docs))
	ambiguous := map[string]bool{}
	for _, d := range docs {
		number := normalizeNumber(d.OfficialNumber)
		if held, taken := index[number]; taken && held != d.ID {
			ambiguous[number] = true
		}
		index[number] = d.ID
	}
	for number := range ambiguous {
		delete(index, number)
	}
	return index
}

func normalizeNumber(s string) string {
	return strings.ToUpper(strings.TrimRight(strings.TrimSpace(s), ".,;"))
}

func equalNumber(a, b string) bool {
	return normalizeNumber(a) == normalizeNumber(b)
}

// before returns the window of text preceding offset, trimmed to whole runes.
// The amending formula precedes the number it amends, so the window only
// looks backwards.
func before(text string, offset, width int) string {
	start := max(offset-width, 0)
	for start > 0 && !isRuneStart(text[start]) {
		start--
	}
	return text[start:offset]
}

// snippet returns a window of text around offset, trimmed to whole runes.
func snippet(text string, offset, width int) string {
	start := max(offset-width/3, 0)
	for start > 0 && !isRuneStart(text[start]) {
		start--
	}
	end := min(offset+width, len(text))
	for end < len(text) && !isRuneStart(text[end]) {
		end++
	}
	return strings.Join(strings.Fields(text[start:end]), " ")
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
