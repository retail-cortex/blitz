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
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestCompleteBlocks(t *testing.T) {
	cases := map[string]int{
		"no newline yet":            0,
		"line one\n":                0,
		"para one\n\npara two":      len("para one\n\n"),
		"```go\nx := 1\n\ny := 2\n": 0, // blank line inside a fence doesn't split
		"```go\nx := 1\n```\nafter": len("```go\nx := 1\n```\n"),
		"a\n\nb\n\nc":               len("a\n\nb\n\n"),
		"~~~\ncode\n~~~\n":          len("~~~\ncode\n~~~\n"),
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			got := completeBlocks(in)
			assert.Equal(t, want, got, "completeBlocks(%q) = %d, want %d", in, got, want)
		})
	}
}

func TestMarkdownStreamRendersProgressively(t *testing.T) {
	var out bytes.Buffer
	md, err := newMarkdownStream(&out, "dark", 80)
	require.NoError(t, err)
	md.Write("# Title\n\nSome **bold**")
	first := out.String()
	assert.Contains(t, first, "Title", "expected only the completed heading block, got %q", first)
	assert.NotContains(t, first, "bold", "expected only the completed heading block, got %q", first)
	md.Write(" text.\n")
	md.Flush()
	assert.Contains(t, out.String(), "bold", "markdown not rendered: %q", out.String())
	assert.NotContains(t, out.String(), "**", "markdown not rendered: %q", out.String())
}

func textEvent(text string, partial, repeat bool) api.Event {
	return api.Event{Text: &api.Text{Text: text, Partial: partial, Repeat: repeat}}
}

func TestPrinterStreamingDedup(t *testing.T) {
	var out bytes.Buffer
	p := NewPrinter(PrinterOptions{Out: &out})
	p.Handle(textEvent("Hel", true, false))
	p.Handle(textEvent("lo", true, false))
	p.Handle(textEvent("Hello", false, true)) // final aggregate repeats streamed text
	p.Handle(textEvent(" again", false, false))
	assert.Equal(t, "Hello again", out.String(), "printed %q, want streamed text once", out.String())
	// Control sequences in model text are neutralised.
	out.Reset()
	p.Handle(textEvent("\x1b]52;c;x\x07ok", false, false))
	assert.False(t, strings.ContainsRune(out.String(), '\x1b'), "escape leaked: %q", out.String())
}

func TestSpinner(t *testing.T) {
	var out syncBuffer
	s := NewSpinner(&out, true)
	s.Start("thinking")
	s.Start("again") // no second spinner
	time.Sleep(150 * time.Millisecond)
	s.Stop()
	s.Stop() // idempotent
	o := out.b.String()
	assert.Contains(t, o, "thinking", "spinner output %q", o)
	assert.NotContains(t, o, "again", "spinner output %q", o)
	assert.True(t, strings.HasSuffix(o, "\r\033[2K"), "spinner output %q", o)
	var off bytes.Buffer
	d := NewSpinner(&off, false)
	d.Start("x")
	d.Stop()
	assert.Equal(t, 0, off.Len(), "disabled spinner wrote output")
	var nilSpinner *Spinner
	nilSpinner.Start("x")
	nilSpinner.Stop()
}

func TestCompleter(t *testing.T) {
	ws := t.TempDir()
	os.MkdirAll(filepath.Join(ws, "src", "pkg"), 0o755)
	os.WriteFile(filepath.Join(ws, "src", "main.go"), nil, 0o644)
	os.WriteFile(filepath.Join(ws, ".env"), nil, 0o644)
	c := NewCompleter(ws)
	c.Command("undo", "--force")
	c.Command("help")
	c.Dynamic("agent", func() []string { return []string{"helios", "blitz"} })

	do := func(line string) []string {
		cands, _ := c.Do([]rune(line), len([]rune(line)))
		var out []string
		for _, r := range cands {
			out = append(out, string(r))
		}
		return out
	}
	got := do("/un")
	assert.Len(t, got, 1, "/un -> %q", got)
	assert.Equal(t, "do ", got[0], "/un -> %q", got)
	got = do("/agent he")
	assert.Len(t, got, 1, "/agent he -> %q", got)
	assert.Equal(t, "lios", got[0], "/agent he -> %q", got)
	got = do("/undo ")
	assert.Len(t, got, 1, "/undo -> %q", got)
	assert.Equal(t, "--force", got[0], "/undo -> %q", got)
	assert.Equal(t, "main.go,pkg/", strings.Join(do("look at @src/"), ","), "@src/ ->")
	// Negative: hidden files only when asked for, no traversal, nothing for plain words.
	got = do("@")
	assert.NotContains(t, strings.Join(got, ","), ".env", "hidden file offered: %q", got)
	got = do("@.")
	assert.Contains(t, strings.Join(got, ","), "env", "hidden file not offered for '@.': %q", got)
	got = do("@../")
	assert.Len(t, got, 0, "traversal completed: %q", got)
	got = do("hello wor")
	assert.Len(t, got, 0, "plain text completed: %q", got)
}

