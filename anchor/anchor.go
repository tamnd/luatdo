// Package anchor locates where definitions live and hands back exact spans.
//
// Anchoring is grammar only and it decides nothing about meaning. It finds the
// definitions article of a document, splits it into definition units, harvests
// the alias declarations the drafter published, and reports what it did not
// find. What each definition denotes is a reading task and belongs to the
// concept pass, which consumes the units this package emits.
//
// The rule this package is built on: anything a grammar can get exactly is
// taken by code, and nothing else is. Article headings, clause boundaries,
// scoping formulas and declared short forms are exact. The phrase a short form
// abbreviates is not, so it is emitted as a candidate and labelled as one.
package anchor

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// Scope is where a set of definitions applies. It is the document itself for a
// law or a decree, and the annex for a regulation issued under a decision. The
// difference is load bearing: a term defined in a Quy chế is scoped to that Quy
// chế, and flattening it onto the parent decision would claim a definition the
// decision never made.
type Scope struct {
	ID         string `json:"id"`         // document or annex identifier
	Kind       string `json:"kind"`       // document or annex
	Instrument string `json:"instrument"` // the instrument the formula names, verbatim
	Formula    string `json:"formula"`    // the scoping sentence, verbatim, empty when absent
	FoundBy    string `json:"found_by"`   // heading, formula, or both
}

// Unit is one definition clause, ready for a reading pass.
//
// The unit is the whole clause. This package does not split the defined term
// from its definition, because that split is the reading and a substring taken
// before the connective is a substring, not a term.
type Unit struct {
	ID        string `json:"id"`         // the clause identifier, or the article's when undivided
	DocID     string `json:"doc_id"`     // the document that carries the text
	ScopeID   string `json:"scope_id"`   // the instrument the definition is scoped to
	ArticleID string `json:"article_id"` // the definitions article
	Number    string `json:"number"`     // clause number as written
	Text      string `json:"text"`
	TextHash  string `json:"text_hash"` // pins the unit to one version of the text
}

// Alias is an alias declaration published by the drafter, such as
// "Ủy ban nhân dân tỉnh, thành phố trực thuộc trung ương (sau đây gọi chung là
// Ủy ban nhân dân cấp tỉnh)". They occur throughout preambles and operative
// provisions, not only in definitions articles, so they are harvested corpus
// wide.
type Alias struct {
	DocID       string `json:"doc_id"`
	ProvisionID string `json:"provision_id"`
	Short       string `json:"short"` // the declared alias, exact
	// LongCandidate is the phrase the alias stands for. Its right edge is
	// exact and its left edge is a guess, because Vietnamese drafting puts no
	// marker at the start of the abbreviated phrase: in "là việc Ngân hàng Nhà
	// nước Việt Nam (sau đây gọi là Ngân hàng Nhà nước)" the phrase begins
	// three words after the segment does. It is a candidate for a reading pass
	// to confirm, never a conclusion.
	LongCandidate string `json:"long_candidate"`
	Form          string `json:"form"`  // goi-tat, goi-chung, or goi
	Quote         string `json:"quote"` // the declaration itself, verbatim
	CharStart     int    `json:"char_start"`
	CharEnd       int    `json:"char_end"`
}

// Result is what one document yielded.
type Result struct {
	DocID   string  `json:"doc_id"`
	DocType string  `json:"doc_type"`
	Scopes  []Scope `json:"scopes,omitempty"`
	Units   []Unit  `json:"units,omitempty"`
	Aliases []Alias `json:"aliases,omitempty"`
	// Residue names what was looked for and not found, so that a document
	// without definitions is a stated fact rather than an empty file. It is
	// empty when at least one scope was found.
	Residue string `json:"residue,omitempty"`
}

