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
	"errors"
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
	"github.com/tamnd/luatdo/codex"
)

// Route is one named endpoint.
type Route struct {
	Name string `json:"name"`
	// Wire is the request format: chat, responses, or codex. Empty means
	// responses when a url is given and chat otherwise, which is what the
	// files written before this field existed meant.
	Wire Wire `json:"wire,omitempty"`
	// URL is a full endpoint, kept for the files that already name one.
	URL string `json:"url,omitempty"`
	// BaseURL may stop at the server root or end at /v1. WireCodex needs
	// neither, because its address is not a thing anyone configures.
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`
	// Auth is the credential path for WireCodex. Empty means the default,
	// which is the file the Codex CLI login already wrote.
	Auth            string   `json:"auth,omitempty"`
	Effort          string   `json:"effort,omitempty"`
	Rank            int      `json:"rank,omitempty"` // lower is tried first
	APIKeyEnv       string   `json:"api_key_env,omitempty"`
	MaxOutputTokens int      `json:"max_output_tokens,omitempty"`
	Pricing         *Pricing `json:"pricing,omitempty"`
	Disabled        bool     `json:"disabled,omitempty"`
	// Note carries why a route is ranked or disabled where it is. A disabled
	// row with no explanation reads as an oversight the next time someone
	// opens the file.
	Note       string `json:"note,omitempty"`
	MaxRetries int    `json:"max_retries,omitempty"`
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
	seen := map[string]bool{}
	for _, r := range f.Routes {
		if err := r.Validate(); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if seen[r.Name] {
			return nil, fmt.Errorf("parse %s: route %s is listed twice", path, r.Name)
		}
		seen[r.Name] = true
		// A disabled route is validated and then skipped. Validating it anyway
		// means a typo in a row someone turned off gets found now rather than
		// on the day they turn it back on.
		if r.Disabled {
			continue
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
//
// The transport cooldown doubles with each consecutive failure up to the
// ceiling, and resets the moment the route answers. A flat delay treats the
// fiftieth failure of an endpoint that went away this morning exactly like the
// first, which over an hours long campaign means retrying a dead host hundreds
// of times and cluttering every report with it.
const (
	QuotaCooldown        = 5 * time.Minute
	TransportCooldown    = 30 * time.Second
	MaxTransportCooldown = 30 * time.Minute
)

// Router is a Completer that tries routes in rank order, skipping the ones on
// cooldown, and records what each call cost and who served it.
type Router struct {
	mu        sync.Mutex
	routes    []Route
	clients   []api.Completer
	coolUntil []time.Time
	strikes   []int
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
// from its api_key_env, so one file can mix a subscription, a free tier and a
// metered endpoint without sharing a key between them.
//
// A route whose transport cannot be built at all is kept in the list and marked
// dead. Dropping it would renumber everything after it and make the report say
// a route was never tried when what happened is that it was misconfigured.
func New(routes []Route) *Router {
	r := &Router{
		routes:    routes,
		coolUntil: make([]time.Time, len(routes)),
		strikes:   make([]int, len(routes)),
		dead:      make([]bool, len(routes)),
		stats:     map[string]*Stat{},
		now:       time.Now,
	}
	for i, rt := range routes {
		retries := rt.MaxRetries
		if retries == 0 {
			retries = 2
		}
		client, err := rt.Client(0, retries, nil)
		if err != nil {
			client = broken{err}
			r.dead[i] = true
		}
		r.clients = append(r.clients, client)
		r.stats[rt.Name] = &Stat{Route: rt.Name, LastError: errorText(err)}
	}
	return r
}

// broken stands in for a route that could not be built, so the slices stay
// aligned and the reason survives to the report.
type broken struct{ err error }

func (b broken) Complete(context.Context, api.Request) (api.Response, error) {
	return api.Response{}, b.err
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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
		call.Model = rt.ModelName()
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
	r.strikes[i] = 0
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
	now := r.now()
	switch Cause(err) {
	case CauseAuth:
		// Retrying a rejected credential is pointless until a person fixes it,
		// and doing so for the rest of a six hour campaign is pointless a few
		// thousand times.
		r.dead[i] = true
	case CauseModel:
		// The endpoint is fine and does not serve this model. No amount of
		// waiting changes that.
		r.dead[i] = true
	case CauseQuota:
		r.coolUntil[i] = now.Add(quotaCooldown(err, now))
	default:
		r.strikes[i]++
		r.coolUntil[i] = now.Add(transportCooldown(r.strikes[i]))
	}
}

// quotaCooldown prefers the reset the provider stated over a guess. A Codex
// plan window can be days wide and the backend says exactly when it reopens, so
// sleeping the five minute default against it would mean waking up to the same
// wall a few hundred times.
func quotaCooldown(err error, now time.Time) time.Duration {
	if quota, ok := errors.AsType[*codex.QuotaError](err); ok {
		if wait := quota.RetryAfter(now); wait > 0 {
			return wait
		}
	}
	return QuotaCooldown
}

func transportCooldown(strikes int) time.Duration {
	wait := TransportCooldown << min(max(0, strikes-1), 16)
	return min(wait, MaxTransportCooldown)
}

// Failure causes, matched from the transport error text because that is all a
// compatible proxy reliably gives back.
const (
	CauseAuth      = "auth"
	CauseQuota     = "quota"
	CauseModel     = "model"
	CauseTransport = "transport"
)

// Cause classifies a completion error.
//
// The order matters. A Codex quota error carries a 429 and the words usage
// limit, and it is a quota answer rather than a rate limit one, so the typed
// error is consulted before any string matching happens.
func Cause(err error) string {
	if err == nil {
		return ""
	}
	if _, ok := errors.AsType[*codex.QuotaError](err); ok {
		return CauseQuota
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "401") || strings.Contains(text, "403") ||
		strings.Contains(text, "unauthorized") || strings.Contains(text, "invalid api key"):
		return CauseAuth
	case strings.Contains(text, "429") || strings.Contains(text, "quota") ||
		strings.Contains(text, "rate limit") || strings.Contains(text, "insufficient_quota"):
		return CauseQuota
	// A model the endpoint does not serve is the one failure a free tier
	// produces that looks transient and is not. The catalogue rotates, a slug
	// that worked last week stops existing, and the route has to leave the run
	// rather than come back every thirty seconds to be told the same thing.
	case strings.Contains(text, "model_not_found") || strings.Contains(text, "unknown model") ||
		strings.Contains(text, "does not exist") || strings.Contains(text, "is not supported"):
		return CauseModel
	default:
		return CauseTransport
	}
}

// Stats returns the per-route accounting in rank order.
//
// A route that was never called is left out, with one exception: a route whose
// transport could not be built at all is reported anyway, because it carries
// the reason and silence about it reads as a route nobody needed.
func (r *Router) Stats() []Stat {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Stat
	for _, rt := range r.routes {
		if s := r.stats[rt.Name]; s != nil && (s.Calls > 0 || s.Failures > 0 || s.LastError != "") {
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
		if s.Calls == 0 {
			// A route that spent nothing cannot make the total unknown.
			continue
		}
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
	Wire    Wire          `json:"wire,omitempty"`
	Model   string        `json:"model"`
	Alive   bool          `json:"alive"`
	Latency time.Duration `json:"latency"`
	Error   string        `json:"error,omitempty"`
	Cause   string        `json:"cause,omitempty"`
	// Limits is the plan state a codex route reports on the way past. There is
	// no usage endpoint anywhere, so a probe is the only chance to read it, and
	// a plan at 97 percent is worth knowing before a campaign starts rather
	// than an hour into one.
	Limits *codex.Limits `json:"limits,omitempty"`
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
			Model: rt.ModelName(),
			Input: "ping",
		})
		p := Probe{Route: rt.Name, Wire: rt.wire(), Model: rt.ModelName(), Latency: now().Sub(start)}
		if err != nil {
			p.Error = err.Error()
			p.Cause = Cause(err)
		} else {
			p.Alive = true
		}
		if client, ok := clients[i].(interface{ Limits() codex.Limits }); ok {
			limits := client.Limits()
			if !limits.ReadAt.IsZero() {
				p.Limits = &limits
			}
		}
		out = append(out, p)
	}
	return out
}

// Clients returns one transport per route, for doctor and other callers that
// need to address routes individually. No retries: a probe that retries is
// measuring patience rather than health.
func Clients(routes []Route, httpClient *http.Client) []api.Completer {
	out := make([]api.Completer, 0, len(routes))
	for _, rt := range routes {
		client, err := rt.Client(0, 0, httpClient)
		if err != nil {
			client = broken{err}
		}
		out = append(out, client)
	}
	return out
}
