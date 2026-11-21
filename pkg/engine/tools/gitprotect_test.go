// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
)

// The files git runs commands from, in a repository at any depth and its
// submodules', and only those (SH-23b).
func TestIsGitControl(t *testing.T) {
	for p, want := range map[string]bool{
		".git/config":                       true,
		".git/config.worktree":              true,
		".git/hooks":                        true,
		".git/hooks/pre-commit":             true,
		".git/info/attributes":              true,
		".git/modules/sub/config":           true,
		".git/modules/a/b/hooks/post-merge": true,
		".git/HEAD":                         false,
		".git/index":                        false,
		".git/objects/ab/cdef":              false,
		".git/refs/heads/main":              false,
		".git/info/exclude":                 false,
		"vendor/lib/.git/config":            false, // a repository the agent cloned
		"config":                            false,
		"git/config":                        false,
		".github/hooks/x":                   false,
	} {
		assert.Equal(t, want, isGitControl(p), p)
	}
	if caseInsensitiveFS {
		assert.True(t, isGitControl(".GIT/hooks/pre-commit"), "another spelling of the same file")
	}
}

// The agent's file tools refuse to write them, directly or through a
// link; git's other files are written as usual.
func TestWorkspaceRefusesGitControl(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, ".git", "config"), "[core]\n")
	for _, p := range []string{".git/config", ".git/hooks/pre-commit", ".git/info/attributes"} {
		_, err := ws.WritablePath(p)
		assert.ErrorIs(t, err, ErrReadOnlyPath, p)
		assert.ErrorContains(t, err, "git's config, hooks and attributes", p)
	}
	require.NoError(t, os.Symlink(filepath.Join(dir, ".git", "config"), filepath.Join(dir, "innocent.ini")))
	_, err := ws.WritablePath("innocent.ini")
	assert.ErrorIs(t, err, ErrReadOnlyPath, "through a link")
	_, err = ws.WritablePath(".git/HEAD")
	assert.NoError(t, err)
	_, err = ws.WritablePath("vendor/lib/.git/config")
	assert.NoError(t, err, "a repository cloned further down is the agent's")
}

// A tool call a local model wrote as text is asked about even in bypass
// mode, where a real call runs unasked (spec_056 CA-50a; security review
// runtime#4).
func TestTextCallsAreAlwaysAsked(t *testing.T) {
	req := api.ApprovalRequest{Tool: "run_shell_command", Kind: api.ActionCommand, Detail: "make build", Targets: []string{"make build"}}
	for _, mode := range []api.PermissionMode{api.ModeBypass} {
		t.Run(string(mode), func(t *testing.T) {
			h, reqs := decisionHooks(api.DecisionOnce)
			h.policy.Mode = mode
			h.SetRules(rulesOf(t, []string{"shell(make *)"}, nil, nil))
			real := agent.NewToolContext(&mockIC{ctx: context.Background()}, "adk-123", &session.EventActions{}, nil)
			require.NoError(t, h.Approve(real, req))
			assert.Empty(t, *reqs, "a real call in bypass mode")
			fromText := agent.NewToolContext(&mockIC{ctx: context.Background()}, TextCallPrefix+"1", &session.EventActions{}, nil)
			require.NoError(t, h.Approve(fromText, req))
			require.Len(t, *reqs, 1, "a call read from text is asked")
		})
	}
}

// Blitz's own settings are blocked wherever they were read from, not only
// ~/.blitz (security review enterprise#4: MODENV_PREFIX or --config left
// them readable).
func TestSettingsBlockedWhereverTheyAre(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env.toml"), "[llm.gemini]\napi_key = \"secret\"\n")
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Dir = dir
	r, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })
	_, blocked := r.Workspace().Blocked().Match(filepath.Join(dir, ".env.toml"))
	assert.True(t, blocked, "the settings file")
	_, blocked = r.Workspace().Blocked().Match(filepath.Join(dir, "workspaces", "x", ".env.toml"))
	assert.True(t, blocked)
	assert.Empty(t, settingsBlocked(""))
}
