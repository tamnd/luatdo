package concept

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/law"
)

// Merging is three steps and each one is done by whatever is best at it.
//
// Code proposes. Clustering is a cheap recall device over labels, aliases and
// definition wording, and it is allowed to be wrong in both directions: a
// cluster is a question, not an answer.
//
// A model compares. It reads both definitions and says whether they denote the
// same thing, with a rationale and the features it thinks differ. It has no
// power to write anything into the graph.
//
// A person decides. The INSTANCE_OF edge exists only because somebody chose it
// and wrote down why, and the layer refuses to build without the name and the
// reason. This is not ceremony. Merging nguoi lao dong across the Labour Code
// and the Social Insurance Law is a legal judgement about scope, and a system
// that makes it silently by string match has destroyed the most interesting
// fact it held.

// Link is one reason code thinks two term uses might be the same. It is
// evidence for a question, never a decision.
type Link struct {
	A     string  `json:"a"`
	B     string  `json:"b"`
	Basis string  `json:"basis"` // label, alias, or definition
	Score float64 `json:"score"`
}

// DefinitionSimilarity is the Jaccard floor over definition tokens at which two
// term uses are worth asking about. It is deliberately low. A missed cluster is
// a merge nobody is ever offered, and a spurious cluster costs one question.
const DefinitionSimilarity = 0.5

// commonTokenShare is the share of definitions a token may appear in before it
// stops being used to find candidate pairs. Tokens like quy, dinh and phap
// appear in most legal definitions in the corpus, and pairing on them would
// compare everything with everything.
const commonTokenShare = 0.1

// Links returns every pair of term uses code has a reason to ask about.
//
// Label and alias matching are exact after folding. Definition similarity is
// found through an inverted index over definition tokens, with the commonest
// tokens dropped, so the cost is the number of pairs that share an informative
// word rather than the square of the corpus.
func Links(terms []TermUse) []Link {
	byLabel := map[string][]int{}
	for i := range terms {
		for _, s := range surfaces(&terms[i]) {
			byLabel[s] = append(byLabel[s], i)
		}
	}

	seen := map[[2]int]bool{}
	var out []Link
	add := func(i, j int, basis string, score float64) {
		if i == j {
			return
		}
		if terms[i].ScopeID == terms[j].ScopeID {
			// One instrument using one phrase twice is one term use, not two
			// things to merge. If the reader emitted both, that is a reading
			// problem and merging papers over it.
			return
		}
		if i > j {
			i, j = j, i
		}
		if seen[[2]int{i, j}] {
			return
		}
		seen[[2]int{i, j}] = true
		out = append(out, Link{A: terms[i].ID, B: terms[j].ID, Basis: basis, Score: score})
	}

	for surface, group := range byLabel {
		basis := "label"
		for _, i := range group {
			if law.Slug(terms[i].LabelVI) != surface {
				basis = "alias"
				break
			}
		}
		for a := range group {
			for b := a + 1; b < len(group); b++ {
				add(group[a], group[b], basis, 1)
			}
		}
	}

	for _, pair := range definitionPairs(terms) {
		i, j := pair[0], pair[1]
		if score := jaccard(tokens(definitionOf(&terms[i])), tokens(definitionOf(&terms[j]))); score >= DefinitionSimilarity {
			add(i, j, "definition", score)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].A != out[j].A {
			return out[i].A < out[j].A
		}
		return out[i].B < out[j].B
	})
	return out
}

// Cluster is a group of term uses that might be one concept, with the anchor
// every question in it is asked against.
type Cluster struct {
	ID string `json:"id"`
	// Anchor is the member every other member is compared with. A cluster of k
	// members therefore costs k-1 questions rather than k(k-1)/2, which is what
	// makes a cluster of four hundred provincial decisions affordable at all.
	// The cost of the star is that two members which both differ from the
	// anchor are never compared with each other, so a second round pairs the
	// declined members among themselves.
	Anchor  string   `json:"anchor"`
	Members []string `json:"members"`
	Bases   []string `json:"bases"`
}

