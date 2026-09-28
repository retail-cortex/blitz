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

package client

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// attach starts a service whose workspaces run on mock models answering
// with replies, and attaches to a new workspace in it.
func attach(t *testing.T, mutate func(*config.Config), replies ...*genai.Content) *Remote {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	s := servicetest.New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		cfg.Images.Dir = t.TempDir()
		if mutate != nil {
			mutate(cfg)
		}
		return engine.Open(ctx, cfg, engine.Options{
			Model: runtime.NewMockLLM("gemini-3.8-flash", replies...),
			NewModel: func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
				_, name := runtime.ParseModelRef(ref, "")
				return runtime.NewMockLLM(name), nil
			},
		})
	})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() { srv.Close(); s.Close() })
	var warnings []string
	r, err := AttachHTTP(context.Background(), http.DefaultClient, srv.URL, t.TempDir(), func(w string) { warnings = append(warnings, w) })
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.LessOrEqual(t, len(warnings), 0, "warnings: %q", warnings)
	})
	return r
}

func text(s string) *genai.Content { return genai.NewContentFromText(s, genai.RoleModel) }

func call(name string, args map[string]any) *genai.Content {
	return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: name, Args: args}}}}
}

func TestRemoteOperationsAndTypedErrors(t *testing.T) {
	r := attach(t, nil)
	ctx := context.Background()
	require.NoError(t, r.ModelErr(), "model %v %v, agent %v", r.ModelErr(), r.Model(), r.ActiveAgent())
	require.NotEqual(t, "", r.Model().Name, "model %v %v, agent %v", r.ModelErr(), r.Model(), r.ActiveAgent())
	require.Equal(t, "blitz", r.ActiveAgent().Name, "model %v %v, agent %v", r.ModelErr(), r.Model(), r.ActiveAgent())
	var unknown *api.UnknownAgentError
	_, err := r.PinModel(ctx, "nobody", "x")
	assert.ErrorAs(t, err, &unknown, "unknown agent: %v", err)
	assert.Equal(t, "nobody", unknown.Name, "unknown agent: %v", err)
	var invalid *api.InvalidSettingError
	_, err = r.UpdateModelSettings("gpt-5", false, []api.Setting{{Key: "temperature", Value: "9"}})
	assert.ErrorAs(t, err, &invalid, "invalid setting: %v", err)
	_, err = r.SaveSnapshot("s", false)
	assert.ErrorIs(t, err, api.ErrNoActiveSession, "no session: %v", err)
	_, err = r.Set(ctx, "agency", "reckless")
	assert.ErrorIs(t, err, api.ErrInvalidAgency, "agency: %v", err)
	res, err := r.PinModel(ctx, "qa", "anthropic/claude-haiku-4-5")
	require.NoError(t, err, "pin %+v", res)
	require.Equal(t, "claude-haiku-4-5", res.Model, "pin %+v %v", res, err)
	require.NoError(t, res.Saved.Err, "pin %+v %v", res, err)
	i := slices.IndexFunc(r.ListAgents(), func(a api.AgentInfo) bool { return a.Name == "qa" })
	assert.GreaterOrEqual(t, i, 0, "pin not listed")
	assert.Equal(t, "claude-haiku-4-5", r.ListAgents()[i].PinnedModel, "pin not listed")
	assert.True(t, r.ImagesEnabled(), "images %v, processes %v, sandbox %v", r.ImagesEnabled(), r.Processes(), r.SandboxSummary())
	assert.Nil(t, r.Processes(), "images %v, processes %v, sandbox %v", r.ImagesEnabled(), r.Processes(), r.SandboxSummary())
	assert.NotEqual(t, 0, len(r.SandboxSummary()), "images %v, processes %v, sandbox %v", r.ImagesEnabled(), r.Processes(), r.SandboxSummary())
}

