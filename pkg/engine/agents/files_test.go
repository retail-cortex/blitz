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

package agents

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noBuiltins(string) bool { return false }

func TestMarshalRoundTrip(t *testing.T) {
	spec := &AgentSpec{
		Name: "quote-writer", DisplayName: "Quote Writer", Description: "Writes a quote",
		Tools: []string{"read_file", "create_file"}, DefaultModel: "anthropic/claude-sonnet-5", AgencyLevel: "medium",
		PermissionMode: "plan", MaxTurns: 10, Background: true, Isolation: "worktree",
		Temperature: new(0.7), TopP: new(0.9), MaxTokens: new(4096), Effort: "low", ThinkingBudget: new(0),
		SystemPrompt: "You write quotes.\n\n{agency_instructions}",
	}
	data, err := spec.Marshal()
	require.NoError(t, err)
	got, err := ParseMarkdownSpec(data)
	require.NoError(t, err, "%s", data)
	assert.Equal(t, spec.AgentMetadata, got.AgentMetadata)
	assert.Equal(t, spec.SystemPrompt, got.SystemPrompt)

	s, ok := got.ModelSettings()
	require.True(t, ok)
	assert.Equal(t, 0.7, *s.Temperature)
	assert.Equal(t, "low", *s.ReasoningEffort)
	assert.Equal(t, 0, *s.ThinkingBudget)

	// Unset settings stay out of the file, and an agent without them has none.
	bare, err := (&AgentSpec{Name: "bare", Description: "d"}).Marshal()
	require.NoError(t, err)
	assert.NotContains(t, string(bare), "temperature")
	assert.Contains(t, string(bare), "tools: []")
	_, ok = AgentMetadata{Name: "bare"}.ModelSettings()
	assert.False(t, ok)
}

func TestModelSettingsChecks(t *testing.T) {
	cases := []struct {
		name  string
		front string
		err   string
	}{
		{"temperature too high", "temperature: 2.5", "temperature"},
		{"top_p zero", "top_p: 0", "top_p"},
		{"max_tokens zero", "max_tokens: 0", "max_tokens"},
		{"negative thinking budget", "thinking_budget: -1", "thinking_budget"},
		{"unknown effort", "effort: huge", "effort"},
		{"xhigh is max", "effort: xhigh", ""},
		{"in range", "temperature: 0\ntop_p: 1\nmax_tokens: 1\nthinking_budget: 0", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec, err := ParseMarkdownSpec([]byte("---\nname: a\ndescription: d\n" + c.front + "\n---\nprompt\n"))
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
				return
			}
			require.NoError(t, err)
			if c.name == "xhigh is max" {
				assert.Equal(t, "max", spec.Effort)
			}
		})
	}
}

func TestSave(t *testing.T) {
	builtin := func(n string) bool { return n == "blitz" }
	agent := func(name, desc string) *AgentSpec {
		return &AgentSpec{Name: name, Description: desc, SystemPrompt: "p"}
	}
	cases := []struct {
		name     string
		spec     *AgentSpec
		previous string
		problem  string
	}{
		{"a bad name", agent("Quote Writer", "d"), "", "name"},
		{"a built-in's name", agent("blitz", "d"), "", "built-in"},
		{"no description", agent("q", " "), "", "description"},
		{"an unknown agency", &AgentSpec{Name: "q", Description: "d", AgencyLevel: "total"}, "", "agency_level"},
		{"a bad setting", &AgentSpec{Name: "q", Description: "d", TopP: new(2.0)}, "", "top_p"},
		{"a name taken in the folder", agent("taken", "d"), "", "already defined"},
		{"a rename onto a taken name", agent("taken", "d"), "other", "already defined"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeAgent(t, dir, "taken.md", "taken", "Taken")
			writeAgent(t, dir, "other.md", "other", "Other")
			path, problems, err := Save(dir, c.spec, c.previous, builtin)
			require.NoError(t, err)
			assert.Empty(t, path)
			require.NotEmpty(t, problems)
			assert.Contains(t, problems[0], c.problem)
		})
	}

	t.Run("new, edited, renamed and deleted", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), ".agents", "agents") // made on the first save
		path, problems, err := Save(dir, agent("quote", "Quotes"), "", builtin)
		require.NoError(t, err)
		require.Empty(t, problems)
		assert.Equal(t, filepath.Join(dir, "quote.md"), path)

		// A hand-named file keeps its name while the agent keeps its own.
		require.NoError(t, os.Rename(path, filepath.Join(dir, "My quotes.md")))
		path, problems, err = Save(dir, agent("quote", "Better quotes"), "quote", builtin)
		require.NoError(t, err)
		require.Empty(t, problems)
		assert.Equal(t, filepath.Join(dir, "My quotes.md"), path)

		// A rename moves it to the new name's file.
		path, problems, err = Save(dir, agent("daily-quote", "Better quotes"), "quote", builtin)
		require.NoError(t, err)
		require.Empty(t, problems)
		assert.Equal(t, filepath.Join(dir, "daily-quote.md"), path)
		files, err := ListFiles(dir, builtin)
		require.NoError(t, err)
		require.Len(t, files, 1)
		assert.Equal(t, "Better quotes", files[0].Spec.Description)

		require.NoError(t, Delete(dir, "daily-quote"))
		var unknown *api.UnknownAgentError
		assert.ErrorAs(t, Delete(dir, "daily-quote"), &unknown)
	})
}

