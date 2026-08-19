package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/eval"
)

// Term is one thing to place in a taxonomy. Parent is the hand written answer
// and is never shown to the model.
type Term struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Parent string `json:"parent,omitempty"`
}

// The two directions LLMs4OL compares. They are run separately and scored
// separately because they fail differently: asking a child for its parent
// always produces one answer and can produce a confident wrong one, and asking
// a parent for its children can leave a child unclaimed or let two parents
// claim it, which the other direction cannot express.
const (
	BottomUp = "bottom-up"
	TopDown  = "top-down"
)

// NoParent is the answer a model gives when nothing in the list fits. It is a
// permitted answer, because a pass that must always choose will always place
// every term and its accuracy will be a measure of the list rather than of the
// model.
const NoParent = "none"

// Inducer runs taxonomy induction in one direction or the other.
type Inducer struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
}

func corrections(n int) int {
	if n <= 0 {
		return 2
	}
	return n
}

// Induced is one direction's output: the parent links it produced, and, for the
// top down direction, every parent that laid claim to a child.
type Induced struct {
	Direction string              `json:"direction"`
	Links     map[string]string   `json:"links"`
	Claims    map[string][]string `json:"claims,omitempty"`
	Usage     api.Usage           `json:"usage"`
	Calls     int                 `json:"calls"`
}

func newInduced(direction string) *Induced {
	return &Induced{Direction: direction, Links: map[string]string{}, Claims: map[string][]string{}}
}

// bottomUpInstructions is the child asking side.
func (in *Inducer) bottomUpInstructions() string {
	var b strings.Builder
	b.WriteString("Bạn được cho tên một lĩnh vực pháp luật hẹp và danh sách các lĩnh vực rộng.\n")
	b.WriteString("Nhiệm vụ: chọn đúng một lĩnh vực rộng chứa lĩnh vực hẹp đó.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. Chỉ chọn một mã trong danh sách được liệt kê, không tự đặt mã mới.\n")
	b.WriteString("2. Nếu không lĩnh vực rộng nào chứa nó, trả về \"" + NoParent + "\". Đây là câu trả lời hợp lệ.\n")
	b.WriteString("3. Chọn theo nghĩa của lĩnh vực, không theo từ ngữ trùng nhau.\n\n")
	b.WriteString("Trả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"parent":"thue","confidence":0.9,"rationale":"..."}`)
	b.WriteString("\n")
	return b.String()
}

// BottomUpPrompt renders one child placement question.
func BottomUpPrompt(child Term, parents []Term) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lĩnh vực hẹp: %s\n\n", child.Label)
	b.WriteString("Các lĩnh vực rộng:\n")
	for _, p := range parents {
		fmt.Fprintf(&b, "  [%s] %s\n", p.ID, p.Label)
	}
	return b.String()
}

type wireParent struct {
	Parent     string  `json:"parent"`
	Confidence float64 `json:"confidence"`
	Rationale  string  `json:"rationale"`
}

// Placement is one child placed, or explicitly not placed.
type Placement struct {
	ChildID    string  `json:"child_id"`
	ParentID   string  `json:"parent_id,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Rationale  string  `json:"rationale,omitempty"`
}

// Place asks for one child's parent.
func (in *Inducer) Place(ctx context.Context, child Term, parents []Term) (Placement, api.Usage, error) {
	var usage api.Usage
	allowed := map[string]bool{}
	for _, p := range parents {
		allowed[p.ID] = true
	}
	input := BottomUpPrompt(child, parents)
	for attempt := 0; attempt <= corrections(in.MaxCorrections); attempt++ {
		resp, err := in.Completer.Complete(ctx, api.Request{
			Model: in.Model, Instructions: in.bottomUpInstructions(), Input: input,
		})
		if err != nil {
			return Placement{ChildID: child.ID}, usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var wire wireParent
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			input = BottomUpPrompt(child, parents) + "\nLần trả lời trước không phải một đối tượng JSON hợp lệ: " + err.Error() + "\nTrả lời lại.\n"
			continue
		}
		if wire.Parent == "" || strings.EqualFold(wire.Parent, NoParent) {
			return Placement{ChildID: child.ID, Rationale: wire.Rationale}, usage, nil
		}
		if !allowed[wire.Parent] {
			input = BottomUpPrompt(child, parents) +
				fmt.Sprintf("\nLần trả lời trước bị từ chối: %q không nằm trong danh sách.\nTrả lời lại.\n", wire.Parent)
			continue
		}
		return Placement{ChildID: child.ID, ParentID: wire.Parent,
			Confidence: wire.Confidence, Rationale: wire.Rationale}, usage, nil
	}
	// An answer that never arrived is not a placement. Recording it as one
	// would put a link in the taxonomy that nothing said.
	return Placement{ChildID: child.ID}, usage, nil
}

// topDownInstructions is the parent asking side.
func (in *Inducer) topDownInstructions() string {
	var b strings.Builder
	b.WriteString("Bạn được cho tên một lĩnh vực pháp luật rộng và danh sách các lĩnh vực hẹp.\n")
	b.WriteString("Nhiệm vụ: chọn tất cả các lĩnh vực hẹp thuộc lĩnh vực rộng đó.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. Chỉ chọn các mã trong danh sách được liệt kê, không tự đặt mã mới.\n")
	b.WriteString("2. Được phép trả về danh sách rỗng nếu không có mã nào thuộc lĩnh vực rộng này.\n")
	b.WriteString("3. Chọn theo nghĩa của lĩnh vực, không theo từ ngữ trùng nhau.\n\n")
	b.WriteString("Trả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"children":["thue-thu-nhap","thue-gtgt"]}`)
	b.WriteString("\n")
	return b.String()
}

