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
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A workflow's steps run in its mode, auto unless it says, unless that's
// looser than the workspace's, which then applies (WF-21).
func TestFlowMode(t *testing.T) {
	w := openTest(t)
	tests := []struct {
		name, workspace, flow string
		want, held            api.PermissionMode
	}{
		{name: "auto by default", workspace: "auto", want: api.ModeAuto},
		{name: "the workspace restricted", workspace: "default", want: api.ModeDefault, held: api.ModeAuto},
		{name: "stricter than the workspace", workspace: "default", flow: "dont-ask", want: api.ModeDontAsk},
		{name: "level with the workspace", workspace: "accept-edits", flow: "auto", want: api.ModeAuto},
		{name: "asking in an auto workspace", workspace: "auto", flow: "default", want: api.ModeDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := w.SetPermissionMode(tt.workspace)
			require.NoError(t, err)
			_, got, held := w.flowMode(context.Background(), flows.Flow{Mode: tt.flow})
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.held, held)
		})
	}
}

// In an auto workspace a workflow's agent step's action goes to the
// reviewer, which allows it, and the receipt says the mode; a workspace
// restricted to asking holds the workflow's auto back, and says so.
func TestFlowRunsInAuto(t *testing.T) {
	w, llm := openTestWith(t, nil,
		toolCall("create_file", map[string]any{"path": "made.txt", "content": "hi\n"}),
		text(`{"decision": "allow", "reason": "the step asked for it"}`),
		text("Made it."))
	_, err := w.SetPermissionMode("auto")
	require.NoError(t, err)
	writeFlow(t, w, "make", "nodes:\n  - {id: a, type: agent, instructions: Make the file.}\n")
	s := newSession(t, w)
	var run string
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/make", Command: true}, func(e api.Event) {
		if e.Step != nil {
			run = e.Step.Run
		}
	})
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(w.Dir(), "made.txt"))
	assert.Len(t, llm.Requests, 3, "the step, the reviewer, the step")
	r, _, err := FlowReceiptOf(w.StorageDir(), run)
	require.NoError(t, err)
	assert.Equal(t, "auto", r.Mode)

	_, err = w.SetPermissionMode("default")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "notes.md"), []byte("Notes."), 0o644))
	writeFlow(t, w, "read", "nodes:\n  - {id: a, type: read_file, path: notes.md}\n")
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/read", Command: true}, func(e api.Event) {
		if e.Step != nil {
			run = e.Step.Run
		}
	})
	require.NoError(t, err)
	r, _, err = FlowReceiptOf(w.StorageDir(), run)
	require.NoError(t, err)
	assert.Equal(t, "default", r.Mode)
	require.NotEmpty(t, r.Events)
	var said bool
	for _, e := range r.Events {
		said = said || e.Text == "held: auto: default"
	}
	assert.True(t, said, "the receipt says why: %+v", r.Events)
}
