package deploy

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

// The runtime half of this package can only be checked by running something,
// and the thing it normally runs is a container engine that CI does not have and
// that half the machines this ships to do not have either.
//
// So the tests run the test binary itself. A process started with
// LUATDO_FAKE_RUNTIME set does what that variable says and exits, which gives
// every one of these cases a real fork, real pipes and a real exit status,
// on whichever of the three platforms the test happens to be running on. A
// mocked exec interface would test the mock.
func TestMain(m *testing.M) {
	switch os.Getenv("LUATDO_FAKE_RUNTIME") {
	case "":
		os.Exit(m.Run())
	case "echo":
		fmt.Println(strings.Join(os.Args[1:], " "))
	case "fail":
		fmt.Fprintln(os.Stderr, "the runtime said no")
		os.Exit(1)
	case "names":
		// What "ps --format {{.Names}}" prints, including a name that has the
		// one we want as a prefix.
		fmt.Println("luatdo-neo4j-old\nsomething-else")
		if os.Getenv("LUATDO_FAKE_UP") != "" {
			fmt.Println("luatdo-neo4j")
		}
	}
	os.Exit(0)
}

// fake is a Runtime that re-executes the test binary in the mode named.
func fake(t *testing.T, mode string) Runtime {
	t.Helper()
	t.Setenv("LUATDO_FAKE_RUNTIME", mode)
	return Runtime{Name: "fake", Path: os.Args[0]}
}

func TestRunPassesEveryArgumentThroughUntouched(t *testing.T) {
	rt := fake(t, "echo")
	var out bytes.Buffer
	// A password with a space in it is the argument that a shell based runner
	// would split in two, and the failure that produces is an authentication
	// error rather than anything about quoting.
	c := Default()
	c.Password = "two words"
	if err := rt.Run(c.Cypher("MATCH (n) RETURN count(n)"), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "two words") {
		t.Errorf("the password was not passed as one argument: %s", got)
	}
	if !strings.Contains(got, "MATCH (n) RETURN count(n)") {
		t.Errorf("the query was not passed as one argument: %s", got)
	}
}

func TestRunPrintsTheReasonBeforeTheStep(t *testing.T) {
	rt := fake(t, "echo")
	var out bytes.Buffer
	if err := rt.Run([]Step{{Args: []string{"x"}, Why: "doing the thing"}}, &out); err != nil {
		t.Fatal(err)
	}
	// Before, not after. The step this exists for takes a minute and a half, and
	// an explanation printed when it finishes explains nothing.
	if !strings.HasPrefix(out.String(), "doing the thing\n") {
		t.Errorf("the reason did not come first: %q", out.String())
	}
}

func TestRunStopsAtTheFirstFailure(t *testing.T) {
	rt := fake(t, "fail")
	var out bytes.Buffer
	err := rt.Run([]Step{
		{Args: []string{"first"}},
		{Args: []string{"second"}},
	}, &out)
	if err == nil {
		t.Fatal("a failing step was treated as success, so a failed import would be followed by a server over an empty volume")
	}
	// The argument list has to be in the message. "exit status 1" on its own
	// sends a person to the container runtime's own logs for no reason.
	if !strings.Contains(err.Error(), "first") {
		t.Errorf("the error does not say which step failed: %v", err)
	}
	if strings.Contains(err.Error(), "second") {
		t.Errorf("the run continued past a failure: %v", err)
	}
}

func TestRunIgnoresFailureWhereTheOutcomeIsTheSameEitherWay(t *testing.T) {
	rt := fake(t, "fail")
	var out bytes.Buffer
	// Creating a volume that exists, and removing a container that does not, both
	// exit non zero and both leave the world in the state the caller wanted.
	if err := rt.Run([]Step{{Args: []string{"volume", "create", "v"}, Ignore: true}}, &out); err != nil {
		t.Errorf("an ignorable failure was reported: %v", err)
	}
}

func TestQuietStepsWriteNothing(t *testing.T) {
	rt := fake(t, "echo")
	var out bytes.Buffer
	if err := rt.Run([]Step{{Args: []string{"noisy"}, Quiet: true}}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("a quiet step wrote %q", out.String())
	}
}

func TestIsRunningMatchesTheWholeNameAndNotAPrefix(t *testing.T) {
	c := Default()
	rt := fake(t, "names")
	// The fake prints luatdo-neo4j-old, which contains the container name. A
	// substring test here reports a server that is not there, and the load step
	// then refuses to run for a reason that is not true.
	if rt.IsRunning(c) {
		t.Error("a container whose name merely starts with ours was read as ours")
	}
	t.Setenv("LUATDO_FAKE_UP", "1")
	if !rt.IsRunning(c) {
		t.Error("the container was listed and was not found")
	}
}

func TestReadyFollowsWhetherTheQueryAnswers(t *testing.T) {
	c := Default()
	if fake(t, "fail").Ready(c) {
		t.Error("a database that refused the query was called ready, which sends a person to a browser page that will not load")
	}
	if !fake(t, "echo").Ready(c) {
		t.Error("a database that answered the query was not called ready")
	}
}

func TestCaptureReturnsOutputWithoutTheRuntimesOwnNoise(t *testing.T) {
	rt := fake(t, "echo")
	out, err := rt.Capture(Step{Args: []string{"hello", "world"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "hello world" {
		t.Errorf("capture returned %q", out)
	}
	if _, err := rt.Capture(Step{Args: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	// Standard error is discarded, because a runtime that writes a warning to it
	// would otherwise land in the middle of a node count.
	if _, err := fake(t, "fail").Capture(Step{Args: []string{"x"}}); err == nil {
		t.Error("a failing capture returned no error")
	}
}
