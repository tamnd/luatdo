package norm

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/eval"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/store"
)

// The norm gold set is annotated before the extraction runs over the same
// clauses, and never after, for the same reason the concept gold set is: an
// annotation written while looking at what a model produced measures agreement
// with that model rather than accuracy, and the two look identical once they
// are written down.

// GoldFile is the annotated clause file inside the norm gold directory.
const GoldFile = "gold_norms.jsonl"

// The senses of the word được, which is the single most consequential
// ambiguity in Vietnamese legal drafting and the one this layer is measured on.
//
// SensePassiveRight is the trap. "Người lao động được trả lương đúng hạn" reads
// like a permission granted to the worker and is a right stated in the passive
// voice, with the duty on the employer. An extractor that files it as a
// permission has moved the obligation off the party that owes it, and every
// question about worker rights then answers with an empty set.
const (
	SenseNone         = "none"          // the clause does not use the word
	SensePermission   = "permission"    // được quyền, được phép
	SensePassiveRight = "passive_right" // được followed by a verb, the passive
	SenseProhibition  = "prohibition"   // không được
)

// Senses is the closed set.
var Senses = []string{SenseNone, SensePermission, SensePassiveRight, SenseProhibition}

// GoldStatement is one norm a person says a clause states.
//
// It carries only what a second annotator would agree on. Wording of the action
// is not scored as a string, because two correct readings of one duty differ in
// wording and scoring them character by character punishes the right answer.
type GoldStatement struct {
	Type        string `json:"statement_type"`
	Bearer      string `json:"bearer,omitempty"`
	Action      string `json:"action"`
	HasSanction bool   `json:"has_sanction,omitempty"`
	Deadline    string `json:"deadline,omitempty"`
}

// Gold is one annotated clause.
type Gold struct {
	UnitID   string `json:"unit_id"`
	DocID    string `json:"doc_id"`
	TextHash string `json:"text_hash"`
	// Text is stored with the annotation so a later run can tell that the clause
	// changed under it. An annotation against text that has since been amended is
	// not evidence about the current corpus.
	Text         string          `json:"text"`
	StatesNoNorm bool            `json:"states_no_norm,omitempty"`
	Statements   []GoldStatement `json:"statements,omitempty"`
	// Duoc is the sense the word carries in this clause, annotated separately
	// from the statements so the trap is measured whether or not the extraction
	// found the norm at all.
	Duoc        string `json:"duoc,omitempty"`
	AnnotatedBy string `json:"annotated_by"`
	AnnotatedAt string `json:"annotated_at"`
	Note        string `json:"note,omitempty"`
}

// ReadGold returns the annotated clauses.
func ReadGold(dir string) ([]Gold, error) {
	return store.ReadJSONL[Gold](filepath.Join(dir, GoldFile))
}

// WriteGold appends annotations.
func WriteGold(dir string, gs []Gold) error {
	return store.AppendJSONL(filepath.Join(dir, GoldFile), gs)
}

// CheckGold returns what is wrong with an annotation set, sorted.
//
// The gold set is the measuring stick, so it is checked harder than the thing
// it measures. A typo in a statement type here scores every correct extraction
// as wrong, and the pipeline then looks broken instead of the ruler.
func CheckGold(gs []Gold) []string {
	var out []string
	seen := map[string]bool{}
	for _, g := range gs {
		where := g.UnitID
		if where == "" {
			out = append(out, "an annotation names no unit")
			continue
		}
		if seen[where] {
			out = append(out, where+" is annotated twice")
		}
		seen[where] = true
		if g.TextHash == "" {
			out = append(out, where+" carries no text hash, so nothing can tell whether the clause changed")
		}
		if g.Text == "" {
			out = append(out, where+" stores no text, so the annotation cannot be reread")
		}
		if g.AnnotatedBy == "" {
			out = append(out, where+" says who wrote it nowhere")
		}
		if g.StatesNoNorm && len(g.Statements) > 0 {
			out = append(out, where+" both states no norm and states one")
		}
		if !g.StatesNoNorm && len(g.Statements) == 0 {
			out = append(out, where+" is silent, which is neither an annotation nor a refusal")
		}
		if g.Duoc != "" && !contains(Senses, g.Duoc) {
			out = append(out, where+" gives được the sense "+g.Duoc+" which is not one of the senses")
		}
		for _, s := range g.Statements {
			if !contains(Types, s.Type) {
				out = append(out, where+" annotates the type "+s.Type+" which is not one of the types")
			}
			if s.Action == "" {
				out = append(out, where+" annotates a statement with no action")
			}
		}
	}
	sort.Strings(out)
	return out
}

