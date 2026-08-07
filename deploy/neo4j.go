// Package deploy runs a local Neo4j over a luatdo export.
//
// This was a shell script, and a shell script is half an answer. The project
// has to run on Linux, macOS and Windows, and the honest choices there are one
// implementation per platform or one implementation in the language the tool is
// already written in. Three scripts drift: the first time a flag changes,
// two of them are wrong and nobody notices until somebody on that platform
// tries. So the steps live here, as data, and the command runs them.
//
// The steps are values rather than calls on purpose. What can go wrong with a
// container invocation is the argument list, and an argument list that is built
// and immediately executed can only be checked by running it, which needs a
// container runtime, a network and two minutes. Returned as a slice it can be
// asserted on in a unit test on a machine with no runtime installed at all.
package deploy

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
)

// Config is everything a local Neo4j needs to be brought up over an export.
//
// The database name is here rather than fixed because it has to match the name
// the export baked into import.sh, and the two going out of step produces a
// server that starts cleanly and holds nothing.
type Config struct {
	Image     string
	Password  string
	Database  string
	Container string
	Volume    string
	HTTPPort  int
	BoltPort  int
	Heap      string
	PageCache string
	// Export is the directory holding import.sh and the CSV files. It is mounted
	// writable, because the importer writes its report next to the data it read
	// and refuses to start when it cannot.
	Export string
}

// Default is the configuration a person who passes no flags gets.
//
// The password is a fixed weak one because this is a local single user database
// that is not reachable from anywhere, and a generated password would mean
// every command in the documentation had a placeholder in it. Anything exposed
// beyond localhost needs a real one, which is what the flag is for.
func Default() Config {
	return Config{
		Image:     "docker.io/library/neo4j:5.26",
		Password:  "luatdo-local",
		Database:  "luatdo",
		Container: "luatdo-neo4j",
		Volume:    "luatdo-neo4j-data",
		HTTPPort:  7474,
		BoltPort:  7687,
		Heap:      "1G",
		PageCache: "512M",
	}
}

// Step is one container runtime invocation.
type Step struct {
	// Args is the command line, without the runtime binary at the front.
	Args []string
	// Why is printed before the step runs, because a person watching a two
	// minute import wants to know which of the three stages they are in.
	Why string
	// Ignore says a non zero exit is not a failure. It is set on the steps that
	// are asking for a state rather than a change, such as creating a volume
	// that is usually already there, and on those a failure and a success mean
	// the same thing afterwards.
	Ignore bool
	// Quiet discards the step's output.
	Quiet bool
}

// Pull fetches the image.
func (c Config) Pull() []Step {
	return []Step{{Args: []string{"pull", c.Image}, Why: "fetching " + c.Image}}
}

// Load imports the export into the volume with no server running.
//
// The offline importer is the fast path and it refuses to run against a live
// database, which is why this is a step of its own and not folded into Up.
// Eight million nodes take about a minute this way and hours over Bolt.
//
// It runs the export's own import.sh rather than reproducing the file list. The
// export writes the exact set of node and relationship files, the flags that
// make multiline Vietnamese text load, and the database name, and a copy of
// that list here would go stale the first time a node type was added.
func (c Config) Load() []Step {
	return []Step{
		{Args: []string{"volume", "create", c.Volume}, Ignore: true, Quiet: true},
		{
			Args: []string{
				"run", "--rm",
				"-v", c.mount(),
				"-v", c.Volume + ":/data",
				"-w", "/import",
				c.Image, "sh", "./import.sh",
			},
			Why: "importing " + c.Export + " into volume " + c.Volume + " as database " + c.Database,
		},
	}
}

