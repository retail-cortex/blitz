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

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// cdApp is an App in workspace a whose /cd opens workspaces as main does,
// with the same session storage, each on its own mock model.
func cdApp(t *testing.T) (app *App, a string, models map[string]*runtime.MockLLM) {
	t.Helper()
	isolateHome(t)
	sessions := t.TempDir()
	models = map[string]*runtime.MockLLM{}
	open := func(dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = sessions
		cfg.Images.Dir = t.TempDir()
		llm := runtime.NewMockLLM("mock", genai.NewContentFromText("ok", genai.RoleModel))
		w, err := engine.Open(context.Background(), cfg, engine.Options{Model: llm})
		if err == nil {
			models[dir] = llm
		}
		return w, err
	}
	a, _ = filepath.EvalSymlinks(t.TempDir())
	w, err := open(a)
	require.NoError(t, err)
	current := w
	t.Cleanup(func() { current.Close() })
	app = &App{Workspace: w, Printer: PrinterOptions{Out: os.Stdout}}
	app.Cd = func(_ context.Context, dir string) (api.Backend, *i18n.Bundle, func(), error) {
		if strings.HasSuffix(dir, "busy") {
			return nil, nil, nil, api.ErrWorkspaceBusy
		}
		nw, err := open(dir)
		if err != nil {
			return nil, nil, nil, err
		}
		return nw, nil, func() { current = nw }, nil
	}
	return app, a, models
}

func TestCdMovesTheSession(t *testing.T) {
	app, a, models := cdApp(t)
	ctx := context.Background()
	s, _, err := app.Workspace.OpenSession("", false)
	require.NoError(t, err)
	_, err = app.Workspace.Run(ctx, s.ID, api.Turn{Text: "look around"}, func(api.Event) {})
	require.NoError(t, err)

	b, _ := filepath.EvalSymlinks(t.TempDir())
	out := captureStdout(t, func() { cmdCd(ctx, []string{b}, app) })
	assert.Contains(t, out, "the session moved with you")
	assert.Equal(t, b, app.Workspace.Dir())
	moved, ok := app.Workspace.ActiveSession()
	require.True(t, ok)
	assert.Equal(t, s.ID, moved.ID, "a new session instead of the moved one")
	assert.Equal(t, a, moved.MovedFrom)

	// The old workspace closed: it can be opened again.
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir, cfg.Session.StorageDir = a, t.TempDir()
	again, err := engine.Open(ctx, cfg, engine.Options{Model: runtime.NewMockLLM("x")})
	require.NoError(t, err, "the old workspace is still locked")
	again.Close()

	// Earlier prompts were in the old workspace: no rewinding them here.
	points, err := app.Workspace.RewindPoints()
	require.NoError(t, err)
	assert.Empty(t, points)
	_, err = app.Workspace.Rewind(ctx, 0, api.RewindConversation, false)
	assert.ErrorContains(t, err, "/cd back")

	// The agent's next prompt says where it is now.
	_, err = app.Workspace.Run(ctx, moved.ID, api.Turn{Text: "go on"}, func(api.Event) {})
	require.NoError(t, err)
	reqs := models[b].Requests
	require.NotEmpty(t, reqs)
	last := reqs[len(reqs)-1].Contents
	text := last[len(last)-1].Parts[0].Text
	assert.Contains(t, text, "The workspace is now "+b)
	assert.Contains(t, text, "moved from "+a)
}

func TestCdStaysPut(t *testing.T) {
	app, a, _ := cdApp(t)
	file := filepath.Join(a, "notes.txt")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	busy := filepath.Join(t.TempDir(), "busy")
	require.NoError(t, os.Mkdir(busy, 0o755))
	cases := []struct {
		name string
		args []string
		says string
	}{
		{name: "no path", says: "Workspace: " + a},
		{name: "not a directory", args: []string{file}, says: "is not a directory"},
		{name: "the same one", args: []string{"."}, says: "Already in"},
		{name: "open elsewhere", args: []string{busy}, says: "Stayed in " + a},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := captureStdout(t, func() { cmdCd(context.Background(), c.args, app) })
			assert.Contains(t, out, c.says)
			assert.Equal(t, a, app.Workspace.Dir())
		})
	}
}

// A chat with nothing in it doesn't move: the new workspace starts one.
func TestCdWithAnEmptyChat(t *testing.T) {
	app, _, _ := cdApp(t)
	_, _, err := app.Workspace.OpenSession("", false)
	require.NoError(t, err)
	b, _ := filepath.EvalSymlinks(t.TempDir())
	out := captureStdout(t, func() { cmdCd(context.Background(), []string{b}, app) })
	assert.Contains(t, out, "in a new session")
	s, ok := app.Workspace.ActiveSession()
	require.True(t, ok)
	assert.Empty(t, s.MovedFrom)
}
