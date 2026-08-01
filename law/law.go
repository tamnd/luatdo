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
	IssuingBody    string      `json:"issuing_body,omitempty"`
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

// DocIDIn builds the stable document identifier of a document issued by a named
// body. For a number issued centrally the body is ignored and the result is the
// same as DocID, because "15/2018/NĐ-CP" names one decree of the government and
// nothing else. For a body relative number it is not: every province issues its
// own "01/2024/QĐ-UBND", so the identifier carries the issuing body and reads
// "vn:law:2024:01-2024-qd-ubnd:ubnd-tinh-long-an". Without a body such a number
// is not an identity at all, and this reports that as an error rather than
// handing back an identifier that another province also owns.
func DocIDIn(officialNumber, issuingBody string) (string, error) {
	id, err := DocID(officialNumber)
	if err != nil {
		return "", err
	}
	if !BodyRelative(officialNumber) {
		return id, nil
	}
	body := Slug(issuingBody)
	if body == "" {
		return "", fmt.Errorf("official number %q is issued under a local number and needs its issuing body", officialNumber)
	}
	return id + ":" + body, nil
}

// localBodies are the issuer abbreviations that name a kind of body rather than
// one body. Every province, district, and ward has a people's committee and a
// people's council, so a number ending in one of these repeats across the
// country. "UB" is the form used before 2005 and "CTUBND" is the chair of a
// committee acting alone. Abbreviations that merely start the same way, such as
// UBTVQH for the standing committee of the National Assembly or HĐBT for the
// former Council of Ministers, name exactly one body and are deliberately not
// in this list.
var localBodies = map[string]bool{
	"UBND":   true,
	"HDND":   true,
	"UB":     true,
	"CTUBND": true,
	"CTUB":   true,
}

// BodyRelative reports whether an official number is unique only within the body
// that issued it. The signal is in the number itself: the suffix of a central
// instrument names one body ("NĐ-CP", "TT-BTC", "QH14"), while the suffix of a
// local one names a people's committee or a people's council, of which there are
// thousands. Reading the number beats reading the scope column of any one
// dataset, which is free text and dirty in every corpus seen so far.
func BodyRelative(officialNumber string) bool {
	suffix := strings.TrimSpace(officialNumber)
	if i := strings.LastIndex(suffix, "/"); i >= 0 {
		suffix = suffix[i+1:]
	}
	_, issuer, ok := strings.Cut(suffix, "-")
	if !ok {
		return false
	}
	for part := range strings.SplitSeq(issuer, "-") {
		if localBodies[bodyAbbrev(part)] {
			return true
		}
	}
	return false
}

// bodyAbbrev reduces one issuer token to the letters it opens with, so the
// council term in "HĐND8" and the stray punctuation in "UBND." do not hide the
// abbreviation underneath.
func bodyAbbrev(token string) string {
	letters := strings.ToUpper(asciiFold(strings.TrimSpace(token)))
	for i := range len(letters) {
		if letters[i] < 'A' || letters[i] > 'Z' {
			return letters[:i]
		}
	}
	return letters
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

// vietnameseFold maps every Vietnamese letter with diacritics onto its base
// ASCII letter. It is the full table, unlike asciiFold, which only covers the
// characters that appear in issuing body abbreviations.
var vietnameseFold = func() *strings.Replacer {
	groups := map[string]string{
		"a": "áàảãạăắằẳẵặâấầẩẫậ",
		"e": "éèẻẽẹêếềểễệ",
		"i": "íìỉĩị",
		"o": "óòỏõọôốồổỗộơớờởỡợ",
		"u": "úùủũụưứừửữự",
		"y": "ýỳỷỹỵ",
		"d": "đ",
	}
	var pairs []string
	for base, letters := range groups {
		for _, r := range letters {
			pairs = append(pairs, string(r), base)
			pairs = append(pairs, strings.ToUpper(string(r)), strings.ToUpper(base))
		}
	}
	return strings.NewReplacer(pairs...)
}()

var slugCleanup = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a Vietnamese phrase into a stable ASCII identifier segment:
// "Người sử dụng lao động" becomes "nguoi-su-dung-lao-dong".
func Slug(s string) string {
	folded := strings.ToLower(vietnameseFold.Replace(strings.TrimSpace(s)))
	return strings.Trim(slugCleanup.ReplaceAllString(folded, "-"), "-")
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
