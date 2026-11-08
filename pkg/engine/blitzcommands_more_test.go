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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every list and show command answers in a new workspace, asking nothing
// (spec_060_blitz_tools DRV-10, DRV-40, DRV-50).
func TestBlitzReadCommands(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionDeny, "", nil)
	for _, name := range []string{
		"connector list", "settings show", "org status", "org policy", "eval list", "eval results", "spec list",
		"memory list", "chat search", "process list", "approval list", "secret list", "workflow list",
		"agent list", "skill list", "mcp list", "rule list", "guardrail list", "command list", "hook list", "plugin list",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runCmd(t, w, name, map[string]any{"terms": "x", "name": "none"})
			assert.NoError(t, err)
		})
	}
	out, err := runCmd(t, w, "connector list", nil)
	require.NoError(t, err)
	assert.NotEmpty(t, out.(map[string]any)["kinds"], "the catalog's kinds")
	assert.Empty(t, *asked)
}

// What a command changes is asked first; declined, it's an error and
// nothing changes (DRV-04).
func TestBlitzChangesDeclined(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionDeny, "", nil)
	cases := []struct {
		name string
		args map[string]any
	}{
		{"rule save", map[string]any{"name": "tidy", "text": "Keep it tidy."}},
		{"guardrail save", map[string]any{"name": "nope", "stage": "input", "check": "no"}},
		{"command save", map[string]any{"name": "hello", "prompt": "Say hello."}},
		{"skill save", map[string]any{"name": "greet", "description": "Greets", "instructions": "Greet."}},
		{"agent save", map[string]any{"name": "helper", "instructions": "Help."}},
		{"memory forget", map[string]any{"name": "x"}},
		{"secret set", map[string]any{"name": "TOKEN"}},
		{"settings set", map[string]any{"key": "effort", "value": "low"}},
		{"mcp add", map[string]any{"name": "web", "url": "https://mcp.example.com"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runCmd(t, w, c.name, c.args)
			assert.Error(t, err)
		})
	}
	assert.Len(t, *asked, len(cases))
	_, err := os.Stat(filepath.Join(w.Dir(), ".blitz", "commands", "hello.md"))
	assert.True(t, os.IsNotExist(err), "declined: nothing written")
}

// Approved, Studio's items are saved where Studio keeps them, and edited
// in place the second time (DRV-50, DRV-51).
func TestBlitzStudioSaves(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	saves := []struct {
		name string
		args map[string]any
		file string
	}{
		{"rule save", map[string]any{"name": "tidy", "description": "Tidy code", "text": "Keep it tidy."}, ".agents/rules/tidy.md"},
		{"guardrail save", map[string]any{"name": "no-swearing", "description": "No swearing", "stage": "output", "action": "block", "check": "No swearing."}, ".agents/guardrails/no-swearing.md"},
		{"command save", map[string]any{"name": "hello", "description": "Say hello", "prompt": "Say hello."}, ".blitz/commands/hello.md"},
		{"skill save", map[string]any{"name": "greet", "description": "Greets people", "instructions": "Greet them warmly."}, ""},
		{"agent save", map[string]any{"name": "helper", "description": "Helps", "instructions": "Help the person."}, ""},
	}
	for _, s := range saves {
		t.Run(s.name, func(t *testing.T) {
			for range 2 { // new, then edited
				out, err := runCmd(t, w, s.name, s.args)
				require.NoError(t, err)
				assert.NotEmpty(t, out.(map[string]any)["saved"])
			}
			if s.file != "" {
				_, err := os.Stat(filepath.Join(w.Dir(), filepath.FromSlash(s.file)))
				assert.NoError(t, err)
			}
		})
	}
	assert.NotEmpty(t, *asked)
	assert.Contains(t, (*asked)[len(*asked)-1].Diff, "Help the person.")

	t.Run("refusals", func(t *testing.T) {
		_, err := runCmd(t, w, "command save", map[string]any{"name": "Bad Name"})
		assert.ErrorContains(t, err, "lowercase")
		_, err = runCmd(t, w, "rule save", map[string]any{"name": "", "text": "x"})
		assert.Error(t, err)
		_, err = runCmd(t, w, "guardrail save", map[string]any{"name": "g", "stage": "sideways", "check": "x"})
		assert.Error(t, err)
		_, err = runCmd(t, w, "workflow save", map[string]any{"workflow": "not a workflow"})
		assert.ErrorContains(t, err, "the workflow")
		_, err = runCmd(t, w, "workflow save", map[string]any{"workflow": map[string]any{"name": "w", "nodes": []any{map[string]any{"id": "a", "type": "agent"}}}})
		assert.Error(t, err, "an agent step needs instructions")
	})
	t.Run("a workflow", func(t *testing.T) {
		out, err := runCmd(t, w, "workflow save", map[string]any{"workflow": map[string]any{"name": "nightly",
			"nodes": []any{map[string]any{"id": "a", "type": "agent", "title": "Check", "instructions": "Check the build."}}}})
		require.NoError(t, err)
		assert.Equal(t, "nightly", out.(map[string]any)["saved"])
	})
}