// Clusters groups term uses by the links between them. The anchor is the
// lexicographically first member, which makes the clustering reproducible: the
// same corpus produces the same questions in the same order, and a reviewer who
// stopped halfway through yesterday resumes in the same place today.
func Clusters(terms []TermUse, links []Link) []Cluster {
	index := map[string]int{}
	for i := range terms {
		index[terms[i].ID] = i
	}
	parent := make([]int, len(terms))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}

	// Bases are recorded against the members rather than the root, because a
	// later union moves the root and a basis filed under the old one would be
	// lost.
	bases := map[string][]string{}
	for _, l := range links {
		a, aok := index[l.A]
		b, bok := index[l.B]
		if !aok || !bok {
			continue
		}
		if ra, rb := find(a), find(b); ra != rb {
			parent[rb] = ra
		}
		bases[l.A] = append(bases[l.A], l.Basis)
		bases[l.B] = append(bases[l.B], l.Basis)
	}

	members := map[int][]string{}
	for i := range terms {
		root := find(i)
		members[root] = append(members[root], terms[i].ID)
	}

	var out []Cluster
	for _, ids := range members {
		if len(ids) < 2 {
			continue
		}
		sort.Strings(ids)
		var basisList []string
		for _, id := range ids {
			basisList = append(basisList, bases[id]...)
		}
		basisList = unique(basisList)
		sort.Strings(basisList)
		out = append(out, Cluster{
			ID:      "vn:cluster:" + law.Slug(terms[index[ids[0]]].LabelVI),
			Anchor:  ids[0],
			Members: ids,
			Bases:   basisList,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Anchor < out[j].Anchor })
	return out
}

// Pairs returns the questions a cluster asks, in a fixed order.
func (c *Cluster) Pairs() [][2]string {
	var out [][2]string
	for _, m := range c.Members {
		if m != c.Anchor {
			out = append(out, [2]string{c.Anchor, m})
		}
	}
	return out
}

// Comparison is the model's reading of one pair. It is advice on a question a
// person answers, and nothing in the build ever reads it as a decision.
type Comparison struct {
	A          string   `json:"a"`
	B          string   `json:"b"`
	Relation   string   `json:"relation"` // same, broader, narrower, differs, unclear
	Rationale  string   `json:"rationale"`
	Differing  []string `json:"differing_features,omitempty"`
	Confidence float64  `json:"confidence"`
	Model      string   `json:"model,omitempty"`
}

// RelationDiffers and RelationUnclear are answers a comparison can give that
// are not memberships. Differs becomes a DIFFERS_FROM edge; unclear becomes
// nothing at all, which is the right output for a pair the evidence does not
// settle.
const (
	RelationDiffers = "differs"
	RelationUnclear = "unclear"
)

// Comparer asks a model to compare two readings.
type Comparer struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
}

// Instructions is the comparison prompt. It says out loud that the two
// definitions being similar is not the question, because similar wording across
// two instruments is the normal case and the interesting fact is whether the
// scope is the same.
func (c *Comparer) Instructions() string {
	var b strings.Builder
	b.WriteString("Bạn so sánh hai định nghĩa của cùng một cụm từ trong hai văn bản quy phạm pháp luật Việt Nam khác nhau.\n")
	b.WriteString("Câu hỏi duy nhất: hai định nghĩa này có chỉ cùng một đối tượng hay không.\n\n")
	b.WriteString("Lưu ý quan trọng: hai định nghĩa dùng từ ngữ giống nhau vẫn có thể có phạm vi khác nhau. Hãy so sánh phạm vi chủ thể, điều kiện và ngoại lệ, không so sánh câu chữ.\n\n")
	b.WriteString("relation phải là một trong:\n")
	b.WriteString("- same: cùng một đối tượng, cùng phạm vi\n")
	b.WriteString("- broader: định nghĩa A rộng hơn định nghĩa B\n")
	b.WriteString("- narrower: định nghĩa A hẹp hơn định nghĩa B\n")
	b.WriteString("- differs: cùng cụm từ nhưng chỉ hai đối tượng khác nhau\n")
	b.WriteString("- unclear: không đủ căn cứ để kết luận\n\n")
	b.WriteString("unclear là câu trả lời hợp lệ và tốt hơn một phỏng đoán.\n")
	b.WriteString("differing_features liệt kê các đặc điểm làm hai định nghĩa khác nhau, lấy từ chính hai định nghĩa.\n")
	b.WriteString("rationale viết bằng tiếng Việt, một hoặc hai câu, nêu căn cứ cụ thể.\n\n")
	b.WriteString("Trả về đúng một đối tượng JSON, không giải thích:\n")
	b.WriteString(`{"relation":"same","rationale":"...","differing_features":[],"confidence":0.8}`)
	b.WriteString("\n")
	return b.String()
}

// ComparePrompt renders two readings side by side.
func ComparePrompt(a, b *TermUse) string {
	var sb strings.Builder
	sb.WriteString("Định nghĩa A\n")
	writeSide(&sb, a)
	sb.WriteString("\nĐịnh nghĩa B\n")
	writeSide(&sb, b)
	return sb.String()
}