// TopDownPrompt renders one parent's question.
func TopDownPrompt(parent Term, children []Term) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lĩnh vực rộng: %s\n\n", parent.Label)
	b.WriteString("Các lĩnh vực hẹp:\n")
	for _, c := range children {
		fmt.Fprintf(&b, "  [%s] %s\n", c.ID, c.Label)
	}
	return b.String()
}

type wireChildren struct {
	Children []string `json:"children"`
}

// Claim asks one parent which children are its own.
func (in *Inducer) Claim(ctx context.Context, parent Term, children []Term) ([]string, api.Usage, error) {
	var usage api.Usage
	allowed := map[string]bool{}
	for _, c := range children {
		allowed[c.ID] = true
	}
	input := TopDownPrompt(parent, children)
	for attempt := 0; attempt <= corrections(in.MaxCorrections); attempt++ {
		resp, err := in.Completer.Complete(ctx, api.Request{
			Model: in.Model, Instructions: in.topDownInstructions(), Input: input,
		})
		if err != nil {
			return nil, usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var wire wireChildren
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			input = TopDownPrompt(parent, children) + "\nLần trả lời trước không phải một đối tượng JSON hợp lệ: " + err.Error() + "\nTrả lời lại.\n"
			continue
		}
		var bad []string
		var out []string
		for _, id := range wire.Children {
			if allowed[id] {
				out = append(out, id)
				continue
			}
			bad = append(bad, id)
		}
		if len(bad) > 0 {
			input = TopDownPrompt(parent, children) +
				fmt.Sprintf("\nLần trả lời trước bị từ chối: %s không nằm trong danh sách.\nTrả lời lại.\n", strings.Join(bad, ", "))
			continue
		}
		sort.Strings(out)
		return out, usage, nil
	}
	return nil, usage, nil
}

// InduceBottomUp places every child by asking the child.
func (in *Inducer) InduceBottomUp(ctx context.Context, children, parents []Term, onEach func(Placement)) (*Induced, error) {
	out := newInduced(BottomUp)
	for _, c := range children {
		p, usage, err := in.Place(ctx, c, parents)
		out.Usage = addUsage(out.Usage, usage)
		out.Calls++
		if err != nil {
			return out, err
		}
		if p.ParentID != "" {
			out.Links[c.ID] = p.ParentID
			out.Claims[c.ID] = []string{p.ParentID}
		}
		if onEach != nil {
			onEach(p)
		}
	}
	return out, nil
}

// InduceTopDown places every child by asking each parent what it owns.
//
// A child two parents claim is left out of Links and kept in Claims. Choosing
// one of them by a rule invented here would report an accuracy for a decision
// the pass did not make.
func (in *Inducer) InduceTopDown(ctx context.Context, children, parents []Term, onEach func(Term, []string)) (*Induced, error) {
	out := newInduced(TopDown)
	for _, p := range parents {
		got, usage, err := in.Claim(ctx, p, children)
		out.Usage = addUsage(out.Usage, usage)
		out.Calls++
		if err != nil {
			return out, err
		}
		for _, id := range got {
			out.Claims[id] = append(out.Claims[id], p.ID)
		}
		if onEach != nil {
			onEach(p, got)
		}
	}
	for id, claims := range out.Claims {
		if len(claims) == 1 {
			out.Links[id] = claims[0]
		}
	}
	return out, nil
}

