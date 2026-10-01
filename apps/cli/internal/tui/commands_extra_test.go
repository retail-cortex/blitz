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
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// commandCase is a slash command run against a stubbed workspace, with
// what its output must and mustn't say.
type commandCase struct {
	setup   func(s *stubBackend)
	line    string
	want    []string
	notWant []string
}

// runCommandCases runs each case on a fresh app as a subtest.
func runCommandCases(t *testing.T, cases map[string]commandCase) {
	t.Helper()
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			app, s := stubApp(t, "")
			if c.setup != nil {
				c.setup(s)
			}
			out := runCmd(t, app, c.line)
			for _, w := range c.want {
				assert.Contains(t, out, w)
			}
			for _, w := range c.notWant {
				assert.NotContains(t, out, w)
			}
		})
	}
}

var errBoom = errors.New("boom")

// The change commands report what they did and how they failed: /undo,
// /checkpoints, /diff, /cost, /context and /compact.
func TestChangeCommandOutputs(t *testing.T) {
	at := time.Date(2026, 1, 2, 15, 4, 5, 0, time.Local)
	runCommandCases(t, map[string]commandCase{
		"undo conflict": {
			setup: func(s *stubBackend) {
				s.undo = func(bool) (api.UndoResult, error) { return api.UndoResult{}, api.ErrUndoConflict }
			},
			line: "/undo", want: []string{api.ErrUndoConflict.Error()},
		},
		"undo restores": {
			setup: func(s *stubBackend) {
				s.undo = func(force bool) (api.UndoResult, error) {
					if !force {
						return api.UndoResult{}, errBoom
					}
					return api.UndoResult{Label: "fix it", Restored: []string{"a.go", "b.go"}}, nil
				}
			},
			line: "/undo --force", want: []string{`Undid "fix it": restored a.go, b.go`},
		},
		"undo after cd": {
			setup: func(s *stubBackend) {
				s.undo = func(bool) (api.UndoResult, error) { return api.UndoResult{}, errors.New("nothing to undo") }
				s.activeSession = func() (api.SessionInfo, bool) { return api.SessionInfo{ID: "s", MovedFrom: "/old/place"}, true }
			},
			line: "/undo", want: []string{"nothing to undo", "Changes made before /cd are in /old/place"},
		},
		"checkpoints none": {
			setup: func(s *stubBackend) { s.listCheckpoints = func() []api.Checkpoint { return nil } },
			line:  "/checkpoints", want: []string{"No file changes recorded"},
		},
		"checkpoints listed": {
			setup: func(s *stubBackend) {
				s.listCheckpoints = func() []api.Checkpoint {
					return []api.Checkpoint{{ID: 3, Label: "edit", Time: at, Files: []string{"x.go", "y.go"}}}
				}
			},
			line: "/checkpoints", want: []string{"#3", "15:04:05", "edit", "x.go, y.go"},
		},
		"git diff fails": {
			setup: func(s *stubBackend) { s.gitDiff = func() (string, error) { return "partial", errBoom } },
			line:  "/diff git", want: []string{"git diff failed: boom", "partial"},
		},
		"git diff clean": {
			setup: func(s *stubBackend) { s.gitDiff = func() (string, error) { return "", nil } },
			line:  "/diff git", want: []string{"No uncommitted changes."},
		},
		"git diff": {
			setup: func(s *stubBackend) { s.gitDiff = func() (string, error) { return "+added\n", nil } },
			line:  "/diff git", want: []string{"+added"}, notWant: []string{"No uncommitted"},
		},
		"session diff none": {
			setup: func(s *stubBackend) { s.sessionDiff = func() string { return " \n" } },
			line:  "/diff", want: []string{"No changes made by tools"},
		},
		"cost without a session": {
			setup: func(s *stubBackend) { s.sessionUsage = func() (api.Usage, error) { return api.Usage{}, errBoom } },
			line:  "/cost", want: []string{"No active session."},
		},
		"cost unpriced": {
			setup: func(s *stubBackend) {
				s.sessionUsage = func() (api.Usage, error) { return api.Usage{Calls: 2, Input: 1500, Output: 2_500_000}, nil }
			},
			line: "/usage", want: []string{"Model calls:   2", "1.5k", "2.5M", "Estimated cost: unknown"},
		},
		"context without a session": {
			setup: func(s *stubBackend) {
				s.contextInfo = func() (api.ContextInfo, error) { return api.ContextInfo{}, errBoom }
			},
			line: "/context", want: []string{"No active session."},
		},
		"context with compaction off": {
			setup: func(s *stubBackend) {
				s.contextInfo = func() (api.ContextInfo, error) {
					return api.ContextInfo{Tokens: 200, Parts: []api.ContextPart{{Name: "replies", Tokens: 200}}}, nil
				}
			},
			line: "/context", want: []string{"Automatic compaction is off", "Replies"},
		},
		"context with compaction": {
			setup: func(s *stubBackend) {
				s.contextInfo = func() (api.ContextInfo, error) {
					return api.ContextInfo{Tokens: 500, AutoCompact: true, Threshold: 1000, Keep: 4}, nil
				}
			},
			line: "/context", want: []string{"Compaction at 1.0k tokens (50% used); the 4 newest"},
		},
		"compact without a session": {
			setup: func(s *stubBackend) { s.activeSession = noSession },
			line:  "/compact", want: []string{"No active session."}, notWant: []string{"Summarizing"},
		},
		"compact nothing to do": {
			setup: func(s *stubBackend) {
				s.activeSession = func() (api.SessionInfo, bool) { return api.SessionInfo{ID: "s"}, true }
				s.compact = func(string) (api.CompactResult, error) { return api.CompactResult{}, api.ErrNothingToCompact }
			},
			line: "/compact", want: []string{api.ErrNothingToCompact.Error()},
		},
		"compact fails": {
			setup: func(s *stubBackend) {
				s.activeSession = func() (api.SessionInfo, bool) { return api.SessionInfo{ID: "s"}, true }
				s.compact = func(string) (api.CompactResult, error) { return api.CompactResult{}, errBoom }
			},
			line: "/compact", want: []string{"Compaction failed: boom"},
		},
		"compact with usage": {
			setup: func(s *stubBackend) {
				s.activeSession = func() (api.SessionInfo, bool) { return api.SessionInfo{ID: "s"}, true }
				s.compact = func(focus string) (api.CompactResult, error) {
					return api.CompactResult{EventsCompacted: 7, SummaryChars: 99, After: api.Usage{Calls: 1, Input: 10, Output: 5}}, nil
				}
			},
			line: "/compact the tests", want: []string{"Replaced 7 earlier events with a 99-character summary", "↳"},
		},
	})
}

