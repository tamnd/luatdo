package concept

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/law"
)

// Discoverer runs pass C: a model reads an ordinary provision and names the
// legally operative concepts it uses.
//
// Pass B had an anchor. A grammar found the clause, so the reading started from
// a place the code was sure about. This pass has no anchor at all and cannot
// have one, because nothing in the surface form of a Vietnamese sentence marks
// thoi gio lam viec as a concept the corpus operates on and ngay hom do as
// ordinary language. Only reading tells them apart.
//
// That is the whole reason this is a separate pass with its own metrics. The
// precision profile is different, the failure mode is different, and a pass
// that mixed the two would let the anchored half carry the unanchored half's
// numbers.
type Discoverer struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
}

// Candidate is one concept a provision was read as using. It is not a node. It
// becomes a node only after aggregation says the corpus operates on it, which
// is the whole point of keeping the two steps apart.
type Candidate struct {
	LabelVI     string  `json:"label_vi"`
	Kind        string  `json:"kind"`
	Quote       string  `json:"quote"`
	CharStart   int     `json:"char_start"`
	CharEnd     int     `json:"char_end"`
	Shows       string  `json:"what_the_provision_shows"`
	DefinedHere bool    `json:"defined_here"`
	Confidence  float64 `json:"confidence"`
	// ProvisionID and DocID are filled in by the caller from the provision that
	// was read. The model never supplies them, the same way it never supplies an
	// identifier in pass B.
	ProvisionID string `json:"provision_id"`
	DocID       string `json:"doc_id"`
	Model       string `json:"model,omitempty"`
}

// Key is the form a candidate is grouped under. Two sightings of one concept
// count together only if they fold to the same key, and the fold is law.Slug so
// that discovery, definitions and the graph all agree on what one phrase is.
func (c *Candidate) Key() string { return law.Slug(c.LabelVI) }

// Validate checks a candidate against the provision it was read out of. A quote
// that is not in the text is the failure mode this pass is most exposed to,
// since there is no anchor to contradict a plausible invention.
func (c *Candidate) Validate(text string) error {
	if strings.TrimSpace(c.LabelVI) == "" {
		return fmt.Errorf("no label")
	}
	if c.Key() == "" {
		return fmt.Errorf("label %q folds to nothing", c.LabelVI)
	}
	if !ValidKind(c.Kind) {
		return fmt.Errorf("kind %q is not one of %s", c.Kind, strings.Join(Kinds, ", "))
	}
	if err := checkQuote(text, c.Quote, c.CharStart, c.CharEnd); err != nil {
		return fmt.Errorf("quote: %w", err)
	}
	// The label has to be in the provision as written. The pass asks for the
	// label as the provision writes it, so a label the provision does not
	// contain is the model having normalised, translated or invented one, and
	// all three break the mention linking that comes later.
	if !strings.Contains(text, c.LabelVI) {
		return fmt.Errorf("label %q is not in the provision as written", c.LabelVI)
	}
	if strings.TrimSpace(c.Shows) == "" {
		return fmt.Errorf("term %q has no statement of what the provision shows about it", c.LabelVI)
	}
	if c.Confidence < 0 || c.Confidence > 1 {
		return fmt.Errorf("confidence %v is outside 0 to 1", c.Confidence)
	}
	return nil
}

// Sighting is the stored artifact of reading one provision for concepts, kept
// whether or not anything was found. A provision that legitimately uses no
// operative concept is a fact worth storing: without it, the denominator of
// every discovery rate is the provisions that happened to succeed.
type Sighting struct {
	ProvisionID string      `json:"provision_id"`
	DocID       string      `json:"doc_id"`
	TextHash    string      `json:"text_hash"`
	Candidates  []Candidate `json:"candidates"`
	Attempts    []Attempt   `json:"attempts,omitempty"`
	Usage       api.Usage   `json:"usage"`
	Model       string      `json:"model,omitempty"`
	ReadAt      time.Time   `json:"read_at"`
	Err         string      `json:"error,omitempty"`
}

type wireCandidate struct {
	LabelVI     string  `json:"label_vi"`
	Kind        string  `json:"kind"`
	Quote       string  `json:"quote"`
	CharStart   int     `json:"char_start"`
	CharEnd     int     `json:"char_end"`
	Shows       string  `json:"what_the_provision_shows"`
	DefinedHere bool    `json:"defined_here"`
	Confidence  float64 `json:"confidence"`
}

type wireDiscovery struct {
	Concepts []wireCandidate `json:"concepts"`
}

