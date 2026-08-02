package norm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// The competency questions of the norm layer, 9 through 15. They are here
// rather than in the command because a question is a claim about what the graph
// can answer, and a claim that only exists inside a print statement cannot be
// tested.
//
// Every answer carries what it could not do. A question that returns a clean
// list and says nothing about the norms it skipped is worse than one that
// returns nothing, because the reader has no way to tell a small answer from a
// small corpus.

// Question9 is the duties one instrument places on one kind of actor, and which
// of them carry a consequence.
type Question9 struct {
	DocID  string
	Bearer string
	Rows   []Duty
	// Unsanctioned counts the duties in Rows with no sanction. It is the number
	// people are actually asking for and it is easy to miscount by eye.
	Unsanctioned int
}

// Duty is one row of question 9.
type Duty struct {
	ID          string
	ProvisionID string
	Bearer      string
	Action      string
	Sanction    string
	BasisDoc    string
}

// AskQuestion9 answers what duties an instrument places on a kind of actor.
//
// The bearer is matched on the class when one is given and on the words
// otherwise. Matching on words alone would miss every provision that says
// "đơn vị sử dụng lao động" where the last one said "người sử dụng lao động",
// and those are the same party.
func AskQuestion9(records []Record, docID, bearer string) Question9 {
	q := Question9{DocID: docID, Bearer: bearer}
	want := law.Slug(bearer)
	for i := range records {
		r := &records[i]
		s := &r.Statement
		if !r.Trusted() || s.Type != "duty" || s.Bearer == nil {
			continue
		}
		if docID != "" && r.DocID != docID {
			continue
		}
		if want != "" && s.Bearer.ClassID != bearer && law.Slug(s.Bearer.Text) != want {
			continue
		}
		row := Duty{ID: r.ID, ProvisionID: r.ProvisionID, Bearer: s.Bearer.Text, Action: s.Action.Text}
		if s.Sanction != nil {
			row.Sanction, row.BasisDoc = s.Sanction.Text, s.Sanction.BasisDoc
		} else {
			q.Unsanctioned++
		}
		q.Rows = append(q.Rows, row)
	}
	sort.Slice(q.Rows, func(i, j int) bool { return q.Rows[i].ProvisionID < q.Rows[j].ProvisionID })
	return q
}

func (q Question9) String() string {
	var b strings.Builder
	who := q.Bearer
	if who == "" {
		who = "anybody"
	}
	fmt.Fprintf(&b, "question 9     duties %s places on %s: %d\n", or(q.DocID, "the corpus"), who, len(q.Rows))
	fmt.Fprintf(&b, "               %d of them name no consequence\n", q.Unsanctioned)
	for i, r := range q.Rows {
		if i >= 20 {
			fmt.Fprintf(&b, "               and %d more\n", len(q.Rows)-20)
			break
		}
		note := "no sanction"
		if r.Sanction != "" {
			note = r.Sanction
			if r.BasisDoc != "" {
				note += " (" + r.BasisDoc + ")"
			}
		}
		fmt.Fprintf(&b, "               %s: %s, %s\n", r.ProvisionID, r.Action, note)
	}
	return b.String()
}

// Question10 is the duties with nobody to owe them.
type Question10 struct {
	Rows []Unbearing
}

// Unbearing is one duty with no identified bearer, and this layer's guess at
// whose fault that is.
//
// Cause is a guess and is labelled as one. A provision that names no actor in
// its own words is a drafting choice, usually because the subject is carried
// from the article above; a provision that names one the extraction did not
// record is an extraction failure. The difference matters because one of them
// is fixed by rereading and the other never will be.
type Unbearing struct {
	ID          string
	ProvisionID string
	Action      string
	Quote       string
	Cause       string // drafting or extraction
}

// The two causes.
const (
	CauseDrafting   = "drafting"
	CauseExtraction = "extraction"
)

// AskQuestion10 finds the duties, rights, prohibitions and permissions that
// name no bearer.
//
// The text of each provision is needed to tell the two causes apart, so a
// caller that has no text gets everything labelled as a drafting matter, which
// is the weaker claim and the right default.
func AskQuestion10(records []Record, text map[string]string) Question10 {
	var q Question10
	for i := range records {
		r := &records[i]
		s := &r.Statement
		if !r.Trusted() || !bearerRequired[s.Type] {
			continue
		}
		if s.Bearer != nil && strings.TrimSpace(s.Bearer.Text) != "" {
			continue
		}
		row := Unbearing{ID: r.ID, ProvisionID: r.ProvisionID, Action: s.Action.Text, Quote: s.Evidence.Quote, Cause: CauseDrafting}
		if namesAnActor(text[r.ProvisionID]) {
			row.Cause = CauseExtraction
		}
		q.Rows = append(q.Rows, row)
	}
	sort.Slice(q.Rows, func(i, j int) bool { return q.Rows[i].ProvisionID < q.Rows[j].ProvisionID })
	return q
}

