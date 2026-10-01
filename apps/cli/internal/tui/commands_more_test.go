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
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The plain commands print what they show: /clear, /sandbox, /skills
// search, /trust, and a bare "/".
func TestMiscCommandOutputs(t *testing.T) {
	runCommandCases(t, map[string]commandCase{
		"bare slash": {line: "/", notWant: []string{"Unknown command"}},
		"clear":      {line: "/clear", want: []string{"\033[H\033[2J"}},
		"sandbox": {
			setup: func(s *stubBackend) {
				s.sandboxSummary = func() []string { return []string{"shell: bwrap", "net: off"} }
			},
			line: "/sandbox", want: []string{"Sandbox Policy", "shell: bwrap", "net: off"},
		},
		"skills search": {
			setup: func(s *stubBackend) {
				s.searchSkills = func(q string) []api.SkillInfo {
					return []api.SkillInfo{{Name: "pdf", Description: "reads " + q}}
				}
			},
			line: "/skills search pdf files", want: []string{"Search Results for 'pdf files' (1)", "pdf", "reads pdf files"},
		},
		"skills search without a query": {
			setup: func(s *stubBackend) { s.searchSkills = func(string) []api.SkillInfo { return nil } },
			line:  "/skills search", want: []string{"Search Results for '' (0)"},
		},
		"tasks none": {
			setup: func(s *stubBackend) { s.listTasks = func() []api.TaskInfo { return nil } },
			line:  "/tasks", want: []string{"No background tasks."},
		},
		"task stop fails": {
			setup: func(s *stubBackend) {
				s.stopTask = func(string) (api.TaskInfo, error) { return api.TaskInfo{}, api.ErrUnknownTask }
			},
			line: "/tasks stop task-9", want: []string{api.ErrUnknownTask.Error()},
		},
		"task with a worktree": {
			setup: func(s *stubBackend) {
				s.task = func(id string) (api.TaskInfo, []string, error) {
					return api.TaskInfo{ID: id, Agent: "qa", State: api.TaskDone, Result: "all green", Worktree: "/wt", Branch: "blitz/t1",
						Started: time.Now()}, nil, nil
				}
			},
			line: "/tasks show task-1", want: []string{"[task-1] qa", "In the worktree /wt, on the branch blitz/t1", "all green"},
		},
		"failed task": {
			setup: func(s *stubBackend) {
				s.task = func(id string) (api.TaskInfo, []string, error) {
					return api.TaskInfo{ID: id, Agent: "qa", State: api.TaskFailed, Error: "model down", Started: time.Now()}, nil, nil
				}
			},
			line: "/tasks show task-2", want: []string{Red + "model down"},
		},
		"cd unavailable": {line: "/cd /tmp", want: []string{"/cd isn't available here."}},
	})
}

// /trust shows the project's settings and asks about those that need
// trust; a failed decision is reported.
func TestTrustCommand(t *testing.T) {
	cases := map[string]struct {
		p    api.ProjectSettings
		err  error
		want []string
	}{
		"declined, changed meanwhile": {
			p: api.ProjectSettings{Files: []string{".blitz/settings.toml"}, Hash: "h", State: api.TrustNew,
				Pending: []api.ProjectItem{{Kind: "hook", Key: "stop", Value: "make"}}, Problems: []string{"bad line 3"}},
			err:  api.ErrProjectChanged,
			want: []string{"hook stop runs make", "bad line 3", "Not reviewed yet", "changed since they were shown"},
		},
		"for this run": {
			p:   api.ProjectSettings{Files: []string{"s.toml"}, Hash: "h", State: api.TrustNew, Loaded: true, Pending: []api.ProjectItem{{Kind: "allow", Value: "x"}}},
			err: errBoom, want: []string{"Trusted for this run only.", "boom"},
		},
		"trusted next open": {
			p:   api.ProjectSettings{Files: []string{"s.toml"}, Hash: "h", State: api.TrustTrusted, Pending: []api.ProjectItem{{Kind: "allow", Value: "x"}}},
			err: errBoom, want: []string{"Trusted: in force when the workspace opens again."},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			app, s := stubApp(t, "d\n")
			s.projectSettings = func() api.ProjectSettings { return c.p }
			s.trustProject = func(string, bool) error { return c.err }
			out := runCmd(t, app, "/trust")
			for _, w := range c.want {
				assert.Contains(t, out, w)
			}
		})
	}
}

