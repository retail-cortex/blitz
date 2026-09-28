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
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// Plan mode plans every prompt: tools that change anything are refused
// and the transcript records the text as typed (not "/plan …").
func TestPlanModeAppliesToEveryPrompt(t *testing.T) {
	create := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "x.txt", "content": "x"}}}}}
	w, llm := openTestWith(t, nil, create, text("here is the plan"), text("done"))
	_, err := w.SetPermissionMode("plan")
	require.NoError(t, err)
	got := w.Settings().PermissionMode
	require.Equal(t, "plan", got, "mode %q", got)
	s, _ := w.NewSession()
	var result map[string]any
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "add x.txt"}, func(e api.Event) {
		if e.ToolResult != nil {
			result = e.ToolResult.Result
		}
	})
	require.NoError(t, err)
	msg, _ := result["error"].(string)
	assert.Contains(t, msg, "plan mode", "create_file wasn't refused: %v", result)
	_, err = os.Stat(filepath.Join(w.Dir(), "x.txt"))
	assert.Error(t, err, "the file was created in plan mode")
	first := llm.Requests[0].Contents
	assert.Contains(t, first[len(first)-1].Parts[0].Text, "plan-only mode", "the prompt wasn't wrapped as a plan")
	got = w.storage.Active().Messages[0].Content
	assert.Equal(t, "add x.txt", got, "transcript recorded %q", got)
	assert.Equal(t, "here is the plan", res.Output, "output %q", res.Output)

	// Back to default: the next prompt runs normally.
	w.SetPermissionMode("default")
	w.Run(context.Background(), s.ID, api.Turn{Text: "go"}, func(api.Event) {})
	last := llm.Requests[2].Contents
	assert.NotContains(t, last[len(last)-1].Parts[0].Text, "plan-only mode", "default mode still planned")
}

func TestSetPermissionModeErrors(t *testing.T) {
	w, _ := openTestWith(t, nil)
	_, err := w.SetPermissionMode("yolo")
	assert.ErrorIs(t, err, api.ErrUnknownMode, "unknown mode: %v", err)
	m, err := w.SetPermissionMode("acceptEdits")
	assert.NoError(t, err, "accept-edits: %q", m)
	assert.Equal(t, "accept-edits", m, "accept-edits: %q %v", m, err)
}
