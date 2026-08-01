// Package relation holds the concept to concept edges: what the corpus as a
// whole treats as holding between two concepts, as opposed to what any one
// provision states.
//
// The distinction matters more than it looks. A norm to concept edge says a
// particular provision imposes something on somebody, it is scoped to that
// provision, and it is what makes the norm layer n-ary rather than a triple. An
// edge in this package says giay phep xay dung requires giay chung nhan quyen
// su dung dat, which no provision in the corpus states in that form. It is the
// difference between a document graph and a knowledge graph, and collapsing the
// second into the first is the commonest modelling error in the field.
//
// Nothing here can be derived by a deterministic function of the corpus.
// Vietnamese text has no marker meaning "a prerequisite relation follows". The
// relation is in the meaning of the sentence and nowhere in its form, so this
// layer is model driven end to end and the engineering problem is not finding
// relations without a model but keeping a model's relations honest.
package relation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/concept"
)

// The seed relation types. Small on purpose: these are the ones that pay for
// themselves in the competency questions, and a vocabulary fixed large up front
// is a vocabulary the model spends the corpus forcing reality into.
const (
	Broader       = "BROADER"
	PartOf        = "PART_OF"
	Requires      = "REQUIRES"
	Produces      = "PRODUCES"
	Grants        = "GRANTS"
	IssuedBy      = "ISSUED_BY"
	RegulatedBy   = "REGULATED_BY"
	EvidencedBy   = "EVIDENCED_BY"
	MeasuredIn    = "MEASURED_IN"
	AlternativeTo = "ALTERNATIVE_TO"
	Excludes      = "EXCLUDES"
	Replaces      = "REPLACES"
)

// Type is one relation the layer may assert, with the definition that
// canonicalization matches against and the concept kinds it is allowed to hold
// between.
//
// The definition is the load bearing field. Two models, or one model on two
// days, will produce can co truoc and la dieu kien de duoc cap for the same
// thing, and asking whether two verb phrases mean the same in the abstract is a
// task models are unreliable at. Asking whether two one sentence definitions
// describe the same relation is a task they are good at.
type Type struct {
	ID string `json:"id"`
	// Definition is one sentence, in the same register the model is asked to
	// write its own proposals in, because the two get compared directly.
	Definition string `json:"definition"`
	// Domain and Range are concept kinds, not registry classes. Empty means any
	// kind, and SameKind means the range must match whatever kind the domain
	// concept turned out to be, which is how a hierarchy relation is stated
	// without enumerating twelve pairs.
	Domain    []string `json:"domain,omitempty"`
	Range     []string `json:"range,omitempty"`
	SameKind  bool     `json:"same_kind,omitempty"`
	Symmetric bool     `json:"symmetric,omitempty"`
	// SurfaceFormsVI is documentation and prompt material. It is not a matcher,
	// and nothing in this package looks for these strings in text.
	SurfaceFormsVI []string `json:"surface_forms_vi,omitempty"`
}

