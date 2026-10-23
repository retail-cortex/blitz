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

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/harness"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// auditSince is the workspace's audit log in the last hour.
func auditSince(w *Workspace) []audit.Entry {
	return audit.Read(config.ExpandHome(w.cfg.Audit.Dir), w.Dir(), time.Now().Add(-time.Hour))
}

func TestHarnessEvidence(t *testing.T) {
	w, _ := openTestWith(t, nil,
		toolCall("read_file", map[string]any{"path": "missing.txt"}), text("It isn't there."),
		text("Sorry: here it is."))
	s := newSession(t, w)
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "read missing.txt"}, ignore)
	require.NoError(t, err)
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "No, that's wrong: look again"}, ignore)
	require.NoError(t, err)

	ev := w.HarnessEvidence(time.Now().Add(-time.Hour))
	byKind := map[harness.Kind]harness.Item{}
	for _, it := range ev.Items {
		byKind[it.Kind] = it
	}
	require.Contains(t, byKind, harness.ToolFailed, "items %+v", ev.Items)
	assert.Equal(t, "read_file", byKind[harness.ToolFailed].Subject)
	assert.Equal(t, "missing.txt", byKind[harness.ToolFailed].Target)
	assert.Equal(t, 1, byKind[harness.ToolFailed].Seen[0].Turn)
	require.Contains(t, byKind, harness.Corrected)
	assert.Equal(t, "No, that's wrong: look again", byKind[harness.Corrected].Seen[0].Excerpt)
	assert.Equal(t, 2, byKind[harness.Corrected].Seen[0].Turn)

	assert.Empty(t, w.HarnessEvidence(time.Now().Add(time.Minute)).Items, "nothing since then")
}

func TestHarnessEvidenceWithoutTheAuditLog(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Audit.Enabled = false })
	ev := w.HarnessEvidence(time.Now().Add(-time.Hour))
	assert.True(t, ev.NoAudit)
	assert.Empty(t, ev.Items)
}

func TestAuditTurnEnd(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		decision string
		detail   string
	}{
		{"at the step limit", api.ErrMaxTurns, audit.DecisionLimit, "steps"},
		{"at the cost limit", fmt.Errorf("turn: %w", api.ErrCostLimit), audit.DecisionLimit, "cost"},
		{"at the time limit", api.ErrTimeLimit, audit.DecisionLimit, "time"},
		{"stopped", context.Canceled, audit.DecisionStopped, ""},
		{"finished", nil, "", ""},
		{"failed otherwise", errors.New("the provider is down"), "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := openTestWith(t, nil)
			w.auditTurnEnd("s1", tt.err)
			var ends []audit.Entry
			for _, e := range auditSince(w) {
				if e.Kind == audit.KindTurnEnd {
					ends = append(ends, e)
				}
			}
			if tt.decision == "" {
				assert.Empty(t, ends, "only a turn that didn't finish is recorded")
				return
			}
			require.Len(t, ends, 1)
			assert.Equal(t, "s1", ends[0].Session)
			assert.Equal(t, tt.decision, ends[0].Decision)
			assert.Equal(t, tt.detail, ends[0].Detail)
		})
	}
}

func TestSteerIsAuditedAsOne(t *testing.T) {
	w, _ := openTestWith(t, nil)
	s := newSession(t, w)
	_ = w.Steer(context.Background(), s.ID, "use pnpm") // no turn runs: it's recorded all the same
	var prompts []audit.Entry
	for _, e := range auditSince(w) {
		if e.Kind == audit.KindPrompt {
			prompts = append(prompts, e)
		}
	}
	require.Len(t, prompts, 1)
	assert.Equal(t, audit.DecisionSteer, prompts[0].Decision)
	assert.Equal(t, "use pnpm", prompts[0].Detail)
}