// /status shows the session, the model's provider and error, MCP servers
// and the project's settings files.
func TestStatusShowsEverything(t *testing.T) {
	app, s := stubApp(t, "")
	s.activeSession = func() (api.SessionInfo, bool) { return api.SessionInfo{ID: "s-1", Title: "Fix it"}, true }
	s.settings = func() api.Settings {
		return api.Settings{Agent: "blitz", Model: api.ModelInfo{Name: "flash", Provider: "gemini"}, PermissionMode: "default"}
	}
	s.modelErr = func() error { return errBoom }
	s.listMCPServers = func() []api.MCPServer { return []api.MCPServer{{Name: "docs"}, {Name: "db"}} }
	s.projectSettings = func() api.ProjectSettings { return api.ProjectSettings{Files: []string{"/w/.blitz/settings.toml"}} }
	out := runCmd(t, app, "/status")
	for _, w := range []string{"s-1 · Fix it", "gemini/flash — boom", "docs, db", "/w/.blitz/settings.toml"} {
		assert.Contains(t, out, w)
	}
}

// /config sends each key to its own setter and reports failures,
// including a save that fails.
func TestConfigSettersAndFailures(t *testing.T) {
	notDir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notDir, nil, 0o600))
	var calls []string
	record := func(what string) { calls = append(calls, what) }
	cases := map[string]struct {
		line string
		call string
		want string
	}{
		"model":       {line: "/config model=x/y", call: "model x/y", want: "model updated to: x/y"},
		"agent":       {line: "/config agent=qa", call: "agent qa", want: "agent updated to: qa"},
		"mode":        {line: "/config permission_mode=plan", call: "mode plan", want: "mode updated to: plan"},
		"locale":      {line: "/config locale=es", call: "locale es", want: "locale updated to: es"},
		"other fails": {line: "/config style=odd", call: "set style odd", want: "Failed to apply setting: boom"},
		"save fails":  {line: "/config model=x/y --save", call: "model x/y", want: "✗ "},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			calls = nil
			app, s := stubApp(t, "")
			app.ConfigDir = notDir
			s.setModel = func(ref string) (string, error) { record("model " + ref); return "", nil }
			s.setAgent = func(n string) (api.AgentInfo, error) { record("agent " + n); return api.AgentInfo{}, nil }
			s.setPermission = func(m string) (string, error) { record("mode " + m); return m, nil }
			s.setLocale = func(in string) (api.LocaleChange, error) { record("locale " + in); return api.LocaleChange{}, nil }
			s.set = func(k, v string) (string, error) { record("set " + k + " " + v); return "", errBoom }
			out := runCmd(t, app, c.line)
			assert.Equal(t, []string{c.call}, calls)
			assert.Contains(t, out, c.want)
		})
	}
}

// /copy needs a session, and reports a clipboard that fails.
func TestCopyFailures(t *testing.T) {
	app, s := stubApp(t, "")
	s.activeSession = noSession
	assert.Contains(t, runCmd(t, app, "/copy"), "No active session.")

	s.activeSession = func() (api.SessionInfo, bool) {
		return api.SessionInfo{ID: "s", Messages: []api.Message{{Role: "model", Text: "answer"}}}, true
	}
	old, oldOut := clipboardCommands, osc52Out
	t.Cleanup(func() { clipboardCommands, osc52Out = old, oldOut })
	clipboardCommands = func() [][]string { return nil }
	closed, err := os.Create(filepath.Join(t.TempDir(), "term"))
	require.NoError(t, err)
	closed.Close()
	osc52Out = func() *os.File { return closed }
	assert.Contains(t, runCmd(t, app, "/copy"), "✗ ")
}

