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
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "fix it"}, func(api.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	first := userTextAt(llm.Requests[0].Contents)
	if !strings.Contains(first, "fix it") || !strings.Contains(first, "The team uses tabs.") || !strings.Contains(first, "Ticket: BLZ-42") {
		t.Errorf("hook context missing from the prompt:\n%s", first)
	}
	// The stop hook made the agent go on once.
	if llm.Calls() != 3 || userTextAt(llm.Requests[2].Contents) != "Now run the tests." || !strings.Contains(res.Output, "tests pass") {
		t.Errorf("stop continuation: %d calls, output %q", llm.Calls(), res.Output)
	}
	var recorded []string
	for _, m := range w.storage.Active().Messages {
		recorded = append(recorded, m.Content)
	}
	if got := strings.Join(recorded, "|"); !strings.HasPrefix(got, "fix it|") || !strings.Contains(got, "(stop hook) Now run the tests.") {
		t.Errorf("transcript %q", got)
	}
	w.NewSession() // ends the first session (in the background) and starts one

	// session_start twice, prompt_submit, post_tool_failure, stop twice, session_end.
	evs := hookEvents(t, log, 7)
	start, submit, stop, failure, end := ofEvent(evs, "session_start"), ofEvent(evs, "prompt_submit"), ofEvent(evs, "stop"), ofEvent(evs, "post_tool_failure"), ofEvent(evs, "session_end")
	if len(start) != 2 || start[0]["reason"] != "startup" || start[1]["reason"] != "new" {
		t.Errorf("session_start %v", start)
	}
	if len(stop) != 2 || stop[0]["stop_hook_active"] == true || stop[1]["stop_hook_active"] != true {
		t.Errorf("stop events %v", stop)
	}
	if len(failure) != 1 || failure[0]["tool"] != "read_file" || failure[0]["error"] == nil {
		t.Errorf("post_tool_failure %v", failure)
	}
	if len(end) != 1 || end[0]["reason"] != "new" || end[0]["session_id"] != s.ID {
		t.Errorf("session_end %v", end)
	}
	if len(submit) != 1 {
		t.Fatalf("prompt_submit %v", submit)
	}
	e := submit[0]
	if e["prompt_id"] == "" || e["permission_mode"] != "default" || e["agent"] != "blitz" || e["cwd"] != w.Dir() ||
		!strings.HasSuffix(e["transcript_path"].(string), s.ID+".jsonl") {
		t.Errorf("event context fields %v", e)
	}
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
	if _, err := os.Stat(filepath.Join(w.Dir(), "allowed.txt")); err != nil {
		t.Errorf("the hook's allow didn't apply: %v", err)
	}
	if msg, _ := results[1]["error"].(string); !strings.Contains(msg, "not on Fridays") {
		t.Errorf("the hook's deny didn't apply: %v", results[1])
	}
	if asked != 1 {
		t.Errorf("the user was asked %d times, want 1 (asked.txt)", asked)
	}
	notes := ofEvent(hookEvents(t, log, 4), "notification")
	if len(notes) != 1 || notes[0]["type"] != "permission_prompt" || !strings.Contains(notes[0]["message"].(string), "asked.txt") {
		t.Errorf("notification %v", notes)
	}
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
	if _, err := w.Compact(context.Background(), "keep decisions"); err != nil {
		t.Fatal(err)
	}
	evs := hookEvents(t, log, 4)
	start, stop := ofEvent(evs, "subagent_start"), ofEvent(evs, "subagent_stop")
	if len(start) != 1 || start[0]["subagent"] != "qa" || start[0]["prompt"] != "check it" {
		t.Errorf("subagent_start %v", start)
	}
	if len(stop) != 1 || stop[0]["output"] != "qa says fine" {
		t.Errorf("subagent_stop %v", stop)
	}
	pre, post := ofEvent(evs, "pre_compact"), ofEvent(evs, "post_compact")
	if len(pre) != 1 || pre[0]["reason"] != "manual" || pre[0]["prompt"] != "keep decisions" || len(post) != 1 {
		t.Errorf("compaction events %v %v", pre, post)
	}
}