// Contested lists the children more than one parent claimed.
func (in *Induced) Contested() []string {
	var out []string
	for id, claims := range in.Claims {
		if len(claims) > 1 {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// Unclaimed lists the children with no parent at all.
func (in *Induced) Unclaimed(children []Term) []string {
	var out []string
	for _, c := range children {
		if len(in.Claims[c.ID]) == 0 {
			out = append(out, c.ID)
		}
	}
	sort.Strings(out)
	return out
}

// Mistake is one child placed under the wrong parent, kept with both answers so
// a reader can see whether the induced parent was defensible.
type Mistake struct {
	ChildID string `json:"child_id"`
	Got     string `json:"got"`
	Want    string `json:"want"`
}

// TaxonomyScore is one direction measured against the hand written parents.
//
// Placed and Correct are kept apart deliberately. A direction that places
// everything and gets four in five right, and one that places half and gets all
// of those right, are different systems, and a single accuracy figure makes
// them look like the same one.
type TaxonomyScore struct {
	Direction string         `json:"direction"`
	Placed    eval.Accuracy  `json:"placed"`
	Correct   eval.Accuracy  `json:"correct"`
	Overall   eval.Accuracy  `json:"overall"`
	Contested []string       `json:"contested,omitempty"`
	Unplaced  []string       `json:"unplaced,omitempty"`
	Mistakes  []Mistake      `json:"mistakes,omitempty"`
	Structure StructureCheck `json:"structure"`
	Calls     int            `json:"calls"`
	Usage     api.Usage      `json:"usage"`
}

// StructureCheck is what the induced links look like as a graph, whatever they
// look like as answers.
//
// OLLM reports that prompting alone produces structurally unsound ontologies,
// measured with Motif Distance. This is not that metric and does not claim to
// be. It is the three structural facts this shape can carry: a child under two
// parents, a cycle, and a child under nobody.
type StructureCheck struct {
	Children    int      `json:"children"`
	Linked      int      `json:"linked"`
	MultiParent int      `json:"multi_parent"`
	Cycles      []string `json:"cycles,omitempty"`
	Orphans     int      `json:"orphans"`
}

// ScoreTaxonomy compares induced links against the gold parents.
func ScoreTaxonomy(induced *Induced, children []Term) TaxonomyScore {
	s := TaxonomyScore{Direction: induced.Direction, Calls: induced.Calls, Usage: induced.Usage}
	for _, c := range children {
		got, placed := induced.Links[c.ID]
		s.Placed.Observe(placed)
		s.Overall.Observe(placed && got == c.Parent)
		if !placed {
			continue
		}
		s.Correct.Observe(got == c.Parent)
		if got != c.Parent {
			s.Mistakes = append(s.Mistakes, Mistake{ChildID: c.ID, Got: got, Want: c.Parent})
		}
	}
	s.Contested = induced.Contested()
	s.Unplaced = induced.Unclaimed(children)
	s.Structure = CheckStructure(induced, children)
	sort.Slice(s.Mistakes, func(i, j int) bool { return s.Mistakes[i].ChildID < s.Mistakes[j].ChildID })
	return s
}

// CheckStructure looks at the induced links as a graph.
func CheckStructure(induced *Induced, children []Term) StructureCheck {
	c := StructureCheck{Children: len(children), Linked: len(induced.Links)}
	for _, claims := range induced.Claims {
		if len(claims) > 1 {
			c.MultiParent++
		}
	}
	for _, ch := range children {
		if len(induced.Claims[ch.ID]) == 0 {
			c.Orphans++
		}
	}
	for _, ch := range children {
		if cycle := walkCycle(induced.Links, ch.ID, len(children)+1); cycle != "" {
			c.Cycles = append(c.Cycles, cycle)
		}
	}
	sort.Strings(c.Cycles)
	c.Cycles = dedupe(c.Cycles)
	return c
}

// walkCycle follows parent links from one node and reports the node it comes
// back to, or the empty string. The walk is bounded by the node count, so a
// cycle terminates the walk instead of hanging it.
func walkCycle(links map[string]string, start string, limit int) string {
	seen := map[string]bool{start: true}
	at := start
	for range limit {
		next, ok := links[at]
		if !ok {
			return ""
		}
		if seen[next] {
			return next
		}
		seen[next] = true
		at = next
	}
	return ""
}

func dedupe(in []string) []string {
	out := in[:0]
	var last string
	for i, s := range in {
		if i == 0 || s != last {
			out = append(out, s)
		}
		last = s
	}
	return out
}

// Agreement is how often the two directions place a child the same way, over
// the children both of them placed.
//
// It is worth having beside the accuracies because it is the number available
// on a corpus with no gold at all, and knowing how much it tracks accuracy here
// says how far it can be trusted there.
func Agreement(a, b *Induced, children []Term) eval.Accuracy {
	var acc eval.Accuracy
	for _, c := range children {
		x, okA := a.Links[c.ID]
		y, okB := b.Links[c.ID]
		if !okA || !okB {
			continue
		}
		acc.Observe(x == y)
	}
	return acc
}

func (s TaxonomyScore) String() string {
	t := eval.NewTable("taxonomy induction, "+s.Direction, fmt.Sprintf("%d terms", s.Placed.Of))
	t.Rate("terms the pass placed", s.Placed)
	t.Rate("placed terms under the hand written parent", s.Correct)
	t.Rate("terms under the hand written parent", s.Overall)
	t.Note("%d calls, %d tokens", s.Calls, s.Usage.TotalTokens)
	t.Note("structure: %d of %d linked, %d claimed by more than one parent, %d orphans, %d cycles",
		s.Structure.Linked, s.Structure.Children, s.Structure.MultiParent, s.Structure.Orphans, len(s.Structure.Cycles))
	if len(s.Contested) > 0 {
		t.Note("contested: %s", strings.Join(s.Contested, ", "))
	}
	return t.String()
}
