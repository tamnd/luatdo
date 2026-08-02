// Package parse converts raw legal documents into the canonical model.
//
// Parsing is deterministic. The same input bytes always produce the same
// provision tree and the same identifiers, and no model is involved anywhere.
// A document whose content fails a sanity check is quarantined with a reason
// instead of being silently dropped or half parsed.
package parse

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/store"
)

// Input is one raw document with its dataset metadata.
type Input struct {
	OfficialNumber string
	// IssuingBody is the authority that signed the document. It is part of the
	// identity of a document numbered locally, where the number alone repeats
	// across every province, and it is only a property everywhere else.
	IssuingBody   string
	Title         string
	TitleEN       string
	DocType       string
	Content       string
	Source        string
	SourceRef     string
	SourceURL     string
	EffectiveFrom string
	// MetadataOnly marks a document whose text has not been downloaded. It
	// still becomes a node, because the official citation graph points at it
	// and an edge to a document that is not there is worse than a document
	// that is honestly marked as text pending.
	MetadataOnly bool
}

var (
	frontMatterKey = regexp.MustCompile(`^\*\*(.+?):\*\*\s*(.*)$`)
	chapterLine    = regexp.MustCompile(`^(?:Chương|CHƯƠNG)\s+([0-9]+|[IVXLC]+)\s*[:.]?\s*(.*)$`)
	sectionLine    = regexp.MustCompile(`^(?:Mục|MỤC)\s+([0-9]+|[IVXLC]+)\s*[:.]?\s*(.*)$`)
	// An article heading is "Điều N. Heading", a bare "Điều N" alone on its
	// line as in the Constitution, or the dotless "Điều N Heading" some
	// amending laws use. The dotless form is accepted only when the heading
	// starts with an uppercase letter, because running text that references an
	// article ("Điều 22 được sửa đổi") continues in lowercase and must not
	// open a new article.
	articleLine = regexp.MustCompile(`^Điều\s+(\d+[a-zđ]?)(?:\.\s*(.*)|\s+(\p{Lu}.*))?$`)
	// A clause opens with "N. text" or, in some laws, a bare "N." alone on
	// its line with the clause text on the following lines.
	clauseLine    = regexp.MustCompile(`^(\d+[a-zđ]?)\.(?:\s+(.*))?$`)
	pointLine     = regexp.MustCompile(`^([a-zđ]{1,2})\)\s+(.*)$`)
	numberLine    = regexp.MustCompile(`^Số:\s*([0-9]+/[0-9]{4}/[^\s,;]+)`)
	signatureLine = regexp.MustCompile(`^(TM\.|KT\.|CHỦ TỊCH QUỐC HỘI|CHỦ TỊCH NƯỚC|Nơi nhận:|XÁC THỰC VĂN BẢN)`)
	// An annex opens with its label alone on a line. "PHỤ LỤC" is unambiguous
	// on its own, because a reference to an annex in running text continues on
	// the same line and so never matches.
	annexLabelLine = regexp.MustCompile(`^(?:PHỤ LỤC|Phụ lục)(?:\s+(?:số\s+)?([IVXLC]+|\d+[A-Za-zĐđ]?))?\s*[:.]?$`)
	// A sub instrument opens the same way, with its kind alone on a line. It
	// is not accepted on that alone, because the word also opens the parent
	// decision's own title block; the issuance formula below is what confirms
	// it. "QUYẾT ĐỊNH" is deliberately absent: it is the parent's own heading.
	annexKindLine = regexp.MustCompile(`^(QUY ĐỊNH|QUY CHẾ|QUY TRÌNH|QUY TẮC|ĐIỀU LỆ|NỘI QUY|THỂ LỆ|DANH MỤC|BIỂU MẪU|ĐỀ ÁN|KẾ HOẠCH|CHƯƠNG TRÌNH|PHƯƠNG ÁN|HƯỚNG DẪN)\s*[:.]?$`)
	// The issuance formula that names the parent instrument. Its presence is
	// what makes the block above it an annex rather than a title.
	issuanceLine = regexp.MustCompile(`(?i)^[(（]?\s*(?:ban hành\s+)?kèm theo\b`)
	// Decorations the sources put between an annex header and its title: rules,
	// the national heading, and the motto under it.
	decorationLine = regexp.MustCompile(`^(?:[-_–—=*.\s]+|CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM|Độc lập\s*[-–—]\s*Tự do\s*[-–—]\s*Hạnh phúc)$`)
)

