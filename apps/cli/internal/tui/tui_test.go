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
	"bytes"
	"context"
	"errors"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

func TestLineReaderSharedSequentialReads(t *testing.T) {
	var out bytes.Buffer
	lr := NewLineReader(strings.NewReader("first\r\nsecond\nlast-no-newline"), &out)

	for _, want := range []string{"first", "second", "last-no-newline"} {
		t.Run(want, func(t *testing.T) {
			got, err := lr.ReadLine("> ")
			require.NoError(t, err, "ReadLine = %q, %v; want %q", got, err, want)
			require.Equal(t, want, got, "ReadLine = %q, %v; want %q", got, err, want)
		})
	}
	_, err := lr.ReadLine("> ")
	assert.ErrorIs(t, err, io.EOF, "expected EOF, got %v", err)
	assert.Equal(t, 4, strings.Count(out.String(), "> "), "prompt not echoed each time: %q", out.String())
}

func TestApprover(t *testing.T) {
	keyed := api.ApprovalRequest{Tool: "run_shell_command", Kind: api.ActionCommand, Detail: "rm -rf build\x1b]52;c;ZXZpbA==\x07",
		Key: "cmd:rm -rf build", KeyLabel: "this exact command"}
	cases := map[string]api.Decision{
		"y\n": api.DecisionOnce, "YES\n": api.DecisionOnce, " y \n": api.DecisionOnce,
		"s\n": api.DecisionSession, "a\n": api.DecisionAlways, "always\n": api.DecisionAlways,
		"\n": api.DecisionDeny, "n\n": api.DecisionDeny, "maybe\n": api.DecisionDeny,
	}
	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			var out bytes.Buffer
			approve := NewApprover(NewLineReader(strings.NewReader(input), &out), 0)
			got, err := approve(context.Background(), keyed)
			assert.NoError(t, err, "input %q: got %v, %v; want %v", input, got, err, want)
			assert.Equal(t, want, got, "input %q: got %v, %v; want %v", input, got, err, want)
			assert.NotContains(t, out.String(), "\x1b]52", "approval prompt echoed raw escape sequence: %q", out.String())
			assert.NotContains(t, out.String(), "\x07", "approval prompt echoed raw escape sequence: %q", out.String())
			assert.Contains(t, out.String(), "rm -rf build", "approval prompt missing detail or scope: %q", out.String())
			assert.Contains(t, out.String(), "this exact command", "approval prompt missing detail or scope: %q", out.String())
		})
	}

	// Without a key, session/always are unavailable and deny.
	unkeyed := api.ApprovalRequest{Tool: "x", Kind: api.ActionWrite, Detail: "d"}
	for _, in := range []string{"s\n", "a\n"} {
		t.Run(in, func(t *testing.T) {
			var out bytes.Buffer
			got, _ := NewApprover(NewLineReader(strings.NewReader(in), &out), 0)(context.Background(), unkeyed)
			assert.Equal(t, api.DecisionDeny, got, "%q without key should deny, got %v", in, got)
			assert.NotContains(t, out.String(), "[s]", "session option offered without a key")
		})
	}

	// Negative: EOF and cancelled context deny with an error.
	var out bytes.Buffer
	approve := NewApprover(NewLineReader(strings.NewReader(""), &out), 0)
	d, err := approve(context.Background(), keyed)
	assert.Equal(t, api.DecisionDeny, d, "expected EOF to deny with error, got %v %v", d, err)
	assert.Error(t, err, "expected EOF to deny with error, got %v", d)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d, err = approve(ctx, keyed)
	assert.Equal(t, api.DecisionDeny, d, "expected cancelled ctx to deny, got %v %v", d, err)
	assert.Error(t, err, "expected cancelled ctx to deny, got %v", d)
}

func TestApproverShowsDiff(t *testing.T) {
	diff := "--- a/f.go\n+++ b/f.go\n@@ -1,3 +1,3 @@\n ctx\n-old\n+new\n" + strings.Repeat(" more\n", 50)
	req := api.ApprovalRequest{Tool: "replace_in_file", Kind: api.ActionWrite, Detail: "Edit f.go", Diff: diff}

	// Truncated diff offers [d]; choosing it prints the full diff and re-asks.
	var out bytes.Buffer
	d, err := NewApprover(NewLineReader(strings.NewReader("d\ny\n"), &out), 10)(context.Background(), req)
	require.NoError(t, err, "got %v", d)
	require.Equal(t, api.DecisionOnce, d, "got %v %v", d, err)
	o := out.String()
	assert.Contains(t, o, Red+"-old"+Reset, "diff not colourised: %q", o)
	assert.Contains(t, o, Green+"+new"+Reset, "diff not colourised: %q", o)
	assert.Contains(t, o, "diff truncated", "expected truncation notice and [d] option")
	assert.Contains(t, o, "[d] show full diff", "expected truncation notice and [d] option")
	assert.GreaterOrEqual(t, strings.Count(o, " more"), 50, "full diff not shown after [d]")

	// Short diffs aren't truncated and don't offer [d].
	out.Reset()
	short := api.ApprovalRequest{Tool: "t", Kind: api.ActionWrite, Diff: "+x\n"}
	NewApprover(NewLineReader(strings.NewReader("y\n"), &out), 10)(context.Background(), short)
	assert.NotContains(t, out.String(), "[d]", "[d] offered for an untruncated diff")
}