// /reflect runs Harness Reflect, which reads the evidence with its tool;
// the next reflection starts where this one began, and what it wrote to
// .agents/EXPERIENCE.md (imported by .agents/AGENTS.md) applies from the
// next prompt.
func TestReflectCommand(t *testing.T) {
	w, llm := openTestWith(t, nil,
		toolCall("read_file", map[string]any{"path": "missing.txt"}), text("It isn't there."),
		toolCall("harness_evidence", map[string]any{"since": "7"}), text("One pattern: nothing to change yet."),
		text("hello"))
	write(t, w.Dir(), "AGENTS.md", "@.agents/AGENTS.md\n")
	write(t, w.Dir(), ".agents/AGENTS.md", "Use tabs.\n\n@EXPERIENCE.md\n")
	write(t, w.Dir(), ".agents/EXPERIENCE.md", "## How to work\n\n## This project\n\n- Deploy with make deploy; there is no make push.\n")
	s := newSession(t, w)
	ctx := context.Background()
	_, err := w.Run(ctx, s.ID, api.Turn{Text: "read missing.txt"}, ignore)
	require.NoError(t, err)
	assert.True(t, w.harnessState().LastReflect.IsZero(), "no reflection yet")

	before := time.Now()
	_, err = w.Run(ctx, s.ID, api.Turn{Text: "/reflect 7", Command: true}, ignore)
	require.NoError(t, err)
	reflect := llm.Requests[2]
	assert.Contains(t, systemText(reflect.Config), "You are Harness Reflect", "the command's agent, though Blitz answered before")
	assert.Contains(t, userTextAt(reflect.Contents), "Reflect on this workspace's runs")
	assert.Contains(t, userTextAt(reflect.Contents), "Evidence since: 7")
	evidence := functionResponseText(llm.Requests[3].Contents, "harness_evidence")
	assert.Contains(t, evidence, "tool_failed read_file `missing.txt`", "the evidence reaches the reflector")
	assert.WithinDuration(t, before, w.harnessState().LastReflect, time.Minute, "the next reflection starts here")
	assert.Equal(t, w.harnessState().LastReflect, w.reflectSince())

	_, err = w.Run(ctx, s.ID, api.Turn{Text: "hi"}, ignore)
	require.NoError(t, err)
	assert.Contains(t, systemText(llm.Requests[len(llm.Requests)-1].Config), "Deploy with make deploy", "the experience is in the instructions")
}

func TestReflectSince(t *testing.T) {
	w := openTest(t)
	assert.WithinDuration(t, time.Now().Add(-reflectWindow), w.reflectSince(), time.Hour, "before a first reflection: the window, from the hour")
	last := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	w.saveHarnessState(harnessState{LastReflect: last})
	assert.True(t, last.Equal(w.reflectSince()), "then: the last reflection")
	text, since := w.readEvidence(context.Background(), time.Time{}, "skills")
	assert.True(t, last.Equal(since))
	assert.Contains(t, text, "Evidence since")
}

// functionResponseText is the latest response to the named tool in
// contents, as JSON.
func functionResponseText(contents []*genai.Content, name string) string {
	for i := len(contents) - 1; i >= 0; i-- {
		for _, p := range contents[i].Parts {
			if r := p.FunctionResponse; r != nil && r.Name == name {
				b, _ := json.Marshal(r.Response)
				return string(b)
			}
		}
	}
	return ""
}

// Every entry of a turn names its session and the workspace, though
// another session was opened since (the service runs several at once).
func TestAuditNamesTheTurnsSession(t *testing.T) {
	w, _ := openTestWith(t, nil, toolCall("read_file", map[string]any{"path": "missing.txt"}), text("gone"))
	first := newSession(t, w)
	newSession(t, w) // the logger's own session is this one now
	_, err := w.Run(context.Background(), first.ID, api.Turn{Text: "read it"}, ignore)
	require.NoError(t, err)
	kinds := map[string]bool{}
	for _, e := range auditSince(w) {
		if e.Kind == audit.KindSession {
			continue
		}
		kinds[e.Kind] = true
		assert.Equal(t, first.ID, e.Session, "%s entry", e.Kind)
		assert.Equal(t, w.Dir(), e.Workspace, "%s entry", e.Kind)
	}
	for _, k := range []string{audit.KindPrompt, audit.KindToolCall, audit.KindToolResult} {
		assert.True(t, kinds[k], "a %s entry", k)
	}
}