// actorWords are the openings a Vietnamese provision uses when it does name its
// actor. The list is short and it is a heuristic: it is used only to sort a
// review queue, never to fill a bearer in.
var actorWords = []string{
	"người sử dụng lao động", "người lao động", "cơ quan", "tổ chức", "cá nhân",
	"doanh nghiệp", "chủ đầu tư", "bộ", "ủy ban nhân dân", "chủ thể",
}

func namesAnActor(text string) bool {
	lower := strings.ToLower(text)
	for _, w := range actorWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

func (q Question10) String() string {
	var b strings.Builder
	drafting := 0
	for _, r := range q.Rows {
		if r.Cause == CauseDrafting {
			drafting++
		}
	}
	fmt.Fprintf(&b, "question 10    norms with no identified bearer: %d\n", len(q.Rows))
	fmt.Fprintf(&b, "               %d name no actor in their own words, %d name one the extraction did not take\n",
		drafting, len(q.Rows)-drafting)
	for i, r := range q.Rows {
		if i >= 20 {
			fmt.Fprintf(&b, "               and %d more\n", len(q.Rows)-20)
			break
		}
		fmt.Fprintf(&b, "               %s (%s): %s\n", r.ProvisionID, r.Cause, r.Action)
	}
	return b.String()
}

// Question11 is one procedure as ordered steps.
type Question11 struct {
	Query      string
	Procedures []Procedure
}

// AskQuestion11 finds the procedures whose label contains the query, with their
// steps in order.
func AskQuestion11(records []Record, position map[string]int, query string) Question11 {
	q := Question11{Query: query}
	want := law.Slug(query)
	for _, p := range GroupProcedures(records, position) {
		if want == "" || strings.Contains(law.Slug(p.Label), want) {
			q.Procedures = append(q.Procedures, p)
		}
	}
	return q
}

func (q Question11) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "question 11    procedures matching %q: %d\n", q.Query, len(q.Procedures))
	for _, p := range q.Procedures {
		fmt.Fprintf(&b, "               %s (%s)\n", p.Label, p.DocID)
		for _, s := range p.Steps {
			line := fmt.Sprintf("                 %d. %s", s.Number, s.Action)
			if s.Bearer != "" {
				line += " [" + s.Bearer + "]"
			}
			if s.Deadline != "" {
				line += " within " + s.Deadline
			}
			fmt.Fprintln(&b, line)
		}
	}
	return b.String()
}

// Question12 is every short deadline with the actor who must meet it.
type Question12 struct {
	WorkingDays int
	Rows        []Timed
	// Unparsed counts the deadlines this could not take apart, which is the
	// number that says how much of the answer is missing.
	Unparsed int
}

// Timed is one row of question 12.
type Timed struct {
	ProvisionID string
	Bearer      string
	Action      string
	Phrase      string
	Value       int
	Calendar    string
}

// AskQuestion12 finds every deadline shorter than a number of working days.
func AskQuestion12(records []Record, workingDays int) Question12 {
	q := Question12{WorkingDays: workingDays}
	for i := range records {
		r := &records[i]
		s := &r.Statement
		if !r.Trusted() || s.Deadline == nil {
			continue
		}
		if _, ok := s.Deadline.Days(); !ok {
			q.Unparsed++
			continue
		}
		if !s.Deadline.ShorterThan(workingDays) {
			continue
		}
		row := Timed{
			ProvisionID: r.ProvisionID, Action: s.Action.Text, Phrase: s.Deadline.Text,
			Value: s.Deadline.Value, Calendar: s.Deadline.Calendar,
		}
		if s.Bearer != nil {
			row.Bearer = s.Bearer.Text
		}
		q.Rows = append(q.Rows, row)
	}
	sort.Slice(q.Rows, func(i, j int) bool { return q.Rows[i].ProvisionID < q.Rows[j].ProvisionID })
	return q
}

func (q Question12) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "question 12    deadlines shorter than %d working days: %d\n", q.WorkingDays, len(q.Rows))
	fmt.Fprintf(&b, "               %d more deadlines could not be taken apart and are not in this answer\n", q.Unparsed)
	for i, r := range q.Rows {
		if i >= 20 {
			fmt.Fprintf(&b, "               and %d more\n", len(q.Rows)-20)
			break
		}
		fmt.Fprintf(&b, "               %s: %s must %s within %s\n", r.ProvisionID, or(r.Bearer, "somebody"), r.Action, r.Phrase)
	}
	return b.String()
}

