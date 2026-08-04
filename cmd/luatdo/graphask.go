package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/tamnd/luatdo/graph"
)

func init() {
	commands = append(commands,
		command{"graph", "ask the projected graph one of the twenty six competency questions", cmdGraph},
	)
}

// paramFlag collects repeated --param name=value flags.
type paramFlag map[string]any

func (p paramFlag) String() string {
	parts := make([]string, 0, len(p))
	for k, v := range p {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, ",")
}

// Set reads a value as a number when it looks like one and as text otherwise.
//
// A command line has no types. Guessing is the only option, and guessing wrong
// on a number is the failure that matters: a query comparing a string to a
// numeric property returns nothing at all, silently, which reads as an empty
// answer rather than as a mistake. The other direction is safe, because no
// parameter in these queries is a number that could be typed as digits and
// meant as text.
func (p paramFlag) Set(value string) error {
	name, text, ok := strings.Cut(value, "=")
	if !ok || name == "" {
		return fmt.Errorf("want --param name=value, got %q", value)
	}
	if n, err := strconv.Atoi(text); err == nil {
		p[name] = n
		return nil
	}
	p[name] = text
	return nil
}

// cmdGraph is the road from a loaded database to an answer without writing
// Cypher.
//
// The queries are shipped in every dump and embedded in this binary, so the
// person who has just imported a graph and wants to know what question 20 says
// about their corpus types one command. The query it ran is one flag away, for
// when the shipped question is nearly but not quite the one they wanted.
func cmdGraph(args []string) error {
	fs := flag.NewFlagSet("graph", flag.ContinueOnError)
	question := fs.Int("question", 0, "question number, 1 to 26")
	limit := fs.Int("limit", 0, "override the row limit the question ships with")
	params := paramFlag{}
	fs.Var(params, "param", "override one parameter, as name=value, repeatable")
	sub, _, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	switch sub {
	case "list", "":
		fmt.Print(graph.Catalogue())
		return nil
	case "query":
		q, err := pickQuestion(*question)
		if err != nil {
			return err
		}
		fmt.Print(q.Cypher())
		return nil
	case "ask":
		q, err := pickQuestion(*question)
		if err != nil {
			return err
		}
		if *limit > 0 {
			params["limit"] = *limit
		}
		answer, err := graph.Ask(context.Background(), graph.TargetFromEnv(), q, params)
		if err != nil {
			return err
		}
		fmt.Print(answer)
		return nil
	default:
		return fmt.Errorf("usage: luatdo graph list | luatdo graph ask --question N [--param name=value] | luatdo graph query --question N")
	}
}

func pickQuestion(n int) (graph.Question, error) {
	if n == 0 {
		return graph.Question{}, fmt.Errorf("pass --question N, or run luatdo graph list to see the twenty six")
	}
	q, ok := graph.QuestionByNumber(n)
	if !ok {
		return graph.Question{}, fmt.Errorf("there is no question %d, the problem statement asks 23 and the act layer adds 3", n)
	}
	return q, nil
}
