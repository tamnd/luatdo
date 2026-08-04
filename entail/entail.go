// Package entail is the cheap entailment gate, stage 5 of the verification
// pipeline.
//
// Every statement this project has ever verified went straight to a strong
// model, which is why the judge's cost sets the ceiling on how much can be
// verified at all. The survey's answer, and the French CamemBERT result behind
// it, is that the expensive model manufactures the supervision and a small model
// does the inference. The judge has already produced a thousand verdicts on this
// corpus. This package learns from them.
//
// The student is the averaged perceptron in distill/, on a different task. There
// it decides whether a phrase is a concept; here it decides whether a provision
// supports a statement. The learning is shared and the features are not, because
// the two tasks look at nothing alike.
//
// What the gate is allowed to do is deliberately narrow. It sorts statements
// into three trays: the ones it is confident the judge would accept, the ones it
// is confident the judge would reject, and everything else, which is the tray the
// judge still reads. The two confident trays are what the milestone is for, and
// their edges are calibrated against a stated budget for the mistake each one
// makes rather than set to a round number that sounded safe.
//
// The gate is measured against two different things and they are two different
// questions, exactly as the tagger is. Against held out judge verdicts it
// measures whether distillation worked, which is agreement. Against the human
// labels in eval/ it measures whether either of them is right. A gate that
// agrees with the judge and is wrong has distilled a judge that is wrong, and
// only reporting both says so.
package entail

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/distill"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
)

// ModelFile is where a trained gate lives inside the eval store.
const ModelFile = "entail_gate.json"

// The two things a labelled instance can have come from. They are never mixed
// in one training set: a gate fitted to the human labels cannot then be scored
// against them, and a file that does not say what it learned from is one command
// away from being scored that way.
const (
	SourceJudge = "judge"
	SourceHuman = "human"
)

// Instance is one labelled pair, reduced to the features the gate can see.
//
// The features are computed once and stored rather than recomputed at every
// fold, because a cross validation that recomputes them is a cross validation
// that can quietly change them halfway through.
type Instance struct {
	ProvisionID string   `json:"provision_id"`
	RecordID    string   `json:"record_id"`
	Features    []string `json:"features"`
	Entailed    bool     `json:"entailed"`
	Source      string   `json:"source"`
}

// Make reduces one statement and the text it claims to come from to an instance.
func Make(provisionID, recordID, text string, s *norm.Statement, entailed bool, source string) Instance {
	return Instance{
		ProvisionID: provisionID, RecordID: recordID,
		Features: Features(text, s), Entailed: entailed, Source: source,
	}
}

// The modality surface forms Vietnamese legal drafting uses. They are the same
// list the omission audit reads, because a gate that weighs one vocabulary while
// the audit counts another would report two different things about one corpus.
var markers = []struct{ form, slug string }{
	{"phải", "phai"},
	{"nghiêm cấm", "nghiem-cam"},
	{"không được", "khong-duoc"},
	{"có trách nhiệm", "co-trach-nhiem"},
	{"có quyền", "co-quyen"},
	{"được phép", "duoc-phep"},
	{"được quyền", "duoc-quyen"},
}

