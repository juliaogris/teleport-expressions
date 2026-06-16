// Package appaccess evaluates Teleport app-access resource rules against a
// request and a caller identity. The rule compiler, path matcher, and predicate
// engine are copied verbatim into internal/resourcematcher from the Teleport
// branch julia/app/policy-matcher-sketch
// (lib/srv/app/resourcematcher). This package wraps that engine with YAML
// parsing so a rule and an input can be supplied as text, including from a
// WebAssembly build.
package appaccess

import (
	"fmt"

	"github.com/gravitational/trace"
	"gopkg.in/yaml.v3"

	rm "github.com/juliaogris/teleport-expressions/internal/resourcematcher"
)

// Input is the request and caller identity a rule is evaluated against. It
// carries YAML tags so it can be parsed from the web page's input field.
type Input struct {
	Request struct {
		Method string `yaml:"method" json:"method"`
		Path   string `yaml:"path" json:"path"`
	} `yaml:"request" json:"request"`
	Identity struct {
		Name   string              `yaml:"name" json:"name"`
		Roles  []string            `yaml:"roles" json:"roles"`
		Traits map[string][]string `yaml:"traits" json:"traits"`
	} `yaml:"identity" json:"identity"`
}

// Result is the outcome of evaluating a rule. Vars holds the path segments the
// matching rule's captures bound, and is nil on a deny.
type Result struct {
	Allowed bool
	Vars    map[string]string
}

// Evaluate parses ruleYAML into a rule, compiles it, and evaluates it against
// in. The rule uses either the declarative fields (paths, methods, where) or
// the bare pred field, never both. Parse, compile, and evaluation errors are
// returned to the caller.
func Evaluate(ruleYAML string, in Input) (Result, error) {
	var rule rm.Rule
	if err := yaml.Unmarshal([]byte(ruleYAML), &rule); err != nil {
		return Result{}, fmt.Errorf("parsing rule YAML: %w", err)
	}

	compiled, err := rule.Compile()
	if err != nil {
		return Result{}, trace.Wrap(err, "compiling rule")
	}

	decision, err := compiled.Evaluate(
		rm.Request{Method: in.Request.Method, Path: in.Request.Path},
		rm.Identity{
			Name:   in.Identity.Name,
			Roles:  in.Identity.Roles,
			Traits: in.Identity.Traits,
		},
	)
	if err != nil {
		return Result{}, trace.Wrap(err, "evaluating rule")
	}
	return Result{Allowed: decision.Allowed, Vars: decision.Vars}, nil
}