// annexWindow is how many lines after an annex label the issuance formula may
// appear. The title of an annex runs to three or four lines and the sources
// put a rule and the national heading around it, so ten is generous without
// reaching the annex's own first article.
const annexWindow = 10

// Parse converts one raw document. It always returns a document; a failed
// sanity check comes back with Status quarantined and an empty provision tree.
func Parse(in Input) (*law.Document, error) {
	id, err := law.DocIDIn(in.OfficialNumber, in.IssuingBody)
	if err != nil {
		return nil, err
	}
	doc := &law.Document{
		ID:             id,
		OfficialNumber: in.OfficialNumber,
		IssuingBody:    in.IssuingBody,
		Title:          in.Title,
		TitleEN:        in.TitleEN,
		DocType:        in.DocType,
		Source:         in.Source,
		SourceRef:      in.SourceRef,
		SourceURL:      in.SourceURL,
		SourceHash:     store.HashBytes([]byte(in.Content)),
		EffectiveFrom:  in.EffectiveFrom,
		Status:         "parsed",
	}
	if in.MetadataOnly {
		doc.Status = "metadata"
		return doc, nil
	}

	front, body := splitFrontMatter(in.Content)
	if v := front["Ngày hiệu lực"]; v != "" {
		doc.EffectiveFrom = v
	}
	if doc.TitleEN == "" {
		doc.TitleEN = front["English"]
	}

	if reason := quarantineReason(in.OfficialNumber, body); reason != "" {
		doc.Status = "quarantined"
		doc.Quarantine = reason
		return doc, nil
	}

	doc.Provisions = parseBody(id, body)
	return doc, nil
}

// splitFrontMatter separates the generated header block from the document
// body. The header is the title line and the bold key-value lines above the
// first horizontal rule; everything after that rule is the body.
func splitFrontMatter(content string) (map[string]string, string) {
	front := map[string]string{}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		s := strings.TrimSpace(line)
		if s == "---" {
			return front, strings.Join(lines[i+1:], "\n")
		}
		if m := frontMatterKey.FindStringSubmatch(s); m != nil {
			front[m[1]] = strings.TrimSpace(m[2])
		}
	}
	return front, content
}

// quarantineReason checks that the body is the instrument the metadata says
// it is. The dataset contains rows whose content is a different document
// entirely, and those must not enter the graph under a wrong identity.
func quarantineReason(officialNumber, body string) string {
	if line := firstMatchingLine(body, numberLine); line != "" {
		m := numberLine.FindStringSubmatch(line)
		if m != nil && !equalNumber(m[1], officialNumber) {
			return "body carries official number " + m[1] + ", metadata says " + officialNumber
		}
	}
	if countArticleLines(body, 2) < 2 {
		return "no article structure found in body"
	}
	return ""
}

// countArticleLines counts lines that open an article, up to limit. The
// article pattern is anchored to a line, so the body is walked line by line
// exactly the way the tree builder walks it.
func countArticleLines(body string, limit int) int {
	count := 0
	for line := range strings.SplitSeq(body, "\n") {
		if articleLine.MatchString(strings.TrimSpace(line)) {
			count++
			if count >= limit {
				break
			}
		}
	}
	return count
}

func firstMatchingLine(body string, re *regexp.Regexp) string {
	for line := range strings.SplitSeq(body, "\n") {
		s := strings.TrimSpace(line)
		if re.MatchString(s) {
			return s
		}
	}
	return ""
}

