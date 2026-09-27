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

func TestEffortCommand(t *testing.T) {
	in := "/effort\n/effort high\n/effort extreme\n/set\n/effort auto\n/exit\n"
	app, _ := newCommandApp(t, in)
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })
	for _, want := range []string{
		"Reasoning effort: auto (each model's own)", "Reasoning effort: high",
		"Usage: /effort minimal|low|medium|high|max|auto", "Effort:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got := app.Workspace.Settings().Effort; got != "" {
		t.Errorf("effort after auto: %q", got)
	}
}

func TestCycleModeSkipsAnUnavailableBypass(t *testing.T) {
	app, _ := newCommandApp(t, "")
	app.Workspace.SetPermissionMode("default")
	var seen []string
	for range 4 {
		cycleMode(app, true)
		seen = append(seen, app.Workspace.Settings().PermissionMode)
	}
	want := []string{"accept-edits", "plan"}
	if _, err := app.Workspace.SetPermissionMode("bypass"); err == nil {
		want = append(want, "bypass", "default") // this machine has the OS sandbox
	} else {
		want = append(want, "default", "accept-edits")
	}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("cycle %v, want %v", seen, want)
	}
}