// The clipboard programs on Linux start with wl-copy under Wayland, and
// OSC 52 goes to the terminal by default.
func TestClipboardCommandsOnLinux(t *testing.T) {
	if goruntime.GOOS == "darwin" || goruntime.GOOS == "windows" {
		t.Skip("Linux and other Unix systems only")
	}
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	cmds := clipboardCommands()
	require.NotEmpty(t, cmds)
	assert.Equal(t, []string{"wl-copy"}, cmds[0])
	assert.Equal(t, os.Stdout, osc52Out())
}

// page uses less when $PAGER is unset, and prints when there's neither.
func TestPageWithoutAPager(t *testing.T) {
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "less"), []byte("#!/bin/sh\necho via-less\n/bin/cat\n"), 0o755))
	t.Setenv("PAGER", "")
	t.Setenv("PATH", bin)
	assert.Contains(t, captureStdout(t, func() { page("text") }), "via-less")
	t.Setenv("PATH", t.TempDir())
	assert.Contains(t, captureStdout(t, func() { page("plain") }), "plain")
}

// notifyCommand uses notify-send on Linux when it's installed.
func TestNotifyCommandOnLinux(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	assert.Nil(t, notifyCommand("Blitz", "done"), "no notify-send")
	require.NoError(t, os.WriteFile(filepath.Join(bin, "notify-send"), []byte("#!/bin/sh\n"), 0o755))
	cmd := notifyCommand("Blitz", "done")
	require.NotNil(t, cmd)
	assert.Equal(t, []string{"notify-send", "Blitz", "done"}, cmd.Args)
}

// splitDiff skips what comes before the first file.
func TestSplitDiffSkipsAPreamble(t *testing.T) {
	files := splitDiff("preamble\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n")
	require.Len(t, files, 1)
	assert.Equal(t, "x", files[0].path)
}

// On a terminal, /diff offers the changed files and pages the one chosen
// until Esc.
func TestBrowseDiffOnATerminal(t *testing.T) {
	app, s := stubApp(t, "")
	f, keys := pickTerminal(t)
	app.Input = f.in
	s.sessionDiff = func() string {
		return "--- a/one.go\n+++ b/one.go\n@@ -1 +1 @@\n-old\n+new\n--- a/two.go\n+++ b/two.go\n@@ -1 +1 @@\n-a\n+b\n"
	}
	t.Setenv("PAGER", "cat")
	keys.in <- []byte("\x1b[B\r")
	keys.in <- []byte("\x1b")
	out := runCmd(t, app, "/diff")
	assert.Contains(t, out, "+b")
	assert.NotContains(t, out, "+new", "only the chosen file is paged")
	assert.Contains(t, f.output(), "Changed files (2)")
}

// cdTarget is a workspace /cd moves to, whose session calls fail as
// told.
type cdTarget struct {
	api.Backend
	dir              string
	moveErr, openErr error
	closed           bool
}

func (c *cdTarget) Dir() string { return c.dir }
func (c *cdTarget) MoveSession(string) (api.SessionInfo, error) {
	return api.SessionInfo{}, c.moveErr
}
func (c *cdTarget) OpenSession(string, bool) (api.SessionInfo, bool, error) {
	return api.SessionInfo{}, false, c.openErr
}
func (c *cdTarget) Close() error { c.closed = true; return nil }

// closeFails is a workspace whose Close fails, in a directory that's gone.
type closeFails struct{ *stubBackend }

func (closeFails) Dir() string  { return "/no/such/old/workspace" }
func (closeFails) Close() error { return errBoom }