func TestReadMultiline(t *testing.T) {
	cases := map[string]string{
		"single\n":                           "single",
		"first \\\nsecond\\\nthird\n":        "first \nsecond\nthird",
		"\"\"\"\nline 1\n\nline 3\n\"\"\"\n": "line 1\n\nline 3",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			got, err := NewLineReader(strings.NewReader(in), io.Discard).ReadInput(context.Background(), "> ")
			assert.NoError(t, err, "ReadInput(%q) = %q, %v; want %q", in, got, err, want)
			assert.Equal(t, want, got, "ReadInput(%q) = %q, %v; want %q", in, got, err, want)
		})
	}
	// Negative: EOF inside a block is an error, not a silent partial entry.
	_, err := NewLineReader(strings.NewReader("\"\"\"\nunterminated\n"), io.Discard).ReadInput(context.Background(), "> ")
	assert.Error(t, err, "expected error for unterminated block")
}

func TestUserPrompter(t *testing.T) {
	var out bytes.Buffer
	lr := NewLineReader(strings.NewReader("2\nfree text\n9\n"), &out)
	ask := NewUserPrompter(lr)
	opts := []string{"alpha", "beta"}

	got, _ := ask(context.Background(), "pick", opts)
	assert.Equal(t, "beta", got, "numeric choice = %q, want beta", got)
	got, _ = ask(context.Background(), "pick", opts)
	assert.Equal(t, "free text", got, "free text = %q", got)
	// Out-of-range number is returned verbatim rather than panicking.
	got, _ = ask(context.Background(), "pick", opts)
	assert.Equal(t, "9", got, "out-of-range = %q", got)
	_, err := ask(context.Background(), "pick", opts)
	assert.Error(t, err, "expected EOF error")
}

func TestFormatToolCallSanitizesAndTruncates(t *testing.T) {
	evil := "\x1b[2J\x1b]0;pwned\x07ls"
	s := FormatToolCall("run_shell_command", map[string]any{"command": evil})
	assert.NotContains(t, s, "\x1b[2J", "unsanitized output %q", s)
	assert.NotContains(t, s, "\x1b]0", "unsanitized output %q", s)
	assert.NotContains(t, s, "\x07", "unsanitized output %q", s)
	assert.Contains(t, s, "ls", "command text lost: %q", s)

	long := strings.Repeat("🐶", 40) // multi-byte; old code sliced mid-rune
	s = FormatToolCall("run_shell_command", map[string]any{"command": long})
	assert.True(t, utf8.ValidString(s), "truncation produced invalid UTF-8: %q", s)
	assert.Contains(t, s, "...", "expected ellipsis for long command")
}

func TestFormatToolResult(t *testing.T) {
	s := FormatToolResult("grep", true, "line1\n\x1b[31mFAKE ✅ [other] done\x1b[0m\nline3")
	assert.Equal(t, 1, strings.Count(s, "\n"), "summary should be collapsed to one line: %q", s)
	assert.NotContains(t, s, "\x1b[31m", "escape sequence leaked: %q", s)
	assert.Contains(t, FormatToolResult("x", false, ""), "done", "empty summary should render done badge")
}

func TestSummarizeToolResponse(t *testing.T) {
	s, ok := SummarizeToolResponse(map[string]any{"error": "boom"})
	assert.False(t, ok, "error summary = %q %v", s, ok)
	assert.Equal(t, "boom", s, "error summary = %q %v", s, ok)
	s, ok = SummarizeToolResponse(map[string]any{"result": "fine"})
	assert.True(t, ok, "result summary = %q %v", s, ok)
	assert.Equal(t, "fine", s, "result summary = %q %v", s, ok)
	s, _ = SummarizeToolResponse(map[string]any{"content": "abcd"})
	assert.Equal(t, "4 bytes read", s, "content summary = %q", s)
	s, ok = SummarizeToolResponse(nil)
	assert.True(t, ok, "nil summary = %q %v", s, ok)
	assert.Equal(t, "", s, "nil summary = %q %v", s, ok)
}

// isolateHome points HOME at a temporary directory, so opening a workspace
// never reads or writes the real ~/.blitz. Call it before
// config.DefaultConfig, which resolves paths under HOME.
func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
}

