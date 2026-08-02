package norm

import (
	"regexp"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// Resolving a sanction basis is what turns question 13 from a research project
// into a query. The question asks which prohibitions have no corresponding
// sanction anywhere in the corpus, and "anywhere" is the hard word: a
// prohibition sits in a law and the penalty for breaking it sits in a decree
// nobody reading the law would open. Until the basis is a document identifier
// the two are unconnected text.

var (
	// The number of the instrument the basis names, in the shape every
	// Vietnamese citation writes it.
	basisNumber = regexp.MustCompile(`\d+/\d{4}/[A-ZĐ][A-ZĐ0-9-]*`)
	// The component inside it. Điều is the only level a sanction basis cites in
	// practice, and khoản follows it when the drafter is being precise.
	basisArticle = regexp.MustCompile(`(?i)Điều\s+([0-9]+[a-zđ]?)`)
	basisClause  = regexp.MustCompile(`(?i)khoản\s+([0-9]+[a-zđ]?)`)
)

// SanctionCoverage is what one resolution pass managed.
//
// Unresolved is separate from external on purpose. A basis pointing outside the
// corpus is a fact about the corpus and is fixed by ingesting more documents. A
// basis nothing could parse is a fact about the extraction and is fixed by
// reading the ones it produced.
type SanctionCoverage struct {
	Sanctions  int `json:"sanctions"`
	Resolved   int `json:"resolved"`   // basis points at a document the corpus holds
	External   int `json:"external"`   // basis names an instrument the corpus does not have
	Unresolved int `json:"unresolved"` // basis names no instrument this could read
	CrossDoc   int `json:"cross_doc"`  // resolved to a document other than the one the norm is in
	Internal   int `json:"internal"`   // resolved to the norm's own document
}

// Index maps the official number of every document in the corpus to its
// identifier, which is what a legal basis has to be read through.
//
// The key is upper cased because a basis writes the instrument type in capitals
// and a document record does not always agree.
func Index(docs []*law.Document) map[string]string {
	out := make(map[string]string, len(docs))
	for _, d := range docs {
		if d.OfficialNumber == "" {
			continue
		}
		out[strings.ToUpper(d.OfficialNumber)] = d.ID
	}
	return out
}

// ResolveSanctions fills the document and component every sanction's legal
// basis points at, from the corpus index of official numbers, and reports what
// it managed.
//
// A basis that names no number resolves to the norm's own document. That is not
// a guess: "bị xử phạt theo quy định tại Điều 17" inside a decree means article
// 17 of that decree, because a Vietnamese drafter who meant another instrument
// names it. A basis that names a number the corpus does not hold is left empty
// rather than pointed anywhere, which is the same rule the citation layer
// follows.
func ResolveSanctions(records []Record, index map[string]string) SanctionCoverage {
	var cov SanctionCoverage
	for i := range records {
		r := &records[i]
		s := r.Statement.Sanction
		if s == nil {
			continue
		}
		cov.Sanctions++
		basis := s.LegalBasis
		number := basisNumber.FindString(basis)
		switch {
		case number == "":
			s.BasisDoc = r.DocID
		case index[strings.ToUpper(number)] != "":
			s.BasisDoc = index[strings.ToUpper(number)]
		default:
			s.BasisDoc = ""
		}
		if s.BasisDoc == "" {
			if number == "" {
				cov.Unresolved++
			} else {
				cov.External++
			}
			continue
		}
		cov.Resolved++
		if s.BasisDoc == r.DocID {
			cov.Internal++
		} else {
			cov.CrossDoc++
		}
		s.BasisProvison = componentOf(s.BasisDoc, basis)
	}
	return cov
}

// componentOf builds the identifier of the component a basis names, or returns
// the document identifier when the basis names the instrument alone.
//
// The clause segment is only appended under an article, because a khoản with no
// Điều beside it is a reference into the provision the sanction is already in
// and the document is the wrong parent for it.
func componentOf(docID, basis string) string {
	m := basisArticle.FindStringSubmatch(basis)
	if m == nil {
		return docID
	}
	id := law.ProvisionID(docID, "article", m[1])
	if c := basisClause.FindStringSubmatch(basis); c != nil {
		id = law.ProvisionID(id, "clause", c[1])
	}
	return id
}

// Unsanctioned returns the prohibitions of a record set that no sanction in the
// set answers, which is question 13 in one call.
//
// The match is on the concept the prohibition's action was resolved to, falling
// back to the action words. Matching on words alone would answer that a
// prohibition is unsanctioned whenever the decree words the offence differently
// from the law, which is most of the time and is exactly the failure this layer
// is supposed to remove.
func Unsanctioned(records []Record) []Record {
	sanctioned := map[string]bool{}
	for i := range records {
		s := &records[i].Statement
		if !records[i].Trusted() || s.Sanction == nil {
			continue
		}
		if s.Sanction.ConceptID != "" {
			sanctioned[s.Sanction.ConceptID] = true
		}
		sanctioned[law.Slug(s.Action.Text)] = true
	}
	var out []Record
	for i := range records {
		r := &records[i]
		if !r.Trusted() || r.Statement.Type != "prohibition" || r.Statement.Sanction != nil {
			continue
		}
		key := law.Slug(r.Statement.Action.Text)
		if r.Statement.Action.ConceptID != "" {
			key = r.Statement.Action.ConceptID
		}
		if sanctioned[key] || sanctioned[law.Slug(r.Statement.Action.Text)] {
			continue
		}
		out = append(out, *r)
	}
	return out
}
