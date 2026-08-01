// Package api is the model transport, ported from taocp-solver.
//
// Callers only ever see the Completer interface; HTTP, retries, and token
// accounting live here. Tests plug in scripted Completers and never touch the
// network.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Completer interface {
	Complete(ctx context.Context, request Request) (Response, error)
}

type Request struct {
	Model        string
	Instructions string
	Input        string
	Effort       string
	Metadata     map[string]string
}

type Response struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	// Route names the endpoint that served this call, when the caller was
	// choosing between several. A solution assembled from two routes has to
	// be able to say which one answered each request.
	Route string `json:"route,omitempty"`
	// PricingModel overrides Model when applying a rate card, for a route
	// whose served model has no published price of its own.
	PricingModel string `json:"pricing_model,omitempty"`
	Text         string `json:"text"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	Usage        Usage  `json:"usage"`
}

// Usage is the billable token accounting returned by the provider. InputTokens
// includes cached reads and cache writes. ReasoningTokens is a subset of
// OutputTokens and must not be added again when calculating cost.
type Usage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	CacheWriteTokens  int `json:"cache_write_tokens"`
	OutputTokens      int `json:"output_tokens"`
	ReasoningTokens   int `json:"reasoning_tokens"`
	TotalTokens       int `json:"total_tokens"`
}

// Normalized fills totals omitted by compatible proxies and prevents malformed
// cache detail from producing negative uncached token counts.
func (u Usage) Normalized() Usage {
	u.InputTokens = max(0, u.InputTokens)
	u.OutputTokens = max(0, u.OutputTokens)
	u.CachedInputTokens = min(max(0, u.CachedInputTokens), u.InputTokens)
	u.CacheWriteTokens = min(max(0, u.CacheWriteTokens), u.InputTokens-u.CachedInputTokens)
	u.ReasoningTokens = min(max(0, u.ReasoningTokens), u.OutputTokens)
	if u.TotalTokens <= 0 {
		u.TotalTokens = u.InputTokens + u.OutputTokens
	}
	return u
}

func (u Usage) UncachedInputTokens() int {
	u = u.Normalized()
	return u.InputTokens - u.CachedInputTokens - u.CacheWriteTokens
}

type Client struct {
	URL             string
	APIKey          string
	HTTPClient      *http.Client
	MaxRetries      int
	MaxRetryDelay   time.Duration
	MaxOutputTokens int
	UserAgent       string
	Sleep           func(context.Context, time.Duration) error
}

type wireRequest struct {
	Model           string            `json:"model"`
	Instructions    string            `json:"instructions,omitempty"`
	Input           string            `json:"input"`
	Reasoning       *reasoning        `json:"reasoning,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	MaxOutputTokens int               `json:"max_output_tokens,omitempty"`
	Store           bool              `json:"store"`
}

type reasoning struct {
	Effort string `json:"effort"`
}

type wireResponse struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	OutputText string `json:"output_text"`
	Output     []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens       int `json:"input_tokens"`
		CachedInputTokens int `json:"cached_input_tokens"`
		CacheWriteTokens  int `json:"cache_write_tokens"`
		OutputTokens      int `json:"output_tokens"`
		ReasoningTokens   int `json:"reasoning_tokens"`
		TotalTokens       int `json:"total_tokens"`
		InputDetails      struct {
			CachedTokens     int `json:"cached_tokens"`
			CacheWriteTokens int `json:"cache_write_tokens"`
		} `json:"input_tokens_details"`
		OutputDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (c *Client) Complete(ctx context.Context, request Request) (Response, error) {
	if strings.TrimSpace(c.URL) == "" {
		return Response{}, errors.New("responses API URL is empty")
	}
	if strings.TrimSpace(request.Model) == "" {
		return Response{}, errors.New("model is empty")
	}
	if strings.TrimSpace(request.Input) == "" {
		return Response{}, errors.New("input is empty")
	}
	body := wireRequest{
		Model:           request.Model,
		Instructions:    request.Instructions,
		Input:           request.Input,
		Metadata:        request.Metadata,
		MaxOutputTokens: c.MaxOutputTokens,
		Store:           false,
	}
	if request.Effort != "" {
		body.Reasoning = &reasoning{Effort: request.Effort}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("encode request: %w", err)
	}

	var last error
	for attempt := 0; attempt <= max(0, c.MaxRetries); attempt++ {
		response, retryAfter, retry, err := c.do(ctx, payload)
		if err == nil {
			return response, nil
		}
		last = err
		if !retry || attempt == max(0, c.MaxRetries) {
			break
		}
		if c.MaxRetryDelay > 0 && retryAfter > c.MaxRetryDelay {
			break
		}
		if retryAfter <= 0 {
			retryAfter = backoff(attempt)
		}
		if err := c.sleep(ctx, retryAfter); err != nil {
			return Response{}, err
		}
	}
	return Response{}, last
}