func equalNumber(a, b string) bool {
	clean := func(s string) string {
		return strings.ToUpper(strings.TrimRight(strings.TrimSpace(s), ".,;"))
	}
	return clean(a) == clean(b)
}

// parser builds the provision tree. Open provisions are tracked by index
// because appending to the slice moves it, so a pointer taken earlier would
// write into an abandoned backing array.
type parser struct {
	provisions []law.Provision
	// root is what structural identifiers hang off: the document, or the annex
	// while one is open. An annex restarts its article numbering at one, so
	// without this the annex's Điều 1 and the parent decision's Điều 1 would
	// be the same node and one would overwrite the other.
	root    string
	annex   int
	annexes int
	chapter int
	section int
	article int
	clause  int
	point   int

	// quote is how deep the walk is inside a quotation. An amending law quotes
	// the text it enacts, and everything inside the quotation belongs to the
	// instruction that opened it rather than to this document's own structure.
	quote int
}

func (p *parser) add(prov law.Provision) int {
	prov.Position = len(p.provisions) + 1
	p.provisions = append(p.provisions, prov)
	return len(p.provisions) - 1
}

func (p *parser) at(i int) *law.Provision {
	if i < 0 {
		return nil
	}
	return &p.provisions[i]
}

// addHeading attaches a heading line to a chapter or a section. The sources
// put the number on one line and the title on the next, in capitals, and a
// title that runs long is broken across several lines, so the lines are joined
// rather than the first one taken. Anything not in capitals is not a heading.
func addHeading(h *law.Provision, line string) {
	if line != strings.ToUpper(line) {
		return
	}
	if h.Heading == "" {
		h.Heading = line
		return
	}
	h.Heading += " " + line
}

// annexID is the identifier of the open annex, empty at the top level.
func (p *parser) annexID() string {
	if p.annex < 0 {
		return ""
	}
	return p.at(p.annex).ID
}

// openAnnex tries to read an annex header starting at lines[i]. On success it
// adds the annex, makes it the root of everything that follows, and returns
// the index of its last header line. On failure it changes nothing.
//
// An annex is numbered by its position rather than by the label it carries,
// because a document can attach a Quy chế and a Phụ lục I at once and the two
// labels do not share a numbering sequence. The label is kept verbatim in the
// heading, which is where a reader looks for it.
func (p *parser) openAnnex(docID string, lines []string, i int) (int, bool) {
	label := ""
	confirmed := false
	switch {
	case annexLabelLine.MatchString(lines[i]):
		label = lines[i]
		confirmed = true
	case annexKindLine.MatchString(lines[i]):
		label = lines[i]
	default:
		return i, false
	}

	var title []string
	end := i
	for j := i + 1; j < len(lines) && j <= i+annexWindow; j++ {
		if issuanceLine.MatchString(lines[j]) {
			confirmed, end = true, j
			// The formula runs on for a line or two with the parent's number
			// and date, and none of it is provision text.
			for ; end+1 < len(lines) && !endsIssuance(lines[end]); end++ {
			}
			break
		}
		if isStructural(lines[j]) {
			break
		}
		if !decorationLine.MatchString(lines[j]) {
			title = append(title, lines[j])
		}
	}
	if !confirmed {
		return i, false
	}

	p.annexes++
	number := fmt.Sprintf("%d", p.annexes)
	heading := strings.TrimSpace(label + " " + strings.Join(title, " "))
	p.annex = p.add(law.Provision{
		ID:      law.ProvisionID(docID, "annex", number),
		Kind:    "annex",
		Number:  number,
		Heading: strings.Join(strings.Fields(heading), " "),
	})
	p.root = p.at(p.annex).ID
	p.chapter, p.section, p.article, p.clause, p.point = -1, -1, -1, -1, -1
	return end, true
}

