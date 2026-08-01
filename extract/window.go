// Package extract runs the LLM passes over provisions.
//
// The extraction unit is one provision wrapped in a hierarchy-aware window,
// never a whole law. Prompts are assembled deterministically from the parsed
// tree, so the exact prompt for any provision can be printed without a model
// call. The registry is closed: the model may only emit known class IDs, and
// anything else becomes an ontology candidate, not a graph fact.
package extract

import (
	"fmt"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// Window is the deterministic context around one provision.
type Window struct {
	ProvisionID string
	DocTitle    string
	Path        []string // headings from chapter down to the provision
	Text        string
}

// BuildWindow assembles the window for one provision of a parsed document.
func BuildWindow(doc *law.Document, provisionID string) (*Window, error) {
	byID := map[string]*law.Provision{}
	for i := range doc.Provisions {
		byID[doc.Provisions[i].ID] = &doc.Provisions[i]
	}
	p, ok := byID[provisionID]
	if !ok {
		return nil, fmt.Errorf("provision %s not found in %s", provisionID, doc.ID)
	}
	w := &Window{ProvisionID: provisionID, DocTitle: doc.Title, Text: p.Text}
	if w.Text == "" {
		w.Text = p.Heading
	}
	var path []string
	for cur := p; cur != nil; cur = byID[cur.ParentID] {
		label := cur.Kind + " " + cur.Number
		if cur.Heading != "" {
			label += ": " + cur.Heading
		}
		path = append([]string{label}, path...)
	}
	w.Path = path
	return w, nil
}

// Prompt renders the window as the user input for a model call.
func (w *Window) Prompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Văn bản: %s\n", w.DocTitle)
	fmt.Fprintf(&b, "Vị trí: %s\n", strings.Join(w.Path, " > "))
	fmt.Fprintf(&b, "Mã điều khoản: %s\n\n", w.ProvisionID)
	fmt.Fprintf(&b, "Nội dung điều khoản:\n%s\n", w.Text)
	return b.String()
}
