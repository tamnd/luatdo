package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// jwt builds an unsigned token carrying the claims a real one carries. Nothing
// here verifies signatures, so a real key would only make the test slower.
func jwt(claims map[string]any) string {
	payload, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	return "x." + base64.RawURLEncoding.EncodeToString(payload) + ".y"
}

func accessToken(expires time.Time) string {
	return jwt(map[string]any{"exp": float64(expires.Unix())})
}

func idToken(plan, account string) string {
	return jwt(map[string]any{authClaim: map[string]any{
		"chatgpt_plan_type": plan, "chatgpt_account_id": account,
	}})
}

func writeAuth(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTokenReadsBothLayouts(t *testing.T) {
	// The CLI nests the tokens. Anything that completes the same OAuth flow and
	// stores the raw response leaves them at the top level. Both are on disk in
	// the wild and neither is wrong.
	future := time.Now().Add(time.Hour)
	nested := writeAuth(t, fmt.Sprintf(`{"tokens":{"access_token":%q,"id_token":%q}}`,
		accessToken(future), idToken("plus", "acc-1")))
	flat := writeAuth(t, fmt.Sprintf(`{"access_token":%q,"id_token":%q}`,
		accessToken(future), idToken("plus", "acc-1")))

	for name, path := range map[string]string{"nested": nested, "flat": flat} {
		auth := &Auth{Path: path}
		token, err := auth.Token(context.Background())
		if err != nil {
			t.Fatalf("%s: Token: %v", name, err)
		}
		if token.PlanType != "plus" || token.AccountID != "acc-1" {
			t.Errorf("%s: plan and account = %q %q", name, token.PlanType, token.AccountID)
		}
		if token.ExpiresAt.IsZero() {
			t.Errorf("%s: no expiry was read, so nothing knows when to refresh", name)
		}
	}
}

func TestTokenRefreshesAndKeepsWhatItDoesNotUnderstand(t *testing.T) {
	// The Codex CLI may be running against the same file. Dropping a field this
	// code has never heard of would break it silently.
	path := writeAuth(t, fmt.Sprintf(
		`{"OPENAI_API_KEY":null,"tokens":{"access_token":%q,"refresh_token":"r1","id_token":%q},"a_field_from_the_future":42}`,
		accessToken(time.Now().Add(-time.Hour)), idToken("pro", "acc-2")))

	fresh := accessToken(time.Now().Add(time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("refresh_token") != "r1" || r.Form.Get("client_id") != ClientID {
			t.Errorf("refresh form = %v, the token endpoint checks both", r.Form)
		}
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"r2","id_token":%q}`,
			fresh, idToken("pro", "acc-2"))
	}))
	defer server.Close()

	auth := &Auth{Path: path, TokenURL: server.URL}
	token, err := auth.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Access != fresh {
		t.Error("the refreshed token was not the one handed back")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var written map[string]any
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatal(err)
	}
	if _, ok := written["a_field_from_the_future"]; !ok {
		t.Error("a field this code does not understand was dropped on write back")
	}
	if _, ok := written["OPENAI_API_KEY"]; !ok {
		t.Error("a null valued field was dropped on write back")
	}
	tokens, _ := written["tokens"].(map[string]any)
	if tokens["refresh_token"] != "r2" {
		t.Errorf("refresh_token = %v, want the rotated one persisted", tokens["refresh_token"])
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Error("no backup was kept of the version this process replaced")
	}
	if info, err := os.Stat(path); err == nil && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, a credential must not widen when it is rewritten", info.Mode().Perm())
	}
}

func TestTokenSurvivesARefreshFailureWhileTheStoredOneIsLive(t *testing.T) {
	// A campaign over this corpus runs for hours. Losing it to one transient
	// error at the token endpoint, while holding a token that still works,
	// would be a poor trade.
	live := accessToken(time.Now().Add(2 * time.Minute))
	path := writeAuth(t, fmt.Sprintf(`{"tokens":{"access_token":%q,"refresh_token":"r1"}}`, live))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	logged := 0
	auth := &Auth{Path: path, TokenURL: server.URL, RefreshEarly: 5 * time.Minute,
		Logf: func(string, ...any) { logged++ }}
	token, err := auth.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Access != live {
		t.Error("the stored token was not used after the refresh failed")
	}
	if _, err := auth.Token(context.Background()); err != nil {
		t.Fatalf("second Token: %v", err)
	}
	if logged != 1 {
		t.Errorf("logged %d times, a repeated failure must not fill the run with the same line", logged)
	}
}

func TestTokenRefusesWhatCannotBeUsed(t *testing.T) {
	expired := accessToken(time.Now().Add(-time.Hour))
	for name, body := range map[string]string{
		"no access token":              `{"tokens":{"refresh_token":"r1"}}`,
		"expired with nothing to do":   fmt.Sprintf(`{"tokens":{"access_token":%q}}`, expired),
		"expired and refresh rejected": fmt.Sprintf(`{"tokens":{"access_token":%q,"refresh_token":"r1"}}`, expired),
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		auth := &Auth{Path: writeAuth(t, body), TokenURL: server.URL}
		if _, err := auth.Token(context.Background()); err == nil {
			t.Errorf("%s: a token that cannot work was handed out", name)
		}
		server.Close()
	}
	if _, err := (&Auth{Path: filepath.Join(t.TempDir(), "absent.json")}).Token(context.Background()); err == nil {
		t.Error("a missing credential file was not reported")
	}
}

func TestTokenWithNoReadableExpiryIsUsed(t *testing.T) {
	// The server checks the token. Refusing one we simply could not parse would
	// be refusing a credential that may well be fine.
	path := writeAuth(t, `{"tokens":{"access_token":"not-a-jwt"}}`)
	token, err := (&Auth{Path: path}).Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Expired(time.Now()) {
		t.Error("a token with no readable expiry was called expired")
	}
}

func TestLockFileIsTakenOverWhenItIsStale(t *testing.T) {
	// A lock left behind by a killed process would otherwise block every
	// refresh from here on.
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	stale := path + ".lock"
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockFile(path, time.Now)
	if err != nil {
		t.Fatalf("lockFile: %v", err)
	}
	unlock()
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("the lock was not released")
	}
}
