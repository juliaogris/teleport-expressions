// Package appaccess evaluates a Teleport role's app_resources rules against a
// request and a caller identity. The rule compiler, path matcher, and predicate
// engine are copied verbatim into internal/resourcematcher from the Teleport
// branch julia/app/policy-matcher-sketch
// (lib/srv/app/resourcematcher). This package wraps that engine with YAML
// parsing so the rules and an input can be supplied as text, including from a
// WebAssembly build.
package appaccess

import (
	"bytes"
	"fmt"
	"slices"

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
// role_name names the role those rules came from. A real cluster gathers the
// rules from every role a user holds; this single field stands in for that one
// role, so a deny can report which role was evaluated.
type resourcesDoc struct {
	RoleName         string    `yaml:"role_name,omitempty"`
	AppResources     []rm.Rule `yaml:"app_resources,omitempty"`
	AppResourcesExpr []string  `yaml:"app_resources_expression,omitempty"`
}

// Result is the outcome of evaluating the rules. Vars holds the path segments
// the matching rule's captures bound, and is nil on a deny. On an allow it
// carries the matching rule's allow code and reason; on a deny it carries the
// deny kind, the structured reason for the deny, and every deny hint that fired
// across the rules.
type Result struct {
	Allowed     bool
	Vars        map[string]string
	AllowCode   string
	AllowReason string
	DenyKind    string
	DenyHints   []DeniedHint
	// EvaluatedRoles names the roles whose app_resources were evaluated. An
	// empty list marks a misconfigured default-deny, where no role carried any
	// app_resources.
	EvaluatedRoles []string
}

// DeniedHint is one near-miss explanation that fired on a deny.
type DeniedHint struct {
	DenyCode   string
	DenyReason string
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

	// app_resources never stand alone: a role carries them under
	// spec.allow.app_resources, and a request is evaluated against the roles
	// the user holds. Require both halves so the model stays honest, rather
	// than evaluating a nameless role for a user with no roles.
	if doc.RoleName == "" {
		return Result{}, trace.BadParameter("role_name is required: name the role that carries these app_resources")
	}
	if len(in.Identity.Roles) == 0 {
		return Result{}, trace.BadParameter("identity.roles is required: list the roles the user holds")
	}

	// Gather app_resources only from the roles the user actually holds, the way
	// a real cluster collects rules from a user's roles. role_name names the one
	// role the demo defines; its rules apply only when the user holds it.
	// Otherwise no held role carries app_resources, so the set is empty and the
	// request is a default deny.
	var roles []rm.Role
	if slices.Contains(in.Identity.Roles, doc.RoleName) {
		roles = []rm.Role{{Name: doc.RoleName, Resources: doc.AppResources, Expressions: doc.AppResourcesExpr}}
	}
	set, err := rm.CompileRoles(roles)
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
	res := Result{Allowed: decision.Allowed, EvaluatedRoles: decision.EvaluatedRoles}
	if decision.Allow != nil {
		res.Vars = decision.Allow.Vars
		res.AllowCode = decision.Allow.Code
		res.AllowReason = decision.Allow.Reason
	}
	if decision.Deny != nil {
		res.DenyKind = string(decision.Deny.Kind)
		hints := make([]DeniedHint, 0, len(decision.Deny.Hints))
		for _, h := range decision.Deny.Hints {
			hints = append(hints, DeniedHint{DenyCode: h.Code, DenyReason: h.Reason})
		}
		res.DenyHints = hints
	}
	return res, nil
}

// desugaredDoc is the output of Desugar: the role with every rule expressed as
// a bare predicate under app_resources_expression, the parallel of
// node_labels_expression. The sugared app_resources are gone, lowered into the
// expression list, so the web page can show what a declarative rule compiles to
// as one predicate.
type desugaredDoc struct {
	RoleName         string   `yaml:"role_name,omitempty"`
	AppResourcesExpr []string `yaml:"app_resources_expression"`
}

// Desugar lowers a role's sugared app_resources rules to bare predicate
// strings and returns the role as app_resources_expression YAML. Each
// declarative rule collapses into one predicate over the path, method, and
// identity, with the allow code lowered to a set_allow_code call. Any rule the
// author already wrote under app_resources_expression passes through after the
// lowered ones. It lets the web page show the predicate a declarative rule
// compiles to.
func Desugar(resourcesYAML string) (string, error) {
	var doc resourcesDoc
	if err := yaml.Unmarshal([]byte(resourcesYAML), &doc); err != nil {
		return "", fmt.Errorf("parsing app_resources YAML: %w", err)
	}

	role := rm.Role{Name: doc.RoleName, Resources: doc.AppResources, Expressions: doc.AppResourcesExpr}
	lowered, err := rm.DesugarResources(role)
	if err != nil {
		return "", trace.Wrap(err, "desugaring app_resources")
	}

	out := desugaredDoc{RoleName: doc.RoleName, AppResourcesExpr: append(lowered, doc.AppResourcesExpr...)}
	// Encode at a two-space indent, matching the sugared rule the playground
	// shows alongside, so the two panes line up rather than mixing yaml.v3's
	// default four-space indent with the generated two-space sugar.
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		return "", trace.Wrap(err, "encoding app_resources")
	}
	if err := enc.Close(); err != nil {
		return "", trace.Wrap(err, "encoding app_resources")
	}
	return buf.String(), nil
}
