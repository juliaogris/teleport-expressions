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
	"slices"
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

	// Gather app_resources only from the roles the user actually holds, the way
	// a real cluster collects rules from a user's roles. role_name names the one
	// role the demo defines; its rules apply only when the user holds it.
	// Otherwise no held role carries app_resources, so the set is empty and the
	// request is a default deny.
	var roles []rm.Role
	if slices.Contains(in.Identity.Roles, doc.RoleName) {
		roles = []rm.Role{{Name: doc.RoleName, Rules: doc.AppResources}}
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

// isIdentByte reports whether b can appear in a predicate identifier.
func isIdentByte(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' ||
		b >= '0' && b <= '9' || b == '_'
}

// matchParen returns the index of the ")" that closes the "(" at open. A path
// segment string never contains a quote or backslash, so a plain quote toggle
// is enough to skip string contents. It returns -1 if the parenthesis is
// unbalanced.
func matchParen(s string, open int) int {
	depth, inString := 0, false
	for i := open; i < len(s); i++ {
		switch c := s[i]; {
		case inString:
			if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// readQuoted reads the "..."-quoted text starting at the opening quote i and
// returns the inner text and the index just past the closing quote.
func readQuoted(s string, i int) (string, int) {
	j := i + 1
	for j < len(s) && s[j] != '"' {
		j++
	}
	return s[i+1 : j], j + 1
}

// hasTopLevelAnd reports whether s contains a "&&" outside any nested
// parenthesis and outside string literals. Parentheses around an expression
// with a top-level "&&" carry precedence and must not be stripped.
func hasTopLevelAnd(s string) bool {
	depth, inString := 0, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inString:
			if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == '&' && depth == 0 && i+1 < len(s) && s[i+1] == '&':
			return true
		}
	}
	return false
}

// contractLiterals rewrites literal("a", literal("b", rest)) to
// literal("a/b", rest) wherever a literal's only argument after its text is
// another literal, collapsing a hand-written segment chain into the single
// slash-joined form the path surface already uses. A literal with more than
// one child is an alternation, not a chain, so it is left alone. It is a
// display-only rewrite: literal("a/b") and literal("a", literal("b")) compile
// to the same node.
func contractLiterals(s string) string {
	const lit = "literal("
	for i := 0; i+len(lit) <= len(s); i++ {
		if s[i:i+len(lit)] != lit || (i > 0 && isIdentByte(s[i-1])) {
			continue
		}
		open := i + len(lit) - 1
		if open+1 >= len(s) || s[open+1] != '"' {
			continue
		}
		text, afterText := readQuoted(s, open+1)
		m := afterText
		if m >= len(s) || s[m] != ',' {
			continue
		}
		m++
		if m < len(s) && s[m] == ' ' {
			m++
		}
		if m+len(lit) > len(s) || s[m:m+len(lit)] != lit {
			continue
		}
		innerOpen := m + len(lit) - 1
		innerClose := matchParen(s, innerOpen)
		outerClose := matchParen(s, open)
		// The inner literal must be the sole child: the outer literal closes
		// immediately after it. Otherwise the children are alternation
		// siblings, which a slash join would silently merge.
		if innerClose < 0 || innerClose+1 != outerClose {
			continue
		}
		if innerOpen+1 >= len(s) || s[innerOpen+1] != '"' {
			continue
		}
		innerText, afterInnerText := readQuoted(s, innerOpen+1)
		rest := s[afterInnerText:innerClose]
		merged := lit + `"` + text + "/" + innerText + `"` + rest + ")"
		// Re-run from the start so a longer chain collapses fully.
		return contractLiterals(s[:i] + merged + s[outerClose+1:])
	}
	return s
}

// stripRedundantParens removes a grouping parenthesis whose content carries no
// top-level "&&", since such a wrapper only adds noise. The desugarer wraps a
// rule's where clause in parentheses when joining it to the path and method
// clauses; once that clause is a single term, the wrapper is redundant. A
// parenthesis that follows an identifier opens a call argument list and is
// never touched.
func stripRedundantParens(s string) string {
	inString := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inString:
			if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
		case c == '(':
			p := i - 1
			for p >= 0 && s[p] == ' ' {
				p--
			}
			if p >= 0 && isIdentByte(s[p]) {
				continue // call paren, not a grouping paren
			}
			close := matchParen(s, i)
			if close < 0 {
				continue
			}
			inner := s[i+1 : close]
			if strings.TrimSpace(inner) == "" || hasTopLevelAnd(inner) {
				continue
			}
			return stripRedundantParens(s[:i] + inner + s[close+1:])
		}
	}
	return s
}

// formatPredicate reformats a predicate so the matcher tree reads as a path. A
// constructor keeps its scalar arguments, a literal's text or a capture's name,
// on its own line, and breaks onto a new indented line only before an argument
// that is itself a call, the node's child. Sibling arguments that are calls
// share one level, and a child is indented one level further. So
// path.match(literal("files", capture("x", glob()))) renders as a single
// descending path.
//
// The result stays parseable: a line only ever ends in "(", ",", or an
// operator, never a ")" mid-expression, since the engine parses Go expression
// syntax where a line ending in ")" inside an argument list would take an
// inserted semicolon and fail. An empty "()" is kept on one line.
func formatPredicate(s string) string {
	s = compactWhitespace(s)
	s = contractLiterals(s)
	s = stripRedundantParens(s)
	var b strings.Builder
	// base is the indent level at which the current call's child-call
	// arguments break. The stack restores it as each call closes.
	base := 0
	var stack []int
	inString := false
	newline := func(d int) {
		b.WriteByte('\n')
		b.WriteString(strings.Repeat("  ", d))
	}
	// isCallStart reports whether the argument beginning at j, after any spaces
	// and a leading "!", is a call: an identifier, possibly dotted, immediately
	// followed by "(". Only a call argument breaks onto its own line; a scalar
	// stays inline on the constructor's line.
	isCallStart := func(j int) bool {
		for j < len(s) && s[j] == ' ' {
			j++
		}
		if j < len(s) && s[j] == '!' {
			j++
		}
		k := j
		for k < len(s) && (isIdentByte(s[k]) || s[k] == '.') {
			k++
		}
		return k > j && k < len(s) && s[k] == '('
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
			b.WriteByte('(')
			stack = append(stack, base)
			// A call paren follows an identifier and opens an argument list, so
			// it indents its children. A grouping paren follows an operator or
			// nothing and only wraps an expression for precedence, so it keeps
			// its content at the same level rather than adding a layer.
			p := i - 1
			for p >= 0 && s[p] == ' ' {
				p--
			}
			if p >= 0 && isIdentByte(s[p]) {
				base++
				// Break before the first argument only when it is a nested
				// call; a scalar first argument stays on the opening line.
				if isCallStart(i + 1) {
					newline(base)
				}
			}
		case ')':
			b.WriteByte(')')
			if len(stack) > 0 {
				base = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case ',':
			b.WriteByte(',')
			j := i + 1
			if j < len(s) && s[j] == ' ' {
				j++
			}
			if isCallStart(j) {
				newline(base)
			} else {
				b.WriteByte(' ')
			}
			i = j - 1
		case '&':
			if i+1 < len(s) && s[i+1] == '&' {
				b.WriteString("&&")
				i++
				newline(base)
				if i+1 < len(s) && s[i+1] == ' ' {
					i++
				}
			} else {
				b.WriteByte(c)
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
