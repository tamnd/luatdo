package codex

import (
	"bufio"
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
	"sync"
	"time"

	"github.com/tamnd/luatdo/api"
)

const (
	// DefaultResponsesURL is the Codex backend the CLI streams from.
	DefaultResponsesURL = "https://chatgpt.com/backend-api/codex/responses"

	// ClientVersion is sent as the originator version.
	ClientVersion = "0.144.6"

	// DefaultModel is what a codex route uses when it names no model.
	DefaultModel = "gpt-5.6-luna"
)

// Models are the slugs a ChatGPT account token may use.
//
// The list is fixed rather than fetched. The models endpoint under reports: it
// hides entries that answer perfectly well, and a slug it omits is not thereby
// unavailable. A 400 whose body names the model is a slug problem rather than
// an entitlement problem, and reading those as plan rejections is what makes a
// working subscription look locked out.
var Models = []string{
	"gpt-5.6-terra",
	"gpt-5.6-luna",
	"gpt-5.5",
	"gpt-5.4-mini",
}

// QuotaError says the plan is out of quota until a stated time.
//
// The router reads it to set an exact cooldown, and that is the whole reason it
// exists as a type rather than a string. A plan window can be days wide. The
// difference between sleeping until it reopens and retrying every five minutes
// is the difference between a campaign that finishes and a campaign that spends
// four days proving the wall is still there.
type QuotaError struct {
	PlanType string
	ResetsAt time.Time
	Message  string
}

func (e *QuotaError) Error() string {
	message := e.Message
	if message == "" {
		message = "usage limit reached"
	}
	if e.ResetsAt.IsZero() {
		return fmt.Sprintf("codex %s plan: %s", e.PlanType, message)
	}
	return fmt.Sprintf("codex %s plan: %s, resets %s", e.PlanType, message,
		e.ResetsAt.UTC().Format(time.RFC3339))
}

// RetryAfter is how long the caller should wait, for a router that does not
// want to reason about wall clock time.
func (e *QuotaError) RetryAfter(now time.Time) time.Duration {
	if e.ResetsAt.IsZero() {
		return 0
	}
	return max(0, e.ResetsAt.Sub(now))
}

// Window is one rate limit window as the backend reports it.
type Window struct {
	UsedPercent   float64   `json:"used_percent"`
	WindowMinutes int       `json:"window_minutes"`
	ResetsAt      time.Time `json:"resets_at"`
}

// Limits is the rate limit state carried on every response, including a 429.
// There is no usage endpoint, so this is the only place the numbers exist, and
// doctor reports them because a plan at 97 percent is worth knowing about
// before starting a run rather than an hour into one.
type Limits struct {
	PlanType    string    `json:"plan_type,omitempty"`
	ActiveLimit string    `json:"active_limit,omitempty"`
	Primary     *Window   `json:"primary,omitempty"`
	Secondary   *Window   `json:"secondary,omitempty"`
	ReadAt      time.Time `json:"read_at"`
}

// Client calls the Codex backend and satisfies api.Completer.
type Client struct {
	Auth          *Auth
	URL           string
	HTTPClient    *http.Client
	MaxRetries    int
	MaxRetryDelay time.Duration
	Effort        string
	UserAgent     string
	Sleep         func(context.Context, time.Duration) error
	NewSessionID  func() string

	mu     sync.Mutex
	limits Limits
}

func (c *Client) auth() *Auth {
	if c.Auth == nil {
		c.Auth = &Auth{}
	}
	return c.Auth
}

func (c *Client) url() string {
	if strings.TrimSpace(c.URL) != "" {
		return c.URL
	}
	return DefaultResponsesURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Minute}
}

func (c *Client) sessionID() string {
	if c.NewSessionID != nil {
		return c.NewSessionID()
	}
	return newUUID()
}

// Limits is the last rate limit state seen on the wire.
func (c *Client) Limits() Limits {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.limits
}

type wireRequest struct {
	Model        string      `json:"model"`
	Instructions string      `json:"instructions,omitempty"`
	Input        []wireInput `json:"input"`
	Stream       bool        `json:"stream"`
	Store        bool        `json:"store"`
	Reasoning    *reasoning  `json:"reasoning,omitempty"`
}

type wireInput struct {
	Type    string        `json:"type"`
	Role    string        `json:"role"`
	Content []wireContent `json:"content"`
}

type wireContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// reasoning carries the effort only. The endpoint rejects a summary of "none"
// and accepts only concise, detailed or auto, so asking for one at all buys
// nothing but a risk of reasoning prose landing in the JSON every pass here
// parses.
type reasoning struct {
	Effort string `json:"effort"`
}

