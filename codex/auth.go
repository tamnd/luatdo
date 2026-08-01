// Package codex reaches the Codex backend that the official CLI uses, so a
// ChatGPT subscription becomes an ordinary api.Completer.
//
// This matters here for one reason. The corpus is 93225 documents and every
// milestone from M2 onward has shipped a model pass that never saw a model,
// because a metered endpoint at list price is not a thing anyone runs a corpus
// this size through casually. A subscription already paid for is, and the
// credential to use it is already on the machine.
//
// The credential is an OAuth token left by the Codex CLI login flow in
// ~/.codex/auth.json. This package reads that file, refreshes the access token
// when it is close to expiry, and writes the result back without disturbing
// fields it does not understand, because the CLI may be running against the
// same file at the same time.
package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// ClientID is the Codex CLI's public OAuth client. A refresh has to present
	// the client that minted the token, so this is fixed rather than a setting.
	ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	// DefaultTokenURL is the OAuth token endpoint used for refreshes.
	DefaultTokenURL = "https://auth.openai.com/oauth/token"

	// authClaim is the namespaced claim holding the plan and the account id.
	authClaim = "https://api.openai.com/auth"
)

// Token is the part of the credential a request actually needs.
type Token struct {
	Access    string    `json:"access_token"`
	AccountID string    `json:"account_id"`
	PlanType  string    `json:"plan_type"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Expired reports whether the token is past its expiry. A token with no
// readable expiry is treated as live, because refusing to use a credential we
// simply could not parse is worse than letting the server reject it.
func (t Token) Expired(now time.Time) bool {
	return !t.ExpiresAt.IsZero() && !now.Before(t.ExpiresAt)
}

// Auth loads and refreshes the credential in a Codex auth file.
//
// The zero value is usable: it reads ~/.codex/auth.json and refreshes a token
// that expires within five minutes.
type Auth struct {
	Path         string        // defaults to ~/.codex/auth.json
	RefreshEarly time.Duration // refresh this long before expiry, default 5m
	TokenURL     string        // defaults to DefaultTokenURL
	HTTPClient   *http.Client
	Now          func() time.Time
	Logf         func(format string, args ...any)

	mu            sync.Mutex
	cached        Token
	backedUp      bool
	refreshFailed bool
}

// DefaultAuthPath is the Codex CLI credential location.
func DefaultAuthPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".codex", "auth.json")
	}
	return filepath.Join(home, ".codex", "auth.json")
}

func (a *Auth) path() string {
	if strings.TrimSpace(a.Path) != "" {
		return a.Path
	}
	return DefaultAuthPath()
}

func (a *Auth) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Auth) refreshEarly() time.Duration {
	if a.RefreshEarly > 0 {
		return a.RefreshEarly
	}
	return 5 * time.Minute
}

func (a *Auth) logf(format string, args ...any) {
	if a.Logf != nil {
		a.Logf(format, args...)
	}
}

// Token returns a usable access token, refreshing and persisting it when the
// stored one is expired or close to it.
//
// A refresh failure is not fatal while the stored token is still inside its
// expiry: the call proceeds on the old token and the failure is logged once.
// A campaign over this corpus runs for hours, and losing it to one transient
// error at the token endpoint would be a poor trade for that strictness.
func (a *Auth) Token(ctx context.Context) (Token, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	stored, err := readCredential(a.path())
	if err != nil {
		return Token{}, err
	}
	token := stored.token()
	if token.Access == "" {
		return Token{}, fmt.Errorf("%s has no access token, run the Codex CLI login", a.path())
	}
	now := a.now()
	if !token.Expired(now.Add(a.refreshEarly())) {
		a.cached = token
		return token, nil
	}
	if stored.refreshToken() == "" {
		if token.Expired(now) {
			return Token{}, fmt.Errorf("%s access token expired and holds no refresh token", a.path())
		}
		return token, nil
	}

	refreshed, err := a.refresh(ctx, stored.refreshToken())
	if err != nil {
		if token.Expired(now) {
			return Token{}, fmt.Errorf("refresh Codex token: %w", err)
		}
		if !a.refreshFailed {
			a.refreshFailed = true
			a.logf("codex: token refresh failed, continuing on the stored token: %v", err)
		}
		return token, nil
	}
	a.refreshFailed = false

	updated := stored.withRefreshed(refreshed, now)
	if err := a.write(updated); err != nil {
		// The token in hand is good even when persisting it is not possible, so
		// report the write problem and use it rather than failing the request.
		a.logf("codex: could not write %s: %v", a.path(), err)
	}
	token = updated.token()
	a.cached = token
	return token, nil
}

// Cached is the last token handed out, for reporting without a network call.
func (a *Auth) Cached() Token {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cached
}

// credential is an auth file kept in the shape it arrived in.
//
// Two layouts exist. The Codex CLI nests the tokens under a "tokens" object.
// Anything that completes the same OAuth flow and stores the raw token response
// leaves them at the top level. Unknown keys are carried through untouched so a
// write back never drops a field this code has not heard of.
type credential struct {
	root   map[string]json.RawMessage
	tokens map[string]json.RawMessage
	nested bool
	mode   os.FileMode
	raw    []byte
}

func readCredential(path string) (*credential, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no Codex credential at %s, run the Codex CLI login", path)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	cred := &credential{mode: 0o600, raw: raw}
	if info, err := os.Stat(path); err == nil {
		cred.mode = info.Mode().Perm()
	}
	if err := json.Unmarshal(raw, &cred.root); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if nested, ok := cred.root["tokens"]; ok {
		if err := json.Unmarshal(nested, &cred.tokens); err == nil && cred.tokens != nil {
			cred.nested = true
			return cred, nil
		}
	}
	cred.tokens = cred.root
	return cred, nil
}

func (c *credential) str(key string) string {
	raw, ok := c.tokens[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func (c *credential) refreshToken() string { return c.str("refresh_token") }

func (c *credential) token() Token {
	token := Token{
		Access:    c.str("access_token"),
		AccountID: c.str("account_id"),
	}
	// The id token carries the plan and the account, and the access token
	// carries the expiry. Neither is required to be present.
	if claims, err := decodeJWT(c.str("id_token")); err == nil {
		if auth, ok := claims[authClaim].(map[string]any); ok {
			if value, ok := auth["chatgpt_plan_type"].(string); ok {
				token.PlanType = value
			}
			if value, ok := auth["chatgpt_account_id"].(string); ok && token.AccountID == "" {
				token.AccountID = value
			}
		}
	}
	if claims, err := decodeJWT(token.Access); err == nil {
		if exp, ok := claims["exp"].(float64); ok {
			token.ExpiresAt = time.Unix(int64(exp), 0)
		}
	}
	return token
}

// withRefreshed returns a copy carrying the new tokens, leaving every other
// field exactly as it was read.
func (c *credential) withRefreshed(fresh refreshResponse, now time.Time) *credential {
	out := &credential{nested: c.nested, mode: c.mode, raw: c.raw, root: cloneRaw(c.root)}
	out.tokens = out.root
	if c.nested {
		out.tokens = cloneRaw(c.tokens)
	}
	set := func(key, value string) {
		if value == "" {
			return
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return
		}
		out.tokens[key] = encoded
	}
	set("access_token", fresh.AccessToken)
	set("refresh_token", fresh.RefreshToken)
	set("id_token", fresh.IDToken)
	if out.nested {
		if encoded, err := json.Marshal(out.tokens); err == nil {
			out.root["tokens"] = encoded
		}
	}
	if encoded, err := json.Marshal(now.UTC().Format(time.RFC3339Nano)); err == nil {
		out.root["last_refresh"] = encoded
	}
	return out
}

func cloneRaw(in map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(in))
	maps.Copy(out, in)
	return out
}

type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
}

func (a *Auth) refresh(ctx context.Context, refreshToken string) (refreshResponse, error) {
	endpoint := a.TokenURL
	if strings.TrimSpace(endpoint) == "" {
		endpoint = DefaultTokenURL
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {ClientID},
		"scope":         {"openid profile email offline_access"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return refreshResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := a.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return refreshResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return refreshResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return refreshResponse{}, fmt.Errorf("token endpoint returned %s: %s", resp.Status, trim(body, 300))
	}
	var decoded refreshResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return refreshResponse{}, fmt.Errorf("decode token response: %w", err)
	}
	if strings.TrimSpace(decoded.AccessToken) == "" {
		return refreshResponse{}, errors.New("token endpoint returned no access token")
	}
	return decoded, nil
}

// write persists the credential atomically, keeping a backup of the version
// this process first replaced.
func (a *Auth) write(cred *credential) error {
	path := a.path()
	encoded, err := json.MarshalIndent(cred.root, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')

	unlock, err := lockFile(path, a.now)
	if err != nil {
		return err
	}
	defer unlock()

	if !a.backedUp {
		if err := os.WriteFile(path+".bak", cred.raw, 0o600); err != nil {
			return fmt.Errorf("back up %s: %w", path, err)
		}
		a.backedUp = true
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".auth-*.json")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := temp.Write(encoded); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	mode := cred.mode
	if mode == 0 {
		mode = 0o600
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// lockFile takes a cooperative lock next to the credential. It is a lock file
// rather than flock because this has to behave the same on the gaming pc as on
// the servers, and the only contender is another process doing the same short
// read, modify, write.
func lockFile(path string, now func() time.Time) (func(), error) {
	name := path + ".lock"
	deadline := now().Add(10 * time.Second)
	for {
		file, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = file.Close()
			return func() { _ = os.Remove(name) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		// A lock left behind by a killed process would otherwise block every
		// refresh from here on, so an old one is taken over rather than obeyed.
		if info, statErr := os.Stat(name); statErr == nil && now().Sub(info.ModTime()) > time.Minute {
			_ = os.Remove(name)
			continue
		}
		if !now().Before(deadline) {
			return nil, fmt.Errorf("timed out waiting for %s", name)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// decodeJWT reads the claim set out of a JWT without verifying the signature.
// Nothing here is a security decision: the server checks the token, and this
// only wants the plan name and the expiry for scheduling.
func decodeJWT(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func trim(raw []byte, limit int) string {
	value := strings.TrimSpace(string(raw))
	if len(value) > limit {
		return value[:limit]
	}
	return value
}
