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

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stdio gives the run in as its stdin and collects what it writes to stdout
// and stderr, which runs use directly (not cobra's streams).
func stdio(t *testing.T, in string) func() string {
	t.Helper()
	dir := t.TempDir()
	inPath := filepath.Join(dir, "stdin")
	require.NoError(t, os.WriteFile(inPath, []byte(in), 0o600))
	inF, err := os.Open(inPath)
	require.NoError(t, err)
	outF, err := os.Create(filepath.Join(dir, "stdout"))
	require.NoError(t, err)
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = inF, outF, outF
	t.Cleanup(func() {
		os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr
		inF.Close()
		outF.Close()
	})
	return func() string {
		data, _ := os.ReadFile(outF.Name())
		return string(data)
	}
}

// runEnv is an isolated home with the fake model and no service, and a
// workspace.
func runEnv(t *testing.T) string {
	t.Helper()
	fakeModel(t, isolate(t))
	t.Setenv("BLITZ_SOCKET", filepath.Join(t.TempDir(), "none.sock"))
	return t.TempDir()
}

// A one-shot run with every run flag that changes the workspace: the
// answer is printed, and an unpriced model with a cost limit is warned
// about.
func TestRunOneShotWithRunFlags(t *testing.T) {
	ws := runEnv(t)
	extra := t.TempDir()
	instructions := filepath.Join(t.TempDir(), "extra.md")
	require.NoError(t, os.WriteFile(instructions, []byte("Answer briefly."), 0o600))
	out := stdio(t, "")
	_, err := runCLI(t, "-d", ws, "--permission-mode", "accept-edits", "--effort", "low",
		"--allow", "shell(go test *)", "--deny", "web(*.internal)", "--name", "first run",
		"--append-system-prompt", "Be kind.", "--append-system-prompt-file", instructions,
		"--add-dir", extra, "--max-cost-usd", "1", "say", "hi")
	require.NoError(t, err, out())
	assert.Contains(t, out(), "done")
	assert.Contains(t, out(), "fake", "the unpriced model is named in a warning")
}

// The run flags' own errors are usage errors, found once the workspace is
// open.
func TestRunFlagErrors(t *testing.T) {
	ws := runEnv(t)
	// No OS sandbox, wherever the tests run: bypass needs one, and CI has
	// it. The variable, as it overrides the settings.
	t.Setenv("BLITZ_SANDBOX_SHELL", "off")
	for name, args := range map[string][]string{
		"bad allow rule":        {"--allow", "nonsense(x", "hi"},
		"missing prompt file":   {"--append-system-prompt-file", filepath.Join(ws, "missing.md"), "hi"},
		"unknown session":       {"--resume", "no-such-session", "hi"},
		"bypass without a box":  {"--permission-mode", "bypass", "hi"},
		"worktree outside repo": {"--worktree", "x", "hi"},
		"missing image":         {"--image", filepath.Join(ws, "none.png"), "hi"},
		"prompt twice":          {"-p", "a", "b"},
	} {
		t.Run(name, func(t *testing.T) {
			stdio(t, "")
			_, err := runCLI(t, append([]string{"-d", ws}, args...)...)
			assert.Equal(t, exitUsage, exitCodeFor(err), "%v", err)
		})
	}
}

// --continue and --fork pick up the last session; --no-session-persistence
// leaves none behind.
func TestRunSessionsAcrossRuns(t *testing.T) {
	ws := runEnv(t)
	stdio(t, "")
	_, err := runCLI(t, "-d", ws, "--name", "base", "first")
	require.NoError(t, err)
	e, err := engine.Open(context.Background(), mustConfig(t, ws), engine.Options{})
	require.NoError(t, err)
	before, err := e.ListSessions(true)
	e.Close()
	require.NoError(t, err)
	require.Len(t, before, 1)

	_, err = runCLI(t, "-d", ws, "--continue", "--fork", "--name", "copy", "second")
	require.NoError(t, err)
	_, err = runCLI(t, "-d", ws, "--no-session-persistence", "third")
	require.NoError(t, err)

	e, err = engine.Open(context.Background(), mustConfig(t, ws), engine.Options{})
	require.NoError(t, err)
	defer e.Close()
	after, err := e.ListSessions(true)
	require.NoError(t, err)
	assert.Len(t, after, 2, "the fork is a new session; the unpersisted run left none")
}

func mustConfig(t *testing.T, ws string) *config.Config {
	t.Helper()
	cfg, err := loadConfig(&globalFlags{dir: ws})
	require.NoError(t, err)
	return cfg
}

