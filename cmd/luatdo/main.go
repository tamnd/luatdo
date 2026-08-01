// Command luatdo builds a Vietnamese legal knowledge graph and exports it to Neo4j.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "luatdo:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stdout)
		return nil
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println("luatdo", version)
		return nil
	case "help", "--help", "-h":
		usage(os.Stdout)
		return nil
	default:
		return dispatch(args[0], args[1:])
	}
}

func usage(w *os.File) {
	var b strings.Builder
	b.WriteString(`luatdo builds a Vietnamese legal knowledge graph and exports it to Neo4j.

Usage:

  luatdo <command> [arguments]

Commands:

`)
	for _, c := range commands {
		fmt.Fprintf(&b, "  %-10s %s\n", c.name, c.summary)
	}
	b.WriteString("\nRun \"luatdo <command> -h\" for details on a command.\n")
	_, _ = w.WriteString(b.String())
}

// parseSub parses a command's flags and returns its subcommand with whatever
// positional arguments follow it.
//
// The flag package stops parsing at the first argument that is not a flag, so a
// command that reads its subcommand out of fs.Arg(0) silently ignores every flag
// written after it. "luatdo concepts sample -n 3" drew two hundred units and
// said so, and the only reason nobody noticed for six milestones is that the
// documented invocation happened to pass the default. Taking the subcommand off
// the front before parsing makes a flag work wherever a person puts it, and the
// second branch keeps the older form working for anyone with a script that
// writes the flags first.
func parseSub(fs *flag.FlagSet, args []string) (string, []string, error) {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return "", nil, err
	}
	rest := fs.Args()
	if sub == "" && len(rest) > 0 {
		sub, rest = rest[0], rest[1:]
	}
	return sub, rest, nil
}

// arg returns the nth positional argument or the empty string. A missing
// positional is a usage error rather than a panic, and every caller here already
// checks for the empty string.
func arg(args []string, n int) string {
	if n < len(args) {
		return args[n]
	}
	return ""
}

type command struct {
	name    string
	summary string
	run     func(args []string) error
}

var commands []command

func dispatch(name string, args []string) error {
	for _, c := range commands {
		if c.name == name {
			return c.run(args)
		}
	}
	return fmt.Errorf("unknown command %q, run \"luatdo help\"", name)
}
