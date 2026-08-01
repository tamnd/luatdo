// Command luatdo builds a Vietnamese legal knowledge graph and exports it to Neo4j.
package main

import (
	"fmt"
	"os"
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
	fmt.Fprint(w, `luatdo builds a Vietnamese legal knowledge graph and exports it to Neo4j.

Usage:

  luatdo <command> [arguments]

Commands:

`)
	for _, c := range commands {
		fmt.Fprintf(w, "  %-10s %s\n", c.name, c.summary)
	}
	fmt.Fprint(w, "\nRun \"luatdo <command> -h\" for details on a command.\n")
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
