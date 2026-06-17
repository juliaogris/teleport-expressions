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
	"strings"

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
	RoleName     string    `yaml:"role_name,omitempty"`
	AppResources []rm.Rule `yaml:"app_resources"`
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

	// The demo models a single role: role_name names it and app_resources are
	// its rules. CompileRoles builds the union and remembers the role name, so
	// the decision reports it as an evaluated role without a separate list.
	set, err := rm.CompileRoles([]rm.Role{{Name: doc.RoleName, Rules: doc.AppResources}})
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

// Desugar lowers every rule in an app_resources list to a single where
// predicate and returns the resulting app_resources YAML. The declarative
// paths and methods collapse into the same where condition, so the desugared
// rule reads as one predicate over the path, method, and identity. It lets the
// web page show the predicate a declarative rule compiles to.
func Desugar(resourcesYAML string) (string, error) {
	var doc resourcesDoc
	if err := yaml.Unmarshal([]byte(resourcesYAML), &doc); err != nil {
		return "", fmt.Errorf("parsing app_resources YAML: %w", err)
	}

	out := resourcesDoc{RoleName: doc.RoleName, AppResources: make([]rm.Rule, 0, len(doc.AppResources))}
	for i, r := range doc.AppResources {
		// DesugaredRule lowers to the bare predicate form and carries the audit
		// metadata and decode config, so the two surfaces stay equivalent. A
		// deny hint's default On is materialized, since the bare form has no
		// path or method clause to default from.
		dr, err := r.DesugaredRule()
		if err != nil {
			return "", trace.Wrap(err, "desugaring rule %d", i)
		}
		dr.Where = formatPredicate(dr.Where)
		out.AppResources = append(out.AppResources, dr)
	}

	marshalled, err := yaml.Marshal(out)
	if err != nil {
		return "", trace.Wrap(err, "encoding app_resources")
	}
	return string(marshalled), nil
}

// compactWhitespace collapses every run of whitespace outside string literals
// to a single space, and drops spaces that sit just inside "(", or just before
// ")" or ",". It normalizes a predicate that an author may have already wrapped
// over several lines, so formatPredicate can reformat it from a canonical form
// rather than layering its own breaks on top of the author's and leaving blank
// lines and stray indentation.
func compactWhitespace(s string) string {
	var b strings.Builder
	inString := false
	pendingSpace := false
	var last byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			b.WriteByte(c)
			last = c
			if c == '"' {
				inString = false
			}
			continue
		}
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			pendingSpace = true
			continue
		}
		if pendingSpace && b.Len() > 0 && last != '(' && c != ')' && c != ',' {
			b.WriteByte(' ')
		}
		pendingSpace = false
		b.WriteByte(c)
		last = c
		if c == '"' {
			inString = true
		}
	}
	return b.String()
}

// formatPredicate reformats a predicate into an indented multi-line form so the
// matcher tree's nesting is visible. It first collapses the input to a
// canonical spacing with compactWhitespace, then breaks after "(", ",", and
// "&&". Closing parentheses stay on the line they close, so the result remains
// parseable: the engine parses Go expression syntax, where a line may end in
// "(", ",", or an operator, but a line ending in ")" inside an argument list
// would have a semicolon inserted and fail to parse. An empty "()" is kept on
// one line.
func formatPredicate(s string) string {
	s = compactWhitespace(s)
	var b strings.Builder
	depth := 0
	inString := false
	newline := func(d int) {
		b.WriteByte('\n')
		b.WriteString(strings.Repeat("  ", d))
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			b.WriteByte(c)
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
			b.WriteByte(c)
		case '(':
			if i+1 < len(s) && s[i+1] == ')' {
				b.WriteString("()")
				i++
				continue
			}
			depth++
			b.WriteByte('(')
			newline(depth)
		case ')':
			if depth > 0 {
				depth--
			}
			b.WriteByte(')')
		case ',':
			b.WriteByte(',')
			if i+1 < len(s) && s[i+1] == ' ' {
				i++
			}
			newline(depth)
		case '&':
			if i+1 < len(s) && s[i+1] == '&' {
				b.WriteString("&&")
				i++
				if i+1 < len(s) && s[i+1] == ' ' {
					i++
				}
				newline(depth)
			} else {
				b.WriteByte(c)
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
