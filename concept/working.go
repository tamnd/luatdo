package concept

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/luatdo/api"
)

// A working definition is what the corpus treats a concept as meaning, written
// by us because nobody wrote it down. It is the single most useful thing in
// this layer for a person reading the graph and the single most dangerous thing
// in it for correctness, so it is fenced on every side:
//
//   - It lives in its own type and its own field. A statutory definition and
//     our reading of usage are different kinds of fact and the graph never lets
//     one stand in for the other.
//   - It names the provisions it was derived from, and every claim in it must
//     be supported by a quote from one of them, checked in code.
//   - It records the text hashes of those provisions, so a rebuild can tell
//     that the sources changed and the definition needs regenerating.
//   - It is never evidence for a norm. That is enforced where norms are built,
//     and stated here so the reason is next to the thing.
type WorkingDefinition struct {
	TermUseID string `json:"term_use_id"`
	LabelVI   string `json:"label_vi"`
	Text      string `json:"text"`
	// Claims are the definition broken into the statements it makes, each with
	// the provision and the verbatim quote that supports it. A claim with no
	// support is rejected, which is what stops this from being a summary.
	Claims    []Claim   `json:"claims"`
	Sources   []Source  `json:"sources"`
	Model     string    `json:"model,omitempty"`
	WrittenAt time.Time `json:"written_at"`
}

// Claim is one statement of a working definition and its evidence.
type Claim struct {
	Text        string `json:"text"`
	ProvisionID string `json:"provision_id"`
	Quote       string `json:"quote"`
}

// Source is a provision a working definition was derived from, with the hash of
// the text as it stood. When the hash changes the definition is stale, and
// stale is a state the store can see rather than a thing somebody remembers.
type Source struct {
	ProvisionID string `json:"provision_id"`
	TextHash    string `json:"text_hash"`
}

// Stale reports whether any source provision has changed since the definition
// was written. current maps a provision identifier to its text hash now.
func (w *WorkingDefinition) Stale(current map[string]string) bool {
	for _, s := range w.Sources {
		if h, ok := current[s.ProvisionID]; ok && h != s.TextHash {
			return true
		}
	}
	return false
}

// Validate checks a working definition against the provisions it claims to have
// read. texts maps a provision identifier to its text.
func (w *WorkingDefinition) Validate(texts map[string]string) error {
	if strings.TrimSpace(w.Text) == "" {
		return fmt.Errorf("no text")
	}
	if len(w.Claims) == 0 {
		return fmt.Errorf("no claims, so nothing in it can be checked")
	}
	if len(w.Sources) == 0 {
		return fmt.Errorf("no sources")
	}
	sources := map[string]bool{}
	for _, s := range w.Sources {
		sources[s.ProvisionID] = true
	}
	for i, c := range w.Claims {
		if strings.TrimSpace(c.Text) == "" {
			return fmt.Errorf("claim %d has no text", i+1)
		}
		if !sources[c.ProvisionID] {
			return fmt.Errorf("claim %d cites %s, which is not one of the sources", i+1, c.ProvisionID)
		}
		text, ok := texts[c.ProvisionID]
		if !ok {
			return fmt.Errorf("claim %d cites %s, which was not among the provisions read", i+1, c.ProvisionID)
		}
		if c.Quote == "" {
			return fmt.Errorf("claim %d has no supporting quote", i+1)
		}
		if !strings.Contains(text, c.Quote) {
			return fmt.Errorf("claim %d quotes %q, which is not in %s", i+1, c.Quote, c.ProvisionID)
		}
	}
	return nil
}

// Definer runs pass C3. It is given a promoted concept and the provisions that
// use it, and asked what the corpus treats the concept as meaning.
type Definer struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
	// MaxProvisions is how many provisions go into one prompt. It is small on
	// purpose: the model is being asked what the corpus treats a concept as
	// meaning, and forty provisions of context makes that a summarisation task
	// where the quotes stop being checkable by anyone reading the output.
	MaxProvisions int
}

// DefaultMaxProvisions is the number of provisions a working definition is
// written from unless the caller says otherwise.
const DefaultMaxProvisions = 8

type wireClaim struct {
	Text        string `json:"text"`
	ProvisionID string `json:"provision_id"`
	Quote       string `json:"quote"`
}

type wireWorking struct {
	Text   string      `json:"working_definition"`
	Claims []wireClaim `json:"claims"`
}

