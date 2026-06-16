// Package appaccess evaluates a Teleport role's app_resources rules against a
// request and a caller identity. The rule compiler, path matcher, and predicate
// engine are copied verbatim into internal/resourcematcher from the Teleport
// branch julia/app/policy-matcher-sketch
// (lib/srv/app/resourcematcher). This package wraps that engine with YAML
// parsing so the rules and an input can be supplied as text, including from a
// WebAssembly build.
package appaccess

import (
	"fmt"

	"github.com/gravitational/trace"
	"gopkg.in/yaml.v3"

	rm "github.com/juliaogris/teleport-expressions/internal/resourcematcher"
)

// Input is the request and caller identity the rules are evaluated against. It
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

// resourcesDoc mirrors the relevant slice of a role spec: app_resources is a
// list of rules, the same field a role would carry alongside app_labels.
type resourcesDoc struct {
	AppResources []rm.Rule `yaml:"app_resources"`
}

// Result is the outcome of evaluating the rules. Vars holds the path segments
// the matching rule's captures bound, and is nil on a deny.
type Result struct {
	Allowed bool
	Vars    map[string]string
}

// Evaluate parses an app_resources list, compiles the rules into a rule set,
// and evaluates the set against in. The rules are additive: the request is
// allowed if any rule matches, and the returned captures come from the first
// matching rule. Each rule uses either the declarative fields (paths, methods,
// where) or the bare pred field, never both. Parse, compile, and evaluation
// errors are returned to the caller.
func Evaluate(resourcesYAML string, in Input) (Result, error) {
	var doc resourcesDoc
	if err := yaml.Unmarshal([]byte(resourcesYAML), &doc); err != nil {
		return Result{}, fmt.Errorf("parsing app_resources YAML: %w", err)
	}

	set, err := rm.CompileRules(doc.AppResources)
	if err != nil {
		return Result{}, trace.Wrap(err, "compiling app_resources")
	}

	decision, err := set.Evaluate(
		rm.Request{Method: in.Request.Method, Path: in.Request.Path},
		rm.Identity{
			Name:   in.Identity.Name,
			Roles:  in.Identity.Roles,
			Traits: in.Identity.Traits,
		},
	)
	if err != nil {
		return Result{}, trace.Wrap(err, "evaluating app_resources")
	}
	return Result{Allowed: decision.Allowed, Vars: decision.Vars}, nil
}

// Desugar lowers every rule in an app_resources list to its bare predicate
// form and returns the resulting app_resources YAML. A rule already in the
// predicate form is returned unchanged. It lets the web page show the
// predicate a declarative rule compiles to.
func Desugar(resourcesYAML string) (string, error) {
	var doc resourcesDoc
	if err := yaml.Unmarshal([]byte(resourcesYAML), &doc); err != nil {
		return "", fmt.Errorf("parsing app_resources YAML: %w", err)
	}

	out := resourcesDoc{AppResources: make([]rm.Rule, 0, len(doc.AppResources))}
	for i, r := range doc.AppResources {
		pred, err := r.DesugarPredicate()
		if err != nil {
			return "", trace.Wrap(err, "desugaring rule %d", i)
		}
		out.AppResources = append(out.AppResources, rm.Rule{Pred: pred})
	}

	marshalled, err := yaml.Marshal(out)
	if err != nil {
		return "", trace.Wrap(err, "encoding app_resources")
	}
	return string(marshalled), nil
}