func TestDeleteFile(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.md")
	require.NoError(t, os.WriteFile(broken, []byte("no frontmatter"), 0o644))
	outside := filepath.Join(t.TempDir(), "x.md")
	require.NoError(t, os.WriteFile(outside, nil, 0o644))
	for _, p := range []string{outside, filepath.Join(dir, "..", filepath.Base(filepath.Dir(outside)), "x.md"), dir, filepath.Join(dir, "notes.txt"), "broken.md"} {
		assert.Error(t, DeleteFile(dir, p), "%s", p)
	}
	assert.FileExists(t, outside)
	require.NoError(t, DeleteFile(dir, broken), "a file that doesn't parse can go")
	assert.NoFileExists(t, broken)
}

func TestListFiles(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "good.md", "good", "Good")
	writeAgent(t, filepath.Join(dir, "team"), "nested.md", "nested", "Nested")
	writeAgent(t, dir, "evil.md", "blitz", "Evil")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.md"), []byte("no frontmatter"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not an agent"), 0o644))

	files, err := ListFiles(dir, func(n string) bool { return n == "blitz" })
	require.NoError(t, err)
	got := map[string]string{}
	for _, f := range files {
		got[filepath.Base(f.Path)] = f.Problem
	}
	assert.Equal(t, map[string]string{
		"good.md": "", "nested.md": "", "evil.md": `agent name "blitz" is reserved by a built-in agent`,
		"broken.md": "markdown spec missing starting frontmatter delimiter '---'",
	}, got)

	none, err := ListFiles(filepath.Join(dir, "missing"), noBuiltins)
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestRefresh(t *testing.T) {
	reg, err := NewRegistry()
	require.NoError(t, err)
	dir := t.TempDir()
	writeAgent(t, dir, "one.md", "one", "One")
	require.NoError(t, reg.LoadExternalAgents(dir))

	changed, err := reg.Refresh()
	require.NoError(t, err)
	assert.False(t, changed, "nothing changed on disk")

	writeAgent(t, dir, "two.md", "two", "Two")
	changed, err = reg.Refresh()
	require.NoError(t, err)
	assert.True(t, changed)
	_, ok := reg.Get("two")
	assert.True(t, ok, "an added file loads")

	// An edit in place (same size, new time) reloads too.
	writeAgent(t, dir, "two.md", "two", "Owt")
	later := time.Now().Add(time.Minute)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "two.md"), later, later))
	changed, _ = reg.Refresh()
	assert.True(t, changed)
	two, _ := reg.Get("two")
	assert.Equal(t, "Owt", two.DisplayName)

	require.NoError(t, os.Remove(filepath.Join(dir, "one.md")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.md"), []byte("---\nname: x\nmax_turns: -1\n---\n"), 0o644))
	changed, err = reg.Refresh()
	assert.True(t, changed)
	assert.ErrorContains(t, err, "bad.md", "a file that doesn't load is reported")
	_, ok = reg.Get("one")
	assert.False(t, ok, "a removed file's agent is gone")
	_, ok = reg.Get("blitz")
	assert.True(t, ok, "built-ins stay")
	assert.True(t, reg.IsBuiltin("blitz"))
	assert.False(t, reg.IsBuiltin("two"))
}