func TestUsageLine(t *testing.T) {
	before := api.Usage{Calls: 1, Input: 1000, Output: 100, Priced: true, CostUSD: 0.01}
	after := api.Usage{Calls: 3, Input: 13_400, Output: 1_300, LastPrompt: 12_345, Priced: true, CostUSD: 0.0325}
	got := UsageLine(before, after)
	for _, want := range []string{"12.4k in", "1.2k out", "context 12.3k", "$0.0225", "session $0.0325"} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, got, want, "usage line %q missing %q", got, want)
		})
	}
	after.Priced = false
	assert.NotContains(t, UsageLine(before, after), "$", "unpriced usage should not show cost")
	assert.Equal(t, "", UsageLine(before, before), "no calls -> no line")
}

func newFullApp(t *testing.T) *App {
	t.Helper()
	isolateHome(t)
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = filepath.Join(t.TempDir(), "approvals.json")
	cfg.Images.Dir = t.TempDir()
	cfg.Blitz.AutoApprove = true
	llm := runtime.NewMockLLM("gemini-3.8-flash",
		&genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "made.txt", "content": "by tool\n"}}}}},
		genai.NewContentFromText("created", genai.RoleModel))
	llm.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 500, CandidatesTokenCount: 50}
	app := openApp(t, cfg, llm)
	return app
}

// captureStdout runs f and returns what it printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	f()
	w.Close()
	os.Stdout = old
	return <-done
}

func TestREPLTurnUndoDiffCost(t *testing.T) {
	app := newFullApp(t)
	app.Input = NewLineReader(strings.NewReader("make a file\n/diff\n/cost\n/checkpoints\n/undo\n/exit\n"), io.Discard)
	out := captureStdout(t, func() {
		assert.NoError(t, RunREPL(context.Background(), app))
	})
	made := filepath.Join(local(app).Tools().Workspace().Dir(), "made.txt")
	_, err := os.Stat(made)
	assert.ErrorIs(t, err, fs.ErrNotExist, "/undo did not remove the file the turn created")
	for _, want := range []string{"+by tool", "Model calls:   2", "make a file", "Undid", "context 500"} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, out, want, "REPL output missing %q:\n%s", want, out)
		})
	}
}

func TestApprovalsAndMemoryCommands(t *testing.T) {
	app := newFullApp(t)
	ctx := context.Background()
	hooks := local(app).Tools().Hooks()
	hooks.Store().Add("cmd:ls -la", "")

	out := captureStdout(t, func() { HandleCommand(ctx, "/approvals", app) })
	assert.Contains(t, out, "command: ls -la", "/approvals output: %s", out)
	captureStdout(t, func() { HandleCommand(ctx, "/approvals revoke 1", app) })
	assert.False(t, hooks.Store().Has("cmd:ls -la"), "revoke did not remove the rule")
	out = captureStdout(t, func() { HandleCommand(ctx, "/approvals revoke 9", app) })
	assert.Contains(t, out, "Usage", "bad revoke index: %s", out)

	captureStdout(t, func() { HandleCommand(ctx, "/memory add always run go vet", app) })
	b, err := os.ReadFile(filepath.Join(app.Workspace.Dir(), "BLITZ.md"))
	assert.NoError(t, err, "/memory add: %q", b)
	assert.Contains(t, string(b), "- always run go vet", "/memory add: %q %v", b, err)
	out = captureStdout(t, func() { HandleCommand(ctx, "/memory", app) })
	assert.Contains(t, out, "BLITZ.md", "/memory: %s", out)
	out = captureStdout(t, func() { HandleCommand(ctx, "/mcp", app) })
	assert.Contains(t, out, "No MCP servers", "/mcp: %s", out)
	out = captureStdout(t, func() { HandleCommand(ctx, "/undo", app) })
	assert.Contains(t, out, "nothing to undo", "/undo with nothing: %s", out)
}

func TestResumeCommand(t *testing.T) {
	app := newFullApp(t)
	rec, _ := local(app).Storage().CreateSession("", "earlier", "blitz")
	local(app).Storage().AddMessage("user", "remember the plan")
	local(app).Storage().CreateSession("", "later", "blitz")

	out := captureStdout(t, func() { HandleCommand(context.Background(), "/resume "+rec.ID, app) })
	assert.Equal(t, rec.ID, local(app).Storage().Active().ID, "resume: active=%s out=%s", local(app).Storage().Active().ID, out)
	assert.Contains(t, out, "remember the plan", "resume: active=%s out=%s", local(app).Storage().Active().ID, out)
	out = captureStdout(t, func() { HandleCommand(context.Background(), "/resume ../../etc", app) })
	assert.Contains(t, out, "invalid session id", "bad id: %s", out)
}

