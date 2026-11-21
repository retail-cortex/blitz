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

// A profile's own sites reach the private network by name only: never a
// literal address, never a site that names no domain (BP-04a; security
// review electron#1).
func TestProfileSiteHost(t *testing.T) {
	is := profileSiteHost([]string{"*", "10.*", "192.168.1.5", "intranet", "*.corp.example", "wiki.example.org"})
	for host, want := range map[string]bool{
		"wiki.example.org":  true,
		"git.corp.example":  true,
		"attacker.example":  false,
		"192.168.1.5":       false,
		"10.0.0.1":          false,
		"[fd00::1]":         false,
		"intranet":          false,
		"anything.anywhere": false,
	} {
		assert.Equal(t, want, is(host), host)
	}
}

// The person's own file operations refuse .git in any spelling the disk
// takes for it, and an upload replacing a file keeps its mode (security
// review files#8, files#10).
func TestUserFilesGitSpellingAndUploadMode(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	ctx := context.Background()
	if caseInsensitiveFS {
		_, err := UserPath(".GIT/hooks/pre-commit")
		assert.ErrorIs(t, err, ErrGitDir)
	}
	_, err := UserPath(".gitignore")
	assert.NoError(t, err, "a name that starts the same isn't .git")
	writeFile(t, filepath.Join(dir, "secret.txt"), "old")
	require.NoError(t, os.Chmod(filepath.Join(dir, "secret.txt"), 0o600))
	tmp, err := ws.UserCreateUpload("secret.txt")
	require.NoError(t, err)
	require.NoError(t, ws.UserAppend(tmp, []byte("new")))
	require.NoError(t, ws.UserFinishUpload(ctx, tmp, "secret.txt", true))
	info, err := os.Stat(filepath.Join(dir, "secret.txt"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "kept, not 0644")
}

// A folder copied into itself by another spelling (a case variant on a
// disk that ignores case, a linked parent) is refused, not copied for
// ever (security review files#9).
func TestUserCopyIntoItselfBySpelling(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	ctx := context.Background()
	writeFile(t, filepath.Join(dir, "docs", "a.md"), "a")
	require.NoError(t, os.Symlink("docs", filepath.Join(dir, "alias")))
	assert.ErrorContains(t, ws.UserCopy(ctx, "docs", filepath.Join("alias", "copy")), "into itself")
	if caseInsensitiveFS {
		assert.ErrorContains(t, ws.UserCopy(ctx, "docs", filepath.Join("DOCS", "copy")), "into itself")
	}
	require.NoError(t, ws.UserCopy(ctx, "docs", "docs2"), "elsewhere is fine")
}

// Unsafe mode is the person's: refused while connected to an
// organization, and a setting asking for it then opens in default with
// the reason (APR-14e); without one it's taken.
func TestUnsafeModeAndTheOrganization(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Blitz.PermissionMode = "unsafe"
	r, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })
	assert.Equal(t, api.ModeUnsafe, r.Hooks().Mode())
	require.NoError(t, r.SetPermissionMode(api.ModeDefault))
	require.NoError(t, r.SetPermissionMode(api.ModeUnsafe))

	org := config.DefaultConfig()
	org.Tools.WorkspaceDir = t.TempDir()
	org.Blitz.PermissionMode = "unsafe"
	org.Enterprise.URL = "https://blitz.example.com"
	ro, err := NewRegistry(org, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { ro.Close() })
	assert.Equal(t, api.ModeDefault, ro.Hooks().Mode())
	assert.ErrorIs(t, ro.ModeNote(), api.ErrUnsafeNotHere)
	assert.ErrorIs(t, ro.SetPermissionMode(api.ModeUnsafe), api.ErrUnsafeNotHere)
	// Nothing asked in it, as in bypass.
	h, reqs := decisionHooks(api.DecisionDeny)
	h.policy.Mode = api.ModeUnsafe
	require.NoError(t, h.Approve(context.Background(), api.ApprovalRequest{Tool: "run_shell_command", Kind: api.ActionCommand, Detail: "make"}))
	assert.Empty(t, *reqs)
}
