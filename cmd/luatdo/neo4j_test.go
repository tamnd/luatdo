package main

import (
	"runtime"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/deploy"
)

func TestCountReadsTheRowAndNotTheHeader(t *testing.T) {
	if got := count("nodes\n8175346\n"); got != "8175346" {
		t.Errorf("count is %q, want 8175346", got)
	}
	// A server that is up over a volume nobody imported into is the failure this
	// whole subcommand exists to catch, and it prints the header alone.
	for _, out := range []string{"", "nodes\n", "   \n", "nodes\nnull\n"} {
		if got := count(out); got != "an unknown number of" {
			t.Errorf("count(%q) is %q, and a result with no row has to read as unknown rather than as a number", out, got)
		}
	}
}

func TestEnvLinesAreRunnableInTheShellTheyArePrintedTo(t *testing.T) {
	c := deploy.Default()
	lines := envLines(c)
	joined := strings.Join(lines, "\n")
	// All four have to be there whatever the platform. These are the names
	// graph.TargetFromEnv reads, and a person who pastes three of them gets an
	// authentication error with nothing in it about the fourth.
	for _, kv := range c.Env() {
		if !strings.Contains(joined, kv[0]) {
			t.Errorf("%s was not printed:\n%s", kv[0], joined)
		}
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(joined, "$env:LUATDO_NEO4J_URI") {
			t.Errorf("Windows got shell syntax it cannot run:\n%s", joined)
		}
		if !strings.Contains(joined, "cmd.exe") {
			t.Errorf("nothing tells a person in cmd.exe what to write instead:\n%s", joined)
		}
		return
	}
	if !strings.Contains(joined, "export LUATDO_NEO4J_URI=") {
		t.Errorf("Unix got shell syntax it cannot run:\n%s", joined)
	}
	if strings.Contains(joined, "$env:") {
		t.Errorf("PowerShell syntax was printed to a POSIX shell:\n%s", joined)
	}
}

func TestMegabytesIsReadableAtTheSizeThisActuallyDownloads(t *testing.T) {
	if got := megabytes(deploy.DatasetBytes); got != "550MB" {
		t.Errorf("the published dataset reports as %s, and a progress line has to be believable", got)
	}
	if got := megabytes(0); got != "0MB" {
		t.Errorf("megabytes(0) is %s", got)
	}
}

func TestNeo4jLoadRefusesWhenThereIsNoExportToLoad(t *testing.T) {
	c := deploy.Default()
	c.Export = t.TempDir()
	err := neo4jLoad(deploy.Runtime{Name: "fake", Path: "/nonexistent"}, c)
	if err == nil {
		t.Fatal("a load with nothing to load was attempted, and the container error for that says nothing useful")
	}
	// The message has to name the way out. Somebody reaching this has either not
	// downloaded the graph or not projected their own, and the two have different
	// commands.
	for _, want := range []string{"neo4j fetch", "export neo4j"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}