// /memory's subcommands show, add, list and forget, and report failures.
func TestMemoryCommandOutputs(t *testing.T) {
	runCommandCases(t, map[string]commandCase{
		"reload fails": {
			setup: func(s *stubBackend) { s.reloadMemory = func() ([]string, error) { return nil, errBoom } },
			line:  "/memory reload", want: []string{"✗ boom"},
		},
		"none loaded": {
			setup: func(s *stubBackend) { s.reloadMemory = func() ([]string, error) { return nil, nil } },
			line:  "/memory", want: []string{"No instruction files found"},
		},
		"add fails": {
			setup: func(s *stubBackend) { s.addMemory = func(string) (string, error) { return "", errBoom } },
			line:  "/memory add a note", want: []string{"✗ boom"},
		},
		"notes fail": {
			setup: func(s *stubBackend) { s.listNotes = func() ([]api.Note, error) { return nil, errBoom } },
			line:  "/memory notes", want: []string{"✗ boom"},
		},
		"no notes": {
			setup: func(s *stubBackend) { s.listNotes = func() ([]api.Note, error) { return nil, nil } },
			line:  "/memory notes", want: []string{"No notes yet"},
		},
		"notes": {
			setup: func(s *stubBackend) {
				s.listNotes = func() ([]api.Note, error) {
					return []api.Note{{Name: "style", Kind: "feedback", Text: "short\nanswers"}}, nil
				}
			},
			line: "/memory notes", want: []string{"Notes the agent saved", "style", "(feedback)", "short answers"},
		},
		"forget usage": {line: "/memory forget", want: []string{"Usage: /memory"}},
		"forget fails": {
			setup: func(s *stubBackend) { s.forgetNote = func(string) error { return errBoom } },
			line:  "/memory forget style", want: []string{"✗ boom"},
		},
		"forget": {
			setup: func(s *stubBackend) { s.forgetNote = func(string) error { return nil } },
			line:  "/memory forget style", want: []string{"Forgot style"},
		},
		"unknown": {line: "/memory sideways", want: []string{"Usage: /memory"}},
	})
}

