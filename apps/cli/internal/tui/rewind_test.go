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
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// rewindApp is a command app whose session has two prompts.
func rewindApp(t *testing.T) (*App, string) {
	t.Helper()
	app, _ := newCommandApp(t, "", genai.NewContentFromText("one", genai.RoleModel), genai.NewContentFromText("two", genai.RoleModel))
	s, err := app.Workspace.NewSession()
	require.NoError(t, err)
	for _, p := range []string{"first prompt", "second prompt"} {
		_, err := app.Workspace.Run(context.Background(), s.ID, api.Turn{Text: p}, func(api.Event) {})
		require.NoError(t, err)
	}
	return app, s.ID
}

func TestRewindCommandOnAPipe(t *testing.T) {
	app, _ := rewindApp(t)
	out := ansiPattern.ReplaceAllString(captureStdout(t, func() { HandleCommand(context.Background(), "/rewind", app) }), "")
	require.Contains(t, out, "1  second prompt", "list:\n%s", out)
	require.Contains(t, out, "2  first prompt", "list:\n%s", out)
	require.Contains(t, out, "Usage: /rewind", "list:\n%s", out)
	out = captureStdout(t, func() { HandleCommand(context.Background(), "/rewind 1 conversation", app) })
	require.Contains(t, out, "Conversation rewound", "rewind:\n%s", out)
	require.Contains(t, out, "The prompt was: second prompt", "rewind:\n%s", out)
	a, _ := app.Workspace.ActiveSession()
	assert.Equal(t, 2, a.MessageCount, "messages %d", a.MessageCount)
	for _, bad := range []string{"/rewind 5", "/rewind 1 sideways"} {
		out := captureStdout(t, func() { HandleCommand(context.Background(), bad, app) })
		assert.Contains(t, out, "Usage: /rewind", "%s:\n%s", bad, out)
	}
}

func TestRewindPickersPutThePromptBack(t *testing.T) {
	app, _ := rewindApp(t)
	f, keys := pickTerminal(t)
	app.Input = f.in
	keys.in <- []byte("\r") // the latest prompt
	keys.in <- []byte("\r") // the first mode offered: conversation (no files changed)
	out := captureStdout(t, func() { HandleCommand(context.Background(), "/rewind", app) })
	require.Contains(t, out, "Conversation rewound", "output:\n%s\n%s", out, f.output())
	screen := f.output()
	assert.Contains(t, screen, "second prompt", "pickers (code modes must be left out when no files changed):\n%s", screen)
	assert.Contains(t, screen, "Conversation only", "pickers (code modes must be left out when no files changed):\n%s", screen)
	assert.NotContains(t, screen, "Code only", "pickers (code modes must be left out when no files changed):\n%s", screen)
	// The next prompt starts with the rewound text.
	f.keys(t, " again\r")
	line, err := within(t, func() (string, error) { return f.in.ReadInput(context.Background(), "> ") })
	require.NoError(t, err, "next input %q", line)
	require.Equal(t, "second prompt again", line, "next input %q %v", line, err)
}

func TestEscEscAtAnEmptyPromptRewinds(t *testing.T) {
	f := newFakeTerminal(t)
	f.keys(t, "\x1b", "\x1b")
	_, err := within(t, func() (string, error) { return f.in.ReadInput(context.Background(), "> ") })
	require.ErrorIs(t, err, ErrRewindKey, "Esc Esc: %v", err)
	// Not in a question.
	f.keys(t, "\x1b")
	_, err = within(t, func() (string, error) { return f.in.Ask(context.Background(), "? ") })
	require.NotErrorIs(t, err, ErrRewindKey, "Esc at a question opened /rewind")
}

// The agent's task list is drawn as a checklist, instead of the todo
// tool's call and result lines.
func TestPrinterShowsTasks(t *testing.T) {
	var out syncBuffer
	p := NewPrinter(PrinterOptions{Out: &out})
	p.Handle(api.Event{ToolCall: &api.ToolCall{Name: "todo", Args: map[string]any{}}})
	p.Handle(api.Event{ToolResult: &api.ToolResult{Name: "todo", Result: map[string]any{"items": []any{}}}})
	p.Handle(api.Event{Tasks: []api.Task{{Content: "read", Status: "done"}, {Content: "fix", Status: "in_progress"}, {Content: "test", Status: "pending"}}})
	p.End()
	got := ansiPattern.ReplaceAllString(out.b.String(), "")
	assert.Contains(t, got, "☒ read", "output:\n%s", got)
	assert.Contains(t, got, "☐ fix", "output:\n%s", got)
	assert.Contains(t, got, "☐ test", "output:\n%s", got)
	assert.NotContains(t, got, "todo", "output:\n%s", got)
	// A failed todo call is shown like any tool's.
	out.b.Reset()
	p.Handle(api.Event{ToolResult: &api.ToolResult{Name: "todo", Result: map[string]any{"error": "item 1 has no content"}}})
	p.End()
	assert.Contains(t, out.b.String(), "no content", "failed todo hidden: %q", out.b.String())
}
