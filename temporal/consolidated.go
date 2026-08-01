package temporal

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// The consolidated text is free ground truth and it is the only check in this
// package that can prove the version graph wrong rather than merely
// inconsistent.
//
// A văn bản hợp nhất is the office of the drafter publishing the amended
// instrument as one document with every amendment already applied. That is
// exactly what the version graph computes. When both exist for the same date,
// they must agree, and every place they disagree is a component this layer got
// wrong: an amendment applied to the wrong clause, a phrase edit that hit too
// much, an insertion appended to the end instead of after the sibling it named.
//
// Nothing about this check is clever. It is worth more than the rest of the
// verification in the package because it does not depend on the reading being
// right.

// IsConsolidated reports whether a document is a consolidated text. Both signals
// are used because the corpus has instruments whose number carries VBHN and
// whose title does not, and the reverse.
func IsConsolidated(d *law.Document) bool {
	if d == nil {
		return false
	}
	if strings.Contains(strings.ToUpper(d.OfficialNumber), "VBHN") {
		return true
	}
	return strings.Contains(strings.ToLower(d.Title), "hợp nhất")
}

// Divergence is one component where the computed text and the published
// consolidated text disagree.
type Divergence struct {
	Path      string `json:"path"` // article-15:clause-2, as both documents state it
	Reason    string `json:"reason"`
	Computed  string `json:"computed,omitempty"`
	Published string `json:"published,omitempty"`
}

// Reasons a component diverges.
const (
	DivergeMissingInPublished = "computed a component the consolidated text does not have"
	DivergeMissingInComputed  = "the consolidated text has a component the version graph does not"
	DivergeTextDiffers        = "both have the component and the text differs"
)

// Match is the result of one consolidated text comparison.
type Match struct {
	DocID        string       `json:"doc_id"`       // the instrument being checked
	ConsolidedID string       `json:"consolidated"` // the published consolidated text
	Date         string       `json:"date"`         // the date the computation was made at
	Compared     int          `json:"compared"`     // components present in both
	Agreed       int          `json:"agreed"`       // components whose text matched
	Divergences  []Divergence `json:"divergences,omitempty"`
}

// Rate is the share of compared components that matched. It is reported rather
// than asserted against a threshold, because a threshold here would turn a
// number that means something into a boolean that does not.
func (m Match) Rate() float64 {
	if m.Compared == 0 {
		return 0
	}
	return float64(m.Agreed) / float64(m.Compared)
}

func (m Match) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s against %s at %s: %d components compared, %d agreed",
		m.DocID, m.ConsolidedID, m.Date, m.Compared, m.Agreed)
	if m.Compared > 0 {
		fmt.Fprintf(&b, " (%.1f%%)", m.Rate()*100)
	}
	b.WriteString("\n")
	for _, d := range m.Divergences {
		fmt.Fprintf(&b, "  %-40s %s\n", d.Path, d.Reason)
	}
	return b.String()
}

// Compare checks the computed text of a document at a date against a published
// consolidated text of it.
//
// Components are matched by structural path rather than by identifier, because
// the consolidated text is a different document with its own identifiers and the
// path is the only thing the two share.
func Compare(v *View, docID string, consolidated *law.Document, date string) Match {
	m := Match{DocID: docID, ConsolidedID: consolidated.ID, Date: date}

	computed := map[string]string{}
	for _, ver := range v.InForceAt(docID, date) {
		path := strings.TrimPrefix(strings.TrimPrefix(ver.ComponentID, docID), ":")
		if path == "" || len(ver.Children) > 0 {
			// A component with children is compared through its children. Its own
			// text is the heading, and a heading that differs is not an amendment
			// anybody made.
			continue
		}
		computed[path] = normalize(ver.Text)
	}

	published := map[string]string{}
	for i := range consolidated.Provisions {
		p := &consolidated.Provisions[i]
		if hasChildren(consolidated, p.ID) {
			continue
		}
		path := strings.TrimPrefix(strings.TrimPrefix(p.ID, consolidated.ID), ":")
		if path == "" {
			continue
		}
		published[path] = normalize(p.Text)
	}

	for _, path := range union(computed, published) {
		c, inComputed := computed[path]
		p, inPublished := published[path]
		switch {
		case inComputed && inPublished && c == p:
			m.Compared++
			m.Agreed++
		case inComputed && inPublished:
			m.Compared++
			m.Divergences = append(m.Divergences, Divergence{
				Path: path, Reason: DivergeTextDiffers, Computed: c, Published: p,
			})
		case inComputed:
			m.Divergences = append(m.Divergences, Divergence{
				Path: path, Reason: DivergeMissingInPublished, Computed: c,
			})
		default:
			m.Divergences = append(m.Divergences, Divergence{
				Path: path, Reason: DivergeMissingInComputed, Published: p,
			})
		}
	}
	return m
}

func hasChildren(d *law.Document, id string) bool {
	for i := range d.Provisions {
		if d.Provisions[i].ParentID == id {
			return true
		}
	}
	return false
}

func union(a, b map[string]string) []string {
	seen := map[string]bool{}
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// normalize collapses whitespace so a comparison is about words rather than
// about how two publishers wrapped their lines. It stops there: punctuation and
// diacritics are content, and folding them would hide the phrase edits this
// check exists to catch.
func normalize(s string) string { return strings.Join(strings.Fields(s), " ") }
