// Package event holds the acts legal text is about, as nodes rather than as
// strings in a slot.
//
// The relation layer relates concepts to concepts. That is the right layer and
// it is not enough, because Vietnamese statutes are overwhelmingly written about
// acts and what follows from them. A norm carries its action as a phrase in a
// slot, so the graph knows an employer must give notice and cannot walk from
// giving notice to what happens when notice is not given, although both are in
// the corpus and both are already extracted.
//
// An event here is not an occurrence in the world. Nothing in a statute records
// that a particular company filed a particular return on a particular day. It is
// the act type the corpus regulates, the thing a drafter names when they write
// nộp hồ sơ or thu hồi giấy phép, and its identity is corpus wide on purpose:
// an act named in a law and the same act named in the decree implementing it
// have to be one node or no chain crosses a document boundary, which is where
// the interesting chains are.
//
// That identity decision is the risk in this package and it is stated here
// rather than buried. Two provisions using the same act phrase for two different
// acts will merge, and the defence is that every occurrence keeps its own
// provision, quote and participants, so a node that merged too much can be taken
// apart by reading it. Nothing downstream may assume an event node is atomic.
package event

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/law"
)

// The seed event classes.
//
// Small on purpose and closed on purpose, for the reason the relation registry
// is: a vocabulary fixed large up front is a vocabulary the model spends the
// corpus forcing reality into. These are the acts that recur across Vietnamese
// statutes whatever the subject matter, and anything else the model proposes
// goes to the candidates queue with its evidence rather than into the registry.
const (
	Submit     = "SUBMIT"     // nộp hồ sơ, đơn, tờ khai
	Issue      = "ISSUE"      // cấp, ban hành một giấy tờ hoặc văn bản
	Notify     = "NOTIFY"     // thông báo, báo cáo cho một bên khác
	Register   = "REGISTER"   // đăng ký, ghi vào sổ
	Declare    = "DECLARE"    // khai, kê khai
	Inspect    = "INSPECT"    // kiểm tra, thanh tra, giám sát
	Decide     = "DECIDE"     // quyết định, phê duyệt, chấp thuận, từ chối
	Pay        = "PAY"        // nộp tiền, trả tiền, thanh toán
	Penalise   = "PENALISE"   // xử phạt, xử lý vi phạm
	Suspend    = "SUSPEND"    // đình chỉ, tạm dừng
	Revoke     = "REVOKE"     // thu hồi, tước, huỷ bỏ
	Conclude   = "CONCLUDE"   // giao kết, ký kết
	Terminate  = "TERMINATE"  // chấm dứt, thanh lý
	Transfer   = "TRANSFER"   // chuyển giao, chuyển nhượng, bàn giao
	Compensate = "COMPENSATE" // bồi thường, hoàn trả
	Complain   = "COMPLAIN"   // khiếu nại, tố cáo, khởi kiện
	Publish    = "PUBLISH"    // công bố, niêm yết, công khai
	Establish  = "ESTABLISH"  // thành lập, tổ chức
)

// Class is one act type the layer may assert.
//
// Definition carries the same weight it does in the relation registry: two
// models, or one model twice, will write cấp phép and ra quyết định cho phép for
// one act, and asking whether two verb phrases mean the same in the abstract is
// something models are unreliable at, while asking whether two one sentence
// definitions describe the same act is something they are good at.
type Class struct {
	ID         string `json:"id"`
	Definition string `json:"definition"`
	// Roles are the participant roles this class expects. It is documentation
	// and prompt material and a check on what came back, not a requirement: a
	// provision naming an act without saying who does it is normal drafting.
	Roles []string `json:"roles,omitempty"`
	// SurfaceFormsVI is documentation and prompt material. Nothing in this
	// package matches text against these strings.
	SurfaceFormsVI []string `json:"surface_forms_vi,omitempty"`
}

