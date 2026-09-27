package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"

	"google.golang.org/genai"
)

// rewindApp is a command app whose session has two prompts.
func rewindApp(t *testing.T) (*App, string) {
	t.Helper()
	app, _ := newCommandApp(t, "", genai.NewContentFromText("one", genai.RoleModel), genai.NewContentFromText("two", genai.RoleModel))
	s, err := app.Workspace.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"first prompt", "second prompt"} {
		if _, err := app.Workspace.Run(context.Background(), s.ID, api.Turn{Text: p}, func(api.Event) {}); err != nil {
			t.Fatal(err)
		}
	}
	return app, s.ID
}

func TestRewindCommandOnAPipe(t *testing.T) {
	app, _ := rewindApp(t)
	out := ansiPattern.ReplaceAllString(captureStdout(t, func() { HandleCommand(context.Background(), "/rewind", app) }), "")
	if !strings.Contains(out, "1  second prompt") || !strings.Contains(out, "2  first prompt") || !strings.Contains(out, "Usage: /rewind") {
		t.Fatalf("list:\n%s", out)
	}
	out = captureStdout(t, func() { HandleCommand(context.Background(), "/rewind 1 conversation", app) })
	if !strings.Contains(out, "Conversation rewound") || !strings.Contains(out, "The prompt was: second prompt") {
		t.Fatalf("rewind:\n%s", out)
	}
	if a, _ := app.Workspace.ActiveSession(); a.MessageCount != 2 {
		t.Errorf("messages %d", a.MessageCount)
	}
	for _, bad := range []string{"/rewind 5", "/rewind 1 sideways"} {
		if out := captureStdout(t, func() { HandleCommand(context.Background(), bad, app) }); !strings.Contains(out, "Usage: /rewind") {
			t.Errorf("%s:\n%s", bad, out)
		}
	}
}

func TestRewindPickersPutThePromptBack(t *testing.T) {
	app, _ := rewindApp(t)
	f, keys := pickTerminal(t)
	app.Input = f.in
	keys.in <- []byte("\r") // the latest prompt
	keys.in <- []byte("\r") // the first mode offered: conversation (no files changed)
	out := captureStdout(t, func() { HandleCommand(context.Background(), "/rewind", app) })
	if !strings.Contains(out, "Conversation rewound") {
		t.Fatalf("output:\n%s\n%s", out, f.output())
	}
	screen := f.output()
	if !strings.Contains(screen, "second prompt") || !strings.Contains(screen, "Conversation only") || strings.Contains(screen, "Code only") {
		t.Errorf("pickers (code modes must be left out when no files changed):\n%s", screen)
	}
	// The next prompt starts with the rewound text.
	f.keys(t, " again\r")
	line, err := within(t, func() (string, error) { return f.in.ReadInput(context.Background(), "> ") })
	if err != nil || line != "second prompt again" {
		t.Fatalf("next input %q %v", line, err)
	}
}

func TestEscEscAtAnEmptyPromptRewinds(t *testing.T) {
	f := newFakeTerminal(t)
	f.keys(t, "\x1b", "\x1b")
	if _, err := within(t, func() (string, error) { return f.in.ReadInput(context.Background(), "> ") }); !errors.Is(err, ErrRewindKey) {
		t.Fatalf("Esc Esc: %v", err)
	}
	// Not in a question.
	f.keys(t, "\x1b")
	if _, err := within(t, func() (string, error) { return f.in.Ask(context.Background(), "? ") }); errors.Is(err, ErrRewindKey) {
		t.Fatal("Esc at a question opened /rewind")
	}
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
	if !strings.Contains(got, "☒ read") || !strings.Contains(got, "☐ fix") || !strings.Contains(got, "☐ test") || strings.Contains(got, "todo") {
		t.Errorf("output:\n%s", got)
	}
	// A failed todo call is shown like any tool's.
	out.b.Reset()
	p.Handle(api.Event{ToolResult: &api.ToolResult{Name: "todo", Result: map[string]any{"error": "item 1 has no content"}}})
	p.End()
	if !strings.Contains(out.b.String(), "no content") {
		t.Errorf("failed todo hidden: %q", out.b.String())
	}
}
