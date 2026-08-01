package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChatClient speaks the OpenAI compatible chat completions wire.
//
// Almost everything that is not OpenAI speaks this and only this. The zen
// endpoint in front of DeepSeek, Nemotron and the rest of the free tier, a
// local Ollama on the gaming pc, and any proxy someone puts in the way all
// answer here, while the Responses format in client.go is served by a much
// shorter list. A corpus this size needs whatever capacity it can reach, so the
// transport that reaches the most of it is the one worth having.
//
// It streams, because a provision extraction can run for minutes and a
// connection that sends nothing for minutes is a connection something in the
// middle will close.
type ChatClient struct {
	URL             string
	APIKey          string
	HTTPClient      *http.Client
	MaxRetries      int
	MaxRetryDelay   time.Duration
	MaxOutputTokens int
	UserAgent       string
	Sleep           func(context.Context, time.Duration) error
}

type chatRequest struct {
	Model               string        `json:"model"`
	Messages            []chatMessage `json:"messages"`
	Stream              bool          `json:"stream"`
	StreamOptions       streamOptions `json:"stream_options"`
	PromptCacheKey      string        `json:"prompt_cache_key,omitempty"`
	MaxCompletionTokens int           `json:"max_completion_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		CacheReadTokens  int `json:"cache_read_input_tokens"`
		CacheWriteTokens int `json:"cache_creation_input_tokens"`
		PromptDetails    struct {
			CachedTokens     int `json:"cached_tokens"`
			CacheWriteTokens int `json:"cache_write_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends one turn and returns the whole answer.
func (c *ChatClient) Complete(ctx context.Context, request Request) (Response, error) {
	if strings.TrimSpace(c.URL) == "" {
		return Response{}, errors.New("chat completions URL is empty")
	}
	if strings.TrimSpace(request.Model) == "" {
		return Response{}, errors.New("model is empty")
	}
	if strings.TrimSpace(request.Input) == "" {
		return Response{}, errors.New("input is empty")
	}
	body := chatRequest{
		Model: request.Model,
		Messages: []chatMessage{
			{Role: "system", Content: request.Instructions},
			{Role: "user", Content: request.Input},
		},
		Stream:              true,
		StreamOptions:       streamOptions{IncludeUsage: true},
		PromptCacheKey:      cacheKey(request.Instructions),
		MaxCompletionTokens: c.MaxOutputTokens,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("encode chat request: %w", err)
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

func (c *ChatClient) do(ctx context.Context, payload []byte) (Response, time.Duration, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(payload))
	if err != nil {
		return Response{}, 0, false, fmt.Errorf("create chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
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
		return Response{}, 0, true, fmt.Errorf("call chat completions: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return Response{}, retryAfter, retry,
			fmt.Errorf("chat completions returned %s: %s%s", resp.Status, responseError(raw), retryAfterSuffix(retryAfter))
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		response, err := parseChatStream(resp.Body)
		if err != nil {
			return Response{}, 0, true, err
		}
		return response, 0, false, nil
	}
	// Some proxies ignore the stream flag and answer with one JSON object.
	// Refusing that would be refusing a perfectly good answer.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return Response{}, 0, true, fmt.Errorf("read chat response: %w", err)
	}
	response, err := parseChatJSON(raw)
	if err != nil {
		return Response{}, 0, false, err
	}
	return response, 0, false, nil
}

func parseChatStream(reader io.Reader) (Response, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var output strings.Builder
	var response Response
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		var chunk chatChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return Response{}, fmt.Errorf("decode chat stream: %w", err)
		}
		if chunk.Error != nil {
			return Response{}, fmt.Errorf("chat stream error: %s", chunk.Error.Message)
		}
		if response.ID == "" {
			response.ID = chunk.ID
		}
		if chunk.Model != "" {
			response.Model = chunk.Model
		}
		for _, choice := range chunk.Choices {
			output.WriteString(choice.Delta.Content)
			output.WriteString(choice.Message.Content)
		}
		if chunk.Usage != nil {
			setChatUsage(&response, chunk)
		}
	}
	if err := scanner.Err(); err != nil {
		return Response{}, fmt.Errorf("read chat stream: %w", err)
	}
	response.Text = strings.TrimSpace(output.String())
	if response.Text == "" {
		return Response{}, errors.New("chat completions returned no text")
	}
	return response, nil
}

func parseChatJSON(raw []byte) (Response, error) {
	var chunk chatChunk
	if err := json.Unmarshal(raw, &chunk); err != nil {
		return Response{}, fmt.Errorf("decode chat response: %w", err)
	}
	if chunk.Error != nil {
		return Response{}, fmt.Errorf("chat completions error: %s", chunk.Error.Message)
	}
	response := Response{ID: chunk.ID, Model: chunk.Model}
	var text strings.Builder
	for _, choice := range chunk.Choices {
		text.WriteString(choice.Message.Content)
		text.WriteString(choice.Delta.Content)
	}
	if chunk.Usage != nil {
		setChatUsage(&response, chunk)
	}
	response.Text = strings.TrimSpace(text.String())
	if response.Text == "" {
		return Response{}, errors.New("chat completions returned no text")
	}
	return response, nil
}

func setChatUsage(response *Response, chunk chatChunk) {
	if chunk.Usage == nil {
		return
	}
	cached := chunk.Usage.PromptDetails.CachedTokens
	if cached == 0 {
		cached = chunk.Usage.CacheReadTokens
	}
	cacheWrite := chunk.Usage.PromptDetails.CacheWriteTokens
	if cacheWrite == 0 {
		cacheWrite = chunk.Usage.CacheWriteTokens
	}
	usage := Usage{
		InputTokens:       chunk.Usage.PromptTokens,
		CachedInputTokens: cached,
		CacheWriteTokens:  cacheWrite,
		OutputTokens:      chunk.Usage.CompletionTokens,
		ReasoningTokens:   chunk.Usage.CompletionDetails.ReasoningTokens,
		TotalTokens:       chunk.Usage.TotalTokens,
	}.Normalized()
	response.InputTokens = usage.InputTokens
	response.OutputTokens = usage.OutputTokens
	response.Usage = usage
}

func (c *ChatClient) sleep(ctx context.Context, duration time.Duration) error {
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

// cacheKey lets a provider reuse the prefix of a prompt across calls. Every
// pass in this repo sends the same long instruction block in front of a short
// provision, so the saving is most of the input tokens on most of the calls.
func cacheKey(instructions string) string {
	if instructions == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(instructions))
	return "luatdo-" + hex.EncodeToString(sum[:8])
}