// The participant roles. Five, because a role set nobody can hold in their head
// is a role set annotators disagree on, and role accuracy is one of the numbers
// this layer is judged by.
const (
	// RoleAgent is who performs the act. It is the role that decides whether a
	// chain can be walked as a duty of anybody.
	RoleAgent = "AGENT"
	// RoleObject is what the act is done to or about.
	RoleObject = "OBJECT"
	// RoleRecipient is the party the act is directed at, which is not the same
	// as what it is done to: a notice is given about a decision to a worker.
	RoleRecipient = "RECIPIENT"
	// RoleAuthority is the body whose power the act is exercised under, when it
	// is somebody other than the agent.
	RoleAuthority = "AUTHORITY"
	// RoleInstrument is the document or record the act is carried out with.
	RoleInstrument = "INSTRUMENT"
)

// Roles in a fixed order, so a prompt built from them is byte identical between
// runs.
var Roles = []string{RoleAgent, RoleObject, RoleRecipient, RoleAuthority, RoleInstrument}

// RoleKinds is the concept kinds a role may be filled by. A role with no entry
// accepts any kind, which is the honest position for OBJECT: a statute regulates
// acts done to documents, to money, to people and to land.
var RoleKinds = map[string][]string{
	RoleAgent:      {concept.KindActor, concept.KindBody},
	RoleRecipient:  {concept.KindActor, concept.KindBody},
	RoleAuthority:  {concept.KindBody, concept.KindActor},
	RoleInstrument: {concept.KindArtifact},
}

// The event to event edge types.
//
// Four, and each one answers a question somebody actually asks. Triggers is the
// consequence chain, precedes is procedural order, precondition is what has to
// have happened first, and precludes is the act that stops another act being
// available. There is no generic FOLLOWS, for the same reason there is no
// RELATED_TO in the relation registry: an edge saying only that two acts are
// connected cannot be queried for anything.
const (
	Triggers       = "TRIGGERS"
	Precedes       = "PRECEDES"
	PreconditionOf = "PRECONDITION_OF"
	Precludes      = "PRECLUDES"
)

// ChainType is one event to event edge type with the sentence that defines it.
type ChainType struct {
	ID         string `json:"id"`
	Definition string `json:"definition"`
}

// ChainTypes is the closed set of chain edges, in fixed order.
var ChainTypes = []ChainType{
	{Triggers, "Việc X xảy ra làm phát sinh Y, nghĩa là Y là hệ quả pháp lý của X"},
	{Precedes, "X được thực hiện trước Y trong cùng một trình tự, thủ tục"},
	{PreconditionOf, "Y chỉ được thực hiện khi X đã hoàn thành"},
	{Precludes, "Khi X đã xảy ra thì Y không còn được thực hiện"},
}

// ValidChain reports whether a chain type is one of the four.
func ValidChain(t string) bool {
	for _, c := range ChainTypes {
		if c.ID == t {
			return true
		}
	}
	return false
}

