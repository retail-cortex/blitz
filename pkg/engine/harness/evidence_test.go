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

package harness

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

// at is t0 plus i minutes.
func at(i int) time.Time { return t0.Add(time.Duration(i) * time.Minute) }

func prompt(i int, session, text string) audit.Entry {
	return audit.Entry{Time: at(i), Session: session, Kind: audit.KindPrompt, Detail: text}
}

func call(i int, session, tool string, args map[string]any) audit.Entry {
	return audit.Entry{Time: at(i), Session: session, Kind: audit.KindToolCall, Tool: tool, Args: args}
}

func find(ev Evidence, kind Kind, subject string) *Item {
	for i := range ev.Items {
		if ev.Items[i].Kind == kind && ev.Items[i].Subject == subject {
			return &ev.Items[i]
		}
	}
	return nil
}

func TestBuildEachSource(t *testing.T) {
	sessions := map[string]Session{"s": {Title: "Deploy the site", Agent: "blitz"}, "job": {Title: "⏰ nightly", Origin: "worker", Agent: "blitz"}}
	tests := []struct {
		name    string
		audit   []audit.Entry
		kind    Kind
		subject string
		target  string
		excerpt string
		costly  bool
	}{
		{
			name: "a failed tool, with its path",
			audit: []audit.Entry{prompt(0, "s", "read it"), call(1, "s", "read_file", map[string]any{"path": "a.txt"}),
				{Time: at(1), Session: "s", Kind: audit.KindToolResult, Tool: "read_file", Decision: "error", Error: "no such file", Args: map[string]any{"path": "a.txt"}}},
			kind: ToolFailed, subject: "read_file", target: "a.txt", excerpt: "no such file",
		},
		{
			name: "a command that exited nonzero, with the end of its output",
			audit: []audit.Entry{prompt(0, "s", "deploy"),
				{Time: at(1), Session: "s", Kind: audit.KindToolResult, Tool: "run_shell_command", Decision: "exit", Error: "exit code 2", Detail: "make: *** No rule to make target 'push'", Args: map[string]any{"command": "make push\necho done"}}},
			kind: ToolFailed, subject: "run_shell_command", target: "make push", excerpt: "exit code 2: make: *** No rule to make target 'push'",
		},
		{
			name: "the user's denial, costly at once, with the command it was about",
			audit: []audit.Entry{prompt(0, "s", "clean up"), call(1, "s", "run_shell_command", map[string]any{"command": "rm -rf build"}),
				{Time: at(1), Session: "s", Kind: audit.KindDenial, Tool: "run_shell_command", Decision: "deny", Detail: "run_shell_command rm -rf build"}},
			kind: Denied, subject: "run_shell_command", target: "rm -rf build", excerpt: "denied by the user", costly: true,
		},
		{
			name:  "the auto reviewer's denial, with its reason",
			audit: []audit.Entry{prompt(0, "s", "push"), {Time: at(1), Session: "s", Kind: audit.KindDenial, Tool: "run_shell_command", Decision: "auto-deny: pushes to main", Detail: "git push origin main"}},
			kind:  Denied, subject: "run_shell_command", target: "git push origin main", excerpt: "denied by the auto reviewer: pushes to main",
		},
		{
			name:  "a guardrail's block",
			audit: []audit.Entry{prompt(0, "s", "x"), {Time: at(1), Session: "s", Kind: audit.KindGuardrail, Tool: "guardrail:secrets", Decision: "block", Detail: "an API key"}},
			kind:  Blocked, subject: "secrets", excerpt: "an API key",
		},
		{
			name:  "a rule that sent the work back",
			audit: []audit.Entry{prompt(0, "s", "x"), {Time: at(1), Session: "s", Kind: audit.KindRule, Tool: "rule:cite-sources", Decision: "revise", Detail: "no sources"}},
			kind:  Revised, subject: "cite-sources", excerpt: "no sources",
		},
		{
			name:  "a steer, about the session's agent",
			audit: []audit.Entry{prompt(0, "s", "x"), {Time: at(1), Session: "s", Kind: audit.KindPrompt, Decision: audit.DecisionSteer, Detail: "use pnpm, not npm"}},
			kind:  Steered, subject: "blitz", excerpt: "use pnpm, not npm",
		},
		{
			name:  "a correction of the last answer",
			audit: []audit.Entry{prompt(0, "s", "add a test"), prompt(5, "s", "No, that's not what I asked")},
			kind:  Corrected, subject: "blitz", excerpt: "No, that's not what I asked",
		},
		{
			name:  "an undo, costly at once, with the prompt it undid",
			audit: []audit.Entry{prompt(0, "s", "refactor it"), {Time: at(3), Session: "s", Kind: audit.KindUndo, Detail: "rewind: files"}},
			kind:  Undone, subject: "blitz", excerpt: "refactor it", costly: true,
		},
		{
			name:  "a stopped turn",
			audit: []audit.Entry{prompt(0, "s", "explain"), {Time: at(2), Session: "s", Kind: audit.KindTurnEnd, Decision: audit.DecisionStopped}},
			kind:  Stopped, subject: "blitz", excerpt: "explain",
		},
		{
			name:  "a job's turn at its cost limit",
			audit: []audit.Entry{prompt(0, "job", "nightly report"), {Time: at(9), Session: "job", Kind: audit.KindTurnEnd, Decision: audit.DecisionLimit, Detail: "cost"}},
			kind:  Limited, subject: "blitz", target: "cost", excerpt: "nightly report", costly: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := Build(Inputs{Since: t0, Audit: tt.audit, Sessions: sessions})
			it := find(ev, tt.kind, tt.subject)
			require.NotNil(t, it, "no %s item about %s in %+v", tt.kind, tt.subject, ev.Items)
			assert.Equal(t, tt.target, it.Target)
			assert.Equal(t, 1, it.Count)
			require.Len(t, it.Seen, 1)
			assert.Equal(t, tt.excerpt, it.Seen[0].Excerpt)
			assert.Equal(t, tt.costly, it.Patterned(), "once is a pattern only with a cost")
		})
	}
}

