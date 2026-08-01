package route

import (
	"testing"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/codex"
)

func TestWireIsInferredFromWhatTheFileGave(t *testing.T) {
	// A routes file written before wires existed names a full responses URL and
	// no wire at all. Breaking it to add a field would not be an upgrade.
	for name, tc := range map[string]struct {
		route Route
		want  Wire
	}{
		"an older file":     {Route{URL: "https://a/v1/responses", Model: "m"}, WireResponses},
		"a base url only":   {Route{BaseURL: "https://a/v1", Model: "m"}, WireChat},
		"said so":           {Route{Wire: WireResponses, BaseURL: "https://a", Model: "m"}, WireResponses},
		"said so in caps":   {Route{Wire: "CHAT", BaseURL: "https://a", Model: "m"}, WireChat},
		"the subscription":  {Route{Wire: WireCodex}, WireCodex},
		"codex needs no ur": {Route{Wire: WireCodex, Model: "gpt-5.6-luna"}, WireCodex},
	} {
		if got := tc.route.wire(); got != tc.want {
			t.Errorf("%s: wire = %q, want %q", name, got, tc.want)
		}
	}
}

func TestValidateSaysWhatIsMissing(t *testing.T) {
	for name, r := range map[string]Route{
		"no name":       {BaseURL: "https://a", Model: "m"},
		"no model":      {Name: "x", BaseURL: "https://a"},
		"nowhere to go": {Name: "x", Model: "m"},
		"unknown wire":  {Name: "x", Wire: "grpc", BaseURL: "https://a", Model: "m"},
	} {
		if err := r.Validate(); err == nil {
			t.Errorf("%s: loaded anyway, and it would fail at the first call with a transport error naming none of this", name)
		}
	}
	// A codex route may name no model at all. It says use the subscription,
	// which is a complete instruction.
	if err := (Route{Name: "codex", Wire: WireCodex}).Validate(); err != nil {
		t.Errorf("a bare codex route was rejected: %v", err)
	}
	if got := (Route{Name: "codex", Wire: WireCodex}).ModelName(); got != codex.DefaultModel {
		t.Errorf("ModelName = %q, want the codex default", got)
	}
}

func TestEndpointToleratesEitherShapeOfBaseURL(t *testing.T) {
	for name, tc := range map[string]struct {
		route Route
		want  string
	}{
		"stops at the root": {Route{BaseURL: "https://a"}, "https://a/v1/chat/completions"},
		"ends at v1":        {Route{BaseURL: "https://a/v1"}, "https://a/v1/chat/completions"},
		"trailing slash":    {Route{BaseURL: "https://a/v1/"}, "https://a/v1/chat/completions"},
		"a full url wins":   {Route{URL: "https://a/custom"}, "https://a/custom"},
	} {
		if got := tc.route.endpoint("/chat/completions"); got != tc.want {
			t.Errorf("%s: endpoint = %q, want %q", name, got, tc.want)
		}
	}
}

func TestKeyComesFromTheEnvironmentAndNeverFromTheFile(t *testing.T) {
	// A routes file holds no secret, so it can be committed, pasted into an
	// issue, or copied to the other three machines with nothing to scrub.
	t.Setenv("LUATDO_TEST_KEY", "  sk-secret  ")
	if got := (Route{APIKeyEnv: "LUATDO_TEST_KEY"}).Key(); got != "sk-secret" {
		t.Errorf("Key = %q, want it read and trimmed from the environment", got)
	}
	if got := (Route{}).Key(); got != "" {
		t.Errorf("Key = %q on a route naming no variable", got)
	}
}

func TestClientBuildsTheTransportTheWireNames(t *testing.T) {
	for name, tc := range map[string]struct {
		route Route
		want  string
	}{
		"chat":      {Route{Name: "a", Wire: WireChat, BaseURL: "https://a", Model: "m"}, "*api.ChatClient"},
		"responses": {Route{Name: "b", Wire: WireResponses, BaseURL: "https://a", Model: "m"}, "*api.Client"},
		"codex":     {Route{Name: "c", Wire: WireCodex}, "*codex.Client"},
	} {
		client, err := tc.route.Client(0, 1, nil)
		if err != nil {
			t.Fatalf("%s: Client: %v", name, err)
		}
		if got := typeName(client); got != tc.want {
			t.Errorf("%s: client = %s, want %s", name, got, tc.want)
		}
	}
	if _, err := (Route{Name: "x"}).Client(0, 1, nil); err == nil {
		t.Error("a route that does not validate produced a transport")
	}
}

func typeName(client api.Completer) string {
	switch client.(type) {
	case *api.ChatClient:
		return "*api.ChatClient"
	case *api.Client:
		return "*api.Client"
	case *codex.Client:
		return "*codex.Client"
	default:
		return "unknown"
	}
}

func TestCodexRouteCarriesItsCredentialPath(t *testing.T) {
	client, err := (Route{Name: "c", Wire: WireCodex, Auth: "/tmp/other.json", Effort: "high"}).Client(0, 1, nil)
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	c, ok := client.(*codex.Client)
	if !ok {
		t.Fatalf("client = %T", client)
	}
	if c.Auth.Path != "/tmp/other.json" {
		t.Errorf("auth path = %q, want the one the route named so two accounts can be two routes", c.Auth.Path)
	}
	if c.Effort != "high" {
		t.Errorf("effort = %q, want the route's", c.Effort)
	}
}
