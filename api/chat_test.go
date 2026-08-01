package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sse(lines ...string) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("data: " + line + "\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func chatServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestChatClientReadsAStream(t *testing.T) {
	var body []byte
	server := chatServer(t, func(w http.ResponseWriter, r *http.Request) {
		body = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse(
			`{"id":"c1","model":"m","choices":[{"delta":{"content":"phần "}}]}`,
			`{"choices":[{"delta":{"content":"một"}}]}`,
			`{"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":40}}}`,
		)))
	})
	client := &ChatClient{URL: server.URL}
	resp, err := client.Complete(context.Background(), Request{Model: "m", Input: "hỏi"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "phần một" {
		t.Errorf("Text = %q, the deltas must be joined in order", resp.Text)
	}
	if resp.ID != "c1" || resp.Model != "m" {
		t.Errorf("id and model = %q %q, want them off the first chunk that carried them", resp.ID, resp.Model)
	}
	if resp.Usage.CachedInputTokens != 40 || resp.Usage.OutputTokens != 20 {
		t.Errorf("usage = %+v, the cost report is built on this", resp.Usage)
	}
	if !strings.Contains(string(body), `"stream":true`) {
		t.Error("the request did not ask for a stream, and a pass that runs for minutes needs one")
	}
}

func TestChatClientAcceptsAWholeJSONAnswer(t *testing.T) {
	// A proxy that ignores the stream flag is answering the question anyway.
	server := chatServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c2","model":"m","choices":[{"message":{"content":"xong"}}],` +
			`"usage":{"prompt_tokens":5,"completion_tokens":2}}`))
	})
	client := &ChatClient{URL: server.URL}
	resp, err := client.Complete(context.Background(), Request{Model: "m", Input: "hỏi"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "xong" {
		t.Errorf("Text = %q", resp.Text)
	}
}

func TestChatClientRetriesOnlyWhatIsWorthRetrying(t *testing.T) {
	calls := 0
	server := chatServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse(`{"choices":[{"delta":{"content":"ok"}}]}`)))
	})
	client := &ChatClient{URL: server.URL, MaxRetries: 2,
		Sleep: func(context.Context, time.Duration) error { return nil }}
	if _, err := client.Complete(context.Background(), Request{Model: "m", Input: "x"}); err != nil {
		t.Fatalf("a bad gateway must be retried: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want one failure and one success", calls)
	}

	calls = 0
	bad := chatServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"model_not_found"}}`))
	})
	client = &ChatClient{URL: bad.URL, MaxRetries: 3,
		Sleep: func(context.Context, time.Duration) error { return nil }}
	_, err := client.Complete(context.Background(), Request{Model: "m", Input: "x"})
	if err == nil {
		t.Fatal("a 400 was treated as success")
	}
	if calls != 1 {
		t.Errorf("calls = %d, a request the endpoint refuses does not get better on the fourth try", calls)
	}
	if !strings.Contains(err.Error(), "model_not_found") {
		t.Errorf("error = %v, the body has to survive so the router can classify it", err)
	}
}

func TestChatClientRefusesAnEmptyAnswer(t *testing.T) {
	// A stream that carried no content is a failed call wearing a 200. Passing
	// it on would land as a JSON parse error several layers away with nothing
	// left to say where it came from.
	server := chatServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse(`{"choices":[{"delta":{"content":""}}]}`)))
	})
	client := &ChatClient{URL: server.URL}
	if _, err := client.Complete(context.Background(), Request{Model: "m", Input: "x"}); err == nil {
		t.Error("an empty answer was returned as an answer")
	}
}

func TestChatClientChecksItsOwnArguments(t *testing.T) {
	for _, tc := range []struct {
		name    string
		client  *ChatClient
		request Request
	}{
		{"no url", &ChatClient{}, Request{Model: "m", Input: "x"}},
		{"no model", &ChatClient{URL: "http://x"}, Request{Input: "x"}},
		{"no input", &ChatClient{URL: "http://x"}, Request{Model: "m"}},
	} {
		if _, err := tc.client.Complete(context.Background(), tc.request); err == nil {
			t.Errorf("%s was sent anyway", tc.name)
		}
	}
}

func TestCacheKeyIsStableAndScoped(t *testing.T) {
	// Every pass here sends one long instruction block in front of a short
	// provision, so a stable key over the instructions is most of the input
	// tokens on most of the calls.
	first := cacheKey("hướng dẫn")
	if first != cacheKey("hướng dẫn") {
		t.Error("the same instructions produced two keys, which caches nothing")
	}
	if first == cacheKey("hướng dẫn khác") {
		t.Error("different instructions produced one key, which would serve the wrong prefix")
	}
	if cacheKey("") != "" {
		t.Error("empty instructions produced a key")
	}
	if !strings.HasPrefix(first, "luatdo-") {
		t.Errorf("key = %q, want it namespaced so it cannot collide with another tool on the same account", first)
	}
}