var (
	// The heading of a definitions article. Both the standard "từ ngữ" and the
	// "thuật ngữ" some technical circulars use, and the trailing "trong Thông
	// tư này" that a few headings carry.
	definitionsHeading = regexp.MustCompile(`(?i)^giải thích\s+(?:từ ngữ|thuật ngữ)`)
	// The scoping formula, in its two orders. "Trong Luật này, các từ ngữ dưới
	// đây được hiểu như sau:" is by far the most common, and the pattern is
	// loose about the punctuation and the determiner because the corpus varies
	// on both and is sometimes truncated mid sentence.
	// Note the absence of \b after the Vietnamese words. Go's \b is an ASCII
	// word boundary, so it never matches after a letter like ữ, and a pattern
	// written with one here would silently fail on the commonest wording in the
	// corpus while still looking correct.
	scopeFormula    = regexp.MustCompile(`(?i)^trong\s+(bộ luật|luật|pháp lệnh|nghị định|nghị quyết|thông tư|thông tư liên tịch|quyết định|quy chế|quy định|quy trình|điều lệ|chỉ thị|văn bản|hướng dẫn|kế hoạch|đề án|chương trình)\s+này\s*[,;]?\s*(?:các|những|một số)?\s*(?:từ ngữ|thuật ngữ)(?:\s[^\n]*?)?(?:được hiểu|hiểu như sau)`)
	scopeFormulaAlt = regexp.MustCompile(`(?i)^(?:các|những)\s+(?:từ ngữ|thuật ngữ)\s+(?:trong|tại|của)\s+(bộ luật|luật|pháp lệnh|nghị định|nghị quyết|thông tư|thông tư liên tịch|quyết định|quy chế|quy định|quy trình|điều lệ|chỉ thị|văn bản|hướng dẫn|kế hoạch|đề án|chương trình)\s+này(?:\s[^\n]*?)?(?:được hiểu|hiểu như sau)`)
	// The alias declaration. The three published forms, and nothing else: an
	// alias is a drafting act with a fixed phrase, so a looser pattern would
	// only add guesses.
	aliasDeclaration = regexp.MustCompile(`sau đây\s+gọi\s+(tắt là|chung là|là)\s+`)
)

// aliasForms maps the declaration's own wording onto the recorded form.
var aliasForms = map[string]string{
	"tắt là":   "goi-tat",
	"chung là": "goi-chung",
	"là":       "goi",
}

// subInstruments are the instruments that travel as an annex to a decision
// rather than as documents of their own. A scoping formula naming one of these
// scopes its definitions to the annex, which is why the formula is read for
// which instrument it names rather than only for whether it matched.
var subInstruments = map[string]bool{
	"quy chế":      true,
	"quy định":     true,
	"quy trình":    true,
	"điều lệ":      true,
	"hướng dẫn":    true,
	"kế hoạch":     true,
	"đề án":        true,
	"chương trình": true,
}

// Anchor runs the whole grammar pass over one document.
func Anchor(doc *law.Document) Result {
	r := Result{DocID: doc.ID, DocType: doc.DocType}
	if doc.Status != "parsed" || len(doc.Provisions) == 0 {
		r.Residue = "no provision text: document is " + doc.Status
		return r
	}

	index := newIndex(doc)
	for _, article := range index.definitionArticles() {
		scope, units := index.anchorArticle(doc, article)
		r.Scopes = append(r.Scopes, scope)
		r.Units = append(r.Units, units...)
	}
	r.Aliases = Aliases(doc)

	if len(r.Scopes) == 0 {
		r.Residue = "no definitions article: no heading matching Giải thích từ ngữ and no scoping formula"
	} else if len(r.Units) == 0 {
		r.Residue = "definitions article found with no clauses to split"
	}
	return r
}

// index is one document's provision tree in the shapes this pass needs.
type index struct {
	provisions []law.Provision
	byID       map[string]int
	children   map[string][]int
	// annexOf maps a provision identifier to the annex that contains it, empty
	// for the provisions of the parent instrument.
	annexOf map[string]string
}