// Complete sends one turn and returns the whole answer.
//
// Note there is no max_output_tokens: this endpoint rejects the field outright
// rather than clamping it, so a cap has to come from the prompt or the context.
func (c *Client) Complete(ctx context.Context, request api.Request) (api.Response, error) {
	if strings.TrimSpace(request.Input) == "" {
		return api.Response{}, errors.New("input is empty")
	}
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = DefaultModel
	}
	effort := request.Effort
	if effort == "" {
		effort = c.Effort
	}
	body := wireRequest{
		Model:        model,
		Instructions: request.Instructions,
		Input: []wireInput{{
			Type: "message", Role: "user",
			Content: []wireContent{{Type: "input_text", Text: request.Input}},
		}},
		Stream: true,
		Store:  false,
	}
	if effort != "" {
		body.Reasoning = &reasoning{Effort: effort}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return api.Response{}, fmt.Errorf("encode codex request: %w", err)
	}

	var last error
	for attempt := 0; attempt <= max(0, c.MaxRetries); attempt++ {
		response, retryAfter, retry, err := c.do(ctx, payload)
		if err == nil {
			return response, nil
		}
		last = err
		if _, ok := errors.AsType[*QuotaError](err); ok {
			// Waiting out a plan window inside one request is never right. The
			// router decides whether to sleep or take another route.
			return api.Response{}, err
		}
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
			return api.Response{}, err
		}
	}
	return api.Response{}, last
}

func (c *Client) do(ctx context.Context, payload []byte) (api.Response, time.Duration, bool, error) {
	token, err := c.auth().Token(ctx)
	if err != nil {
		return api.Response{}, 0, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(), bytes.NewReader(payload))
	if err != nil {
		return api.Response{}, 0, false, err
	}
	c.setHeaders(req, token)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return api.Response{}, 0, false, ctx.Err()
		}
		return api.Response{}, 0, true, fmt.Errorf("call codex responses: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	limits := parseLimits(resp.Header, time.Now())
	if limits.PlanType == "" {
		limits.PlanType = token.PlanType
	}
	c.mu.Lock()
	c.limits = limits
	c.mu.Unlock()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if quota := quotaError(resp.StatusCode, resp.Header, raw, token.PlanType); quota != nil {
			return api.Response{}, 0, false, quota
		}
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		retryAfter := headerRetryAfter(resp.Header)
		return api.Response{}, retryAfter, retry,
			fmt.Errorf("codex responses returned %s: %s", resp.Status, trim(raw, 500))
	}

	response, err := parseStream(resp.Body)
	if err != nil {
		return api.Response{}, 0, true, err
	}
	return response, 0, false, nil
}

