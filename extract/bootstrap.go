package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/ontology"
)

// Sample picks up to n provisions spread evenly across the corpus, favoring
// clauses because they are the extraction unit. Deterministic: the same corpus
// yields the same sample.
func Sample(docs []*law.Document, n int) []struct {
	Doc         *law.Document
	ProvisionID string
} {
	type item = struct {
		Doc         *law.Document
		ProvisionID string
	}
	var pool []item
	for _, d := range docs {
		if d.Status != "parsed" {
			continue
		}
		for i := range d.Provisions {
			p := &d.Provisions[i]
			if p.Kind == "clause" && p.Text != "" {
				pool = append(pool, item{Doc: d, ProvisionID: p.ID})
			}
		}
	}
	if n <= 0 || n >= len(pool) {
		return pool
	}
	step := len(pool) / n
	out := make([]item, 0, n)
	for i := 0; i < len(pool) && len(out) < n; i += step {
		out = append(out, pool[i])
	}
	return out
}

const bootstrapInstructions = `Bạn đọc một điều khoản luật Việt Nam và đề xuất các lớp thực thể pháp lý xuất hiện trong đó.
Đây là bước khảo sát mở để xây dựng ontology, nên hãy đề xuất nhãn tổng quát, không gắn với văn bản cụ thể.
Trả về đúng một đối tượng JSON theo dạng:
{"candidates":[{"kind":"class","label":"...","quote":"..."}]}
Mỗi quote phải là một đoạn nguyên văn từ điều khoản.`

// Bootstrap runs stage-one open discovery over one sampled provision and
// returns registry candidates. Failures come back as errors; a partial corpus
// sweep is fine because candidates only accumulate.
func Bootstrap(ctx context.Context, c api.Completer, model string, doc *law.Document, provisionID string) ([]ontology.Candidate, api.Usage, error) {
	w, err := BuildWindow(doc, provisionID)
	if err != nil {
		return nil, api.Usage{}, err
	}
	resp, err := c.Complete(ctx, api.Request{Model: model, Instructions: bootstrapInstructions, Input: w.Prompt()})
	if err != nil {
		return nil, api.Usage{}, err
	}
	var parsed struct {
		Candidates []struct {
			Kind  string `json:"kind"`
			Label string `json:"label"`
			Quote string `json:"quote"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(StripFences(resp.Text)), &parsed); err != nil {
		return nil, resp.Usage, fmt.Errorf("bootstrap %s: not a single JSON object: %v", provisionID, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var out []ontology.Candidate
	for _, c := range parsed.Candidates {
		label := strings.TrimSpace(c.Label)
		if label == "" {
			continue
		}
		kind := c.Kind
		if kind != "predicate" {
			kind = "class"
		}
		out = append(out, ontology.Candidate{
			Kind:      kind,
			Label:     label,
			Provision: provisionID,
			Quote:     c.Quote,
			Source:    "bootstrap",
			Status:    "proposed",
			At:        now,
		})
	}
	return out, resp.Usage, nil
}