// The confusion table and the accuracy live in eval, because the concept layer
// scores the same way and two copies of an arithmetic are two chances for a
// number in a report to mean something other than what the reader takes it to
// mean. These are aliases and not wrappers so the stored JSON is unchanged.
type (
	Count    = eval.Count
	Accuracy = eval.Accuracy
)

// Metrics is what the gold set says about an extraction pass.
type Metrics struct {
	Units      int      `json:"units"`
	Scored     int      `json:"scored"`
	Missing    []string `json:"missing,omitempty"`
	Stale      []string `json:"stale,omitempty"`
	Statements Count    `json:"statements"`
	Types      Accuracy `json:"types"`
	Bearers    Accuracy `json:"bearers"`
	// BearersMissed counts the annotated statements whose bearer the extraction
	// left empty. It is separate from Bearers because a wrong bearer and no
	// bearer are different failures with different fixes.
	BearersMissed int      `json:"bearers_missed"`
	Sanctions     Count    `json:"sanctions"`
	Deadlines     Count    `json:"deadlines"`
	StatesNoNorm  Count    `json:"states_no_norm"`
	Duoc          Accuracy `json:"duoc"`
	// DuocConfusion is annotated sense to extracted sense to count, which is
	// where the permission for right swap shows up as a number instead of as a
	// rate that quietly stays high.
	DuocConfusion map[string]map[string]int `json:"duoc_confusion,omitempty"`
}

// Score compares extracted records against the annotations.
//
// Only annotated clauses are scored, and a clause whose text changed since it
// was annotated is reported as stale rather than scored, because an annotation
// of words that have since been amended is not evidence about this corpus.
// The hash map is every unit the pass covered, unit identifier to the hash of
// the text it read. It is what tells a clause the pass never reached from one
// it reached and found nothing in, which are opposite results that look the
// same in the record set.
func Score(gold []Gold, records []Record, hash map[string]string) Metrics {
	m := Metrics{Units: len(gold), DuocConfusion: map[string]map[string]int{}}
	byUnit := map[string][]Record{}
	for _, r := range records {
		byUnit[r.ProvisionID] = append(byUnit[r.ProvisionID], r)
	}
	for i := range gold {
		g := &gold[i]
		h, covered := hash[g.UnitID]
		if !covered {
			m.Missing = append(m.Missing, g.UnitID)
			continue
		}
		if g.TextHash != "" && h != "" && h != g.TextHash {
			m.Stale = append(m.Stale, g.UnitID)
			continue
		}
		m.Scored++
		scoreUnit(&m, g, byUnit[g.UnitID])
	}
	sort.Strings(m.Missing)
	sort.Strings(m.Stale)
	return m
}

// String renders the metrics as a report.
//
// What was not scored is printed first. A recall figure over the eleven clauses
// the pass happened to reach, printed above nothing that says so, is read as a
// figure over the gold set, and that is how an evaluation ends up describing a
// run nobody made.
func (m Metrics) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "units          %d annotated, %d scored, %d not covered by the pass, %d annotated against text that has changed\n",
		m.Units, m.Scored, len(m.Missing), len(m.Stale))
	fmt.Fprintf(&b, "statements     precision %.3f recall %.3f f1 %.3f, from %d found, %d invented, %d missed\n",
		m.Statements.Precision(), m.Statements.Recall(), m.Statements.F1(),
		m.Statements.TP, m.Statements.FP, m.Statements.FN)
	fmt.Fprintf(&b, "types          %.3f of %d matched statements\n", m.Types.Rate(), m.Types.Of)
	fmt.Fprintf(&b, "bearers        %.3f of %d decided, %d left with nobody\n", m.Bearers.Rate(), m.Bearers.Of, m.BearersMissed)
	fmt.Fprintf(&b, "sanctions      precision %.3f recall %.3f\n", m.Sanctions.Precision(), m.Sanctions.Recall())
	fmt.Fprintf(&b, "deadlines      precision %.3f recall %.3f\n", m.Deadlines.Precision(), m.Deadlines.Recall())
	fmt.Fprintf(&b, "states no norm precision %.3f recall %.3f\n", m.StatesNoNorm.Precision(), m.StatesNoNorm.Recall())
	fmt.Fprintf(&b, "được           %.3f of %d clauses where the word was annotated\n", m.Duoc.Rate(), m.Duoc.Of)
	for _, want := range Senses {
		got := m.DuocConfusion[want]
		for _, had := range Senses {
			if want == had || got[had] == 0 {
				continue
			}
			fmt.Fprintf(&b, "               %d read as %s where the annotation says %s\n", got[had], had, want)
		}
	}
	return b.String()
}

