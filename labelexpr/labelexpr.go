// Teleport
// Copyright (C) 2023 Gravitational, Inc.
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

// Package labelexpr evaluates Teleport label expressions against a set of
// resource labels, a username, and user traits. The parser specification is
// copied, with light edits, from Teleport's lib/services/label_expressions.go
// at tag v18.8.3. It is reproduced here so the evaluator can run as a small,
// standalone program (including a WebAssembly build) without importing the
// wider Teleport packages.
package labelexpr

import (
	"slices"
	"strings"

	"github.com/gravitational/trace"

	"github.com/juliaogris/teleport-expressions/internal/set"
	"github.com/juliaogris/teleport-expressions/internal/typical"
)

// Input is the environment a label expression is evaluated against. Labels are
// the resource labels, Username is the connecting user, and Traits are that
// user's traits keyed by trait name.
type Input struct {
	Labels   map[string]string   `json:"labels" yaml:"labels"`
	Username string              `json:"username" yaml:"username"`
	Traits   map[string][]string `json:"traits" yaml:"traits"`
}

// labelGetter reads labels from a plain map.
type labelGetter map[string]string

func (l labelGetter) GetLabel(key string) (string, bool) {
	value, ok := l[key]
	return value, ok
}

func (l labelGetter) GetAllLabels() map[string]string {
	return l
}

type labelExpressionEnv struct {
	resourceLabelGetter labelGetter
	username            string
	userTraits          map[string][]string
}

var labelExpressionParser = mustNewLabelExpressionParser()

// Evaluate parses expr and evaluates it against in, returning whether the
// expression matches. Parsing and evaluation errors are returned to the caller.
func Evaluate(expr string, in Input) (bool, error) {
	parsed, err := labelExpressionParser.Parse(expr)
	if err != nil {
		return false, trace.Wrap(err, "parsing label expression")
	}
	match, err := parsed.Evaluate(labelExpressionEnv{
		resourceLabelGetter: labelGetter(in.Labels),
		username:            in.Username,
		userTraits:          in.Traits,
	})
	if err != nil {
		return false, trace.Wrap(err, "evaluating label expression")
	}
	return match, nil
}

func mustNewLabelExpressionParser() *typical.CachedParser[labelExpressionEnv, bool] {
	parser, err := newLabelExpressionParser()
	if err != nil {
		panic(trace.Wrap(err, "failed to create label expression parser (this is a bug)"))
	}
	return parser
}

func newLabelExpressionParser() (*typical.CachedParser[labelExpressionEnv, bool], error) {
	parser, err := typical.NewCachedParser[labelExpressionEnv, bool](typical.ParserSpec[labelExpressionEnv]{
		Variables: map[string]typical.Variable{
			"user.metadata.name": typical.DynamicVariable(
				func(env labelExpressionEnv) (string, error) {
					if env.username == "" {
						return "", trace.NotFound("user.metadata.name is not available in this context")
					}
					return env.username, nil
				}),
			"user.spec.traits": typical.DynamicVariable(
				func(env labelExpressionEnv) (map[string][]string, error) {
					return env.userTraits, nil
				}),
			"labels": typical.DynamicMapFunction(
				func(env labelExpressionEnv, key string) (string, error) {
					label, _ := env.resourceLabelGetter.GetLabel(key)
					return label, nil
				}),
		},
		Functions: map[string]typical.Function{
			"set": typical.UnaryVariadicFunction[labelExpressionEnv](
				func(args ...string) ([]string, error) {
					return args, nil
				}),
			"labels_matching": typical.UnaryFunctionWithEnv(labelsMatching),
			"contains": typical.BinaryFunction[labelExpressionEnv](
				func(list []string, item string) (bool, error) {
					return slices.Contains(list, item), nil
				}),
			"contains_any": typical.BinaryFunction[labelExpressionEnv](containsAny),
			"contains_all": typical.BinaryFunction[labelExpressionEnv](containsAll),
			"regexp.match": typical.BinaryFunction[labelExpressionEnv](
				func(list []string, re string) (bool, error) {
					match, err := regexMatchesAny(list, re)
					if err != nil {
						return false, trace.Wrap(err, "invalid regular expression %q", re)
					}
					return match, nil
				}),
			// Use regexp.replace and email.local to get behavior identical to
			// role templates.
			"regexp.replace": typical.TernaryFunction[labelExpressionEnv](regexpReplace),
			"email.local":    typical.UnaryFunction[labelExpressionEnv](emailLocal),
			"strings.upper": typical.UnaryFunction[labelExpressionEnv](
				func(list []string) ([]string, error) {
					out := make([]string, len(list))
					for i, s := range list {
						out[i] = strings.ToUpper(s)
					}
					return out, nil
				}),
			"strings.lower": typical.UnaryFunction[labelExpressionEnv](
				func(list []string) ([]string, error) {
					out := make([]string, len(list))
					for i, s := range list {
						out[i] = strings.ToLower(s)
					}
					return out, nil
				}),
		},
	})
	return parser, trace.Wrap(err)
}

// labelsMatching returns the aggregate of all label values for all keys that
// match keyExpr. It supports globs or full regular expressions and must find a
// complete match for the key.
func labelsMatching(env labelExpressionEnv, keyExpr string) ([]string, error) {
	allLabels := env.resourceLabelGetter.GetAllLabels()
	var matchingLabelValues []string
	for key, value := range allLabels {
		match, err := matchString(key, keyExpr)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		if match {
			matchingLabelValues = append(matchingLabelValues, value)
		}
	}
	return matchingLabelValues, nil
}

// containsAny returns true if list contains any element of items.
func containsAny(list []string, items []string) (bool, error) {
	s := set.New(list...)
	for _, item := range items {
		if s.Contains(item) {
			return true, nil
		}
	}
	return false, nil
}

// containsAll returns true if list contains every element of items. If items is
// empty, it returns false, to avoid matching resources that otherwise appear
// unrelated to the expression.
func containsAll(list []string, items []string) (bool, error) {
	if len(items) == 0 {
		return false, nil
	}
	s := set.New(list...)
	for _, item := range items {
		if !s.Contains(item) {
			return false, nil
		}
	}
	return true, nil
}
