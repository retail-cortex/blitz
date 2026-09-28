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
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// hookLog returns a hook command that appends each event's JSON to log,
// then runs then (a shell snippet that can read $input).
func hookLog(log, then string) config.HookConfig {
	return config.HookConfig{Command: `input=$(cat); printf '%s\n' "$input" >> '` + log + `'; ` + then}
}

// hookEvents reads the logged events, waiting up to 5 s for want of them
// (background hooks run on their own worker).
func hookEvents(t *testing.T, log string, want int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var out []map[string]any
		if f, err := os.Open(log); err == nil {
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				var m map[string]any
				if json.Unmarshal(sc.Bytes(), &m) == nil {
					out = append(out, m)
				}
			}
			f.Close()
		}
		if len(out) >= want || time.Now().After(deadline) {
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func ofEvent(evs []map[string]any, name string) []map[string]any {
	var out []map[string]any
	for _, e := range evs {
		if e["event"] == name {
			out = append(out, e)
		}
	}
	return out
}

func TestLifecycleHooks(t *testing.T) {
	log := filepath.Join(t.TempDir(), "events.jsonl")
	read := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "read_file", Args: map[string]any{"path": "missing.txt"}}}}}
	w, llm := openTestWith(t, func(c *config.Config) {
		c.Hooks.SessionStart = []config.HookConfig{hookLog(log, `echo "The team uses tabs."`)}
		c.Hooks.PromptSubmit = []config.HookConfig{hookLog(log, `echo "Ticket: BLZ-42"`)}
		c.Hooks.Stop = []config.HookConfig{hookLog(log, `case "$input" in *'"stop_hook_active":true'*) ;; *) echo '{"continue": true, "reason": "Now run the tests."}';; esac`)}
		c.Hooks.PostToolFailure = []config.HookConfig{hookLog(log, "")}
		c.Hooks.SessionEnd = []config.HookConfig{hookLog(log, "")}
	}, read, text("done"), text("tests pass"))

	s, _, err := w.OpenSession("", false)
	require.NoError(t, err)
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "fix it"}, func(api.Event) {})
	require.NoError(t, err)
	first := userTextAt(llm.Requests[0].Contents)
	assert.Contains(t, first, "fix it", "hook context missing from the prompt:\n%s", first)
	assert.Contains(t, first, "The team uses tabs.", "hook context missing from the prompt:\n%s", first)
	assert.Contains(t, first, "Ticket: BLZ-42", "hook context missing from the prompt:\n%s", first)
	// The stop hook made the agent go on once.
	assert.Equal(t, 3, llm.Calls(), "stop continuation: %d calls, output %q", llm.Calls(), res.Output)
	assert.Equal(t, "Now run the tests.", userTextAt(llm.Requests[2].Contents), "stop continuation: %d calls, output %q", llm.Calls(), res.Output)
	assert.Contains(t, res.Output, "tests pass", "stop continuation: %d calls, output %q", llm.Calls(), res.Output)
	var recorded []string
	for _, m := range w.storage.Active().Messages {
		recorded = append(recorded, m.Content)
	}
	got := strings.Join(recorded, "|")
	assert.True(t, strings.HasPrefix(got, "fix it|"), "transcript %q", got)
	assert.Contains(t, got, "(stop hook) Now run the tests.", "transcript %q", got)
	w.NewSession() // ends the first session (in the background) and starts one

	// session_start twice, prompt_submit, post_tool_failure, stop twice, session_end.
	evs := hookEvents(t, log, 7)
	start, submit, stop, failure, end := ofEvent(evs, "session_start"), ofEvent(evs, "prompt_submit"), ofEvent(evs, "stop"), ofEvent(evs, "post_tool_failure"), ofEvent(evs, "session_end")
	assert.Len(t, start, 2, "session_start %v", start)
	assert.Equal(t, "startup", start[0]["reason"], "session_start %v", start)
	assert.Equal(t, "new", start[1]["reason"], "session_start %v", start)
	assert.Len(t, stop, 2, "stop events %v", stop)
	assert.NotEqual(t, true, stop[0]["stop_hook_active"], "stop events %v", stop)
	assert.Equal(t, true, stop[1]["stop_hook_active"], "stop events %v", stop)
	assert.Len(t, failure, 1, "post_tool_failure %v", failure)
	assert.Equal(t, "read_file", failure[0]["tool"], "post_tool_failure %v", failure)
	assert.NotNil(t, failure[0]["error"], "post_tool_failure %v", failure)
	assert.Len(t, end, 1, "session_end %v", end)
	assert.Equal(t, "new", end[0]["reason"], "session_end %v", end)
	assert.Equal(t, s.ID, end[0]["session_id"], "session_end %v", end)
	require.Len(t, submit, 1, "prompt_submit %v", submit)
	e := submit[0]
	assert.NotEqual(t, "", e["prompt_id"], "event context fields %v", e)
	assert.Equal(t, "default", e["permission_mode"], "event context fields %v", e)
	assert.Equal(t, "blitz", e["agent"], "event context fields %v", e)
	assert.Equal(t, w.Dir(), e["cwd"], "event context fields %v", e)
	assert.True(t, strings.HasSuffix(e["transcript_path"].(string), s.ID+".jsonl"), "event context fields %v", e)
}

