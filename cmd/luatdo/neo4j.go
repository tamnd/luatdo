package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tamnd/luatdo/deploy"
	"github.com/tamnd/luatdo/store"
)

func init() {
	commands = append(commands,
		command{"neo4j", "run a local Neo4j over the graph, on any platform", cmdNeo4j},
	)
}

func cmdNeo4j(args []string) error {
	fs := flag.NewFlagSet("neo4j", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	c := deploy.Default()
	fs.StringVar(&c.Image, "image", c.Image, "container image")
	fs.StringVar(&c.Password, "password", c.Password, "database password")
	fs.StringVar(&c.Database, "database", c.Database, "database name, which has to match the one the export baked in")
	fs.IntVar(&c.HTTPPort, "http-port", c.HTTPPort, "port for the browser")
	fs.IntVar(&c.BoltPort, "bolt-port", c.BoltPort, "port for Bolt")
	fs.StringVar(&c.Heap, "heap", c.Heap, "java heap")
	fs.StringVar(&c.PageCache, "pagecache", c.PageCache, "page cache")
	runtimeName := fs.String("runtime", "", "container runtime, podman or docker, default whichever is on PATH")
	url := fs.String("url", "", "dataset to fetch, default the published one")
	sha := fs.String("sha256", "", "checksum of the dataset named by -url")
	yes := fs.Bool("yes", false, "do not ask before destroying anything")
	sub, _, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	c.Export = filepath.Join(s.Export(), "neo4j")

	data := deploy.PublishedDataset()
	if *url != "" {
		data = deploy.Dataset{URL: *url, SHA256: *sha}
	}

	// fetch is the one subcommand that needs no container runtime, so it is
	// answered before the machine is asked for one. A person on a host with
	// nothing installed can still download the corpus and read the CSVs.
	if sub == "fetch" {
		return neo4jFetch(data, s)
	}

	rt, err := deploy.FindRuntime(*runtimeName)
	if err != nil {
		return fmt.Errorf("%w, install one of them and run this again", err)
	}

	switch sub {
	case "install":
		return neo4jInstall(rt, c, data, s)
	case "pull":
		return rt.Run(c.Pull(), os.Stdout)
	case "load":
		return neo4jLoad(rt, c)
	case "up":
		return neo4jUp(rt, c)
	case "down":
		return rt.Run(c.Down(), os.Stdout)
	case "status":
		return neo4jStatus(rt, c)
	case "logs":
		return rt.Run(c.Logs(), os.Stdout)
	case "wipe":
		if !*yes && !confirm("this removes volume "+c.Volume+" and the graph in it") {
			return fmt.Errorf("nothing was removed")
		}
		return rt.Run(c.Wipe(), os.Stdout)
	default:
		return fmt.Errorf("usage: luatdo neo4j install|fetch|pull|load|up|down|status|logs|wipe")
	}
}

// neo4jInstall is the whole of it in one command: get the graph, load it, and
// start a server over it.
//
// The steps are all available separately, and this exists because a person
// meeting the project for the first time should not have to know that an
// offline import is a separate stage from starting a server. They should get a
// URL and a query to paste.
func neo4jInstall(rt deploy.Runtime, c deploy.Config, data deploy.Dataset, s *store.Store) error {
	if !deploy.HasExport(c.Export) {
		if err := neo4jFetch(data, s); err != nil {
			return err
		}
	} else {
		fmt.Printf("using the export already in %s, delete it to fetch the published one instead\n", c.Export)
	}
	if err := rt.Run(c.Pull(), os.Stdout); err != nil {
		return err
	}
	if err := neo4jLoad(rt, c); err != nil {
		return err
	}
	return neo4jUp(rt, c)
}

func neo4jFetch(data deploy.Dataset, s *store.Store) error {
	fmt.Printf("fetching %s\n", data.URL)
	err := deploy.Fetch(context.Background(), data, s.Export(), func(done, total int64) {
		if total > 0 {
			fmt.Printf("\r  %s of %s  %3.0f%%", megabytes(done), megabytes(total), 100*float64(done)/float64(total))
			return
		}
		fmt.Printf("\r  %s", megabytes(done))
	})
	fmt.Println()
	if err != nil {
		return err
	}
	fmt.Printf("unpacked into %s\n", filepath.Join(s.Export(), "neo4j"))
	return nil
}

func neo4jLoad(rt deploy.Runtime, c deploy.Config) error {
	if !deploy.HasExport(c.Export) {
		return fmt.Errorf("no export in %s, run luatdo neo4j fetch to download the published graph or luatdo export neo4j to project your own", c.Export)
	}
	// The importer refuses to run against a live database, and its refusal is
	// less clear than this one.
	if rt.IsRunning(c) {
		return fmt.Errorf("%s is running, stop it with luatdo neo4j down before loading", c.Container)
	}
	return rt.Run(c.Load(), os.Stdout)
}

func neo4jUp(rt deploy.Runtime, c deploy.Config) error {
	if rt.IsRunning(c) {
		fmt.Printf("%s is already up\n", c.Container)
	} else if err := rt.Run(c.Up(), os.Stdout); err != nil {
		return err
	}
	// Waiting rather than reporting success at the fork. On a graph this size
	// the server is minutes from answering when the container is already up,
	// and a person told to open a browser at that moment gets an error page and
	// concludes the install failed.
	fmt.Print("waiting for the database to accept queries")
	deadline := time.Now().Add(10 * time.Minute)
	for !rt.Ready(c) {
		if time.Now().After(deadline) {
			fmt.Println()
			return fmt.Errorf("%s did not accept a query within ten minutes, look at luatdo neo4j logs", c.Container)
		}
		fmt.Print(".")
		time.Sleep(3 * time.Second)
	}
	fmt.Println()
	fmt.Printf("%s is up at %s, user neo4j, password %s, database %s\n", c.Container, c.Browser(), c.Password, c.Database)
	fmt.Println("point the rest of the tool at it with:")
	for _, line := range envLines(c) {
		fmt.Println(line)
	}
	return nil
}

// envLines is how a person puts those four variables into their shell.
//
// It is not the same sentence everywhere, and printing the wrong one is worse
// than printing nothing: somebody pastes it, gets no error on Windows because
// set and $env: both look like assignments to a reader, and then every command
// after it cannot reach the database.
//
// Windows gets the PowerShell form, because that is the shell the installer
// tells people to use and the one Windows Terminal opens by default, with the
// cmd.exe form named underneath rather than guessed at. PSModulePath is set in
// cmd.exe too, so there is nothing in the environment that reliably tells the
// two apart, and a wrong guess is worse than a short note.
func envLines(c deploy.Config) []string {
	var lines []string
	if runtime.GOOS == "windows" {
		for _, kv := range c.Env() {
			lines = append(lines, fmt.Sprintf("  $env:%s = \"%s\"", kv[0], kv[1]))
		}
		return append(lines, "  in cmd.exe write these as set NAME=value instead")
	}
	for _, kv := range c.Env() {
		lines = append(lines, fmt.Sprintf("  export %s=%s", kv[0], kv[1]))
	}
	return lines
}

func neo4jStatus(rt deploy.Runtime, c deploy.Config) error {
	fmt.Printf("runtime %s at %s\n", rt.Name, rt.Path)
	if deploy.HasExport(c.Export) {
		fmt.Printf("export  %s\n", c.Export)
	} else {
		fmt.Printf("export  none in %s\n", c.Export)
	}
	if !rt.IsRunning(c) {
		fmt.Printf("server  %s is not running\n", c.Container)
		return nil
	}
	fmt.Printf("server  %s is up at %s\n", c.Container, c.Browser())
	if !rt.Ready(c) {
		fmt.Println("        it is not accepting queries yet, which on a graph this size is normal for a few minutes after a start")
		return nil
	}
	// Counted rather than assumed. A volume that was wiped and a volume that was
	// never loaded both produce a server that starts perfectly and holds nothing,
	// and the only way to tell that from a working install is to ask it.
	out, err := rt.Capture(c.Cypher("MATCH (n) RETURN count(n) AS nodes")[0])
	if err != nil {
		return err
	}
	fmt.Printf("        %s nodes\n", count(out))
	out, err = rt.Capture(c.Cypher("MATCH ()-[r]->() RETURN count(r) AS rels")[0])
	if err != nil {
		return err
	}
	fmt.Printf("        %s relationships\n", count(out))
	return nil
}

func confirm(what string) bool {
	fmt.Printf("%s. Type yes to continue: ", what)
	var answer string
	_, _ = fmt.Scanln(&answer)
	return strings.TrimSpace(strings.ToLower(answer)) == "yes"
}

func megabytes(n int64) string {
	return fmt.Sprintf("%.0fMB", float64(n)/(1<<20))
}

// count reads one number out of a cypher-shell plain result, which is a header
// line and then the row.
//
// A query that matched nothing prints the header on its own, and taking the last
// line whatever it is would report the graph as holding "nodes" nodes. That is
// the case this has to get right, because a server that is up and empty is
// exactly what status exists to catch.
func count(out string) string {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "an unknown number of"
	}
	last := fields[len(fields)-1]
	for _, r := range last {
		if r < '0' || r > '9' {
			return "an unknown number of"
		}
	}
	return last
}
