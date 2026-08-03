package conflict

import "strings"

// Rank is what the three classical conflict rules say about a pair, and it is
// reported rather than applied.
//
// Lex superior, lex posterior and lex specialis decide which of two clashing
// norms prevails, and they are the reason a legal system tolerates clashes at
// all. They are also not a function this package is entitled to compute. Which
// rule governs depends on facts outside the pair, the three can point in
// different directions, and a machine that silently picked one would be
// deciding a legal question and presenting it as a lookup.
//
// So all three are computed, all three are printed, and Winner is deliberately
// absent from this type.
type Rank struct {
	// Superior names the statement whose instrument is higher in the
	// hierarchy of legal instruments, or is empty where they are level or
	// where the type of one is unknown.
	Superior string `json:"superior,omitempty"`
	// Posterior names the statement from the later instrument.
	Posterior string `json:"posterior,omitempty"`
	// Specialis names the statement with strictly more conditions on it,
	// which is the closest thing to specificity this comparison can see.
	Specialis string `json:"specialis,omitempty"`
	// Agree reports whether the rules that had an opinion all named the same
	// statement. Where they disagree the pair needs a lawyer and the report
	// says so instead of averaging them.
	Agree bool `json:"agree"`
}

func (r Rank) String() string {
	var parts []string
	if r.Superior != "" {
		parts = append(parts, "higher instrument "+r.Superior)
	}
	if r.Posterior != "" {
		parts = append(parts, "later instrument "+r.Posterior)
	}
	if r.Specialis != "" {
		parts = append(parts, "more specific "+r.Specialis)
	}
	if len(parts) == 0 {
		return ""
	}
	out := strings.Join(parts, ", ")
	if !r.Agree {
		out += ", and they do not point the same way"
	}
	return out
}

// DocInfo is what ranking needs to know about an instrument. It is an interface
// rather than a map so a caller can back it with the document store without
// loading 128,094 documents to compare eight statements.
type DocInfo interface {
	// Type is the instrument type as the corpus writes it, for example
	// "Nghị định". An unknown document returns an empty string.
	Type(docID string) string
	// Effective is the date the instrument took effect, in the form every
	// interval in this project compares, or empty.
	Effective(docID string) string
}

// tiers order Vietnamese instrument types by legal force, lower number first.
//
// The list is from the Law on Promulgation of Legal Normative Documents and it
// is coarse on purpose. Two instruments in the same tier get no lex superior
// answer, which is correct: a circular of one ministry does not outrank a
// circular of another, and pretending otherwise would produce a confident
// resolution of exactly the cases that need a person.
var tiers = map[string]int{
	"hiến pháp": 1, "constitution": 1,
	"bộ luật": 2, "luật": 2, "law": 2, "code": 2,
	"pháp lệnh": 3,
	"lệnh":      4, "nghị quyết": 4,
	"nghị định":  5,
	"quyết định": 6, "chỉ thị": 6,
	"thông tư": 7, "thông tư liên tịch": 7,
}

func tierOf(docType string) (int, bool) {
	t, ok := tiers[strings.ToLower(strings.TrimSpace(docType))]
	return t, ok
}

// rank applies the three rules to one pair.
func rank(a, b *Form, docs DocInfo) Rank {
	var r Rank
	var opinions []string

	if docs != nil {
		ta, oka := tierOf(docs.Type(a.DocID))
		tb, okb := tierOf(docs.Type(b.DocID))
		if oka && okb && ta != tb {
			r.Superior = a.StatementID
			if tb < ta {
				r.Superior = b.StatementID
			}
			opinions = append(opinions, r.Superior)
		}
		ea, eb := docs.Effective(a.DocID), docs.Effective(b.DocID)
		if ea != "" && eb != "" && ea != eb {
			r.Posterior = a.StatementID
			if eb > ea {
				r.Posterior = b.StatementID
			}
			opinions = append(opinions, r.Posterior)
		}
	}
	if na, nb := len(a.Scope.Conditions), len(b.Scope.Conditions); na != nb {
		r.Specialis = a.StatementID
		if nb > na {
			r.Specialis = b.StatementID
		}
		opinions = append(opinions, r.Specialis)
	}

	r.Agree = true
	for _, o := range opinions {
		if o != opinions[0] {
			r.Agree = false
		}
	}
	return r
}

// Docs is the simple DocInfo a command builds from the document store.
type Docs struct {
	Types      map[string]string
	Effectives map[string]string
}

func (d *Docs) Type(id string) string      { return d.Types[id] }
func (d *Docs) Effective(id string) string { return d.Effectives[id] }

// Describe renders a rank for a person, naming the provisions rather than the
// statement hashes.
func Describe(r Rank, a, b *Form) string {
	name := func(id string) string {
		switch id {
		case a.StatementID:
			return a.ProvisionID
		case b.StatementID:
			return b.ProvisionID
		}
		return id
	}
	s := r.String()
	for _, f := range []*Form{a, b} {
		s = strings.ReplaceAll(s, f.StatementID, name(f.StatementID))
	}
	return s
}
