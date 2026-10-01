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
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
)

// testRegistryConfig is a configuration whose state stays in temp dirs.
func testRegistryConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	cfg.Checkpoints.Dir = ""
	cfg.Images.Dir = t.TempDir()
	return cfg
}

// TestRegistryConfigErrors checks a registry with a bad setting fails to
// open, naming the problem.
func TestRegistryConfigErrors(t *testing.T) {
	aFile := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(aFile, []byte("{"), 0o600))
	for name, tc := range map[string]struct {
		set  func(*config.Config)
		want string
	}{
		"project permissions": {func(c *config.Config) { c.ProjectPermissions.Deny = []string{"bogus(x)"} }, "project's [permissions]"},
		"command pattern":     {func(c *config.Config) { c.Sandbox.Commands.Deny = []string{"re:("} }, "invalid command pattern"},
		"permission mode":     {func(c *config.Config) { c.Blitz.PermissionMode = "sideways" }, "permission_mode"},
		"approvals file":      {func(c *config.Config) { c.Tools.ApprovalsFile = aFile }, "corrupt approvals"},
		"image store":         {func(c *config.Config) { c.Images.Enabled, c.Images.Dir = true, filepath.Join(aFile, "img") }, "image store"},
		"mcp server":          {func(c *config.Config) { c.MCP.Servers = []config.MCPServerConfig{{}} }, "needs a name"},
		"hook":                {func(c *config.Config) { c.Hooks.Stop = []config.HookConfig{{Command: " "}} }, "needs command or args"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testRegistryConfig(t)
			tc.set(cfg)
			_, err := NewRegistry(cfg, nil, nil)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// TestRegistryOptionalFeatures checks settings that change what a registry
// sets up without failing it: shell-writable dirs, a checkpoint directory
// that can't be made, Google search.
func TestRegistryOptionalFeatures(t *testing.T) {
	cfg := testRegistryConfig(t)
	extra := t.TempDir()
	cfg.Sandbox.ShellWritablePaths = []string{extra, "missing-dir"}
	aFile := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(aFile, nil, 0o600))
	cfg.Checkpoints.Enabled, cfg.Checkpoints.Dir = true, aFile
	cfg.Web.Enabled, cfg.Web.SearchProvider = true, "google"
	cfg.Web.SearchAPIKey = "key"
	r, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	defer r.Close()
	assert.NotNil(t, r.Checkpoints(), "kept in memory when the directory fails")
	assert.Len(t, r.GetToolsForAgent([]string{"web_search"}), 1)
}

// TestRegistryAccessors checks the registry hands out what it built and
// takes replacements.
func TestRegistryAccessors(t *testing.T) {
	cfg := testRegistryConfig(t)
	cfg.Sandbox.Commands.Deny = []string{"rm"}
	r, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	defer r.Close()

	assert.Same(t, r.Rules(), r.Hooks().Rules())
	assert.Equal(t, VerdictDeny, r.CommandPolicy().Evaluate("rm x").Verdict)
	assert.Nil(t, r.SkillScripts(), "skills need a provider")
	assert.NotEmpty(t, r.WorkflowTools())
	summary := r.SandboxSummary()
	assert.Contains(t, summary, "deny: rm")

	var warned []string
	r.SetWarn(func(s string) { warned = append(warned, s) })
	r.ScriptHooks().Warn("x")
	assert.Equal(t, []string{"x"}, warned)

	m, err := NewMCPManager(nil, nil, nil)
	require.NoError(t, err)
	r.SetMCP(m)
	assert.Same(t, m, r.MCP())

	assert.Nil(t, r.EditDiagnostics(context.Background(), "read_file", nil, nil), "not an edit tool")
}

// TestRegistryWiresEventHooks checks permission_request hooks answer
// approvals and notification hooks hear the user being asked.
func TestRegistryWiresEventHooks(t *testing.T) {
	cfg := testRegistryConfig(t)
	out := filepath.Join(t.TempDir(), "notified")
	cfg.Hooks.PermissionRequest = []config.HookConfig{{Command: `echo '{"decision":"allow"}'`}}
	cfg.Hooks.Notification = []config.HookConfig{{Command: "cat > " + out}}
	r, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	defer r.Close()

	req := api.ApprovalRequest{Tool: "create_file", Kind: api.ActionWrite, Detail: "a.txt", Targets: []string{"a.txt"}}
	assert.NoError(t, r.Hooks().Approve(context.Background(), req), "the hook allows it")

	r.Hooks().Notify(context.Background(), "idle", "waiting")
	require.NoError(t, r.ScriptHooks().flush(context.Background()))
	b, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"message":"waiting"`)
}