// Seed is the starting relation vocabulary. Order is fixed so a prompt built
// from it is byte identical between runs.
var Seed = []Type{
	{
		ID:             Broader,
		Definition:     "X là một loại của Y, nghĩa là mọi X đều là Y",
		SameKind:       true,
		SurfaceFormsVI: []string{"là một loại", "thuộc", "bao gồm"},
	},
	{
		ID:             PartOf,
		Definition:     "X là một bộ phận cấu thành của Y nhưng không phải là một loại của Y",
		Domain:         []string{concept.KindArtifact, concept.KindAction, concept.KindThing},
		Range:          []string{concept.KindArtifact, concept.KindAction, concept.KindThing},
		SurfaceFormsVI: []string{"là một phần của", "thuộc thành phần", "gồm"},
	},
	{
		ID:             Requires,
		Definition:     "Y phải tồn tại hoặc phải đã xảy ra thì X mới được cấp hoặc mới được thực hiện",
		Domain:         []string{concept.KindAction, concept.KindArtifact, concept.KindStatus},
		Range:          []string{concept.KindArtifact, concept.KindAction, concept.KindStatus, concept.KindCondition},
		SurfaceFormsVI: []string{"phải có", "sau khi đã", "với điều kiện", "trên cơ sở"},
	},
	{
		ID:             Produces,
		Definition:     "Thực hiện X làm cho Y hình thành, được cấp hoặc phát sinh",
		Domain:         []string{concept.KindAction},
		Range:          []string{concept.KindArtifact, concept.KindStatus},
		SurfaceFormsVI: []string{"cấp", "ban hành", "dẫn đến việc", "kết quả là"},
	},
	{
		ID:             Grants,
		Definition:     "Việc có X cho phép chủ thể được hưởng quyền hoặc được thực hiện Y",
		Domain:         []string{concept.KindArtifact, concept.KindStatus},
		Range:          []string{concept.KindStatus, concept.KindAction},
		SurfaceFormsVI: []string{"có quyền", "được phép", "cho phép"},
	},
	{
		ID:             IssuedBy,
		Definition:     "Giấy tờ hoặc văn bản X do cơ quan Y cấp hoặc ban hành",
		Domain:         []string{concept.KindArtifact},
		Range:          []string{concept.KindBody, concept.KindActor},
		SurfaceFormsVI: []string{"do ... cấp", "do ... ban hành"},
	},
	{
		ID:             RegulatedBy,
		Definition:     "X chịu sự điều chỉnh của văn bản hoặc chế độ Y",
		Range:          []string{concept.KindArtifact, concept.KindStatus, concept.KindRule},
		SurfaceFormsVI: []string{"theo quy định của", "chịu sự điều chỉnh của"},
	},
	{
		ID:             EvidencedBy,
		Definition:     "Tình trạng hoặc sự kiện X được chứng minh bằng giấy tờ Y",
		Domain:         []string{concept.KindStatus, concept.KindCondition},
		Range:          []string{concept.KindArtifact},
		SurfaceFormsVI: []string{"được chứng minh bằng", "giấy tờ chứng minh"},
	},
	{
		ID:             MeasuredIn,
		Definition:     "Đại lượng X được tính theo đơn vị hoặc căn cứ Y",
		Domain:         []string{concept.KindAmount},
		Range:          []string{concept.KindAmount, concept.KindArtifact, concept.KindRule, concept.KindTime},
		SurfaceFormsVI: []string{"tính theo", "căn cứ vào", "mức"},
	},
	{
		ID:             AlternativeTo,
		Definition:     "X và Y được đưa ra để thay thế lẫn nhau cho cùng một mục đích",
		SameKind:       true,
		Symmetric:      true,
		SurfaceFormsVI: []string{"hoặc", "một trong các"},
	},
	{
		ID:             Excludes,
		Definition:     "X và Y không thể cùng đúng đối với cùng một chủ thể",
		Domain:         []string{concept.KindStatus, concept.KindAction, concept.KindCondition},
		Range:          []string{concept.KindStatus, concept.KindAction, concept.KindCondition},
		Symmetric:      true,
		SurfaceFormsVI: []string{"không đồng thời", "không được vừa ... vừa"},
	},
	{
		ID:             Replaces,
		Definition:     "Trong cách dùng từ của hệ thống văn bản, X thay thế cho Y",
		SameKind:       true,
		SurfaceFormsVI: []string{"thay thế cho", "trước đây gọi là"},
	},
}

// Registry is the relation vocabulary at one ontology version, which is the
// thing an edge cites when it says it is canonical.
type Registry struct {
	Version int    `json:"version"`
	Types   []Type `json:"types"`
}

// SeedRegistry returns the seed vocabulary at a version.
func SeedRegistry(version int) *Registry {
	return &Registry{Version: version, Types: append([]Type(nil), Seed...)}
}

// Type returns the named relation type, or nil.
func (r *Registry) Type(id string) *Type {
	for i := range r.Types {
		if r.Types[i].ID == id {
			return &r.Types[i]
		}
	}
	return nil
}

// IDs returns the type identifiers in registry order.
func (r *Registry) IDs() []string {
	out := make([]string, 0, len(r.Types))
	for _, t := range r.Types {
		out = append(out, t.ID)
	}
	return out
}

