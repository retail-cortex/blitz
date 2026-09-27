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

package tools

import (
	"fmt"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// Task statuses in the todo list.
const (
	TaskPending    = "pending"
	TaskInProgress = "in_progress"
	TaskDone       = "done"
)

// TodoItem is one task in the agent's list.
type TodoItem struct {
	Content string `json:"content" jsonschema:"What to do, in a few words"`
	Status  string `json:"status" jsonschema:"pending, in_progress or done"`
}

// TodoInput is the whole list, replacing the previous one.
type TodoInput struct {
	Items []TodoItem `json:"items" jsonschema:"Every task, in order, with its current status"`
}

// maxTodoItems bounds a list.
const maxTodoItems = 50

// NewTodoTool keeps the agent's task list for a multi-step job; front ends
// show it as a checklist (see TodoItems). It changes nothing else, so it
// works in plan mode too.
func NewTodoTool() (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name: "todo",
			Description: "Keep a task list for work with several steps, so the user can follow along. Send the whole list each time " +
				"(it replaces the previous one), with each item pending, in_progress or done; mark a step in_progress when you start it " +
				"and done as soon as it is finished. Skip it for one-step tasks.",
		},
		func(_ agent.Context, in TodoInput) (map[string]any, error) {
			if len(in.Items) > maxTodoItems {
				return map[string]any{"error": fmt.Sprintf("at most %d items", maxTodoItems)}, nil
			}
			items := make([]any, 0, len(in.Items))
			done, active := 0, 0
			for i, it := range in.Items {
				content := strings.TrimSpace(it.Content)
				if content == "" {
					return map[string]any{"error": fmt.Sprintf("item %d has no content", i+1)}, nil
				}
				status := strings.ToLower(strings.TrimSpace(it.Status))
				switch status {
				case "", "todo":
					status = TaskPending
				case "in-progress", "active", "doing":
					status = TaskInProgress
				case "completed", "complete":
					status = TaskDone
				}
				switch status {
				case TaskDone:
					done++
				case TaskInProgress:
					active++
				case TaskPending:
				default:
					return map[string]any{"error": fmt.Sprintf("item %d: status %q (use pending, in_progress or done)", i+1, it.Status)}, nil
				}
				items = append(items, map[string]any{"content": content, "status": status})
			}
			out := map[string]any{"items": items, "done": done, "total": len(items)}
			if active > 1 {
				out["note"] = "work on one item at a time: only one should be in_progress"
			}
			return out, nil
		},
	)
}

// TodoItems reads the list from a todo tool result (nil if it isn't one).
func TodoItems(result map[string]any) []TodoItem {
	raw, ok := result["items"].([]any)
	if !ok {
		return nil
	}
	out := make([]TodoItem, 0, len(raw))
	for _, r := range raw {
		m, _ := r.(map[string]any)
		c, _ := m["content"].(string)
		s, _ := m["status"].(string)
		if c != "" {
			out = append(out, TodoItem{Content: c, Status: s})
		}
	}
	return out
}
