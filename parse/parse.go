// Package parse converts raw legal documents into the canonical model.
//
// Parsing is deterministic. The same input bytes always produce the same
// provision tree and the same identifiers, and no model is involved anywhere.
// A document whose content fails a sanity check is quarantined with a reason
// instead of being silently dropped or half parsed.
package parse

import (
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
)

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
	chapter    int
	section    int
	article    int
	clause     int
	point      int
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

// parseBody walks the body line by line. Chapters and sections group
// articles, articles hold clauses, clauses hold points. Text between an
// article heading and its first clause belongs to the article, and text after
// a point line belongs to that point. The signature block after the final
// article is not a provision.
func parseBody(docID, body string) []law.Provision {
	p := &parser{chapter: -1, section: -1, article: -1, clause: -1, point: -1}

	sawArticle := false
	for raw := range strings.SplitSeq(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if sawArticle && signatureLine.MatchString(line) {
			break
		}
		switch {
		case chapterLine.MatchString(line):
			m := chapterLine.FindStringSubmatch(line)
			number := law.RomanToArabic(m[1])
			p.chapter = p.add(law.Provision{
				ID:      law.ProvisionID(docID, "chapter", number),
				Kind:    "chapter",
				Number:  number,
				Heading: strings.TrimSpace(m[2]),
			})
			p.section, p.article, p.clause, p.point = -1, -1, -1, -1
		case sectionLine.MatchString(line) && p.chapter >= 0 && p.article < 0:
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
			parent := docID
			if p.section >= 0 {
				parent = p.at(p.section).ID
			} else if p.chapter >= 0 {
				parent = p.at(p.chapter).ID
			}
			p.article = p.add(law.Provision{
				ID:       law.ProvisionID(docID, "article", m[1]),
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
		case p.section >= 0 && !sawArticle:
			if h := p.at(p.section); h.Heading == "" && line == strings.ToUpper(line) {
				h.Heading = line
			}
		case p.chapter >= 0 && !sawArticle:
			if h := p.at(p.chapter); h.Heading == "" && line == strings.ToUpper(line) {
				h.Heading = line
			}
		}
	}

	for i := range p.provisions {
		if p.provisions[i].Text != "" {
			p.provisions[i].TextHash = store.HashBytes([]byte(p.provisions[i].Text))
		}
	}
	return p.provisions
}