func TestPermissionRequestAndNotificationHooks(t *testing.T) {
	log := filepath.Join(t.TempDir(), "events.jsonl")
	create := func(p string) *genai.Content {
		return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": p, "content": "x"}}}}}
	}
	w, _ := openTestWith(t, func(c *config.Config) {
		c.Hooks.PermissionRequest = []config.HookConfig{hookLog(log, `case "$input" in *allowed.txt*) echo '{"decision":"allow"}';; *denied.txt*) echo '{"decision":"deny","reason":"not on Fridays"}';; esac`)}
		c.Hooks.Notification = []config.HookConfig{hookLog(log, "")}
	}, create("allowed.txt"), create("denied.txt"), create("asked.txt"), text("done"))
	asked := 0
	w.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) { asked++; return 1, nil }, nil)
	s, _ := w.NewSession()
	var results []map[string]any
	w.Run(context.Background(), s.ID, api.Turn{Text: "make files"}, func(e api.Event) {
		if e.ToolResult != nil {
			results = append(results, e.ToolResult.Result)
		}
	})
	_, err := os.Stat(filepath.Join(w.Dir(), "allowed.txt"))
	assert.NoError(t, err, "the hook's allow didn't apply")
	msg, _ := results[1]["error"].(string)
	assert.Contains(t, msg, "not on Fridays", "the hook's deny didn't apply: %v", results[1])
	assert.Equal(t, 1, asked, "the user was asked %d times, want 1 (asked.txt)", asked)
	notes := ofEvent(hookEvents(t, log, 4), "notification")
	assert.Len(t, notes, 1, "notification %v", notes)
	assert.Equal(t, "permission_prompt", notes[0]["type"], "notification %v", notes)
	assert.Contains(t, notes[0]["message"].(string), "asked.txt", "notification %v", notes)
}

func TestSubagentAndCompactionHooks(t *testing.T) {
	log := filepath.Join(t.TempDir(), "events.jsonl")
	invoke := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "invoke_agent", Args: map[string]any{"agent_name": "qa", "prompt": "check it"}}}}}
	w, _ := openTestWith(t, func(c *config.Config) {
		for _, list := range []*[]config.HookConfig{&c.Hooks.SubagentStart, &c.Hooks.SubagentStop, &c.Hooks.PreCompact, &c.Hooks.PostCompact} {
			*list = []config.HookConfig{hookLog(log, "")}
		}
	}, invoke, text("qa says fine"), text("done"), text("second answer"), text("summary of the start"))
	s, _ := w.NewSession()
	w.Run(context.Background(), s.ID, api.Turn{Text: "delegate"}, func(api.Event) {})
	w.Run(context.Background(), s.ID, api.Turn{Text: "again"}, func(api.Event) {})
	_, err := w.Compact(context.Background(), "keep decisions")
	require.NoError(t, err)
	evs := hookEvents(t, log, 4)
	start, stop := ofEvent(evs, "subagent_start"), ofEvent(evs, "subagent_stop")
	assert.Len(t, start, 1, "subagent_start %v", start)
	assert.Equal(t, "qa", start[0]["subagent"], "subagent_start %v", start)
	assert.Equal(t, "check it", start[0]["prompt"], "subagent_start %v", start)
	assert.Len(t, stop, 1, "subagent_stop %v", stop)
	assert.Equal(t, "qa says fine", stop[0]["output"], "subagent_stop %v", stop)
	pre, post := ofEvent(evs, "pre_compact"), ofEvent(evs, "post_compact")
	assert.Len(t, pre, 1, "compaction events %v %v", pre, post)
	assert.Equal(t, "manual", pre[0]["reason"], "compaction events %v %v", pre, post)
	assert.Equal(t, "keep decisions", pre[0]["prompt"], "compaction events %v %v", pre, post)
	assert.Len(t, post, 1, "compaction events %v %v", pre, post)
}
