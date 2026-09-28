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

package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestPermissionRulesLiveAndSaved(t *testing.T) {
	create := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "docs/a.md", "content": "x"}}}}}
	search := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_agents", Args: map[string]any{}}}}}
	w, _ := openTestWith(t, func(c *config.Config) { c.Permissions.Deny = []string{"shell(rm *)"} }, create, text("done"), search, text("done"))
	cfgDir := config.ConfigDir("")
	os.MkdirAll(cfgDir, 0o700)
	os.WriteFile(filepath.Join(cfgDir, ".env.toml"), []byte("# mine\n[permissions]\ndeny = [\"shell(rm *)\"]\n"), 0o600)

	got := w.ListPermissionRules()
	require.Len(t, got, 1, "rules %+v", got)
	require.Equal(t, "shell(rm *)", got[0].Rule, "rules %+v", got)
	require.Equal(t, "config", got[0].Source, "rules %+v", got)
	// A session allow rule applies at once: no approver, yet the write runs.
	res, err := w.AddPermissionRule("allow", "Edit(docs/**)", true)
	require.NoError(t, err, "add: %+v", res)
	require.Equal(t, "write(docs/**)", res.Rule, "add: %+v %v", res, err)
	require.NoError(t, res.Saved.Err, "add: %+v %v", res, err)
	s, _ := w.NewSession()
	w.Run(context.Background(), s.ID, api.Turn{Text: "write"}, func(api.Event) {})
	_, err = os.Stat(filepath.Join(w.Dir(), "docs", "a.md"))
	assert.NoError(t, err, "allow rule didn't apply")
	data, _ := os.ReadFile(filepath.Join(cfgDir, ".env.toml"))
	assert.Contains(t, string(data), `allow = ["write(docs/**)"]`, "config file:\n%s", data)
	assert.Contains(t, string(data), "# mine", "config file:\n%s", data)
	// Read rules must be saved and apply from the next start.
	_, err = w.AddPermissionRule("deny", "read(secrets/**)", false)
	assert.Error(t, err, "an unsaved read rule was accepted")
	res, err = w.AddPermissionRule("deny", "read(secrets/**)", true)
	assert.NoError(t, err, "saved read rule: %+v", res)
	assert.True(t, res.NextStart, "saved read rule: %+v %v", res, err)
	_, err = w.AddPermissionRule("deny", "what(x)", false)
	assert.ErrorIs(t, err, api.ErrBadRule, "bad rule: %v", err)
	// Remove, also from the file.
	res, _ = w.RemovePermissionRule("write(docs/**)", true)
	assert.Equal(t, 1, res.Removed, "remove: %+v", res)
	data, _ = os.ReadFile(filepath.Join(cfgDir, ".env.toml"))
	assert.NotContains(t, string(data), "write(docs/**)", "rule still saved:\n%s", data)
}

// A deny rule naming a tool refuses the call before it runs.
func TestToolDenyRuleInTheEngine(t *testing.T) {
	agents := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_agents", Args: map[string]any{}}}}}
	w, _ := openTestWith(t, func(c *config.Config) { c.Permissions.Deny = []string{"list_agents"} }, agents, text("ok"))
	s, _ := w.NewSession()
	var result map[string]any
	w.Run(context.Background(), s.ID, api.Turn{Text: "who"}, func(e api.Event) {
		if e.ToolResult != nil {
			result = e.ToolResult.Result
		}
	})
	msg, _ := result["error"].(string)
	assert.Contains(t, msg, "deny list_agents", "list_agents wasn't refused: %v", result)
}
