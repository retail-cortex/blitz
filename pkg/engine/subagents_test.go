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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// modelCall is a model reply that calls one tool.
func modelCall(name string, args map[string]any) *genai.Content {
	return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: name, Args: args}}}}
}

// todoCall is a model reply that sends a task list.
func todoCall(items ...string) *genai.Content {
	var list []any
	for i := 0; i+1 < len(items); i += 2 {
		list = append(list, map[string]any{"content": items[i], "status": items[i+1]})
	}
	return modelCall("todo", map[string]any{"items": list})
}

// A sub-agent run in the foreground (as /setup runs project-spec) shows its
// work in the conversation as it goes: its tool calls under its name and its
// task list, then the agent's own list again when it returns. Its words go
// only to the agent that asked, and all of its events are kept in the
// session's sub-agent log.
func TestForegroundSubagentShowsItsWork(t *testing.T) {
	var dir string
	w, _ := openTestWith(t, func(c *config.Config) { dir = c.Session.StorageDir },
		todoCall("Survey", "done", "Specification", "in_progress"),
		modelCall("invoke_agent", map[string]any{"agent_name": "project-spec", "prompt": "write the spec"}),
		todoCall("Interview", "in_progress", "Write the specs", "pending"),
		modelCall("list_files", map[string]any{"directory": "."}),
		text("Wrote 3 specs."),
		text("Set up."),
	)
	s, err := w.NewSession()
	require.NoError(t, err)
	var mu sync.Mutex
	var got []api.Event
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/setup"}, func(e api.Event) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e)
	})
	require.NoError(t, err)

	var lists, authors []string
	var subTools []string
	for _, e := range got {
		if e.Tasks != nil {
			var names []string
			for _, task := range e.Tasks {
				names = append(names, task.Content+":"+task.Status)
			}
			lists = append(lists, strings.Join(names, ","))
			authors = append(authors, e.Author)
		}
		if e.Author == "project-spec" {
			if e.ToolCall != nil {
				subTools = append(subTools, e.ToolCall.Name)
			}
			assert.Nil(t, e.Text, "the sub-agent's words aren't the conversation's: %+v", e.Text)
		}
	}
	require.Len(t, lists, 3, "the agent's list, the sub-agent's, then the agent's again: %v", lists)
	assert.Equal(t, "Survey:done,Specification:in_progress", lists[0], "lists %v", lists)
	assert.Equal(t, "Interview:in_progress,Write the specs:pending", lists[1], "lists %v", lists)
	assert.Equal(t, "project-spec", authors[1], "the sub-agent's list is under its name: %v", authors)
	assert.Equal(t, lists[0], lists[2], "the agent's own list comes back: %v", lists)
	assert.NotEqual(t, "project-spec", authors[2], "the agent's own list comes back: %v", authors)
	assert.Equal(t, []string{"todo", "list_files"}, subTools, "the sub-agent's tool calls show")

	f, err := os.Open(filepath.Join(dir, s.ID+".subagents.jsonl"))
	require.NoError(t, err, "the sub-agent log is kept")
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	log := strings.Join(lines, "\n")
	assert.GreaterOrEqual(t, len(lines), 4, "every event of the sub-agent: %s", log)
	assert.Contains(t, log, `"author":"project-spec"`, "events name the sub-agent")
	assert.Contains(t, log, "Wrote 3 specs.", "its words are in its log")
}

// The handoff to the coder, as the window sees it: the coder's tool calls
// show under its name while it works, its answer goes to blitz as the tool's
// result, and the turn's text is blitz's alone.
func TestHandoffToCoderShowsItsWork(t *testing.T) {
	w, _ := openTestWith(t, nil,
		modelCall("invoke_agent", map[string]any{"agent_name": "coder", "prompt": "fix the typo"}),
		modelCall("list_files", map[string]any{"directory": "."}),
		text("Fixed the typo in README.md."),
		text("Done: the coder fixed it."),
	)
	s, err := w.NewSession()
	require.NoError(t, err)
	var mu sync.Mutex
	var coderTools []string
	var handoff *api.ToolResult
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "fix the typo"}, func(e api.Event) {
		mu.Lock()
		defer mu.Unlock()
		if e.Author == "coder" && e.ToolCall != nil {
			coderTools = append(coderTools, e.ToolCall.Name)
		}
		if e.ToolResult != nil && e.ToolResult.Name == "invoke_agent" {
			handoff = e.ToolResult
		}
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"list_files"}, coderTools, "the coder's work shows")
	require.NotNil(t, handoff, "the handoff's result")
	assert.Equal(t, "Fixed the typo in README.md.", handoff.Result["response"], "the coder's answer goes to blitz")
	assert.Equal(t, "Done: the coder fixed it.", res.Output, "the turn's text is blitz's")
}

// Partial events (the stream's pieces) are skipped; one that can't be saved
// still shows, and the turn goes on.
func TestSubagentEventsSkipsPartialsAndOutlivesASaveFailure(t *testing.T) {
	w, _ := openTestWith(t, nil)
	var got []api.Event
	observe := w.subagentEvents(w.storage, "../not-a-session", func(e api.Event) { got = append(got, e) })
	call := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_files"}}}}
	observe(nil)
	observe(&adksession.Event{LLMResponse: model.LLMResponse{Content: call, Partial: true}, Author: "coder"})
	assert.Empty(t, got, "nothing for a missing or partial event")
	observe(&adksession.Event{LLMResponse: model.LLMResponse{Content: call}, Author: "coder"})
	require.Len(t, got, 1, "shown although it couldn't be saved")
	assert.Equal(t, "coder", got[0].Author)
}
