package event

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/eval"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/store"
)

// The event gold set is annotated before the pass runs over the same provisions,
// and never after, for the reason every gold set in this repository is: an
// annotation written while looking at what a model produced measures agreement
// with that model rather than accuracy, and the two are indistinguishable once
// they are written down.

// GoldFile is the annotated provision file inside the event gold directory.
const GoldFile = "gold_events.jsonl"

// GoldPath is where one campaign's annotations live. A campaign named here gets
// its own file, because a tax gold set appended to a labour one measures
// neither.
func GoldPath(dir, campaign string) string {
	if campaign == "" {
		return filepath.Join(dir, GoldFile)
	}
	return filepath.Join(dir, "gold_events_"+campaign+".jsonl")
}

// GoldParticipant is one party a person says takes part in an act.
type GoldParticipant struct {
	Role string `json:"role"`
	// LabelVI is the party as a concept label, matched by slug. The phrase as
	// the drafter wrote it is not scored, because two annotators writing the
	// same party differ in wording and scoring the wording punishes both.
	LabelVI string `json:"label_vi"`
}

// GoldEvent is one act a person says a provision names.
type GoldEvent struct {
	Class   string `json:"class"`
	LabelVI string `json:"label_vi"`
	// Participants may be left empty, and an empty list means the annotator did
	// not annotate roles for this act rather than that the act has no parties.
	// The two are told apart by RolesAnnotated on the provision.
	Participants []GoldParticipant `json:"participants,omitempty"`
}

// GoldChain is one act to act relation a person says the provision states.
type GoldChain struct {
	FromLabel string `json:"from_label"`
	ToLabel   string `json:"to_label"`
	Type      string `json:"type"`
}

// Gold is one annotated provision.
type Gold struct {
	UnitID   string `json:"unit_id"`
	DocID    string `json:"doc_id"`
	TextHash string `json:"text_hash"`
	// Text is stored with the annotation so a later run can tell that the
	// provision changed under it. An annotation against words that have since
	// been amended is not evidence about the current corpus.
	Text string `json:"text"`
	// NamesNoAct is the annotator saying this provision names no act at all,
	// which is a common and correct answer for a definition or a scope clause.
	NamesNoAct bool        `json:"names_no_act,omitempty"`
	Events     []GoldEvent `json:"events,omitempty"`
	Chains     []GoldChain `json:"chains,omitempty"`
	// RolesAnnotated says the participant lists on this provision are complete,
	// so a party the pass found and the annotation does not hold counts against
	// the pass. Without it the roles are scored only where the annotator wrote
	// some, and a missing party is not held against anybody.
	RolesAnnotated bool   `json:"roles_annotated,omitempty"`
	AnnotatedBy    string `json:"annotated_by"`
	AnnotatedAt    string `json:"annotated_at"`
	Note           string `json:"note,omitempty"`
}

// ReadGold returns one campaign's annotated provisions.
func ReadGold(dir, campaign string) ([]Gold, error) {
	return store.ReadJSONL[Gold](GoldPath(dir, campaign))
}

// WriteGold appends annotations.
func WriteGold(dir, campaign string, gs []Gold) error {
	return store.AppendJSONL(GoldPath(dir, campaign), gs)
}

// CheckGold returns what is wrong with an annotation set, sorted.
//
// The gold set is the measuring stick, so it is checked harder than the thing it
// measures. A role typo here scores every correct extraction as wrong, and the
// pipeline then looks broken instead of the ruler.
func CheckGold(gs []Gold) []string {
	var out []string
	seen := map[string]bool{}
	for _, g := range gs {
		where := g.UnitID
		if where == "" {
			out = append(out, "an annotation names no provision")
			continue
		}
		if seen[where] {
			out = append(out, where+" is annotated twice")
		}
		seen[where] = true
		if g.TextHash == "" {
			out = append(out, where+" carries no text hash, so nothing can tell whether the provision changed")
		}
		if g.Text == "" {
			out = append(out, where+" stores no text, so the annotation cannot be reread")
		}
		if g.AnnotatedBy == "" {
			out = append(out, where+" says who wrote it nowhere")
		}
		if g.NamesNoAct && len(g.Events) > 0 {
			out = append(out, where+" both names no act and names one")
		}
		if !g.NamesNoAct && len(g.Events) == 0 {
			out = append(out, where+" is silent, which is neither an annotation nor a refusal")
		}
		labels := map[string]bool{}
		for _, e := range g.Events {
			if e.LabelVI == "" {
				out = append(out, where+" annotates an act with no label")
				continue
			}
			labels[law.Slug(e.LabelVI)] = true
			if e.Class == "" {
				out = append(out, where+" annotates "+e.LabelVI+" with no class")
			}
			if Forbidden[strings.ToUpper(e.Class)] {
				out = append(out, where+" annotates "+e.LabelVI+" with the generic class "+e.Class)
			}
			for _, p := range e.Participants {
				if !slices.Contains(Roles, p.Role) {
					out = append(out, where+" annotates the role "+p.Role+" which is not one of the roles")
				}
				if p.LabelVI == "" {
					out = append(out, where+" annotates a "+p.Role+" with no label")
				}
			}
		}
		for _, c := range g.Chains {
			if !ValidChain(c.Type) {
				out = append(out, where+" annotates the chain type "+c.Type+" which is not one of the chain types")
			}
			if !labels[law.Slug(c.FromLabel)] || !labels[law.Slug(c.ToLabel)] {
				out = append(out, where+" annotates a chain between acts it does not annotate")
			}
			if law.Slug(c.FromLabel) == law.Slug(c.ToLabel) {
				out = append(out, where+" annotates a chain from an act to itself")
			}
		}
	}
	sort.Strings(out)
	return out
}