// Seed is the starting class vocabulary, in fixed order.
var Seed = []Class{
	{
		ID:             Submit,
		Definition:     "Một chủ thể nộp hồ sơ, đơn, tờ khai hoặc tài liệu cho một cơ quan hoặc một bên khác",
		Roles:          []string{RoleAgent, RoleObject, RoleRecipient},
		SurfaceFormsVI: []string{"nộp", "gửi hồ sơ", "đệ trình"},
	},
	{
		ID:             Issue,
		Definition:     "Một cơ quan cấp hoặc ban hành một giấy tờ, giấy phép hoặc văn bản",
		Roles:          []string{RoleAgent, RoleObject, RoleRecipient},
		SurfaceFormsVI: []string{"cấp", "ban hành", "cấp lại"},
	},
	{
		ID:             Notify,
		Definition:     "Một chủ thể báo cho một bên khác biết một sự việc hoặc một quyết định",
		Roles:          []string{RoleAgent, RoleObject, RoleRecipient},
		SurfaceFormsVI: []string{"thông báo", "báo cáo", "báo trước"},
	},
	{
		ID:             Register,
		Definition:     "Một chủ thể đăng ký hoặc một cơ quan ghi nhận vào sổ, vào danh bạ",
		Roles:          []string{RoleAgent, RoleObject, RoleAuthority},
		SurfaceFormsVI: []string{"đăng ký", "ghi vào sổ", "cấp mã số"},
	},
	{
		ID:             Declare,
		Definition:     "Một chủ thể khai, kê khai hoặc tự xác định số liệu thuộc trách nhiệm của mình",
		Roles:          []string{RoleAgent, RoleObject, RoleRecipient},
		SurfaceFormsVI: []string{"kê khai", "khai báo", "tự tính"},
	},
	{
		ID:             Inspect,
		Definition:     "Một cơ quan kiểm tra, thanh tra hoặc giám sát một chủ thể hoặc một hoạt động",
		Roles:          []string{RoleAgent, RoleObject, RoleAuthority},
		SurfaceFormsVI: []string{"kiểm tra", "thanh tra", "giám sát"},
	},
	{
		ID:             Decide,
		Definition:     "Một cơ quan quyết định, phê duyệt, chấp thuận hoặc từ chối một đề nghị",
		Roles:          []string{RoleAgent, RoleObject, RoleAuthority},
		SurfaceFormsVI: []string{"quyết định", "phê duyệt", "chấp thuận", "từ chối"},
	},
	{
		ID:             Pay,
		Definition:     "Một chủ thể nộp hoặc trả một khoản tiền cho một bên khác",
		Roles:          []string{RoleAgent, RoleObject, RoleRecipient},
		SurfaceFormsVI: []string{"nộp thuế", "trả lương", "thanh toán"},
	},
	{
		ID:             Penalise,
		Definition:     "Một cơ quan xử phạt hoặc xử lý một hành vi vi phạm",
		Roles:          []string{RoleAgent, RoleObject, RoleAuthority},
		SurfaceFormsVI: []string{"xử phạt", "xử lý vi phạm", "phạt tiền"},
	},
	{
		ID:             Suspend,
		Definition:     "Một cơ quan đình chỉ hoặc tạm dừng một hoạt động, một giấy tờ hoặc một quyền",
		Roles:          []string{RoleAgent, RoleObject, RoleAuthority},
		SurfaceFormsVI: []string{"đình chỉ", "tạm đình chỉ", "tạm dừng"},
	},
	{
		ID:             Revoke,
		Definition:     "Một cơ quan thu hồi, tước hoặc huỷ bỏ một giấy tờ, một quyền hoặc một khoản tiền",
		Roles:          []string{RoleAgent, RoleObject, RoleAuthority},
		SurfaceFormsVI: []string{"thu hồi", "tước", "huỷ bỏ"},
	},
	{
		ID:             Conclude,
		Definition:     "Hai bên giao kết hoặc ký kết một hợp đồng, một thoả thuận",
		Roles:          []string{RoleAgent, RoleObject, RoleRecipient},
		SurfaceFormsVI: []string{"giao kết", "ký kết", "ký"},
	},
	{
		ID:             Terminate,
		Definition:     "Một bên chấm dứt một hợp đồng, một quan hệ hoặc một hoạt động",
		Roles:          []string{RoleAgent, RoleObject, RoleRecipient},
		SurfaceFormsVI: []string{"chấm dứt", "thanh lý", "đơn phương chấm dứt"},
	},
	{
		ID:             Transfer,
		Definition:     "Một bên chuyển giao, chuyển nhượng hoặc bàn giao một tài sản, một quyền hoặc một hồ sơ cho bên khác",
		Roles:          []string{RoleAgent, RoleObject, RoleRecipient},
		SurfaceFormsVI: []string{"chuyển giao", "chuyển nhượng", "bàn giao"},
	},
	{
		ID:             Compensate,
		Definition:     "Một bên bồi thường, hoàn trả hoặc khắc phục thiệt hại cho bên khác",
		Roles:          []string{RoleAgent, RoleObject, RoleRecipient},
		SurfaceFormsVI: []string{"bồi thường", "hoàn trả", "khắc phục hậu quả"},
	},
	{
		ID:             Complain,
		Definition:     "Một chủ thể khiếu nại, tố cáo hoặc khởi kiện về một quyết định hoặc một hành vi",
		Roles:          []string{RoleAgent, RoleObject, RoleRecipient},
		SurfaceFormsVI: []string{"khiếu nại", "tố cáo", "khởi kiện"},
	},
	{
		ID:             Publish,
		Definition:     "Một chủ thể công bố, niêm yết hoặc công khai một thông tin, một văn bản",
		Roles:          []string{RoleAgent, RoleObject, RoleRecipient},
		SurfaceFormsVI: []string{"công bố", "niêm yết", "công khai"},
	},
	{
		ID:             Establish,
		Definition:     "Một chủ thể thành lập hoặc tổ chức một đơn vị, một cơ sở, một hội đồng",
		Roles:          []string{RoleAgent, RoleObject, RoleAuthority},
		SurfaceFormsVI: []string{"thành lập", "tổ chức", "sáp nhập"},
	},
}