// Settings change after approval: the effort, the image model (the
// providers reloaded) and the model (DRV-80).
func TestBlitzSettingsSetMore(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	for _, c := range []struct{ key, value string }{
		{"image_model", "gemini-3.1-flash-image"},
		{"model", "gemini/gemini-3.8-flash"},
	} {
		t.Run(c.key, func(t *testing.T) {
			out, err := runCmd(t, w, "settings set", map[string]any{"key": c.key, "value": c.value})
			require.NoError(t, err)
			assert.Equal(t, c.value, out.(map[string]any)[c.key])
		})
	}
	_, err := runCmd(t, w, "settings set", map[string]any{"key": "mode", "value": "sideways"})
	assert.Error(t, err)
	_, err = runCmd(t, w, "settings set", map[string]any{"key": "mode", "value": "nonsense"})
	assert.Error(t, err)
}

// Commands about things that aren't there say so and ask nothing.
func TestBlitzCommandsOnNothing(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"eval run", map[string]any{"name": "none"}, "no eval set"},
		{"process stop", map[string]any{"id": 42}, "no process 42"},
		{"connector enable", map[string]any{"name": "none"}, "no connection"},
		{"mcp enable", map[string]any{"name": "none"}, "no MCP server"},
		{"chat export", map[string]any{"path": "chat.md"}, ""},
		{"spec review", map[string]any{"path": "nope.md"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runCmd(t, w, c.name, c.args)
			require.Error(t, err)
			if c.want != "" {
				assert.ErrorContains(t, err, c.want)
			}
		})
	}
	assert.Empty(t, *asked)
	_, _ = runCmd(t, w, "approval forget", map[string]any{"key": "nothing"}) // never asked: it only makes Blitz ask more
	assert.Empty(t, *asked)
}

// Connections and MCP servers turn on and off after approval (DRV-12,
// DRV-73); a new conversation starts with chat new.
func TestBlitzOnAndOff(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "recipes.db"), nil, 0o600))
	_, err := runCmd(t, w, "connector save", map[string]any{"kind": "sqlite", "name": "recipes", "read_only": true,
		"settings": map[string]any{"database": "recipes.db"}, "queries": []any{map[string]any{"name": "all", "description": "Every recipe", "statement": "select 1"}}})
	require.NoError(t, err)
	for _, name := range []string{"connector disable", "connector enable"} {
		out, err := runCmd(t, w, name, map[string]any{"name": "recipes"})
		require.NoError(t, err, name)
		assert.Equal(t, name == "connector enable", out.(map[string]any)["on"])
	}
	assert.True(t, connectionNamed(t, w, "recipes").On)
	list, err := runCmd(t, w, "connector list", nil)
	require.NoError(t, err)
	assert.Contains(t, fmt.Sprint(list.(map[string]any)["connections"]), "all", "its queries named")
	_, err = runCmd(t, w, "connector save", map[string]any{"kind": "sqlite", "name": "bad", "queries": []any{"not a query"}})
	assert.ErrorContains(t, err, "a query")

	out, err := runCmd(t, w, "mcp add", map[string]any{"name": "web", "url": "http://127.0.0.1:1/mcp"})
	require.NoError(t, err)
	assert.Equal(t, "web", out.(map[string]any)["added"])
	for _, name := range []string{"mcp disable", "mcp enable"} {
		_, err := runCmd(t, w, name, map[string]any{"name": "web"})
		assert.NoError(t, err, name)
	}
	assert.NotEmpty(t, *asked)

	chat, err := runCmd(t, w, "chat new", nil)
	require.NoError(t, err)
	assert.NotEmpty(t, chat.(map[string]any)["session"])
}

