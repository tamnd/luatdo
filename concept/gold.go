package concept

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// The gold set is annotated before the pipeline runs over the same clauses, and
// never after. An annotation written while looking at what a model produced is
// an agreement rate with that model, not an accuracy measure, and the two are
// indistinguishable once written down. The sampling command draws the units,
// the annotation is done by hand against the clause text alone, and the reading
// pass is pointed at the same units afterwards.
//
// The set is small on purpose. Two hundred clauses annotated properly is worth
// more than two thousand annotated quickly, because the numbers this produces
// are the only evidence the layer has that its readings mean anything.

// GoldFile and GoldPairsFile are the two halves of the set: what each clause
// defines, and which readings across clauses are the same thing.
const (
	GoldFile      = "gold.jsonl"
	GoldPairsFile = "gold_pairs.jsonl"
)

// GoldTerm is one term a person says a clause defines. It carries only the
// fields a second annotator would agree on: the label, the genus as written,
// the kind, whether it is a role, and where the definition points when it
// points elsewhere. Definition wording is not scored, because two correct
// paraphrases of one definition differ and scoring them as a string would
// punish the right answer.
type GoldTerm struct {
	LabelVI            string   `json:"label_vi"`
	Genus              string   `json:"genus,omitempty"`
	Kind               string   `json:"kind"`
	IsRole             bool     `json:"is_role"`
	DefinesByReference string   `json:"defines_by_reference,omitempty"`
	EnumeratedSubtypes []string `json:"enumerated_subtypes,omitempty"`
	Aliases            []string `json:"aliases,omitempty"`
}

// Gold is one annotated clause.
type Gold struct {
	UnitID   string `json:"unit_id"`
	DocID    string `json:"doc_id"`
	ScopeID  string `json:"scope_id"`
	TextHash string `json:"text_hash"`
	// Text is stored with the annotation so a later run can tell that the
	// clause changed under it. An annotation against text that has since been
	// amended is not evidence about the current corpus.
	Text           string     `json:"text"`
	DefinesNothing bool       `json:"defines_nothing,omitempty"`
	Terms          []GoldTerm `json:"terms,omitempty"`
	AnnotatedBy    string     `json:"annotated_by"`
	AnnotatedAt    string     `json:"annotated_at"`
	Note           string     `json:"note,omitempty"`
}

// GoldPair is one annotated merge question.
type GoldPair struct {
	A           string `json:"a"`
	B           string `json:"b"`
	Verdict     string `json:"verdict"` // same, broader, narrower, or differs
	Rationale   string `json:"rationale"`
	AnnotatedBy string `json:"annotated_by"`
	AnnotatedAt string `json:"annotated_at"`
}

// ReadGold returns the annotated clauses.
func ReadGold(dir string) ([]Gold, error) { return readJSONL[Gold](filepath.Join(dir, GoldFile)) }

// ReadGoldPairs returns the annotated merge questions.
func ReadGoldPairs(dir string) ([]GoldPair, error) {
	return readJSONL[GoldPair](filepath.Join(dir, GoldPairsFile))
}

// WriteGold appends annotations.
func WriteGold(dir string, gs []Gold) error { return appendJSONL(filepath.Join(dir, GoldFile), gs) }

// WriteGoldPairs appends annotated merge questions.
func WriteGoldPairs(dir string, ps []GoldPair) error {
	return appendJSONL(filepath.Join(dir, GoldPairsFile), ps)
}

