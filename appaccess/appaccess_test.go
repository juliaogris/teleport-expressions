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
paths: ["/api/v4/projects/{project}/**"]
methods: [GET]
where: contains(user.traits["allowed_projects"], vars.project)
`
	allowedInput := mustInput(t, `
request:
  method: GET
  path: /api/v4/projects/alpha/issues
identity:
  name: alice
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
  traits:
    allowed_projects: [alpha]
`),
			want: false,
		},
		{
			name: "bare predicate form",
			rule: `pred: path.match(greedy())`,
			in:   allowedInput,
			want: true,
		},
		{
			name:    "both surfaces is a compile error",
			rule:    "paths: [/api/**]\npred: path.match(greedy())",
			in:      allowedInput,
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