// modelFactory builds models by name, for tests of /model and pins.
type modelFactory = func(ctx context.Context, cfg *config.Config, name string) (model.LLM, error)

// openApp opens a workspace for cfg around llm and builds the App from it,
// as main does.
func openApp(t *testing.T, cfg *config.Config, llm model.LLM) *App {
	return openAppWith(t, cfg, engine.Options{Model: llm})
}

func openAppWith(t *testing.T, cfg *config.Config, o engine.Options) *App {
	t.Helper()
	cfg.Session.StorageDir = t.TempDir()
	w, err := engine.Open(context.Background(), cfg, o)
	require.NoError(t, err)
	t.Cleanup(func() { w.Close() })
	return &App{Workspace: w, Printer: PrinterOptions{Out: io.Discard}}
}

// local is the App's local workspace, for tests that look inside it.
func local(app *App) *engine.Workspace { return app.Workspace.(*engine.Workspace) }

// savedConfig reads back the config file that commands save to.
func savedConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join(os.Getenv("HOME"), ".blitz"))
	require.NoError(t, err)
	return cfg
}

func newTestApp(t *testing.T, factory modelFactory) *App {
	t.Helper()
	isolateHome(t)
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Images.Dir = t.TempDir()
	app := openAppWith(t, cfg, engine.Options{Model: runtime.NewMockLLM("mock-a"), NewModel: factory})
	app.Printer = PrinterOptions{}
	return app
}

func TestHandleCommandModel(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t, func(ctx context.Context, cfg *config.Config, name string) (model.LLM, error) {
		if name == "bad-model" {
			return nil, errors.New("unknown model")
		}
		return runtime.NewMockLLM(name), nil
	})

	// Positive: /model rebuilds the engine with the new LLM.
	handled, err := HandleCommand(ctx, "/model mock-b", app)
	require.True(t, handled, "handled=%v err=%v", handled, err)
	require.NoError(t, err, "handled=%v err=%v", handled, err)
	assert.Equal(t, "mock-b", local(app).Engine().ModelName(), "model not switched: engine=%q cfg=%q", local(app).Engine().ModelName(), local(app).Config().Blitz.DefaultModel)
	assert.Equal(t, "mock-b", local(app).Config().Blitz.DefaultModel, "model not switched: engine=%q cfg=%q", local(app).Engine().ModelName(), local(app).Config().Blitz.DefaultModel)
	// Negative: factory failure leaves the current model in place.
	HandleCommand(ctx, "/model bad-model", app)
	assert.Equal(t, "mock-b", local(app).Engine().ModelName(), "failed switch changed model to %q", local(app).Engine().ModelName())
	assert.Equal(t, "mock-b", local(app).Config().Blitz.DefaultModel, "failed switch changed model to %q", local(app).Engine().ModelName())
}

func TestHandleCommandSetAndSession(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t, nil)

	HandleCommand(ctx, "/set agency=low", app)
	assert.Equal(t, "low", local(app).Config().Blitz.AgencyLevel, "agency not updated: %q", local(app).Config().Blitz.AgencyLevel)
	// Negative: invalid agency rejected.
	HandleCommand(ctx, "/set agency=reckless", app)
	assert.Equal(t, "low", local(app).Config().Blitz.AgencyLevel, "invalid agency accepted: %q", local(app).Config().Blitz.AgencyLevel)
	// The persona settings are gone.
	out := captureStdout(t, func() { HandleCommand(ctx, "/set owner_name=Ada", app) })
	assert.Contains(t, out, "owner_name", "owner_name should be unknown now:\n%s", out)

	// /session new records the active agent and becomes active.
	HandleCommand(ctx, "/agent helios", app)
	HandleCommand(ctx, "/session new", app)
	a := local(app).Storage().Active()
	assert.NotNil(t, a, "new session should use active agent, got %+v", a)
	assert.Equal(t, "helios", a.Agent, "new session should use active agent, got %+v", a)
	// Negative: unknown agent keeps current one.
	HandleCommand(ctx, "/agent ghost", app)
	assert.Equal(t, "helios", local(app).Engine().ActiveAgent(), "unknown agent changed active agent")
}

func TestHandleCommandRouting(t *testing.T) {
	app := newTestApp(t, nil)
	ctx := context.Background()
	handled, _ := HandleCommand(ctx, "hello puppy", app)
	assert.False(t, handled, "plain text should not be handled as a command")
	_, err := HandleCommand(ctx, "/quit", app)
	assert.ErrorIs(t, err, ErrExit, "expected ErrExit, got %v", err)
	handled, err = HandleCommand(ctx, "/nonsense", app)
	assert.True(t, handled, "unknown command: handled=%v err=%v", handled, err)
	assert.NoError(t, err, "unknown command: handled=%v err=%v", handled, err)
	// /model without a factory is reported, not a panic.
	handled, _ = HandleCommand(ctx, "/model x", app)
	assert.True(t, handled, "expected /model to be handled")
}

