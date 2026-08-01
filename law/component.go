package law

import "strings"

// Component is the identity of a structural node: this is article 94 of the
// Labour Code. TextVersion is what it says.
//
// The two are separate because they have different lifetimes. Article 94
// survives its own amendment and what it says does not, so a layer that points
// at a provision has to say which of the two it means. Norms, terms and
// relations all point at the text, because a norm stated by the 2021 wording is
// not stated by the 2019 wording. Citations point at the component, because a
// later law that amends article 94 amends the article rather than one of its
// wordings.
type Component struct {
	ID       string `json:"id"`
	DocID    string `json:"doc_id"`
	ParentID string `json:"parent_id,omitempty"`
	Kind     string `json:"kind"` // annex, chapter, section, article, clause, point
	Number   string `json:"number"`
	Heading  string `json:"heading,omitempty"`
	Position int    `json:"position"`
	// RenumberedFrom is the number this component carried when its identifier
	// was minted, set only once a later instrument has moved it. The identifier
	// never moves with it: a component that was article 94 and is now article
	// 95 keeps the identifier article-94, and this field is how a reader learns
	// that the two disagree. Nothing in the corpus populates it yet, because
	// reading amendment instructions is a later milestone, but the field exists
	// now because every identifier minted before it does would otherwise have
	// to be rewritten afterwards.
	RenumberedFrom string `json:"renumbered_from,omitempty"`
}

// TextVersion is what a component said over an interval.
//
// FromDate and ToDate are the interval the text was in force. ToDate is empty
// while the version is current. A component with no amendment history has
// exactly one version, open ended, starting on the day the document took
// effect.
type TextVersion struct {
	ID          string `json:"id"`
	ComponentID string `json:"component_id"`
	DocID       string `json:"doc_id"`
	Text        string `json:"text"`
	TextHash    string `json:"text_hash"`
	FromDate    string `json:"from_date,omitempty"`
	ToDate      string `json:"to_date,omitempty"`
}

// ProvisionAlias is the old label for a component, and ProvisionAliasUntil is
// the last release that carries it.
//
// Splitting Provision into Component and TextVersion renames a node type that
// queries, saved Cypher and the M1 through M5 notes all refer to by the old
// name. Carrying the old name as a second label on the same node costs nothing
// and keeps those queries running. It is a label rather than a copied node
// because a copy would disagree with the original the first time a component is
// renumbered. It goes away in the release after the one named here, at which
// point a query that says Provision has to say Component.
const (
	ProvisionAlias      = "Provision"
	ProvisionAliasUntil = "v0.1.0"
)

// versionIDLength is how much of the text hash goes into a version identifier.
// Twelve hex characters is 48 bits, which is far more than enough to separate
// the handful of wordings one component ever has, and short enough that the
// identifier stays readable in a query result.
const versionIDLength = 12

// TextVersionID mints the identifier of one wording of one component. It is
// derived from the content rather than counted, so re-running the pipeline on
// the same text produces the same identifier and re-running it on amended text
// produces a different one without anything having to remember how many
// versions came before.
func TextVersionID(componentID, textHash string) string {
	if textHash == "" {
		return componentID + ":text"
	}
	if len(textHash) > versionIDLength {
		textHash = textHash[:versionIDLength]
	}
	return componentID + ":text-" + textHash
}

// Component returns the identity half of a provision.
func (p *Provision) Component(docID string) Component {
	return Component{
		ID:       p.ID,
		DocID:    docID,
		ParentID: p.ParentID,
		Kind:     p.Kind,
		Number:   p.Number,
		Heading:  p.Heading,
		Position: p.Position,
	}
}

// Renumber records that a component now reads under a different number. The
// identifier does not move, because everything that points at this component
// points at the identifier and rewriting it would break every pointer to
// preserve a label.
//
// The first number is the one kept in RenumberedFrom. A component moved twice
// was minted under its first number and that is what its identifier says, so
// recording the second move would lose the only number the identifier agrees
// with.
func (c *Component) Renumber(number string) {
	if number == c.Number {
		return
	}
	if c.RenumberedFrom == "" {
		c.RenumberedFrom = c.Number
	}
	c.Number = number
}

// Split separates a document's provisions into components and text versions.
//
// A provision with no text yields no version rather than an empty one. A
// chapter is a real component with a heading and no text of its own, and giving
// it a version carrying the empty string would claim it said nothing when it
// says nothing here by design.
func Split(doc *Document) ([]Component, []TextVersion) {
	components := make([]Component, 0, len(doc.Provisions))
	var versions []TextVersion
	for i := range doc.Provisions {
		p := &doc.Provisions[i]
		components = append(components, p.Component(doc.ID))
		if strings.TrimSpace(p.Text) == "" {
			continue
		}
		versions = append(versions, TextVersion{
			ID:          TextVersionID(p.ID, p.TextHash),
			ComponentID: p.ID,
			DocID:       doc.ID,
			Text:        p.Text,
			TextHash:    p.TextHash,
			FromDate:    doc.EffectiveFrom,
		})
	}
	return components, versions
}
