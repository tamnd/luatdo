// Package conflict finds pairs of trusted statements that cannot both be
// complied with.
//
// The survey is unambiguous about how this has to be built. Every paper that
// tried to ask a language model whether two norms conflict reported the same
// three failures: prompt sensitivity, no verification of the evidence, and
// overconfidence. The paradigm that works is the model as a semantic parser
// into a formal shape, with a deterministic checker doing the comparison, and
// the checker naming the minimal set of things responsible for the clash rather
// than the pair alone.
//
// So the split here is hard and it is the point of the package. The model reads
// one statement at a time and never sees a second one. It maps that statement
// onto a Form, which is a closed vocabulary of deontic operator, party, act,
// object and scope. Everything after that is code: which pairs are comparable,
// whether their scopes intersect, which rule fires, and which slots are
// responsible. No model is asked whether a conflict exists, because a model
// that is asked will find one.
//
// What the package will not say is that anything it found is a legal
// contradiction. Two provisions that clash on this comparison are a pair for a
// person to read. Vietnamese law resolves genuine clashes by rank and by date
// and by specificity, all three of which are reported, and none of which is
// applied.
package conflict

import (
	"regexp"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
)

// The four deontic operators. This is the closed set the checker compares over,
// and it is deliberately smaller than the eight statement types the norm layer
// carries.
//
// A definition, a procedure and an exception state no modality over an act, so
// they have no operator and never enter a pair. Right and permission stay
// separate because they behave differently against a prohibition: a permission
// to do X and a prohibition on doing X is the classic clash between a law and a
// later decree, while a right that its holder never exercises is not violated
// by a prohibition on somebody else doing the same thing.
const (
	Obligation  = "obligation"
	Prohibition = "prohibition"
	Permission  = "permission"
	Right       = "right"
)

// Operators lists the closed set.
var Operators = []string{Obligation, Prohibition, Permission, Right}

// operatorOf maps a statement type onto its deontic operator, and reports
// whether the statement has one at all.
//
// The modality field is consulted only where the type does not decide, because
// the type is the field the invariants enforce and the modality is free text
// that the first campaign filled with eleven different values.
func operatorOf(s *norm.Statement) (string, bool) {
	switch s.Type {
	case "duty":
		return Obligation, true
	case "prohibition":
		return Prohibition, true
	case "permission":
		return Permission, true
	case "right":
		return Right, true
	}
	switch strings.ToLower(strings.TrimSpace(s.Modality)) {
	case "obligation", "requirement":
		return Obligation, true
	case "prohibition":
		return Prohibition, true
	case "permission":
		return Permission, true
	case "right", "entitlement":
		return Right, true
	}
	return "", false
}

// Form is a trusted statement in the shape the checker compares.
//
// Party, Act and Object hold canonical forms rather than the words the
// provision used. Two provisions that say "thông báo cho người lao động" and
// "báo cho người lao động biết" state the same act, and a comparison over the
// surface strings misses every clash that is worth finding while firing on
// every coincidence of wording that is not. Producing the canonical form is
// what the model is for and it is the only thing it is for.
//
// Source keeps the words anyway. A pair this package reports is a pair a person
// reads, and a person reading it needs the sentence and not our normalisation
// of it.
type Form struct {
	StatementID string `json:"statement_id"`
	DocID       string `json:"doc_id"`
	ProvisionID string `json:"provision_id"`

	Operator string `json:"operator"`
	Party    string `json:"party"`            // canonical bearer
	Act      string `json:"act"`              // canonical act, verb phrase without its object
	Object   string `json:"object,omitempty"` // what the act is done to
	Toward   string `json:"toward,omitempty"` // canonical counterparty

	Scope Scope `json:"scope"`

	Deadline *norm.Deadline `json:"deadline,omitempty"`

	// Sanction is the consequence this norm attaches to the act, folded to a
	// slug. It is filled from the statement and never from a model, because it
	// is a copy rather than a reading.
	Sanction string `json:"sanction,omitempty"`

	Canon  Canon  `json:"canon,omitzero"`
	Source Source `json:"source"`
}