// The health facts count only what this workspace's audit log entries
// name it in: old entries that name no workspace could be any one's.
func TestHealthReadsOnlyThisWorkspacesEntries(t *testing.T) {
	w, _ := openTestWith(t, nil)
	log, err := audit.Open(config.ExpandHome(w.cfg.Audit.Dir), nil)
	require.NoError(t, err)
	skill := func(name, workspace string) audit.Entry {
		return audit.Entry{Kind: audit.KindToolCall, Tool: "activate_skill", Args: map[string]any{"skill_name": name}, Workspace: workspace}
	}
	log.Log(skill("ours", w.Dir()))
	log.Log(skill("anyones", ""))
	log.Log(skill("theirs", "/elsewhere"))
	require.NoError(t, log.Close())

	var names []string
	for _, e := range w.healthInputs(context.Background()).Audit {
		if e.Tool == "activate_skill" {
			names = append(names, e.Args["skill_name"].(string))
		}
	}
	assert.Equal(t, []string{"ours"}, names)
}

// chatted is a session with one turn in it (the mock's next reply), so
// it's on disk: the evidence reads the sessions it knows.
func chatted(t *testing.T, w *Workspace) api.SessionInfo {
	t.Helper()
	s := newSession(t, w)
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "hi"}, ignore)
	require.NoError(t, err)
	return s
}

// failCommands records each command failing twice in session, in w's
// audit log: one pattern per command.
func failCommands(t *testing.T, w *Workspace, session string, commands ...string) {
	t.Helper()
	log, err := audit.Open(config.ExpandHome(w.cfg.Audit.Dir), nil)
	require.NoError(t, err)
	log.SetWorkspace(w.Dir())
	ctx := audit.WithSession(context.Background(), session)
	for _, c := range commands {
		for range 2 {
			log.LogContext(ctx, audit.Entry{Kind: audit.KindToolResult, Tool: "run_shell_command", Decision: "exit", Error: "exit code 127", Args: map[string]any{"command": c}})
		}
	}
	require.NoError(t, log.Close())
}

// declaredTools are the tools a model request offered.
func declaredTools(req *model.LLMRequest) []string {
	var out []string
	for _, tl := range req.Config.Tools {
		for _, d := range tl.FunctionDeclarations {
			out = append(out, d.Name)
		}
	}
	return out
}

// The welcome suggests /reflect from five patterns since the last
// reflection, and the review once a scheduled reflection proposed changes;
// only for a workspace with .agents/AGENTS.md.
func TestReflectionTile(t *testing.T) {
	w, _ := openTestWith(t, nil, text("hello"))
	s := chatted(t, w)
	failCommands(t, w, s.ID, "make a", "make b", "make c", "make d")
	r, err := w.Reflection()
	require.NoError(t, err)
	assert.Zero(t, r, "no .agents/AGENTS.md, no experience to add to")

	write(t, w.Dir(), ".agents/AGENTS.md", "Use tabs.\n")
	r, _ = w.Reflection()
	assert.Equal(t, api.Reflection{Patterns: 4}, r)
	assert.NotContains(t, kinds(w.Suggestions(context.Background())), api.SuggestReflect, "four aren't enough")

	failCommands(t, w, s.ID, "make e")
	r, _ = w.Reflection()
	assert.Equal(t, 5, r.Patterns, "counted again once the log changed")
	tiles := w.Suggestions(context.Background()).Tiles
	require.NotEmpty(t, tiles)
	assert.Equal(t, api.Suggestion{Kind: api.SuggestReflect, Count: 5}, tiles[0])

	require.NoError(t, w.saveProposals(context.Background(), []string{"add EXPERIENCE: a", "add EXPERIENCE: b"}))
	assert.Equal(t, api.Suggestion{Kind: api.SuggestReflectReview, Count: 2}, w.Suggestions(context.Background()).Tiles[0])

	w.reflected(time.Now())
	r, _ = w.Reflection()
	assert.Zero(t, r, "a reflection reviewed them, and the evidence starts after it")
}

