// Teleport
// Copyright (C) 2023-2024 Gravitational, Inc.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

// The functions in this file are copied, with light edits, from Teleport's
// lib/utils/replace.go and lib/utils/parse/parse.go at tag v18.8.3. They are
// the regular-expression and string helpers that the label expression
// functions depend on. They are reproduced here so that the evaluator does
// not need to import the wider lib/utils package, which pulls in a large
// dependency tree that is unsuitable for a WebAssembly build.

package labelexpr

import (
	"net/mail"
	"regexp"
	"strings"

	"github.com/gravitational/trace"
)

var replaceWildcard = regexp.MustCompile(`(\\\*)`)

// globToRegexp replaces glob-style standalone wildcard values with real .*
// regexp-friendly values, does not modify regexp-compatible values, and quotes
// non-wildcard values.
func globToRegexp(in string) string {
	return replaceWildcard.ReplaceAllString(regexp.QuoteMeta(in), "(.*)")
}

// isRegexp returns true if the expression is a raw regex pattern, meaning that
// it starts with a caret and ends with a dollar sign.
func isRegexp(expression string) bool {
	return strings.HasPrefix(expression, "^") && strings.HasSuffix(expression, "$")
}

// expressionToRegexp converts a Teleport expression to a regexp string. A raw
// regular expression is left untouched. A plain string or glob is anchored and
// has its wildcards expanded.
func expressionToRegexp(expression string) string {
	if isRegexp(expression) {
		return expression
	}
	return "^" + globToRegexp(expression) + "$"
}

// compileExpression compiles the given expression with Teleport's custom
// globbing and quoting logic.
func compileExpression(expression string) (*regexp.Regexp, error) {
	expr, err := regexp.Compile(expressionToRegexp(expression))
	if err != nil {
		return nil, trace.BadParameter("%s", err)
	}
	return expr, nil
}

// regexMatchesAny returns true if any of the inputs matches the expression. It
// is the implementation behind the regexp.match function.
func regexMatchesAny(inputs []string, expression string) (bool, error) {
	expr, err := compileExpression(expression)
	if err != nil {
		return false, trace.Wrap(err)
	}
	for _, in := range inputs {
		if expr.MatchString(in) {
			return true, nil
		}
	}
	return false, nil
}

// matchString matches a single input against the given expression.
func matchString(input, expression string) (bool, error) {
	expr, err := compileExpression(expression)
	if err != nil {
		return false, trace.BadParameter("%s", err)
	}
	return expr.MatchString(input), nil
}

// emailLocal returns the local part of each input email address. It is the
// implementation behind the email.local function.
func emailLocal(inputs []string) ([]string, error) {
	return stringListMap(inputs, func(email string) (string, error) {
		if email == "" {
			return "", trace.BadParameter("found empty email.local argument")
		}
		addr, err := mail.ParseAddress(email)
		if err != nil {
			return "", trace.BadParameter(
				"failed to parse email.local argument %q: %s", email, err)
		}
		parts := strings.SplitN(addr.Address, "@", 2)
		if len(parts) != 2 {
			return "", trace.BadParameter(
				"could not find local part in email.local argument %q, %q",
				email, addr.Address)
		}
		return parts[0], nil
	})
}

// regexpReplace returns a new list which is the result of replacing each
// instance of match with replacement for each item in the input list. It is the
// implementation behind the regexp.replace function.
func regexpReplace(inputs []string, match string, replacement string) ([]string, error) {
	re, err := regexp.Compile(match)
	if err != nil {
		return nil, trace.BadParameter("invalid regexp %q: %s", match, err)
	}
	return stringListMap(inputs, func(in string) (string, error) {
		if !re.MatchString(in) {
			return "", nil
		}
		return re.ReplaceAllString(in, replacement), nil
	})
}

// stringListMap applies f to every input and drops any results that map to the
// empty string.
func stringListMap(inputs []string, f func(string) (string, error)) ([]string, error) {
	out := make([]string, 0, len(inputs))
	for _, input := range inputs {
		mapped, err := f(input)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		if len(mapped) == 0 {
			continue
		}
		out = append(out, mapped)
	}
	return out, nil
}
