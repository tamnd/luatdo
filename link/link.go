// Package link resolves extracted mentions against the registry and the
// defined-term table.
//
// Resolution is scored and never a silent model choice. A mention that no
// alias or definition explains stays unresolved with its role recorded, which
// is correct; a fabricated target would be a bug.
package link

import (
	"github.com/tamnd/luatdo/extract"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/term"
)

// Resolution is one linked or unresolved mention.
type Resolution struct {
	ProvisionID string  `json:"provision_id"`
	DocID       string  `json:"doc_id"`
	Text        string  `json:"text"`
	ClassID     string  `json:"class_id,omitempty"`
	TargetKind  string  `json:"target_kind"` // class, term, or unresolved
	TargetID    string  `json:"target_id,omitempty"`
	Score       float64 `json:"score"`
	Basis       string  `json:"basis,omitempty"` // label, alias, term-same-doc, term-corpus
	Role        string  `json:"role,omitempty"`  // for unresolved mentions
}

// Linker holds the alias tables built once per run.
type Linker struct {
	classBySlug map[string]classHit
	termDocs    map[string]map[string]bool // term slug -> doc IDs defining it
}

type classHit struct {
	id    string
	basis string
}

// New builds the linker from the registry and the corpus definition table.
func New(reg *ontology.Registry, defs []term.Definition) *Linker {
	l := &Linker{classBySlug: map[string]classHit{}, termDocs: map[string]map[string]bool{}}
	for _, c := range reg.Classes {
		if s := law.Slug(c.LabelVI); s != "" {
			l.classBySlug[s] = classHit{id: c.ID, basis: "label"}
		}
	}
	for _, c := range reg.Classes {
		for _, a := range c.Aliases {
			s := law.Slug(a)
			if s == "" {
				continue
			}
			if _, taken := l.classBySlug[s]; !taken {
				l.classBySlug[s] = classHit{id: c.ID, basis: "alias"}
			}
		}
	}
	for _, d := range defs {
		s := law.Slug(d.Term)
		if l.termDocs[s] == nil {
			l.termDocs[s] = map[string]bool{}
		}
		l.termDocs[s][d.DocID] = true
	}
	return l
}

// Resolve links every mention of one extraction job.
func (l *Linker) Resolve(job *extract.Job) []Resolution {
	var out []Resolution
	for _, m := range job.Mentions {
		r := Resolution{
			ProvisionID: job.ProvisionID,
			DocID:       job.DocID,
			Text:        m.Text,
			ClassID:     m.ClassID,
		}
		slug := law.Slug(m.Text)
		switch {
		case l.termDocs[slug] != nil && l.termDocs[slug][job.DocID]:
			r.TargetKind, r.TargetID, r.Score, r.Basis = "term", term.TermID(m.Text), 1.0, "term-same-doc"
		case l.classBySlug[slug].id != "":
			hit := l.classBySlug[slug]
			r.TargetKind, r.TargetID, r.Basis = "class", hit.id, hit.basis
			r.Score = 1.0
			if hit.basis == "alias" {
				r.Score = 0.9
			}
		case l.termDocs[slug] != nil:
			r.TargetKind, r.TargetID, r.Score, r.Basis = "term", term.TermID(m.Text), 0.8, "term-corpus"
		default:
			// The extractor's class assignment stands as the mention class,
			// but there is no node to land on, so the mention is unresolved.
			r.TargetKind, r.Score = "unresolved", 0
		}
		out = append(out, r)
	}
	for _, u := range job.Unresolved {
		out = append(out, Resolution{
			ProvisionID: job.ProvisionID,
			DocID:       job.DocID,
			Text:        u.Text,
			TargetKind:  "unresolved",
			Role:        u.Role,
		})
	}
	return out
}
