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

package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

// pinApp opens an App whose models are mocks named after the reference.
// setup runs first with the (temporary) home directory, e.g. to add agents.
func pinApp(t *testing.T, setup ...func(home string)) *App {
	t.Helper()
	isolateHome(t)
	for _, f := range setup {
		f(os.Getenv("HOME"))
	}
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Images.Dir = t.TempDir()
	cfg.Audit.Enabled = false
	return openAppWith(t, cfg, engine.Options{
		Model: runtime.NewMockLLM("gemini-3.8-flash"),
		NewModel: func(_ context.Context, _ *config.Config, name string) (model.LLM, error) {
			return runtime.NewMockLLM(strings.TrimPrefix(name, "anthropic/")), nil
		},
	})
}

// workspacePins are the pins in force in app's workspace: the global ones
// with the workspace's own over them.
func workspacePins(t *testing.T, app *App) map[string]string {
	t.Helper()
	cfg, err := config.LoadWorkspace(filepath.Join(os.Getenv("HOME"), ".blitz"), local(app).Dir())
	require.NoError(t, err)
	out := map[string]string{}
	for agent, ref := range cfg.AgentModels {
		if ref != "" {
			out[agent] = ref
		}
	}
	return out
}

func TestPinModelAndUnpin(t *testing.T) {
	app := pinApp(t)
	ctx := context.Background()
	run := func(cmd string) string {
		return captureStdout(t, func() { HandleCommand(ctx, cmd, app) })
	}

	assert.Contains(t, run("/pin_model"), "No agent is pinned", "empty list")
	out := run("/pin_model nobody anthropic/x")
	assert.Contains(t, out, "Unknown agent", "unknown agent:\n%s", out)
	assert.Len(t, workspacePins(t, app), 0, "unknown agent:\n%s", out)
	assert.Contains(t, run("/pin_model qa"), "Usage: /pin_model", "usage")

	out = run("/pin_model qa anthropic/claude-haiku-4-5")
	assert.Contains(t, out, "qa now runs on claude-haiku-4-5", "pin:\n%s", out)
	assert.Contains(t, out, "Saved in", "pin:\n%s", out)
	m, pinned := local(app).Engine().AgentModel("qa")
	require.True(t, pinned, "engine pin: %s %v", m, pinned)
	require.Equal(t, "claude-haiku-4-5", m, "engine pin: %s %v", m, pinned)
	got := workspacePins(t, app)
	require.Len(t, got, 1, "saved = %v\n%s", got, out)
	assert.Empty(t, savedConfig(t).AgentModels, "the pin went to the global settings")
	require.Equal(t, "anthropic/claude-haiku-4-5", got["qa"], "saved = %v", got)
	list := run("/pin_model")
	assert.Contains(t, list, "qa")
	assert.Contains(t, list, "claude-haiku-4-5")
	assert.Contains(t, run("/agents"), "pinned claude-haiku-4-5", "/agents doesn't show the pin")

	out = run("/unpin qa")
	_, pinned = local(app).Engine().AgentModel("qa")
	require.False(t, pinned, "unpin:\n%s", out)
	require.Contains(t, out, "qa now runs on gemini-3.8-flash", "unpin:\n%s", out)
	got = workspacePins(t, app)
	require.Len(t, got, 0, "unpin not saved: %v", got)
}

func TestModelCommandNotesAPinnedActiveAgent(t *testing.T) {
	app := pinApp(t)
	ctx := context.Background()
	captureStdout(t, func() { HandleCommand(ctx, "/pin_model blitz anthropic/claude-sonnet-5", app) })
	out := captureStdout(t, func() { HandleCommand(ctx, "/model gemini-3.5-flash-lite", app) })
	require.Contains(t, out, "blitz is pinned to claude-sonnet-5", "no note that the active agent keeps its pin:\n%s", out)
	require.Equal(t, "claude-sonnet-5", local(app).Engine().ModelName(), "active agent's model = %s", local(app).Engine().ModelName())
}

// An agent that declares default_model goes back to it on /unpin.
func TestUnpinRestoresTheAgentsOwnDefault(t *testing.T) {
	app := pinApp(t, func(home string) {
		dir := filepath.Join(home, ".blitz", "agents")
		os.MkdirAll(dir, 0o700)
		os.WriteFile(filepath.Join(dir, "reviewer.md"), []byte("---\nname: reviewer\ndisplay_name: Reviewer\ndescription: reviews\ntools: [read_file]\ndefault_model: anthropic/claude-haiku-4-5\n---\nYou review code.\n"), 0o600)
	})
	ctx := context.Background()
	captureStdout(t, func() { HandleCommand(ctx, "/pin_model reviewer anthropic/claude-sonnet-5", app) })
	out := captureStdout(t, func() { HandleCommand(ctx, "/unpin reviewer", app) })
	m, _ := local(app).Engine().AgentModel("reviewer")
	require.Equal(t, "claude-haiku-4-5", m, "after /unpin reviewer runs on %s, want its default_model:\n%s", m, out)
}
