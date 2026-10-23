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

package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// /init runs the guided setup (/setup, recorded as such) and loads the
// instructions the agent wrote before the next prompt.
func TestInitWritesAndLoadsBlitzMD(t *testing.T) {
	write := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
		Name: "create_file", Args: map[string]any{"path": "BLITZ.md", "content": "Run tests with `make check`."}}}}}
	app, llm := newCommandAppWith(t, configtest.RunTools, "/init\nhello\n/exit\n", write, genai.NewContentFromText("wrote BLITZ.md", genai.RoleModel), genai.NewContentFromText("hi", genai.RoleModel))
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })

	first := requestTextAt(llm, 0)
	require.Contains(t, first, "Set up this workspace's agent harness", "the setup prompt wasn't sent:\n%s", first)
	assert.Contains(t, out, filepath.Join(local(app).Dir(), "BLITZ.md"), "memory wasn't reloaded after /init:\n%s", out)
	// Memory is part of the system instruction, not the conversation.
	var system strings.Builder
	if cfg := llm.Requests[2].Config; cfg != nil && cfg.SystemInstruction != nil {
		for _, p := range cfg.SystemInstruction.Parts {
			system.WriteString(p.Text)
		}
	}
	assert.Contains(t, system.String(), "Run tests with `make check`.", "the next prompt's instructions don't carry the new BLITZ.md")
	assert.NotContains(t, requestTextAt(llm, 0), "Run tests with", "BLITZ.md was loaded before /init wrote it")
	var recorded []string
	for _, m := range local(app).Storage().Active().Messages {
		recorded = append(recorded, m.Content)
	}
	assert.NotEqual(t, 0, len(recorded), "transcript %q", recorded)
	assert.Equal(t, "/setup", recorded[0], "transcript %q", recorded)
}

// /skills update and /agents update run the bundled update commands as
// turns, recorded as typed; /skills alone is still the REPL's list.
func TestUpdateCommandsRunTurns(t *testing.T) {
	app, llm := newCommandAppWith(t, configtest.RunTools, "/skills update the auth spec\n/agents update\n/skills\n/exit\n",
		genai.NewContentFromText("skills reviewed", genai.RoleModel), genai.NewContentFromText("agents reviewed", genai.RoleModel))
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })
	require.Len(t, llm.Requests, 2, "two turns, and /skills listed the skills:\n%s", out)
	assert.Contains(t, requestTextAt(llm, 0), "Update the skills")
	assert.Contains(t, requestTextAt(llm, 0), "the auth spec")
	assert.Contains(t, requestTextAt(llm, 1), "Update the agents")
	var recorded []string
	for _, m := range local(app).Storage().Active().Messages {
		recorded = append(recorded, m.Content)
	}
	assert.Contains(t, recorded, "/skills update the auth spec")
	assert.Contains(t, recorded, "/agents update")
}

func TestIsUpdateCommand(t *testing.T) {
	for line, want := range map[string]bool{
		"/skills update": true, "/agents update x": true, "/Skills UPDATE": true, "/rules update": true, "/rules": false,
		"/skills": false, "/skills list": false, "/agents": false, "/skills-update": false, "/review update": false,
	} {
		assert.Equal(t, want, isUpdateCommand(line), line)
	}
}

// The welcome says when the harness has something to learn.
func TestReflectHint(t *testing.T) {
	tests := []struct {
		name string
		r    api.Reflection
		err  error
		want string
	}{
		{name: "nothing yet", r: api.Reflection{Patterns: api.ReflectAt - 1}},
		{name: "patterns", r: api.Reflection{Patterns: 6}, want: "Agents hit the same problems (6 patterns since the last reflection): /reflect to review what to learn"},
		{name: "proposals", r: api.Reflection{Patterns: 6, Proposals: 1}, want: "1 change to the harness to review: /reflect"},
		{name: "the service can't say", err: errors.New("unavailable")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &stubBackend{reflection: func() (api.Reflection, error) { return tt.r, tt.err }}
			assert.Equal(t, tt.want, reflectHint(w))
		})
	}
}