// The counters live in eval, because three layers score the same way and three
// copies of one arithmetic are three chances for a number in a report to mean
// something other than what a reader takes it to mean.
type (
	Count    = eval.Count
	Accuracy = eval.Accuracy
)

// Metrics is what the gold set says about an extraction pass.
//
// Four numbers, never folded into one. Events say whether the acts were found,
// classes say whether they were typed, roles say whether the right parties were
// attached in the right slots, and chain direction says whether the arrows point
// the way the text runs. A layer can be excellent at the first and useless at
// the last, and a single headline figure hides exactly that.
type Metrics struct {
	Units   int      `json:"units"`
	Scored  int      `json:"scored"`
	Missing []string `json:"missing,omitempty"`
	Stale   []string `json:"stale,omitempty"`

	Events  Count    `json:"events"`
	Classes Accuracy `json:"classes"`
	// ClassConfusion is annotated class to extracted class to count, over the
	// acts that matched, which is where a systematic swap shows up as a number
	// rather than as a rate that quietly stays high.
	ClassConfusion map[string]map[string]int `json:"class_confusion,omitempty"`

	Roles Accuracy `json:"roles"`
	// RolesMissed counts annotated participants the pass did not attach at all,
	// kept apart from Roles because a wrong slot and no party are different
	// failures with different fixes.
	RolesMissed int `json:"roles_missed"`
	// RolesInvented counts parties the pass attached that the annotation does
	// not hold, over the provisions whose roles were annotated in full.
	RolesInvented int `json:"roles_invented"`

	// Chains is scored on the pair of acts without regard to order, so finding
	// the connection and pointing it the right way are two results.
	Chains         Count    `json:"chains"`
	ChainTypes     Accuracy `json:"chain_types"`
	ChainDirection Accuracy `json:"chain_direction"`

	NamesNoAct Count `json:"names_no_act"`
}

// Score compares one pass against the annotations.
//
// Only annotated provisions are scored. A provision whose text changed since it
// was annotated is reported as stale rather than scored, and a provision the
// pass never reached is reported as missing rather than as a miss, because a
// recall figure that counts unread provisions as failures describes a run
// nobody made. The hash map is every provision the pass covered, provision
// identifier to the hash of the text it read.
func Score(gold []Gold, occurrences []Occurrence, chains []Chain, hash map[string]string) Metrics {
	m := Metrics{Units: len(gold), ClassConfusion: map[string]map[string]int{}}

	byUnit := map[string][]Occurrence{}
	label := map[string]string{}
	for _, o := range occurrences {
		byUnit[o.Evidence.ProvisionID] = append(byUnit[o.Evidence.ProvisionID], o)
		label[o.EventID] = o.LabelVI
	}
	// A chain is scored against every provision that supports it, so a folded
	// chain corroborated by three provisions is scored in all three rather than
	// counting as two misses and one hit.
	chainsByUnit := map[string][]Chain{}
	for _, c := range chains {
		for _, ev := range c.Evidence {
			chainsByUnit[ev.ProvisionID] = append(chainsByUnit[ev.ProvisionID], c)
		}
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
		scoreUnit(&m, g, byUnit[g.UnitID], chainsByUnit[g.UnitID], label)
	}
	sort.Strings(m.Missing)
	sort.Strings(m.Stale)
	return m
}

func scoreUnit(m *Metrics, g *Gold, got []Occurrence, chains []Chain, label map[string]string) {
	switch {
	case g.NamesNoAct && len(got) == 0:
		m.NamesNoAct.TP++
	case g.NamesNoAct && len(got) > 0:
		m.NamesNoAct.FP++
	case !g.NamesNoAct && len(got) == 0:
		m.NamesNoAct.FN++
	}

	// Acts are matched on the label alone. Matching on class as well would make
	// one act with the wrong class count as a miss and an invention at once,
	// which double counts a single mistake and hides that the act was found.
	want := map[string]*GoldEvent{}
	for i := range g.Events {
		want[law.Slug(g.Events[i].LabelVI)] = &g.Events[i]
	}
	have := map[string]*Occurrence{}
	for i := range got {
		have[law.Slug(got[i].LabelVI)] = &got[i]
	}
	for slug, w := range want {
		o := have[slug]
		if o == nil {
			m.Events.FN++
			continue
		}
		m.Events.TP++
		m.Classes.Observe(strings.EqualFold(o.Class, w.Class))
		if m.ClassConfusion[w.Class] == nil {
			m.ClassConfusion[w.Class] = map[string]int{}
		}
		m.ClassConfusion[w.Class][o.Class]++
		scoreRoles(m, g, w, o)
	}
	for slug := range have {
		if want[slug] == nil {
			m.Events.FP++
		}
	}

	scoreChains(m, g, chains, label)
}