func newIndex(doc *law.Document) *index {
	ix := &index{
		provisions: doc.Provisions,
		byID:       make(map[string]int, len(doc.Provisions)),
		children:   map[string][]int{},
		annexOf:    map[string]string{},
	}
	for i := range doc.Provisions {
		p := &doc.Provisions[i]
		ix.byID[p.ID] = i
		ix.children[p.ParentID] = append(ix.children[p.ParentID], i)
	}
	for i := range doc.Provisions {
		p := &doc.Provisions[i]
		if p.Kind == "annex" {
			ix.annexOf[p.ID] = p.ID
			continue
		}
		if a := ix.annexOf[p.ParentID]; a != "" {
			ix.annexOf[p.ID] = a
		}
	}
	return ix
}

// definitionArticles returns the articles that open a definitions block, in
// document order. An article qualifies on its heading or on the scoping
// formula, and the formula is looked for in the article's own text and in its
// first clause, because drafters put it in either.
func (ix *index) definitionArticles() []int {
	var out []int
	for i := range ix.provisions {
		p := &ix.provisions[i]
		if p.Kind != "article" {
			continue
		}
		if definitionsHeading.MatchString(strings.TrimSpace(p.Heading)) {
			out = append(out, i)
			continue
		}
		if _, _, ok := ix.formulaOf(i); ok {
			out = append(out, i)
		}
	}
	return out
}

// formulaOf finds the scoping sentence of an article, returning the instrument
// it names and the sentence verbatim.
func (ix *index) formulaOf(article int) (instrument, formula string, ok bool) {
	texts := []string{ix.provisions[article].Text}
	if kids := ix.children[ix.provisions[article].ID]; len(kids) > 0 {
		texts = append(texts, ix.provisions[kids[0]].Text)
	}
	for _, text := range texts {
		for line := range strings.SplitSeq(text, "\n") {
			line = strings.TrimSpace(line)
			for _, re := range []*regexp.Regexp{scopeFormula, scopeFormulaAlt} {
				if m := re.FindStringSubmatch(line); m != nil {
					return strings.ToLower(m[1]), line, true
				}
			}
		}
	}
	return "", "", false
}

// anchorArticle builds the scope of one definitions article and splits it.
func (ix *index) anchorArticle(doc *law.Document, article int) (Scope, []Unit) {
	p := &ix.provisions[article]
	instrument, formula, hasFormula := ix.formulaOf(article)
	scope := Scope{
		ID:         doc.ID,
		Kind:       "document",
		Instrument: instrument,
		Formula:    formula,
		FoundBy:    "formula",
	}
	if definitionsHeading.MatchString(strings.TrimSpace(p.Heading)) {
		scope.FoundBy = "heading"
		if hasFormula {
			scope.FoundBy = "both"
		}
	}
	// The scope is the annex when the article sits inside one, and also when
	// the formula names a sub instrument, which is how a Quy chế announces
	// itself even where the annex boundary was not parsed.
	if annex := ix.annexOf[p.ID]; annex != "" {
		scope.ID, scope.Kind = annex, "annex"
	} else if subInstruments[instrument] {
		scope.ID = p.ID + ":scope"
		scope.Kind = "annex"
	}

	units := ix.split(doc, p, scope)
	return scope, units
}

// split turns the clauses of a definitions article into units. An article that
// was never divided into clauses is one unit of its own, because a short
// definitions article is sometimes written as a single run of sentences and
// dropping it would lose every term in it.
func (ix *index) split(doc *law.Document, article *law.Provision, scope Scope) []Unit {
	var units []Unit
	for _, i := range ix.children[article.ID] {
		c := &ix.provisions[i]
		if c.Kind != "clause" || strings.TrimSpace(c.Text) == "" {
			continue
		}
		units = append(units, Unit{
			ID:        c.ID,
			DocID:     doc.ID,
			ScopeID:   scope.ID,
			ArticleID: article.ID,
			Number:    c.Number,
			Text:      c.Text,
			TextHash:  c.TextHash,
		})
	}
	if len(units) == 0 && strings.TrimSpace(article.Text) != "" {
		units = append(units, Unit{
			ID:        article.ID,
			DocID:     doc.ID,
			ScopeID:   scope.ID,
			ArticleID: article.ID,
			Number:    article.Number,
			Text:      article.Text,
			TextHash:  article.TextHash,
		})
	}
	return units
}

