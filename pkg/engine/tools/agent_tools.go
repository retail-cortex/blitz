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

	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// AgentSummary provides summary info for list_agents.
type AgentSummary struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

// ListAgentsInput defines input for listing agents.
type ListAgentsInput struct {
	Filter string `json:"filter,omitempty" jsonschema:"Optional substring filter for agent names"`
}

// ListAgentsOutput holds registered agents.
type ListAgentsOutput struct {
	Agents []AgentSummary `json:"agents"`
}

// NewListAgentsTool creates an ADK tool for listing agents.
func NewListAgentsTool(registry *agents.Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "list_agents",
			Description: "List all available agent personas and capabilities",
		},
		func(ctx agent.Context, input ListAgentsInput) (ListAgentsOutput, error) {
			filter := strings.ToLower(input.Filter)
			list := registry.List()
			summaries := make([]AgentSummary, 0, len(list))
			for _, a := range list {
				if filter != "" && !strings.Contains(strings.ToLower(a.Name), filter) {
					continue
				}
				summaries = append(summaries, AgentSummary{
					Name:        a.Name,
					DisplayName: a.DisplayName,
					Description: a.Description,
				})
			}
			return ListAgentsOutput{Agents: summaries}, nil
		},
	)
}

// InvokeAgentInput defines arguments for invoke_agent.
type InvokeAgentInput struct {
	AgentName string `json:"agent_name" jsonschema:"The name of the agent to invoke (e.g. qa, helios, planning-agent)"`
	Prompt    string `json:"prompt" jsonschema:"The specific task instruction for the delegated agent"`
	// Background runs it beside this turn as a task.
	Background *bool `json:"background,omitempty" jsonschema:"Run the agent in the background as a task and return at once with its task_id; its result reaches you when it finishes (task_output reads it before). Nothing it would need to ask the user about runs. Use for independent work: reviews, research, long test runs"`
}

// InvokeAgentOutput holds result of invoking an agent.
type InvokeAgentOutput struct {
	AgentName string `json:"agent_name"`
	Response  string `json:"response"`
	// TaskID and Status: a background task, started.
	TaskID string `json:"task_id,omitempty"`
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

// NewInvokeAgentTool creates an ADK tool for subagent delegation. The actual
// invocation is supplied at runtime through hooks.SetSubagentInvoker.
func NewInvokeAgentTool(registry *agents.Registry, hooks *Hooks) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "invoke_agent",
			Description: "Delegate a task to another specialized agent persona and receive their response",
		},
		func(ctx agent.Context, input InvokeAgentInput) (InvokeAgentOutput, error) {
			fail := func(msg string) (InvokeAgentOutput, error) {
				return InvokeAgentOutput{AgentName: input.AgentName, Error: msg}, nil
			}
			spec, ok := registry.Get(input.AgentName)
			if !ok {
				return fail(fmt.Sprintf("agent '%s' is not registered; use list_agents to inspect available agents", input.AgentName))
			}
			if strings.TrimSpace(input.Prompt) == "" {
				return fail("prompt must not be empty")
			}
			background := spec.Background // the agent's default
			if input.Background != nil {
				background = *input.Background
			}
			if background {
				runner := hooks.taskRunner()
				switch {
				case runner == nil:
					return fail("background tasks are not available in this session")
				case isUnattended(ctx):
					return fail("background tasks can't be started from a background task or an unattended run; invoke the agent without background")
				}
				t, err := runner.StartTask(ctx, input.AgentName, input.Prompt)
				if err != nil {
					return fail(err.Error())
				}
				return InvokeAgentOutput{
					AgentName: input.AgentName, TaskID: t.ID, Status: t.State,
					Response: fmt.Sprintf("Started in the background as %s. Its result reaches you when it finishes; task_output reads it before.", t.ID),
				}, nil
			}
			invoker := hooks.subagentInvoker()
			if invoker == nil {
				return fail("subagent delegation is not available in this session")
			}
			res, err := invoker(ctx, input.AgentName, input.Prompt)
			if err != nil {
				return fail(fmt.Sprintf("subagent failed: %v", err))
			}
			return InvokeAgentOutput{AgentName: input.AgentName, Response: res}, nil
		},
	)
}