// Forbidden names the relations no automated pass may ever emit, whatever the
// registry says.
//
// SAME_AS, INSTANCE_OF and DIFFERS_FROM are identity decisions. They live in the
// concept package, they carry a human decider and a written rationale, and a
// model producing one here would be minting identity from a sentence.
//
// RELATED_TO and its cousins are the generic attractor: a model under
// uncertainty reaches for the vaguest relation available, and an edge that says
// only that two things are connected cannot be queried for anything. There is no
// RELATED_TO in the seed set, none may be promoted into it, and a model with
// nothing specific to say is supposed to return nothing, which is the correct
// output rather than a failure to extract.
var Forbidden = map[string]bool{
	"SAME_AS":         true,
	"INSTANCE_OF":     true,
	"DIFFERS_FROM":    true,
	"RELATED_TO":      true,
	"RELATES_TO":      true,
	"ASSOCIATED_WITH": true,
	"LIEN_QUAN":       true,
	"CO_LIEN_QUAN":    true,
}

// Source says where an edge came from, in decreasing order of reliability.
const (
	SourceDefinitional = "definitional" // the drafter wrote it, in a definition clause
	SourceProvision    = "provision"    // read out of one provision
	SourceCorpus       = "corpus"       // aggregated across provisions, never asserted from one
)

// Status says whether an edge is trusted. Two different things keep an edge out
// of the canonical set and both are worth telling apart, which is what Why is
// for: the registry may not hold its relation type, or the corpus may have shown
// it exactly once.
const (
	StatusCanonical   = "canonical"
	StatusProvisional = "provisional"
)

// Reasons an edge is provisional.
const (
	WhyUnknownType    = "unknown_type"    // the type is not in the registry at this version
	WhySingleSupport  = "single_support"  // one provision said it and nothing corroborates
	WhyDirectionWrong = "direction_wrong" // the blind pass read it the other way round
)

// Evidence is one provision supporting an edge, with a quote verified byte for
// byte at its offsets. An edge with no evidence does not build.
type Evidence struct {
	ProvisionID string `json:"provision_id"`
	Quote       string `json:"quote"`
	CharStart   int    `json:"char_start"`
	CharEnd     int    `json:"char_end"`
	// AsWritten is the model's own words for the relation in this provision. It
	// is what feeds the Define step for anything the registry does not cover, so
	// it is kept even after canonicalization: the registry predicate says what
	// the edge is, and this says what the text said.
	AsWritten string `json:"as_written,omitempty"`
	// DirectionCheck is the model's prose statement of which way the relation
	// runs, written while the quote was in view. It is a cheap consistency
	// signal against the typed fields and it is read by a person when the blind
	// verifier disagrees.
	DirectionCheck string `json:"direction_check,omitempty"`
	DocID          string `json:"doc_id,omitempty"`
}

// Edge is one concept to concept relation with everything needed to argue with
// it.
type Edge struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Source string `json:"source"`

	// Definition is filled for a type the registry does not hold: the model's
	// one sentence account of the relation it proposed. Such an edge without one
	// cannot be canonicalized later and cannot be reviewed, so it is required.
	Definition string `json:"definition,omitempty"`
	// Why says what is keeping an edge out of the canonical set, so the review
	// queue is a list of specific problems rather than a pile.
	Why string `json:"why,omitempty"`

	Evidence     []Evidence `json:"evidence"`
	SupportCount int        `json:"support_count"`
	// SupportDocs counts distinct documents, because forty provisions in one
	// decree is one source repeating itself and four documents from four bodies
	// is the corpus agreeing.
	SupportDocs int `json:"support_docs"`

	FirstSeen string `json:"first_seen,omitempty"`
	LastSeen  string `json:"last_seen,omitempty"`

	Confidence      float64 `json:"confidence"`
	OntologyVersion int     `json:"ontology_version,omitempty"`
	Job             string  `json:"job,omitempty"`

	// Direction is the outcome of the blind second pass. Unverified is not the
	// same as agreed, and the two are never folded together, because a graph
	// with 95 percent relation precision and 80 percent direction accuracy is
	// worse than useless for traversal.
	Direction string `json:"direction,omitempty"`
}

// Direction verdicts.
const (
	DirectionUnverified = ""         // no blind pass has run on this edge
	DirectionAgreed     = "agreed"   // the verifier read the same way round
	DirectionFlipped    = "flipped"  // the verifier read it the other way
	DirectionUnclear    = "unclear"  // the verifier could not tell from the quote
	DirectionDisputed   = "disputed" // sources disagree across the supporting evidence
)

// Key identifies an edge for folding. Direction is part of it, because a
// flipped edge is a different fact and not a duplicate.
func (e Edge) Key() string { return e.FromID + "|" + e.Type + "|" + e.ToID }

