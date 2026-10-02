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
	"path/filepath"
	"strings"
	"testing"

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