// Opening things goes to the newest window; with none, the command says
// so, and a web page is open_page's (DRV-30, DRV-31).
func TestBlitzOpen(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "notes.md"), []byte("# Notes\n"), 0o600))
	_, err := runCmd(t, w, "open url", map[string]any{"url": "https://example.com"})
	assert.ErrorContains(t, err, "open_page")
	_, err = runCmd(t, w, "open view", map[string]any{"view": "pictures"})
	assert.ErrorContains(t, err, "no Blitz window is open")
	_, err = runCmd(t, w, "picture", map[string]any{"prompt": "a fox"})
	assert.ErrorContains(t, err, "generate_image")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan api.AgentBrowserCommand, 8)
	go func() {
		_ = w.WatchAgentBrowser(ctx, func(c api.AgentBrowserCommand) error { got <- c; return nil })
	}()
	require.Eventually(t, func() bool {
		w.browsers.mu.Lock()
		defer w.browsers.mu.Unlock()
		return len(w.browsers.windows) > 0
	}, 5*time.Second, 5*time.Millisecond)

	cases := []struct {
		name string
		args map[string]any
		want api.AgentBrowserCommand
	}{
		{"open file", map[string]any{"path": "notes.md", "line": 1}, api.AgentBrowserCommand{Kind: "file", Path: "notes.md", Line: 1}},
		{"open studio", map[string]any{"section": "skills"}, api.AgentBrowserCommand{Kind: "view", Path: "studio:skills"}},
		{"open settings", map[string]any{"section": "providers"}, api.AgentBrowserCommand{Kind: "view", Path: "settings:providers"}},
		{"open view", map[string]any{"view": "pictures"}, api.AgentBrowserCommand{Kind: "view", Path: "pictures"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runCmd(t, w, c.name, c.args)
			require.NoError(t, err)
			cmd := <-got
			assert.Equal(t, c.want.Kind, cmd.Kind)
			assert.Equal(t, c.want.Path, cmd.Path)
			assert.Equal(t, c.want.Line, cmd.Line)
		})
	}
	out, err := runCmd(t, w, "picture", map[string]any{"prompt": "a fox", "references": []any{"notes.md"}})
	require.NoError(t, err)
	assert.Equal(t, "Pictures", out.(map[string]any)["opened"])
	assert.Contains(t, (<-got).Message, "a fox")
	_, err = runCmd(t, w, "open file", map[string]any{"path": "../outside.md"})
	assert.Error(t, err)
}

// From the CLI, the jobs have their own commands and bad arguments answer
// with the command's usage (DRV-02).
func TestRunBlitzCommandRefusals(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	ctx := context.Background()
	_, err := w.RunBlitzCommand(ctx, "job list", nil, false, nil)
	assert.ErrorContains(t, err, "blitz jobs")
	run, err := w.RunBlitzCommand(ctx, "secret set", map[string]any{"name": "TOKEN"}, true, nil)
	assert.ErrorContains(t, err, "TOKEN is needed")
	assert.Empty(t, run.Result)
	run, err = w.RunBlitzCommand(ctx, "help", map[string]any{"command": "connector"}, false, nil)
	require.NoError(t, err)
	assert.Contains(t, run.Help, "connector save")
}

