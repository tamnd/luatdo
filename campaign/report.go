package campaign

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/extract"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/route"
)

// A campaign report is what the pass is for. The run log says how many
// provisions were read; the report says what is now in the graph, and, more to
// the point, what is not.

// Report is one campaign's state, recomputed from what is on disk rather than
// accumulated as the run goes, so an interrupted campaign reports the truth
// about what it managed and a resumed one does not double count.
type Report struct {
	Scope Scope `json:"scope"`
	// Documents in scope, and how many of them the pass has reached.
	Documents int `json:"documents"`
	Reached   int `json:"reached"`
	// Provisions extractable in scope, and how many carry a job.
	Extractable int `json:"extractable"`
	Extracted   int `json:"extracted"`

	Statements int            `json:"statements"`
	Verified   int            `json:"verified"`
	Approved   int            `json:"approved"`
	Rejected   int            `json:"rejected"`
	ByType     map[string]int `json:"by_type"`

	Bearers    int `json:"bearers"`    // statements naming a bearer
	Placed     int `json:"placed"`     // bearers the registry placed
	References int `json:"references"` // references that name a thing
	Conceptual int `json:"conceptual"` // references the concept layer resolved

	Deadlines int `json:"deadlines"` // statements carrying a deadline phrase
	Parsed    int `json:"parsed"`    // deadlines taken apart into a length or a date
	Short     int `json:"short"`     // deadlines shorter than five working days

	Sanctions  norm.SanctionCoverage `json:"sanctions"`
	Procedures int                   `json:"procedures"`
	Steps      int                   `json:"steps"`

	Unsanctioned int `json:"unsanctioned"` // prohibitions nothing punishes
	Unbearing    int `json:"unbearing"`    // norms with nobody to owe them
	Dangling     int `json:"dangling"`     // authorities nothing resolves

	// Calls and cost come from the campaign summaries on disk. Cost is a string
	// because a route with no rate card prices nothing, and reporting a zero
	// there would read as free.
	Runs   int    `json:"runs"`
	Calls  int    `json:"calls"`
	Tokens int    `json:"tokens"`
	Cost   string `json:"cost"`
}

// Compile builds the report from the records of one campaign's documents.
//
// Everything here is counted over the statements the pass produced, whatever
// their verdict, except the four query lines at the bottom, which are counted
// over the trusted ones. A report that counted rejected statements into the
// answers would be describing a graph nobody would query.
func Compile(sc Scope, docs map[string]bool, extractable, extracted map[string]int, records []norm.Record, procedures []norm.Procedure, index map[string]string) Report {
	r := Report{Scope: sc, Documents: len(docs), ByType: map[string]int{}}
	for id := range docs {
		r.Extractable += extractable[id]
		r.Extracted += extracted[id]
		if extracted[id] > 0 {
			r.Reached++
		}
	}
	var trusted []norm.Record
	for i := range records {
		rec := &records[i]
		if !docs[rec.DocID] {
			continue
		}
		s := &rec.Statement
		r.Statements++
		r.ByType[s.Type]++
		switch rec.Status {
		case norm.StatusVerified:
			r.Verified++
		case norm.StatusApproved:
			r.Approved++
		default:
			r.Rejected++
		}
		if s.Bearer != nil && s.Bearer.Text != "" {
			r.Bearers++
			if s.Bearer.ClassID != "" {
				r.Placed++
			}
		}
		for _, ref := range []*norm.Ref{s.Bearer, s.Counterparty, &s.Action, s.Object} {
			if ref == nil {
				continue
			}
			r.References++
			if ref.ConceptID != "" {
				r.Conceptual++
			}
		}
		if s.Deadline != nil {
			r.Deadlines++
			if _, ok := s.Deadline.Days(); ok {
				r.Parsed++
				if s.Deadline.ShorterThan(5) {
					r.Short++
				}
			} else if s.Deadline.AnchorAt == norm.AnchorBy {
				r.Parsed++
			}
		}
		if rec.Trusted() {
			trusted = append(trusted, *rec)
		}
	}
	for _, p := range procedures {
		if !docs[p.DocID] {
			continue
		}
		r.Procedures++
		r.Steps += len(p.Steps)
	}
	// Resolving again over the scoped records is what gives a coverage figure
	// about this campaign rather than about the store. It is deterministic and
	// writes the same identifiers the build already wrote.
	r.Sanctions = norm.ResolveSanctions(trusted, index)
	r.Unsanctioned = len(norm.Unsanctioned(trusted))
	r.Unbearing = len(norm.AskQuestion10(trusted, nil).Rows)
	r.Dangling = len(norm.AskQuestion15(trusted, nil).Rows)
	return r
}