func TestBuildIgnores(t *testing.T) {
	tests := []struct {
		name  string
		audit []audit.Entry
	}{
		{"a tool that worked", []audit.Entry{prompt(0, "s", "x"), {Time: at(1), Session: "s", Kind: audit.KindToolResult, Tool: "read_file", Decision: "ok"}}},
		{"a denial nobody decided", []audit.Entry{prompt(0, "s", "x"), {Time: at(1), Session: "s", Kind: audit.KindDenial, Tool: "run_shell_command", Decision: "no-approver"}}},
		{"a guardrail's pass and warning", []audit.Entry{prompt(0, "s", "x"), {Time: at(1), Session: "s", Kind: audit.KindGuardrail, Tool: "guardrail:pii", Decision: "pass"}, {Time: at(2), Session: "s", Kind: audit.KindGuardrail, Tool: "guardrail:pii", Decision: "warn"}}},
		{"a rule's pass", []audit.Entry{prompt(0, "s", "x"), {Time: at(1), Session: "s", Kind: audit.KindRule, Tool: "rule:r", Decision: "pass"}}},
		{"a session's first prompt, even one starting with no", []audit.Entry{prompt(0, "s", "No tests yet: write some")}},
		{"an entry that names neither a session nor a workspace", []audit.Entry{prompt(0, "s", "x"), {Time: at(1), Kind: audit.KindToolResult, Tool: "read_file", Decision: "error"}}},
		{"a tool stopped with its turn", []audit.Entry{prompt(0, "s", "x"), {Time: at(1), Session: "s", Kind: audit.KindToolResult, Tool: "browser", Decision: "error", Error: "prompt failed: context canceled"}}},
		{"a session the workspace doesn't list (an eval's case)", []audit.Entry{prompt(0, "eval", "x"), {Time: at(1), Session: "eval", Kind: audit.KindTurnEnd, Decision: audit.DecisionStopped}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := Build(Inputs{Since: t0, Audit: tt.audit, Sessions: map[string]Session{"s": {}}})
			assert.Empty(t, ev.Items)
		})
	}
}

// A refused action's result repeats the refusal: it's counted once, as a
// denial; the tool's next failure is its own.
func TestBuildCountsARefusalOnce(t *testing.T) {
	log := []audit.Entry{
		prompt(0, "s", "write it"), call(1, "s", "create_file", map[string]any{"path": "a.md"}),
		{Time: at(1), Session: "s", Kind: audit.KindDenial, Tool: "create_file", Decision: "unattended-refused", Detail: "Create a.md"},
		{Time: at(1), Session: "s", Kind: audit.KindToolResult, Tool: "create_file", Decision: "error", Error: "action not approved: Create a.md", Args: map[string]any{"path": "a.md"}},
		{Time: at(2), Session: "s", Kind: audit.KindToolResult, Tool: "create_file", Decision: "error", Error: "disk full", Args: map[string]any{"path": "b.md"}},
	}
	ev := Build(Inputs{Since: t0, Audit: log, Sessions: map[string]Session{"s": {}}})
	require.Len(t, ev.Items, 2, "items %+v", ev.Items)
	kinds := []Kind{ev.Items[0].Kind, ev.Items[1].Kind}
	assert.ElementsMatch(t, []Kind{Denied, ToolFailed}, kinds)
	assert.Equal(t, "b.md", find(ev, ToolFailed, "create_file").Target)
}

