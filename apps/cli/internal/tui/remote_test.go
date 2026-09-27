package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
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
	if err != nil {
		t.Fatal(err)
	}
	app := &App{Workspace: r, Printer: PrinterOptions{}}
	ctx := context.Background()
	run := func(cmd string) string { return captureStdout(t, func() { HandleCommand(ctx, cmd, app) }) }

	if out := run("/pin_model qa anthropic/claude-haiku-4-5"); !strings.Contains(out, "qa now runs on claude-haiku-4-5") {
		t.Errorf("/pin_model:\n%s", out)
	}
	if out := run("/pin_model nobody x"); !strings.Contains(out, "Unknown agent") {
		t.Errorf("unknown agent:\n%s", out)
	}
	if out := run("/session save early"); !strings.Contains(out, "No active session") {
		t.Errorf("no session:\n%s", out)
	}
	s1, _ := r.NewSession()
	out := captureStdout(t, func() { runTurn(ctx, app, s1.ID, "hi", nil, turnOptions{}) })
	if !strings.Contains(out, "remote hello") {
		t.Errorf("turn:\n%s", out)
	}
	if out := run("/session save first"); !strings.Contains(out, "Saved snapshot first") {
		t.Errorf("/session save:\n%s", out)
	}
	if out := run("/session save first"); !strings.Contains(out, "--force replaces it") {
		t.Errorf("taken:\n%s", out)
	}
}
