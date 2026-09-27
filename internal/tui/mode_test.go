package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestModeCommand(t *testing.T) {
	app, _ := newCommandApp(t, "/mode\n/mode acceptEdits\n/mode yolo\n/set\n/exit\n")
	app.Workspace.SetPermissionMode("default") // newCommandApp may start in bypass
	var prompts bytes.Buffer
	app.Input = NewLineReader(strings.NewReader("/mode\n/mode acceptEdits\n/mode yolo\n/set\n/exit\n"), &prompts)
	out := captureStdout(t, func() { RunREPL(context.Background(), app) }) + prompts.String()
	for _, want := range []string{
		"Permission mode: default", "accept-edits", "make file changes in the workspace without asking",
		"Permission mode: accept-edits", "Usage: /mode default|accept-edits|plan|dont-ask|bypass",
		"[accept-edits]", // the prompt shows a mode other than default
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got := app.Workspace.Settings().PermissionMode; got != "accept-edits" {
		t.Errorf("mode %q", got)
	}
}
