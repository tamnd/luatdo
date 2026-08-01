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

// Index builds the official number to document ID map used for resolution.
func Index(docs []*law.Document) map[string]string {
	index := map[string]string{}
	for _, d := range docs {
		index[normalizeNumber(d.OfficialNumber)] = d.ID
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