func TestReflectionWithoutTheAuditLog(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Audit.Enabled = false })
	write(t, w.Dir(), ".agents/AGENTS.md", "Use tabs.\n")
	r, err := w.Reflection()
	require.NoError(t, err)
	assert.Zero(t, r)
}

func TestReflectDue(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		schedule string
		state    harnessState
		want     bool
	}{
		{name: "off by default"},
		{name: "off", schedule: "off", state: harnessState{LastScheduled: now.Add(-48 * time.Hour)}},
		{name: "not a schedule", schedule: "now and then", state: harnessState{LastScheduled: now.Add(-48 * time.Hour)}},
		{name: "first seen: counts from now", schedule: "every hour"},
		{name: "an hour after the last", schedule: "every hour", state: harnessState{LastScheduled: now.Add(-2 * time.Hour)}, want: true},
		{name: "not yet", schedule: "every day at 02:00", state: harnessState{LastScheduled: now.Add(-time.Minute)}},
		{name: "a /reflect since counts", schedule: "every hour", state: harnessState{LastScheduled: now.Add(-2 * time.Hour), LastReflect: now.Add(-time.Minute)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := openTestWith(t, func(c *config.Config) { c.Harness.ReflectSchedule = tt.schedule })
			w.saveHarnessState(tt.state)
			assert.Equal(t, tt.want, w.ReflectDue(now))
			if tt.name == "first seen: counts from now" {
				assert.True(t, now.Equal(w.harnessState().LastScheduled), "it starts counting")
				assert.False(t, w.ReflectDue(now.Add(time.Second)))
			}
		})
	}
}

// A scheduled reflection reads the evidence and keeps its proposals, in a
// read-only session of its own that's gone after; /reflect then offers
// them, and they're gone once it's done.
func TestScheduledReflect(t *testing.T) {
	w, llm := openTestWith(t, func(c *config.Config) { c.Harness.MaxCostUSD = 0.25 }, text("hello"),
		toolCall("harness_evidence", map[string]any{}),
		toolCall("create_file", map[string]any{"path": ".agents/EXPERIENCE.md", "content": "## How to work\n"}),
		toolCall("harness_propose", map[string]any{"changes": []any{"add .agents/EXPERIENCE.md: run make deploy — 2 failed runs of make push", "update .agents/AGENTS.md: the build is make all"}}),
		text("Proposed 2."),
		toolCall("harness_evidence", map[string]any{}), text("Which of these?"))
	write(t, w.Dir(), ".agents/AGENTS.md", "Use tabs.\n")
	s := chatted(t, w)
	failCommands(t, w, s.ID, "make push")
	llm.Requests = nil
	sessions := func() int {
		list, err := w.storage.ListWorkspace(w.Dir())
		require.NoError(t, err)
		return len(list)
	}
	before := sessions()

	require.NoError(t, w.ScheduledReflect(context.Background()))
	require.Len(t, llm.Requests, 4)
	assert.Contains(t, systemText(llm.Requests[0].Config), "You are Harness Reflect")
	assert.Contains(t, userTextAt(llm.Requests[0].Contents), "running unattended")
	assert.Contains(t, declaredTools(llm.Requests[0]), "harness_propose")
	assert.Contains(t, functionResponseText(llm.Requests[2].Contents, "create_file"), "a scheduled reflection", "a read-only turn: nothing in the workspace changes")
	st := w.harnessState()
	assert.Len(t, st.Proposals, 2)
	assert.False(t, st.LastScheduled.IsZero())
	assert.Empty(t, st.Skipped)
	assert.True(t, st.LastReflect.IsZero(), "nothing was reviewed: the evidence still starts where it did")
	assert.Equal(t, before, sessions(), "its session is gone")
	assert.NoFileExists(t, filepath.Join(w.Dir(), ".agents", "EXPERIENCE.md"))

	require.NoError(t, w.ScheduledReflect(context.Background()))
	assert.Len(t, llm.Requests, 4, "nothing new since: no model call")
	assert.Equal(t, "nothing new to learn since the last scheduled reflection", w.harnessState().Skipped)

	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/reflect", Command: true}, ignore)
	require.NoError(t, err)
	evidence := functionResponseText(llm.Requests[len(llm.Requests)-1].Contents, "harness_evidence")
	assert.Contains(t, evidence, "A scheduled reflection on")
	assert.Contains(t, evidence, "run make deploy")
	st = w.harnessState()
	assert.Empty(t, st.Proposals, "reviewed")
	assert.False(t, st.LastReflect.IsZero())
}