// --input-format stream-json runs each user line on stdin as a turn.
func TestRunStreamJSONInputFromStdin(t *testing.T) {
	ws := runEnv(t)
	out := stdio(t, `{"type":"user","text":"one"}`+"\n"+`{"type":"user","text":"two"}`+"\n")
	_, err := runCLI(t, "-d", ws, "--input-format", "stream-json", "--output-format", "stream-json")
	require.NoError(t, err, out())
	results := 0
	for _, line := range strings.Split(strings.TrimSpace(out()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["type"] == "result" {
			results++
		}
	}
	assert.Equal(t, 2, results, "a result per user line:\n%s", out())
}

// -i runs the REPL on stdin: a prompt, /cd to another workspace, and the
// end of input ends it.
func TestRunREPLOnStdin(t *testing.T) {
	ws := runEnv(t)
	other := t.TempDir()
	out := stdio(t, "hello\n/cd "+other+"\n")
	_, err := runCLI(t, "-d", ws, "-i")
	require.NoError(t, err, out())
	assert.Contains(t, out(), "done")
	assert.Contains(t, out(), other)

	// The session moved with /cd: --continue there resumes it and shows
	// where it was.
	out = stdio(t, "")
	_, err = runCLI(t, "-d", other, "-i", "--continue")
	require.NoError(t, err, out())
	assert.Contains(t, out(), "hello")
}

// Without a model the REPL still starts, with a warning; a one-shot run
// fails at once.
func TestRunREPLWithoutAModel(t *testing.T) {
	isolate(t)
	t.Setenv("BLITZ_SOCKET", filepath.Join(t.TempDir(), "none.sock"))
	out := stdio(t, "")
	_, err := runCLI(t, "-d", t.TempDir(), "-i")
	require.NoError(t, err, out())
	assert.Contains(t, out(), "blitz doctor")
}

// Project settings waiting for trust are warned about; trusted ones are
// only noted.
func TestRunProjectNotice(t *testing.T) {
	fakeModel(t, isolate(t))
	t.Setenv("BLITZ_SOCKET", filepath.Join(t.TempDir(), "none.sock"))
	ws := projectDir(t)
	out := stdio(t, "")
	_, err := runCLI(t, "-d", ws, "hi")
	require.NoError(t, err, out())
	assert.Contains(t, out(), "waiting for your trust")

	cfg := mustConfig(t, ws)
	p, err := engine.ReviewProject(cfg)
	require.NoError(t, err)
	require.NoError(t, engine.TrustProject(cfg, p.Hash, true))
	out = stdio(t, "")
	_, err = runCLI(t, "-d", ws, "-i")
	require.NoError(t, err, out())
	assert.Contains(t, out(), "applied (/trust shows them)")
}

// --worktree=name runs in a new worktree of the repository. (The name
// needs the "=": --worktree takes no value otherwise.)
func TestRunInAWorktree(t *testing.T) {
	fakeModel(t, isolate(t))
	t.Setenv("BLITZ_SOCKET", filepath.Join(t.TempDir(), "none.sock"))
	repo := gitRepo(t)
	out := stdio(t, "")
	_, err := runCLI(t, "-d", repo, "--worktree=try", "hi")
	require.NoError(t, err, out())
	assert.Contains(t, out(), "In the worktree "+filepath.Join(repo, ".blitz", "worktrees", "try"))
}

// -v prints the version.
func TestVersionFlag(t *testing.T) {
	isolate(t)
	out := stdio(t, "")
	_, err := runCLI(t, "-v")
	require.NoError(t, err)
	assert.Contains(t, out(), "version "+version)
}

// The REPL's completer offers the commands and, for those taking a name,
// the workspace's agents, pinned agents, models, locales and sessions.
func TestNewCompleter(t *testing.T) {
	e := testEnv(t)
	t.Setenv("GEMINI_API_KEY", "AIza-test") // builds the pinned model; never called
	_, err := e.PinModel(context.Background(), "qa", "gemini/gemini-3.8-pro")
	require.NoError(t, err)
	sess, err := e.Storage().CreateSession("", "first", "blitz")
	require.NoError(t, err)
	require.NoError(t, e.Storage().AddMessage("user", "hello"))
	_, err = e.Storage().Snapshot(sess.ID, "before", false)
	require.NoError(t, err)
	c := newCompleter(e)
	complete := func(line string) []string {
		got, _ := c.Do([]rune(line), len([]rune(line)))
		var out []string
		for _, r := range got {
			out = append(out, line[strings.LastIndex(line, " ")+1:]+string(r))
		}
		return out
	}
	assert.Contains(t, complete("/he"), "/help ")
	assert.Contains(t, complete("/agent "), "qa")
	assert.Contains(t, complete("/pin_model "), "blitz")
	assert.Equal(t, []string{"qa"}, complete("/unpin "))
	assert.Contains(t, complete("/model_settings "), "gemini-3.8-pro")
	assert.Contains(t, complete("/locale "), "fr-CA")
	assert.Contains(t, complete("/resume "), sess.ID)
	assert.Contains(t, complete("/resume "), "before", "snapshots by name")
}

// Notices and the status line are for a terminal only.
func TestTerminalOnlySettings(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UI.StatusLine = "{model}"
	cfg.UI.NotifyAfter = 30
	assert.Equal(t, "off", notifier(cfg, false).Mode)
	assert.Equal(t, 30*time.Second, notifier(cfg, true).After)
	assert.Equal(t, "", statusLine(cfg, false))
	assert.Equal(t, "{model}", statusLine(cfg, true))
}

// blitz init runs /init; -p with JSON output reads no stdin; a session
// resumed from another workspace is warned about; /cd to a folder that
// isn't there fails without ending the REPL.
func TestRunMoreEntryPoints(t *testing.T) {
	ws := runEnv(t)
	out := stdio(t, "")
	_, err := runCLI(t, "-d", ws, "init")
	require.NoError(t, err, out())

	out = stdio(t, "")
	_, err = runCLI(t, "-d", ws, "-p", "hi", "--output-format", "json")
	require.NoError(t, err, out())
	assert.Contains(t, out(), `"type":"result"`)

	e, err := engine.Open(context.Background(), mustConfig(t, ws), engine.Options{})
	require.NoError(t, err)
	list, err := e.ListSessions(true)
	e.Close()
	require.NoError(t, err)
	require.NotEmpty(t, list)
	other := t.TempDir()
	out = stdio(t, "/cd "+filepath.Join(other, "missing")+"\n")
	_, err = runCLI(t, "-d", other, "-i", "--resume", list[0].ID)
	require.NoError(t, err, out())
	assert.Contains(t, out(), ws, "the session's own workspace is named")
	assert.Contains(t, out(), "missing")
}
