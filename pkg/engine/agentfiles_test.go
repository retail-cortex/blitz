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
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// Agent files added, edited and removed in .agents/agents apply without
// reopening the workspace, models included.
func TestAgentFilesReload(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	dir := w.ProjectAgentsDir()
	require.Equal(t, filepath.Join(w.Dir(), ".agents", "agents"), dir)
	write := func(front string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(dir, 0o755))
		path := filepath.Join(dir, "quote.md")
		require.NoError(t, os.WriteFile(path, []byte("---\nname: quote\ndescription: Quotes\ntools: []\n"+front+"---\nYou write quotes.\n"), 0o644))
		later := time.Now().Add(time.Duration(len(front)+1) * time.Minute) // a new time even for the same size
		require.NoError(t, os.Chtimes(path, later, later))
	}
	find := func() (api.AgentInfo, bool) {
		i := slices.IndexFunc(w.ListAgents(), func(a api.AgentInfo) bool { return a.Name == "quote" })
		if i < 0 {
			return api.AgentInfo{}, false
		}
		return w.ListAgents()[i], true
	}

	_, ok := find()
	require.False(t, ok)

	write("")
	info, ok := find()
	require.True(t, ok, "a new file's agent is offered")
	assert.Empty(t, info.PinnedModel)

	write("default_model: claude-haiku-4-5\n")
	info, _ = find()
	assert.Equal(t, "claude-haiku-4-5", info.PinnedModel, "its default_model applies")

	_, err := w.SetAgent(ctx, "quote")
	require.NoError(t, err)
	write("")
	info, _ = find()
	assert.Empty(t, info.PinnedModel, "without default_model it's back on the configured model")
	assert.True(t, info.Active)

	require.NoError(t, os.Remove(filepath.Join(dir, "quote.md")))
	_, ok = find()
	assert.False(t, ok, "a removed file's agent is gone")
	assert.Equal(t, "blitz", w.ActiveAgent().Name, "the active agent falls back to blitz")
}

// Blitz routes to the user's agents (~/.blitz/agents) and the workspace's
// (.agents/agents) as it does to the built-in ones: transfer_to_agent hands
// them the turn, and invoke_agent runs them and returns what they said.
func TestBlitzRoutesToGlobalAndWorkspaceAgents(t *testing.T) {
	type place struct {
		name string
		dir  func(c *config.Config) string
	}
	places := []place{
		{"global", func(*config.Config) string { return config.ExpandHome(config.UserAgentsDir) }},
		{"workspace", func(c *config.Config) string { return filepath.Join(c.Tools.WorkspaceDir, config.ProjectAgentsDir) }},
	}
	tests := []struct {
		name    string
		replies func(agent string) []*genai.Content
		check   func(t *testing.T, agent string, authors []string, results []map[string]any)
	}{
		{"transfer_to_agent", func(agent string) []*genai.Content {
			return []*genai.Content{toolCall("transfer_to_agent", map[string]any{"agent_name": agent}), text(agent + " here")}
		}, func(t *testing.T, agent string, authors []string, _ []map[string]any) {
			require.NotEmpty(t, authors)
			assert.Equal(t, agent, authors[len(authors)-1], "authors %v", authors)
		}},
		{"invoke_agent", func(agent string) []*genai.Content {
			return []*genai.Content{toolCall("invoke_agent", map[string]any{"agent_name": agent, "prompt": "check it"}), text(agent + " says fine"), text("done")}
		}, func(t *testing.T, agent string, authors []string, results []map[string]any) {
			require.Len(t, results, 1)
			assert.Equal(t, agent+" says fine", results[0]["response"], "result %v", results[0])
			assert.Empty(t, results[0]["error"], "result %v", results[0])
			assert.Equal(t, "blitz", authors[len(authors)-1], "blitz answers after the call: %v", authors)
		}},
	}
	for _, p := range places {
		for _, tt := range tests {
			t.Run(p.name+"/"+tt.name, func(t *testing.T) {
				agent := p.name + "-helper"
				w, _ := openTestWith(t, func(c *config.Config) {
					dir := p.dir(c)
					require.NoError(t, os.MkdirAll(dir, 0o755))
					require.NoError(t, os.WriteFile(filepath.Join(dir, agent+".md"),
						[]byte("---\nname: "+agent+"\ndescription: Helps\ntools: []\n---\nYou help.\n"), 0o644))
				}, tt.replies(agent)...)
				require.True(t, slices.ContainsFunc(w.ListAgents(), func(a api.AgentInfo) bool { return a.Name == agent }), "%s is offered", agent)
				s, _ := w.NewSession()
				var authors []string
				var results []map[string]any
				_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "go"}, func(e api.Event) {
					if e.Text != nil && e.Author != "" {
						authors = append(authors, e.Author)
					}
					if e.ToolResult != nil && e.ToolResult.Name == "invoke_agent" {
						results = append(results, e.ToolResult.Result)
					}
				})
				require.NoError(t, err)
				tt.check(t, agent, authors, results)
			})
		}
	}
}