func (c *Client) do(ctx context.Context, payload []byte) (Response, time.Duration, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(payload))
	if err != nil {
		return Response{}, 0, false, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, 0, false, ctx.Err()
		}
		return Response{}, 0, true, fmt.Errorf("call responses API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return Response{}, 0, true, fmt.Errorf("read responses API: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := responseError(raw)
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return Response{}, retryAfter, retry,
			fmt.Errorf("responses API returned %s: %s%s", resp.Status, message, retryAfterSuffix(retryAfter))
	}

	var decoded wireResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Response{}, 0, false, fmt.Errorf("decode responses API: %w", err)
	}
	if decoded.Error != nil {
		return Response{}, 0, false, fmt.Errorf("responses API error: %s", decoded.Error.Message)
	}
	text := strings.TrimSpace(decoded.OutputText)
	if text == "" {
		var parts []string
		for _, item := range decoded.Output {
			for _, content := range item.Content {
				if content.Type == "output_text" || content.Type == "text" || content.Type == "" {
					if value := strings.TrimSpace(content.Text); value != "" {
						parts = append(parts, value)
					}
				}
			}
		}
		text = strings.Join(parts, "\n\n")
	}
	if text == "" {
		return Response{}, 0, true, errors.New("responses API returned no text")
	}
	usage := Usage{
		InputTokens:       decoded.Usage.InputTokens,
		CachedInputTokens: decoded.Usage.InputDetails.CachedTokens,
		CacheWriteTokens:  decoded.Usage.InputDetails.CacheWriteTokens,
		OutputTokens:      decoded.Usage.OutputTokens,
		ReasoningTokens:   decoded.Usage.OutputDetails.ReasoningTokens,
		TotalTokens:       decoded.Usage.TotalTokens,
	}
	if usage.CachedInputTokens == 0 {
		usage.CachedInputTokens = decoded.Usage.CachedInputTokens
	}
	if usage.CacheWriteTokens == 0 {
		usage.CacheWriteTokens = decoded.Usage.CacheWriteTokens
	}
	if usage.ReasoningTokens == 0 {
		usage.ReasoningTokens = decoded.Usage.ReasoningTokens
	}
	usage = usage.Normalized()
	return Response{
		ID:           decoded.ID,
		Model:        decoded.Model,
		Text:         text,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		Usage:        usage,
	}, 0, false, nil
}

func (c *Client) sleep(ctx context.Context, duration time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, duration)
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func responseError(raw []byte) string {
	var decoded wireResponse
	if json.Unmarshal(raw, &decoded) == nil && decoded.Error != nil {
		return strings.TrimSpace(decoded.Error.Message)
	}
	value := strings.TrimSpace(string(raw))
	if len(value) > 1000 {
		value = value[:1000]
	}
	return value
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		return max(0, time.Until(when))
	}
	return 0
}

func retryAfterSuffix(delay time.Duration) string {
	if delay <= 0 {
		return ""
	}
	return fmt.Sprintf(" (retry after %s)", delay)
}

func backoff(attempt int) time.Duration {
	base := min(30*time.Second, time.Second<<min(attempt, 5))
	jitter := time.Duration(rand.IntN(500)) * time.Millisecond
	return base + jitter
}
