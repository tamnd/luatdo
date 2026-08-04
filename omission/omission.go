// Package omission audits what the pass never said anything about.
//
// Spec file 09 section 5 calls omission the dominant risk and the invisible
// one, and every recall figure this project has quoted is recall against an
// annotation rather than against the text. A gold set says how much of what a
// person wrote down was found. It cannot say anything at all about the clauses
// nobody annotated, which is all of them.
//
// The check here is the one spec file 04 section 9.3 specifies and it is
// deliberately dumb. Vietnamese legal drafting states duties, prohibitions and
// rights with a small closed set of surface forms. A sentence carrying one of
// them and no statement over it is either an extraction miss or a sentence
// where the form means something else, and both of those are worth reading. It
// is a screen, not a measurement of recall: it finds nothing about the norms
// stated without one of these words, and there are plenty.
//
// The result is a list, not a rate. A rate over omissions is a number that
// improves when the corpus grows, and the only thing anybody can do with a
// suspected miss is read it.
package omission

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
)

// Markers are the surface forms the audit screens on, in the order the
// milestone lists them.
//
// Each is matched over the folded text, so "Phải" at the start of a sentence
// and "phải" inside one are the same marker, and "có quyền" matches whether the
// law wrote one space or two.
var Markers = []string{"phải", "nghiêm cấm", "không được", "có trách nhiệm", "có quyền"}

// Sentence is one sentence of a provision with its span in the provision text.
type Sentence struct {
	Text  string
	Start int
	End   int
}

// Sentences splits Vietnamese legal prose.
//
// The rule is a newline, or a full stop, semicolon or colon followed by
// whitespace. Legal drafting puts one rule per clause and separates enumerated
// items with semicolons, so the semicolon is a sentence boundary here in a way
// it would not be in ordinary prose. Requiring whitespace after the stop is
// what keeps "1.000.000 đồng" and "khoản 1.2" in one piece.
//
// It is not a general sentence splitter and does not try to be. It splits the
// text the same way on every platform, it never loses a byte, and the spans it
// returns index back into the text it was given, which is what the coverage
// test needs.
func Sentences(text string) []Sentence {
	var out []Sentence
	start := 0
	for i, r := range text {
		if r != '\n' && r != '.' && r != ';' && r != ':' {
			continue
		}
		end := i + len(string(r))
		if r != '\n' {
			if end >= len(text) {
				break
			}
			if next := text[end]; next != ' ' && next != '\n' && next != '\t' {
				continue
			}
		}
		if s := (Sentence{Text: strings.TrimSpace(text[start:end]), Start: start, End: end}); s.Text != "" {
			out = append(out, s)
		}
		start = end
	}
	if s := (Sentence{Text: strings.TrimSpace(text[start:]), Start: start, End: len(text)}); s.Text != "" {
		out = append(out, s)
	}
	return out
}

// carries returns the markers a sentence carries.
func carries(text string) []string {
	var out []string
	for _, m := range Markers {
		if law.HasPhrase(text, m) {
			out = append(out, m)
		}
	}
	return out
}

// The three states a sentence carrying a marker can be in.
const (
	// Covered means a trusted statement quotes it. Nothing to look at.
	Covered = "covered"
	// Dropped means the only statements over it were rejected or invalid. The
	// pass read the sentence and the verification threw the reading away, which
	// is a different failure from never reading it and is reported separately
	// because the fix is a different one.
	Dropped = "dropped"
	// Missed means nothing was ever extracted from it.
	Missed = "missed"
)

// Finding is one sentence the audit wants a person to read.
type Finding struct {
	DocID       string   `json:"doc_id"`
	ProvisionID string   `json:"provision_id"`
	Sentence    string   `json:"sentence"`
	Markers     []string `json:"markers"`
	State       string   `json:"state"`
}

// Count is one marker's tally.
type Count struct {
	Sentences int `json:"sentences"`
	Covered   int `json:"covered"`
	Dropped   int `json:"dropped"`
	Missed    int `json:"missed"`
}

// Report is the audit over a scope.
type Report struct {
	Provisions int              `json:"provisions"`
	Sentences  int              `json:"sentences"`
	WithMarker int              `json:"with_marker"`
	Covered    int              `json:"covered"`
	Dropped    int              `json:"dropped"`
	Missed     int              `json:"missed"`
	ByMarker   map[string]Count `json:"by_marker"`
	Findings   []Finding        `json:"findings"`
}

