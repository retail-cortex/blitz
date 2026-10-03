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
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestPermissionRulesLiveAndSaved(t *testing.T) {
	create := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "docs/a.md", "content": "x"}}}}}
	search := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_agents", Args: map[string]any{}}}}}
	off := false
	w, _ := openTestWith(t, func(c *config.Config) {
		c.Permissions.Deny = []string{"shell(rm *)"}
		c.Permissions.ReadOnlyDefaults = &off
	}, create, text("done"), search, text("done"))
	cfgDir := config.ConfigDir("")
	os.MkdirAll(cfgDir, 0o700)
	os.WriteFile(filepath.Join(cfgDir, ".env.toml"), []byte("# mine\n[permissions]\nread_only_defaults = false\ndeny = [\"shell(rm *)\"]\n"), 0o600)

	got := w.ListPermissionRules()
	require.Len(t, got, 1, "rules %+v", got)
	require.Equal(t, "shell(rm *)", got[0].Rule, "rules %+v", got)
	require.Equal(t, "global", got[0].Source, "rules %+v", got)
	// A session allow rule applies at once: no approver, yet the write runs.
	res, err := w.AddPermissionRule("allow", "Edit(docs/**)", api.ScopeGlobal)
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
	_, err = w.AddPermissionRule("deny", "read(secrets/**)", api.ScopeSession)
	assert.Error(t, err, "an unsaved read rule was accepted")
	res, err = w.AddPermissionRule("deny", "read(secrets/**)", api.ScopeGlobal)
	assert.NoError(t, err, "saved read rule: %+v", res)
	assert.True(t, res.NextStart, "saved read rule: %+v %v", res, err)
	_, err = w.AddPermissionRule("deny", "what(x)", api.ScopeSession)
	assert.ErrorIs(t, err, api.ErrBadRule, "bad rule: %v", err)
	// Remove, also from the file.
	res, _ = w.RemovePermissionRule("write(docs/**)", api.ScopeGlobal)
	assert.Equal(t, 1, res.Removed, "remove: %+v", res)
	data, _ = os.ReadFile(filepath.Join(cfgDir, ".env.toml"))
	assert.NotContains(t, string(data), "write(docs/**)", "rule still saved:\n%s", data)
}

// A rule saved for the workspace goes to its own settings, not the global
// ones, applies at once, and is listed as the workspace's; the built-in
// read-only rules are listed as built-in.
func TestPermissionRuleSavedForTheWorkspace(t *testing.T) {
	w, _ := openTestWith(t, nil)
	res, err := w.AddPermissionRule("allow", "Bash(make)", api.ScopeWorkspace)
	require.NoError(t, err)
	require.NoError(t, res.Saved.Err)
	assert.Contains(t, res.Saved.Path, filepath.Join(".blitz", "workspaces"), "saved to %s", res.Saved.Path)
	global, _, _ := config.ScopePermissions("", "")
	assert.Empty(t, global.Allow, "the global settings changed")

	sources := map[string]string{}
	for _, r := range w.ListPermissionRules() {
		sources[r.Rule] = r.Source
	}
	assert.Equal(t, "workspace", sources["shell(make)"])
	assert.Equal(t, "built-in", sources["shell(ls)"])
	assert.Equal(t, tools.VerdictAutoApprove, w.tools.CommandPolicy().Evaluate("make build").Verdict, "the rule doesn't apply")

	_, err = w.RemovePermissionRule("shell(make)", api.ScopeWorkspace)
	require.NoError(t, err)
	own, _, _ := config.ScopePermissions("", w.Dir())
	assert.Empty(t, own.Allow)
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

// A rule that can't be saved still applies for the session, and says why
// it wasn't saved.
func TestPermissionRuleKeptWhenSavingFails(t *testing.T) {
	w := openTest(t)
	rules, _ := w.settingsFiles()
	writeSettings(t, rules[1], "[permissions\n") // the workspace's settings no longer parse
	res, err := w.AddPermissionRule("deny", "write(secret/**)", api.ScopeWorkspace)
	require.NoError(t, err)
	assert.Error(t, res.Saved.Err)
	e, _ := w.tools.Rules().Decide(tools.RuleWrite, []string{"secret/a"})
	assert.Equal(t, tools.EffectDeny, e)
}

// A rule saved but not read back (the workspace's settings no longer
// parse) applies for the session, with a warning.
func TestPermissionRuleSavedButNotReloaded(t *testing.T) {
	w := openTest(t)
	var warnings []string
	w.warn = func(s string) { warnings = append(warnings, s) }
	rules, _ := w.settingsFiles()
	writeSettings(t, rules[1], "[permissions\n")
	res, err := w.AddPermissionRule("deny", "write(secret/**)", api.ScopeGlobal)
	require.NoError(t, err)
	require.NoError(t, res.Saved.Err)
	e, _ := w.tools.Rules().Decide(tools.RuleWrite, []string{"secret/a"})
	assert.Equal(t, tools.EffectDeny, e)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "reading the saved permission rules again")
}

// A reload with a rule that can't be read keeps the rest, and says why
// until a reload applies them all.
func TestPermissionsProblem(t *testing.T) {
	w := openTest(t)
	require.NoError(t, w.PermissionsProblem())

	bad := *w.Config()
	bad.Permissions.Deny = []string{"shell(rm *)", "shell(re:[)"}
	assert.Error(t, w.ReloadPermissions(&bad))
	require.Error(t, w.PermissionsProblem())
	assert.Contains(t, w.PermissionsProblem().Error(), "re:[")
	var rules []string
	for _, r := range w.ListPermissionRules() {
		rules = append(rules, r.Effect+" "+r.Rule)
	}
	assert.Contains(t, rules, "deny shell(rm *)", "the good rule applies")

	good := *w.Config()
	good.Permissions.Deny = []string{"shell(rm *)"}
	require.NoError(t, w.ReloadPermissions(&good))
	assert.NoError(t, w.PermissionsProblem())
}