// Up starts a server over the volume.
func (c Config) Up() []Step {
	return []Step{
		{Args: []string{"rm", "-f", c.Container}, Ignore: true, Quiet: true},
		{
			Args: []string{
				"run", "-d", "--name", c.Container,
				"-p", fmt.Sprintf("%d:7474", c.HTTPPort),
				"-p", fmt.Sprintf("%d:7687", c.BoltPort),
				"-v", c.Volume + ":/data",
				"-e", "NEO4J_AUTH=neo4j/" + c.Password,
				// Community edition runs a single user database and will not
				// create a second one, so the name is set through configuration
				// rather than through Cypher.
				"-e", "NEO4J_initial_dbms_default__database=" + c.Database,
				"-e", "NEO4J_server_memory_heap_max__size=" + c.Heap,
				"-e", "NEO4J_server_memory_pagecache_size=" + c.PageCache,
				c.Image,
			},
			Why:   "starting " + c.Container,
			Quiet: true,
		},
	}
}

// Down stops and removes the container, keeping the volume.
func (c Config) Down() []Step {
	return []Step{{Args: []string{"rm", "-f", c.Container}, Ignore: true, Quiet: true}}
}

// Wipe removes the volume, which throws the imported graph away.
//
// It is a command of its own rather than a flag on Down because it is the one
// step here that destroys something, and a flag on a stop command is too easy
// to type by accident.
func (c Config) Wipe() []Step {
	return []Step{
		{Args: []string{"rm", "-f", c.Container}, Ignore: true, Quiet: true},
		{Args: []string{"volume", "rm", c.Volume}, Ignore: true, Quiet: true},
	}
}

// Logs follows the server log.
func (c Config) Logs() []Step {
	return []Step{{Args: []string{"logs", "-f", c.Container}}}
}

// Cypher runs one query against the running server through the container's own
// shell, so that reading the database needs no driver, no port reachable from
// the host and no password on the host's command line beyond this one.
func (c Config) Cypher(query string) []Step {
	return []Step{{
		Args: []string{
			"exec", c.Container, "cypher-shell",
			"-u", "neo4j", "-p", c.Password, "-d", c.Database,
			"--format", "plain", query,
		},
	}}
}

// Running reports whether the container is up.
func (c Config) Running() []Step {
	return []Step{{Args: []string{"ps", "--format", "{{.Names}}"}}}
}

// Env is the environment the rest of the tool needs to reach this server,
// in the order a person would export them.
//
// It is generated rather than documented because the names have been wrong in
// documentation before. A prose list of four environment variables that the
// code reads under slightly different names produces an authentication error
// with nothing in it pointing at the cause.
func (c Config) Env() [][2]string {
	return [][2]string{
		{"LUATDO_NEO4J_URI", fmt.Sprintf("bolt://localhost:%d", c.BoltPort)},
		{"LUATDO_NEO4J_USER", "neo4j"},
		{"LUATDO_NEO4J_PASSWORD", c.Password},
		{"LUATDO_NEO4J_DATABASE", c.Database},
	}
}

// Browser is where the graph can be looked at.
func (c Config) Browser() string {
	return "http://localhost:" + strconv.Itoa(c.HTTPPort)
}

// mount is the export directory bind mount.
//
// The path is resolved through any symlinks first, because on macOS the runtime
// is a virtual machine and the mount source is read inside it. /tmp on macOS is
// a symlink to /private/tmp, the machine shares /private and not /tmp, and so an
// export under /tmp is handed over as a path that exists on the host, does not
// exist in the machine, and fails with "statfs: no such file or directory" about
// a directory the person can see is there. Resolving it here turns that into a
// mount that works. On the machines where nothing is symlinked this changes
// nothing.
//
// The z suffix relabels for SELinux and is added only on Linux, which is the
// only place it means anything. On Windows the source is a path with a drive
// letter in it, so it already contains a colon, and every colon that can be
// left out of that argument is one fewer thing for a runtime's mount parser to
// get wrong.
func (c Config) mount() string {
	source := c.Export
	// Best effort. A path that cannot be resolved is passed through as it was
	// given, because the runtime's own error about it is better than one from
	// here about a step before the one that matters.
	if resolved, err := filepath.EvalSymlinks(source); err == nil {
		source = resolved
	}
	m := source + ":/import"
	if runtime.GOOS == "linux" {
		m += ":z"
	}
	return m
}