// Aliases harvests every alias declaration in a document, in provision order.
func Aliases(doc *law.Document) []Alias {
	var out []Alias
	for i := range doc.Provisions {
		p := &doc.Provisions[i]
		out = append(out, aliasesIn(doc.ID, p)...)
	}
	return out
}

// aliasesIn reads one provision. Offsets are byte offsets into the provision
// text as stored, so a quote can be verified against the same bytes the hash
// was taken over.
func aliasesIn(docID string, p *law.Provision) []Alias {
	var out []Alias
	for _, loc := range aliasDeclaration.FindAllStringSubmatchIndex(p.Text, -1) {
		form := aliasForms[p.Text[loc[2]:loc[3]]]
		short, end := aliasShort(p.Text, loc[1])
		if short == "" {
			continue
		}
		out = append(out, Alias{
			DocID:         docID,
			ProvisionID:   p.ID,
			Short:         short,
			LongCandidate: aliasLong(p.Text[:loc[0]]),
			Form:          form,
			Quote:         p.Text[loc[0]:end],
			CharStart:     loc[0],
			CharEnd:       end,
		})
	}
	return out
}

// aliasCloser ends the declared short form. The alias runs to the end of the
// parenthesis it is usually declared in, or to the end of the sentence or list
// item when it is declared inline.
const aliasCloser = ")\n;.:"

// aliasShort reads the short form that starts at from, and returns it with the
// offset just past it.
func aliasShort(text string, from int) (string, int) {
	end := len(text)
	if i := strings.IndexAny(text[from:], aliasCloser); i >= 0 {
		end = from + i
	}
	short := strings.TrimSpace(text[from:end])
	// A short form is a name, so a run that long is a sentence the declaration
	// opened rather than the alias itself, and taking it would be a guess.
	if short == "" || len([]rune(short)) > 120 || law.Slug(short) == "" {
		return "", 0
	}
	return short, end
}

// aliasLong takes the phrase before the declaration back to the nearest
// boundary. Commas are not boundaries: "Ủy ban nhân dân tỉnh, thành phố trực
// thuộc trung ương" is one phrase and cutting at its comma would halve it.
func aliasLong(before string) string {
	before = strings.TrimRight(before, " (")
	if i := strings.LastIndexAny(before, "\n;:."); i >= 0 {
		before = before[i+1:]
	}
	before = strings.TrimSpace(strings.Join(strings.Fields(before), " "))
	if r := []rune(before); len(r) > 200 {
		before = strings.TrimSpace(string(r[len(r)-200:]))
	}
	return before
}

// SummaryFile and ResidueFile are what a corpus wide pass writes next to the
// per document results. The residue is a plain list of identifiers rather than
// prose, because a residue described is a residue nobody can check.
const (
	SummaryFile = "summary.json"
	ResidueFile = "unanchored.txt"
)

// Summary counts one anchoring pass over a corpus, split the way the decisions
// that follow it are made: by instrument type and by issuing body.
type Summary struct {
	Documents   int             `json:"documents"`
	WithContent int             `json:"with_content"`
	Anchored    int             `json:"anchored"`
	Scopes      int             `json:"scopes"`
	Units       int             `json:"units"`
	Aliases     int             `json:"aliases"`
	AnnexScoped int             `json:"annex_scoped"`
	FoundBy     map[string]int  `json:"found_by"`
	ByDocType   map[string]Pair `json:"by_doc_type"`
	ByBody      map[string]Pair `json:"by_issuing_body"`
	// Unanchored lists the documents that have text and no definitions
	// article, identifier by identifier. A residue described is a residue
	// nobody can check.
	//
	// The list is not serialised with the summary. It runs to a hundred
	// thousand identifiers, it is written whole to the residue file, and a
	// second copy inside the summary would only make every reader of the
	// counts load four megabytes of it. The count is the summary's business
	// and the list is the residue file's.
	Unanchored      []string `json:"-"`
	UnanchoredCount int      `json:"unanchored"`
}