// Canon is the canonical wording behind the slugs, in Vietnamese.
//
// The slug is what the checker compares and it is not what a person reads.
// "nguoi-su-dung-lao-dong" is a fine map key and a poor thing to put in front of
// a lawyer, so the words the parser chose are kept beside the key it produced
// and the report prints those.
type Canon struct {
	Party    string `json:"party,omitempty"`
	Act      string `json:"act,omitempty"`
	Object   string `json:"object,omitempty"`
	Toward   string `json:"toward,omitempty"`
	Sanction string `json:"sanction,omitempty"`
}

// Words returns the party, act and object as a person would read them, falling
// back to the slug where a form was built without a parse pass.
func (f *Form) Words() (party, act, object string) {
	return or(f.Canon.Party, f.Party), or(f.Canon.Act, f.Act), or(f.Canon.Object, f.Object)
}

func or(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// Source is what the provision actually said, carried so a reported pair can be
// read rather than trusted.
type Source struct {
	Type   string `json:"statement_type"`
	Bearer string `json:"bearer,omitempty"`
	Action string `json:"action"`
	Quote  string `json:"quote"`
}

// Scope is where and when a Form applies.
//
// Conditions and Exceptions are canonical atoms rather than the sentences they
// came from, for the same reason Act is. They are the field that decides most
// of this package's precision: the existing Cypher form of question 19 compares
// bearer and action alone, says in its own comment that it will produce false
// positives because two norms with the same bearer and action are usually
// distinguished by a condition it ignores, and that comment is the defect this
// package exists to fix.
//
// From and To are the validity interval of the statement's provision, half open
// like every other interval in this project. Two norms that were never in force
// on the same day cannot conflict, and a detector without this test reports
// every amendment in the corpus as a contradiction of the text it replaced.
type Scope struct {
	From       string   `json:"from,omitempty"`
	To         string   `json:"to,omitempty"`
	Conditions []string `json:"conditions,omitempty"`
	Exceptions []string `json:"exceptions,omitempty"`
	// Defers names an instrument this statement stands down to, from an
	// exception of kind override. A norm that says another instrument governs
	// instead is not in conflict with that instrument, it is pointing at it.
	Defers []string `json:"defers,omitempty"`
}

// Comparable reports whether a form can enter a pair at all.
//
// A form with no operator states no modality, and a form with no party or no
// act has nothing to clash over. This is checked once here rather than in each
// rule, so a rule that forgets it cannot exist.
func (f *Form) Comparable() bool {
	return f.Operator != "" && f.Party != "" && f.Act != ""
}

// Overlaps reports whether two forms were ever in force on the same day.
//
// Intervals are half open, so a version that ends the day its successor starts
// does not overlap it. An empty From is the beginning of time and an empty To
// is the end of it, which is the same reading the temporal layer uses.
func (s Scope) Overlaps(o Scope) bool {
	if s.To != "" && o.From != "" && s.To <= o.From {
		return false
	}
	if o.To != "" && s.From != "" && o.To <= s.From {
		return false
	}
	return true
}

// The three answers to whether two condition sets describe circumstances that
// can both hold.
const (
	// CircumstancesShared means one set is contained in the other, so every
	// case the narrower norm reaches is also a case the wider one reaches.
	CircumstancesShared = "shared"
	// CircumstancesUnknown means neither contains the other. The sets may
	// still intersect and this package cannot tell, which is a fact about
	// the comparison rather than about the law.
	CircumstancesUnknown = "unknown"
	// CircumstancesPossible means containment failed and a judge was asked and
	// said one situation can satisfy both sets. It is kept apart from shared
	// because shared is proved here and this is somebody's answer.
	CircumstancesPossible = "possible"
	// CircumstancesDisjoint means a judge said no situation satisfies both, so
	// the two norms are never triggered together and the pair is not a
	// conflict. See Adjudicate.
	CircumstancesDisjoint = "disjoint"
)

// Circumstances compares two condition sets.
//
// The containment test is the honest one available without a solver. If A's
// conditions are a subset of B's, then every situation in which B applies is a
// situation in which A applies, and a clash between them is a clash that can
// really happen. If neither contains the other, the two may or may not ever be
// triggered together, and saying so is better than guessing either way.
//
// This is where the false positive rate of the whole package is decided, which
// is why the unknown case is reported separately rather than folded into either
// answer.
func Circumstances(a, b Scope) string {
	if subset(a.Conditions, b.Conditions) || subset(b.Conditions, a.Conditions) {
		return CircumstancesShared
	}
	return CircumstancesUnknown
}

func subset(a, b []string) bool {
	if len(a) == 0 {
		return true
	}
	in := make(map[string]bool, len(b))
	for _, x := range b {
		in[x] = true
	}
	for _, x := range a {
		if !in[x] {
			return false
		}
	}
	return true
}

// Draft builds the part of a Form that needs no model.
//
// Everything deterministic is filled here and the model is asked only for what
// is left, which is the canonical wording. That split keeps the model out of
// identity, out of interval arithmetic and out of the operator decision, per
// the routing rule that says code does anything that is identity, cardinality
// or offset work.
func Draft(r *norm.Record) (*Form, bool) {
	op, ok := operatorOf(&r.Statement)
	if !ok {
		return nil, false
	}
	f := &Form{
		StatementID: r.ID, DocID: r.DocID, ProvisionID: r.ProvisionID,
		Operator: op,
		Deadline: r.Statement.Deadline,
		Source: Source{
			Type: r.Statement.Type, Action: r.Statement.Action.Text,
			Quote: r.Statement.Evidence.Quote,
		},
	}
	if s := r.Statement.Sanction; s != nil {
		f.Sanction = law.Slug(s.Text)
		f.Canon.Sanction = strings.TrimSpace(s.Text)
	}
	if r.Statement.Bearer != nil {
		f.Source.Bearer = r.Statement.Bearer.Text
		// The concept identifier is a better party key than any wording when
		// the concept layer resolved it, because it is the same identifier
		// across every instrument that names the party differently.
		f.Party = r.Statement.Bearer.ConceptID
	}
	for _, e := range r.Statement.Exceptions {
		if e.Kind == norm.ExcOverride {
			f.Scope.Defers = append(f.Scope.Defers, deferrals(e.Text)...)
		}
	}
	return f, true
}

// deferralPattern matches the official number of an instrument inside the words
// of an override exception, such as "trừ trường hợp Nghị định 145/2020/NĐ-CP quy
// định khác".
//
// The number is matched without the type word in front of it, because a drafter
// writes the type word about half the time and the number identifies the
// instrument on its own.
var deferralPattern = regexp.MustCompile(`\d+/\d{4}/[A-ZĐ][A-ZĐ0-9-]*`)

// deferrals resolves the instruments an override exception stands down to.
//
// An exception naming no number resolves to nothing, and so does one naming a
// provincial number, because those are only an identity together with the
// issuing body and the words of the exception rarely carry it. Both cases leave
// the pair to be compared normally, which is the safe direction: an unrecognised
// deferral produces a finding a reader can dismiss in a second, while one
// recognised from a loose match removes a pair nobody will ever see again.
func deferrals(text string) []string {
	var out []string
	for _, number := range deferralPattern.FindAllString(text, -1) {
		if id, err := law.DocID(number); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// Key is the pairing key: two forms are compared only if they agree on it.
//
// The object is in the key. An obligation to publish a decision and a
// prohibition on publishing personal data are not a clash, and a key of party
// and act alone makes them one.
func (f *Form) Key() string {
	return strings.Join([]string{f.Party, f.Act, f.Object}, "|")
}

// SortForms orders forms by statement identifier, so a run over the same input
// reports the same pairs in the same order on every platform.
func SortForms(fs []*Form) {
	sort.Slice(fs, func(i, j int) bool { return fs[i].StatementID < fs[j].StatementID })
}
