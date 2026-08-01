package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/ontology"
)

// Mention is one validated entity mention with its evidence.
type Mention struct {
	Text    string `json:"text"`
	ClassID string `json:"class_id"`
	Quote   string `json:"quote"`
}

// Unresolved is a mention the model could not place in the registry. Storing
// it is correct; guessing a class is not.
type Unresolved struct {
	Text   string `json:"text"`
	Role   string `json:"role,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Job is the stored artifact of one mention extraction: the full attempt
// history, the validated result, and the accounting.
type Job struct {
	ProvisionID     string       `json:"provision_id"`
	DocID           string       `json:"doc_id"`
	OntologyVersion int          `json:"ontology_version"`
	Model           string       `json:"model,omitempty"`
	Attempts        []Attempt    `json:"attempts"`
	Mentions        []Mention    `json:"mentions"`
	Unresolved      []Unresolved `json:"unresolved,omitempty"`
	Usage           api.Usage    `json:"usage"`
	CompletedAt     time.Time    `json:"completed_at"`
}

// Attempt records one raw model response and what was wrong with it, if
// anything. The audit trail is the store.
type Attempt struct {
	Raw   string `json:"raw"`
	Error string `json:"error,omitempty"`
}

// Extractor runs pass 2 under a closed registry.
type Extractor struct {
	Completer      api.Completer
	Model          string
	Registry       *ontology.Registry
	MaxCorrections int // bounded retries on malformed or invalid output
}

type wireMentions struct {
	Mentions   []Mention    `json:"mentions"`
	Unresolved []Unresolved `json:"unresolved_mentions"`
}

// Instructions is the system prompt for pass 2. The class list is inlined so
// the registry travels with every call.
func (e *Extractor) Instructions() string {
	var b strings.Builder
	b.WriteString("Bạn trích xuất các đề cập thực thể pháp lý từ một điều khoản luật Việt Nam.\n")
	b.WriteString("Chỉ được dùng các mã lớp sau, không được bịa mã mới:\n")
	for _, c := range e.Registry.Classes {
		fmt.Fprintf(&b, "- %s: %s\n", c.ID, c.LabelVI)
	}
	b.WriteString("\nTrả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"mentions":[{"text":"...","class_id":"vn-legal:...","quote":"..."}],"unresolved_mentions":[{"text":"...","role":"...","reason":"..."}]}` + "\n")
	b.WriteString("Mỗi quote phải là một đoạn nguyên văn, sao chép đúng từng ký tự từ nội dung điều khoản.\n")
	b.WriteString("Đề cập không khớp lớp nào thì đưa vào unresolved_mentions, không đoán.\n")
	return b.String()
}

// Run extracts mentions for one provision and returns the stored job. A
// malformed or invalid response is retried with the validation error appended,
// at most MaxCorrections times, then the job is stored with its failure.
func (e *Extractor) Run(ctx context.Context, doc *law.Document, provisionID string) (*Job, error) {
	w, err := BuildWindow(doc, provisionID)
	if err != nil {
		return nil, err
	}
	job := &Job{
		ProvisionID:     provisionID,
		DocID:           doc.ID,
		OntologyVersion: e.Registry.Version,
		Model:           e.Model,
	}
	input := w.Prompt()
	for attempt := 0; attempt <= max(0, e.MaxCorrections); attempt++ {
		resp, err := e.Completer.Complete(ctx, api.Request{
			Model:        e.Model,
			Instructions: e.Instructions(),
			Input:        input,
		})
		if err != nil {
			return nil, err
		}
		job.Usage = addUsage(job.Usage, resp.Usage)
		parsed, verr := e.validate(resp.Text, w.Text)
		if verr == nil {
			job.Attempts = append(job.Attempts, Attempt{Raw: resp.Text})
			job.Mentions = parsed.Mentions
			job.Unresolved = parsed.Unresolved
			job.CompletedAt = time.Now().UTC()
			return job, nil
		}
		job.Attempts = append(job.Attempts, Attempt{Raw: resp.Text, Error: verr.Error()})
		input = w.Prompt() + "\nLần trả lời trước bị lỗi: " + verr.Error() + "\nTrả lời lại, chỉ một đối tượng JSON.\n"
	}
	job.CompletedAt = time.Now().UTC()
	return job, fmt.Errorf("extract %s: no valid response after %d attempts: %s",
		provisionID, len(job.Attempts), job.Attempts[len(job.Attempts)-1].Error)
}

// validate parses the model output and enforces the hard invariants: known
// class IDs only, and every quote present byte for byte in the provision text.
func (e *Extractor) validate(raw, provisionText string) (*wireMentions, error) {
	var parsed wireMentions
	if err := json.Unmarshal([]byte(StripFences(raw)), &parsed); err != nil {
		return nil, fmt.Errorf("not a single JSON object: %v", err)
	}
	for _, m := range parsed.Mentions {
		if e.Registry.Class(m.ClassID) == nil {
			return nil, fmt.Errorf("class %q is not in ontology v%d", m.ClassID, e.Registry.Version)
		}
		if m.Quote == "" || !strings.Contains(provisionText, m.Quote) {
			return nil, fmt.Errorf("quote for %q does not appear verbatim in the provision", m.Text)
		}
	}
	return &parsed, nil
}

// StripFences removes a markdown code fence wrapper if the model added one.
func StripFences(s string) string {
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

// JobPath is where a provision's extraction artifact lives.
func JobPath(extractDir, provisionID string) string {
	return filepath.Join(extractDir, law.FileName(provisionID))
}