func TestThemeFromEnv(t *testing.T) {
	for v, want := range map[string]string{"": "dark", "15;0": "dark", "0;15": "light", "0;7": "light", "garbage": "dark"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("COLORFGBG", v)
			got := themeFromEnv()
			assert.Equal(t, want, got, "COLORFGBG=%q -> %q, want %q", v, got, want)
		})
	}
}

// ctrlCInput scripts REPL lines and simulates Ctrl+C at any Ask prompt, the
// way the line editor reports it in raw mode (no SIGINT).
type ctrlCInput struct {
	lines   []string
	handler func()
	asked   int
}

func (c *ctrlCInput) SetInterruptHandler(f func()) { c.handler = f }
func (c *ctrlCInput) Ask(ctx context.Context, text string) (string, error) {
	c.asked++
	if c.handler != nil {
		c.handler()
	}
	return "", context.Canceled
}
func (c *ctrlCInput) ReadInput(ctx context.Context, prompt string) (string, error) {
	if len(c.lines) == 0 {
		return "", io.EOF
	}
	l := c.lines[0]
	c.lines = c.lines[1:]
	return l, nil
}

func TestCtrlCAtApprovalCancelsTurn(t *testing.T) {
	isolateHome(t)
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = filepath.Join(t.TempDir(), "approvals.json")
	cfg.Images.Dir = t.TempDir()
	cfg.Blitz.AutoApprove = false // approvals required
	llm := runtime.NewMockLLM("m",
		&genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "x.txt", "content": "x"}}}}},
		genai.NewContentFromText("should never be reached", genai.RoleModel))
	app := openApp(t, cfg, llm)
	reg := local(app).Tools()
	in := &ctrlCInput{lines: []string{"make x"}}
	app.Input = in
	reg.Hooks().SetApprover(NewApprover(in, 0))

	out := captureStdout(t, func() { RunREPL(context.Background(), app) })
	require.NotEqual(t, 0, in.asked, "approval was never requested")
	assert.Contains(t, out, "Interrupted", "turn was not cancelled by Ctrl+C at the approval prompt:\n%s", out)
	assert.LessOrEqual(t, llm.Calls(), 1, "model kept running after Ctrl+C (%d calls)", llm.Calls())
	assert.Nil(t, in.handler, "interrupt handler should be cleared after the turn")
}

func TestSessionListScopedToWorkspace(t *testing.T) {
	app := newFullApp(t)
	local(app).Storage().SetWorkspace("/proj/other")
	local(app).Storage().CreateSession("", "elsewhere", "blitz")
	local(app).Storage().AddMessage("user", "hi") // saved with its first message
	local(app).Storage().SetWorkspace("/proj/here")
	local(app).Storage().CreateSession("", "local", "blitz")
	local(app).Storage().AddMessage("user", "hi")

	out := captureStdout(t, func() { HandleCommand(context.Background(), "/session list", app) })
	assert.Contains(t, out, "local", "/session list should show only this workspace:\n%s", out)
	assert.NotContains(t, out, "elsewhere", "/session list should show only this workspace:\n%s", out)
	assert.Contains(t, out, "(1)", "/session list should show only this workspace:\n%s", out)
	out = captureStdout(t, func() { HandleCommand(context.Background(), "/session list --all", app) })
	assert.Contains(t, out, "elsewhere", "/session list --all should show every workspace:\n%s", out)
	assert.Contains(t, out, "/proj/other", "/session list --all should show every workspace:\n%s", out)
}

func TestCompactCommand(t *testing.T) {
	app := newFullApp(t) // mock: create_file call, "created", then default replies
	ctx := context.Background()
	assert.Contains(t, captureStdout(t, func() { HandleCommand(ctx, "/compact", app) }), "No active session", "no session")
	local(app).Storage().CreateSession("", "t", "blitz")
	sid := local(app).Storage().Active().ID
	for _, p := range []string{"make a file", "second turn"} {
		t.Run(p, func(t *testing.T) {
			require.NoError(t, local(app).Engine().Execute(ctx, sid, p, nil))
		})
	}
	out := captureStdout(t, func() { HandleCommand(ctx, "/compact keep file names", app) })
	assert.Contains(t, out, "Replaced", "/compact output: %s", out)
	fresh := newFullApp(t)
	local(fresh).Storage().CreateSession("", "t", "blitz")
	out = captureStdout(t, func() { HandleCommand(ctx, "/compact", fresh) })
	assert.Contains(t, out, "nothing to compact", "empty session: %s", out)
}