// Under an organization, its policy and status are read back, and an MCP
// server of one's own is refused unless the policy allows it (DRV-72,
// DRV-82).
func TestBlitzUnderAnOrganization(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", func(cfg *config.Config) {
		cfg.Org = config.OrgLayer{Defaults: t.TempDir(), Locked: t.TempDir(), Expires: time.Now().Add(time.Hour), Problem: "the catalog is old"}
		cfg.Enterprise.URL = "https://blitz.example.com"
	})
	pol, err := runCmd(t, w, "org policy", nil)
	require.NoError(t, err)
	assert.Contains(t, pol.(map[string]any)["note"], "[connectors] own = true")
	st, err := runCmd(t, w, "org status", nil)
	require.NoError(t, err)
	status := st.(map[string]any)
	assert.Equal(t, true, status["catalog"])
	assert.Equal(t, "the catalog is old", status["problem"])
	assert.NotNil(t, status["catalog_expires"])
	checks, err := runCmd(t, w, "doctor", nil)
	require.NoError(t, err)
	assert.Contains(t, checks.([]doctorCheck), doctorCheck{"organization", "problem", "the catalog is old"})
	_, err = runCmd(t, w, "mcp add", map[string]any{"name": "web", "url": "https://mcp.example.com"})
	assert.ErrorContains(t, err, "doesn't allow MCP servers of your own")
	assert.Empty(t, *asked)
}