// Features turns one provision and one proposed statement into the strings the
// gate weighs.
//
// Everything here is computed from the pair alone. Nothing consults the
// registry, the concept layer or any other record, because the gate has to run
// inside the extraction pass on a statement that exists nowhere yet.
//
// The features are of three kinds. What the statement claims about itself, which
// is its type and modality and which slots it filled. Whether its words are in
// the provision at all, which is the cheap approximation of grounding and the
// one the survey's decompose and check design turns on. And the crossings of the
// two, which is where the failures this gate is supposed to catch actually live:
// a prohibition read out of a provision with no negation in it, or a duty whose
// bearer appears nowhere in the words.
func Features(text string, s *norm.Statement) []string {
	folded := "-" + law.Slug(text) + "-"
	f := []string{
		"bias",
		"type=" + s.Type,
		"modality=" + or(s.Modality, "none"),
		"type+modality=" + s.Type + "/" + or(s.Modality, "none"),
	}

	quote := s.Evidence.Quote
	f = append(f,
		"quote_verbatim="+yesno(quote != "" && strings.Contains(text, quote)),
		"quote_words="+words(quote),
		"text_words="+words(text),
	)

	f = append(f, ref(folded, "action", &s.Action)...)
	f = append(f, ref(folded, "bearer", s.Bearer)...)
	f = append(f, ref(folded, "object", s.Object)...)
	f = append(f, ref(folded, "counterparty", s.Counterparty)...)

	// The whole claim against the whole provision. The per slot coverage above
	// says which part is ungrounded, and this says how much of the claim is.
	all := cover(folded, s.Action.Text, refText(s.Bearer), refText(s.Object), refText(s.Counterparty))
	f = append(f, "claim_cover="+band(all), "type+claim_cover="+s.Type+"/"+band(all))

	f = append(f,
		"conditions="+count(len(s.Conditions)),
		"exceptions="+count(len(s.Exceptions)),
		"deadline="+yesno(s.Deadline != nil),
		"sanction="+yesno(s.Sanction != nil),
	)
	if s.Deadline != nil {
		f = append(f, "deadline_in_text="+yesno(strings.Contains(text, s.Deadline.Text)))
	}
	if s.Sanction != nil {
		f = append(f,
			"sanction_quote_in_text="+yesno(s.Sanction.Quote != "" && strings.Contains(text, s.Sanction.Quote)),
			"sanction_basis="+yesno(s.Sanction.LegalBasis != ""))
	}
	for _, c := range s.Conditions {
		f = append(f, "condition_kind="+c.Kind, "condition_quote_in_text="+yesno(c.Quote != "" && strings.Contains(text, c.Quote)))
	}
	for _, e := range s.Exceptions {
		f = append(f, "exception_kind="+e.Kind, "exception_quote_in_text="+yesno(e.Quote != "" && strings.Contains(text, e.Quote)))
	}

	seen := false
	for _, m := range markers {
		if !strings.Contains(folded, "-"+m.slug+"-") {
			continue
		}
		seen = true
		f = append(f, "text_has="+m.slug, "type+text_has="+s.Type+"/"+m.slug)
	}
	if !seen {
		f = append(f, "text_has=none", "type+text_has="+s.Type+"/none")
	}
	// The two crossings worth naming. A prohibition drawn out of a provision that
	// negates nothing is the error the falsification judge was written for, and a
	// duty drawn out of a provision with no obligation marker is the drafting
	// convention argument that half the sampled disagreements turned out to be.
	if s.Type == "prohibition" && !strings.Contains(folded, "-khong-duoc-") && !strings.Contains(folded, "-cam-") {
		f = append(f, "prohibition_without_negation")
	}
	if s.Type == "duty" && !strings.Contains(folded, "-phai-") && !strings.Contains(folded, "-co-trach-nhiem-") {
		f = append(f, "duty_without_obligation_marker")
	}

	f = append(f, "confidence="+confidence(s.Confidence))
	return f
}

// ref is the features of one participant slot, including its absence. An empty
// slot is a fact about the statement and gets a feature of its own, because
// "this duty names nobody" is exactly the kind of thing the judge rejects.
func ref(folded, slot string, r *norm.Ref) []string {
	if r == nil || strings.TrimSpace(r.Text) == "" {
		return []string{slot + "=none"}
	}
	c := cover(folded, r.Text)
	return []string{
		slot + "=present",
		slot + "_cover=" + band(c),
		slot + "_class=" + or(r.ClassID, "none"),
		slot + "_actor=" + yesno(r.IsActor),
	}
}

func refText(r *norm.Ref) string {
	if r == nil {
		return ""
	}
	return r.Text
}