// ModelCalls counts what one job spent on the service: every attempt of every
// candidate, and the judge on each statement it kept long enough to judge.
//
// The count comes from the artifact rather than from a counter kept during the
// run, so a campaign resumed four times still reports the calls all four made.
func ModelCalls(job *extract.NormJob) int {
	n := 0
	for _, c := range job.Candidates {
		n += len(c.Attempts)
	}
	for i := range job.Records {
		if job.Records[i].Entailment != nil {
			n++
		}
		if job.Records[i].Falsification != nil {
			n++
		}
	}
	return n
}

// Account folds the campaign summaries on disk into the report.
//
// The token and cost totals are over every run against this store, not over the
// scope, because a summary records what a run spent and not which documents it
// spent it on. The report says so rather than dividing a number it does not
// have.
func (r *Report) Account(summaries []Summary) {
	cost := route.Cost{Available: true}
	for i := range summaries {
		s := &summaries[i]
		r.Runs++
		r.Tokens += s.Usage.TotalTokens
		cost = cost.Add(s.Cost)
	}
	r.Cost = cost.String()
}

// String renders the report.
//
// The gaps come first and they are named as gaps. A campaign report that opens
// with the number of statements extracted invites the reader to take that as
// the number of statements in the corpus, and the difference between those two
// is the entire question of whether any of this can be trusted.
func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "campaign       %s, %s\n", r.Scope.Name, r.Scope.Note)
	fmt.Fprintf(&b, "documents      %d in scope, %d reached, %d untouched\n",
		r.Documents, r.Reached, r.Documents-r.Reached)
	fmt.Fprintf(&b, "provisions     %d extractable, %d extracted, %d left in the queue\n",
		r.Extractable, r.Extracted, r.Extractable-r.Extracted)
	fmt.Fprintf(&b, "statements     %d, %d verified by the judge, %d kept by a person, %d rejected\n",
		r.Statements, r.Verified, r.Approved, r.Rejected)
	types := make([]string, 0, len(r.ByType))
	for t := range r.ByType {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		fmt.Fprintf(&b, "               %-12s %d\n", t, r.ByType[t])
	}
	fmt.Fprintf(&b, "bearers        %d named, %d placed in the registry\n", r.Bearers, r.Placed)
	fmt.Fprintf(&b, "concepts       %d of %d references resolved to a concept\n", r.Conceptual, r.References)
	fmt.Fprintf(&b, "deadlines      %d phrases, %d taken apart, %d shorter than five working days\n",
		r.Deadlines, r.Parsed, r.Short)
	fmt.Fprintf(&b, "sanctions      %d, %d resolved (%d into another instrument), %d outside the corpus, %d unreadable\n",
		r.Sanctions.Sanctions, r.Sanctions.Resolved, r.Sanctions.CrossDoc, r.Sanctions.External, r.Sanctions.Unresolved)
	fmt.Fprintf(&b, "procedures     %d with %d steps\n", r.Procedures, r.Steps)
	fmt.Fprintf(&b, "open questions %d prohibitions nothing punishes, %d norms with no bearer, %d authorities nothing resolves\n",
		r.Unsanctioned, r.Unbearing, r.Dangling)
	fmt.Fprintf(&b, "model          %d calls in scope, over %d runs against this store spending %d tokens, cost %s\n",
		r.Calls, r.Runs, r.Tokens, r.Cost)
	return b.String()
}