// CheckGold returns what is wrong with an annotation set, sorted. The gold set
// is the measuring stick, so it gets checked harder than the thing it measures:
// a typo in a kind name here would silently score every correct reading as
// wrong and the pipeline would look broken instead of the ruler.
func CheckGold(gs []Gold, pairs []GoldPair) []string {
	var out []string
	seen := map[string]bool{}
	terms := map[string]bool{}
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
		if g.DefinesNothing && len(g.Terms) > 0 {
			out = append(out, where+" both defines nothing and defines something")
		}
		if !g.DefinesNothing && len(g.Terms) == 0 {
			out = append(out, where+" is silent, which is neither an annotation nor a refusal")
		}
		labels := map[string]bool{}
		for _, term := range g.Terms {
			label := law.Slug(term.LabelVI)
			switch {
			case term.LabelVI == "":
				out = append(out, where+" annotates a term with no label")
			case label == "":
				out = append(out, where+" annotates the label "+term.LabelVI+" which slugs to nothing")
			case labels[label]:
				out = append(out, where+" annotates "+term.LabelVI+" twice")
			}
			labels[label] = true
			terms[TermUseID(g.ScopeID, term.LabelVI)] = true
			if !ValidKind(term.Kind) {
				out = append(out, where+" gives "+term.LabelVI+" the kind "+term.Kind+" which is not one of the kinds")
			}
			if term.Genus != "" && !contains(g.Text, term.Genus) {
				out = append(out, where+" gives "+term.LabelVI+" a genus the clause does not contain")
			}
			for _, sub := range term.EnumeratedSubtypes {
				if !contains(g.Text, sub) {
					out = append(out, where+" lists the subtype "+sub+" which the clause does not contain")
				}
			}
		}
	}
	for _, p := range pairs {
		switch {
		case !terms[p.A]:
			out = append(out, "the pair "+p.A+" and "+p.B+" names "+p.A+" which no annotation defines")
		case !terms[p.B]:
			out = append(out, "the pair "+p.A+" and "+p.B+" names "+p.B+" which no annotation defines")
		}
		switch p.Verdict {
		case RelationSame, RelationBroader, RelationNarrower, RelationDiffers:
		default:
			out = append(out, "the pair "+p.A+" and "+p.B+" has the verdict "+p.Verdict+" which is not a verdict")
		}
		if p.Rationale == "" {
			out = append(out, "the pair "+p.A+" and "+p.B+" was decided without a reason")
		}
		if p.AnnotatedBy == "" {
			out = append(out, "the pair "+p.A+" and "+p.B+" says who decided it nowhere")
		}
	}
	sort.Strings(out)
	return out
}

// Count is one confusion table. It is kept as counts rather than a rate so a
// report can say ninety percent of ten and ninety percent of two thousand
// differently.
type Count struct {
	TP int `json:"tp"`
	FP int `json:"fp"`
	FN int `json:"fn"`
}