func TestScheduledReflectSkips(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Config)
		agents bool
		want   string
	}{
		{name: "without the audit log", mutate: func(c *config.Config) { c.Audit.Enabled = false }, agents: true, want: "the audit log is off"},
		{name: "without .agents/AGENTS.md", want: "there's no .agents/AGENTS.md"},
		{name: "without patterns", agents: true, want: "nothing new to learn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, llm := openTestWith(t, tt.mutate)
			if tt.agents {
				write(t, w.Dir(), ".agents/AGENTS.md", "Use tabs.\n")
			}
			require.NoError(t, w.ScheduledReflect(context.Background()))
			assert.Empty(t, llm.Requests)
			st := w.harnessState()
			assert.Contains(t, st.Skipped, tt.want)
			assert.False(t, st.LastScheduled.IsZero(), "not again until the next time")
		})
	}
}

// In a conversation, harness_propose refuses: the user is asked instead.
func TestHarnessProposeInAConversation(t *testing.T) {
	w, llm := openTestWith(t, nil, toolCall("harness_propose", map[string]any{"changes": []any{"add x"}}), text("ok"))
	write(t, w.Dir(), ".agents/AGENTS.md", "Use tabs.\n")
	s := newSession(t, w)
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/reflect", Command: true}, ignore)
	require.NoError(t, err)
	assert.Contains(t, functionResponseText(llm.Requests[1].Contents, "harness_propose"), "ask the user")
	assert.Empty(t, w.harnessState().Proposals)
}

func TestReflectSinceStopsAtTheLongestWindow(t *testing.T) {
	w := openTest(t)
	w.saveHarnessState(harnessState{LastReflect: time.Now().AddDate(-1, 0, 0)})
	assert.WithinDuration(t, time.Now().Add(-maxReflectWindow), w.reflectSince(), time.Hour)
}

// The update tile says the project's skills or agents went wrong in the
// runs since they last changed (HE-12), and which; editing them since
// clears it.
func TestHarnessFailing(t *testing.T) {
	w, _ := openTestWith(t, nil, text("hello"))
	write(t, w.Dir(), ".agents/AGENTS.md", "Use tabs.\n")
	write(t, w.Dir(), ".agents/skills/deploy/SKILL.md", "---\nname: deploy\ndescription: Deploys\n---\nRun make push.\n")
	write(t, w.Dir(), ".agents/agents/qa.md", "---\nname: qa\ndescription: Tests\n---\nTest.\n")
	old := time.Now().Add(-time.Hour)
	for _, f := range []string{".agents/skills/deploy/SKILL.md", ".agents/agents/qa.md"} {
		require.NoError(t, os.Chtimes(filepath.Join(w.Dir(), f), old, old))
	}
	s := chatted(t, w)
	assert.Empty(t, w.harnessFailing(), "nothing went wrong yet")

	log, err := audit.Open(config.ExpandHome(w.cfg.Audit.Dir), nil)
	require.NoError(t, err)
	log.SetWorkspace(w.Dir())
	ctx := audit.WithSession(context.Background(), s.ID)
	for range 2 {
		log.LogContext(ctx, audit.Entry{Kind: audit.KindPrompt, Detail: "deploy"})
		log.LogContext(ctx, audit.Entry{Kind: audit.KindToolCall, Tool: "activate_skill", Args: map[string]any{"skill_name": "deploy"}})
		log.LogContext(ctx, audit.Entry{Kind: audit.KindToolResult, Tool: "run_shell_command", Decision: "exit", Error: "exit code 127", Args: map[string]any{"command": "make push"}})
	}
	require.NoError(t, log.Close())
	assert.Equal(t, "skills", w.harnessFailing())
	assert.Contains(t, w.Suggestions(context.Background()).Tiles, api.Suggestion{Kind: api.SuggestHarnessUpdate, Detail: "skills"})

	write(t, w.Dir(), ".agents/skills/deploy/SKILL.md", "---\nname: deploy\ndescription: Deploys\n---\nRun make deploy.\n")
	assert.Empty(t, w.harnessFailing(), "changed since: the update was made")
}