// A conversation is exported to a file after approval; specs are listed,
// reviewed, verified and planned without a model unless asked (DRV-41).
func TestBlitzChatsAndSpecs(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	s, err := w.NewSession()
	require.NoError(t, err)
	_, _ = w.Run(context.Background(), s.ID, api.Turn{Text: "hi"}, func(api.Event) {}) // stored once it has a message
	out, err := runCmd(t, w, "chat export", map[string]any{"path": "chat.md"})
	require.NoError(t, err)
	assert.Equal(t, "chat.md", out.(map[string]any)["written"])
	require.Len(t, *asked, 1)
	assert.Contains(t, (*asked)[0].Diff, "+++ b/chat.md")

	spec := filepath.Join(".agents", "specs", "spec_001_shop.md")
	require.NoError(t, os.MkdirAll(filepath.Join(w.Dir(), ".agents", "specs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), spec), []byte("# Shop\n\n## Requirements\n\n- **SH-01** The shop lists its products.\n"), 0o644))
	for _, name := range []string{"spec list", "spec review", "spec verify", "spec implement"} {
		t.Run(name, func(t *testing.T) {
			_, err := runCmd(t, w, name, map[string]any{"path": filepath.ToSlash(spec)})
			assert.NoError(t, err)
		})
	}

	// A model's review or check, or an eval run, is asked first: declined,
	// nothing runs.
	wd, declined, _ := blitzWorkspace(t, api.DecisionDeny, "", nil)
	require.NoError(t, os.MkdirAll(filepath.Join(wd.Dir(), ".agents", "specs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wd.Dir(), spec), []byte("# Shop\n\n- **SH-01** The shop lists its products.\n"), 0o644))
	_, err = runCmd(t, wd, "spec review", map[string]any{"path": filepath.ToSlash(spec), "judge": true})
	assert.Error(t, err)
	writeEval(t, wd, "shop", "| ID | Prompt | Output |\n|-|-|-|\n| a | hi | hello |\n")
	_, err = runCmd(t, wd, "eval run", map[string]any{"name": "shop"})
	assert.Error(t, err)
	assert.NotEmpty(t, *declined)
}

// open_file goes to the newest window's editor, and says when there's no
// window (DRV-31).
func TestOpenFileInTheWindow(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	assert.ErrorIs(t, w.openFile(ctx, "a.md", 1), ErrNoBrowserWindow)
	got := make(chan api.AgentBrowserCommand, 1)
	go func() {
		_ = w.WatchAgentBrowser(ctx, func(c api.AgentBrowserCommand) error { got <- c; return nil })
	}()
	require.Eventually(t, func() bool {
		w.browsers.mu.Lock()
		defer w.browsers.mu.Unlock()
		return len(w.browsers.windows) > 0
	}, 5*time.Second, 5*time.Millisecond)
	require.NoError(t, w.openFile(ctx, "a.md", 3))
	assert.Equal(t, api.AgentBrowserCommand{Kind: "file", Path: "a.md", Line: 3}, <-got)
}

// Every change a command makes is asked first: declined, it's an error.
func TestBlitzMoreChangesDeclined(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "recipes.db"), nil, 0o600))
	_, err := runCmd(t, w, "connector save", map[string]any{"kind": "sqlite", "name": "recipes", "settings": map[string]any{"database": "recipes.db"}})
	require.NoError(t, err)
	_, err = runCmd(t, w, "mcp add", map[string]any{"name": "web", "url": "http://127.0.0.1:1/mcp"})
	require.NoError(t, err)
	s, err := w.NewSession()
	require.NoError(t, err)
	_, _ = w.Run(context.Background(), s.ID, api.Turn{Text: "hi"}, func(api.Event) {})

	// What's set up shows in the lists and doctor.
	list, err := runCmd(t, w, "connector list", nil)
	require.NoError(t, err)
	assert.NotEmpty(t, list.(map[string]any)["connections"])
	checks, err := runCmd(t, w, "doctor", nil)
	require.NoError(t, err)
	assert.NotEmpty(t, checks)
	show, err := runCmd(t, w, "settings show", nil)
	require.NoError(t, err)
	assert.NotEmpty(t, show.(map[string]any)["workspace_model"])

	// Saved again: the same file, edited.
	_, err = runCmd(t, w, "connector save", map[string]any{"kind": "sqlite", "name": "recipes", "description": "Again", "settings": map[string]any{"database": "recipes.db"}})
	require.NoError(t, err)

	w.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionDeny, nil }, nil)
	cases := []struct {
		name string
		args map[string]any
	}{
		{"connector disable", map[string]any{"name": "recipes"}},
		{"mcp disable", map[string]any{"name": "web"}},
		{"chat export", map[string]any{"path": "chat.md"}},
		{"workflow save", map[string]any{"workflow": map[string]any{"name": "nightly", "nodes": []any{map[string]any{"id": "a", "type": "agent", "instructions": "Check."}}}}},
		{"settings set", map[string]any{"key": "model", "value": "gemini/gemini-3.8-flash"}},
		{"connector save", map[string]any{"kind": "sqlite", "name": "more", "settings": map[string]any{"database": "recipes.db"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runCmd(t, w, c.name, c.args)
			assert.Error(t, err)
		})
	}
}

// Under a conversation with messages, the model changes for it, not the
// workspace (DRV-80).
func TestBlitzSettingsSetInAConversation(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	s, err := w.NewSession()
	require.NoError(t, err)
	_, _ = w.Run(context.Background(), s.ID, api.Turn{Text: "hi"}, func(api.Event) {})
	out, err := runCmd(t, w, "settings set", map[string]any{"key": "model", "value": "gemini/gemini-3.8-flash"})
	require.NoError(t, err)
	assert.Equal(t, "gemini/gemini-3.8-flash", out.(map[string]any)["conversation_model"])
	show, err := runCmd(t, w, "settings show", nil)
	require.NoError(t, err)
	assert.Equal(t, "gemini/gemini-3.8-flash", show.(map[string]any)["conversation_model"])
}

// A secret typed as nothing, or asked where nobody is, isn't saved
// (DRV-05).
func TestBlitzSecretNotTyped(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "  ", nil)
	_, err := runCmd(t, w, "secret set", map[string]any{"name": "TOKEN"})
	assert.ErrorContains(t, err, "no value was typed")
	_, err = runCmd(t, w, "connector save", map[string]any{"kind": "postgres", "name": "shop",
		"settings": map[string]any{"host": "127.0.0.1", "port": "1", "database": "shop", "user": "me"}})
	assert.ErrorContains(t, err, "no value was typed")
	_, err = w.askSecret(tools.Unattended(context.Background(), nil), "TOKEN", "x")
	assert.ErrorContains(t, err, "nobody is here")
}

// Doctor names Ollama when it's the provider, and that it doesn't answer
// here (DRV-81).
func TestDoctorOllama(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "", func(cfg *config.Config) { cfg.LLM.Provider = "ollama" })
	out, err := runCmd(t, w, "doctor", nil)
	require.NoError(t, err)
	checks := out.([]doctorCheck)
	assert.True(t, slices.ContainsFunc(checks, func(c doctorCheck) bool { return c.Check == "ollama" && c.State == "problem" }), "%v", checks)
	show, err := runCmd(t, w, "settings show", nil)
	require.NoError(t, err)
	assert.NotEmpty(t, show.(map[string]any)["mode_note"], "auto held back for a local model")
	assert.Equal(t, "x", refOf(api.ModelInfo{Name: "x"}))
	assert.Equal(t, "a/b", refOf(api.ModelInfo{Provider: "p", Name: "a/b"}))
}