func TestRemoteTurnWithApprovalAndQuestion(t *testing.T) {
	create := call("create_file", map[string]any{"path": "made.txt", "content": "hi\n"})
	ask := call("ask_user_question", map[string]any{"question": "Tabs or spaces?"})
	r := attach(t, func(c *config.Config) { c.Blitz.AutoApprove = false }, create, ask, text("all done"))
	var asked []string
	r.SetUI(
		func(_ context.Context, req api.ApprovalRequest) (api.Decision, error) {
			asked = append(asked, "approve:"+string(req.Kind))
			return api.DecisionOnce, nil
		},
		func(_ context.Context, q string, _ []string) (string, error) {
			asked = append(asked, "ask:"+q)
			return "tabs", nil
		},
	)
	s, err := r.NewSession()
	require.NoError(t, err)
	var events []string
	accepted, finished := false, false
	res, err := r.Run(context.Background(), s.ID, api.Turn{
		Text: "make a file", OnAccepted: func() { accepted = true }, OnFinished: func() { finished = true },
	}, func(e api.Event) {
		switch {
		case e.ToolCall != nil:
			events = append(events, "call:"+e.ToolCall.Name)
		case e.ToolResult != nil:
			events = append(events, "result:"+e.ToolResult.Name)
		}
	})
	require.NoError(t, err, "run: %+v %v accepted=%v finished=%v", res, err, accepted, finished)
	require.Equal(t, "all done", res.Output, "run: %+v %v accepted=%v finished=%v", res, err, accepted, finished)
	require.True(t, accepted, "run: %+v %v accepted=%v finished=%v", res, err, accepted, finished)
	require.True(t, finished, "run: %+v %v accepted=%v finished=%v", res, err, accepted, finished)
	assert.Equal(t, []string{"approve:write_file", "ask:Tabs or spaces?"}, asked, "asked %v", asked)
	assert.Equal(t, []string{"call:create_file", "result:create_file", "call:ask_user_question", "result:ask_user_question"}, events, "events %v", events)
	_, err = os.Stat(filepath.Join(r.Dir(), "made.txt"))
	assert.NoError(t, err, "approved write")
	active, _ := r.ActiveSession()
	assert.Len(t, active.Messages, 2, "transcript %+v", active.Messages)
}

func TestRemoteBlockedPrompt(t *testing.T) {
	r := attach(t, func(c *config.Config) {
		c.Hooks.PromptSubmit = []config.HookConfig{{Command: `echo "no secrets" >&2; exit 2`}}
	})
	s, _ := r.NewSession()
	_, err := r.Run(context.Background(), s.ID, api.Turn{Text: "my password"}, func(api.Event) {})
	var blocked *api.BlockedError
	assert.ErrorAs(t, err, &blocked, "blocked: %v", err)
	assert.Contains(t, blocked.Reason, "no secrets", "blocked: %v", err)
}

func TestRemoteImages(t *testing.T) {
	r := attach(t, nil, text("a small image"))
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 12, 8)))
	os.WriteFile(filepath.Join(r.Dir(), "a.png"), buf.Bytes(), 0o644)

	imgs, err := r.LoadAttachments([]string{"a.png"}, "", func(w string) { t.Errorf("warning %s", w) })
	require.NoError(t, err, "attachments %v", imgs)
	require.Len(t, imgs, 1, "attachments %v %v", imgs, err)
	require.Equal(t, 12, imgs[0].Width, "attachments %v %v", imgs, err)
	require.Contains(t, imgs[0].Summary(), "12×8", "attachments %v %v", imgs, err)
	_, err = r.LoadAttachments([]string{"missing.png"}, "", nil)
	assert.Error(t, err, "missing image")
	assert.Contains(t, err.Error(), "missing.png", "missing image: %v", err)
	s, _ := r.NewSession()
	_, err = r.Run(context.Background(), s.ID, api.Turn{Text: "what is this?", Images: imgs}, func(api.Event) {})
	require.NoError(t, err, "turn with an image")
}

func TestAttachReportsAnUnavailableModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	s := servicetest.New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		return engine.Open(ctx, cfg, engine.Options{})
	})
	srv := httptest.NewServer(s.Handler())
	defer func() { srv.Close(); s.Close() }()
	r, err := AttachHTTP(context.Background(), http.DefaultClient, srv.URL, t.TempDir(), nil)
	assert.NoError(t, err, "attach %v, model error %v", err, r.ModelErr())
	assert.Error(t, r.ModelErr(), "attach %v, model error %v", err, r.ModelErr())
}