// Registry is the class vocabulary at one ontology version, which is what an
// event cites when it says it is canonical.
type Registry struct {
	Version int     `json:"version"`
	Classes []Class `json:"classes"`
}

// SeedRegistry returns the seed vocabulary at a version.
func SeedRegistry(version int) *Registry {
	return &Registry{Version: version, Classes: append([]Class(nil), Seed...)}
}

// Class returns the named class, or nil.
func (r *Registry) Class(id string) *Class {
	for i := range r.Classes {
		if r.Classes[i].ID == id {
			return &r.Classes[i]
		}
	}
	return nil
}

// IDs returns the class identifiers in registry order.
func (r *Registry) IDs() []string {
	out := make([]string, 0, len(r.Classes))
	for _, c := range r.Classes {
		out = append(out, c.ID)
	}
	return out
}

// Forbidden names the act types no automated pass may emit whatever the registry
// holds. They are the generic attractor: a model with nothing specific to say
// reaches for the vaguest act available, and an event node meaning "something
// was done" is a node no question can be asked of.
var Forbidden = map[string]bool{
	"ACTION":     true,
	"ACT":        true,
	"EVENT":      true,
	"DO":         true,
	"PERFORM":    true,
	"HANH_VI":    true,
	"SU_KIEN":    true,
	"THUC_HIEN":  true,
	"OTHER":      true,
	"MISC":       true,
	"PROCESS":    true,
	"ACTIVITY":   true,
	"HOAT_DONG":  true,
	"THI_HANH":   true,
	"QUY_DINH":   true,
	"REGULATION": true,
}

// Status says whether an event or a chain is trusted, with the same meaning and
// the same patience as the relation layer: one provision saying a thing is one
// sentence, and promoting on it is the cheapest way to put a confident
// hallucination in the graph.
const (
	StatusCanonical   = "canonical"
	StatusProvisional = "provisional"
)

// Reasons something is provisional.
const (
	WhyUnknownClass   = "unknown_class"
	WhySingleSupport  = "single_support"
	WhyDirectionWrong = "direction_wrong"
)

// Evidence is one provision supporting an event or a chain, with a quote
// verified byte for byte at its offsets.
type Evidence struct {
	ProvisionID string `json:"provision_id"`
	DocID       string `json:"doc_id,omitempty"`
	Quote       string `json:"quote"`
	CharStart   int    `json:"char_start"`
	CharEnd     int    `json:"char_end"`
	// AsWritten is the act as the drafter wrote it, kept after folding because
	// the node label is canonical and this is what the text said.
	AsWritten string `json:"as_written,omitempty"`
	// DirectionCheck is the model's prose statement of which way a chain runs,
	// written while the quote was in view. It is read by a person when the blind
	// verifier disagrees.
	DirectionCheck string `json:"direction_check,omitempty"`
}

// Participant is one concept playing one role in an act.
//
// The concept identifier is required and the label is carried beside it. A
// participant that could not be resolved against the concept layer is not
// recorded: an edge to a party nobody can name is not queryable, and inventing a
// node for it would put the string back where this package took it out of.
type Participant struct {
	Role      string `json:"role"`
	ConceptID string `json:"concept_id"`
	LabelVI   string `json:"label_vi,omitempty"`
	// AsWritten is the phrase in the provision, which is often longer and
	// always more specific than the concept label.
	AsWritten    string `json:"as_written,omitempty"`
	SupportCount int    `json:"support_count,omitempty"`
}

