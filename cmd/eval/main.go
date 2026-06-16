// Command eval reads a label expression and a YAML input from the command line
// or standard input, evaluates the expression against the input, and prints the
// boolean result.
//
// Usage:
//
//	eval -expr '<expression>' [-input <file>]
//	eval -expr '<expression>' < input.yaml
//
// The input is YAML with the following structure:
//
//	labels:
//	  env: prod
//	  owner: devs
//	username: alice
//	traits:
//	  groups: [devs, security]
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/juliaogris/teleport-expressions/labelexpr"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	expr := flag.String("expr", "", "label expression to evaluate (required)")
	inputPath := flag.String("input", "", "path to a YAML input file (defaults to stdin)")
	flag.Parse()

	if *expr == "" {
		return fmt.Errorf("the -expr flag is required")
	}

	raw, err := readInput(*inputPath)
	if err != nil {
		return err
	}

	var in labelexpr.Input
	if err := yaml.Unmarshal(raw, &in); err != nil {
		return fmt.Errorf("parsing YAML input: %w", err)
	}

	match, err := labelexpr.Evaluate(*expr, in)
	if err != nil {
		return err
	}

	fmt.Println(match)
	return nil
}

func readInput(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}