func TestBuildGroupsRepeats(t *testing.T) {
	var log []audit.Entry
	for i := range 5 {
		s := fmt.Sprintf("s%d", i)
		log = append(log, prompt(i*10, s, "deploy"),
			audit.Entry{Time: at(i*10 + 1), Session: s, Kind: audit.KindToolResult, Tool: "run_shell_command", Decision: "exit", Error: "exit code 127", Args: map[string]any{"command": "make push"}})
	}
	log = append(log, prompt(60, "s0", "read"), audit.Entry{Time: at(61), Session: "s0", Kind: audit.KindToolResult, Tool: "read_file", Decision: "error", Error: "gone", Args: map[string]any{"path": "x"}})
	sessions := map[string]Session{}
	for i := range 5 {
		sessions[fmt.Sprintf("s%d", i)] = Session{Title: fmt.Sprintf("Deploy %d", i)}
	}
	ev := Build(Inputs{Since: t0, Audit: log, Sessions: sessions})

	require.Len(t, ev.Items, 2)
	it := ev.Items[0]
	assert.Equal(t, "make push", it.Target, "the pattern comes first")
	assert.Equal(t, 5, it.Count)
	assert.True(t, it.Patterned())
	assert.Equal(t, at(1), it.First)
	assert.Equal(t, at(41), it.Last)
	require.Len(t, it.Seen, maxSeen, "it cites the latest three")
	assert.Equal(t, "Deploy 4", it.Seen[0].Title, "newest first")
	assert.Equal(t, "read_file", ev.Items[1].Subject)
	assert.False(t, ev.Items[1].Patterned())
	assert.Equal(t, 1, ev.Patterns(), "the tile counts the items that are patterns")
}

func TestBuildBlamesSkillsAndAgentsInTurnsThatWentWrong(t *testing.T) {
	log := []audit.Entry{
		// A turn with the deploy skill that failed, and one that went well.
		prompt(0, "s", "deploy"), call(1, "s", "activate_skill", map[string]any{"skill_name": "deploy"}),
		{Time: at(2), Session: "s", Kind: audit.KindToolResult, Tool: "run_shell_command", Decision: "exit", Error: "exit code 1", Args: map[string]any{"command": "make push"}},
		prompt(10, "s", "deploy again"), call(11, "s", "run_skill_script", map[string]any{"skill": "deploy"}),
		// A turn that invoked qa and was stopped.
		prompt(20, "s", "review it"), call(21, "s", "invoke_agent", map[string]any{"agent_name": "qa"}),
		{Time: at(22), Session: "s", Kind: audit.KindTurnEnd, Decision: audit.DecisionStopped},
	}
	ev := Build(Inputs{Since: t0, Audit: log, Sessions: map[string]Session{"s": {Agent: "blitz"}}, Fixes: map[string][]string{"skill/deploy": {"Say which make target pushes"}}})

	assert.Equal(t, map[string]int{"skill/deploy": 2, "agent/qa": 1}, ev.Used)
	sk := find(ev, Skill, "deploy")
	require.NotNil(t, sk)
	assert.Equal(t, 1, sk.Count, "one of its two turns went wrong")
	assert.Equal(t, "deploy", sk.Seen[0].Excerpt, "the prompt of the turn")
	assert.Equal(t, 1, sk.Seen[0].Turn)
	assert.Equal(t, []string{"Say which make target pushes"}, sk.Fixes, "the health judge's fix")
	qa := find(ev, Agent, "qa")
	require.NotNil(t, qa)
	assert.Equal(t, 3, qa.Seen[0].Turn)
}

func TestBuildCaps(t *testing.T) {
	var log []audit.Entry
	for i := range MaxItems + 5 {
		log = append(log, prompt(2*i, "s", "x"), audit.Entry{Time: at(2*i + 1), Session: "s", Kind: audit.KindToolResult, Tool: "read_file", Decision: "error", Args: map[string]any{"path": fmt.Sprint(i)}})
	}
	ev := Build(Inputs{Since: t0, Audit: log, Sessions: map[string]Session{"s": {}}})
	assert.Len(t, ev.Items, MaxItems)
	assert.Equal(t, 5, ev.Dropped)
	assert.Equal(t, fmt.Sprint(MaxItems+4), ev.Items[0].Target, "the most recent kept")
}