// cover is the share of the claim's words that are in the provision at all.
//
// It folds both sides through law.Slug, so diacritics and casing cannot make a
// word that is there look like a word that is not, and it matches whole folded
// words rather than substrings, so "kho" does not find itself inside "khong".
func cover(folded string, phrases ...string) float64 {
	found, total := 0, 0
	for _, p := range phrases {
		for _, w := range strings.Split(law.Slug(p), "-") {
			if w == "" {
				continue
			}
			total++
			if strings.Contains(folded, "-"+w+"-") {
				found++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(found) / float64(total)
}

// band buckets a rate. The buckets are coarse on purpose: a perceptron over a
// continuous feature learns a threshold nobody can see, and five named buckets
// are five weights somebody can read off the model file and argue with.
func band(r float64) string {
	switch {
	case r >= 1:
		return "all"
	case r >= 0.75:
		return "most"
	case r >= 0.5:
		return "half"
	case r > 0:
		return "some"
	}
	return "none"
}

func count(n int) string {
	switch {
	case n == 0:
		return "0"
	case n == 1:
		return "1"
	case n <= 3:
		return "2-3"
	}
	return "4+"
}

func words(s string) string {
	n := len(strings.Fields(s))
	switch {
	case n == 0:
		return "0"
	case n <= 5:
		return "1-5"
	case n <= 15:
		return "6-15"
	case n <= 40:
		return "16-40"
	case n <= 120:
		return "41-120"
	}
	return "120+"
}

func confidence(c float64) string {
	switch {
	case c >= 0.9:
		return "high"
	case c >= 0.7:
		return "medium"
	case c > 0:
		return "low"
	}
	return "unstated"
}

func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// Gate is the trained student and the two edges it decides on.
type Gate struct {
	Weights map[string]float64 `json:"weights"`
	// Accept is the score at or above which a statement is taken as entailed
	// without a judge call, and Reject the score at or below which it is taken as
	// not entailed. Between them is the tray the judge reads.
	Accept  float64 `json:"accept"`
	Reject  float64 `json:"reject"`
	Accepts bool    `json:"accepts"`
	Rejects bool    `json:"rejects"`
	// Audit is the percent of the gate's own decisions that go to the judge
	// anyway. The spec asks for it in as many words: the point of a fixed sample
	// of what stage 5 decided is that the gate's error rate keeps being measured
	// in production rather than assumed from the day it was calibrated.
	Audit int `json:"audit"`
	// Budget is the rate of each mistake the bands were calibrated to tolerate,
	// stored so a gate found on disk says what it was allowed to get wrong.
	Budget       float64 `json:"budget"`
	Epochs       int     `json:"epochs"`
	TrainedOn    int     `json:"trained_on"`
	Positives    int     `json:"positives"`
	CalibratedOn int     `json:"calibrated_on"`
	TeacherHash  string  `json:"teacher_hash,omitempty"`
	Source       string  `json:"source,omitempty"`
}

// Train fits the gate. The order of instances decides the weights, so it is
// fixed here rather than left to the caller: two trainings over the same
// verdicts produce the same model file, byte for byte, on all three platforms.
func Train(instances []Instance, epochs int) *Gate {
	sorted := append([]Instance(nil), instances...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].ProvisionID != sorted[j].ProvisionID {
			return sorted[i].ProvisionID < sorted[j].ProvisionID
		}
		return sorted[i].RecordID < sorted[j].RecordID
	})
	g := &Gate{Epochs: epochs, TrainedOn: len(sorted), TeacherHash: Fingerprint(sorted)}
	for _, in := range sorted {
		if in.Entailed {
			g.Positives++
		}
		if g.Source == "" {
			g.Source = in.Source
		} else if g.Source != in.Source {
			g.Source = "mixed"
		}
	}
	g.Weights = distill.Fit(epochs, func(yield func([]string, bool) bool) {
		for _, in := range sorted {
			if !yield(in.Features, in.Entailed) {
				return
			}
		}
	})
	return g
}

// Score is the gate's reading of one pair. Positive means it thinks the
// provision supports the statement, and how far from zero is how sure it is.
func (g *Gate) Score(text string, s *norm.Statement) float64 {
	return distill.Dot(g.Weights, Features(text, s))
}

// ScoreFeatures is the same reading over features somebody already computed.
func (g *Gate) ScoreFeatures(f []string) float64 { return distill.Dot(g.Weights, f) }

// Verdict is the whole stage 5 decision for one statement: the score, which
// tray it falls in, and whether the audit pulled it out of that tray and sent it
// to the judge anyway.
//
// The audit draw is a hash of the record identifier rather than a random one, so
// the same statement is audited on every machine and a re-run does not quietly
// audit a different sample.
func (g *Gate) Verdict(recordID, text string, s *norm.Statement) norm.GateVerdict {
	v := norm.GateVerdict{Score: g.Score(text, s), Decision: norm.GateJudge}
	switch {
	case g.Accepts && v.Score >= g.Accept:
		v.Decision = norm.GateAccept
	case g.Rejects && v.Score <= g.Reject:
		v.Decision = norm.GateReject
	}
	if v.Decision != norm.GateJudge && g.Audit > 0 && int(hash64(recordID)%100) < g.Audit {
		v.Audited = true
	}
	return v
}

// Write serialises the gate, weights sorted, so two trainings that produced the
// same model produce the same file.
func (g *Gate) Write(w io.Writer) error {
	type pair struct {
		Feature string  `json:"f"`
		Weight  float64 `json:"w"`
	}
	out := struct {
		Gate
		Weights []pair `json:"weights"`
	}{Gate: *g}
	out.Gate.Weights = nil
	for _, f := range sortedKeys(g.Weights) {
		out.Weights = append(out.Weights, pair{f, g.Weights[f]})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// Read loads a gate written by Write.
func Read(r io.Reader) (*Gate, error) {
	var in struct {
		Gate
		Weights []struct {
			Feature string  `json:"f"`
			Weight  float64 `json:"w"`
		} `json:"weights"`
	}
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return nil, err
	}
	g := in.Gate
	g.Weights = map[string]float64{}
	for _, p := range in.Weights {
		g.Weights[p.Feature] = p.Weight
	}
	return &g, nil
}

// Fingerprint is a stable hash of a training set, recorded on the model so a
// gate found on disk can say which verdicts it came from.
func Fingerprint(instances []Instance) string {
	sorted := append([]Instance(nil), instances...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].ProvisionID != sorted[j].ProvisionID {
			return sorted[i].ProvisionID < sorted[j].ProvisionID
		}
		return sorted[i].RecordID < sorted[j].RecordID
	})
	h := fnv.New64a()
	for _, in := range sorted {
		// Hash writes never fail, and the alternative to ignoring the error is
		// threading one out of a fingerprint function that cannot fail.
		_, _ = io.WriteString(h, in.RecordID+"\x00")
		_, _ = io.WriteString(h, fmt.Sprint(in.Entailed)+"\x00")
		for _, f := range in.Features {
			_, _ = io.WriteString(h, f+"\x00")
		}
	}
	return fmt.Sprintf("%016x", h.Sum64())
}

// Heaviest returns the n features with the largest weights either way, which is
// how a person reads what the gate learned.
func Heaviest(weights map[string]float64, n int) []string {
	keys := sortedKeys(weights)
	sort.SliceStable(keys, func(i, j int) bool { return abs(weights[keys[i]]) > abs(weights[keys[j]]) })
	if len(keys) > n {
		keys = keys[:n]
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%+.3f %s", weights[k], k))
	}
	return out
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func hash64(s string) uint64 {
	h := fnv.New64a()
	_, _ = io.WriteString(h, s)
	return h.Sum64()
}