// Occurrence is one provision naming one act. It is what the extractor returns
// and what a document's sighting file holds, before anything is folded.
type Occurrence struct {
	EventID   string `json:"event_id"`
	Class     string `json:"class"`
	LabelVI   string `json:"label_vi"`
	AsWritten string `json:"as_written,omitempty"`
	// Definition is the model's one sentence account of a class the registry
	// does not hold. Required for such a class, because it is the only thing the
	// candidates queue can be reviewed on.
	Definition   string        `json:"definition,omitempty"`
	Participants []Participant `json:"participants,omitempty"`
	Evidence     Evidence      `json:"evidence"`
	Confidence   float64       `json:"confidence"`
}

// Event is the folded node: one act type, everything the corpus said about it,
// and enough evidence to argue with it.
type Event struct {
	ID      string `json:"id"`
	Class   string `json:"class"`
	LabelVI string `json:"label_vi"`
	Status  string `json:"status"`
	Why     string `json:"why,omitempty"`
	// Definition is carried for a class the registry does not hold.
	Definition string `json:"definition,omitempty"`
	// Aliases are the other phrases the corpus used for this act, which is how
	// a merged node says what it merged.
	Aliases      []string      `json:"aliases,omitempty"`
	Participants []Participant `json:"participants,omitempty"`
	Evidence     []Evidence    `json:"evidence"`
	SupportCount int           `json:"support_count"`
	// SupportDocs counts distinct documents, because forty provisions in one
	// decree is one drafter repeating themselves.
	SupportDocs     int     `json:"support_docs"`
	Confidence      float64 `json:"confidence"`
	OntologyVersion int     `json:"ontology_version,omitempty"`
}

// Chain is one event to event edge.
type Chain struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Why    string `json:"why,omitempty"`

	Evidence     []Evidence `json:"evidence"`
	SupportCount int        `json:"support_count"`
	SupportDocs  int        `json:"support_docs"`
	Confidence   float64    `json:"confidence"`
	// Direction is the outcome of the blind second pass, kept separate from the
	// chain being right at all. A consequence graph with good precision and
	// mediocre direction walks confidently to the wrong consequence, which is
	// the failure this project has already shipped once in the amendment layer.
	Direction       string `json:"direction,omitempty"`
	OntologyVersion int    `json:"ontology_version,omitempty"`
}

// Direction verdicts, with the same meanings as the relation layer's.
const (
	DirectionUnverified = ""
	DirectionAgreed     = "agreed"
	DirectionFlipped    = "flipped"
	DirectionUnclear    = "unclear"
	DirectionDisputed   = "disputed"
)

// Key identifies a chain for folding. Direction is part of it, because a
// flipped chain is a different claim and not a duplicate.
func (c Chain) Key() string { return c.FromID + "|" + c.Type + "|" + c.ToID }

// Reverse returns the chain with its ends swapped, keeping the evidence.
func (c Chain) Reverse() Chain {
	c.FromID, c.ToID = c.ToID, c.FromID
	return c
}

// ID builds the corpus wide identifier of an act. Two provisions naming the same
// act with the same class produce the same identifier, in any document, which is
// the whole point of the layer.
func ID(class, label string) string {
	slug := law.Slug(label)
	if slug == "" {
		return ""
	}
	return "vn:event:" + strings.ToLower(class) + ":" + slug
}

// Kinds is the concept kind of every participant the checker knows about. A
// missing entry is missing evidence rather than a violation, the same reading
// the relation checker uses.
type Kinds map[string]string

