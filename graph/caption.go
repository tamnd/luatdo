package graph

import (
	"strings"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
)

// undated is what a caption says when the node it names has no date. A node
// whose caption renders empty is a circle with nothing in it, and a reader
// cannot tell that apart from a rendering fault. Saying so in as many words is
// the whole of the fix: no date is put on the node, because 21,678 wordings
// belong to documents whose commencement date the source never recorded, and a
// caption is not a place to invent one.
const undated = "no date"

// VersionCaption is what a TextVersion node shows in the browser.
func VersionCaption(v *law.TextVersion) string {
	if v.FromDate != "" {
		return v.FromDate
	}
	return undated
}

// normCaption is what a Norm node shows.
//
// The action is the right caption for a norm that states one, which almost all
// of them do. A definition states no action, so it falls back to what it
// defines, and then to the kind of norm it is, so that the caption is short and
// present rather than short and empty.
func normCaption(s *norm.Statement) string {
	if a := strings.TrimSpace(s.Action.Text); a != "" {
		return a
	}
	if s.Object != nil {
		if o := strings.TrimSpace(s.Object.Text); o != "" {
			return o
		}
	}
	if t := strings.TrimSpace(s.Type); t != "" {
		return t
	}
	return "norm"
}
