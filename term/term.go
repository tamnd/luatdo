// Package term extracts defined terms from interpretation articles.
//
// Most Vietnamese laws carry a dedicated "Giải thích từ ngữ" article whose
// clauses each define one term, and the definitional formula is regular enough
// that no model is needed. Extraction here is deterministic, and a clause that
// does not match the formula is simply not a definition, never a guess.
package term

import (
	"regexp"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// Definition is one term defined by one provision, scoped to its document.
type Definition struct {
	TermID      string `json:"term_id"` // vn:term:<slug>, shared across documents
	Term        string `json:"term"`
	DocID       string `json:"doc_id"`
	ProvisionID string `json:"provision_id"`
	Text        string `json:"text"`       // the full defining clause text
	Connective  string `json:"connective"` // la or bao-gom
}

var (
	interpretationHeading = regexp.MustCompile(`(?i)giải thích từ ngữ`)
	// The definitional formula: "<term> là <definition>" or the enumerating
	// "<term> bao gồm ...", where the enumeration often opens with a colon
	// directly after the connective. (?s) lets the term run across the line
	// break that appears when the source put the number, the term, and the
	// definition on separate lines. The term is capped because a long prefix
	// before "là" is a sentence, not a term.
	definitionPattern = regexp.MustCompile(`(?s)^(.{1,120}?)\s+(là|bao gồm):?\s+`)
)

// TermID returns the corpus-wide identifier of a term surface form.
func TermID(termText string) string {
	return "vn:term:" + law.Slug(termText)
}

// Extract returns every definition found in the document's interpretation
// articles. Clauses are matched against the definitional formula; when an
// interpretation article has no parsed clauses, its own text is scanned as a
// fallback so coarse parses still yield their terms.
func Extract(doc *law.Document) []Definition {
	articles := map[string]bool{}
	for i := range doc.Provisions {
		p := &doc.Provisions[i]
		if p.Kind == "article" && interpretationHeading.MatchString(p.Heading) {
			articles[p.ID] = true
		}
	}
	if len(articles) == 0 {
		return nil
	}

	var defs []Definition
	for i := range doc.Provisions {
		p := &doc.Provisions[i]
		if p.Kind != "clause" || !articles[p.ParentID] {
			continue
		}
		if d, ok := fromClause(doc.ID, p); ok {
			defs = append(defs, d)
		}
	}
	return defs
}

func fromClause(docID string, p *law.Provision) (Definition, bool) {
	m := definitionPattern.FindStringSubmatch(p.Text)
	if m == nil {
		return Definition{}, false
	}
	termText := normalizeTerm(m[1])
	if termText == "" || law.Slug(termText) == "" {
		return Definition{}, false
	}
	connective := "la"
	if m[2] == "bao gồm" {
		connective = "bao-gom"
	}
	return Definition{
		TermID:      TermID(termText),
		Term:        termText,
		DocID:       docID,
		ProvisionID: p.ID,
		Text:        p.Text,
		Connective:  connective,
	}, true
}

// normalizeTerm collapses whitespace and strips the quoting and emphasis
// punctuation that some sources wrap around the defined phrase.
func normalizeTerm(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.Trim(s, `"'“”*_ `)
}