// /fork, /export, /style, /hooks, /approvals and /mcp show what there is and
// report failures.
func TestSessionAndListCommandOutputs(t *testing.T) {
	at := time.Date(2026, 3, 4, 10, 11, 12, 0, time.Local)
	runCommandCases(t, map[string]commandCase{
		"fork too many args": {line: "/fork 1 2", want: []string{"Usage: /fork"}},
		"fork bad turn":      {line: "/fork #0", want: []string{"Usage: /fork"}},
		"fork fails": {
			setup: func(s *stubBackend) {
				s.forkSession = func(int) (api.SessionInfo, error) { return api.SessionInfo{}, errBoom }
			},
			line: "/fork #2", want: []string{"✗ boom"},
		},
		"export fails": {
			setup: func(s *stubBackend) { s.exportSession = func(string) (string, error) { return "", errBoom } },
			line:  "/export", want: []string{"✗ boom"},
		},
		"export to a missing directory": {
			setup: func(s *stubBackend) { s.exportSession = func(string) (string, error) { return "# s", nil } },
			line:  "/export no/such/dir/x.md", want: []string{"✗ "},
		},
		"style fails": {
			setup: func(s *stubBackend) { s.set = func(string, string) (string, error) { return "", errBoom } },
			line:  "/style terse", want: []string{"✗ boom"},
		},
		"style set": {
			setup: func(s *stubBackend) { s.set = func(string, string) (string, error) { return "terse", nil } },
			line:  "/style terse", want: []string{"Output style terse"},
		},
		"styles listed": {
			setup: func(s *stubBackend) {
				s.listStyles = func() []api.StyleInfo {
					return []api.StyleInfo{{Name: "plain", Description: "as is", Active: true}, {Name: "terse", Description: "short"}}
				}
			},
			line: "/style", want: []string{"Output styles", "● " + Reset + "plain", "terse"},
		},
		"no hooks": {
			setup: func(s *stubBackend) { s.listHooks = func() []api.HookInfo { return nil } },
			line:  "/hooks", want: []string{"No hooks configured"},
		},
		"hooks": {
			setup: func(s *stubBackend) {
				s.listHooks = func() []api.HookInfo {
					return []api.HookInfo{
						{Event: "pre_tool", Runs: "lint.sh", Match: "edit", If: "go", FailClosed: true,
							Failures: []api.HookFailure{{Time: at, Error: "exit 1"}}},
						{Event: "pre_tool", Runs: "fmt.sh", Source: ".blitz/settings.toml"},
						{Event: "stop", Runs: "bell"},
					}
				}
			},
			line: "/hooks",
			want: []string{"pre_tool", "lint.sh", "match edit, if go, fail_closed, your settings", "10:11:12 exit 1",
				"fmt.sh", ".blitz/settings.toml", "stop", "bell"},
		},
		"approvals clear": {line: "/approvals clear", want: []string{"Revoked 0 rules."}},
		"no approvals":    {line: "/approvals", want: []string{"No remembered approvals."}},
		"mcp servers": {
			setup: func(s *stubBackend) {
				s.listMCPServers = func() []api.MCPServer {
					return []api.MCPServer{{Name: "docs", Target: "npx docs", AutoApprove: true}, {Name: "db", Target: "http://db"}}
				}
			},
			line: "/mcp", want: []string{"MCP servers:", "docs", "npx docs", "auto-approved", "http://db", "approval required"},
		},
	})
}

// describeApproval says what each kind of approval allows.
func TestDescribeApproval(t *testing.T) {
	cases := map[string]struct {
		a    api.Approval
		want string
	}{
		"command in":  {api.Approval{Kind: "cmd", Subject: "make", Dir: "/w"}, "command in /w: make"},
		"command":     {api.Approval{Kind: "cmd", Subject: "make"}, "command: make"},
		"write":       {api.Approval{Kind: "write", Subject: "src"}, "file edits in src"},
		"delete":      {api.Approval{Kind: "delete", Subject: "tmp"}, "file deletions in tmp"},
		"web":         {api.Approval{Kind: "web", Subject: "go.dev"}, "web requests to go.dev"},
		"mcp":         {api.Approval{Kind: "mcp", Subject: "docs__get"}, "MCP tool docs__get"},
		"forged tool": {api.Approval{Kind: "uc-run", Subject: "sum"}, "forged tool sum"},
		"other":       {api.Approval{Kind: "new", Key: "new:thing"}, "new:thing"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, c.want, describeApproval(c.a))
		})
	}
}

// /approvals lists approvals kept for the session and for always.
func TestApprovalsListShowsScopes(t *testing.T) {
	app := newFullApp(t)
	store := local(app).Tools().Hooks().Store()
	store.Add("web:go.dev", "")
	out := runCmd(t, app, "/approvals")
	assert.Contains(t, out, "web requests to go.dev")
	assert.Contains(t, out, "Revoke with /approvals revoke")
}

