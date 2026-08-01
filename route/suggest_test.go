package route

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCatalogueReadsAModelsList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("no credential was sent, and most catalogues need one")
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"b-free"},{"id":"a-free"},{"id":""}]}`))
	}))
	defer server.Close()

	t.Setenv("LUATDO_TEST_KEY", "sk-test")
	got := Catalogue(context.Background(), Route{
		Name: "zen", Wire: WireChat, BaseURL: server.URL, APIKeyEnv: "LUATDO_TEST_KEY", Model: "a-free",
	}, nil)
	if !slices.Equal(got, []string{"a-free", "b-free"}) {
		t.Errorf("catalogue = %v, want it sorted with the blank dropped", got)
	}
}

func TestCatalogueTreatsSilenceAsSilence(t *testing.T) {
	// Plenty of compatible servers have no models endpoint at all, and a route
	// file is not wrong for naming one of them.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	if got := Catalogue(context.Background(), Route{Name: "x", BaseURL: server.URL, Model: "m"}, nil); got != nil {
		t.Errorf("catalogue = %v, want nothing", got)
	}
	// The codex backend has no base URL to ask, and its real catalogue is
	// under reported by the endpoint that would answer, so the list is fixed.
	if got := Catalogue(context.Background(), Route{Name: "c", Wire: WireCodex}, nil); len(got) == 0 {
		t.Error("the codex route reported no models at all")
	}
}

func zen(name, model string, rank int) Route {
	return Route{Name: name, Wire: WireChat, BaseURL: ZenURL, Model: model,
		APIKeyEnv: "OPENCODE_API_KEY", Rank: rank}
}

func TestSuggestKeepsRanksAndDisablesDrift(t *testing.T) {
	// A rank is measured evidence about what answered well here. Regenerating
	// it from a catalogue would throw that away. A withdrawal can be reversed,
	// and a missing row just looks like an oversight.
	routes := []Route{zen("keeper", "still-here-free", 30), zen("gone", "withdrawn-free", 31)}
	catalogues := map[string][]string{
		"keeper": {"still-here-free", "brand-new-free", "paid-model"},
		"gone":   {"still-here-free", "brand-new-free"},
	}
	out := Suggest(routes, catalogues)

	byName := map[string]Route{}
	for _, r := range out {
		byName[r.Name] = r
	}
	if keeper := byName["keeper"]; keeper.Disabled || keeper.Rank != 30 {
		t.Errorf("keeper = %+v, want it untouched", keeper)
	}
	gone := byName["gone"]
	if !gone.Disabled {
		t.Error("a route whose model the endpoint no longer lists stayed enabled")
	}
	if gone.Note == "" {
		t.Error("a row was disabled with no reason, which reads as an oversight")
	}
	if gone.Rank != 31 {
		t.Errorf("gone rank = %d, a disabled route keeps its place", gone.Rank)
	}

	fresh, ok := byName["free-brand-new"]
	if !ok {
		t.Fatalf("the new free model was not offered, routes = %v", names(out))
	}
	if !fresh.Disabled {
		t.Error("an unproven model was enabled without anyone deciding to")
	}
	if fresh.Rank <= 31 {
		t.Errorf("rank = %d, a new route must not displace one with evidence behind it", fresh.Rank)
	}
	if _, offered := byName["free-paid-model"]; offered {
		t.Error("a model with no free suffix was offered, and this cannot tell what it would cost")
	}
}

func TestSuggestIsStableWhenNothingChanged(t *testing.T) {
	routes := []Route{zen("a", "a-free", 30), zen("b", "b-free", 31)}
	catalogues := map[string][]string{"a": {"a-free", "b-free"}, "b": {"a-free", "b-free"}}
	out := Suggest(routes, catalogues)
	if len(out) != 2 {
		t.Fatalf("routes = %v, want the file unchanged", names(out))
	}
	for _, r := range out {
		if r.Disabled {
			t.Errorf("%s was disabled for no reason", r.Name)
		}
	}
}

func TestDriftNamesOnlyWhatWasProbed(t *testing.T) {
	routes := []Route{zen("probed", "gone-free", 30), zen("silent", "unknown-free", 31)}
	lines := Drift(routes, map[string][]string{"probed": {"other-free"}})
	if len(lines) != 1 {
		t.Fatalf("drift = %v, a route that published no catalogue says nothing either way", lines)
	}
}

func TestWriteRefusesToOverwriteMeasuredRanks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "routes.json")
	if err := Write(path, Starter()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("the file it wrote does not load: %v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("the file it wrote has no enabled routes")
	}
	if err := Write(path, Starter()); err == nil {
		t.Error("a routes file was overwritten, and the ranks in it were measured")
	}
}

func TestStarterIsUsable(t *testing.T) {
	starter := Starter()
	seen := map[string]bool{}
	codexRoutes := 0
	for _, r := range starter {
		if err := r.Validate(); err != nil {
			t.Errorf("%s does not validate: %v", r.Name, err)
		}
		if seen[r.Name] {
			t.Errorf("%s is listed twice", r.Name)
		}
		seen[r.Name] = true
		if r.wire() == WireCodex {
			codexRoutes++
		}
		if r.Disabled && r.Note == "" {
			t.Errorf("%s is disabled with no reason given", r.Name)
		}
	}
	if codexRoutes == 0 {
		t.Error("the starter list offers no subscription route, and a corpus this size is not something to meter at list price")
	}
	if starter[0].Rank > starter[len(starter)-1].Rank {
		t.Error("the starter list is not in rank order")
	}
}

func TestStarterWritesAFileWithNoSecretsInIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routes.json")
	if err := Write(path, Starter()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sk-", "Bearer", "access_token"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the routes file contains %q, and it is meant to be committable", secret)
		}
	}
}

func names(routes []Route) []string {
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		out = append(out, r.Name)
	}
	return out
}
