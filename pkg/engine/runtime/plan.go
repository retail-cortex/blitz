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

package runtime

import (
	"fmt"
	"slices"
	"strings"
)

// planReadOnlyTools may run in plan mode: they read, search or ask, and
// change nothing. Anything else, including MCP tools (whose effects are
// unknown), is refused. Sub-agents run inside the same plan-mode run, so
// invoke_agent can research but its agents can't write either.
var planReadOnlyTools = map[string]bool{
	"read_file":             true,
	"list_files":            true,
	"glob":                  true,
	"grep":                  true,
	"search_workspace":      true,
	"lsp":                   true,
	"view_image":            true,
	"view_document":         true,
	"view_media":            true,
	"web_fetch":             true,
	"web_search":            true,
	"list_agents":           true,
	"invoke_agent":          true,
	"list_or_search_skills": true,
	"list_mcp_resources":    true,
	"remember":              true,
	"read_mcp_resource":     true,
	"list_tasks":            true,
	"task_output":           true,
	"activate_skill":        true,
	"ask_user_question":     true,
	"todo":                  true,
	"exit_plan_mode":        true,
	"enter_plan_mode":       true,
}

// PlanAllows reports whether a tool may run in plan mode.
func PlanAllows(toolName string) bool { return planReadOnlyTools[toolName] }

// WithPlanOnly runs the prompt in plan mode: the agent may read and search
// but every tool that could change something is refused, so the answer is
// a plan grounded in the code rather than an edit.
func WithPlanOnly() ExecOption { return func(s *runState) { s.planOnly = true } }

// WithReadOnly refuses the same tools as plan mode, for a prompt that only
// looks things up (e.g. /search). mode names it in the refusal.
func WithReadOnly(mode string) ExecOption {
	return func(s *runState) { s.planOnly, s.mode = true, mode }
}

// planRefusal returns the tool result for a tool refused in plan mode, or
// nil when the tool may run. planning is the turn's plan state (the agent
// entered plan mode itself).
func planRefusal(st *runState, planning bool, toolName string) map[string]any {
	if planReadOnlyTools[toolName] || !planning && (st == nil || !st.planOnly) {
		return nil
	}
	if st != nil && st.mode != "" {
		return map[string]any{"error": fmt.Sprintf("%s is read-only: %s is disabled because it could change something.", st.mode, toolName)}
	}
	return map[string]any{"error": fmt.Sprintf("plan mode: %s is disabled because it could change something. Describe this step in the plan instead.", toolName)}
}

// PlanPrompt wraps a goal in the instructions for plan mode.
func PlanPrompt(goal string) string {
	return "You are in plan-only mode. You may read files, search and look things up, but tools that change anything (editing files, running commands) are disabled. Investigate as much as you need, then write a plan with:\n" +
		"1. A short summary of the objective\n" +
		"2. A numbered implementation plan, naming the files and functions involved\n" +
		"3. Risks and unknowns\n" +
		"4. How to verify the change\n" +
		"5. Questions for the user, only if something blocks the plan\n\n" +
		"Present it with exit_plan_mode for the user to approve; if they can't review it, answer with the plan.\n\n" +
		"Goal:\n" + strings.TrimSpace(goal)
}

// CarryOutPrompt asks the agent to carry out the plan the user approved.
func CarryOutPrompt(planFile string) string {
	s := "The user approved your plan. Carry it out now, step by step, keeping the task list up to date with the todo tool, and verify the result."
	if planFile != "" {
		s += " The plan is saved in " + planFile + "."
	}
	return s
}

// toolAliases maps Claude Code's tool names, as written in commands'
// allowed-tools, to Blitz's.
var toolAliases = map[string][]string{
	"bash":         {"run_shell_command", "manage_background_process"},
	"read":         {"read_file", "view_image", "view_document", "view_media"},
	"edit":         {"replace_in_file", "delete_snippet", "apply_patch"},
	"multiedit":    {"replace_in_file", "apply_patch"},
	"write":        {"create_file"},
	"grep":         {"grep"},
	"glob":         {"glob"},
	"ls":           {"list_files"},
	"webfetch":     {"web_fetch"},
	"websearch":    {"web_search"},
	"task":         {"invoke_agent", "list_agents"},
	"agent":        {"invoke_agent", "list_agents"},
	"notebookedit": {"notebook_edit"},
}

// WithAllowedTools limits the run to the named tools (Blitz's names, or
// Claude Code's such as Bash or Read; "Bash(git *)" counts as Bash): any
// other tool is refused. Empty means no limit.
func WithAllowedTools(names []string) ExecOption {
	return func(s *runState) {
		if len(names) == 0 {
			return
		}
		s.allowed = map[string]bool{}
		for _, n := range names {
			if i := strings.IndexByte(n, '('); i >= 0 {
				n = n[:i]
			}
			n = strings.TrimSpace(n)
			s.allowed[n] = true
			for _, alias := range toolAliases[strings.ToLower(n)] {
				s.allowed[alias] = true
			}
		}
	}
}

// allowedRefusal returns the tool result for a tool outside the run's
// allowed tools, or nil.
func allowedRefusal(st *runState, toolName string) map[string]any {
	if st == nil || st.allowed == nil || st.allowed[toolName] {
		return nil
	}
	names := make([]string, 0, len(st.allowed))
	for n := range st.allowed {
		names = append(names, n)
	}
	slices.Sort(names)
	return map[string]any{"error": fmt.Sprintf("%s isn't among the tools this command allows (%s)", toolName, strings.Join(names, ", "))}
}