// /pin_model, /unpin and /tools report each outcome.
func TestPinAndToolsOutputs(t *testing.T) {
	runCommandCases(t, map[string]commandCase{
		"pins listed": {
			setup: func(s *stubBackend) {
				s.listAgents = func() []api.AgentInfo {
					return []api.AgentInfo{{Name: "blitz"}, {Name: "qa", PinnedModel: "openai/gpt-5"}, {Name: "docs", PinnedModel: "gemini/flash"}}
				}
			},
			line: "/pin", want: []string{"Pinned models", "qa", "openai/gpt-5", "docs"}, notWant: []string{"No agent is pinned"},
		},
		"pin usage": {line: "/pin_model qa", want: []string{"Usage: /pin_model"}},
		"pin fails": {
			setup: func(s *stubBackend) {
				s.pinModel = func(string, string) (api.PinResult, error) { return api.PinResult{}, errBoom }
			},
			line: "/pin_model qa x/y", want: []string{"Could not change the agent's model: boom"},
		},
		"pin not saved": {
			setup: func(s *stubBackend) {
				s.pinModel = func(a, m string) (api.PinResult, error) {
					return api.PinResult{Agent: a, Model: m, Saved: api.Saved{Err: errBoom}}, nil
				}
			},
			line: "/pin_model qa x/y", want: []string{"qa now runs on x/y.", "Changed for this session, but not saved: boom"},
		},
		"unpin usage": {line: "/unpin", want: []string{"Usage: /unpin <agent>"}},
		"unpin unknown agent": {
			setup: func(s *stubBackend) {
				s.unpin = func(a string) (api.PinResult, error) { return api.PinResult{}, &api.UnknownAgentError{Name: a} }
			},
			line: "/unpin nobody", want: []string{"Unknown agent: nobody"},
		},
		"tools with MCP": {
			setup: func(s *stubBackend) {
				s.activeAgentTools = func() api.AgentTools {
					return api.AgentTools{Agent: "blitz",
						Tools: []api.ToolInfo{{Name: "read_file", Description: "Reads a file. More.\nDetail", PlanAllowed: true}, {Name: "edit"}},
						MCP:   []api.MCPOffer{{Server: "docs"}, {Server: "db", Tools: []string{"query", "schema"}, Prefix: "db"}}}
				}
			},
			line: "/tools",
			want: []string{"Tools for blitz", "read_file", "Reads a file.", "mcp:docs", "all tools from this MCP server",
				"mcp:db", "query, schema (named db__…)"},
			notWant: []string{"More."},
		},
	})
}

