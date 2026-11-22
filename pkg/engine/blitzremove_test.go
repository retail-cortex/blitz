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
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/gates"
)

// Removing this workspace's items: asked with the file that goes, then
// gone; what isn't there says so with nothing asked (CLP-34).
func TestBlitzRemoves(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "s3cret", nil)
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "recipes.db"), nil, 0o600))
	saves := []struct {
		cmd  string
		args map[string]any
	}{
		{"rule save", map[string]any{"name": "tidy", "description": "Tidy code", "text": "Keep it tidy."}},
		{"guardrail save", map[string]any{"name": "nosecrets", "description": "No secrets", "stage": "output", "check": "No secrets in answers."}},
		{"skill save", map[string]any{"name": "greet", "description": "Greets people", "instructions": "Greet them warmly."}},
		{"secret set", map[string]any{"name": "TOKEN"}},
		{"connector save", map[string]any{"kind": "sqlite", "name": "recipes", "read_only": true, "settings": map[string]any{"database": "recipes.db"}}},
	}
	for _, s := range saves {
		_, err := runCmd(t, w, s.cmd, s.args)
		require.NoError(t, err, s.cmd)
	}

	out, err := runCmd(t, w, "connector test", map[string]any{"name": "recipes"})
	require.NoError(t, err)
	assert.Equal(t, true, out.(map[string]any)["ok"])

	removes := []struct{ cmd, name, inDiff string }{
		{"rule remove", "tidy", "-Keep it tidy."},
		{"guardrail remove", "nosecrets", "-No secrets in answers."},
		{"skill remove", "greet", "-Greet them warmly."},
		{"secret remove", "TOKEN", ""},
		{"connector remove", "recipes", "recipes.db"},
	}
	for _, r := range removes {
		t.Run(r.cmd, func(t *testing.T) {
			before := len(*asked)
			_, err := runCmd(t, w, r.cmd, map[string]any{"name": r.name})
			require.NoError(t, err)
			require.Len(t, *asked, before+1, "asked first")
			if r.inDiff != "" {
				assert.Contains(t, (*asked)[before].Diff, r.inDiff, "with what goes")
			}
			_, err = runCmd(t, w, r.cmd, map[string]any{"name": r.name})
			assert.Error(t, err, "gone")
			assert.Len(t, *asked, before+1, "nothing asked for what isn't there")
		})
	}
	_, ok := config.Vault(w.cfg.Dir).Lookup(config.SecretScope(w.cfg.Dir, w.Dir()), "TOKEN")
	assert.False(t, ok)
	_, err = runCmd(t, w, "connector test", map[string]any{"name": "recipes"})
	assert.ErrorContains(t, err, "no connection")
	_, err = runCmd(t, w, "workflow remove", map[string]any{"name": "nope"})
	assert.ErrorContains(t, err, "no workflow")
}

// A changed rule or guardrail shows what changes, not a new file.
func TestRuleSaveDiffsTheOldRule(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	_, err := runCmd(t, w, "rule save", map[string]any{"name": "tidy", "description": "Tidy", "text": "Keep it tidy."})
	require.NoError(t, err)
	_, err = runCmd(t, w, "rule save", map[string]any{"name": "tidy", "description": "Tidy", "text": "Keep it very tidy."})
	require.NoError(t, err)
	diff := (*asked)[len(*asked)-1].Diff
	assert.Contains(t, diff, "-Keep it tidy.")
	assert.Contains(t, diff, "+Keep it very tidy.")
	assert.NotContains(t, diff, "/dev/null")
}

func TestFileBody(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "r.md")
	require.NoError(t, os.WriteFile(p, []byte("---\nname: x\n---\n\nThe body.\n"), 0o600))
	assert.Equal(t, "The body.\n", fileBody(p))
	require.NoError(t, os.WriteFile(p, []byte("No front matter."), 0o600))
	assert.Equal(t, "No front matter.", fileBody(p))
	assert.Empty(t, fileBody(filepath.Join(dir, "none.md")))
}

func TestWorkflowRemove(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	_, err := runCmd(t, w, "workflow save", map[string]any{"workflow": map[string]any{"name": "nightly", "nodes": []any{map[string]any{"id": "a", "type": "agent", "instructions": "Check."}}}})
	require.NoError(t, err)
	_, err = runCmd(t, w, "workflow remove", map[string]any{"name": "nightly"})
	require.NoError(t, err)
	assert.Contains(t, (*asked)[len(*asked)-1].Diff, "Check.")
	assert.Empty(t, w.Workflows())
}

// Declined, a remove leaves the item where it was.
func TestDeclinedRemoveKeepsIt(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	_, err := runCmd(t, w, "rule save", map[string]any{"name": "tidy", "description": "Tidy", "text": "Keep it tidy."})
	require.NoError(t, err)
	w.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionDeny, nil }, nil)
	_, err = runCmd(t, w, "rule remove", map[string]any{"name": "tidy"})
	require.Error(t, err)
	assert.True(t, slices.ContainsFunc(w.Rules(), func(r gates.Rule) bool { return r.Name == "tidy" }), "still there")
}

// An agent of the workspace's own goes, asked with its file; a built-in
// one doesn't.
func TestAgentRemove(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	_, err := runCmd(t, w, "agent save", map[string]any{"name": "reviewer", "description": "Reviews", "instructions": "Review the diff."})
	require.NoError(t, err)
	_, err = runCmd(t, w, "agent remove", map[string]any{"name": "reviewer"})
	require.NoError(t, err)
	assert.Contains(t, (*asked)[len(*asked)-1].Diff, "Review the diff.")
	assert.False(t, slices.ContainsFunc(w.ListAgents(), func(a api.AgentInfo) bool { return a.Name == "reviewer" }), "gone")
	before := len(*asked)
	_, err = runCmd(t, w, "agent remove", map[string]any{"name": "blitz"})
	assert.ErrorContains(t, err, "built-in")
	_, err = runCmd(t, w, "agent remove", map[string]any{"name": "reviewer"})
	assert.ErrorContains(t, err, "no agent")
	assert.Len(t, *asked, before, "nothing asked")
}
