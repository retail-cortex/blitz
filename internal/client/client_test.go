package client

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/internal/app"
	"github.com/retail-cortex/blitz/internal/config"
	"github.com/retail-cortex/blitz/internal/runtime"
	"github.com/retail-cortex/blitz/internal/server"
	"github.com/retail-cortex/blitz/internal/tools"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// attach starts a service whose workspaces run on mock models answering
// with replies, and attaches to a new workspace in it.
func attach(t *testing.T, mutate func(*config.Config), replies ...*genai.Content) *Remote {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	s := server.New(func(ctx context.Context, dir string) (*app.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		cfg.Images.Dir = t.TempDir()
		if mutate != nil {
			mutate(cfg)
		}
		return app.Open(ctx, cfg, app.Options{
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
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if len(warnings) > 0 {
			t.Errorf("warnings: %q", warnings)
		}
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
	if r.ModelErr() != nil || r.Model().Name == "" || r.ActiveAgent().Name != "blitz" {
		t.Fatalf("model %v %v, agent %v", r.ModelErr(), r.Model(), r.ActiveAgent())
	}
	var unknown *app.UnknownAgentError
	if _, err := r.PinModel(ctx, "nobody", "x"); !errors.As(err, &unknown) || unknown.Name != "nobody" {
		t.Errorf("unknown agent: %v", err)
	}
	var invalid *app.InvalidSettingError
	if _, err := r.UpdateModelSettings("gpt-5", false, []app.Setting{{Key: "temperature", Value: "9"}}); !errors.As(err, &invalid) {
		t.Errorf("invalid setting: %v", err)
	}
	if _, err := r.SaveSnapshot("s", false); !errors.Is(err, app.ErrNoActiveSession) {
		t.Errorf("no session: %v", err)
	}
	if _, err := r.Set(ctx, "agency", "reckless"); !errors.Is(err, app.ErrInvalidAgency) {
		t.Errorf("agency: %v", err)
	}
	res, err := r.PinModel(ctx, "qa", "anthropic/claude-haiku-4-5")
	if err != nil || res.Model != "claude-haiku-4-5" || res.Saved.Err != nil {
		t.Fatalf("pin %+v %v", res, err)
	}
	if i := slices.IndexFunc(r.ListAgents(), func(a app.AgentInfo) bool { return a.Name == "qa" }); i < 0 || r.ListAgents()[i].PinnedModel != "claude-haiku-4-5" {
		t.Error("pin not listed")
	}
	if !r.ImagesEnabled() || r.Processes() != nil || len(r.SandboxSummary()) == 0 {
		t.Errorf("images %v, processes %v, sandbox %v", r.ImagesEnabled(), r.Processes(), r.SandboxSummary())
	}
}

func TestRemoteTurnWithApprovalAndQuestion(t *testing.T) {
	create := call("create_file", map[string]any{"path": "made.txt", "content": "hi\n"})
	ask := call("ask_user_question", map[string]any{"question": "Tabs or spaces?"})
	r := attach(t, func(c *config.Config) { c.Blitz.AutoApprove = false }, create, ask, text("all done"))
	var asked []string
	r.SetUI(
		func(_ context.Context, req tools.ApprovalRequest) (tools.Decision, error) {
			asked = append(asked, "approve:"+string(req.Kind))
			return tools.DecisionOnce, nil
		},
		func(_ context.Context, q string, _ []string) (string, error) {
			asked = append(asked, "ask:"+q)
			return "tabs", nil
		},
	)
	s, err := r.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	accepted, finished := false, false
	res, err := r.Run(context.Background(), s.ID, app.Turn{
		Text: "make a file", OnAccepted: func() { accepted = true }, OnFinished: func() { finished = true },
	}, func(e app.Event) {
		switch {
		case e.ToolCall != nil:
			events = append(events, "call:"+e.ToolCall.Name)
		case e.ToolResult != nil:
			events = append(events, "result:"+e.ToolResult.Name)
		}
	})
	if err != nil || res.Output != "all done" || !accepted || !finished {
		t.Fatalf("run: %+v %v accepted=%v finished=%v", res, err, accepted, finished)
	}
	if !slices.Equal(asked, []string{"approve:write_file", "ask:Tabs or spaces?"}) {
		t.Errorf("asked %v", asked)
	}
	if !slices.Equal(events, []string{"call:create_file", "result:create_file", "call:ask_user_question", "result:ask_user_question"}) {
		t.Errorf("events %v", events)
	}
	if _, err := os.Stat(filepath.Join(r.Dir(), "made.txt")); err != nil {
		t.Errorf("approved write: %v", err)
	}
	if active, _ := r.ActiveSession(); len(active.Messages) != 2 {
		t.Errorf("transcript %+v", active.Messages)
	}
}

func TestRemoteBlockedPrompt(t *testing.T) {
	r := attach(t, func(c *config.Config) {
		c.Hooks.PromptSubmit = []config.HookConfig{{Command: `echo "no secrets" >&2; exit 2`}}
	})
	s, _ := r.NewSession()
	_, err := r.Run(context.Background(), s.ID, app.Turn{Text: "my password"}, func(app.Event) {})
	var blocked *app.BlockedError
	if !errors.As(err, &blocked) || !strings.Contains(blocked.Reason, "no secrets") {
		t.Errorf("blocked: %v", err)
	}
}

func TestRemoteImages(t *testing.T) {
	r := attach(t, nil, text("a small image"))
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 12, 8)))
	os.WriteFile(filepath.Join(r.Dir(), "a.png"), buf.Bytes(), 0o644)

	imgs, err := r.LoadAttachments([]string{"a.png"}, "", func(w string) { t.Errorf("warning %s", w) })
	if err != nil || len(imgs) != 1 || imgs[0].Width != 12 || !strings.Contains(imgs[0].Summary(), "12×8") {
		t.Fatalf("attachments %v %v", imgs, err)
	}
	if _, err := r.LoadAttachments([]string{"missing.png"}, "", nil); err == nil || !strings.Contains(err.Error(), "missing.png") {
		t.Errorf("missing image: %v", err)
	}
	s, _ := r.NewSession()
	if _, err := r.Run(context.Background(), s.ID, app.Turn{Text: "what is this?", Images: imgs}, func(app.Event) {}); err != nil {
		t.Fatalf("turn with an image: %v", err)
	}
}

func TestAttachReportsAnUnavailableModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	s := server.New(func(ctx context.Context, dir string) (*app.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		return app.Open(ctx, cfg, app.Options{})
	})
	srv := httptest.NewServer(s.Handler())
	defer func() { srv.Close(); s.Close() }()
	r, err := AttachHTTP(context.Background(), http.DefaultClient, srv.URL, t.TempDir(), nil)
	if err != nil || r.ModelErr() == nil {
		t.Errorf("attach %v, model error %v", err, r.ModelErr())
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(context.Background(), sess.ID, app.Turn{Text: "loop", MaxTurns: 2}, func(app.Event) {}); !errors.Is(err, app.ErrMaxTurns) {
		t.Errorf("max turns over the API: %v", err)
	}
	r2 := attach(t, func(c *config.Config) { c.Blitz.AutoApprove = true }, call("run_shell_command", map[string]any{"command": "sleep 5"}))
	sess2, _, _ := r2.OpenSession("", false)
	start := time.Now()
	if _, err := r2.Run(context.Background(), sess2.ID, app.Turn{Text: "wait", Timeout: 300 * time.Millisecond}, func(app.Event) {}); !errors.Is(err, app.ErrTimeLimit) {
		t.Errorf("timeout over the API: %v", err)
	}
	if time.Since(start) > 4*time.Second {
		t.Error("the timeout wasn't applied in the service")
	}
}

// The permission mode is the workspace's in the service: set over the API
// and read back in the settings, with typed errors.
func TestRemotePermissionMode(t *testing.T) {
	r := attach(t, nil)
	if m, err := r.SetPermissionMode("acceptEdits"); err != nil || m != "accept-edits" {
		t.Fatalf("set: %q %v", m, err)
	}
	if got := r.Settings().PermissionMode; got != "accept-edits" {
		t.Errorf("settings mode %q", got)
	}
	if _, err := r.SetPermissionMode("yolo"); !errors.Is(err, app.ErrUnknownMode) {
		t.Errorf("unknown mode over the API: %v", err)
	}
}

// The session effort is set with Set and read back in the settings.
func TestRemoteEffort(t *testing.T) {
	r := attach(t, nil)
	if _, err := r.Set(context.Background(), "effort", "high"); err != nil {
		t.Fatal(err)
	}
	if got := r.Settings().Effort; got != "high" {
		t.Errorf("settings effort %q", got)
	}
	var invalid *app.InvalidSettingError
	if _, err := r.Set(context.Background(), "effort", "extreme"); !errors.As(err, &invalid) {
		t.Errorf("invalid effort over the API: %v", err)
	}
}

// Rewinding goes through the service: points, a conversation rewind, and
// typed errors.
func TestRemoteRewind(t *testing.T) {
	r := attach(t, nil, genai.NewContentFromText("one", genai.RoleModel), genai.NewContentFromText("two", genai.RoleModel))
	sess, _, err := r.OpenSession("", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"first", "second"} {
		if _, err := r.Run(context.Background(), sess.ID, app.Turn{Text: p}, func(app.Event) {}); err != nil {
			t.Fatal(err)
		}
	}
	points, err := r.RewindPoints()
	if err != nil || len(points) != 2 || points[1].Text != "second" || !points[1].Conversation || points[1].Time.IsZero() {
		t.Fatalf("points %+v %v", points, err)
	}
	res, err := r.Rewind(context.Background(), points[1].Index, app.RewindConversation, false)
	if err != nil || res.Prompt != "second" || res.Mode != app.RewindConversation {
		t.Fatalf("rewind %+v %v", res, err)
	}
	if a, _ := r.ActiveSession(); a.MessageCount != 2 {
		t.Errorf("messages after rewinding: %d", a.MessageCount)
	}
	if _, err := r.Rewind(context.Background(), 1, app.RewindBoth, false); !errors.Is(err, app.ErrNotRewindPoint) {
		t.Errorf("not a prompt: %v", err)
	}
	if _, err := r.Rewind(context.Background(), 0, "sideways", false); !errors.Is(err, app.ErrUnknownRewindMode) {
		t.Errorf("unknown mode: %v", err)
	}
}

// The agent's task list reaches the client as Tasks events.
func TestRemoteTasks(t *testing.T) {
	todo := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "todo", Args: map[string]any{
		"items": []any{map[string]any{"content": "step one", "status": "in_progress"}}}}}}}
	r := attach(t, nil, todo, genai.NewContentFromText("ok", genai.RoleModel))
	sess, _, _ := r.OpenSession("", false)
	var tasks []app.Task
	if _, err := r.Run(context.Background(), sess.ID, app.Turn{Text: "go"}, func(e app.Event) {
		if e.Tasks != nil {
			tasks = e.Tasks
		}
	}); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Content != "step one" || tasks[0].Status != "in_progress" {
		t.Fatalf("tasks %+v", tasks)
	}
}

