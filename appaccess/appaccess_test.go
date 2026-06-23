// Teleport
// Copyright (C) 2026 Gravitational, Inc.
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

package appaccess_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/juliaogris/teleport-expressions/appaccess"
)

func mustInput(t *testing.T, raw string) appaccess.Input {
	t.Helper()
	var in appaccess.Input
	require.NoError(t, yaml.Unmarshal([]byte(raw), &in))
	return in
}

func TestEvaluate(t *testing.T) {
	const captureRule = `
role_name: tester
app_resources:
  - paths: ["/api/v4/projects/{project}/**"]
    methods: [GET]
    where: contains(user.traits["allowed_projects"], vars.project)
`
	allowedInput := mustInput(t, `
request:
  method: GET
  path: /api/v4/projects/alpha/issues
identity:
  name: alice
  roles: [tester]
  traits:
    allowed_projects: [alpha, beta]
`)

	tests := []struct {
		name     string
		rule     string
		in       appaccess.Input
		want     bool
		wantVars map[string]string
		wantErr  bool
	}{
		{
			name:     "path and trait match binds capture",
			rule:     captureRule,
			in:       allowedInput,
			want:     true,
			wantVars: map[string]string{"project": "alpha"},
		},
		{
			name: "capture not in trait denies",
			rule: captureRule,
			in: mustInput(t, `
request:
  method: GET
  path: /api/v4/projects/forbidden/issues
identity:
  name: alice
  roles: [tester]
  traits:
    allowed_projects: [alpha, beta]
`),
			want: false,
		},
		{
			name: "wrong method denies",
			rule: captureRule,
			in: mustInput(t, `
request:
  method: POST
  path: /api/v4/projects/alpha/issues
identity:
  name: alice
  roles: [tester]
  traits:
    allowed_projects: [alpha]
`),
			want: false,
		},
		{
			name: "where holds the whole predicate",
			rule: "role_name: tester\napp_resources:\n  - where: path.match(greedy())",
			in:   allowedInput,
			want: true,
		},
		{
			name: "additive: second rule in the list matches",
			rule: `
role_name: tester
app_resources:
  - paths: ["/admin/**"]
  - paths: ["/api/v4/projects/{project}/**"]
    methods: [GET]
    where: contains(user.traits["allowed_projects"], vars.project)
`,
			in:       allowedInput,
			want:     true,
			wantVars: map[string]string{"project": "alpha"},
		},
		{
			name:    "both surfaces is a compile error",
			rule:    "role_name: tester\napp_resources:\n  - paths: [/api/**]\n    pred: path.match(greedy())",
			in:      allowedInput,
			wantErr: true,
		},
		{
			name: "empty list denies",
			rule: "role_name: tester\napp_resources: []",
			in:   allowedInput,
			want: false,
		},
		{
			// capture_encoded binds an encoded segment raw as one token, so an
			// encoded GitLab-style id matches and binds the whole value. The match
			// opts into the encoded separator with allow_encoded(set("/")).
			name: "capture_encoded binds an encoded id raw",
			rule: "role_name: tester\napp_resources:\n  - pred: |-\n      path.match(literal(\"files\", capture_encoded(\"x\", set(\"/\"))), allow_encoded(set(\"/\")))",
			in: mustInput(t, `
request:
  method: GET
  path: /files/a%2Fb
identity:
  name: alice
  roles: [tester]
`),
			want:     true,
			wantVars: map[string]string{"x": "a%2Fb"},
		},
		{
			name: "user lacks the role denies",
			rule: captureRule,
			in: mustInput(t, `
request:
  method: GET
  path: /api/v4/projects/alpha/issues
identity:
  name: alice
  roles: [other]
  traits:
    allowed_projects: [alpha, beta]
`),
			want: false,
		},
		{
			name:    "missing role_name errors",
			rule:    "app_resources:\n  - paths: [\"/api/**\"]",
			in:      allowedInput,
			wantErr: true,
		},
		{
			name: "missing identity roles errors",
			rule: captureRule,
			in: mustInput(t, `
request:
  method: GET
  path: /api/v4/projects/alpha/issues
identity:
  name: alice
  traits:
    allowed_projects: [alpha, beta]
`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := appaccess.Evaluate(tt.rule, tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Allowed)
			if tt.wantVars != nil {
				require.Equal(t, tt.wantVars, got.Vars)
			}
		})
	}
}