// Limits travel with a remote turn and come back as app's errors, so an
// attached one-shot run exits as a local one does.
func TestRemoteTurnLimits(t *testing.T) {
	loop := make([]*genai.Content, 6)
	for i := range loop {
		loop[i] = call("list_files", map[string]any{})
	}
	r := attach(t, func(c *config.Config) { c.Blitz.AutoApprove = true }, loop...)
	sess, _, err := r.OpenSession("", false)
	require.NoError(t, err)
	_, err = r.Run(context.Background(), sess.ID, api.Turn{Text: "loop", MaxTurns: 2}, func(api.Event) {})
	assert.ErrorIs(t, err, api.ErrMaxTurns, "max turns over the API: %v", err)
	r2 := attach(t, func(c *config.Config) { c.Blitz.AutoApprove = true }, call("run_shell_command", map[string]any{"command": "sleep 5"}))
	sess2, _, _ := r2.OpenSession("", false)
	start := time.Now()
	_, err = r2.Run(context.Background(), sess2.ID, api.Turn{Text: "wait", Timeout: 300 * time.Millisecond}, func(api.Event) {})
	assert.ErrorIs(t, err, api.ErrTimeLimit, "timeout over the API: %v", err)
	assert.LessOrEqual(t, time.Since(start), 4*time.Second, "the timeout wasn't applied in the service")
}

// The permission mode is the workspace's in the service: set over the API
// and read back in the settings, with typed errors.
func TestRemotePermissionMode(t *testing.T) {
	r := attach(t, nil)
	m, err := r.SetPermissionMode("acceptEdits")
	require.NoError(t, err, "set: %q", m)
	require.Equal(t, "accept-edits", m, "set: %q %v", m, err)
	got := r.Settings().PermissionMode
	assert.Equal(t, "accept-edits", got, "settings mode %q", got)
	_, err = r.SetPermissionMode("yolo")
	assert.ErrorIs(t, err, api.ErrUnknownMode, "unknown mode over the API: %v", err)
}

// The session effort is set with Set and read back in the settings.
func TestRemoteEffort(t *testing.T) {
	r := attach(t, nil)
	_, err := r.Set(context.Background(), "effort", "high")
	require.NoError(t, err)
	got := r.Settings().Effort
	assert.Equal(t, "high", got, "settings effort %q", got)
	var invalid *api.InvalidSettingError
	_, err = r.Set(context.Background(), "effort", "extreme")
	assert.ErrorAs(t, err, &invalid, "invalid effort over the API: %v", err)
}

// Rewinding goes through the service: points, a conversation rewind, and
// typed errors.
func TestRemoteRewind(t *testing.T) {
	r := attach(t, nil, genai.NewContentFromText("one", genai.RoleModel), genai.NewContentFromText("two", genai.RoleModel))
	sess, _, err := r.OpenSession("", false)
	require.NoError(t, err)
	for _, p := range []string{"first", "second"} {
		t.Run(p, func(t *testing.T) {
			_, err := r.Run(context.Background(), sess.ID, api.Turn{Text: p}, func(api.Event) {})
			require.NoError(t, err)
		})
	}
	points, err := r.RewindPoints()
	require.NoError(t, err, "points %+v", points)
	require.Len(t, points, 2, "points %+v %v", points, err)
	require.Equal(t, "second", points[1].Text, "points %+v %v", points, err)
	require.True(t, points[1].Conversation, "points %+v %v", points, err)
	require.False(t, points[1].Time.IsZero(), "points %+v %v", points, err)
	res, err := r.Rewind(context.Background(), points[1].Index, api.RewindConversation, false)
	require.NoError(t, err, "rewind %+v", res)
	require.Equal(t, "second", res.Prompt, "rewind %+v %v", res, err)
	require.Equal(t, api.RewindConversation, res.Mode, "rewind %+v %v", res, err)
	a, _ := r.ActiveSession()
	assert.Equal(t, 2, a.MessageCount, "messages after rewinding: %d", a.MessageCount)
	require.NoError(t, r.Steer(context.Background(), sess.ID, "a steer message"))
	a, _ = r.ActiveSession()
	assert.Len(t, a.Messages, 3, "message kinds over the API: %+v", a.Messages)
	assert.Equal(t, "", a.Messages[0].Kind, "message kinds over the API: %+v", a.Messages)
	assert.Equal(t, "steer", a.Messages[2].Kind, "message kinds over the API: %+v", a.Messages)
	_, err = r.Rewind(context.Background(), 1, api.RewindBoth, false)
	assert.ErrorIs(t, err, api.ErrNotRewindPoint, "not a prompt: %v", err)
	_, err = r.Rewind(context.Background(), 0, "sideways", false)
	assert.ErrorIs(t, err, api.ErrUnknownRewindMode, "unknown mode: %v", err)
}

