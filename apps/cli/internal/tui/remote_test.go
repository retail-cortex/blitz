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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// The REPL drives a workspace held by the service exactly as a local one.
func TestREPLOnARemoteWorkspace(t *testing.T) {
	isolateHome(t)
	s := servicetest.New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		cfg.Images.Dir = t.TempDir()
		return engine.Open(ctx, cfg, engine.Options{
			Model: runtime.NewMockLLM("gemini-3.8-flash", genai.NewContentFromText("remote hello", genai.RoleModel)),
			NewModel: func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
				_, name := runtime.ParseModelRef(ref, "")
				return runtime.NewMockLLM(name), nil
			},
		})
	})
	srv := httptest.NewServer(s.Handler())
	defer func() { srv.Close(); s.Close() }()
	r, err := client.AttachHTTP(context.Background(), http.DefaultClient, srv.URL, t.TempDir(), func(w string) { t.Errorf("warning %s", w) })
	require.NoError(t, err)
	app := &App{Workspace: r, Printer: PrinterOptions{}}
	ctx := context.Background()
	run := func(cmd string) string { return captureStdout(t, func() { HandleCommand(ctx, cmd, app) }) }

	assert.Contains(t, run("/pin_model qa anthropic/claude-haiku-4-5"), "qa now runs on claude-haiku-4-5", "/pin_model")
	assert.Contains(t, run("/pin_model nobody x"), "Unknown agent", "unknown agent")
	assert.Contains(t, run("/session save early"), "No active session", "no session")
	s1, _ := r.NewSession()
	out := captureStdout(t, func() { runTurn(ctx, app, s1.ID, "hi", nil, turnOptions{}) })
	assert.Contains(t, out, "remote hello", "turn:\n%s", out)
	out = run("/session save first")
	assert.Contains(t, out, "Saved snapshot first", "/session save:\n%s", out)
	out = run("/session save first")
	assert.Contains(t, out, "--force replaces it", "taken:\n%s", out)
}