// Pair is a with content and anchored count for one slice of the corpus.
type Pair struct {
	WithContent int `json:"with_content"`
	Anchored    int `json:"anchored"`
}

// NewSummary returns an empty summary with its maps ready.
func NewSummary() *Summary {
	return &Summary{FoundBy: map[string]int{}, ByDocType: map[string]Pair{}, ByBody: map[string]Pair{}}
}

// Add folds one document's result into the summary.
func (s *Summary) Add(doc *law.Document, r Result) {
	s.Documents++
	docType := strings.ToLower(strings.TrimSpace(doc.DocType))
	if docType == "" {
		docType = "unknown"
	}
	body := strings.TrimSpace(doc.IssuingBody)
	if body == "" {
		body = "unknown"
	}
	if doc.Status != "parsed" || len(doc.Provisions) == 0 {
		return
	}

	s.WithContent++
	s.Units += len(r.Units)
	s.Aliases += len(r.Aliases)
	anchored := len(r.Scopes) > 0
	for _, sc := range r.Scopes {
		s.Scopes++
		s.FoundBy[sc.FoundBy]++
		if sc.Kind == "annex" {
			s.AnnexScoped++
		}
	}
	if !anchored {
		s.Unanchored = append(s.Unanchored, doc.ID)
		s.UnanchoredCount++
	}
	s.ByDocType[docType] = bump(s.ByDocType[docType], anchored)
	s.ByBody[body] = bump(s.ByBody[body], anchored)
	if anchored {
		s.Anchored++
	}
}

func bump(p Pair, anchored bool) Pair {
	p.WithContent++
	if anchored {
		p.Anchored++
	}
	return p
}

func (s *Summary) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "documents     %d\n", s.Documents)
	fmt.Fprintf(&b, "with text     %d %s\n", s.WithContent, percent(s.WithContent, s.Documents))
	fmt.Fprintf(&b, "anchored      %d %s of the documents with text\n", s.Anchored, percent(s.Anchored, s.WithContent))
	fmt.Fprintf(&b, "units         %d definition clauses\n", s.Units)
	fmt.Fprintf(&b, "aliases       %d declarations\n", s.Aliases)
	fmt.Fprintf(&b, "found by      %d heading only, %d formula only, %d both\n",
		s.FoundBy["heading"], s.FoundBy["formula"], s.FoundBy["both"])
	fmt.Fprintf(&b, "annex scoped  %d of %d scopes\n", s.AnnexScoped, s.Scopes)
	fmt.Fprintf(&b, "unanchored    %d documents with text and no definitions article\n", s.UnanchoredCount)
	section(&b, "by instrument type", s.ByDocType)
	section(&b, "by issuing body", s.ByBody)
	return strings.TrimRight(b.String(), "\n")
}

// section prints one breakdown, largest slice first, so the head of the corpus
// is visible without reading the whole table.
func section(b *strings.Builder, title string, m map[string]Pair) {
	if len(m) == 0 {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]].WithContent != m[keys[j]].WithContent {
			return m[keys[i]].WithContent > m[keys[j]].WithContent
		}
		return keys[i] < keys[j]
	})
	fmt.Fprintf(b, "%s\n", title)
	for _, k := range keys {
		p := m[k]
		fmt.Fprintf(b, "  %-40s %d of %d %s\n", k, p.Anchored, p.WithContent, percent(p.Anchored, p.WithContent))
	}
}

func percent(n, of int) string {
	if of == 0 {
		return ""
	}
	return fmt.Sprintf("(%d%%)", n*100/of)
}
