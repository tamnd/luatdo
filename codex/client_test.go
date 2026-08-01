package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/luatdo/api"
)

func events(lines ...string) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("data: " + line + "\n\n")
	}
	return b.String()
}

// liveAuth is a credential that needs no refresh, so a client test is about
// the client.
func liveAuth(t *testing.T) *Auth {
	t.Helper()
	path := writeAuth(t, fmt.Sprintf(`{"tokens":{"access_token":%q,"id_token":%q}}`,
		accessToken(time.Now().Add(time.Hour)), idToken("plus", "acc-1")))
	return &Auth{Path: path}
}

func codexServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestClientReadsAStreamToCompletion(t *testing.T) {
	var sent map[string]any
	server := codexServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Error(err)
		}
		if r.Header.Get("chatgpt-account-id") != "acc-1" {
			t.Errorf("account header = %q, the backend routes on it", r.Header.Get("chatgpt-account-id"))
		}
		w.Header().Set("x-codex-plan-type", "plus")
		w.Header().Set("x-codex-primary-window-minutes", "10080")
		w.Header().Set("x-codex-primary-used-percent", "6.5")
		_, _ = w.Write([]byte(events(
			`{"type":"response.output_text.delta","delta":"{\"a\":"}`,
			`{"type":"response.output_text.delta","delta":"1}"}`,
			`{"type":"response.completed","response":{"id":"r1","model":"gpt-5.6-luna",`+
				`"usage":{"input_tokens":900,"output_tokens":40,"input_tokens_details":{"cached_tokens":800},`+
				`"output_tokens_details":{"reasoning_tokens":25}}}}`,
		)))
	})
	client := &Client{Auth: liveAuth(t), URL: server.URL, Effort: "high"}
	resp, err := client.Complete(context.Background(), api.Request{Model: "gpt-5.6-luna", Input: "hỏi"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != `{"a":1}` {
		t.Errorf("Text = %q, the deltas must be joined with nothing between them", resp.Text)
	}
	if resp.Usage.CachedInputTokens != 800 || resp.Usage.ReasoningTokens != 25 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	if _, ok := sent["max_output_tokens"]; ok {
		t.Error("max_output_tokens was sent, and this endpoint rejects the field outright")
	}
	if reasoning, _ := sent["reasoning"].(map[string]any); reasoning["effort"] != "high" {
		t.Errorf("reasoning = %v, want the route's effort", sent["reasoning"])
	}
	limits := client.Limits()
	if limits.Primary == nil || limits.Primary.UsedPercent != 6.5 {
		t.Errorf("limits = %+v, they exist nowhere but these headers", limits)
	}
	if got := limits.Primary.String(); !strings.Contains(got, "7d") {
		t.Errorf("window = %q, want the length in days rather than 10080 minutes", got)
	}
}

func TestClientRefusesAStreamThatStoppedEarly(t *testing.T) {
	// Half a JSON object is not a shorter extraction. It is one that fails to
	// parse a few layers away with nothing left to say why.
	server := codexServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(events(`{"type":"response.output_text.delta","delta":"{\"a\":"}`)))
	})
	client := &Client{Auth: liveAuth(t), URL: server.URL}
	if _, err := client.Complete(context.Background(), api.Request{Model: "m", Input: "x"}); err == nil {
		t.Error("a truncated stream was returned as an answer")
	}
}

func TestClientReportsAFailedResponse(t *testing.T) {
	server := codexServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(events(
			`{"type":"response.failed","response":{"status":"failed","error":{"message":"nội dung bị từ chối"}}}`)))
	})
	client := &Client{Auth: liveAuth(t), URL: server.URL}
	_, err := client.Complete(context.Background(), api.Request{Model: "m", Input: "x"})
	if err == nil || !strings.Contains(err.Error(), "nội dung bị từ chối") {
		t.Errorf("err = %v, the reason has to survive", err)
	}
}

func TestQuotaErrorCarriesTheResetAndStopsTheRetryLoop(t *testing.T) {
	// A plan window can be days wide. Retrying inside one request would burn
	// the whole run against a wall the backend already said when it opens.
	resets := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	calls := 0
	server := codexServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprintf(w, `{"error":{"type":"usage_limit_reached","plan_type":"plus","resets_at":%d}}`,
			resets.Unix())
	})
	client := &Client{Auth: liveAuth(t), URL: server.URL, MaxRetries: 5,
		Sleep: func(context.Context, time.Duration) error { return nil }}
	_, err := client.Complete(context.Background(), api.Request{Model: "m", Input: "x"})
	quota, ok := errors.AsType[*QuotaError](err)
	if !ok {
		t.Fatalf("err = %v, want a typed quota error the router can read", err)
	}
	if !quota.ResetsAt.Equal(resets) {
		t.Errorf("ResetsAt = %s, want %s", quota.ResetsAt, resets)
	}
	if wait := quota.RetryAfter(time.Now()); wait < 47*time.Hour {
		t.Errorf("RetryAfter = %s, want the real window rather than a default", wait)
	}
	if calls != 1 {
		t.Errorf("calls = %d, an exhausted plan does not reopen on the second try", calls)
	}
}

func TestAnOrdinaryRateLimitIsNotAQuota(t *testing.T) {
	// A 429 from a rate limiter clears in seconds. Reading it as an exhausted
	// plan would park a working route for hours.
	calls := 0
	server := codexServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_exceeded","message":"slow down"}}`))
			return
		}
		_, _ = w.Write([]byte(events(
			`{"type":"response.output_text.delta","delta":"ok"}`,
			`{"type":"response.completed","response":{"id":"r","model":"m"}}`)))
	})
	client := &Client{Auth: liveAuth(t), URL: server.URL, MaxRetries: 2,
		Sleep: func(context.Context, time.Duration) error { return nil }}
	resp, err := client.Complete(context.Background(), api.Request{Model: "m", Input: "x"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "ok" || calls != 2 {
		t.Errorf("text %q after %d calls, want a retry that succeeded", resp.Text, calls)
	}
}

func TestParseWindowIgnoresAWindowTheAccountDoesNotRun(t *testing.T) {
	// Zero minutes means the account has no such window, not that it has one of
	// zero length that is permanently expired.
	headers := http.Header{}
	headers.Set("x-codex-secondary-window-minutes", "0")
	headers.Set("x-codex-secondary-used-percent", "50")
	if window := parseWindow(headers, "secondary"); window != nil {
		t.Errorf("window = %+v, want none", window)
	}
	if got := (*Window)(nil).String(); got != "none" {
		t.Errorf("String on no window = %q", got)
	}
}

func TestClientDefaultsTheModel(t *testing.T) {
	var sent map[string]any
	server := codexServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		_, _ = w.Write([]byte(events(
			`{"type":"response.output_text.delta","delta":"ok"}`,
			`{"type":"response.completed","response":{"id":"r","model":"m"}}`)))
	})
	client := &Client{Auth: liveAuth(t), URL: server.URL}
	if _, err := client.Complete(context.Background(), api.Request{Input: "x"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if sent["model"] != DefaultModel {
		t.Errorf("model = %v, a route that only says use the subscription is a reasonable thing to write", sent["model"])
	}
}
