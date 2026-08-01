package main

import (
	"flag"
	"io"
	"testing"
)

func newFlags() (*flag.FlagSet, *int, *string) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	n := fs.Int("n", 200, "")
	by := fs.String("by", "", "")
	return fs, n, by
}

func TestParseSubHonoursAFlagWrittenAfterTheSubcommand(t *testing.T) {
	// This is the bug the helper exists for. The flag package stops parsing at
	// the first argument that is not a flag, so "concepts sample -n 3" drew two
	// hundred units and said so.
	fs, n, _ := newFlags()
	sub, rest, err := parseSub(fs, []string{"sample", "-n", "3"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sub != "sample" {
		t.Errorf("subcommand %q", sub)
	}
	if *n != 3 {
		t.Errorf("n is %d, want 3", *n)
	}
	if len(rest) != 0 {
		t.Errorf("positional arguments %v", rest)
	}
}

func TestParseSubStillAcceptsFlagsWrittenFirst(t *testing.T) {
	fs, n, _ := newFlags()
	sub, rest, err := parseSub(fs, []string{"-n", "3", "read", "vn:law:2019:45-2019-qh14"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sub != "read" {
		t.Errorf("subcommand %q", sub)
	}
	if *n != 3 {
		t.Errorf("n is %d, want 3", *n)
	}
	if len(rest) != 1 || rest[0] != "vn:law:2019:45-2019-qh14" {
		t.Errorf("positional arguments %v", rest)
	}
}

func TestParseSubKeepsPositionalArgumentsAfterTheSubcommand(t *testing.T) {
	fs, _, by := newFlags()
	sub, rest, err := parseSub(fs, []string{"answer", "-by", "tamnd", "a", "b", "same", "cùng một định nghĩa"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sub != "answer" {
		t.Errorf("subcommand %q", sub)
	}
	if *by != "tamnd" {
		t.Errorf("by is %q", *by)
	}
	if len(rest) != 4 || rest[0] != "a" || rest[3] != "cùng một định nghĩa" {
		t.Errorf("positional arguments %v", rest)
	}
}

func TestParseSubOfNothingIsTheEmptySubcommand(t *testing.T) {
	// The review command treats an empty subcommand as list, so this has to be
	// a value rather than an error.
	fs, _, _ := newFlags()
	sub, rest, err := parseSub(fs, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sub != "" || len(rest) != 0 {
		t.Errorf("got %q and %v", sub, rest)
	}
}

func TestParseSubDoesNotMistakeAFlagValueForTheSubcommand(t *testing.T) {
	// The value of -data is a path, and a path does not start with a dash.
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	data := fs.String("data", "", "")
	sub, _, err := parseSub(fs, []string{"-data", "/tmp/luatdo", "build"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if *data != "/tmp/luatdo" {
		t.Errorf("data is %q", *data)
	}
	if sub != "build" {
		t.Errorf("subcommand %q, want build", sub)
	}
}

func TestArgOutOfRangeIsEmptyRatherThanAPanic(t *testing.T) {
	rest := []string{"a"}
	if arg(rest, 0) != "a" {
		t.Error("the first argument is wrong")
	}
	if arg(rest, 1) != "" {
		t.Error("a missing argument should be the empty string")
	}
}
