package event

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tamnd/luatdo/api"
)

// Candidate is one concept offered to the model for a provision, the same
// bounded set the relation extractor works from and for the same reason: asking
// for acts and their participants at once, with the participants to be found as
// well, is two tasks and fails at both.
type Candidate struct {
	ID      string
	LabelVI string
	Kind    string
}

// Norm is one trusted statement already extracted from this provision, offered
// so the model can say which act each statement is about.
//
// This is how the action slot stops being a dead end, and it is done here rather
// than by matching strings afterwards. The norm layer and this pass read the
// same provision, so the alignment is a question about one paragraph that
// somebody can check, while matching a folded action phrase against a folded act
// label across the corpus is a coincidence detector that would confidently link
// trả lương in a labour code to trả lương in a tax decree.
type Norm struct {
	StatementID string
	Type        string
	Action      string
	Sanction    string
}

// Link is one norm slot pointing at an act.
type Link struct {
	StatementID string `json:"statement_id"`
	ProvisionID string `json:"provision_id"`
	DocID       string `json:"doc_id,omitempty"`
	EventID     string `json:"event_id"`
	Kind        string `json:"kind"`
}

// The two slots that can point at an act.
const (
	LinkAction   = "action"
	LinkSanction = "sanction"
)

// Result is what one provision produced.
type Result struct {
	Occurrences []Occurrence `json:"occurrences,omitempty"`
	Chains      []Chain      `json:"chains,omitempty"`
	Links       []Link       `json:"links,omitempty"`
}

// Extractor is the provision level read.
type Extractor struct {
	Completer      api.Completer
	Model          string
	Registry       *Registry
	MaxCorrections int
}

func (x *Extractor) registry() *Registry {
	if x.Registry != nil {
		return x.Registry
	}
	return SeedRegistry(1)
}

type wireParticipant struct {
	Role      string `json:"role"`
	ConceptID string `json:"concept_id"`
	AsWritten string `json:"as_written"`
}

// The wire form carries no byte offsets. A model that sends them anyway is not
// refused, the extra keys are ignored, and the offsets on the evidence are
// computed from the provision text.
type wireEvent struct {
	Class        string            `json:"class"`
	LabelVI      string            `json:"label_vi"`
	AsWritten    string            `json:"as_written"`
	Definition   string            `json:"class_definition"`
	Participants []wireParticipant `json:"participants"`
	Quote        string            `json:"quote"`
	Confidence   float64           `json:"confidence"`
}

type wireChain struct {
	From           string  `json:"from_event"`
	To             string  `json:"to_event"`
	Type           string  `json:"type"`
	Quote          string  `json:"quote"`
	DirectionCheck string  `json:"direction_check"`
	Confidence     float64 `json:"confidence"`
}

type wireLink struct {
	StatementID string `json:"statement_id"`
	Slot        string `json:"slot"`
	Event       string `json:"event"`
}

type wireResult struct {
	Events []wireEvent `json:"events"`
	Chains []wireChain `json:"chains"`
	Links  []wireLink  `json:"links"`
}

