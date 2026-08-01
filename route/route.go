// Package route turns a file of named endpoints into one Completer with
// failover, cooldowns, and cost accounting.
//
// A campaign that runs for hours against several endpoints needs to survive
// one of them rate limiting, expiring, or going down, without losing the run.
// Failover is per call and cause matched: a quota error cools the route for
// long enough to matter, a transport blip does not, and an authentication
// failure disables the route for the process because retrying it is pointless.
// Every call records which route served it, so a corpus assembled from three
// endpoints can still say where each statement came from.
package route

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tamnd/luatdo/api"
)

// Route is one named endpoint.
type Route struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Model      string   `json:"model"`
	Effort     string   `json:"effort,omitempty"`
	Rank       int      `json:"rank,omitempty"` // lower is tried first
	APIKeyEnv  string   `json:"api_key_env,omitempty"`
	Pricing    *Pricing `json:"pricing,omitempty"`
	Disabled   bool     `json:"disabled,omitempty"`
	Note       string   `json:"note,omitempty"`
	MaxRetries int      `json:"max_retries,omitempty"`
}

// Pricing is a rate card in US dollars per million tokens. A route without one
// reports its cost as unavailable, which is the honest answer; an invented
// zero would quietly understate a campaign.
type Pricing struct {
	InputPerM       float64 `json:"input_per_m"`
	CachedInputPerM float64 `json:"cached_input_per_m"`
	CacheWritePerM  float64 `json:"cache_write_per_m"`
	OutputPerM      float64 `json:"output_per_m"`
}

// Cost is the list price of some usage under one rate card.
type Cost struct {
	USD       float64 `json:"usd"`
	Available bool    `json:"available"`
}

func (c Cost) String() string {
	if !c.Available {
		return "unavailable"
	}
	return fmt.Sprintf("$%.4f", c.USD)
}

// Add sums two costs. Unavailable is contagious: a total that silently drops
// the unpriced half of a campaign would be worse than no total at all.
func (c Cost) Add(other Cost) Cost {
	if !c.Available || !other.Available {
		return Cost{USD: c.USD + other.USD, Available: false}
	}
	return Cost{USD: c.USD + other.USD, Available: true}
}

// Estimate applies the rate card to usage.
func (p *Pricing) Estimate(u api.Usage) Cost {
	if p == nil {
		return Cost{}
	}
	u = u.Normalized()
	const perM = 1_000_000.0
	usd := float64(u.UncachedInputTokens())/perM*p.InputPerM +
		float64(u.CachedInputTokens)/perM*p.CachedInputPerM +
		float64(u.CacheWriteTokens)/perM*p.CacheWritePerM +
		float64(u.OutputTokens)/perM*p.OutputPerM
	return Cost{USD: usd, Available: true}
}

// File is the routes file: ~/.config/luatdo/routes.json by default.
type File struct {
	Routes []Route `json:"routes"`
}

// DefaultPath resolves LUATDO_ROUTES or ~/.config/luatdo/routes.json.
func DefaultPath() string {
	if p := os.Getenv("LUATDO_ROUTES"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "routes.json"
	}
	return filepath.Join(home, ".config", "luatdo", "routes.json")
}