func (c *Client) setHeaders(req *http.Request, token Token) {
	req.Header.Set("Authorization", "Bearer "+token.Access)
	req.Header.Set("chatgpt-account-id", token.AccountID)
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("session_id", c.sessionID())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	agent := c.UserAgent
	if agent == "" {
		agent = "codex_cli_rs/" + ClientVersion
	}
	req.Header.Set("User-Agent", agent)
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

type streamEvent struct {
	Type     string `json:"type"`
	Delta    string `json:"delta"`
	Response struct {
		ID     string `json:"id"`
		Model  string `json:"model"`
		Status string `json:"status"`
		Usage  struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
			InputDetails struct {
				CachedTokens     int `json:"cached_tokens"`
				CacheWriteTokens int `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
			OutputDetails struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"response"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// parseStream reads the Responses event stream to completion.
//
// A stream that stops before response.completed is an error, never a partial
// answer. Half a JSON object is not a shorter extraction, it is an extraction
// that will fail to parse a few layers away from here with nothing left to say
// why.
func parseStream(reader io.Reader) (api.Response, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var text strings.Builder
	var response api.Response
	completed := false

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		var event streamEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return api.Response{}, fmt.Errorf("decode codex stream: %w", err)
		}
		if event.Error != nil {
			return api.Response{}, fmt.Errorf("codex stream error: %s", event.Error.Message)
		}
		switch event.Type {
		case "response.output_text.delta":
			text.WriteString(event.Delta)
		case "response.failed", "response.incomplete":
			message := event.Response.Status
			if event.Response.Error != nil {
				message = event.Response.Error.Message
			}
			return api.Response{}, fmt.Errorf("codex response %s: %s", event.Type, message)
		case "response.completed":
			completed = true
			response.ID = event.Response.ID
			response.Model = event.Response.Model
			usage := api.Usage{
				InputTokens:       event.Response.Usage.InputTokens,
				CachedInputTokens: event.Response.Usage.InputDetails.CachedTokens,
				CacheWriteTokens:  event.Response.Usage.InputDetails.CacheWriteTokens,
				OutputTokens:      event.Response.Usage.OutputTokens,
				ReasoningTokens:   event.Response.Usage.OutputDetails.ReasoningTokens,
				TotalTokens:       event.Response.Usage.TotalTokens,
			}.Normalized()
			response.Usage = usage
			response.InputTokens = usage.InputTokens
			response.OutputTokens = usage.OutputTokens
		}
	}
	if err := scanner.Err(); err != nil {
		return api.Response{}, fmt.Errorf("read codex stream: %w", err)
	}
	if !completed {
		return api.Response{}, errors.New("codex stream ended before response.completed")
	}
	response.Text = strings.TrimSpace(text.String())
	if response.Text == "" {
		return api.Response{}, errors.New("codex returned no text")
	}
	return response, nil
}

type errorBody struct {
	Error struct {
		Type            string  `json:"type"`
		Message         string  `json:"message"`
		PlanType        string  `json:"plan_type"`
		ResetsAt        int64   `json:"resets_at"`
		ResetsInSeconds float64 `json:"resets_in_seconds"`
	} `json:"error"`
}

// quotaError recognises an exhausted plan, and only that. A 429 from an
// ordinary rate limiter is a transient failure worth retrying in seconds; a 429
// saying usage_limit_reached means the window is closed for days, and treating
// the two the same in either direction is wrong.
func quotaError(status int, headers http.Header, raw []byte, plan string) *QuotaError {
	var decoded errorBody
	_ = json.Unmarshal(raw, &decoded)
	if decoded.Error.Type != "usage_limit_reached" && status != http.StatusTooManyRequests {
		return nil
	}
	if decoded.Error.Type != "usage_limit_reached" && !strings.Contains(
		strings.ToLower(decoded.Error.Message), "usage limit") {
		return nil
	}
	quota := &QuotaError{PlanType: decoded.Error.PlanType, Message: decoded.Error.Message}
	if quota.PlanType == "" {
		quota.PlanType = plan
	}
	switch {
	case decoded.Error.ResetsAt > 0:
		quota.ResetsAt = time.Unix(decoded.Error.ResetsAt, 0)
	case decoded.Error.ResetsInSeconds > 0:
		quota.ResetsAt = time.Now().Add(time.Duration(decoded.Error.ResetsInSeconds) * time.Second)
	default:
		if delay := headerRetryAfter(headers); delay > 0 {
			quota.ResetsAt = time.Now().Add(delay)
		}
	}
	return quota
}

// parseLimits reads the rate limit headers. A window of zero minutes means the
// account does not run that window at all, not that it has one of zero length.
func parseLimits(headers http.Header, now time.Time) Limits {
	limits := Limits{
		PlanType:    strings.TrimSpace(headers.Get("x-codex-plan-type")),
		ActiveLimit: strings.TrimSpace(headers.Get("x-codex-active-limit")),
		ReadAt:      now,
	}
	limits.Primary = parseWindow(headers, "primary")
	limits.Secondary = parseWindow(headers, "secondary")
	return limits
}

func parseWindow(headers http.Header, which string) *Window {
	minutes := headerNumber(headers, "x-codex-"+which+"-window-minutes")
	if minutes <= 0 {
		return nil
	}
	window := &Window{
		UsedPercent:   headerNumber(headers, "x-codex-"+which+"-used-percent"),
		WindowMinutes: int(minutes),
	}
	if at := headerNumber(headers, "x-codex-"+which+"-reset-at"); at > 0 {
		window.ResetsAt = time.Unix(int64(at), 0)
	} else if in := headerNumber(headers, "x-codex-"+which+"-reset-after-seconds"); in > 0 {
		window.ResetsAt = time.Now().Add(time.Duration(in) * time.Second)
	}
	return window
}

// String renders one window for doctor.
func (w *Window) String() string {
	if w == nil {
		return "none"
	}
	out := fmt.Sprintf("%.0f%% of a %s window", w.UsedPercent, windowLength(w.WindowMinutes))
	if !w.ResetsAt.IsZero() {
		out += ", resets " + w.ResetsAt.Local().Format("Mon 15:04")
	}
	return out
}

func windowLength(minutes int) string {
	switch {
	case minutes >= 1440 && minutes%1440 == 0:
		return fmt.Sprintf("%dd", minutes/1440)
	case minutes >= 60 && minutes%60 == 0:
		return fmt.Sprintf("%dh", minutes/60)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

func headerNumber(headers http.Header, name string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(headers.Get(name)), 64)
	if err != nil {
		return 0
	}
	return value
}

// headerRetryAfter prefers the reset the backend states over Retry-After,
// because the rate limit headers ride along on the 429 and carry the real one.
func headerRetryAfter(headers http.Header) time.Duration {
	for _, which := range []string{"primary", "secondary"} {
		if seconds := headerNumber(headers, "x-codex-"+which+"-reset-after-seconds"); seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	value := strings.TrimSpace(headers.Get("Retry-After"))
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

func backoff(attempt int) time.Duration {
	base := min(30*time.Second, time.Second<<min(attempt, 5))
	return base + time.Duration(rand.IntN(500))*time.Millisecond
}

func newUUID() string {
	var buf [16]byte
	for i := range buf {
		buf[i] = byte(rand.IntN(256))
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
