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
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
)

// The web search prompt lists the results, their snippets and the engine's
// own answer, marked unverified.
func TestWebSearchPrompt(t *testing.T) {
	got := WebSearchPrompt("go generics", []tools.SearchResult{
		{Title: "Go blog", URL: "https://go.dev/blog", Snippet: "type parameters"},
		{Title: "Spec", URL: "https://go.dev/ref/spec"},
	}, "Generics landed in 1.18.")
	assert.Contains(t, got, "I searched the web for: go generics")
	assert.Contains(t, got, "1. Go blog\n   https://go.dev/blog\n   type parameters\n")
	assert.Contains(t, got, "2. Spec\n   https://go.dev/ref/spec\n")
	assert.Contains(t, got, "unverified")
	assert.NotContains(t, WebSearchPrompt("x", nil, ""), "unverified", "no answer, no summary section")
}

// The session search prompt quotes the matches, or says there were none.
func TestSessionSearchPrompt(t *testing.T) {
	at := time.Date(2026, 3, 4, 5, 6, 0, 0, time.UTC)
	got := SessionSearchPrompt("flags", []session.Match{{Index: 2, Message: session.Message{Role: "user", Timestamp: at}, Excerpt: "the CLI flags"}}, 5)
	assert.Contains(t, got, "(1 of 5 matching messages")
	assert.Contains(t, got, "[message 3, user, 2026-03-04 05:06]\nthe CLI flags")
	none := SessionSearchPrompt("flags", nil, 0)
	assert.Contains(t, none, "No message in the session transcript")
}

// The plan prompts carry the goal and the plan's file.
func TestPlanPrompts(t *testing.T) {
	assert.Contains(t, PlanPrompt("  add a flag \n"), "Goal:\nadd a flag")
	assert.Contains(t, CarryOutPrompt("plan.md"), "saved in plan.md")
	assert.NotContains(t, CarryOutPrompt(""), "saved in")
	assert.True(t, PlanAllows("read_file"))
	assert.False(t, PlanAllows("create_file"))
}

// Read-only runs name their mode in the refusal; allowed tools take Claude
// Code's names and patterns.
func TestRunRefusals(t *testing.T) {
	st := &runState{}
	WithReadOnly("/search")(st)
	assert.Contains(t, planRefusal(st, false, "create_file")["error"], "/search is read-only")

	st = &runState{}
	WithAllowedTools(nil)(st)
	assert.Nil(t, st.allowed, "no names, no limit")
	WithAllowedTools([]string{"Bash(git *)", " Read "})(st)
	assert.Nil(t, allowedRefusal(st, "run_shell_command"))
	assert.Nil(t, allowedRefusal(st, "view_image"))
	assert.Nil(t, allowedRefusal(nil, "anything"))
	assert.Contains(t, allowedRefusal(st, "create_file")["error"], "create_file isn't among the tools")
}