// Load reads a routes file and returns its enabled routes in rank order.
func Load(path string) ([]Route, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var out []Route
	for _, r := range f.Routes {
		if r.Disabled {
			continue
		}
		if r.Name == "" || r.URL == "" || r.Model == "" {
			return nil, fmt.Errorf("parse %s: route needs a name, a url, and a model", path)
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s has no enabled routes", path)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
	return out, nil
}

// Cooldowns per failure cause. A route that hit a quota is useless for a
// while; a route that returned one bad gateway probably is not.
const (
	QuotaCooldown     = 5 * time.Minute
	TransportCooldown = 30 * time.Second
)

// Router is a Completer that tries routes in rank order, skipping the ones on
// cooldown, and records what each call cost and who served it.
type Router struct {
	mu        sync.Mutex
	routes    []Route
	clients   []api.Completer
	coolUntil []time.Time
	dead      []bool
	stats     map[string]*Stat
	now       func() time.Time
}

// Stat is the per-route accounting a campaign reports at the end.
type Stat struct {
	Route     string    `json:"route"`
	Calls     int       `json:"calls"`
	Failures  int       `json:"failures"`
	Usage     api.Usage `json:"usage"`
	Cost      Cost      `json:"cost"`
	LastError string    `json:"last_error,omitempty"`
}

// New builds a router over the given routes. Each route's credential comes
// from its api_key_env, so one file can mix a subscription endpoint and a
// metered one without sharing a key.
func New(routes []Route) *Router {
	r := &Router{
		routes:    routes,
		coolUntil: make([]time.Time, len(routes)),
		dead:      make([]bool, len(routes)),
		stats:     map[string]*Stat{},
		now:       time.Now,
	}
	for _, rt := range routes {
		retries := rt.MaxRetries
		if retries == 0 {
			retries = 2
		}
		r.clients = append(r.clients, &api.Client{
			URL:        rt.URL,
			APIKey:     os.Getenv(rt.APIKeyEnv),
			MaxRetries: retries,
		})
		r.stats[rt.Name] = &Stat{Route: rt.Name}
	}
	return r
}

// WithCompleters replaces the transports, which is how tests drive a router
// without a network.
func (r *Router) WithCompleters(clients []api.Completer) *Router {
	r.clients = clients
	return r
}

// Complete tries every live route in order and returns the first success. The
// request model and effort come from the route, so callers name the work and
// the file names the endpoint.
func (r *Router) Complete(ctx context.Context, request api.Request) (api.Response, error) {
	var attempted []string
	var last error
	for i := range r.routes {
		if !r.available(i) {
			continue
		}
		rt := r.routes[i]
		attempted = append(attempted, rt.Name)
		call := request
		call.Model = rt.Model
		if call.Effort == "" {
			call.Effort = rt.Effort
		}
		resp, err := r.clients[i].Complete(ctx, call)
		if err != nil {
			r.fail(i, err)
			last = err
			if ctx.Err() != nil {
				return api.Response{}, ctx.Err()
			}
			continue
		}
		resp.Route = rt.Name
		r.succeed(i, resp.Usage)
		return resp, nil
	}
	if last == nil {
		return api.Response{}, fmt.Errorf("no route available, all %d are on cooldown or disabled", len(r.routes))
	}
	return api.Response{}, fmt.Errorf("every route failed (%s): %w", strings.Join(attempted, ", "), last)
}

func (r *Router) available(i int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.dead[i] && !r.now().Before(r.coolUntil[i])
}

func (r *Router) succeed(i int, u api.Usage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.coolUntil[i] = time.Time{}
	s := r.stats[r.routes[i].Name]
	s.Calls++
	s.Usage = AddUsage(s.Usage, u)
	s.Cost = s.Cost.Add(r.routes[i].Pricing.Estimate(u))
}

func (r *Router) fail(i int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stats[r.routes[i].Name]
	s.Failures++
	s.LastError = err.Error()
	switch Cause(err) {
	case CauseAuth:
		r.dead[i] = true
	case CauseQuota:
		r.coolUntil[i] = r.now().Add(QuotaCooldown)
	default:
		r.coolUntil[i] = r.now().Add(TransportCooldown)
	}
}

// Failure causes, matched from the transport error text because that is all a
// compatible proxy reliably gives back.
const (
	CauseAuth      = "auth"
	CauseQuota     = "quota"
	CauseTransport = "transport"
)

// Cause classifies a completion error.
func Cause(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "401") || strings.Contains(text, "403") ||
		strings.Contains(text, "unauthorized") || strings.Contains(text, "invalid api key"):
		return CauseAuth
	case strings.Contains(text, "429") || strings.Contains(text, "quota") ||
		strings.Contains(text, "rate limit") || strings.Contains(text, "insufficient_quota"):
		return CauseQuota
	default:
		return CauseTransport
	}
}

// Stats returns the per-route accounting in rank order.
func (r *Router) Stats() []Stat {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Stat
	for _, rt := range r.routes {
		if s := r.stats[rt.Name]; s != nil && (s.Calls > 0 || s.Failures > 0) {
			out = append(out, *s)
		}
	}
	return out
}