func TestRunREPLUsesCurrentSession(t *testing.T) {
	app := newTestApp(t, nil)
	var out bytes.Buffer
	app.Input = NewLineReader(strings.NewReader("hello\n/session new\nsecond\n/exit\n"), &out)
	require.NoError(t, RunREPL(context.Background(), app))
	// The second prompt must be recorded in the session created by /session new.
	active := local(app).Storage().Active()
	assert.NotNil(t, active, "expected 'second' in the new active session, got %+v", active)
	assert.NotEqual(t, 0, len(active.Messages), "expected 'second' in the new active session, got %+v", active)
	assert.Equal(t, "second", active.Messages[0].Content, "expected 'second' in the new active session, got %+v", active)

	// EOF ends the REPL cleanly.
	app.Input = NewLineReader(strings.NewReader(""), &out)
	assert.NoError(t, RunREPL(context.Background(), app), "EOF should end REPL without error, got")
}

func TestLineReaderCancelDoesNotLoseInput(t *testing.T) {
	pr, pw := io.Pipe()
	var out bytes.Buffer
	lr := NewLineReader(pr, &out)

	// Negative: a cancelled read returns promptly with ctx error.
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { _, err := lr.Ask(ctx, "> "); errCh <- err }()
	cancel()
	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled, "expected context.Canceled, got %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled Ask did not return")
	}

	// Positive: the line typed afterwards goes to the next reader, not the abandoned one.
	go pw.Write([]byte("kept\n"))
	got, err := lr.ReadLine("> ")
	require.NoError(t, err, "expected 'kept', got %q", got)
	require.Equal(t, "kept", got, "expected 'kept', got %q %v", got, err)
	pw.Close()
}

func TestLineReaderSerializesConcurrentAsks(t *testing.T) {
	var out syncBuffer
	lr := NewLineReader(strings.NewReader("a\nb\n"), &out)
	var wg sync.WaitGroup
	answers := make(chan string, 2)
	for _, q := range []string{"Q1:\n", "Q2:\n"} {
		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			ans, _ := lr.Ask(context.Background(), q)
			answers <- q + ans
		}(q)
	}
	wg.Wait()
	close(answers)
	got := map[string]bool{}
	for a := range answers {
		got[a] = true
	}
	// Each prompt must be paired with exactly one full answer.
	assert.False(t, !(got["Q1:\na"] && got["Q2:\nb"]) && !(got["Q1:\nb"] && got["Q2:\na"]), "answers mixed up: %v", got)
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

// blockingLLM blocks until the request context is cancelled.
type blockingLLM struct{ started chan struct{} }

func (b *blockingLLM) Name() string { return "blocking" }
func (b *blockingLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		select {
		case b.started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		yield(nil, ctx.Err())
	}
}

func TestREPLInterruptAtPromptExits(t *testing.T) {
	app := newTestApp(t, nil)
	pr, pw := io.Pipe()
	defer pw.Close()
	app.Input = NewLineReader(pr, io.Discard)
	sigs := make(chan os.Signal, 1)
	app.Interrupts = sigs

	done := make(chan error, 1)
	go func() { done <- RunREPL(context.Background(), app) }()
	time.Sleep(100 * time.Millisecond)
	sigs <- os.Interrupt
	select {
	case err := <-done:
		assert.NoError(t, err, "expected clean exit, got")
	case <-time.After(3 * time.Second):
		t.Fatal("REPL did not exit on interrupt while idle")
	}
}

func TestREPLInterruptCancelsTurnOnly(t *testing.T) {
	app := newTestApp(t, nil)
	llm := &blockingLLM{started: make(chan struct{}, 1)}
	require.NoError(t, local(app).Engine().SetModel(context.Background(), llm))
	pr, pw := io.Pipe()
	app.Input = NewLineReader(pr, io.Discard)
	sigs := make(chan os.Signal, 1)
	app.Interrupts = sigs

	done := make(chan error, 1)
	go func() { done <- RunREPL(context.Background(), app) }()

	go pw.Write([]byte("long task\n"))
	select {
	case <-llm.started:
	case <-time.After(3 * time.Second):
		t.Fatal("turn never started")
	}
	sigs <- os.Interrupt

	// The REPL must survive the interrupt and keep accepting input.
	go pw.Write([]byte("/exit\n"))
	select {
	case err := <-done:
		assert.NoError(t, err, "unexpected error")
	case <-time.After(3 * time.Second):
		t.Fatal("REPL did not return to the prompt after interrupting a turn")
	}
	pw.Close()
}