func writeSide(sb *strings.Builder, t *TermUse) {
	fmt.Fprintf(sb, "Văn bản: %s\n", t.ScopeID)
	fmt.Fprintf(sb, "Thuật ngữ: %s\n", t.LabelVI)
	if t.Genus != "" {
		fmt.Fprintf(sb, "Loại: %s\n", t.Genus)
	}
	for _, d := range t.Differentiae {
		fmt.Fprintf(sb, "Đặc điểm: %s\n", d.Text)
	}
	if len(t.EnumeratedSubtypes) > 0 {
		fmt.Fprintf(sb, "Liệt kê: %s\n", strings.Join(t.EnumeratedSubtypes, "; "))
	}
	fmt.Fprintf(sb, "Nguyên văn: %s\n", t.Quote)
}

// Compare asks the model about one pair.
func (c *Comparer) Compare(ctx context.Context, a, b *TermUse) (*Comparison, api.Usage, error) {
	var usage api.Usage
	input := ComparePrompt(a, b)
	for attempt := 0; attempt <= maxCorrections(c.MaxCorrections); attempt++ {
		resp, err := c.Completer.Complete(ctx, api.Request{
			Model:        c.Model,
			Instructions: c.Instructions(),
			Input:        input,
		})
		if err != nil {
			return nil, usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var got Comparison
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &got); err != nil {
			input = ComparePrompt(a, b) + "\nLần trả lời trước không phải một đối tượng JSON hợp lệ: " + err.Error() + "\nTrả lời lại, chỉ một đối tượng JSON.\n"
			continue
		}
		if !comparisonRelation(got.Relation) {
			input = ComparePrompt(a, b) + fmt.Sprintf("\nrelation %q không hợp lệ. Chọn một trong same, broader, narrower, differs, unclear.\n", got.Relation)
			continue
		}
		if strings.TrimSpace(got.Rationale) == "" {
			input = ComparePrompt(a, b) + "\nThiếu rationale. Nêu căn cứ cụ thể cho kết luận.\n"
			continue
		}
		got.A, got.B, got.Model = a.ID, b.ID, c.Model
		return &got, usage, nil
	}
	// A comparison the model could not produce is not an error. The pair still
	// goes to a person, with no advice attached.
	return &Comparison{A: a.ID, B: b.ID, Relation: RelationUnclear, Rationale: "mô hình không trả lời được", Model: c.Model}, usage, nil
}

// Question is one pair queued for a person, with everything needed to answer it
// without opening another file.
type Question struct {
	ClusterID  string      `json:"cluster_id"`
	A          TermUse     `json:"a"`
	B          TermUse     `json:"b"`
	Bases      []string    `json:"bases"`
	Comparison *Comparison `json:"comparison,omitempty"`
	At         string      `json:"at"`
}

// Answer is one person's decision. Rationale is required, and Check rejects the
// layer without it, because a merge with no stated reason cannot be reviewed,
// argued with or undone by anyone who was not in the room.
type Answer struct {
	ClusterID string `json:"cluster_id"`
	A         string `json:"a"`
	B         string `json:"b"`
	// Verdict is same, broader, narrower, differs or defer. Defer is a real
	// answer: it takes the pair out of the queue for this round and leaves the
	// graph saying nothing, which is what an unresolved question should look
	// like.
	Verdict   string `json:"verdict"`
	Rationale string `json:"rationale"`
	DecidedBy string `json:"decided_by"`
	DecidedAt string `json:"decided_at"`
	// Disambiguator names the concept when the plain label is ambiguous.
	Disambiguator string `json:"disambiguator,omitempty"`
}

// VerdictDefer leaves a pair undecided on purpose.
const VerdictDefer = "defer"

