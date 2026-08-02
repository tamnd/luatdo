package temporal

import (
	"sort"
)

// Kinds of record that read a provision and therefore inherit its validity.
const (
	AnchorNorm     = "norm"
	AnchorTermUse  = "term_use"
	AnchorRelation = "relation_edge"
)

// Where an interval came from. This is on every stamp because the three are not
// equally strong and a reader who cannot tell them apart is worse off than one
// who has no interval at all.
const (
	// SourceVersion is a version node. Somebody read the amendments to this
	// component, so both ends of the interval are known.
	SourceVersion = "version_graph"
	// SourceCommencement is the day the document took effect, used for a
	// component the version graph does not cover and that nothing in the
	// citation graph says was ever amended. The interval is open and there is
	// no reason to think it closed.
	SourceCommencement = "commencement"
	// SourceCommencementAmended is the same day, for a document the citation
	// graph says was amended by an instrument nobody has read yet. The interval
	// starts where it says and its end is unknown rather than open, and this is
	// the value that must not be read as "still in force".
	SourceCommencementAmended = "commencement_amended"
)

// Validity is the interval one record is good for.
//
// A norm, a term use and a relation edge are readings of particular words. The
// words have an interval and the component does not, so the reading takes the
// interval of the wording it was read from rather than standing outside time.
type Validity struct {
	Kind        string `json:"kind"`         // norm, term_use, relation_edge
	RecordID    string `json:"record_id"`    // the identifier of the record in its own store
	ProvisionID string `json:"provision_id"` // the component the record reads
	VersionID   string `json:"version_id,omitempty"`
	From        string `json:"from"`
	To          string `json:"to,omitempty"` // empty means open, subject to Source
	Force       string `json:"force"`
	Source      string `json:"source"`
}

// Reading is a record that reads a provision, reduced to the fields this
// package needs. The stores it comes from are none of this package's business.
type Reading struct {
	Kind        string
	RecordID    string
	DocID       string
	ProvisionID string
}

// Commencement is what is known about a document the version graph does not
// cover: the day it took effect, and whether anything claims to have amended it.
type Commencement struct {
	From    string
	Amended bool
}

// Stamp gives every reading the interval of the wording it reads.
//
// A provision with more than one wording produces more than one interval, and
// that is the honest answer rather than a defect. A statement read out of the
// 2019 wording of article 94 was not stated by the 2021 wording, and until
// somebody records which wording was in front of the extractor, the only true
// thing to say is that the reading belongs to one of these. The alternative is
// picking the newest, which is a guess that reads exactly like a fact.
//
// A provision the version graph does not cover falls back to the day its
// document commenced, and the stamp says so. That is a real answer rather than
// a filler: the words have been there since the document took effect and no
// amendment to them has been read. Where the citation graph does claim an
// amendment, the fallback is marked as the weaker kind, because an interval
// left open on a document somebody amended is the one mistake this layer exists
// to prevent.
//
// A reading whose document commenced on no date anybody recorded yields
// nothing. An undated interval is not an interval.
func Stamp(v *View, readings []Reading, known map[string]Commencement) []Validity {
	var out []Validity
	for _, r := range readings {
		versions := v.Versions(r.ProvisionID)
		if len(versions) > 0 {
			for _, ver := range versions {
				out = append(out, Validity{
					Kind: r.Kind, RecordID: r.RecordID, ProvisionID: r.ProvisionID,
					VersionID: ver.ID, From: ver.From, To: ver.To,
					Force: ver.Force, Source: SourceVersion,
				})
			}
			continue
		}
		c, ok := known[r.DocID]
		if !ok || c.From == "" {
			continue
		}
		source := SourceCommencement
		if c.Amended {
			source = SourceCommencementAmended
		}
		out = append(out, Validity{
			Kind: r.Kind, RecordID: r.RecordID, ProvisionID: r.ProvisionID,
			From: c.From, Force: ForceInForce, Source: source,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].RecordID != out[j].RecordID {
			return out[i].RecordID < out[j].RecordID
		}
		return out[i].From < out[j].From
	})
	return out
}

// InForceAt reports whether a record is a reading of words in force on a date.
//
// A suspended wording is not in force, so a norm read from it is not either,
// and neither is a reading whose interval rests on a document with an amendment
// nobody has read. The second one is the awkward answer and it is the right
// one: this layer would rather say it does not know than say yes.
func (r Validity) InForceAt(date string) bool {
	if r.Source == SourceCommencementAmended {
		return false
	}
	v := Version{From: r.From, To: r.To, Force: r.Force}
	return v.InForceAt(date)
}

// StampCoverage counts stamps by source, so a run reports the strength of what
// it produced instead of one number that hides three different answers.
func StampCoverage(readings []Reading, stamped []Validity) map[string]int {
	out := map[string]int{}
	seen := map[string]bool{}
	for _, s := range stamped {
		out[s.Source]++
		seen[s.Kind+"\x00"+s.RecordID] = true
	}
	for _, r := range readings {
		if !seen[r.Kind+"\x00"+r.RecordID] {
			out["none"]++
		}
	}
	return out
}