// /cd stays put when the new workspace can't take the session, or the
// exit question is cancelled, and reports a failure to close the old one.
func TestCdFailures(t *testing.T) {
	target := t.TempDir()
	withMessages := func() (api.SessionInfo, bool) { return api.SessionInfo{ID: "s", MessageCount: 2}, true }
	cases := map[string]struct {
		active func() (api.SessionInfo, bool)
		next   *cdTarget
		want   string
	}{
		"move fails": {active: withMessages, next: &cdTarget{dir: target, moveErr: errBoom}, want: "Stayed in"},
		"open fails": {active: noSession, next: &cdTarget{dir: target, openErr: errBoom}, want: "Stayed in"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			app, s := stubApp(t, "")
			s.activeSession = c.active
			app.Cd = func(context.Context, string) (api.Backend, *i18n.Bundle, func(), error) {
				return c.next, nil, func() { t.Error("committed a failed move") }, nil
			}
			out := runCmd(t, app, "/cd "+target)
			assert.Contains(t, out, c.want)
			assert.True(t, c.next.closed, "the new workspace was left open")
			assert.NotEqual(t, target, app.Workspace.Dir())
		})
	}

	t.Run("old workspace fails to close", func(t *testing.T) {
		app, s := stubApp(t, "")
		s.activeSession = noSession
		app.Workspace = closeFails{s}
		locales := i18n.Default()
		committed := false
		app.Cd = func(context.Context, string) (api.Backend, *i18n.Bundle, func(), error) {
			return &cdTarget{dir: target}, locales, func() { committed = true }, nil
		}
		out := runCmd(t, app, "/cd "+target)
		assert.True(t, committed)
		assert.Same(t, locales, app.Locales)
		assert.Contains(t, out, "boom")
		assert.Contains(t, out, "in a new session")
	})

	t.Run("exit question cancelled", func(t *testing.T) {
		app, s := stubApp(t, "c\n")
		s.listTasks = func() []api.TaskInfo {
			return []api.TaskInfo{{ID: "task-1", State: api.TaskRunning, Started: time.Now()}}
		}
		app.Cd = func(context.Context, string) (api.Backend, *i18n.Bundle, func(), error) {
			t.Error("moved after cancelling")
			return nil, nil, nil, errBoom
		}
		out := runCmd(t, app, "/cd "+target)
		assert.Contains(t, out, "Exit cancelled")
	})
}

// Background tasks' requests: Input-less apps skip them, a multiple
// choice question is asked as one, and a failed answer stops asking.
func TestAnswerTasksEdges(t *testing.T) {
	app, s := stubApp(t, "1,2\n")
	asked := 0
	s.pendingRequests = func() []api.TaskRequest {
		asked++
		return []api.TaskRequest{
			{ID: "r1", TaskID: "task-1", Agent: "qa", Question: "Which?", Options: []string{"a", "b"}, MultiSelect: true},
			{ID: "r2", TaskID: "task-1", Agent: "qa", Question: "Never asked", Options: []string{"x"}},
		}
	}
	var answers []string
	s.answerRequest = func(id string, _ api.Decision, answer string) error {
		answers = append(answers, id+"="+answer)
		return errBoom
	}
	out := captureStdout(t, func() { answerTasks(context.Background(), app) })
	assert.Equal(t, []string{"r1=a\nb"}, answers)
	assert.Contains(t, out, "boom")

	app.Input = nil
	answerTasks(context.Background(), app)
	assert.Equal(t, 1, asked, "asked without an input")
}

// LicenseText knows the three texts and refuses others; /license reports
// an unknown one.
func TestLicenseTexts(t *testing.T) {
	for _, which := range []string{"", "full", "third-party", "third_party", "thirdparty"} {
		t.Run(fmt.Sprintf("%q", which), func(t *testing.T) {
			text, err := LicenseText(which, "/license")
			require.NoError(t, err)
			assert.NotEmpty(t, text)
		})
	}
	app, _ := stubApp(t, "")
	assert.Contains(t, runCmd(t, app, "/license nope"), `unknown license text "nope"`)
}

// Ensure the config package's directory helper is what /status names.
func TestStatusNamesTheConfigFile(t *testing.T) {
	app, _ := stubApp(t, "")
	app.ConfigDir = t.TempDir()
	assert.Contains(t, runCmd(t, app, "/status"), filepath.Join(config.ConfigDir(app.ConfigDir), ".env.toml"))
}