func TestAddToSection(t *testing.T) {
	tests := []struct{ name, text, want string }{
		{"an empty file", "", "## How to work\n\n- new\n"},
		{"no section yet", "Use tabs.\n", "Use tabs.\n\n## How to work\n\n- new\n"},
		{"an empty section", "## How to work\n\n## This project\n", "## How to work\n\n- new\n\n## This project\n"},
		{"after the last entry", "## How to work\n\n- old\n\n## This project\n\n- x\n", "## How to work\n\n- old\n- new\n\n## This project\n\n- x\n"},
		{"the last section", "# Me\n\n## How to work\n- old\n", "# Me\n\n## How to work\n- old\n- new\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, addToSection(tt.text, howToWorkHeading, "- new"))
		})
	}
}

// The reflector reads the "How to work" entries of the user's other
// workspaces, and a chosen one goes to the global file once (HE-21).
func TestGlobalExperience(t *testing.T) {
	global := filepath.Join(t.TempDir(), "BLITZ.md")
	w, _ := openTestWith(t, func(c *config.Config) { c.Memory.Global = global })
	assert.Empty(t, w.globalExperienceText(), "no other workspace")

	other := t.TempDir()
	write(t, other, ".agents/EXPERIENCE.md", "# Experience\n\n## How to work\n\n- Read the error before retrying. (2026-10-01 \"a\")\n\n## This project\n\n- Deploy with make deploy.\n")
	st, err := session.NewStorage(w.cfg.Session.StorageDir)
	require.NoError(t, err)
	st.SetWorkspace(other)
	_, err = st.CreateSession(session.NewSessionID(), "elsewhere", "blitz")
	require.NoError(t, err)
	require.NoError(t, st.AddMessage("user", "hi")) // saved once it has one
	st.Release()

	text := w.globalExperienceText()
	assert.Contains(t, text, fmt.Sprintf("- `%s`: Read the error before retrying.", other))
	assert.NotContains(t, text, "make deploy", "only how to work, never a project's facts")
	assert.Contains(t, text, "is empty")
	evidence, _ := w.readEvidence(context.Background(), time.Time{}, "")
	assert.Contains(t, evidence, "Read the error before retrying", "the reflector reads them")
	skills, _ := w.readEvidence(context.Background(), time.Time{}, "skills")
	assert.NotContains(t, skills, "Read the error", "not the updates")

	path, err := w.saveGlobalExperience(context.Background(), "Read the error before retrying.")
	require.NoError(t, err)
	assert.Equal(t, global, path)
	_, err = w.saveGlobalExperience(context.Background(), "Read the error before retrying.")
	require.NoError(t, err)
	data, err := os.ReadFile(global)
	require.NoError(t, err)
	assert.Equal(t, "## How to work\n\n- Read the error before retrying.\n", string(data), "once")
	assert.Contains(t, w.globalExperienceText(), "holds:\n## How to work")
}
