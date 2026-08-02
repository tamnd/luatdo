package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/campaign"
	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/graph"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/route"
	"github.com/tamnd/luatdo/store"
)

func init() {
	commands = append(commands,
		command{"doctor", "check the data directory, the routes, and the graph target", cmdDoctor},
		command{"run", "work the coverage queue with parallel workers", cmdRun},
	)
}

// engine is the model access one command was given: a router over a routes
// file when there is one, or the single endpoint from the environment.
type engine struct {
	completer api.Completer
	model     string
	pricing   *route.Pricing
	routes    []route.Route
	source    string
}

// openEngine prefers the routes file, because a campaign that can fail over is
// worth more than one that cannot, and falls back to the environment so a
// single endpoint still needs no configuration file.
func openEngine() (*engine, error) {
	path := route.DefaultPath()
	routes, err := route.Load(path)
	if err == nil {
		router := route.New(routes)
		return &engine{completer: router, model: routes[0].ModelName(), routes: routes, source: path}, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	completer, model, cerr := completerFromEnv()
	if cerr != nil {
		return nil, fmt.Errorf("no routes file at %s and no endpoint in the environment: %w", path, cerr)
	}
	return &engine{completer: completer, model: model, source: "environment"}, nil
}

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	suggest := fs.Bool("suggest-routes", false, "ask the endpoints what they serve and print a routes file")
	write := fs.Bool("write-routes", false, "write the suggested routes file, refusing to overwrite one")
	skipProbe := fs.Bool("no-probe", false, "check configuration without calling any endpoint")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *suggest || *write {
		return suggestRoutes(*write)
	}

	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	fmt.Printf("data       %s\n", s.Root)
	report, err := coverage.Compute(s)
	if err != nil {
		return err
	}
	fmt.Printf("documents  %d parsed, %d quarantined\n", report.Parsed, report.Quarantined)
	fmt.Printf("norms      %d of %d units extracted\n", report.Extracted, report.Extractable)
	if reg, err := ontology.Load(s.Ontology()); err != nil {
		fmt.Printf("ontology   missing, run luatdo ontology init\n")
	} else {
		frozen := "unfrozen"
		if reg.Frozen() {
			frozen = "frozen at " + reg.FrozenAt
		}
		fmt.Printf("ontology   v%d, %d classes, %d predicates, %s\n", reg.Version, len(reg.Classes), len(reg.Predicates), frozen)
	}

	eng, err := openEngine()
	if err != nil {
		fmt.Printf("routes     %v\n", err)
		return fmt.Errorf("no model access configured, run luatdo doctor --suggest-routes")
	}
	fmt.Printf("routes     %s\n", eng.source)
	if len(eng.routes) == 0 {
		fmt.Printf("  %-14s %s\n", "environment", eng.model)
	}
	for _, r := range eng.routes {
		fmt.Printf("  %-14s %-24s rank %d\n", r.Name, r.ModelName(), r.Rank)
	}
	if *skipProbe {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	probes := probeEngine(ctx, eng)
	alive := 0
	for _, p := range probes {
		if p.Alive {
			alive++
			fmt.Printf("  %-14s alive in %s\n", p.Route, p.Latency.Round(time.Millisecond))
		} else {
			fmt.Printf("  %-14s %s: %s\n", p.Route, p.Cause, p.Error)
		}
		// The plan windows ride along on the response headers and exist
		// nowhere else, so a probe is the only chance to read them. They are
		// printed on a failed probe too, because a plan out of quota is
		// exactly when the numbers matter.
		if p.Limits != nil {
			if p.Limits.Primary != nil {
				fmt.Printf("  %-14s   plan %s, primary %s\n", "", p.Limits.PlanType, p.Limits.Primary)
			}
			if p.Limits.Secondary != nil {
				fmt.Printf("  %-14s   secondary %s\n", "", p.Limits.Secondary)
			}
		}
	}

	target := graph.TargetFromEnv()
	if counts, err := graph.Live(ctx, target); err != nil {
		fmt.Printf("neo4j      %s unreachable: %v\n", target.URI, err)
	} else {
		fmt.Printf("neo4j      %s holds %d documents, %d provisions, %d norms\n",
			target.URI, counts["documents"], counts["provisions"], counts["norms"])
	}

	if alive == 0 {
		return fmt.Errorf("no route is alive, nothing can run")
	}
	fmt.Printf("ready      %d of %d routes alive\n", alive, len(probes))
	return nil
}

// suggestRoutes asks the endpoints what they serve and prints the routes file
// that follows from the answer.
//
// It starts from the file already on disk when there is one, so the ranks
// someone measured survive, and from the starter list when there is not. The
// free tier catalogue rotates without telling anyone, and a route whose model
// was withdrawn last week fails in a way that reads like a network problem, so
// this exists to make the drift visible in one request per endpoint.
func suggestRoutes(write bool) error {
	path := route.DefaultPath()
	routes, err := route.Load(path)
	source := path
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		routes, source = route.Starter(), "the starter list"
	}
	fmt.Fprintf(os.Stderr, "probing %d routes from %s\n", len(routes), source)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	catalogues := route.Catalogues(ctx, routes, nil)
	for _, r := range routes {
		if models, ok := catalogues[r.Name]; ok {
			fmt.Fprintf(os.Stderr, "  %-14s serves %d models\n", r.Name, len(models))
		} else {
			fmt.Fprintf(os.Stderr, "  %-14s published no catalogue, its configured model is taken on trust\n", r.Name)
		}
	}
	for _, line := range route.Drift(routes, catalogues) {
		fmt.Fprintf(os.Stderr, "  drift  %s\n", line)
	}

	suggested := route.Suggest(routes, catalogues)
	if !write {
		fmt.Fprintf(os.Stderr, "\nwrite this to %s\n\n", path)
		raw, err := json.MarshalIndent(route.File{Routes: suggested}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", raw)
		return nil
	}
	if err := route.Write(path, suggested); err != nil {
		return fmt.Errorf("%w, edit it by hand rather than losing the ranks in it", err)
	}
	fmt.Printf("wrote %s with %d routes\n", path, len(suggested))
	return nil
}

func probeEngine(ctx context.Context, eng *engine) []route.Probe {
	if len(eng.routes) == 0 {
		return route.Doctor(ctx,
			[]route.Route{{Name: "environment", Model: eng.model}},
			[]api.Completer{eng.completer}, nil)
	}
	return route.Doctor(ctx, eng.routes, route.Clients(eng.routes, nil), nil)
}

// reportRoutes prints what each route actually did. Every pass that spends
// money prints it, because a campaign that cannot say which endpoint answered
// and what it cost is a campaign nobody can budget for.
func reportRoutes(eng *engine) {
	if len(eng.routes) == 0 {
		return
	}
	router, ok := eng.completer.(*route.Router)
	if !ok {
		return
	}
	for _, st := range router.Stats() {
		fmt.Printf("route %-14s %d calls, %d failures, %d tokens, cost %s\n",
			st.Route, st.Calls, st.Failures, st.Usage.TotalTokens, st.Cost)
	}
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	parallel := fs.String("parallel", "auto", "worker count, or auto")
	limit := fs.Int("limit", 0, "stop after this many provisions, 0 for the whole queue")
	mode := fs.String("mode", "fast", "fast or slow")
	population := fs.Int("population", 3, "independent candidates in slow mode")
	corrections := fs.Int("max-corrections", 2, "bounded retries on invalid model output")
	dryRun := fs.Bool("dry-run", false, "print the queue and the plan, call no model")
	scope := fs.String("campaign", "", "restrict the queue to a named campaign, see luatdo campaign list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	tasks, err := coverage.Queue(s)
	if err != nil {
		return err
	}
	if *scope != "" {
		sc, err := campaign.LookupScope(*scope)
		if err != nil {
			return err
		}
		_, inScope, err := campaignDocs(s, sc)
		if err != nil {
			return err
		}
		before := len(tasks)
		tasks = campaign.InScope(tasks, inScope)
		fmt.Printf("campaign %s: %d documents in scope, %d of %d queued provisions kept\n",
			sc.Name, len(inScope), len(tasks), before)
	}
	if *limit > 0 && *limit < len(tasks) {
		tasks = tasks[:*limit]
	}
	workers, err := parallelism(*parallel)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Println("run: queue empty, nothing to extract")
		return nil
	}
	if *dryRun {
		fmt.Printf("run: %d provisions queued, %d workers, mode %s\n", len(tasks), workers, *mode)
		for i, t := range tasks[:min(10, len(tasks))] {
			fmt.Printf("  %2d %-12s %s\n", i+1, t.DocType, t.ProvisionID)
		}
		if len(tasks) > 10 {
			fmt.Printf("  ... %d more\n", len(tasks)-10)
		}
		return nil
	}

	reg, err := ontology.Load(s.Ontology())
	if err != nil {
		return err
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		// Hand the signal back to the runtime so a second one kills the
		// process. The first one only means stop starting new work.
		stop()
		fmt.Fprintln(os.Stderr, "draining, finishing the provisions already in flight, signal again to abort")
	}()

	runner := &campaign.Runner{
		Store: s, Registry: reg, Completer: eng.completer, Pricing: eng.pricing,
		Model: eng.model, Mode: *mode, Population: *population,
		MaxCorrections: *corrections, Workers: workers,
		Report: func(res campaign.Result) { fmt.Println(res) },
	}
	fmt.Printf("run: %d provisions queued, %d workers, mode %s, routes %s\n", len(tasks), workers, *mode, eng.source)
	summary, err := runner.Run(ctx, tasks)
	if err != nil {
		return err
	}
	fmt.Println(summary)
	reportRoutes(eng)
	stamp := summary.StartedAt.Format("20060102-150405")
	path := filepath.Join(s.Campaign(), stamp+".json")
	if err := store.WriteJSON(path, summary); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	if summary.Failed > 0 {
		return fmt.Errorf("%d provisions failed and stay in the queue", summary.Failed)
	}
	return nil
}

// parallelism resolves the worker count. Workers wait on a remote service
// rather than on the processor, so auto is not the core count; it is a small
// number that keeps a few requests in flight without turning one machine into
// the reason a shared quota runs out.
func parallelism(value string) (int, error) {
	if value == "" || value == "auto" {
		if env := os.Getenv("LUATDO_PARALLEL"); env != "" {
			value = env
		} else {
			return min(4, max(2, runtime.NumCPU()/2)), nil
		}
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("parallel %q is not a worker count", value)
	}
	return n, nil
}