func TestText(t *testing.T) {
	log := []audit.Entry{
		prompt(0, "s", "deploy"), call(1, "s", "activate_skill", map[string]any{"skill_name": "deploy"}),
		{Time: at(2), Session: "s", Kind: audit.KindToolResult, Tool: "run_shell_command", Decision: "exit", Error: "exit code 127", Detail: "make: not found", Args: map[string]any{"command": "make push"}},
		{Time: at(3), Session: "s", Kind: audit.KindToolResult, Tool: "run_shell_command", Decision: "exit", Error: "exit code 127", Detail: "line one\nline two", Args: map[string]any{"command": "make push"}},
		{Time: at(4), Session: "s", Kind: audit.KindDenial, Tool: "web_fetch", Decision: "deny", Detail: "https://x.example", Agent: "qa"},
	}
	ev := Build(Inputs{Since: t0, Audit: log, Sessions: map[string]Session{"s": {Title: "Ship it", Origin: "worker"}}, Fixes: map[string][]string{"skill/deploy": {"Name the target"}}})
	got := ev.Text()
	for _, want := range []string{
		"Evidence since 2026-10-20: 3 items, 2 of them patterns.",
		`"Ship it" [worker run] turn 1 agent qa: denied by the user`,
		"- [pattern] tool_failed run_shell_command `make push` ×2 (2026-10-20 – 2026-10-20)",
		`· 2026-10-20 09:03 "Ship it" [worker run] turn 1: exit code 127: line one ⏎ line two`,
		"- skill deploy ×1",
		"· health check suggests: Name the target",
		"Turns each skill and agent was in: skill/deploy 1;",
	} {
		assert.Contains(t, got, want)
	}
}

func TestAbout(t *testing.T) {
	log := []audit.Entry{
		prompt(0, "s", "deploy"), call(1, "s", "activate_skill", map[string]any{"skill_name": "deploy"}), call(2, "s", "invoke_agent", map[string]any{"agent_name": "qa"}),
		{Time: at(3), Session: "s", Kind: audit.KindToolResult, Tool: "run_skill_script", Decision: "exit", Error: "exit code 1", Args: map[string]any{"skill": "deploy"}},
		{Time: at(4), Session: "s", Kind: audit.KindToolResult, Tool: "read_file", Decision: "error", Args: map[string]any{"path": "x"}},
		{Time: at(5), Session: "s", Kind: audit.KindRule, Tool: "rule:cite", Decision: "revise"},
		{Time: at(6), Session: "s", Kind: audit.KindDenial, Tool: "run_shell_command", Decision: "deny", Detail: "rm -rf x"},
		{Time: at(7), Session: "s", Kind: audit.KindTurnEnd, Decision: audit.DecisionStopped},
	}
	ev := Build(Inputs{Since: t0, Audit: log, Sessions: map[string]Session{"s": {Agent: "blitz"}}})
	kinds := func(e Evidence) []string {
		var out []string
		for _, it := range e.Items {
			out = append(out, string(it.Kind)+" "+it.Subject)
		}
		slices.Sort(out)
		return out
	}
	tests := []struct {
		about string
		want  []string
		used  map[string]int
	}{
		{"skills", []string{"skill deploy", "tool_failed run_skill_script"}, map[string]int{"skill/deploy": 1}},
		{"agents", []string{"agent qa", "denied run_shell_command", "stopped blitz"}, map[string]int{"agent/qa": 1}},
		{"rules", []string{"denied run_shell_command", "revised cite"}, map[string]int{}},
	}
	for _, tt := range tests {
		t.Run(tt.about, func(t *testing.T) {
			got := ev.About(tt.about)
			assert.Equal(t, tt.want, kinds(got))
			assert.Equal(t, tt.used, got.Used)
		})
	}
	assert.Len(t, ev.About("").Items, len(ev.Items), "everything")
}

func TestTextWithoutTheAuditLog(t *testing.T) {
	assert.Contains(t, Build(Inputs{Since: t0, NoAudit: true}).Text(), "the audit log is off")
}

func TestBuildWithoutSessionsReadsEverything(t *testing.T) {
	ev := Build(Inputs{Since: t0, Audit: []audit.Entry{prompt(0, "s", "x"), {Time: at(1), Session: "s", Kind: audit.KindTurnEnd, Decision: audit.DecisionStopped}}})
	require.Len(t, ev.Items, 1)
	assert.Equal(t, "blitz", ev.Items[0].Subject, "an unknown session's agent is the default")
}

func TestIsCorrection(t *testing.T) {
	tests := []struct {
		prompt string
		want   bool
	}{
		{"No, use the other file", true},
		{"no", true},
		{"That's wrong: the port is 8080", true},
		{"that’s not it", true},
		{"It still fails with the same error", true},
		{"Please undo that", false},
		{"undo that", true},
		{"The build doesn't work after your change", true},
		{"No es lo que pedí", true},
		{"Sigue fallando", true},
		{"Eso está mal", true},
		{"Non, pas ce fichier", true},
		{"Ce n'est pas ce que j'ai demandé", true},
		{"Ça ne marche pas", true},
		{"No worries, carry on", false},
		{"Nothing else, thanks", false},
		{"Now add a test", false},
		{"Add a wrong-password test", false},
		{"/undo", false},
		{"", false},
		{"Notes: make it shorter", false},
	}
	for _, tt := range tests {
		t.Run(tt.prompt, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCorrection(tt.prompt))
		})
	}
}
