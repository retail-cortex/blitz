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
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func newCommandApp(t *testing.T, input string, replies ...*genai.Content) (*App, *runtime.MockLLM) {
	t.Helper()
	isolateHome(t)
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Images.Dir = t.TempDir()
	cfg.Audit.Enabled = false
	cfg.Blitz.AutoApprove = true
	llm := runtime.NewMockLLM("gemini-3.8-flash", replies...)
	app := openApp(t, cfg, llm)
	app.Input = NewLineReader(strings.NewReader(input), io.Discard)
	return app, llm
}

func toolCallContent(name string, args map[string]any) *genai.Content {
	return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: name, Args: args}}}}
}

func TestPlanCommandRefusesEditsAndRecordsTheGoal(t *testing.T) {
	app, llm := newCommandApp(t, "/plan\n/plan add notes.txt\n/exit\n",
		toolCallContent("create_file", map[string]any{"path": "notes.txt", "content": "x"}),
		genai.NewContentFromText("1. Create notes.txt", genai.RoleModel))
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })

	assert.Contains(t, out, "Usage: /plan <goal>", "bare /plan should print usage:\n%s", out)
	require.Equal(t, 2, llm.Calls(), "want 2 model calls for the plan, got %d", llm.Calls())
	_, err := os.Stat(filepath.Join(local(app).Tools().Workspace().Dir(), "notes.txt"))
	require.Error(t, err, "/plan created a file")
	first := llm.Requests[0].Contents
	text := first[len(first)-1].Parts[0].Text
	require.Contains(t, text, "plan-only mode", "plan prompt = %q", text)
	require.Contains(t, text, "add notes.txt", "plan prompt = %q", text)
	got := userMessages(local(app).Storage())
	require.Len(t, got, 1, "transcript = %v", got)
	require.Equal(t, "/plan add notes.txt", got[0], "transcript = %v", got)
}

func TestPlanGoalIsNotRunAsACommand(t *testing.T) {
	app, llm := newCommandApp(t, "/plan /clear everything\n/exit\n", genai.NewContentFromText("plan", genai.RoleModel))
	captureStdout(t, func() { RunREPL(context.Background(), app) })
	require.Equal(t, 1, llm.Calls(), "goal starting with / was not sent to the agent (%d calls)", llm.Calls())
}

func TestShellPassthroughRunsInWorkspaceWithoutTheAgent(t *testing.T) {
	app, llm := newCommandApp(t, "!echo hi > made.txt\n!exit 3\n!\n/exit\n")
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })
	require.Equal(t, 0, llm.Calls(), "! reached the agent (%d calls)", llm.Calls())
	b, err := os.ReadFile(filepath.Join(local(app).Tools().Workspace().Dir(), "made.txt"))
	require.NoError(t, err, "command did not run in the workspace: %q", b)
	require.Equal(t, "hi", strings.TrimSpace(string(b)), "command did not run in the workspace: %q %v", b, err)
	for _, want := range []string{"$ echo hi > made.txt", "Done", "Exit code 3", "Usage: !<command>"} {
		assert.Contains(t, out, want, "output missing %q:\n%s", want, out)
	}
	require.Len(t, userMessages(local(app).Storage()), 0, "! commands were recorded as prompts")
}

// Ctrl+C during a ! command stops the command; the REPL's copy of the
// signal must not also trigger exit at the next prompt.
func TestCtrlCDuringShellPassthroughDoesNotExit(t *testing.T) {
	app, llm := newCommandApp(t, "", genai.NewContentFromText("hi", genai.RoleModel))
	// Like a person, the next line arrives after the command, so a stale
	// interrupt left in the channel would win the race at the prompt.
	app.Input = NewLineReader(&slowReader{chunks: []string{"!sleep 0.5\n", "hello\n/exit\n"}, delay: 300 * time.Millisecond}, io.Discard)
	sigs := make(chan os.Signal, 1)
	app.Interrupts = sigs
	go func() {
		time.Sleep(200 * time.Millisecond)
		sigs <- os.Interrupt
	}()
	captureStdout(t, func() { RunREPL(context.Background(), app) })
	require.Equal(t, 1, llm.Calls(), "the prompt after the ! command was not sent (%d model calls); a stale Ctrl+C ended the session", llm.Calls())
}

func TestToolsAndShowCommands(t *testing.T) {
	app, _ := newCommandApp(t, "")
	local(app).Config().MCP.Servers = []config.MCPServerConfig{{Name: "gh", Prefix: "gh"}, {Name: "qa-only", Agents: []string{"qa"}}}
	out := captureStdout(t, func() { HandleCommand(context.Background(), "/tools", app) })
	for _, want := range []string{"read_file", "run_shell_command", "mcp:gh", "gh__"} {
		assert.Contains(t, out, want, "/tools missing %q:\n%s", want, out)
	}
	assert.NotContains(t, out, "qa-only", "/tools listed an MCP server not offered to the active agent")
	// read_file is marked as available in /plan, run_shell_command is not.
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "read_file") && !strings.Contains(line, "●"):
			t.Errorf("read_file not marked for /plan: %q", line)
		case strings.Contains(line, "run_shell_command") && strings.Contains(line, "●"):
			t.Errorf("run_shell_command marked for /plan: %q", line)
		}
	}

	show := captureStdout(t, func() { HandleCommand(context.Background(), "/show", app) })
	set := captureStdout(t, func() { HandleCommand(context.Background(), "/set", app) })
	require.NotEqual(t, "", show, "/show should match /set:\n%s\nvs\n%s", show, set)
	require.Equal(t, set, show, "/show should match /set:\n%s\nvs\n%s", show, set)
}

// slowReader returns its chunks one per Read, pausing before each after the first.
type slowReader struct {
	chunks []string
	delay  time.Duration
	n      int
}

func (r *slowReader) Read(p []byte) (int, error) {
	if r.n >= len(r.chunks) {
		return 0, io.EOF
	}
	if r.n > 0 {
		time.Sleep(r.delay)
	}
	c := r.chunks[r.n]
	r.n++
	return copy(p, c), nil
}
