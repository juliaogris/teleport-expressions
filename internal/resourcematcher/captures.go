/*
 * Teleport
 * Copyright (C) 2026  Gravitational, Inc.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package resourcematcher

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"slices"
	"strconv"

	"github.com/gravitational/trace"
)

// validateCaptures is the load-time capture check. It rejects a rule whose
// predicate reads a vars.<name> that no matcher in the same rule binds. Without
// it, a typo such as vars.projct would be silently unbound and read as the
// empty string at request time; the request would still fail closed, but the
// author would get no signal that the rule never does what they wrote.
//
// The check restores at load the type-safety that vars.<name> defers to
// evaluation: vars names are dynamic, so they cannot be enumerated in the
// parser spec, and GetUnknownIdentifier accepts any vars.<name> at parse time.
//
// The predicate is the same Go expression syntax the engine parses, so this
// reuses go/parser to walk the AST. It collects the set of names bound by
// capture("name", ...) calls and the set referenced by vars.<name> selectors,
// and reports the first reference with no matching binding. An expression that
// does not parse is left to the engine, which reports the parse error with full
// type information.
func validateCaptures(expr string) error {
	parsed, err := goparser.ParseExpr(expr)
	if err != nil {
		return nil
	}

	var bound, referenced []string
	ast.Inspect(parsed, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if name, ok := captureName(x); ok {
				bound = append(bound, name)
			}
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && id.Name == "vars" {
				referenced = append(referenced, x.Sel.Name)
			}
		}
		return true
	})

	for _, name := range referenced {
		if !slices.Contains(bound, name) {
			return trace.BadParameter(
				"predicate reads vars.%s but no matcher in the rule captures %q", name, name)
		}
	}
	return nil
}

// captureName returns the bound name of a capture("name", ...) call. It reports
// false for any other call, and for a capture call whose first argument is not
// a string literal (a dynamic name cannot be checked at load).
func captureName(call *ast.CallExpr) (string, bool) {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "capture" || len(call.Args) == 0 {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	name, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return name, true
}
