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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCmdStatus(t *testing.T) {
	app := newTestApp(t, nil)
	app.Version = "9.9.9"
	out := captureStdout(t, func() { cmdStatus(app) })
	for _, want := range []string{"Status", "9.9.9", "mock-a", app.Workspace.Dir(), "(local)", ".env.toml"} {
		assert.Contains(t, out, want)
	}
	app.Attached = true
	assert.Contains(t, captureStdout(t, func() { cmdStatus(app) }), "in the Blitz service")
}

func TestCmdConfig(t *testing.T) {
	app := newTestApp(t, nil)
	app.ConfigDir = t.TempDir()
	tests := []struct {
		name, line string
		want       []string
		saved      string // in .env.toml afterwards
	}{
		{name: "show", line: "/config", want: []string{"Settings", "Keys: agency, agent"}},
		{name: "set alias", line: "/set agency=low", want: []string{"agency updated to: low"}},
		{name: "unknown", line: "/config colour=red", want: []string{"colour"}},
		{name: "invalid", line: "/config agency=wild", want: []string{"low, medium, high, extreme"}},
		{name: "usage", line: "/config agency", want: []string{"Usage: /set key=value"}},
		{name: "save", line: "/config agency=high --save", want: []string{"agency updated to: high", "Saved blitz.agency_level"}, saved: `agency_level = "high"`},
		{name: "session only", line: "/config effort=high --save", want: []string{"isn't kept with --save"}},
		{name: "style saved", line: "/config style=concise --save", want: []string{"Saved ui.style"}, saved: `style = "concise"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				handled, err := HandleCommand(context.Background(), tt.line, app)
				require.NoError(t, err)
				require.True(t, handled)
			})
			for _, w := range tt.want {
				assert.Contains(t, out, w)
			}
			if tt.saved != "" {
				b, err := os.ReadFile(filepath.Join(app.ConfigDir, ".env.toml"))
				require.NoError(t, err)
				assert.Contains(t, string(b), tt.saved)
			}
		})
	}
	assert.Equal(t, "concise", app.Workspace.Settings().Style)
}

// fakeClipboard makes the clipboard a file, returning its path.
func fakeClipboard(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "clip")
	old := clipboardCommands
	clipboardCommands = func() [][]string { return [][]string{{"no-such-clipboard"}, {"sh", "-c", "cat > " + file}} }
	t.Cleanup(func() { clipboardCommands = old })
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	return file
}

func TestCopyToClipboard(t *testing.T) {
	t.Run("program", func(t *testing.T) {
		file := fakeClipboard(t)
		how, err := copyToClipboard("hello")
		require.NoError(t, err)
		assert.Equal(t, "sh", how)
		b, _ := os.ReadFile(file)
		assert.Equal(t, "hello", string(b))
	})
	t.Run("OSC 52 over SSH", func(t *testing.T) {
		file := fakeClipboard(t)
		t.Setenv("SSH_TTY", "/dev/ttys001")
		term, err := os.Create(filepath.Join(t.TempDir(), "term"))
		require.NoError(t, err)
		defer term.Close()
		old := osc52Out
		osc52Out = func() *os.File { return term }
		defer func() { osc52Out = old }()
		how, err := copyToClipboard("hello")
		require.NoError(t, err)
		assert.Equal(t, "OSC 52", how)
		b, _ := os.ReadFile(term.Name())
		assert.Equal(t, "\033]52;c;aGVsbG8=\a", string(b))
		_, err = os.Stat(file)
		assert.True(t, os.IsNotExist(err), "the program ran over SSH")
	})
}

func TestREPLCopy(t *testing.T) {
	app := newFullApp(t)
	file := fakeClipboard(t)
	app.Input = NewLineReader(strings.NewReader("/copy\nmake a file\n/copy\n/copy 2\n/copy x\n/exit\n"), io.Discard)
	out := captureStdout(t, func() { assert.NoError(t, RunREPL(context.Background(), app)) })
	b, _ := os.ReadFile(file)
	assert.Equal(t, "created", string(b))
	for _, want := range []string{"Copied (via sh)", "No answer to copy", "Usage: /copy [n]"} {
		assert.Contains(t, out, want)
	}
}

func TestSplitDiff(t *testing.T) {
	d := "--- a/one.go\n+++ b/one.go\n@@ -1 +1 @@\n-old\n+new\n+more\n--- /dev/null\n+++ b/two.go\n@@ -0,0 +1 @@\n+x\n--- a/gone.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-y\n"
	files := splitDiff(d)
	require.Len(t, files, 3)
	tests := []struct {
		path       string
		added, del int
	}{{"one.go", 2, 1}, {"two.go", 1, 0}, {"gone.go", 0, 1}}
	for i, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.path, files[i].path)
			assert.Equal(t, tt.added, files[i].added)
			assert.Equal(t, tt.del, files[i].del)
			assert.True(t, strings.HasPrefix(files[i].text, "--- "))
		})
	}
	assert.Equal(t, d, files[0].text+files[1].text+files[2].text)
	git := "diff --git a/x b/x\nindex 1..2\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\ndiff --git a/y b/y\n--- a/y\n+++ b/y\n@@ -1 +1 @@\n-c\n+d\n"
	assert.Len(t, splitDiff(git), 2)
}

func TestBrowseDiff(t *testing.T) {
	app := newTestApp(t, nil)
	d := "--- a/one.go\n+++ b/one.go\n@@ -1 +1 @@\n-old\n+new\n--- a/two.go\n+++ b/two.go\n@@ -1 +1 @@\n-a\n+b\n"
	out := captureStdout(t, func() { browseDiff(context.Background(), d, app) })
	assert.Contains(t, out, "one.go")
	assert.Contains(t, out, "two.go")
	t.Setenv("PAGER", "cat")
	assert.Contains(t, captureStdout(t, func() { page("paged text") }), "paged text")
	t.Setenv("PAGER", "false")
	assert.Contains(t, captureStdout(t, func() { page("fallback") }), "fallback")
}

func TestPrintContextParts(t *testing.T) {
	out := captureStdout(t, func() {
		printContextParts(api.ContextInfo{Tokens: 1000, Parts: []api.ContextPart{{Name: "system_prompt", Tokens: 250}, {Name: "tool_results", Tokens: 750}}})
	})
	assert.Contains(t, out, "System prompt")
	assert.Contains(t, out, "25.0%")
	assert.Contains(t, out, "75.0%")
	assert.Empty(t, captureStdout(t, func() { printContextParts(api.ContextInfo{}) }))
}

func TestNotifier(t *testing.T) {
	var shown []string
	old := notifyCommand
	notifyCommand = func(title, body string) *exec.Cmd {
		shown = append(shown, body)
		return exec.Command("true")
	}
	defer func() { notifyCommand = old }()
	tests := []struct {
		mode        string
		bell, shown bool
	}{{"both", true, true}, {"bell", true, false}, {"desktop", false, true}, {"off", false, false}}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			shown = nil
			out := captureStdout(t, func() { Notifier{Mode: tt.mode}.Send("done") })
			assert.Equal(t, tt.bell, out == "\a")
			assert.Equal(t, tt.shown, len(shown) == 1)
		})
	}

	t.Run("only in a long turn", func(t *testing.T) {
		n := Notifier{After: time.Minute, Mode: "desktop"}
		approve := n.Approving(func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionOnce, nil })
		ask := n.Asking(func(context.Context, string, []string) (string, error) { return "yes", nil })
		shown = nil
		turnStart.Store(time.Now().UnixNano())
		_, _ = approve(context.Background(), api.ApprovalRequest{Tool: "run_shell_command"})
		assert.Empty(t, shown, "a short turn notified")
		turnStart.Store(time.Now().Add(-2 * time.Minute).UnixNano())
		defer turnStart.Store(0)
		d, err := approve(context.Background(), api.ApprovalRequest{Tool: "run_shell_command"})
		require.NoError(t, err)
		assert.Equal(t, api.DecisionOnce, d)
		a, _ := ask(context.Background(), "q?", nil)
		assert.Equal(t, "yes", a)
		assert.Equal(t, []string{"Blitz is waiting for your approval (run_shell_command)", "Blitz has a question for you"}, shown)
		assert.False(t, Notifier{Mode: "both"}.longRunning(), "After 0 never notifies")
	})
}
