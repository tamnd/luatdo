package route

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tamnd/luatdo/api"
)

// scripted is a Completer that answers from a list, so the router can be
// driven through failover without a network.
type scripted struct {
	replies []reply
	calls   int
}

type reply struct {
	text  string
	usage api.Usage
	err   error
}

func (s *scripted) Complete(context.Context, api.Request) (api.Response, error) {
	i := min(s.calls, len(s.replies)-1)
	s.calls++
	r := s.replies[i]
	if r.err != nil {
		return api.Response{}, r.err
	}
	return api.Response{Text: r.text, Usage: r.usage}, nil
}

func writeRoutes(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "routes.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	path := writeRoutes(t, `{"routes":[
		{"name":"metered","url":"https://b/v1","model":"m2","rank":5},
		{"name":"retired","url":"https://c/v1","model":"m3","rank":1,"disabled":true},
		{"name":"subscription","url":"https://a/v1","model":"m1","rank":2}
	]}`)
	routes, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("routes = %d, the disabled one must not load", len(routes))
	}
	if routes[0].Name != "subscription" || routes[1].Name != "metered" {
		t.Errorf("order = %s, %s, want rank order", routes[0].Name, routes[1].Name)
	}

	if _, err := Load(writeRoutes(t, `{"routes":[{"name":"x","model":"m"}]}`)); err == nil {
		t.Error("a route without a url must not load")
	}
	if _, err := Load(writeRoutes(t, `{"routes":[{"name":"x","url":"u","model":"m","disabled":true}]}`)); err == nil {
		t.Error("a file with nothing enabled must not load")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); !os.IsNotExist(err) {
		t.Errorf("missing file error = %v, callers distinguish it to fall back to the environment", err)
	}
}

func TestCostUnavailableIsContagious(t *testing.T) {
	priced := Cost{USD: 0.5, Available: true}
	unpriced := Cost{}
	if got := unpriced.String(); got != "unavailable" {
		t.Errorf("String = %q, want unavailable", got)
	}
	total := priced.Add(unpriced).Add(Cost{USD: 0.25, Available: true})
	if total.Available {
		t.Error("a total that includes unpriced usage must not claim to be a total")
	}
	if total.USD != 0.75 {
		t.Errorf("USD = %v, the known part is still summed", total.USD)
	}
	if got := priced.Add(Cost{USD: 0.25, Available: true}); !got.Available || got.String() != "$0.7500" {
		t.Errorf("priced total = %v", got)
	}
}

func TestPricingEstimate(t *testing.T) {
	p := &Pricing{InputPerM: 3, CachedInputPerM: 0.3, CacheWritePerM: 3.75, OutputPerM: 15}
	// 1M input of which 400k cached and 100k written, 200k output.
	got := p.Estimate(api.Usage{InputTokens: 1_000_000, CachedInputTokens: 400_000, CacheWriteTokens: 100_000, OutputTokens: 200_000})
	want := 500_000.0/1e6*3 + 400_000.0/1e6*0.3 + 100_000.0/1e6*3.75 + 200_000.0/1e6*15
	if !got.Available || got.USD != want {
		t.Errorf("Estimate = %v, want %v", got, want)
	}
	var nilCard *Pricing
	if got := nilCard.Estimate(api.Usage{InputTokens: 100}); got.Available {
		t.Error("a route with no rate card must report unavailable, never zero")
	}
}

func TestCause(t *testing.T) {
	cases := map[string]string{
		"responses API returned 401 Unauthorized: bad key": CauseAuth,
		"invalid api key": CauseAuth,
		"responses API returned 429: rate limit reached": CauseQuota,
		"insufficient_quota for this month":              CauseQuota,
		"dial tcp: connection refused":                   CauseTransport,
	}
	for text, want := range cases {
		if got := Cause(errors.New(text)); got != want {
			t.Errorf("Cause(%q) = %s, want %s", text, got, want)
		}
	}
	if got := Cause(nil); got != "" {
		t.Errorf("Cause(nil) = %q", got)
	}
}

func routerFor(t *testing.T, clients []api.Completer, now func() time.Time) *Router {
	t.Helper()
	routes := []Route{
		{Name: "first", URL: "https://a/v1", Model: "m1", Rank: 0, Pricing: &Pricing{InputPerM: 1, OutputPerM: 2}},
		{Name: "second", URL: "https://b/v1", Model: "m2", Rank: 1},
	}
	r := New(routes).WithCompleters(clients)
	r.now = now
	return r
}

func TestRouterFailsOverAndCoolsDown(t *testing.T) {
	clock := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	first := &scripted{replies: []reply{{err: errors.New("responses API returned 429: rate limit")}}}
	second := &scripted{replies: []reply{{text: "ok", usage: api.Usage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110}}}}
	r := routerFor(t, []api.Completer{first, second}, now)

	resp, err := r.Complete(context.Background(), api.Request{Input: "x"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Route != "second" {
		t.Errorf("Route = %q, want the failover route", resp.Route)
	}

	// The quota failure cools the first route, so the next call skips it
	// without spending another request proving the quota is still gone.
	if _, err := r.Complete(context.Background(), api.Request{Input: "x"}); err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if first.calls != 1 {
		t.Errorf("first route called %d times while on cooldown", first.calls)
	}

	clock = clock.Add(QuotaCooldown + time.Second)
	first.replies = []reply{{text: "back", usage: api.Usage{InputTokens: 50, OutputTokens: 5, TotalTokens: 55}}}
	resp, err = r.Complete(context.Background(), api.Request{Input: "x"})
	if err != nil {
		t.Fatalf("third Complete: %v", err)
	}
	if resp.Route != "first" {
		t.Errorf("Route = %q, want the cooled route back in rank order", resp.Route)
	}

	stats := r.Stats()
	if len(stats) != 2 {
		t.Fatalf("stats = %+v", stats)
	}
	usage, cost := r.Totals()
	if usage.TotalTokens != 55+110+110 {
		t.Errorf("total tokens = %d", usage.TotalTokens)
	}
	if cost.Available {
		t.Error("the second route has no rate card, so the campaign total cannot claim to be one")
	}
}

func TestRouterAuthFailureIsFinal(t *testing.T) {
	clock := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	first := &scripted{replies: []reply{{err: errors.New("responses API returned 401 Unauthorized")}}}
	second := &scripted{replies: []reply{{text: "ok"}}}
	r := routerFor(t, []api.Completer{first, second}, func() time.Time { return clock })

	for range 3 {
		if _, err := r.Complete(context.Background(), api.Request{Input: "x"}); err != nil {
			t.Fatalf("Complete: %v", err)
		}
	}
	clock = clock.Add(24 * time.Hour)
	if _, err := r.Complete(context.Background(), api.Request{Input: "x"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if first.calls != 1 {
		t.Errorf("dead route called %d times, a bad credential does not heal with time", first.calls)
	}
}

func TestRouterEveryRouteFailed(t *testing.T) {
	clock := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	down := errors.New("dial tcp: connection refused")
	r := routerFor(t,
		[]api.Completer{&scripted{replies: []reply{{err: down}}}, &scripted{replies: []reply{{err: down}}}},
		func() time.Time { return clock })

	_, err := r.Complete(context.Background(), api.Request{Input: "x"})
	if err == nil {
		t.Fatal("Complete must fail when nothing answers")
	}
	if !errors.Is(err, down) {
		t.Errorf("error = %v, want the last transport error wrapped", err)
	}
	// Both are on the transport cooldown now, so there is nothing to try.
	if _, err := r.Complete(context.Background(), api.Request{Input: "x"}); err == nil {
		t.Fatal("Complete must fail while every route is cooling")
	}
}

func TestMeterAccountsPerUnitOfWork(t *testing.T) {
	clock := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	first := &scripted{replies: []reply{{text: "a", usage: api.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000, TotalTokens: 2_000_000}}}}
	second := &scripted{replies: []reply{{text: "b"}}}
	r := routerFor(t, []api.Completer{first, second}, func() time.Time { return clock })

	m := r.Meter()
	if _, err := m.Complete(context.Background(), api.Request{Input: "x"}); err != nil {
		t.Fatal(err)
	}
	if got := m.Usage().TotalTokens; got != 2_000_000 {
		t.Errorf("tokens = %d", got)
	}
	if got := m.Cost(); !got.Available || got.USD != 3 {
		t.Errorf("cost = %v, want $3 at 1 in and 2 out per million", got)
	}
	if got := m.Routes(); got != "first" {
		t.Errorf("routes = %q", got)
	}

	// A second meter is independent, which is what makes a per job line exact
	// while other workers are spending on the same router.
	fresh := r.Meter()
	if got := fresh.Usage().TotalTokens; got != 0 {
		t.Errorf("a new meter starts at %d tokens", got)
	}
	if got := fresh.Routes(); got != "none" {
		t.Errorf("an unused meter reports %q", got)
	}
}

func TestMeterWithoutRateCard(t *testing.T) {
	m := NewMeter(&scripted{replies: []reply{{text: "a", usage: api.Usage{InputTokens: 10, OutputTokens: 1, TotalTokens: 11}}}}, nil)
	if _, err := m.Complete(context.Background(), api.Request{Input: "x"}); err != nil {
		t.Fatal(err)
	}
	if m.Cost().Available {
		t.Error("no rate card means no cost, not a free campaign")
	}
	if got := m.Routes(); got != "direct" {
		t.Errorf("routes = %q, want the unrouted label", got)
	}
}

func TestDoctorProbesEveryRoute(t *testing.T) {
	routes := []Route{{Name: "first", Model: "m1"}, {Name: "second", Model: "m2"}}
	clients := []api.Completer{
		&scripted{replies: []reply{{text: "pong"}}},
		&scripted{replies: []reply{{err: errors.New("responses API returned 429: rate limit")}}},
	}
	clock := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	probes := Doctor(context.Background(), routes, clients, func() time.Time {
		clock = clock.Add(10 * time.Millisecond)
		return clock
	})
	if len(probes) != 2 {
		t.Fatalf("probes = %d", len(probes))
	}
	if !probes[0].Alive || probes[0].Latency != 10*time.Millisecond {
		t.Errorf("first probe = %+v", probes[0])
	}
	if probes[1].Alive || probes[1].Cause != CauseQuota {
		t.Errorf("second probe = %+v, a quota failure is not a dead endpoint but it is not alive either", probes[1])
	}
}

func TestDefaultPathHonoursEnvironment(t *testing.T) {
	t.Setenv("LUATDO_ROUTES", filepath.Join("somewhere", "routes.json"))
	if got := DefaultPath(); got != filepath.Join("somewhere", "routes.json") {
		t.Errorf("DefaultPath = %q", got)
	}
}
