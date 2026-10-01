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

package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// preToolHook configures one pre_tool hook on every tool printing reply.
func preToolHook(reply string, more ...func(*config.Config)) func(*config.Config) {
	return func(c *config.Config) {
		c.Hooks.PreTool = []config.HookConfig{{Match: "*", Command: "echo '" + reply + "'"}}
		for _, m := range more {
			m(c)
		}
	}
}

// pre_tool hooks may rewrite a call's arguments, let it through, send it to
// the user, or add context for the agent; the deny rules apply to the
// rewritten call.
func TestEnginePreToolHookDecisions(t *testing.T) {
	cases := []struct {
		name    string
		tool    string
		reply   string
		cfg     func(*config.Config)
		wantErr string
		check   func(t *testing.T, resp map[string]any)
	}{
		{
			name:  "updated args and context",
			reply: `{"decision":"allow","updated_args":{"directory":"sub"},"additional_context":"note from the hook"}`,
			check: func(t *testing.T, resp map[string]any) {
				assert.Equal(t, "note from the hook", resp["hook_context"])
				assert.Contains(t, resp["directory"], "sub", "the call listed the rewritten path: %v", resp)
			},
		},
		{
			name:  "context on a failed call",
			reply: `{"updated_args":{"bogus":1},"additional_context":"note from the hook"}`,
			check: func(t *testing.T, resp map[string]any) {
				assert.Equal(t, "note from the hook", resp["hook_context"])
				assert.NotEmpty(t, resp["error"], "the tool's error must stay visible: %v", resp)
			},
		},
		{
			name:    "updated args denied",
			tool:    "activate_skill",
			reply:   `{"updated_args":{"name":"secret-x"}}`,
			cfg:     func(c *config.Config) { c.Permissions.Deny = []string{"skill(secret*)"} },
			wantErr: "denied by the permission rule",
		},
		{
			name:    "ask without anyone to ask",
			reply:   `{"decision":"ask"}`,
			cfg:     func(c *config.Config) { c.Blitz.AutoApprove, c.Blitz.PermissionMode = false, "dont-ask" },
			wantErr: "list_files",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			more := []func(*config.Config){}
			if c.cfg != nil {
				more = append(more, c.cfg)
			}
			tool := c.tool
			if tool == "" {
				tool = "list_files"
			}
			f := newEngineWith(t, fixtureOpts{cfg: preToolHook(c.reply, more...)},
				toolCall(tool, map[string]any{}), textContent("done"))
			sub := filepath.Join(f.cfg.Tools.WorkspaceDir, "sub")
			require.NoError(t, os.MkdirAll(sub, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(sub, "inside.txt"), nil, 0o644))
			resps, err := functionResponses(t, f.eng, "s", "go")
			require.NoError(t, err)
			resp := resps[tool]
			if c.wantErr != "" {
				assert.Contains(t, resp["error"], c.wantErr)
				return
			}
			c.check(t, resp)
		})
	}
}

// A deny rule naming a skill refuses the call before it runs.
func TestEngineDenyRuleRefusesTool(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{cfg: func(c *config.Config) { c.Permissions.Deny = []string{"skill(secret*)"} }},
		toolCall("activate_skill", map[string]any{"name": "secret-x"}), textContent("done"))
	resps, err := functionResponses(t, f.eng, "s", "go")
	require.NoError(t, err)
	assert.Contains(t, resps["activate_skill"]["error"], "denied by the permission rule deny")
}

// argsSummary is the call's arguments as JSON, or nothing when they can't
// be encoded.
func TestArgsSummary(t *testing.T) {
	assert.Equal(t, `{"a":1}`, argsSummary(map[string]any{"a": 1}))
	assert.Empty(t, argsSummary(map[string]any{"f": func() {}}))
}