// Instructions is the pass C3 system prompt.
func (d *Definer) Instructions() string {
	var b strings.Builder
	b.WriteString("Bạn đọc một số điều khoản cùng sử dụng một khái niệm mà không văn bản nào định nghĩa khái niệm đó.\n")
	b.WriteString("Nhiệm vụ: nói rõ các điều khoản này coi khái niệm đó là gì.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. Đây không phải định nghĩa của pháp luật. Đây là cách hiểu rút ra từ cách dùng, và phải viết như vậy.\n")
	b.WriteString("2. Mỗi ý trong working_definition phải có một claim tương ứng, kèm mã điều khoản và đoạn trích nguyên văn chứng minh ý đó.\n")
	b.WriteString("3. Đoạn trích phải sao chép nguyên văn từ đúng điều khoản được nêu trong provision_id.\n")
	b.WriteString("4. Không dùng kiến thức bên ngoài các điều khoản được cung cấp. Không nhớ lại định nghĩa từ văn bản khác.\n")
	b.WriteString("5. Nếu các điều khoản không đủ để nói khái niệm là gì thì trả về working_definition rỗng và claims rỗng. Đây là câu trả lời hợp lệ.\n\n")
	b.WriteString("Trả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"working_definition":"...","claims":[{"text":"...","provision_id":"...","quote":"..."}]}`)
	b.WriteString("\n")
	return b.String()
}

// WorkingPrompt renders the concept and its provisions.
func WorkingPrompt(label string, provisions []Source, texts map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Khái niệm: %s\n\n", label)
	b.WriteString("Các điều khoản sử dụng khái niệm này:\n")
	for _, s := range provisions {
		fmt.Fprintf(&b, "\n[%s]\n%s\n", s.ProvisionID, texts[s.ProvisionID])
	}
	return b.String()
}

// Define writes one working definition. A concept the provisions do not settle
// comes back as nil with no error, which is the right answer often enough that
// it is not treated as a failure.
func (d *Definer) Define(ctx context.Context, t *TermUse, agg *Aggregation, texts map[string]string, hashes map[string]string) (*WorkingDefinition, api.Usage, error) {
	if t.Origin != OriginUndefinedUsage {
		// The fence, enforced at the only place that can create one of these.
		// A concept with a statutory definition does not get a second definition
		// written by us sitting next to it.
		return nil, api.Usage{}, fmt.Errorf("%s has origin %s, and a working definition belongs only to a concept nobody defined", t.ID, t.Origin)
	}

	limit := d.MaxProvisions
	if limit <= 0 {
		limit = DefaultMaxProvisions
	}
	var sources []Source
	for _, id := range agg.Provisions {
		if len(sources) >= limit {
			break
		}
		if _, ok := texts[id]; !ok {
			continue
		}
		sources = append(sources, Source{ProvisionID: id, TextHash: hashes[id]})
	}
	if len(sources) == 0 {
		return nil, api.Usage{}, nil
	}

	var usage api.Usage
	input := WorkingPrompt(t.LabelVI, sources, texts)
	for attempt := 0; attempt <= maxCorrections(d.MaxCorrections); attempt++ {
		resp, err := d.Completer.Complete(ctx, api.Request{
			Model:        d.Model,
			Instructions: d.Instructions(),
			Input:        input,
		})
		if err != nil {
			return nil, usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var wire wireWorking
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			input = WorkingPrompt(t.LabelVI, sources, texts) + correction("câu trả lời không phải một đối tượng JSON hợp lệ: "+err.Error())
			continue
		}
		if strings.TrimSpace(wire.Text) == "" && len(wire.Claims) == 0 {
			return nil, usage, nil
		}
		w := &WorkingDefinition{
			TermUseID: t.ID, LabelVI: t.LabelVI, Text: strings.TrimSpace(wire.Text),
			Sources: sources, Model: d.Model, WrittenAt: time.Now().UTC(),
		}
		for _, c := range wire.Claims {
			w.Claims = append(w.Claims, Claim{
				Text: strings.TrimSpace(c.Text), ProvisionID: c.ProvisionID, Quote: c.Quote,
			})
		}
		if err := w.Validate(texts); err != nil {
			input = WorkingPrompt(t.LabelVI, sources, texts) + correction(err.Error())
			continue
		}
		return w, usage, nil
	}
	return nil, usage, nil
}

// CheckWorking returns every working definition that does not belong to a term
// use it is allowed to belong to. It runs in the build alongside the layer
// invariants, so the fence is a build failure rather than a convention.
func CheckWorking(defs []WorkingDefinition, terms []TermUse) []string {
	byID := map[string]*TermUse{}
	for i := range terms {
		byID[terms[i].ID] = &terms[i]
	}
	var out []string
	seen := map[string]bool{}
	for i := range defs {
		w := &defs[i]
		t := byID[w.TermUseID]
		switch {
		case t == nil:
			out = append(out, fmt.Sprintf("working definition names term use %s, which does not exist", w.TermUseID))
		case t.Origin != OriginUndefinedUsage:
			out = append(out, fmt.Sprintf("working definition on %s, whose origin is %s and which therefore has a definition of its own", w.TermUseID, t.Origin))
		case t.DefinitionVI != "":
			out = append(out, fmt.Sprintf("term use %s carries both a statutory definition and a working definition", w.TermUseID))
		}
		if seen[w.TermUseID] {
			out = append(out, fmt.Sprintf("term use %s has two working definitions", w.TermUseID))
		}
		seen[w.TermUseID] = true
		if len(w.Claims) == 0 {
			out = append(out, fmt.Sprintf("working definition on %s makes no checkable claim", w.TermUseID))
		}
	}
	sort.Strings(out)
	return out
}
