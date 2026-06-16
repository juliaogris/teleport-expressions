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

package labelexpr_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/juliaogris/teleport-expressions/labelexpr"
)

func TestEvaluate(t *testing.T) {
	input := labelexpr.Input{
		Labels: map[string]string{
			"env":   "prod",
			"owner": "devs",
			"team":  "security",
		},
		Username: "alice@example.com",
		Traits: map[string][]string{
			"groups": {"devs", "security"},
		},
	}

	tests := []struct {
		name    string
		expr    string
		want    bool
		wantErr bool
	}{
		{
			name: "label equality",
			expr: `labels["env"] == "prod"`,
			want: true,
		},
		{
			name: "label inequality",
			expr: `labels["env"] == "staging"`,
			want: false,
		},
		{
			name: "contains trait and label",
			expr: `contains(user.spec.traits["groups"], labels["owner"])`,
			want: true,
		},
		{
			name: "labels_matching glob",
			expr: `contains(labels_matching("*"), "prod")`,
			want: true,
		},
		{
			name: "regexp.match miss",
			expr: `regexp.match(set(labels["env"]), "stag.*")`,
			want: false,
		},
		{
			name: "email.local",
			expr: `contains(email.local(set(user.metadata.name)), "alice")`,
			want: true,
		},
		{
			name: "boolean composition",
			expr: `labels["env"] == "prod" && contains(user.spec.traits["groups"], "security")`,
			want: true,
		},
		{
			name:    "parse error",
			expr:    `labels["env"`,
			wantErr: true,
		},
		{
			name:    "type error",
			expr:    `labels["env"]`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := labelexpr.Evaluate(tt.expr, input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
