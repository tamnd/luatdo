package route

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/codex"
)

// Wire is the request format an endpoint speaks.
//
// Three of them, because the capacity this corpus needs is spread across three.
// Responses is what OpenAI itself serves. Chat is what everything else serves,
// including the zen endpoint in front of DeepSeek and the rest of the free
// tier, and any local model on the gaming pc. Codex is not an endpoint anyone
// publishes a URL for; it is the backend the Codex CLI talks to, reached with
// the subscription credential already sitting in ~/.codex/auth.json.
type Wire string

const (
	WireChat      Wire = "chat"      // POST /v1/chat/completions, streaming
	WireResponses Wire = "responses" // POST /v1/responses
	WireCodex     Wire = "codex"     // the Codex backend, reached with a stored credential
)

// wire resolves the format for a route, defaulting from what it was given.
//
// A route file written before wires existed names a full responses URL and no
// wire at all, and it keeps working, because breaking a working configuration
// to add a field is not an upgrade.
func (r Route) wire() Wire {
	switch Wire(strings.ToLower(strings.TrimSpace(string(r.Wire)))) {
	case WireChat:
		return WireChat
	case WireResponses:
		return WireResponses
	case WireCodex:
		return WireCodex
	}
	if strings.TrimSpace(r.URL) != "" {
		return WireResponses
	}
	return WireChat
}

// Validate reports what is missing from a route, so a file with a typo says so
// when it loads rather than at the first call with a transport error that names
// none of it.
func (r Route) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("route has no name")
	}
	switch Wire(strings.ToLower(strings.TrimSpace(string(r.Wire)))) {
	case "", WireChat, WireResponses, WireCodex:
	default:
		return fmt.Errorf("route %s has unknown wire %q, want chat, responses, or codex", r.Name, r.Wire)
	}
	if r.wire() == WireCodex {
		// The model may be empty: codex.DefaultModel covers it, and a route file
		// that only says "use the subscription" is a reasonable thing to write.
		return nil
	}
	if strings.TrimSpace(r.Model) == "" {
		return fmt.Errorf("route %s has no model", r.Name)
	}
	if strings.TrimSpace(r.URL) == "" && strings.TrimSpace(r.BaseURL) == "" {
		return fmt.Errorf("route %s speaks %s and needs a base_url", r.Name, r.wire())
	}
	return nil
}

// ModelName is what to ask the endpoint for.
func (r Route) ModelName() string {
	if model := strings.TrimSpace(r.Model); model != "" {
		return model
	}
	if r.wire() == WireCodex {
		return codex.DefaultModel
	}
	return ""
}

// Key is the credential to send. It comes from the environment variable the
// file names rather than from the file, so a routes file carries no secret and
// can be committed, pasted into an issue, or copied to the other three machines
// without anyone having to remember to scrub it first.
func (r Route) Key() string {
	if r.APIKeyEnv == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(r.APIKeyEnv))
}

// endpoint joins the base URL to a path, tolerating a base that already ends at
// /v1 and one that stops at the server root. A route that gave a full URL keeps
// it verbatim, because that is what the older files hold.
func (r Route) endpoint(path string) string {
	if full := strings.TrimSpace(r.URL); full != "" {
		return full
	}
	base := strings.TrimRight(strings.TrimSpace(r.BaseURL), "/")
	if strings.HasSuffix(base, "/v1") {
		return base + path
	}
	return base + "/v1" + path
}

const userAgent = "luatdo-router"

// Client builds the transport for one route.
func (r Route) Client(timeout time.Duration, maxRetries int, httpClient *http.Client) (api.Completer, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if httpClient == nil {
		if timeout <= 0 {
			timeout = 30 * time.Minute
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	switch r.wire() {
	case WireCodex:
		return &codex.Client{
			Auth:       &codex.Auth{Path: r.Auth, HTTPClient: httpClient},
			HTTPClient: httpClient,
			MaxRetries: maxRetries,
			Effort:     r.Effort,
		}, nil
	case WireResponses:
		return &api.Client{
			URL: r.endpoint("/responses"), APIKey: r.Key(), HTTPClient: httpClient,
			MaxRetries: maxRetries, MaxOutputTokens: r.MaxOutputTokens, UserAgent: userAgent,
		}, nil
	default:
		return &api.ChatClient{
			URL: r.endpoint("/chat/completions"), APIKey: r.Key(), HTTPClient: httpClient,
			MaxRetries: maxRetries, MaxOutputTokens: r.MaxOutputTokens, UserAgent: userAgent,
		}, nil
	}
}