// Totals sums usage and cost across routes.
func (r *Router) Totals() (api.Usage, Cost) {
	var usage api.Usage
	cost := Cost{Available: true}
	for _, s := range r.Stats() {
		usage = AddUsage(usage, s.Usage)
		cost = cost.Add(s.Cost)
	}
	return usage, cost
}

// AddUsage sums two usage records.
func AddUsage(a, b api.Usage) api.Usage {
	a.InputTokens += b.InputTokens
	a.CachedInputTokens += b.CachedInputTokens
	a.CacheWriteTokens += b.CacheWriteTokens
	a.OutputTokens += b.OutputTokens
	a.ReasoningTokens += b.ReasoningTokens
	a.TotalTokens += b.TotalTokens
	return a
}

// Meter wraps a Completer and accounts one unit of work: the tokens it spent,
// what that costs at list price, and which routes served it. A campaign gives
// each job its own meter, so the per job reporting line is exact rather than a
// difference of two shared counters that other workers moved in between.
type Meter struct {
	mu     sync.Mutex
	inner  api.Completer
	price  func(routeName string) *Pricing
	usage  api.Usage
	cost   Cost
	routes []string
}

// Meter returns a metered view of the router.
func (r *Router) Meter() *Meter {
	return &Meter{
		inner: r,
		price: func(name string) *Pricing {
			for i := range r.routes {
				if r.routes[i].Name == name {
					return r.routes[i].Pricing
				}
			}
			return nil
		},
		cost: Cost{Available: true},
	}
}

// NewMeter meters a plain completer that has no route file behind it. A nil
// rate card is honest about not knowing the price rather than reporting zero.
func NewMeter(inner api.Completer, pricing *Pricing) *Meter {
	return &Meter{
		inner: inner,
		price: func(string) *Pricing { return pricing },
		cost:  Cost{Available: true},
	}
}

func (m *Meter) Complete(ctx context.Context, request api.Request) (api.Response, error) {
	resp, err := m.inner.Complete(ctx, request)
	if err != nil {
		return resp, err
	}
	name := resp.Route
	if name == "" {
		name = "direct"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage = AddUsage(m.usage, resp.Usage)
	m.cost = m.cost.Add(m.price(resp.Route).Estimate(resp.Usage))
	if !slices.Contains(m.routes, name) {
		m.routes = append(m.routes, name)
	}
	return resp, nil
}

// Usage returns the tokens spent so far.
func (m *Meter) Usage() api.Usage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage
}

// Cost returns the list price of the tokens spent so far.
func (m *Meter) Cost() Cost {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cost
}

// Routes names the endpoints that served this unit of work, in first use
// order. More than one name means the unit failed over partway through.
func (m *Meter) Routes() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.routes) == 0 {
		return "none"
	}
	return strings.Join(m.routes, "+")
}

// Probe is one route's health as reported by doctor.
type Probe struct {
	Route   string        `json:"route"`
	Model   string        `json:"model"`
	Alive   bool          `json:"alive"`
	Latency time.Duration `json:"latency"`
	Error   string        `json:"error,omitempty"`
	Cause   string        `json:"cause,omitempty"`
}

// Doctor probes every route with a trivial completion and reports which are
// alive. Probes run sequentially so that a shared quota is not spent proving
// that a shared quota is exhausted.
func Doctor(ctx context.Context, routes []Route, clients []api.Completer, now func() time.Time) []Probe {
	if now == nil {
		now = time.Now
	}
	var out []Probe
	for i, rt := range routes {
		start := now()
		_, err := clients[i].Complete(ctx, api.Request{
			Model: rt.Model,
			Input: "ping",
		})
		p := Probe{Route: rt.Name, Model: rt.Model, Latency: now().Sub(start)}
		if err != nil {
			p.Error = err.Error()
			p.Cause = Cause(err)
		} else {
			p.Alive = true
		}
		out = append(out, p)
	}
	return out
}

// Clients returns one transport per route, for doctor and other callers that
// need to address routes individually.
func Clients(routes []Route, httpClient *http.Client) []api.Completer {
	var out []api.Completer
	for _, rt := range routes {
		out = append(out, &api.Client{
			URL:        rt.URL,
			APIKey:     os.Getenv(rt.APIKeyEnv),
			HTTPClient: httpClient,
			MaxRetries: 0,
		})
	}
	return out
}
