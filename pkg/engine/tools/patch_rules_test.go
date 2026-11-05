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
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
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

// The web settings are made once, and skill scripts' fetches get all of
// them, web.max_bytes included (N26 engine-tools-2#17).
func TestRegistryWebSettingsOnce(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.UCToolsDir = filepath.Join(t.TempDir(), "uc")
	cfg.Sandbox.Shell = "off"
	cfg.Web.Enabled, cfg.Skills.Enabled = true, true
	cfg.Web.MaxBytes = 12345
	cfg.Web.AllowPrivate = true
	prov, err := skills.NewProvider()
	require.NoError(t, err)
	r, err := NewRegistry(cfg, nil, prov)
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })
	require.NotNil(t, r.skillScripts)
	require.NotNil(t, r.skillScripts.web)
	tests := []struct {
		name string
		got  WebFetchConfig
	}{
		{"web_fetch's", r.web},
		{"skill scripts'", r.skillScripts.web.cfg},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, int64(12345), tt.got.MaxBytes)
			assert.True(t, tt.got.AllowPrivate)
			assert.Same(t, r.rules, tt.got.Rules)
		})
	}
}

// Limits are named where they stop something, and the tools' defaults are
// the configuration's (N26 engine-tools-2#19, #20).
func TestLimitsAreNamed(t *testing.T) {
	tests := []struct {
		name, got, want string
	}{
		{"a shell time-out", timedOut(2 * time.Minute), "set timeout_seconds, up to 1800"},
		{"a workspace too large to copy", errWorkspaceTooBig.Error(), "over 512 MiB or 50000 files"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, tt.got, tt.want)
		})
	}
	d := config.DefaultConfig()
	assert.Equal(t, time.Duration(d.Tools.ShellTimeoutSeconds)*time.Second, defaultShellTimeout)
	assert.Equal(t, d.Tools.MaxFileSizeBytes, DefaultMaxFileSize)
	f := newWebFetcher(WebFetchConfig{})
	assert.Equal(t, d.Web.MaxBytes, f.cfg.MaxBytes)
	assert.Equal(t, time.Duration(d.Web.TimeoutSeconds)*time.Second, f.cfg.Timeout)
}

// A file apply_patch moves keeps its mode, an executable script its +x
// (N26 engine-tools-2#26).
func TestApplyPatchMoveKeepsMode(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
	}{
		{"an executable script", 0o755},
		{"a private file", 0o600},
		{"a plain file", 0o644},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws, dir := newTestWorkspace(t)
			src := filepath.Join(dir, "run.sh")
			writeFile(t, src, "echo hi\n")
			require.NoError(t, os.Chmod(src, tt.mode))
			rt := toolOf(t)(NewApplyPatchTool(ws, allowAll()))
			out := runTool(t, rt, map[string]any{"patch": "*** Begin Patch\n*** Update File: run.sh\n*** Move to: bin/run.sh\n@@\n-echo hi\n+echo hello\n*** End Patch"})
			require.Equal(t, true, out["success"], "%v", out)
			info, err := os.Stat(filepath.Join(dir, "bin", "run.sh"))
			require.NoError(t, err)
			assert.Equal(t, tt.mode, info.Mode().Perm())
		})
	}
}