func scoreUnit(m *Metrics, g *Gold, got []Record) {
	var verified []Record
	for _, r := range got {
		if r.Trusted() {
			verified = append(verified, r)
		}
	}
	switch {
	case g.StatesNoNorm && len(verified) == 0:
		m.StatesNoNorm.TP++
	case g.StatesNoNorm && len(verified) > 0:
		m.StatesNoNorm.FP++
	case !g.StatesNoNorm && len(verified) == 0:
		m.StatesNoNorm.FN++
	}

	// Statements are matched on the action, because the action is the part two
	// annotators write the same way. Type and bearer are then scored on the
	// matched pairs, which is what makes a wrong type a wrong type rather than
	// a miss and an invention at once.
	want := map[string]*GoldStatement{}
	for i := range g.Statements {
		want[law.Slug(g.Statements[i].Action)] = &g.Statements[i]
	}
	have := map[string]*Statement{}
	for i := range verified {
		have[law.Slug(verified[i].Statement.Action.Text)] = &verified[i].Statement
	}
	for slug, w := range want {
		s := have[slug]
		if s == nil {
			m.Statements.FN++
			continue
		}
		m.Statements.TP++
		m.Types.Of++
		if s.Type == w.Type {
			m.Types.Right++
		}
		switch {
		case w.Bearer == "":
		case s.Bearer == nil || s.Bearer.Text == "":
			m.BearersMissed++
		default:
			m.Bearers.Of++
			if law.Slug(s.Bearer.Text) == law.Slug(w.Bearer) {
				m.Bearers.Right++
			}
		}
		m.Sanctions.Observe(w.HasSanction, s.Sanction != nil)
		m.Deadlines.Observe(w.Deadline != "", s.Deadline != nil)
	}
	for slug := range have {
		if want[slug] == nil {
			m.Statements.FP++
		}
	}

	if g.Duoc == "" {
		return
	}
	predicted := duocSense(verified)
	m.Duoc.Of++
	if predicted == g.Duoc {
		m.Duoc.Right++
	}
	if m.DuocConfusion[g.Duoc] == nil {
		m.DuocConfusion[g.Duoc] = map[string]int{}
	}
	m.DuocConfusion[g.Duoc][predicted]++
}

// duocSense reads back which sense the extraction committed to, from the types
// of the statements whose evidence quotes the word.
//
// Nothing asks the model what sense it chose. A model asked to name its own
// reading names the one it thinks is wanted, and what matters is the type it
// actually wrote down, because that is what the graph carries and what every
// later query answers from.
//
// The order is prohibition, then permission, then right. A clause carrying
// "không được" states a prohibition whatever else it also states, and a clause
// that produced both a permission and a right was read as granting something,
// which is the reading the annotation is disagreeing with when it says the
// word was passive.
func duocSense(records []Record) string {
	found := map[string]bool{}
	for i := range records {
		s := &records[i].Statement
		if !strings.Contains(s.Evidence.Quote, "được") {
			continue
		}
		switch s.Type {
		case "prohibition":
			found[SenseProhibition] = true
		case "permission":
			found[SensePermission] = true
		case "right":
			found[SensePassiveRight] = true
		}
	}
	for _, sense := range []string{SenseProhibition, SensePermission, SensePassiveRight} {
		if found[sense] {
			return sense
		}
	}
	return SenseNone
}

func contains(set []string, s string) bool {
	for _, v := range set {
		if v == s {
			return true
		}
	}
	return false
}