// Reasoning settings survive the trip to the service and back.
func TestRemoteReasoningSettings(t *testing.T) {
	r := attach(t, nil)
	ch, err := r.UpdateModelSettings("gpt-5", false, []app.Setting{{Key: "reasoning_effort", Value: "high"}, {Key: "thinking_budget", Value: "2048"}})
	if err != nil {
		t.Fatal(err)
	}
	s := ch.Settings
	if s.ReasoningEffort == nil || *s.ReasoningEffort != "high" || s.ThinkingBudget == nil || *s.ThinkingBudget != 2048 {
		t.Fatalf("after updating: %+v", s)
	}
	if all := r.AllModelSettings(); all["gpt-5"].ReasoningEffort == nil {
		t.Errorf("all settings lost the effort: %+v", all["gpt-5"])
	}
}

func TestRemotePermissionRules(t *testing.T) {
	r := attach(t, nil)
	if res, err := r.AddPermissionRule("ask", "Bash(git push *)", false); err != nil || res.Rule != "shell(git push *)" {
		t.Fatalf("add: %+v %v", res, err)
	}
	if got := r.ListPermissionRules(); len(got) != 1 || got[0].Effect != "ask" || got[0].Source != "session" {
		t.Errorf("list %+v", got)
	}
	if _, err := r.AddPermissionRule("deny", "nope(x)", false); !errors.Is(err, app.ErrBadRule) {
		t.Errorf("bad rule over the API: %v", err)
	}
	if res, _ := r.RemovePermissionRule("shell(git push *)", false); res.Removed != 1 {
		t.Errorf("remove %+v", res)
	}
}

func TestRemoteCommands(t *testing.T) {
	r := attach(t, nil, text("reviewed"))
	found := false
	for _, c := range r.ListCommands() {
		if c.Name == "review" && c.Source == "bundled" {
			found = true
		}
	}
	if !found {
		t.Fatal("bundled /review not listed over the API")
	}
	sess, _, _ := r.OpenSession("", false)
	if _, err := r.Run(context.Background(), sess.ID, app.Turn{Text: "/review", Command: true}, func(app.Event) {}); err != nil {
		t.Errorf("run /review over the API: %v", err)
	}
	if _, err := r.Run(context.Background(), sess.ID, app.Turn{Text: "/nope", Command: true}, func(app.Event) {}); !errors.Is(err, app.ErrUnknownCommand) {
		t.Errorf("unknown command over the API: %v", err)
	}
}
