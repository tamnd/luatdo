package campaign

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/subject"
)

// A campaign covers a named slice of the corpus rather than the whole of it.
//
// The whole corpus is a hundred and twenty eight thousand documents, and a pass
// over all of them measures the throughput of a queue and nothing else. A slice
// picked to hold one area of law end to end, the code and the decrees under it
// and the circulars under those, is what makes the competency questions
// answerable: question 13 asks which prohibitions nothing punishes, and the
// answer is meaningless unless the instruments that do the punishing are in the
// same pass as the ones that do the forbidding.

// Scope is the definition of one campaign, kept as data so a report written
// months later selects the same documents the run did.
type Scope struct {
	Name string `json:"name"`
	// Subject is a subject identifier. A document is in scope when it or one of
	// its subdomains is assigned to it.
	Subject string `json:"subject,omitempty"`
	// DocTypes is the instrument types in scope, empty for every type.
	DocTypes []string `json:"doc_types,omitempty"`
	// ExcludeIssuers keeps out the documents whose issuing body starts with one
	// of these, folded and lowercased.
	ExcludeIssuers []string `json:"exclude_issuers,omitempty"`
	// EffectiveFrom, when set, keeps out documents that took effect after it.
	// The date is compared as a string because every date in the corpus is
	// stored as one and they are all the same shape.
	EffectiveFrom string `json:"effective_from,omitempty"`
	Note          string `json:"note,omitempty"`
}

// Scopes are the campaigns this build knows by name.
//
// labour-2025 is the labour code and every national instrument under it: the
// decrees, the circulars and joint circulars, and the ministerial and prime
// ministerial decisions. It leaves out the documents a province issued, which
// are two thirds of the labour subject by count and almost none of it by
// normative content, since a provincial decision under the labour code usually
// assigns a budget or renames a committee rather than placing a duty on
// anybody. Dropping them takes the subject from four and a half thousand
// documents to roughly twelve hundred, and it is a statement about what the
// pass is for rather than a way of making it cheaper.
//
// The cut is on the issuing body and not on the instrument type, because a
// decision is central when a ministry signs it and local when a province does,
// and the type says nothing either way.
var Scopes = map[string]Scope{
	"labour-2025": {
		Name:           "labour-2025",
		Subject:        "lao-dong",
		ExcludeIssuers: Provincial,
		EffectiveFrom:  "2025-12-31",
		Note:           "the labour code and the national instruments under it, in force through 2025",
	},
}

// Provincial is the issuing bodies of a province, in the spellings the corpus
// uses. Both spellings of Ủy are here because the datasets disagree and a
// filter that misses one silently doubles the scope.
var Provincial = []string{
	"ubnd", "ủy ban nhân dân", "uỷ ban nhân dân",
	"hđnd", "hội đồng nhân dân",
}

// ScopeNames lists the known campaigns.
func ScopeNames() []string {
	out := make([]string, 0, len(Scopes))
	for name := range Scopes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// LookupScope returns a campaign definition by name.
func LookupScope(name string) (Scope, error) {
	sc, ok := Scopes[name]
	if !ok {
		return Scope{}, fmt.Errorf("no campaign named %q, this build knows %s", name, strings.Join(ScopeNames(), ", "))
	}
	return sc, nil
}

// Documents returns the identifiers of the documents in scope, from the subject
// assignments and the parsed documents.
//
// A document with no subject record at all is out of scope rather than in it. A
// campaign that silently swept up every unclassified document would be a
// campaign over the whole corpus wearing a name.
func (sc Scope) Documents(records []subject.Record, docs []*law.Document) map[string]bool {
	byID := make(map[string]*law.Document, len(docs))
	for _, d := range docs {
		byID[d.ID] = d
	}
	types := map[string]bool{}
	for _, t := range sc.DocTypes {
		types[strings.ToLower(t)] = true
	}
	out := map[string]bool{}
	for i := range records {
		r := &records[i]
		doc := byID[r.DocID]
		if doc == nil || doc.Status == "quarantined" {
			continue
		}
		if sc.Subject != "" && !assigned(r, sc.Subject) {
			continue
		}
		if len(types) > 0 && !types[strings.ToLower(doc.DocType)] {
			continue
		}
		if sc.excluded(doc.IssuingBody) {
			continue
		}
		if sc.EffectiveFrom != "" && doc.EffectiveFrom > sc.EffectiveFrom {
			continue
		}
		out[r.DocID] = true
	}
	return out
}

// excluded reports whether an issuing body is out of scope.
func (sc Scope) excluded(body string) bool {
	lower := strings.ToLower(strings.TrimSpace(body))
	for _, prefix := range sc.ExcludeIssuers {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// assigned reports whether a document sits under a subject or under one of its
// subdomains. The subject layer writes the parent alongside every subdomain, so
// the prefix test is a second line of defence rather than the only one.
func assigned(r *subject.Record, id string) bool {
	for _, a := range r.Subjects {
		if a.SubjectID == id || strings.HasPrefix(a.SubjectID, id+"/") {
			return true
		}
	}
	return false
}

// InScope keeps the queue entries whose document is in the campaign.
func InScope(tasks []coverage.Task, docs map[string]bool) []coverage.Task {
	out := make([]coverage.Task, 0, len(tasks))
	for _, t := range tasks {
		if docs[t.DocID] {
			out = append(out, t)
		}
	}
	return out
}
