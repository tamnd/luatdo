package route

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/luatdo/codex"
)

// The routes file is not a thing anyone should be maintaining by hand.
//
// A free tier catalogue rotates. Slugs appear, slugs are withdrawn, and the
// first anyone hears about a withdrawal is a campaign that stops halfway with
// an error that reads like a network problem. Asking the endpoint what it
// actually serves takes one request and answers both questions: which
// configured routes have drifted, and what is on offer that no route covers.

// ZenURL is the endpoint in front of the free tier models.
const ZenURL = "https://opencode.ai/zen/v1"

// Starter is the route list doctor offers when there is no file yet.
//
// It is a starting point rather than a recommendation. The subscription goes
// first because it is already paid for and a corpus of 93225 documents is not
// something to meter at list price. The free routes follow, unranked by any
// evidence because none has been gathered here yet, and doctor probes them so
// the ranks can be earned rather than assumed.
func Starter() []Route {
	free := func(rank int, name, model, note string) Route {
		return Route{
			Name: name, Wire: WireChat, BaseURL: ZenURL, Model: model,
			APIKeyEnv: "OPENCODE_API_KEY", Rank: rank, Note: note,
		}
	}
	return []Route{
		{
			Name: "codex", Wire: WireCodex, Model: codex.DefaultModel, Effort: "high", Rank: 10,
			Note: "ChatGPT subscription, no rate card so it reports no cost, quota is per plan window",
		},
		free(30, "free-deepseek", "deepseek-v4-flash-free", "strong on structured output, cheap to run wide"),
		free(31, "free-nemotron", "nemotron-3-ultra-free", "slower, use it where the reading is hard"),
		free(32, "free-mimo", "mimo-v2.5-free", "leanest of the free tier"),
		{
			Name: "metered", Wire: WireResponses, BaseURL: "https://api.openai.com/v1",
			Model: "gpt-5", APIKeyEnv: "OPENAI_API_KEY", Rank: 90, Disabled: true,
			Pricing: &Pricing{InputPerM: 1.25, CachedInputPerM: 0.125, OutputPerM: 10.0},
			Note:    "disabled on purpose, a full pass over this corpus at list price is not a casual decision",
		},
	}
}

// Catalogue asks one endpoint which models it serves.
//
// An endpoint that does not answer is not an error. Plenty of compatible
// servers have no models endpoint at all, and a route file is not wrong for
// naming one of them.
func Catalogue(ctx context.Context, r Route, httpClient *http.Client) []string {
	if r.wire() == WireCodex {
		return slices.Clone(codex.Models)
	}
	if strings.TrimSpace(r.BaseURL) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint("/models"), nil)
	if err != nil {
		return nil
	}
	if key := r.Key(); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	// The limit is here because this talks to whatever the file names, and a
	// server answering with a gigabyte of HTML should cost a read that fails
	// rather than the memory.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil
	}
	var envelope struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil
	}
	var out []string
	for _, item := range envelope.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// Catalogues probes every route in turn.
//
// Sequentially, because several of these routes share one account, and firing
// the probes at once is a way to find out that a shared quota is exhausted by
// exhausting it.
func Catalogues(ctx context.Context, routes []Route, httpClient *http.Client) map[string][]string {
	out := map[string][]string{}
	for _, r := range routes {
		if models := Catalogue(ctx, r, httpClient); len(models) > 0 {
			out[r.Name] = models
		}
	}
	return out
}

// Suggest builds a refreshed route list from what the endpoints advertise.
//
// It keeps the rank of every route that still exists, because a rank is
// measured evidence about what answered well and regenerating it from a
// catalogue would throw that away. A route whose model has disappeared is
// disabled with the reason rather than deleted, since a withdrawal can be
// reversed and a missing row just looks like an oversight. A model on offer
// that no route covers is appended disabled, so a person decides whether an
// unproven model gets to touch the graph.
func Suggest(routes []Route, catalogues map[string][]string) []Route {
	out := make([]Route, 0, len(routes))
	covered := map[string]bool{}
	for _, r := range routes {
		covered[r.ModelName()] = true
		catalogue, probed := catalogues[r.Name]
		if probed && !slices.Contains(catalogue, r.ModelName()) {
			r.Disabled = true
			r.Note = fmt.Sprintf("%s no longer lists %s, it serves %d other models",
				r.Name, r.ModelName(), len(catalogue))
		}
		out = append(out, r)
	}
	for _, r := range routes {
		catalogue := catalogues[r.Name]
		if len(catalogue) == 0 || r.wire() == WireCodex {
			continue
		}
		for _, model := range catalogue {
			if covered[model] || !strings.HasSuffix(model, "-free") {
				continue
			}
			covered[model] = true
			out = append(out, Route{
				Name:      suggestedName(model, out),
				Wire:      r.wire(),
				BaseURL:   r.BaseURL,
				Model:     model,
				APIKeyEnv: r.APIKeyEnv,
				Rank:      nextFreeRank(out),
				Disabled:  true,
				Note:      "new in the catalogue, never run here, enable it once a pass has proven it",
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
	return out
}

// freeBandStart is where the free routes are ranked. A new one lands at the end
// of the band so it never displaces a route with evidence behind it.
const freeBandStart = 30

func nextFreeRank(routes []Route) int {
	rank := freeBandStart
	for _, r := range routes {
		if r.Rank >= rank && r.Rank < freeBandStart+50 {
			rank = r.Rank + 1
		}
	}
	return rank
}

func suggestedName(model string, routes []Route) string {
	base := "free-" + strings.TrimSuffix(model, "-free")
	name := base
	for suffix := 2; ; suffix++ {
		if !slices.ContainsFunc(routes, func(r Route) bool { return r.Name == name }) {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, suffix)
	}
}

// Drift names the routes whose configured model the endpoint no longer lists.
func Drift(routes []Route, catalogues map[string][]string) []string {
	var out []string
	for _, r := range routes {
		catalogue, probed := catalogues[r.Name]
		if !probed || slices.Contains(catalogue, r.ModelName()) {
			continue
		}
		out = append(out, fmt.Sprintf("%s asks for %s, which %s no longer lists",
			r.Name, r.ModelName(), r.BaseURL))
	}
	return out
}

// Write saves a routes file for editing. It refuses to overwrite one that is
// already there, because a route file holds ranks someone measured and this
// command has no way to tell whether they wanted them back.
func Write(path string, routes []Route) error {
	raw, err := json.MarshalIndent(File{Routes: routes}, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return err
	}
	return file.Close()
}
