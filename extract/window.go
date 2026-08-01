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
//
// Text is the provision together with its points, and it is the only text a
// model may quote from: a clause that opens an enumeration does not mean
// anything without the items, and an evidence quote taken from a point has to
// validate against something. Lead is the lead-in text of the ancestors, which
// is there to be read and never to be quoted.
type Window struct {
	ProvisionID string
	DocTitle    string
	Path        []string // headings from chapter down to the provision
	Lead        string
	Text        string
}

// BuildWindow assembles the window for one provision of a parsed document.
func BuildWindow(doc *law.Document, provisionID string) (*Window, error) {
	byID := map[string]*law.Provision{}
	children := map[string][]*law.Provision{}
	for i := range doc.Provisions {
		p := &doc.Provisions[i]
		byID[p.ID] = p
		children[p.ParentID] = append(children[p.ParentID], p)
	}
	p, ok := byID[provisionID]
	if !ok {
		return nil, fmt.Errorf("provision %s not found in %s", provisionID, doc.ID)
	}
	w := &Window{ProvisionID: provisionID, DocTitle: doc.Title}

	var body []string
	if p.Text != "" {
		body = append(body, p.Text)
	} else {
		body = append(body, p.Heading)
	}
	appendDescendants(children, p, &body)
	w.Text = strings.TrimRight(strings.Join(body, "\n"), "\n")

	var path, lead []string
	for cur := p; cur != nil; cur = byID[cur.ParentID] {
		label := cur.Kind + " " + cur.Number
		if cur.Heading != "" {
			label += ": " + cur.Heading
		}
		path = append([]string{label}, path...)
		if cur != p && cur.Text != "" {
			lead = append([]string{cur.Text}, lead...)
		}
	}
	w.Path = path
	w.Lead = strings.Join(lead, "\n")
	return w, nil
}

// appendDescendants walks the subtree in document order, prefixing each item
// with its own numbering so the model sees the enumeration the way the law
// prints it.
func appendDescendants(children map[string][]*law.Provision, p *law.Provision, body *[]string) {
	for _, c := range children[p.ID] {
		if c.Text != "" {
			*body = append(*body, c.Number+") "+c.Text)
		}
		appendDescendants(children, c, body)
	}
}

// Prompt renders the window as the user input for a model call.
func (w *Window) Prompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Văn bản: %s\n", w.DocTitle)
	fmt.Fprintf(&b, "Vị trí: %s\n", strings.Join(w.Path, " > "))
	fmt.Fprintf(&b, "Mã điều khoản: %s\n\n", w.ProvisionID)
	if w.Lead != "" {
		fmt.Fprintf(&b, "Dẫn nhập của điều, chỉ để đọc hiểu, không được trích dẫn:\n%s\n\n", w.Lead)
	}
	fmt.Fprintf(&b, "Nội dung điều khoản:\n%s\n", w.Text)
	return b.String()
}