// The agent's task list reaches the client as Tasks events.
func TestRemoteTasks(t *testing.T) {
	todo := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "todo", Args: map[string]any{
		"items": []any{map[string]any{"content": "step one", "status": "in_progress"}}}}}}}
	r := attach(t, nil, todo, genai.NewContentFromText("ok", genai.RoleModel))
	sess, _, _ := r.OpenSession("", false)
	var tasks []api.Task
	_, err := r.Run(context.Background(), sess.ID, api.Turn{Text: "go"}, func(e api.Event) {
		if e.Tasks != nil {
			tasks = e.Tasks
		}
	})
	require.NoError(t, err)
	require.Len(t, tasks, 1, "tasks %+v", tasks)
	require.Equal(t, "step one", tasks[0].Content, "tasks %+v", tasks)
	require.Equal(t, "in_progress", tasks[0].Status, "tasks %+v", tasks)
}

// Reasoning settings survive the trip to the service and back.
func TestRemoteReasoningSettings(t *testing.T) {
	r := attach(t, nil)
	ch, err := r.UpdateModelSettings("gpt-5", false, []api.Setting{{Key: "reasoning_effort", Value: "high"}, {Key: "thinking_budget", Value: "2048"}})
	require.NoError(t, err)
	s := ch.Settings
	require.NotNil(t, s.ReasoningEffort, "after updating: %+v", s)
	require.Equal(t, "high", *s.ReasoningEffort, "after updating: %+v", s)
	require.NotNil(t, s.ThinkingBudget, "after updating: %+v", s)
	require.Equal(t, 2048, *s.ThinkingBudget, "after updating: %+v", s)
	all := r.AllModelSettings()
	assert.NotNil(t, all["gpt-5"].ReasoningEffort, "all settings lost the effort: %+v", all["gpt-5"])
}

func TestRemotePermissionRules(t *testing.T) {
	r := attach(t, nil)
	res, err := r.AddPermissionRule("ask", "Bash(git push *)", false)
	require.NoError(t, err, "add: %+v", res)
	require.Equal(t, "shell(git push *)", res.Rule, "add: %+v %v", res, err)
	got := r.ListPermissionRules()
	assert.Len(t, got, 1, "list %+v", got)
	assert.Equal(t, "ask", got[0].Effect, "list %+v", got)
	assert.Equal(t, "session", got[0].Source, "list %+v", got)
	_, err = r.AddPermissionRule("deny", "nope(x)", false)
	assert.ErrorIs(t, err, api.ErrBadRule, "bad rule over the API: %v", err)
	res, _ = r.RemovePermissionRule("shell(git push *)", false)
	assert.Equal(t, 1, res.Removed, "remove %+v", res)
}

func TestRemoteCommands(t *testing.T) {
	r := attach(t, nil, text("reviewed"))
	found := false
	for _, c := range r.ListCommands() {
		if c.Name == "review" && c.Source == "bundled" {
			found = true
		}
	}
	require.True(t, found, "bundled /review not listed over the API")
	sess, _, _ := r.OpenSession("", false)
	_, err := r.Run(context.Background(), sess.ID, api.Turn{Text: "/review", Command: true}, func(api.Event) {})
	assert.NoError(t, err, "run /review over the API")
	_, err = r.Run(context.Background(), sess.ID, api.Turn{Text: "/nope", Command: true}, func(api.Event) {})
	assert.ErrorIs(t, err, api.ErrUnknownCommand, "unknown command over the API: %v", err)
}