// Instructions is the pass C system prompt. Most of it is about what not to
// return, because the failure mode here is not missing a concept, it is
// returning every noun phrase in the provision and calling the list a
// discovery.
func (d *Discoverer) Instructions() string {
	var b strings.Builder
	b.WriteString("Bạn đọc một điều khoản của văn bản quy phạm pháp luật Việt Nam.\n")
	b.WriteString("Nhiệm vụ: nêu các khái niệm pháp lý mà điều khoản này thực sự vận hành trên đó.\n\n")
	b.WriteString("Khái niệm pháp lý ở đây là: chủ thể mà quy định hướng tới, hành vi được điều chỉnh, giấy tờ hoặc tài sản mà quy định đòi hỏi hoặc tạo ra, trạng thái pháp lý mà quy định xác lập, ngưỡng hoặc mức mà quy định đặt ra.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. label_vi phải xuất hiện nguyên văn trong điều khoản. Không chuẩn hóa, không dịch, không rút gọn, không đổi số ít số nhiều.\n")
	b.WriteString("2. quote là đoạn trích nguyên văn chứa khái niệm đó, char_start và char_end là vị trí byte thật của quote, tính từ 0.\n")
	b.WriteString("3. what_the_provision_shows nói điều khoản này cho biết gì về khái niệm, viết ngắn gọn, chỉ dựa vào nội dung điều khoản.\n")
	b.WriteString("4. Bỏ qua từ ngữ thông thường. Ngày hôm đó, trường hợp này, nội dung sau đây không phải khái niệm pháp lý.\n")
	b.WriteString("5. Bỏ qua tên văn bản và số hiệu văn bản được dẫn chiếu. Luật số 45/2019/QH14 là một trích dẫn, không phải khái niệm.\n")
	b.WriteString("6. defined_here là true chỉ khi chính điều khoản này định nghĩa khái niệm đó.\n")
	b.WriteString("7. Nếu điều khoản không vận hành trên khái niệm pháp lý nào thì trả về {\"concepts\":[]}. Danh sách rỗng là câu trả lời hợp lệ. Không kể thêm cho đủ.\n")
	b.WriteString("8. Không suy đoán ngoài nội dung điều khoản.\n\n")
	b.WriteString("kind phải là một trong các giá trị sau:\n")
	for _, k := range Kinds {
		fmt.Fprintf(&b, "- %s: %s\n", k, KindLabels[k])
	}
	b.WriteString("\nTrả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"concepts":[{"label_vi":"...","kind":"action","quote":"...","char_start":0,"char_end":0,"what_the_provision_shows":"...","defined_here":false,"confidence":0.9}]}`)
	b.WriteString("\n")
	return b.String()
}

// DiscoveryPrompt renders one provision as the model input.
func DiscoveryPrompt(doc *law.Document, p *law.Provision) string {
	var b strings.Builder
	if doc != nil {
		fmt.Fprintf(&b, "Văn bản: %s\n", doc.Title)
		fmt.Fprintf(&b, "Loại văn bản: %s\n", doc.DocType)
	}
	fmt.Fprintf(&b, "Mã điều khoản: %s\n", p.ID)
	if p.Heading != "" {
		fmt.Fprintf(&b, "Tiêu đề: %s\n", p.Heading)
	}
	fmt.Fprintf(&b, "\nNội dung:\n%s\n", p.Text)
	return b.String()
}

// Discover reads one provision. Like pass B it returns the artifact whether or
// not the reading worked, and a reading the model could not get right inside
// the correction budget is recorded rather than retried forever.
func (d *Discoverer) Discover(ctx context.Context, doc *law.Document, p *law.Provision) (*Sighting, error) {
	s := &Sighting{ProvisionID: p.ID, TextHash: p.TextHash, Model: d.Model}
	if doc != nil {
		s.DocID = doc.ID
	}
	input := DiscoveryPrompt(doc, p)

	for attempt := 0; attempt <= maxCorrections(d.MaxCorrections); attempt++ {
		resp, err := d.Completer.Complete(ctx, api.Request{
			Model:        d.Model,
			Instructions: d.Instructions(),
			Input:        input,
		})
		if err != nil {
			s.Err = err.Error()
			s.ReadAt = time.Now().UTC()
			return s, err
		}
		s.Usage = addUsage(s.Usage, resp.Usage)

		candidates, problem := d.parse(s, p, resp.Text)
		if problem == "" {
			s.Attempts = append(s.Attempts, Attempt{Raw: resp.Text})
			s.Candidates = candidates
			s.ReadAt = time.Now().UTC()
			return s, nil
		}
		s.Attempts = append(s.Attempts, Attempt{Raw: resp.Text, Error: problem})
		input = DiscoveryPrompt(doc, p) + correction(problem)
	}

	s.Err = "no valid discovery within the correction budget"
	s.ReadAt = time.Now().UTC()
	return s, nil
}

func (d *Discoverer) parse(s *Sighting, p *law.Provision, raw string) ([]Candidate, string) {
	var wire wireDiscovery
	if err := json.Unmarshal([]byte(stripFences(raw)), &wire); err != nil {
		return nil, "câu trả lời không phải một đối tượng JSON hợp lệ: " + err.Error()
	}
	out := make([]Candidate, 0, len(wire.Concepts))
	seen := map[string]bool{}
	for _, w := range wire.Concepts {
		c := Candidate{
			LabelVI:     strings.TrimSpace(w.LabelVI),
			Kind:        w.Kind,
			Quote:       w.Quote,
			CharStart:   w.CharStart,
			CharEnd:     w.CharEnd,
			Shows:       strings.TrimSpace(w.Shows),
			DefinedHere: w.DefinedHere,
			Confidence:  w.Confidence,
			ProvisionID: p.ID,
			DocID:       s.DocID,
			Model:       d.Model,
		}
		if err := c.Validate(p.Text); err != nil {
			return nil, err.Error()
		}
		if seen[c.Key()] {
			// One provision naming one concept twice is one sighting. Counting it
			// twice would let a single provision push a candidate over a
			// promotion threshold on its own.
			continue
		}
		seen[c.Key()] = true
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CharStart < out[j].CharStart })
	return out, ""
}
