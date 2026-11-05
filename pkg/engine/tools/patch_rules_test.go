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
	"os"
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

// write(...) and delete(...) patterns take the blocked paths' forms: a
// name without a slash at any depth, an absolute or ~ location wherever
// the target is shown, a slash relative to the workspace (N26
// engine-tools-2#5).
func TestPathRuleForms(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	ws := filepath.Join(home, "proj")
	tests := []struct {
		name, rule, target string
		want               bool
	}{
		{"a name at the top", "*.pem", "x.pem", true},
		{"a name deeper down", "*.pem", "certs/x.pem", true},
		{"a name as a folder", "secrets", "a/secrets/k.txt", true},
		{"a name that doesn't fit", "*.pem", "certs/x.pem.txt", false},
		{"a ~ location inside the workspace", "~/proj/secrets/**", "secrets/x", true},
		{"an absolute location inside the workspace", ws + "/secrets", "secrets/x", true},
		{"an absolute location, absolute target", ws + "/secrets", ws + "/secrets/x", true},
		{"an absolute location elsewhere", "/etc/**", "secrets/x", false},
		{"a relative path with a slash", "docs/**", "docs/a/b.md", true},
		{"a relative path, not deeper", "docs/**", "src/docs/a.md", false},
		{"a relative path, absolute target in the workspace", "docs/**", ws + "/docs/a.md", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules, err := NewPermissionRules(config.PermissionsConfig{Deny: []string{"write(" + tt.rule + ")"}}, "test")
			require.NoError(t, err)
			rules.SetWorkspace(ws)
			got, _ := rules.Decide(RuleWrite, []string{tt.target})
			assert.Equal(t, tt.want, got == EffectDeny)
		})
	}
}
