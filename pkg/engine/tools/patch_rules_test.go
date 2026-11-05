// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tools

import (
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A patch's deletions and moves answer to delete(...) rules, as
// delete_file does, in accept-edits too (N26 engine-tools-2#4).
func TestApplyPatchDeleteRules(t *testing.T) {
	tests := []struct {
		name, rule, patch string
		refused           bool
	}{
		{"deny refuses a deleted file", "deny", "*** Delete File: migrations/001.sql\n", true},
		{"deny refuses a moved-away file", "deny", "*** Update File: migrations/001.sql\n*** Move to: old/001.sql\n@@\n-create\n+create\n", true},
		{"deny leaves an edit alone", "deny", "*** Update File: migrations/001.sql\n@@\n-create\n+create table\n", false},
		{"ask asks, and nobody can answer", "ask", "*** Delete File: migrations/001.sql\n", true},
		{"ask leaves an edit alone", "ask", "*** Update File: migrations/001.sql\n@@\n-create\n+create table\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws, dir := newTestWorkspace(t)
			writeFile(t, filepath.Join(dir, "migrations", "001.sql"), "create\n")
			var pc config.PermissionsConfig
			if tt.rule == "deny" {
				pc.Deny = []string{"delete(migrations/**)"}
			} else {
				pc.Ask = []string{"delete(*)", "delete(**)"}
			}
			rules, err := NewPermissionRules(pc, "test")
			require.NoError(t, err)
			h := NewHooks(Policy{Mode: api.ModeAcceptEdits})
			h.SetRules(rules)
			rt := toolOf(t)(NewApplyPatchTool(ws, h))
			out := runTool(t, rt, map[string]any{"patch": "*** Begin Patch\n" + tt.patch + "*** End Patch"})
			if tt.refused {
				assert.NotEqual(t, true, out["success"], "%v", out)
				assert.FileExists(t, filepath.Join(dir, "migrations", "001.sql"))
			} else {
				assert.Equal(t, true, out["success"], "%v", out)
			}
		})
	}
}

func TestDecideRequestDeletes(t *testing.T) {
	rules, err := NewPermissionRules(config.PermissionsConfig{
		Allow: []string{"write(**)", "delete(tmp/**)"},
		Ask:   []string{"delete(docs/**)"},
		Deny:  []string{"delete(keep/**)"},
	}, "test")
	require.NoError(t, err)
	tests := []struct {
		name    string
		deletes []string
		want    Effect
	}{
		{"no deletions: the write rule", nil, EffectAllow},
		{"allowed deletions", []string{"tmp/x"}, EffectAllow},
		{"a deletion no rule allows", []string{"src/x"}, ""},
		{"an ask rule", []string{"docs/x"}, EffectAsk},
		{"a deny rule wins", []string{"docs/x", "keep/y"}, EffectDeny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := api.ApprovalRequest{Kind: api.ActionWrite, Targets: append([]string{"src/a.go"}, tt.deletes...), Deletes: tt.deletes}
			got, _ := rules.decideRequest(req)
			assert.Equal(t, tt.want, got)
		})
	}
}
