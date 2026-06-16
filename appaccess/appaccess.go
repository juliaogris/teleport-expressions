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

	out := resourcesDoc{AppResources: make([]rm.Rule, 0, len(doc.AppResources))}
	for i, r := range doc.AppResources {
		pred, err := r.DesugarPredicate()
		if err != nil {
			return "", trace.Wrap(err, "desugaring rule %d", i)
		}
		out.AppResources = append(out.AppResources, rm.Rule{
			Where: formatPredicate(pred),
			// Carry the path-decoding config onto the desugared rule, so the
			// bare predicate form decodes the path the same way the
			// declarative form did and the two surfaces stay equivalent.
			URLDecoding: r.URLDecoding,
		})
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