// Reverse returns the edge with its endpoints swapped, keeping the evidence.
func (e Edge) Reverse() Edge {
	e.FromID, e.ToID = e.ToID, e.FromID
	return e
}

// Kinds is the concept kind of every endpoint the checker knows about, which is
// how domain and range are enforced. A missing entry is missing evidence rather
// than a violation: an endpoint whose kind nothing recorded is reported as
// unknown instead of being failed for a constraint nobody could evaluate.
type Kinds map[string]string

// Validate checks one edge against the registry and the concept kinds. It is
// the gate every extraction result passes through, and a failure rejects the
// edge rather than logging it.
func (e Edge) Validate(r *Registry, kinds Kinds) error {
	if e.FromID == "" || e.ToID == "" {
		return fmt.Errorf("an edge needs two endpoints")
	}
	if e.FromID == e.ToID {
		return fmt.Errorf("%s relates to itself", e.FromID)
	}
	if Forbidden[strings.ToUpper(e.Type)] {
		return fmt.Errorf("%s is never produced by an automated pass", e.Type)
	}
	if e.Type == "" {
		return fmt.Errorf("no relation type")
	}
	if len(e.Evidence) == 0 {
		return fmt.Errorf("no evidence, and an edge nobody can check is an assertion")
	}
	switch e.Source {
	case SourceDefinitional, SourceProvision, SourceCorpus:
	default:
		return fmt.Errorf("source %q is not one of %s, %s, %s", e.Source, SourceDefinitional, SourceProvision, SourceCorpus)
	}
	if e.Confidence < 0 || e.Confidence > 1 {
		return fmt.Errorf("confidence %v is outside 0 to 1", e.Confidence)
	}

	t := r.Type(e.Type)
	// A type the registry does not hold needs a definition whatever its status,
	// because that definition is the only thing canonicalization can match on
	// and the only thing a reviewer can read.
	if t == nil && strings.TrimSpace(e.Definition) == "" {
		return fmt.Errorf("%s is not in registry v%d and carries no definition, so it can never be canonicalized or reviewed", e.Type, r.Version)
	}
	switch e.Status {
	case StatusCanonical:
		if t == nil {
			return fmt.Errorf("%s is canonical and is not in registry v%d", e.Type, r.Version)
		}
		// Invariant 4. One confident sighting is the cheapest hallucination to
		// produce and the hardest to notice, so patience is the defence.
		if e.SupportCount <= 1 && e.Source != SourceDefinitional {
			return fmt.Errorf("%s is canonical on a single provision and did not come from a definition", e.Type)
		}
		if e.Direction == DirectionFlipped || e.Direction == DirectionDisputed {
			return fmt.Errorf("%s is canonical and the blind direction pass read it as %s", e.Type, e.Direction)
		}
	case StatusProvisional:
	default:
		return fmt.Errorf("status %q is not %s or %s", e.Status, StatusCanonical, StatusProvisional)
	}

	if t != nil {
		if err := checkKinds(t, e, kinds); err != nil {
			return err
		}
	}
	return nil
}

// checkKinds enforces domain and range. An endpoint of unknown kind passes,
// because a constraint that cannot be evaluated is not a violation, and failing
// it would reject every edge touching a concept the layer has not read yet.
func checkKinds(t *Type, e Edge, kinds Kinds) error {
	from, haveFrom := kinds[e.FromID]
	to, haveTo := kinds[e.ToID]
	if haveFrom && len(t.Domain) > 0 && !contains(t.Domain, from) {
		return fmt.Errorf("%s has domain %s and %s is a %s", t.ID, strings.Join(t.Domain, "|"), e.FromID, from)
	}
	if haveTo && len(t.Range) > 0 && !contains(t.Range, to) {
		return fmt.Errorf("%s has range %s and %s is a %s", t.ID, strings.Join(t.Range, "|"), e.ToID, to)
	}
	if t.SameKind && haveFrom && haveTo && from != to {
		return fmt.Errorf("%s holds between concepts of one kind and %s is a %s while %s is a %s", t.ID, e.FromID, from, e.ToID, to)
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Sort orders edges so two runs over the same corpus produce the same file.
func Sort(edges []Edge) {
	sort.SliceStable(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.FromID != b.FromID {
			return a.FromID < b.FromID
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.ToID < b.ToID
	})
}
