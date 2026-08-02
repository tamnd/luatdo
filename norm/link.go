package norm

import (
	"strings"

	"github.com/tamnd/luatdo/law"
)

// LinkConcepts attaches a concept identifier to every reference whose words
// name a concept the concept layer already decided on, and reports how many
// references it could place.
//
// This runs over the trusted store rather than inside the extraction prompt. A
// model handed the corpus concept list would be asked to choose among tens of
// thousands of labels in the same call where it is reading a provision, and the
// choice it made would be unreviewable. The concept layer already made these
// decisions with a person in the loop, so the join is a lookup, and a phrase
// nothing matches simply keeps its class and no concept.
//
// A longest suffix is tried after the whole phrase. Drafters qualify an actor
// in place, so "cơ sở dạy nghề quy định tại khoản 3 Điều 15" is the concept cơ
// sở dạy nghề with a pointer stapled to it, and refusing to see that leaves the
// commonest references in the corpus unlinked. Only the head is tried, never a
// tail, because "giấy phép hoạt động dạy nghề" is not "hoạt động dạy nghề".
func LinkConcepts(records []Record, index map[string]string) (linked, total int) {
	for i := range records {
		s := &records[i].Statement
		for _, ref := range refs(s) {
			total++
			if id, ok := lookup(index, ref.Text); ok {
				ref.ConceptID = id
				linked++
			}
		}
		if s.Sanction != nil {
			total++
			if id, ok := lookup(index, s.Sanction.Text); ok {
				s.Sanction.ConceptID = id
				linked++
			}
		}
	}
	return linked, total
}

// refs is every reference on a statement that names a thing, which is the set
// a concept identifier can belong to.
func refs(s *Statement) []*Ref {
	out := []*Ref{&s.Action}
	for _, r := range []*Ref{s.Bearer, s.Counterparty, s.Object} {
		if r != nil {
			out = append(out, r)
		}
	}
	return out
}

// lookup resolves a phrase to a concept, trying the whole phrase first and then
// dropping trailing words. The floor of two words is what keeps a qualifier
// from collapsing to a bare noun that means something else on its own.
func lookup(index map[string]string, text string) (string, bool) {
	words := strings.Fields(strings.TrimSpace(text))
	if len(words) == 0 {
		return "", false
	}
	for n := len(words); n >= 2; n-- {
		if id, ok := index[law.Slug(strings.Join(words[:n], " "))]; ok {
			return id, true
		}
	}
	if len(words) == 1 {
		id, ok := index[law.Slug(words[0])]
		return id, ok
	}
	return "", false
}
