package deploy

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Runtime is the container runtime this machine has.
//
// Podman is preferred because it needs no daemon and no administrator, which on
// a shared server is the difference between a person being able to run this and
// having to ask somebody. Docker is used when that is what the host has, and
// the two take the same arguments for everything this package asks of them.
type Runtime struct {
	Name string
	Path string
}

// ErrNoRuntime is returned when the machine has neither.
var ErrNoRuntime = errors.New("neither podman nor docker is on PATH")

// FindRuntime looks for a container runtime, preferring podman.
//
// A named runtime is honoured even when the other one is also present, because
// a host with both usually has a reason.
func FindRuntime(prefer string) (Runtime, error) {
	names := []string{"podman", "docker"}
	if prefer != "" {
		names = []string{prefer}
	}
	for _, name := range names {
		// LookPath adds the .exe on Windows, which is the whole of what this
		// package needs to do differently there.
		if path, err := exec.LookPath(name); err == nil {
			return Runtime{Name: name, Path: path}, nil
		}
	}
	if prefer != "" {
		return Runtime{}, fmt.Errorf("%s is not on PATH", prefer)
	}
	return Runtime{}, ErrNoRuntime
}

// Run executes the steps in order and stops at the first one that fails.
//
// Output goes to out as the step produces it rather than being collected,
// because the step this exists for is an import that prints a progress bar for
// a minute and a half, and a progress bar delivered after the fact is a wall of
// dots.
func (r Runtime) Run(steps []Step, out io.Writer) error {
	for _, s := range steps {
		if s.Why != "" {
			_, _ = fmt.Fprintln(out, s.Why)
		}
		cmd := exec.Command(r.Path, s.Args...)
		if s.Quiet {
			cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		} else {
			cmd.Stdout, cmd.Stderr = out, out
		}
		if err := cmd.Run(); err != nil && !s.Ignore {
			return fmt.Errorf("%s %s: %w", r.Name, strings.Join(s.Args, " "), err)
		}
	}
	return nil
}

// Capture executes one step and returns what it wrote, for the steps that are
// questions rather than instructions.
func (r Runtime) Capture(s Step) (string, error) {
	cmd := exec.Command(r.Path, s.Args...)
	cmd.Stderr = io.Discard
	b, err := cmd.Output()
	if err != nil && !s.Ignore {
		return "", fmt.Errorf("%s %s: %w", r.Name, strings.Join(s.Args, " "), err)
	}
	return string(b), nil
}

// IsRunning answers whether the container is up.
func (r Runtime) IsRunning(c Config) bool {
	out, err := r.Capture(Step{Args: c.Running()[0].Args, Ignore: true})
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) == c.Container {
			return true
		}
	}
	return false
}

// Ready answers whether the server is accepting queries yet.
//
// Started and ready are minutes apart on a graph this size: the container is up
// as soon as the process forks, and the database is still recovering and
// building indexes for a good while after. A caller that reports success at the
// first is telling the person to open a browser at a page that will not load.
func (r Runtime) Ready(c Config) bool {
	_, err := r.Capture(c.Cypher("RETURN 1")[0])
	return err == nil
}

// HasExport answers whether a directory holds an export this package can load.
//
// It asks for import.sh rather than for the directory, because an export that
// was interrupted leaves the directory there with some of the CSV files in it,
// and import.sh is written last.
func HasExport(dir string) bool {
	_, err := os.Stat(dir + string(os.PathSeparator) + "import.sh")
	return err == nil
}
