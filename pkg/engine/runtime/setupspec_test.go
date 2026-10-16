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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /setup leads with the specification (spec_memory_012 MEM-43): with none,
// the setup agent invokes project-spec in the foreground, whose questions
// reach the person in the same turn, and carries on with what it wrote.
func TestSetupRunsTheSpecAgentAndItsQuestionsReachThePerson(t *testing.T) {
	spec := NewMockLLM("gemini-3.8-flash",
		toolCall("ask_user_question", map[string]any{"question": "What kind of project is this?"}),
		textContent("Wrote .agents/specs/001-overview.md (Kind: software)."))
	f := newEngineWith(t, fixtureOpts{opts: []Option{WithAgentModel("project-spec", spec)}},
		toolCall("invoke_agent", map[string]any{"agent_name": "project-spec", "prompt": "Write this project's specification. You're running inside /setup."}),
		textContent("Set up: .agents/AGENTS.md, from the specification."))
	var asked []string
	f.tools.Hooks().SetUserPrompter(func(_ context.Context, q string, _ []string) (string, error) {
		asked = append(asked, q)
		return "Software", nil
	})

	got, err := functionResponses(t, f.eng, "s", "/setup", WithAgent("project-setup"))
	require.NoError(t, err)
	assert.Equal(t, []string{"What kind of project is this?"}, asked, "the spec agent's question reached the person")
	require.NotNil(t, got["invoke_agent"], "no invoke_agent response")
	assert.Contains(t, got["invoke_agent"]["response"], "001-overview.md", "setup got the spec agent's summary: %v", got["invoke_agent"])
	assert.Equal(t, 2, spec.Calls(), "the spec agent asked, then wrote")
	assert.Equal(t, 2, f.llm.Calls(), "setup invoked it, then carried on")
}