// Validate checks one occurrence against the registry and the concept kinds. A
// failure rejects the occurrence rather than logging it.
func (o Occurrence) Validate(r *Registry, kinds Kinds) error {
	if o.Class == "" {
		return fmt.Errorf("an act with no class")
	}
	if Forbidden[strings.ToUpper(o.Class)] {
		return fmt.Errorf("%s says only that something was done", o.Class)
	}
	if strings.TrimSpace(o.LabelVI) == "" {
		return fmt.Errorf("%s has no label", o.Class)
	}
	if o.EventID != ID(o.Class, o.LabelVI) {
		return fmt.Errorf("%s does not identify %s %s", o.EventID, o.Class, o.LabelVI)
	}
	if o.Evidence.ProvisionID == "" || o.Evidence.Quote == "" {
		return fmt.Errorf("%s has no evidence, and an act nobody can check is an assertion", o.EventID)
	}
	if o.Confidence < 0 || o.Confidence > 1 {
		return fmt.Errorf("confidence %v is outside 0 to 1", o.Confidence)
	}
	if r.Class(o.Class) == nil && strings.TrimSpace(o.Definition) == "" {
		return fmt.Errorf("%s is not in registry v%d and carries no definition, so it can never be reviewed", o.Class, r.Version)
	}
	seen := map[string]bool{}
	for _, p := range o.Participants {
		if err := p.validate(kinds); err != nil {
			return fmt.Errorf("%s: %w", o.EventID, err)
		}
		key := p.Role + "|" + p.ConceptID
		if seen[key] {
			return fmt.Errorf("%s has %s twice as %s", o.EventID, p.ConceptID, p.Role)
		}
		seen[key] = true
	}
	return nil
}

func (p Participant) validate(kinds Kinds) error {
	if !validRole(p.Role) {
		return fmt.Errorf("%q is not one of the roles %s", p.Role, strings.Join(Roles, ", "))
	}
	if p.ConceptID == "" {
		return fmt.Errorf("a %s that names no concept", p.Role)
	}
	want, constrained := RoleKinds[p.Role]
	kind, known := kinds[p.ConceptID]
	if constrained && known && !slices.Contains(want, kind) {
		return fmt.Errorf("%s is filled by a %s and %s is a %s", p.Role, strings.Join(want, "|"), p.ConceptID, kind)
	}
	return nil
}

func validRole(role string) bool { return slices.Contains(Roles, role) }

// Validate checks one chain. Both ends must exist in the layer, because a chain
// to an act nobody extracted cannot be walked and cannot be read.
func (c Chain) Validate(events map[string]bool) error {
	if c.FromID == "" || c.ToID == "" {
		return fmt.Errorf("a chain needs two ends")
	}
	if c.FromID == c.ToID {
		return fmt.Errorf("%s chains to itself", c.FromID)
	}
	if !ValidChain(c.Type) {
		return fmt.Errorf("%q is not one of the chain types", c.Type)
	}
	if len(c.Evidence) == 0 {
		return fmt.Errorf("%s has no evidence", c.Key())
	}
	if events != nil {
		if !events[c.FromID] {
			return fmt.Errorf("%s chains from %s which the layer does not hold", c.Key(), c.FromID)
		}
		if !events[c.ToID] {
			return fmt.Errorf("%s chains to %s which the layer does not hold", c.Key(), c.ToID)
		}
	}
	switch c.Status {
	case StatusCanonical:
		if c.SupportCount <= 1 {
			return fmt.Errorf("%s is canonical on a single provision", c.Key())
		}
		if c.Direction == DirectionFlipped || c.Direction == DirectionDisputed {
			return fmt.Errorf("%s is canonical and the blind direction pass read it as %s", c.Key(), c.Direction)
		}
	case StatusProvisional:
	default:
		return fmt.Errorf("status %q is not %s or %s", c.Status, StatusCanonical, StatusProvisional)
	}
	return nil
}

// SortEvents orders events so two runs over one corpus produce one file.
func SortEvents(in []Event) {
	sort.SliceStable(in, func(i, j int) bool { return in[i].ID < in[j].ID })
}

// SortChains orders chains the same way.
func SortChains(in []Chain) {
	sort.SliceStable(in, func(i, j int) bool {
		a, b := in[i], in[j]
		if a.FromID != b.FromID {
			return a.FromID < b.FromID
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.ToID < b.ToID
	})
}

// SortParticipants orders participants by role then concept.
func SortParticipants(in []Participant) {
	sort.SliceStable(in, func(i, j int) bool {
		a, b := in[i], in[j]
		if a.Role != b.Role {
			return roleOrder(a.Role) < roleOrder(b.Role)
		}
		return a.ConceptID < b.ConceptID
	})
}

func roleOrder(role string) int {
	for i, r := range Roles {
		if r == role {
			return i
		}
	}
	return len(Roles)
}