// Precision, Recall and F1 return zero rather than a division by zero when
// nothing was predicted or nothing was there to find. Zero is honest here: a
// pass that found nothing has no precision to report.
func (c Count) Precision() float64 { return ratio(c.TP, c.TP+c.FP) }
func (c Count) Recall() float64    { return ratio(c.TP, c.TP+c.FN) }
func (c Count) F1() float64 {
	p, r := c.Precision(), c.Recall()
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

// Accuracy is right out of decided, over the cases where both the annotation
// and the reading had an opinion.
type Accuracy struct {
	Right int `json:"right"`
	Of    int `json:"of"`
}

func (a Accuracy) Rate() float64 { return ratio(a.Right, a.Of) }

// Metrics is what the gold set says about a reading pass.
type Metrics struct {
	Units          int      `json:"units"`
	Scored         int      `json:"scored"`
	Missing        []string `json:"missing,omitempty"`
	Stale          []string `json:"stale,omitempty"`
	Definitions    Count    `json:"definitions"`
	Genus          Accuracy `json:"genus"`
	Kind           Accuracy `json:"kind"`
	Role           Accuracy `json:"role"`
	RolesMissed    int      `json:"roles_missed"`
	RolesInvented  int      `json:"roles_invented"`
	ByReference    Count    `json:"by_reference"`
	Enumerations   Count    `json:"enumerations"`
	DefinesNothing Count    `json:"defines_nothing"`
	Paraphrased    int      `json:"paraphrased"`
}

// Score compares readings against the annotations. Only clauses that were
// annotated are scored, and a clause whose text has changed since it was
// annotated is reported as stale rather than scored against the old reading.
func Score(gold []Gold, jobs []Job) Metrics {
	m := Metrics{Units: len(gold)}
	byUnit := map[string]*Job{}
	for i := range jobs {
		byUnit[jobs[i].UnitID] = &jobs[i]
	}

	for i := range gold {
		g := &gold[i]
		job := byUnit[g.UnitID]
		if job == nil {
			m.Missing = append(m.Missing, g.UnitID)
			continue
		}
		if g.TextHash != "" && job.TextHash != "" && g.TextHash != job.TextHash {
			m.Stale = append(m.Stale, g.UnitID)
			continue
		}
		m.Scored++
		scoreUnit(&m, g, job)
	}
	sort.Strings(m.Missing)
	sort.Strings(m.Stale)
	return m
}

func scoreUnit(m *Metrics, g *Gold, job *Job) {
	// A clause that defines nothing is scored as its own decision, because
	// getting it wrong in either direction is a different failure: a missed
	// clause loses vocabulary, and an invented one puts a term in the graph
	// that no law defines.
	switch {
	case g.DefinesNothing && job.DefinesNo:
		m.DefinesNothing.TP++
	case g.DefinesNothing && !job.DefinesNo:
		m.DefinesNothing.FN++
	case !g.DefinesNothing && job.DefinesNo:
		m.DefinesNothing.FP++
	}

	want := map[string]*GoldTerm{}
	for i := range g.Terms {
		want[law.Slug(g.Terms[i].LabelVI)] = &g.Terms[i]
	}
	got := map[string]*TermUse{}
	for i := range job.TermUses {
		got[law.Slug(job.TermUses[i].LabelVI)] = &job.TermUses[i]
	}
	// An alias the annotation records counts as a hit on the term it names, so
	// a reading that took the drafter's short form as the label is right rather
	// than wrong twice.
	for slug, w := range want {
		if got[slug] != nil {
			continue
		}
		for _, a := range w.Aliases {
			if t := got[law.Slug(a)]; t != nil {
				got[slug] = t
				delete(got, law.Slug(a))
				break
			}
		}
	}

	for slug, w := range want {
		t := got[slug]
		if t == nil {
			m.Definitions.FN++
			if w.IsRole {
				m.RolesMissed++
			}
			continue
		}
		m.Definitions.TP++
		scoreTerm(m, w, t)
	}
	for slug := range got {
		if want[slug] == nil {
			m.Definitions.FP++
			if got[slug].IsRole {
				m.RolesInvented++
			}
		}
	}
}

func scoreTerm(m *Metrics, w *GoldTerm, t *TermUse) {
	if w.Genus != "" {
		m.Genus.Of++
		// The genus is scored on containment rather than equality. An
		// annotator writes the genus as the shortest phrase that names the
		// category, and a reading that took a longer span of the same phrase
		// has got it right.
		if t.Genus != "" && (contains(t.Genus, w.Genus) || contains(w.Genus, t.Genus)) {
			m.Genus.Right++
		}
	}
	if w.Kind != "" {
		m.Kind.Of++
		if t.Kind == w.Kind {
			m.Kind.Right++
		}
	}
	m.Role.Of++
	switch {
	case w.IsRole == t.IsRole:
		m.Role.Right++
	case w.IsRole:
		m.RolesMissed++
	default:
		m.RolesInvented++
	}

	switch {
	case w.DefinesByReference != "" && t.DefinesByReference != nil:
		m.ByReference.TP++
		// A pointer definition that came back with a definition attached is a
		// paraphrase of a document the model was never shown. Validate rejects
		// it, so a count above zero here means something bypassed the reader.
		if t.DefinitionVI != "" {
			m.Paraphrased++
		}
	case w.DefinesByReference != "":
		m.ByReference.FN++
	case t.DefinesByReference != nil:
		m.ByReference.FP++
	}

	if len(w.EnumeratedSubtypes) > 0 || len(t.EnumeratedSubtypes) > 0 {
		have := map[string]bool{}
		for _, s := range t.EnumeratedSubtypes {
			have[law.Slug(s)] = true
		}
		seen := map[string]bool{}
		for _, s := range w.EnumeratedSubtypes {
			seen[law.Slug(s)] = true
			if have[law.Slug(s)] {
				m.Enumerations.TP++
			} else {
				m.Enumerations.FN++
			}
		}
		for s := range have {
			if !seen[s] {
				m.Enumerations.FP++
			}
		}
	}
}

// MergeMetrics is what the annotated pairs say about the comparison pass.
//
// It scores the model's advice and not the graph, because the graph only ever
// contains merges a person decided. Over merge is the number the advice would
// have wrongly joined and under merge the number it would have wrongly left
// apart, and they are counted separately because they are not equally bad: an
// over merge silently destroys a distinction, while an under merge leaves two
// nodes a later pass can still join.
type MergeMetrics struct {
	Pairs      int      `json:"pairs"`
	Scored     int      `json:"scored"`
	Agreed     int      `json:"agreed"`
	OverMerge  int      `json:"over_merge"`
	UnderMerge int      `json:"under_merge"`
	Direction  int      `json:"direction"` // right about the relation, wrong about which way
	Unclear    int      `json:"unclear"`
	Missing    []string `json:"missing,omitempty"`
}

// ScoreMerges compares the model's comparisons against the annotated pairs.
func ScoreMerges(gold []GoldPair, comparisons []Comparison) MergeMetrics {
	m := MergeMetrics{Pairs: len(gold)}
	byPair := map[[2]string]*Comparison{}
	for i := range comparisons {
		c := &comparisons[i]
		byPair[[2]string{c.A, c.B}] = c
		byPair[[2]string{c.B, c.A}] = c
	}

	for _, g := range gold {
		c := byPair[[2]string{g.A, g.B}]
		if c == nil {
			m.Missing = append(m.Missing, g.A+" vs "+g.B)
			continue
		}
		m.Scored++
		relation := c.Relation
		if c.B == g.A {
			// The comparison was made in the other order, so broader and
			// narrower swap. Reading them the way they were written would
			// score a correct answer as a mistake.
			relation = flip(relation)
		}
		switch {
		case relation == RelationUnclear:
			m.Unclear++
		case relation == g.Verdict:
			m.Agreed++
		case g.Verdict == RelationDiffers:
			m.OverMerge++
		case relation == RelationDiffers:
			m.UnderMerge++
		default:
			m.Direction++
		}
	}
	sort.Strings(m.Missing)
	return m
}

func flip(relation string) string {
	switch relation {
	case RelationBroader:
		return RelationNarrower
	case RelationNarrower:
		return RelationBroader
	default:
		return relation
	}
}

// String renders the metrics the way the campaign report prints them.
func (m Metrics) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "gold set   %d units, %d scored", m.Units, m.Scored)
	if len(m.Missing) > 0 {
		fmt.Fprintf(&b, ", %d never read", len(m.Missing))
	}
	if len(m.Stale) > 0 {
		fmt.Fprintf(&b, ", %d annotated against text that has since changed", len(m.Stale))
	}
	fmt.Fprintf(&b, "\ndefinitions precision %s recall %s f1 %s, from %d right, %d invented, %d missed\n",
		pct(m.Definitions.Precision()), pct(m.Definitions.Recall()), pct(m.Definitions.F1()),
		m.Definitions.TP, m.Definitions.FP, m.Definitions.FN)
	fmt.Fprintf(&b, "genus      %s of %d\n", pct(m.Genus.Rate()), m.Genus.Of)
	fmt.Fprintf(&b, "kind       %s of %d\n", pct(m.Kind.Rate()), m.Kind.Of)
	fmt.Fprintf(&b, "role       %s of %d, %d roles missed, %d invented\n",
		pct(m.Role.Rate()), m.Role.Of, m.RolesMissed, m.RolesInvented)
	fmt.Fprintf(&b, "by reference precision %s recall %s, %d paraphrased\n",
		pct(m.ByReference.Precision()), pct(m.ByReference.Recall()), m.Paraphrased)
	fmt.Fprintf(&b, "enumerations precision %s recall %s\n",
		pct(m.Enumerations.Precision()), pct(m.Enumerations.Recall()))
	fmt.Fprintf(&b, "defines nothing %d right, %d invented, %d missed",
		m.DefinesNothing.TP, m.DefinesNothing.FP, m.DefinesNothing.FN)
	return b.String()
}

// String renders the merge metrics.
func (m MergeMetrics) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "merge      %d pairs, %d scored", m.Pairs, m.Scored)
	if len(m.Missing) > 0 {
		fmt.Fprintf(&b, ", %d never compared", len(m.Missing))
	}
	fmt.Fprintf(&b, "\n           %d agreed, %d over merge, %d under merge, %d wrong direction, %d unclear",
		m.Agreed, m.OverMerge, m.UnderMerge, m.Direction, m.Unclear)
	return b.String()
}

func contains(haystack, needle string) bool {
	return strings.Contains(law.Slug(haystack), law.Slug(needle))
}

func ratio(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) / float64(of)
}

func pct(r float64) string { return fmt.Sprintf("%.1f%%", r*100) }