// Commands given what can't be saved say why; an approval saved for good
// is forgotten by its key.
func TestBlitzCommandErrors(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionAlways, "", nil)
	cases := []struct {
		name string
		args map[string]any
	}{
		{"agent save", map[string]any{"name": "Bad Name", "instructions": "x"}},
		{"skill save", map[string]any{"name": "Bad Name", "description": "x", "instructions": "x"}},
		{"rule save", map[string]any{"name": "Bad Name", "text": "x"}},
		{"guardrail save", map[string]any{"name": "Bad Name", "stage": "input", "check": "x"}},
		{"workflow save", map[string]any{"workflow": map[string]any{"name": "Bad Name", "nodes": []any{map[string]any{"id": "a", "type": "agent", "instructions": "x"}}}}},
		{"settings set", map[string]any{"key": "effort", "value": "enormous"}},
		{"chat export", map[string]any{"path": "../outside.md"}},
	}
	s, err := w.NewSession()
	require.NoError(t, err)
	_, _ = w.Run(context.Background(), s.ID, api.Turn{Text: "hi"}, func(api.Event) {})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runCmd(t, w, c.name, c.args)
			assert.Error(t, err)
		})
	}
	_, err = runCmd(t, w, "mcp add", map[string]any{"name": "web", "url": "http://127.0.0.1:1/mcp"})
	require.NoError(t, err)
	_, err = runCmd(t, w, "mcp add", map[string]any{"name": "web", "url": "http://127.0.0.1:1/mcp"})
	assert.Error(t, err, "the same name twice")

	approvals := w.ListApprovals()
	require.NotEmpty(t, approvals, "saved for good")
	out, err := runCmd(t, w, "approval forget", map[string]any{"key": "blitz:mcp add"})
	require.NoError(t, err)
	assert.Positive(t, out.(map[string]any)["forgot"])
	assert.Equal(t, "confirm: x", (&errConfirm{detail: "x"}).Error())
}

// A model's check of a spec is asked first, with what it costs.
func TestBlitzSpecVerifyAsked(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionDeny, "", nil)
	require.NoError(t, os.MkdirAll(filepath.Join(w.Dir(), ".agents", "specs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), ".agents", "specs", "spec_001_shop.md"), []byte("# Shop\n\n- **SH-01** The shop lists its products.\n"), 0o644))
	_, err := runCmd(t, w, "spec verify", map[string]any{"path": ".agents/specs/spec_001_shop.md", "judge": true})
	assert.Error(t, err)
	if assert.Len(t, *asked, 1) {
		assert.Contains(t, (*asked)[0].Detail, "Have a model check the requirements")
	}
	_, err = runCmd(t, w, "mcp enable", map[string]any{"name": "none"})
	assert.Error(t, err)
}

// The eval sets are listed with their cases; a note is forgotten once the
// person agrees; the organization's word on plugins is passed on.
func TestBlitzEvalsNotesAndPlugins(t *testing.T) {
	own := true
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", func(cfg *config.Config) {
		cfg.Org = config.OrgLayer{Defaults: t.TempDir()}
		cfg.Org.Policy.Plugins.Own = &own
	})
	writeEval(t, w, "shop", "| ID | Prompt | Output |\n|-|-|-|\n| a | hi | hello |\n")
	out, err := runCmd(t, w, "eval list", nil)
	require.NoError(t, err)
	assert.Equal(t, []map[string]any{{"name": "shop", "cases": 1}}, out)
	_, _ = runCmd(t, w, "memory forget", map[string]any{"name": "none"})
	assert.Len(t, *asked, 1)
	pol, err := runCmd(t, w, "org policy", nil)
	require.NoError(t, err)
	assert.Equal(t, true, pol.(map[string]any)["own_plugins"])
}
