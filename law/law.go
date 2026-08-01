// Package law defines the canonical document model and stable identifiers.
//
// Identifiers are structural and reproducible from the official instrument and
// its location in the document, never generated. The same document parsed twice
// yields byte-identical identifiers.
package law

import (
	"fmt"
	"regexp"
	"strings"
)

// Document is one legal instrument in canonical form.
type Document struct {
	ID             string      `json:"id"`
	OfficialNumber string      `json:"official_number"`
	Title          string      `json:"title"`
	TitleEN        string      `json:"title_en,omitempty"`
	DocType        string      `json:"doc_type"` // constitution, code, law
	EffectiveFrom  string      `json:"effective_from,omitempty"`
	Source         string      `json:"source"`     // dataset name
	SourceRef      string      `json:"source_ref"` // dataset revision
	SourceURL      string      `json:"source_url,omitempty"`
	SourceHash     string      `json:"source_hash"` // sha256 of the raw content
	Status         string      `json:"status"`      // parsed or quarantined
	Quarantine     string      `json:"quarantine,omitempty"`
	Provisions     []Provision `json:"provisions,omitempty"`
}

// Provision is one structural node: a chapter, section, article, clause, or point.
type Provision struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id,omitempty"`
	Kind     string `json:"kind"` // chapter, section, article, clause, point
	Number   string `json:"number"`
	Heading  string `json:"heading,omitempty"`
	Text     string `json:"text,omitempty"`
	TextHash string `json:"text_hash,omitempty"`
	Position int    `json:"position"`
}

var (
	numberPattern       = regexp.MustCompile(`^(\d+)/(\d{4})/([A-Za-z0-9ĐđƯưÔôÂâ-]+)$`)
	constitutionPattern = regexp.MustCompile(`^Hiến pháp\s+(\d{4})$`)
)

// DocID builds the stable document identifier from an official number such as
// "45/2019/QH14". The result is "vn:law:2019:45-2019-qh14". Document type
// stays a property rather than part of the identifier, so a code and a law
// share one namespace and an identifier never changes if a type label does.
func DocID(officialNumber string) (string, error) {
	trimmed := strings.TrimSpace(officialNumber)
	if m := constitutionPattern.FindStringSubmatch(trimmed); m != nil {
		return "vn:constitution:" + m[1] + ":hien-phap-" + m[1], nil
	}
	m := numberPattern.FindStringSubmatch(trimmed)
	if m == nil {
		return "", fmt.Errorf("official number %q does not match number/year/body", officialNumber)
	}
	slug := strings.ToLower(m[1] + "-" + m[2] + "-" + asciiFold(m[3]))
	return "vn:law:" + m[2] + ":" + slug, nil
}

// ProvisionID appends a structural segment to a parent identifier:
// ProvisionID("vn:law:2019:45-2019-qh14", "article", "94") returns
// "vn:law:2019:45-2019-qh14:article-94".
func ProvisionID(parent, kind, number string) string {
	return parent + ":" + kind + "-" + strings.ToLower(asciiFold(number))
}

// asciiFold maps the Vietnamese characters that appear in issuing body
// abbreviations and point letters onto ASCII, so identifiers stay portable.
func asciiFold(s string) string {
	r := strings.NewReplacer(
		"Đ", "d", "đ", "d",
		"Ư", "u", "ư", "u",
		"Ô", "o", "ô", "o",
		"Â", "a", "â", "a",
	)
	return r.Replace(s)
}

// FileName maps an identifier to a portable file name. Colons are not legal
// in file names on Windows, and the fleet includes a Windows machine.
func FileName(id string) string {
	return strings.ReplaceAll(id, ":", "_") + ".json"
}

// RomanToArabic converts a roman numeral chapter number such as "IV" to "4".
// A number that is not roman comes back unchanged.
func RomanToArabic(s string) string {
	values := map[byte]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100}
	upper := strings.ToUpper(strings.TrimSpace(s))
	if upper == "" {
		return s
	}
	total, prev := 0, 0
	for i := len(upper) - 1; i >= 0; i-- {
		v, ok := values[upper[i]]
		if !ok {
			return s
		}
		if v < prev {
			total -= v
		} else {
			total += v
			prev = v
		}
	}
	return fmt.Sprintf("%d", total)
}