func scoreRoles(m *Metrics, g *Gold, w *GoldEvent, o *Occurrence) {
	if len(w.Participants) == 0 && !g.RolesAnnotated {
		return
	}
	// Parties are matched on the concept label and the role is then scored on
	// the match, so a party found and filed in the wrong slot is a role error
	// and not a missing party. The two have different fixes: the first is a
	// prompt problem and the second is a concept coverage problem.
	byLabel := map[string]string{}
	for _, p := range o.Participants {
		byLabel[law.Slug(p.LabelVI)] = p.Role
	}
	seen := map[string]bool{}
	for _, p := range w.Participants {
		slug := law.Slug(p.LabelVI)
		seen[slug] = true
		role, found := byLabel[slug]
		if !found {
			m.RolesMissed++
			continue
		}
		m.Roles.Observe(role == p.Role)
	}
	if !g.RolesAnnotated {
		return
	}
	for slug := range byLabel {
		if !seen[slug] {
			m.RolesInvented++
		}
	}
}

func scoreChains(m *Metrics, g *Gold, got []Chain, label map[string]string) {
	if len(g.Chains) == 0 && len(got) == 0 {
		return
	}
	want := map[string]*GoldChain{}
	for i := range g.Chains {
		want[unordered(law.Slug(g.Chains[i].FromLabel), law.Slug(g.Chains[i].ToLabel))] = &g.Chains[i]
	}
	have := map[string]Chain{}
	for _, c := range got {
		from, to := law.Slug(label[c.FromID]), law.Slug(label[c.ToID])
		if from == "" || to == "" {
			continue
		}
		have[unordered(from, to)] = c
	}
	for key, w := range want {
		c, found := have[key]
		if !found {
			m.Chains.FN++
			continue
		}
		m.Chains.TP++
		m.ChainTypes.Observe(c.Type == w.Type)
		m.ChainDirection.Observe(law.Slug(label[c.FromID]) == law.Slug(w.FromLabel))
	}
	for key := range have {
		if want[key] == nil {
			m.Chains.FP++
		}
	}
}

func unordered(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

// String renders the metrics as a report.
//
// What was not scored is printed first. A recall figure over the eleven
// provisions the pass happened to reach, printed above nothing that says so, is
// read as a figure over the gold set.
func (m Metrics) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "units          %d annotated, %d scored, %d not covered by the pass, %d annotated against text that has changed\n",
		m.Units, m.Scored, len(m.Missing), len(m.Stale))
	fmt.Fprintf(&b, "events         precision %.3f recall %.3f f1 %.3f, from %d found, %d invented, %d missed\n",
		m.Events.Precision(), m.Events.Recall(), m.Events.F1(), m.Events.TP, m.Events.FP, m.Events.FN)
	fmt.Fprintf(&b, "classes        %.3f of %d matched acts\n", m.Classes.Rate(), m.Classes.Of)
	fmt.Fprintf(&b, "roles          %.3f of %d matched parties, %d parties missed, %d invented\n",
		m.Roles.Rate(), m.Roles.Of, m.RolesMissed, m.RolesInvented)
	fmt.Fprintf(&b, "chains         precision %.3f recall %.3f, from %d found, %d invented, %d missed\n",
		m.Chains.Precision(), m.Chains.Recall(), m.Chains.TP, m.Chains.FP, m.Chains.FN)
	fmt.Fprintf(&b, "chain types    %.3f of %d matched chains\n", m.ChainTypes.Rate(), m.ChainTypes.Of)
	fmt.Fprintf(&b, "chain arrows   %.3f of %d matched chains, scored on its own because a chain found and pointed backwards is worse than one not found\n",
		m.ChainDirection.Rate(), m.ChainDirection.Of)
	fmt.Fprintf(&b, "names no act   precision %.3f recall %.3f\n", m.NamesNoAct.Precision(), m.NamesNoAct.Recall())
	for _, want := range sortedCounts(flatten(m.ClassConfusion)) {
		for had, n := range m.ClassConfusion[want] {
			if had == want {
				continue
			}
			fmt.Fprintf(&b, "               %d read as %s where the annotation says %s\n", n, had, want)
		}
	}
	return b.String()
}

func flatten(in map[string]map[string]int) map[string]int {
	out := map[string]int{}
	for want, row := range in {
		for _, n := range row {
			out[want] += n
		}
	}
	return out
}
