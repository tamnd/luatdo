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
	return parent + ":" + kind + "-" + NumberSegment(number)
}

// repeat separates a structural segment from the occurrence index that tells
// two provisions apart when the instrument numbers them the same. It is a
// character no structural number contains and no file system or IRI objects
// to, and it reads as what it is: an identifier the drafter did not give.
const repeat = "~"

// RepeatID is the identifier of the nth thing in one document to claim a
// number, counting from two. RepeatID("...:point-d", 2) is "...:point-d~2".
//
// A document that numbers two of its points d is not a document this can
// parse into two identifiers the drafter would recognise, and there is no
// third option: either one identifier answers for both provisions, which is
// the defect this exists to stop, or the second occurrence says out loud that
// it is the second. The index is the provision's place in the document, so it
// is as structural and as reproducible as the number beside it.
func RepeatID(id string, n int) string {
	return id + repeat + fmt.Sprintf("%d", n)
}

// Repeated reports whether an identifier names a provision whose own number the
// document had already used.
//
// Only the last segment is read. A clause of a second Điều 3 carries the index
// its article was given and its own number is not repeated at all, so counting
// every identifier with an index in it counts the children of one bad article
// as if the drafter had numbered each of them twice.
func Repeated(id string) bool {
	return strings.Contains(id[strings.LastIndex(id, ":")+1:], repeat)
}

// telex spells the seven modified letters of the Vietnamese alphabet the way
// every Vietnamese typist spells them on an ASCII keyboard. Tone marks are not
// in the table because a structural number never carries one.
var telex = strings.NewReplacer(
	"ă", "aw", "Ă", "aw",
	"â", "aa", "Â", "aa",
	"ê", "ee", "Ê", "ee",
	"ô", "oo", "Ô", "oo",
	"ơ", "ow", "Ơ", "ow",
	"ư", "uw", "Ư", "uw",
	"đ", "dd", "Đ", "dd",
)

// NumberSegment turns a structural number into the identifier segment that
// names it: "94" stays "94", "15a" stays "15a", and "đ" becomes "dd".
//
// The last one is the whole point. Vietnamese drafters letter their points a,
// b, c, d, đ, e, and đ is its own letter of the alphabet rather than a
// decorated d. Folding it to "d" gives two different points of the same clause
// one identifier, which is not a cosmetic problem: whichever one is parsed last
// wins, and a point in time query then answers with a neighbour's text while
// looking exactly like a correct answer. In one sample of 2,400 documents that
// fold produced 1,986 collisions.
func NumberSegment(number string) string {
	return strings.ToLower(telex.Replace(strings.TrimSpace(number)))
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

// HasPhrase reports whether a phrase occurs in a text as whole words, with
// diacritics, casing and punctuation folded away on both sides.
//
// The whole word part is what makes it usable on folded text. Vietnamese words
// are short and folding makes them shorter, so a plain substring search for
// "cấm" finds one inside "camera" and a search for "quy" finds one in most of
// the corpus. Matching between the separators the slug already inserted costs
// one allocation and rules that out.
//
// It does not rule out the other collision, which folding creates and nothing
// here can undo: "phải" and "phái" are both "phai" afterwards. A caller that
// needs those apart has to work on the original text.
func HasPhrase(text, phrase string) bool {
	p := Slug(phrase)
	if p == "" {
		return false
	}
	return strings.Contains("-"+Slug(text)+"-", "-"+p+"-")
}

var dmy = regexp.MustCompile(`^(\d{2})/(\d{2})/(\d{4})$`)
var iso = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)

// ISODate turns a date as the corpus states it into one that sorts.
//
// The datasets write "17/08/2007" and a graph that compares dates as text needs
// "2007-08-17", because "17/08/2007" sorts before "01/06/2016" and a version
// graph built on that ordering applies amendments in the wrong order while
// looking finished. Anything that is neither form comes back empty rather than
// guessed: an unparseable date is a date nobody has, and inventing one puts it
// beyond telling apart from a date somebody read off the instrument.
func ISODate(s string) string {
	s = strings.TrimSpace(s)
	if iso.MatchString(s) {
		return s
	}
	if m := dmy.FindStringSubmatch(s); m != nil {
		return m[3] + "-" + m[2] + "-" + m[1]
	}
	return ""
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