// Instructions is the system prompt. The model is asked for the acts a provision
// names, who takes part in them and how they follow from each other, and it is
// told in as many words that a provision naming no act is a normal answer.
func (x *Extractor) Instructions() string {
	var b strings.Builder
	b.WriteString("Bạn được cho một điều khoản của pháp luật Việt Nam, danh sách khái niệm đã xác định trong điều khoản đó, và có thể có danh sách quy phạm đã rút ra từ chính điều khoản đó.\n")
	b.WriteString("Nhiệm vụ: nêu các hành vi mà điều khoản này nói tới, ai tham gia vào từng hành vi, và quan hệ trước sau giữa các hành vi đó.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. Mỗi hành vi phải chọn một class trong danh sách có sẵn nếu đúng nghĩa. Nếu không có class nào đúng, hãy tự đặt tên bằng chữ in hoa và dấu gạch dưới, và bắt buộc mô tả nó ở class_definition bằng một câu.\n")
	b.WriteString("2. Không dùng những tên chung chung như HANH_VI, ACTION, THUC_HIEN hay OTHER. Nếu điều khoản không nói tới hành vi cụ thể nào, hãy trả về danh sách rỗng. Danh sách rỗng là câu trả lời đúng.\n")
	b.WriteString("3. label_vi là tên hành vi ở dạng ngắn gọn, có động từ, ví dụ: cấp giấy phép xây dựng. Cùng một hành vi ở hai văn bản khác nhau phải được đặt cùng một tên.\n")
	b.WriteString("4. quote phải sao chép nguyên văn từ nội dung điều khoản, đúng từng chữ và từng dấu.\n")
	b.WriteString("5. concept_id của người tham gia phải là mã của một khái niệm trong danh sách được cung cấp. Không nêu người tham gia mà điều khoản không nói tới.\n")
	b.WriteString("6. Quan hệ giữa hai hành vi chỉ được nêu khi chính câu chữ trong điều khoản thể hiện, và cả hai hành vi phải nằm trong danh sách hành vi bạn vừa nêu.\n")
	b.WriteString("7. direction_check phải viết bằng lời một câu nói rõ chiều của quan hệ, ví dụ: nộp hồ sơ xảy ra trước khi cấp giấy phép, không phải ngược lại.\n")
	b.WriteString("8. Nếu có danh sách quy phạm, với mỗi quy phạm hãy cho biết hành vi nào là hành vi của quy phạm đó (slot = action), và nếu quy phạm có chế tài thì hành vi nào là chế tài (slot = sanction). Chỉ nêu khi chắc chắn.\n")
	b.WriteString("9. Không suy đoán ngoài nội dung điều khoản.\n\n")

	b.WriteString("Các class hành vi có sẵn:\n")
	for _, c := range x.registry().Classes {
		fmt.Fprintf(&b, "  %s: %s\n", c.ID, c.Definition)
	}
	b.WriteString("\nCác vai trò của người tham gia:\n")
	b.WriteString("  " + RoleAgent + ": chủ thể thực hiện hành vi\n")
	b.WriteString("  " + RoleObject + ": đối tượng của hành vi\n")
	b.WriteString("  " + RoleRecipient + ": bên nhận, bên được hướng tới\n")
	b.WriteString("  " + RoleAuthority + ": cơ quan có thẩm quyền mà hành vi được thực hiện theo\n")
	b.WriteString("  " + RoleInstrument + ": giấy tờ, hồ sơ dùng để thực hiện hành vi\n")
	b.WriteString("\nCác quan hệ giữa hai hành vi:\n")
	for _, c := range ChainTypes {
		fmt.Fprintf(&b, "  %s: %s\n", c.ID, c.Definition)
	}
	b.WriteString("\nTrả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"events":[{"class":"SUBMIT","label_vi":"nộp hồ sơ đăng ký","as_written":"nộp hồ sơ","class_definition":"","participants":[{"role":"AGENT","concept_id":"...","as_written":"..."}],"quote":"...","confidence":0.9}],`)
	b.WriteString(`"chains":[{"from_event":"nộp hồ sơ đăng ký","to_event":"cấp giấy phép","type":"PRECEDES","quote":"...","direction_check":"...","confidence":0.9}],`)
	b.WriteString(`"links":[{"statement_id":"...","slot":"action","event":"nộp hồ sơ đăng ký"}]}`)
	b.WriteString("\n")
	return b.String()
}

// Prompt renders one provision, its concepts and its norms.
func Prompt(provisionID, text string, cands []Candidate, norms []Norm) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Điều khoản: %s\n\n", provisionID)
	fmt.Fprintf(&b, "Nội dung:\n%s\n\n", text)
	b.WriteString("Các khái niệm trong điều khoản:\n")
	for _, c := range cands {
		fmt.Fprintf(&b, "  [%s] %s (%s)\n", c.ID, c.LabelVI, c.Kind)
	}
	if len(norms) > 0 {
		b.WriteString("\nCác quy phạm đã rút ra từ điều khoản này:\n")
		for _, n := range norms {
			fmt.Fprintf(&b, "  [%s] %s: %s", n.StatementID, n.Type, n.Action)
			if n.Sanction != "" {
				fmt.Fprintf(&b, ", chế tài: %s", n.Sanction)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// Extract reads one provision.
//
// It returns the acts the provision names, each with a quote checked byte for
// byte and participants drawn from the concepts that were offered, the chains
// the provision itself states between those acts, and the links from the
// provision's own norms to them. A provision about nothing that happens returns
// nothing, which is a result and not a failure.
func (x *Extractor) Extract(ctx context.Context, provisionID, docID, text string, cands []Candidate, norms []Norm) (Result, api.Usage, error) {
	var usage api.Usage
	offered := map[string]Candidate{}
	for _, c := range cands {
		offered[c.ID] = c
	}
	statements := map[string]bool{}
	for _, n := range norms {
		statements[n.StatementID] = true
	}
	input := Prompt(provisionID, text, cands, norms)

	// The last refusal is carried out of the loop and into the error. Without it
	// a failed provision says only that it failed, and a whole document can go
	// down on one systematic problem with nothing on screen that names it.
	last := "the model was never asked"
	for attempt := 0; attempt <= maxCorrections(x.MaxCorrections); attempt++ {
		resp, err := x.Completer.Complete(ctx, api.Request{
			Model: x.Model, Instructions: x.Instructions(), Input: input,
		})
		if err != nil {
			return Result{}, usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var wire wireResult
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			last = "câu trả lời không phải một đối tượng JSON hợp lệ: " + err.Error()
			input = Prompt(provisionID, text, cands, norms) + correction(last)
			continue
		}
		out, problem := x.convert(wire, provisionID, docID, text, offered, statements)
		if problem != "" {
			last = problem
			input = Prompt(provisionID, text, cands, norms) + correction(problem)
			continue
		}
		return out, usage, nil
	}
	return Result{}, usage, fmt.Errorf("%s: the model did not produce a usable answer after %d corrections, last refused for: %s",
		provisionID, maxCorrections(x.MaxCorrections), last)
}

// convert turns the wire form into the layer's types, or names the first problem
// so the model can be told what was wrong instead of being asked again
// identically.
//
// The model names an act by its label and never by an identifier. Identifiers
// are computed here from the class and the label, so a model cannot mint one,
// and a chain naming an act the same answer did not describe is rejected rather
// than pointing at a node nobody defined.
func (x *Extractor) convert(wire wireResult, provisionID, docID, text string, offered map[string]Candidate, statements map[string]bool) (Result, string) {
	r := x.registry()
	var out Result
	byLabel := map[string]string{}
	seen := map[string]bool{}

	for _, w := range wire.Events {
		class := strings.ToUpper(strings.TrimSpace(w.Class))
		label := strings.TrimSpace(w.LabelVI)
		if class == "" {
			return Result{}, "một hành vi không có class"
		}
		if Forbidden[class] {
			return Result{}, fmt.Sprintf("%s là tên chung chung, không được dùng. Nếu không có hành vi cụ thể thì trả về danh sách rỗng", class)
		}
		if label == "" {
			return Result{}, fmt.Sprintf("hành vi %s không có label_vi", class)
		}
		id := ID(class, label)
		if id == "" {
			return Result{}, fmt.Sprintf("label_vi %q không tạo được mã", label)
		}
		start, end, err := locateQuote(text, w.Quote)
		if err != nil {
			return Result{}, fmt.Sprintf("đoạn trích %q: %v", w.Quote, err)
		}
		if w.Confidence < 0 || w.Confidence > 1 {
			return Result{}, fmt.Sprintf("confidence %v nằm ngoài khoảng 0 đến 1", w.Confidence)
		}
		o := Occurrence{
			EventID: id, Class: class, LabelVI: label,
			AsWritten:  strings.TrimSpace(w.AsWritten),
			Confidence: w.Confidence,
			Evidence: Evidence{
				ProvisionID: provisionID, DocID: docID,
				Quote: w.Quote, CharStart: start, CharEnd: end,
				AsWritten: strings.TrimSpace(w.AsWritten),
			},
		}
		if r.Class(class) == nil {
			// A class the registry does not hold carries the model's own
			// sentence forward. It is not dropped: the tail is where the
			// interesting law is, and the candidates queue is where it goes.
			o.Definition = strings.TrimSpace(w.Definition)
			if o.Definition == "" {
				return Result{}, fmt.Sprintf("class %s không có trong danh sách nên bắt buộc phải mô tả ở class_definition", class)
			}
		}
		for _, p := range w.Participants {
			role := strings.ToUpper(strings.TrimSpace(p.Role))
			if !validRole(role) {
				return Result{}, fmt.Sprintf("vai trò %q không nằm trong danh sách %s", p.Role, strings.Join(Roles, ", "))
			}
			c, ok := offered[p.ConceptID]
			if !ok {
				return Result{}, fmt.Sprintf("concept_id %q không nằm trong danh sách khái niệm được cung cấp", p.ConceptID)
			}
			o.Participants = append(o.Participants, Participant{
				Role: role, ConceptID: c.ID, LabelVI: c.LabelVI,
				AsWritten: strings.TrimSpace(p.AsWritten), SupportCount: 1,
			})
		}
		SortParticipants(o.Participants)
		if seen[id] {
			// One provision naming an act twice is one act. The second sighting
			// adds nothing a fold would not add, and keeping both would count a
			// single sentence as corroboration of itself.
			continue
		}
		seen[id] = true
		byLabel[key(label)] = id
		out.Occurrences = append(out.Occurrences, o)
	}

	for _, w := range wire.Chains {
		typ := strings.ToUpper(strings.TrimSpace(w.Type))
		if !ValidChain(typ) {
			return Result{}, fmt.Sprintf("quan hệ %q không nằm trong danh sách %s, %s, %s, %s", w.Type, Triggers, Precedes, PreconditionOf, Precludes)
		}
		from, okFrom := byLabel[key(w.From)]
		to, okTo := byLabel[key(w.To)]
		if !okFrom {
			return Result{}, fmt.Sprintf("quan hệ nêu hành vi %q không có trong danh sách hành vi bạn vừa nêu", w.From)
		}
		if !okTo {
			return Result{}, fmt.Sprintf("quan hệ nêu hành vi %q không có trong danh sách hành vi bạn vừa nêu", w.To)
		}
		if from == to {
			return Result{}, fmt.Sprintf("quan hệ nối hành vi %q với chính nó", w.From)
		}
		start, end, err := locateQuote(text, w.Quote)
		if err != nil {
			return Result{}, fmt.Sprintf("đoạn trích %q: %v", w.Quote, err)
		}
		if strings.TrimSpace(w.DirectionCheck) == "" {
			return Result{}, fmt.Sprintf("quan hệ %s thiếu direction_check", typ)
		}
		c := Chain{
			FromID: from, ToID: to, Type: typ,
			Status: StatusProvisional, Why: WhySingleSupport,
			Confidence:      w.Confidence,
			OntologyVersion: r.Version,
			Evidence: []Evidence{{
				ProvisionID: provisionID, DocID: docID,
				Quote: w.Quote, CharStart: start, CharEnd: end,
				DirectionCheck: strings.TrimSpace(w.DirectionCheck),
			}},
			SupportCount: 1, SupportDocs: 1,
		}
		out.Chains = append(out.Chains, c)
	}

	for _, w := range wire.Links {
		slot := strings.ToLower(strings.TrimSpace(w.Slot))
		if slot != LinkAction && slot != LinkSanction {
			return Result{}, fmt.Sprintf("slot %q phải là %s hoặc %s", w.Slot, LinkAction, LinkSanction)
		}
		if !statements[w.StatementID] {
			return Result{}, fmt.Sprintf("statement_id %q không nằm trong danh sách quy phạm được cung cấp", w.StatementID)
		}
		id, ok := byLabel[key(w.Event)]
		if !ok {
			return Result{}, fmt.Sprintf("liên kết nêu hành vi %q không có trong danh sách hành vi bạn vừa nêu", w.Event)
		}
		out.Links = append(out.Links, Link{
			StatementID: w.StatementID, ProvisionID: provisionID, DocID: docID,
			EventID: id, Kind: slot,
		})
	}

	SortChains(out.Chains)
	return out, ""
}

// key folds a label for lookup, so a chain naming an act with different spacing
// or case than the act it was written beside still finds it.
func key(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// locateQuote checks that a quote is in the provision and returns where.
//
// The quote has to appear word for word, which is the check that keeps an act
// from resting on a sentence nobody wrote. The byte offsets are computed here
// rather than taken from the model, because a byte offset into Vietnamese text
// is a count of UTF-8 continuation bytes and nobody can produce one by reading.
// The first real run of this pass lost two entire documents to that demand, and
// every quote it refused was in the paragraph it came from. The offsets exist so
// a person can find the sentence again, and the text is the authority on where
// the sentence is.
func locateQuote(text, quote string) (int, int, error) {
	if quote == "" {
		return 0, 0, fmt.Errorf("rỗng")
	}
	i := strings.Index(text, quote)
	if i < 0 {
		return 0, 0, fmt.Errorf("không có trong nội dung điều khoản")
	}
	return i, i + len(quote), nil
}

func correction(problem string) string {
	return "\nLần trả lời trước bị từ chối: " + problem +
		"\nMọi đoạn trích phải sao chép nguyên văn từ nội dung điều khoản ở trên, đúng từng chữ và từng dấu." +
		"\nTrả lời lại, chỉ một đối tượng JSON.\n"
}

func maxCorrections(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

func addUsage(a, b api.Usage) api.Usage {
	a.InputTokens += b.InputTokens
	a.CachedInputTokens += b.CachedInputTokens
	a.CacheWriteTokens += b.CacheWriteTokens
	a.OutputTokens += b.OutputTokens
	a.ReasoningTokens += b.ReasoningTokens
	a.TotalTokens += b.TotalTokens
	return a
}