// endsIssuance reports whether the issuance formula closes on this line. The
// formula opens with a bracket that the sources sometimes leave a line or two
// before closing.
func endsIssuance(line string) bool {
	return strings.HasSuffix(line, ")") || strings.HasSuffix(line, "）")
}

// isStructural reports whether a line opens a provision, which is where an
// annex header has to stop.
func isStructural(line string) bool {
	return articleLine.MatchString(line) || chapterLine.MatchString(line) ||
		sectionLine.MatchString(line) || clauseLine.MatchString(line)
}

// bodyLines returns the body's non-empty lines, trimmed. The walk needs to
// look ahead from an annex label to the issuance formula that confirms it, so
// the lines are materialised rather than streamed.
func bodyLines(body string) []string {
	var out []string
	for raw := range strings.SplitSeq(body, "\n") {
		if line := strings.TrimSpace(raw); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func (p *parser) appendText(i int, text string) {
	prov := p.at(i)
	if prov == nil {
		return
	}
	if prov.Text != "" {
		prov.Text += "\n"
	}
	prov.Text += text
}

// sectionOpens reports whether a line opens a section.
//
// A section that comes before any article is the easy case and any form of the
// label opens it. A section that comes after one is the common case and was
// missed until a consolidated text showed it: "Mục 2" sits between article 7
// and article 8 of Nghị định số 72/2025/NĐ-CP, and every section after the
// first article of every document in this corpus was being read as the last
// line of whatever point preceded it.
//
// Inside an article the bar is higher. Only the bare label alone on its line,
// with the section title on the next line, opens a section, because a line that
// carries a title after the number is how the sources write a heading and also
// how a table writes a row.
func sectionOpens(line string, inArticle bool) bool {
	m := sectionLine.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	return !inArticle || strings.TrimSpace(m[2]) == ""
}

// Fragment parses a block of text as though it were part of the body of the
// document it belongs to, and returns the components it states.
//
// It exists for the temporal layer. An amending decree that says "Sửa đổi, bổ
// sung Điều 7 như sau:" quotes the whole article, clauses and points included,
// and that quotation is the article from that day on. Storing it as one blob of
// text under article 7 answers "what does point b of clause 2 say today" with
// nothing at all, so the quotation is parsed here, by the same walk that parsed
// the article it replaces, and becomes components rather than prose.
//
// A block with no structure in it yields nothing, which is the ordinary case
// for a clause or a point being replaced by a sentence.
func Fragment(docID, body string) []law.Provision { return parseBody(docID, body) }

// TrimMarker removes the number a component's own text opens with, when that
// number is the component's own.
//
// The parser never keeps it: "a) Trường hợp giá..." is stored as point a with
// text "Trường hợp giá...". Quoted replacement text carries the marker, because
// the drafter is quoting the point as it will read. Storing the quotation as it
// arrived puts a marker in front of one point in a document where no other
// point has one, and a comparison against the published consolidated text then
// reports six characters of difference as a divergence.
func TrimMarker(kind, number, text string) string {
	line, rest, _ := strings.Cut(text, "\n")
	var m []string
	switch kind {
	case "point":
		m = pointLine.FindStringSubmatch(strings.TrimSpace(line))
	case "clause":
		m = clauseLine.FindStringSubmatch(strings.TrimSpace(line))
	default:
		return text
	}
	// The marker has to name this component. A point whose text opens with a
	// different letter is quoting something, not labelling itself.
	if m == nil || law.NumberSegment(m[1]) != law.NumberSegment(number) {
		return text
	}
	return strings.TrimSpace(strings.TrimSpace(m[2]) + "\n" + rest)
}

// parseBody walks the body line by line. Chapters and sections group
// articles, articles hold clauses, clauses hold points. Text between an
// article heading and its first clause belongs to the article, and text after
// a point line belongs to that point.
//
// The signature block after the final article is not a provision, and it is
// also not the end of the document. A Vietnamese decision is often a three
// article instrument whose substance travels underneath it as an annex, so
// after the signature the walk keeps going and looks only for the next annex
// header. Everything between the two is discarded, which is what the earlier
// version did to the annex as well.
func parseBody(docID, body string) []law.Provision {
	provisions, balanced := walkBody(docID, body, true)
	if balanced {
		return provisions
	}
	// A quotation that never closes would swallow the rest of the document into
	// one provision, which is worse than reading the quoted text as structure.
	// So an unbalanced document is walked again with the rule switched off, and
	// gets exactly the parse it got before the rule existed.
	provisions, _ = walkBody(docID, body, false)
	return provisions
}

// walkBody walks the body and reports whether every quotation it opened closed
// again. quoted switches the quotation rule off for the second attempt.
func walkBody(docID, body string, quoted bool) ([]law.Provision, bool) {
	p := &parser{root: docID, annex: -1, chapter: -1, section: -1, article: -1, clause: -1, point: -1}

	lines := bodyLines(body)
	sawArticle, tail := false, false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if quoted && p.quote > 0 {
			// Inside a quotation. "Điều 73." here is the text being enacted
			// elsewhere, not an article of this instrument.
			p.appendText(p.deepest(), line)
			p.quote = quoteDepth(p.quote, line)
			continue
		}
		if sawArticle {
			if next, ok := p.openAnnex(docID, lines, i); ok {
				sawArticle, tail = false, false
				i = next
				continue
			}
			if signatureLine.MatchString(line) {
				tail = true
			}
		}
		if tail {
			continue
		}
		switch {
		case chapterLine.MatchString(line):
			m := chapterLine.FindStringSubmatch(line)
			number := law.RomanToArabic(m[1])
			p.chapter = p.add(law.Provision{
				ID:       law.ProvisionID(p.root, "chapter", number),
				ParentID: p.annexID(),
				Kind:     "chapter",
				Number:   number,
				Heading:  strings.TrimSpace(m[2]),
			})
			p.section, p.article, p.clause, p.point = -1, -1, -1, -1
		case p.chapter >= 0 && sectionOpens(line, p.article >= 0):
			m := sectionLine.FindStringSubmatch(line)
			number := law.RomanToArabic(m[1])
			p.section = p.add(law.Provision{
				ID:       law.ProvisionID(p.at(p.chapter).ID, "section", number),
				ParentID: p.at(p.chapter).ID,
				Kind:     "section",
				Number:   number,
				Heading:  strings.TrimSpace(m[2]),
			})
			p.article, p.clause, p.point = -1, -1, -1
		case articleLine.MatchString(line):
			m := articleLine.FindStringSubmatch(line)
			heading := m[2]
			if heading == "" {
				heading = m[3]
			}
			parent := p.root
			if p.section >= 0 {
				parent = p.at(p.section).ID
			} else if p.chapter >= 0 {
				parent = p.at(p.chapter).ID
			}
			p.article = p.add(law.Provision{
				ID:       law.ProvisionID(p.root, "article", m[1]),
				ParentID: parent,
				Kind:     "article",
				Number:   m[1],
				Heading:  strings.TrimSpace(heading),
			})
			p.clause, p.point = -1, -1
			sawArticle = true
		case p.article >= 0 && clauseLine.MatchString(line):
			m := clauseLine.FindStringSubmatch(line)
			p.clause = p.add(law.Provision{
				ID:       law.ProvisionID(p.at(p.article).ID, "clause", m[1]),
				ParentID: p.at(p.article).ID,
				Kind:     "clause",
				Number:   m[1],
				Text:     strings.TrimSpace(m[2]),
			})
			p.point = -1
		case p.clause >= 0 && pointLine.MatchString(line):
			m := pointLine.FindStringSubmatch(line)
			p.point = p.add(law.Provision{
				ID:       law.ProvisionID(p.at(p.clause).ID, "point", m[1]),
				ParentID: p.at(p.clause).ID,
				Kind:     "point",
				Number:   m[1],
				Text:     strings.TrimSpace(m[2]),
			})
		case p.point >= 0:
			p.appendText(p.point, line)
		case p.clause >= 0:
			p.appendText(p.clause, line)
		case p.article >= 0:
			p.appendText(p.article, line)
		case p.section >= 0 && p.article < 0:
			addHeading(p.at(p.section), line)
		case p.chapter >= 0 && p.article < 0:
			addHeading(p.at(p.chapter), line)
		}
		if quoted {
			p.quote = quoteDepth(p.quote, line)
		}
	}

	for i := range p.provisions {
		if p.provisions[i].Text != "" {
			p.provisions[i].TextHash = store.HashBytes([]byte(p.provisions[i].Text))
		}
	}
	return p.provisions, p.quote == 0
}

// quoteDepth applies one line's quotation marks to the depth carried into it.
//
// The corpus mixes the typographic pair with the ASCII mark, sometimes in the
// same quotation: the 2007 anti corruption amendment opens both of its quoted
// articles with " and closes one with ” and the other with ". So the two
// directed marks count as themselves and the ASCII mark counts as whichever
// one is due next.
func quoteDepth(depth int, line string) int {
	for _, r := range line {
		switch r {
		case '\u201c':
			depth++
		case '\u201d':
			if depth > 0 {
				depth--
			}
		case '"':
			if depth > 0 {
				depth--
			} else {
				depth++
			}
		}
	}
	return depth
}

// deepest is the provision an unstructured line belongs to: the innermost one
// the walk has open.
func (p *parser) deepest() int {
	switch {
	case p.point >= 0:
		return p.point
	case p.clause >= 0:
		return p.clause
	case p.article >= 0:
		return p.article
	case p.section >= 0:
		return p.section
	case p.chapter >= 0:
		return p.chapter
	}
	return -1
}

// Merge folds a document parsed from one dataset into the same document parsed
// from another, and returns what should be on disk.
//
// One instrument is published in more than one place and the two publications
// do not carry the same fields. UTS_VLC is cleaned text with no dates at all,
// and th1nhng0 carries the commencement date from vbpl.vn and rougher text. A
// parse that writes whichever ran last keeps one of those and throws the other
// away, and which one it keeps depends on the order the datasets were fetched
// in. The visible cost of that is a temporal layer that cannot date a single
// statement read out of the seed corpus, because the dates were on disk earlier
// in the same run and got overwritten.
//
// So the incoming parse wins every field it fills, and the existing record
// supplies every field it leaves empty. Provisions are one field for this
// purpose rather than merged clause by clause: two publications of an
// instrument are two readings of it, and interleaving them would produce a
// document neither publisher issued.
func Merge(existing, incoming *law.Document) *law.Document {
	if existing == nil {
		return incoming
	}
	if incoming == nil {
		return existing
	}
	out := *incoming
	fill := func(dst *string, src string) {
		if *dst == "" {
			*dst = src
		}
	}
	fill(&out.IssuingBody, existing.IssuingBody)
	fill(&out.Title, existing.Title)
	fill(&out.TitleEN, existing.TitleEN)
	fill(&out.DocType, existing.DocType)
	fill(&out.EffectiveFrom, existing.EffectiveFrom)
	fill(&out.SourceURL, existing.SourceURL)
	if len(out.Provisions) == 0 && len(existing.Provisions) > 0 {
		// The incoming publication has no text and the one on disk does, so the
		// text stays and the status goes back to what having text means. A
		// metadata row must not demote a document somebody already parsed.
		out.Provisions = existing.Provisions
		out.Status = existing.Status
		out.Quarantine = existing.Quarantine
		out.Source, out.SourceRef = existing.Source, existing.SourceRef
		out.SourceHash = existing.SourceHash
	}
	return &out
}