// Question13 is the prohibitions nothing in the corpus punishes.
type Question13 struct {
	Rows       []Record
	Prohibited int
}

// AskQuestion13 answers which prohibitions have no sanction anywhere.
func AskQuestion13(records []Record) Question13 {
	q := Question13{Rows: Unsanctioned(records)}
	for i := range records {
		if records[i].Trusted() && records[i].Statement.Type == "prohibition" {
			q.Prohibited++
		}
	}
	return q
}

func (q Question13) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "question 13    prohibitions nothing punishes: %d of %d\n", len(q.Rows), q.Prohibited)
	for i, r := range q.Rows {
		if i >= 20 {
			fmt.Fprintf(&b, "               and %d more\n", len(q.Rows)-20)
			break
		}
		fmt.Fprintf(&b, "               %s: %s\n", r.ProvisionID, r.Statement.Action.Text)
	}
	return b.String()
}

// Question14 is what has to hold for one duty and what releases its bearer.
type Question14 struct {
	ID         string
	Statement  *Statement
	Conditions []Clause
	Exceptions []Clause
}

// AskQuestion14 returns the conditions and exceptions of one statement.
func AskQuestion14(records []Record, id string) Question14 {
	for i := range records {
		if records[i].ID != id {
			continue
		}
		s := &records[i].Statement
		return Question14{ID: id, Statement: s, Conditions: s.Conditions, Exceptions: s.Exceptions}
	}
	return Question14{ID: id}
}

func (q Question14) String() string {
	var b strings.Builder
	if q.Statement == nil {
		fmt.Fprintf(&b, "question 14    no statement with the identifier %s\n", q.ID)
		return b.String()
	}
	fmt.Fprintf(&b, "question 14    %s: %s\n", q.ID, q.Statement.Action.Text)
	if len(q.Conditions) == 0 {
		b.WriteString("               it holds unconditionally\n")
	}
	for _, c := range q.Conditions {
		fmt.Fprintf(&b, "               condition (%s): %s\n", c.Kind, c.Text)
	}
	if len(q.Exceptions) == 0 {
		b.WriteString("               nothing releases the bearer\n")
	}
	for _, e := range q.Exceptions {
		fmt.Fprintf(&b, "               exception (%s): %s\n", e.Kind, e.Text)
	}
	return b.String()
}

// Question15 is the norms that name an authority nothing ever resolves.
type Question15 struct {
	Rows []Dangling
}

// Dangling is one reference to an actor the corpus never pins down.
type Dangling struct {
	ProvisionID string
	Text        string
	Action      string
}

// vagueActors are the phrases Vietnamese drafters use when they mean an actor
// they are not naming. Each one is a hole in the graph wherever nothing else
// resolves it, and question 15 is the list of those holes.
var vagueActors = []string{
	"cơ quan có thẩm quyền",
	"cơ quan nhà nước có thẩm quyền",
	"người có thẩm quyền",
	"cấp có thẩm quyền",
	"cơ quan quản lý nhà nước có thẩm quyền",
}

// AskQuestion15 finds the norms whose bearer is one of the vague authority
// phrases and which nothing in the corpus resolves.
//
// The defined set is the labels of every term the corpus defines, slugged. A
// phrase that some instrument defines is not dangling even when it reads as
// vague, because a reader can follow it, and that is the whole question.
func AskQuestion15(records []Record, defined map[string]bool) Question15 {
	var q Question15
	for i := range records {
		r := &records[i]
		s := &r.Statement
		if !r.Trusted() || s.Bearer == nil {
			continue
		}
		if s.Bearer.ConceptID != "" || defined[law.Slug(s.Bearer.Text)] {
			continue
		}
		if !vague(s.Bearer.Text) {
			continue
		}
		q.Rows = append(q.Rows, Dangling{ProvisionID: r.ProvisionID, Text: s.Bearer.Text, Action: s.Action.Text})
	}
	sort.Slice(q.Rows, func(i, j int) bool { return q.Rows[i].ProvisionID < q.Rows[j].ProvisionID })
	return q
}

func vague(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, v := range vagueActors {
		if strings.Contains(lower, v) {
			return true
		}
	}
	return false
}

func (q Question15) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "question 15    norms naming an authority nothing resolves: %d\n", len(q.Rows))
	for i, r := range q.Rows {
		if i >= 20 {
			fmt.Fprintf(&b, "               and %d more\n", len(q.Rows)-20)
			break
		}
		fmt.Fprintf(&b, "               %s: %s must %s\n", r.ProvisionID, r.Text, r.Action)
	}
	return b.String()
}

func or(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