// Provision folds one provision into the report: its text, and every record
// the pass produced over it whatever the verdict.
//
// The records have to be all of them. Passing only the trusted ones turns every
// rejected statement into a missed sentence and doubles the audit's own
// numbers, which would be the most alarming and least true version of this
// report.
func (r *Report) Provision(docID, provisionID, text string, records []norm.Record) {
	if r.ByMarker == nil {
		r.ByMarker = map[string]Count{}
	}
	r.Provisions++
	trusted, any := spans(text, records)
	for _, s := range Sentences(text) {
		r.Sentences++
		markers := carries(s.Text)
		if len(markers) == 0 {
			continue
		}
		r.WithMarker++
		state := Missed
		switch {
		case overlaps(trusted, s):
			state = Covered
		case overlaps(any, s):
			state = Dropped
		}
		switch state {
		case Covered:
			r.Covered++
		case Dropped:
			r.Dropped++
		default:
			r.Missed++
		}
		for _, m := range markers {
			c := r.ByMarker[m]
			c.Sentences++
			switch state {
			case Covered:
				c.Covered++
			case Dropped:
				c.Dropped++
			default:
				c.Missed++
			}
			r.ByMarker[m] = c
		}
		if state != Covered {
			r.Findings = append(r.Findings, Finding{
				DocID: docID, ProvisionID: provisionID,
				Sentence: s.Text, Markers: markers, State: state,
			})
		}
	}
}

// span is a stretch of provision text some statement quoted.
type span struct{ start, end int }

// spans is where the statements of a provision touched its text, split into
// the ones a trusted statement covers and the ones any statement covers.
//
// Every quote a record carries counts, not just the evidence span. A duty whose
// condition quote is the sentence in question has been read, and counting only
// the evidence would report the condition's sentence as missed.
func spans(text string, records []norm.Record) (trusted, any []span) {
	for i := range records {
		rec := &records[i]
		var quotes []string
		s := &rec.Statement
		if s.Evidence.Quote != "" {
			quotes = append(quotes, s.Evidence.Quote)
		}
		for _, c := range s.Conditions {
			quotes = append(quotes, c.Quote)
		}
		for _, e := range s.Exceptions {
			quotes = append(quotes, e.Quote)
		}
		if s.Sanction != nil {
			quotes = append(quotes, s.Sanction.Quote)
		}
		if s.Deadline != nil {
			quotes = append(quotes, s.Deadline.Text)
		}
		for _, q := range quotes {
			if q == "" {
				continue
			}
			// The offsets on the record are into this same text, but they were
			// written by an earlier version of the pass and a reparse can move
			// them. Finding the quote again is cheap and is right whenever the
			// quote is still there at all.
			at := strings.Index(text, q)
			if at < 0 {
				continue
			}
			sp := span{at, at + len(q)}
			any = append(any, sp)
			if rec.Trusted() {
				trusted = append(trusted, sp)
			}
		}
	}
	return trusted, any
}

func overlaps(spans []span, s Sentence) bool {
	for _, sp := range spans {
		if sp.start < s.End && s.Start < sp.end {
			return true
		}
	}
	return false
}

// String renders the audit. The counts come first and the list follows, and the
// list is the point.
func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "omission       %d provisions, %d sentences, %d carrying a modality marker\n",
		r.Provisions, r.Sentences, r.WithMarker)
	fmt.Fprintf(&b, "               %d covered by a trusted statement, %d covered only by a statement verification threw away, %d nothing was ever extracted from\n",
		r.Covered, r.Dropped, r.Missed)
	for _, m := range Markers {
		c := r.ByMarker[m]
		if c.Sentences == 0 {
			continue
		}
		fmt.Fprintf(&b, "               %-16s %d sentences, %d covered, %d dropped, %d missed\n",
			m, c.Sentences, c.Covered, c.Dropped, c.Missed)
	}
	if len(r.Findings) == 0 {
		fmt.Fprintf(&b, "               nothing to read, which for a screen this crude means the screen and not the corpus\n")
		return b.String()
	}
	for _, f := range r.Sorted() {
		fmt.Fprintf(&b, "  %-7s %s [%s]\n                 %s\n",
			f.State, f.ProvisionID, strings.Join(f.Markers, ", "), oneLine(f.Sentence))
	}
	return b.String()
}

// Sorted is the findings in a fixed order: the never extracted first, because
// they are the ones nobody has looked at, then by provision.
func (r Report) Sorted() []Finding {
	out := append([]Finding(nil), r.Findings...)
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].State == Missed) != (out[j].State == Missed) {
			return out[i].State == Missed
		}
		if out[i].ProvisionID != out[j].ProvisionID {
			return out[i].ProvisionID < out[j].ProvisionID
		}
		return out[i].Sentence < out[j].Sentence
	})
	return out
}

// oneLine keeps a listed sentence on one line of a terminal. A sentence that
// wraps over four lines is one nobody reads, and the provision identifier is
// there for anybody who wants the whole thing.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 160 {
		return string(r[:157]) + "..."
	}
	return s
}