// Apply turns answers into concepts, memberships and difference edges. It is
// the only thing in this package that creates a Concept.
//
// Answers are applied in order, so a pair answered twice takes the later
// answer, which is how a correction works in an append only decision log.
func Apply(terms []TermUse, answers []Answer) Layer {
	byID := map[string]*TermUse{}
	for i := range terms {
		byID[terms[i].ID] = &terms[i]
	}

	latest := map[[2]string]Answer{}
	var order [][2]string
	for _, a := range answers {
		key := [2]string{a.A, a.B}
		if _, seen := latest[key]; !seen {
			order = append(order, key)
		}
		latest[key] = a
	}

	layer := Layer{TermUses: terms}
	concepts := map[string]bool{}
	anchored := map[string]string{} // term use id to concept id, for anchors already placed

	for _, key := range order {
		a := latest[key]
		anchor, other := byID[a.A], byID[a.B]
		if anchor == nil || other == nil {
			continue
		}
		switch a.Verdict {
		case VerdictDefer:
			continue
		case RelationDiffers:
			layer.Differences = append(layer.Differences, Difference{
				FromID: anchor.ID, ToID: other.ID,
				DecidedBy: a.DecidedBy, DecidedAt: a.DecidedAt, Rationale: a.Rationale,
				Basis: differingFeatures(anchor, other),
			})
			continue
		case RelationSame, RelationBroader, RelationNarrower:
		default:
			continue
		}

		conceptID, placed := anchored[anchor.ID]
		if !placed {
			conceptID = ConceptID(anchor.LabelVI, a.Disambiguator)
			if !concepts[conceptID] {
				layer.Concepts = append(layer.Concepts, Concept{
					ID:            conceptID,
					LabelVI:       anchor.LabelVI,
					Kind:          anchor.Kind,
					Disambiguator: a.Disambiguator,
				})
				concepts[conceptID] = true
			}
			// The anchor joins its own concept on the strength of the first
			// decision made about it, and the rationale recorded is that
			// decision's. Anything else would give the anchor a membership
			// nobody decided.
			layer.Memberships = append(layer.Memberships, Membership{
				TermUseID: anchor.ID, ConceptID: conceptID, Relation: RelationSame,
				DecidedBy: a.DecidedBy, DecidedAt: a.DecidedAt, Rationale: a.Rationale,
			})
			anchored[anchor.ID] = conceptID
		}
		layer.Memberships = append(layer.Memberships, Membership{
			TermUseID: other.ID, ConceptID: conceptID, Relation: relationOf(a.Verdict),
			DecidedBy: a.DecidedBy, DecidedAt: a.DecidedAt, Rationale: a.Rationale,
		})
	}
	return layer
}

// relationOf flips the direction. The answer says how B stands to A, and the
// membership says how B stands to the concept A named, which is the same
// statement seen from the concept.
func relationOf(verdict string) string {
	switch verdict {
	case RelationBroader:
		// A is broader than B, so B is a narrower member of A's concept.
		return RelationNarrower
	case RelationNarrower:
		return RelationBroader
	default:
		return RelationSame
	}
}

// differingFeatures is the evidence attached to a difference edge: the
// distinguishing features one reading states and the other does not. It is
// computed rather than asked for, because it is a set difference over text the
// readings already carry.
func differingFeatures(a, b *TermUse) []string {
	have := map[string]bool{}
	for _, d := range b.Differentiae {
		have[law.Slug(d.Text)] = true
	}
	var out []string
	for _, d := range a.Differentiae {
		if !have[law.Slug(d.Text)] {
			out = append(out, d.Text)
		}
	}
	for _, d := range b.Differentiae {
		if !hasFeature(a.Differentiae, d.Text) {
			out = append(out, d.Text)
		}
	}
	return out
}

func hasFeature(list []Differentia, text string) bool {
	for _, d := range list {
		if law.Slug(d.Text) == law.Slug(text) {
			return true
		}
	}
	return false
}

func comparisonRelation(r string) bool {
	switch r {
	case RelationSame, RelationBroader, RelationNarrower, RelationDiffers, RelationUnclear:
		return true
	}
	return false
}

// surfaces is every way a term use can be named: its label and its aliases,
// folded. An alias is how two instruments end up using one phrase without
// either of them spelling it out the same way.
func surfaces(t *TermUse) []string {
	out := []string{law.Slug(t.LabelVI)}
	for _, a := range t.Aliases {
		if s := law.Slug(a); s != "" {
			out = append(out, s)
		}
	}
	return unique(out)
}

func definitionOf(t *TermUse) string {
	if t.DefinitionVI != "" {
		return t.DefinitionVI
	}
	var parts []string
	if t.Genus != "" {
		parts = append(parts, t.Genus)
	}
	for _, d := range t.Differentiae {
		parts = append(parts, d.Text)
	}
	return strings.Join(parts, " ")
}

// definitionPairs returns the pairs worth scoring, found through an inverted
// index with the commonest tokens dropped.
func definitionPairs(terms []TermUse) [][2]int {
	posting := map[string][]int{}
	for i := range terms {
		for tok := range tokens(definitionOf(&terms[i])) {
			posting[tok] = append(posting[tok], i)
		}
	}
	limit := max(int(float64(len(terms))*commonTokenShare), 2)

	seen := map[[2]int]bool{}
	var out [][2]int
	for _, ids := range posting {
		if len(ids) > limit {
			continue
		}
		for a := range ids {
			for b := a + 1; b < len(ids); b++ {
				key := [2]int{ids[a], ids[b]}
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, key)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i][0] != out[j][0] {
			return out[i][0] < out[j][0]
		}
		return out[i][1] < out[j][1]
	})
	return out
}

func tokens(s string) map[string]bool {
	out := map[string]bool{}
	for f := range strings.FieldsSeq(law.Slug(s)) {
		for tok := range strings.SplitSeq(f, "-") {
			if len(tok) > 1 {
				out[tok] = true
			}
		}
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for k := range a {
		if b[k] {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