// /resume, /session save, /rename and /locale report each outcome.
func TestSessionCommandOutputs(t *testing.T) {
	runCommandCases(t, map[string]commandCase{
		"resume usage": {line: "/resume", want: []string{"Usage: /resume"}},
		"resume fails": {
			setup: func(s *stubBackend) {
				s.loadSession = func(string) (api.SessionInfo, bool, error) { return api.SessionInfo{}, false, errBoom }
			},
			line: "/resume abc", want: []string{"✗ boom"},
		},
		"resume a snapshot": {
			setup: func(s *stubBackend) {
				s.loadSession = func(string) (api.SessionInfo, bool, error) {
					return api.SessionInfo{ID: "new1", MessageCount: 2, Messages: []api.Message{{Role: "user", Text: "hi"}, {Role: "model", Text: "hello   there"}}}, true, nil
				}
			},
			line: "/resume base", want: []string{"Started session new1 from snapshot base (2 messages)", "you:", "Blitz:", "hello there"},
		},
		"resume from elsewhere": {
			setup: func(s *stubBackend) {
				s.loadSession = func(string) (api.SessionInfo, bool, error) {
					return api.SessionInfo{ID: "old", Workspace: "/somewhere/else", MessageCount: 1}, false, nil
				}
			},
			line: "/session load old", want: []string{"Resumed session old ((untitled), 1 message)", "started in /somewhere/else"},
		},
		"save usage": {line: "/session save", want: []string{"Usage: /session save"}},
		"save without a session": {
			setup: func(s *stubBackend) {
				s.saveSnapshot = func(string, bool) (api.SessionInfo, error) { return api.SessionInfo{}, api.ErrNoActiveSession }
			},
			line: "/session save base", want: []string{"No active session."},
		},
		"save fails": {
			setup: func(s *stubBackend) {
				s.saveSnapshot = func(string, bool) (api.SessionInfo, error) { return api.SessionInfo{}, errBoom }
			},
			line: "/session save base --force", want: []string{"Could not save the snapshot: boom"},
		},
		"rename fails": {
			setup: func(s *stubBackend) {
				s.renameSession = func(string) (api.SessionInfo, error) { return api.SessionInfo{}, errBoom }
			},
			line: "/rename x", want: []string{"Could not rename the session: boom"},
		},
		"list fails": {
			setup: func(s *stubBackend) { s.listSessions = func(bool) ([]api.SessionInfo, error) { return nil, errBoom } },
			line:  "/session list", want: []string{"Error listing sessions: boom"},
		},
		"list all": {
			setup: func(s *stubBackend) {
				s.listSessions = func(bool) ([]api.SessionInfo, error) {
					return []api.SessionInfo{{ID: "a1", Snapshot: "base"}, {ID: "b2", Workspace: "/w"}}, nil
				}
			},
			line: "/session list --all", want: []string{"in all workspaces (2)", "snapshot base", "(unknown: saved before", "/w"},
			notWant: []string{"--all shows other"},
		},
		"new fails": {
			setup: func(s *stubBackend) {
				s.newSession = func() (api.SessionInfo, error) { return api.SessionInfo{}, errBoom }
			},
			line: "/session new", want: []string{"Failed to create session: boom"},
		},
		"locale with custom catalogs": {
			setup: func(s *stubBackend) {
				s.availableLocales = func() ([]api.LocaleInfo, string) { return []api.LocaleInfo{{Tag: "en-US", Name: "English"}}, "/cat" }
			},
			line: "/locale", want: []string{"Available: en-US (English)", "placing a catalog in /cat"},
		},
		"locale unknown": {
			setup: func(s *stubBackend) {
				s.setLocale = func(string) (api.LocaleChange, error) { return api.LocaleChange{}, errBoom }
			},
			line: "/lang klingon", want: []string{`Unknown language "klingon"`},
		},
		"locale without a catalog, not saved": {
			setup: func(s *stubBackend) {
				s.setLocale = func(string) (api.LocaleChange, error) {
					return api.LocaleChange{Tag: "de", NativeName: "Deutsch", LanguageName: "Deutsch", Saved: api.Saved{Err: errBoom}}, nil
				}
			},
			line: "/language de", want: []string{"(de)", "No translation catalog for Deutsch", "saving it failed: boom"},
		},
	})
}

// A snapshot saved under a taken name says how to replace it.
func TestSessionSaveTakenName(t *testing.T) {
	app, s := stubApp(t, "")
	s.saveSnapshot = func(string, bool) (api.SessionInfo, error) { return api.SessionInfo{}, api.ErrSnapshotNameTaken }
	out := runCmd(t, app, "/session save base")
	assert.Contains(t, out, "A snapshot named base already exists")
}

// /export without a name writes a file named for the session.
func TestExportNamedForTheSession(t *testing.T) {
	app, s := stubApp(t, "")
	s.exportSession = func(string) (string, error) { return "# Fix the build\n", nil }
	s.activeSession = func() (api.SessionInfo, bool) { return api.SessionInfo{ID: "abc", Title: "Fix the build"}, true }
	out := runCmd(t, app, "/export")
	path := filepath.Join(app.Workspace.Dir(), "fix-the-build.md")
	assert.Contains(t, out, "Wrote the session to "+path)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "# Fix the build\n", string(b))
}

// PrintRecap shows only the last messages.
func TestPrintRecapKeepsTheLast(t *testing.T) {
	msgs := []api.Message{{Role: "user", Text: "one"}, {Role: "model", Text: "two"}, {Role: "user", Text: "three"}}
	out := captureStdout(t, func() { PrintRecap(msgs, 2) })
	assert.NotContains(t, out, "one")
	assert.Contains(t, out, "two")
	assert.Contains(t, out, "three")
}

// Changing the locale to one with a catalog switches the interface.
func TestLocaleSwitchesTheInterface(t *testing.T) {
	t.Cleanup(func() { i18n.SetCurrent(nil) })
	app, s := stubApp(t, "")
	s.setLocale = func(string) (api.LocaleChange, error) {
		return api.LocaleChange{Tag: "es", NativeName: "Español", HasCatalog: true, Saved: api.Saved{Path: "/cfg/.env.toml"}}, nil
	}
	out := runCmd(t, app, "/locale es")
	assert.Contains(t, out, "/cfg/.env.toml")
	assert.Equal(t, "es", i18n.Current().Tag().String())
}
