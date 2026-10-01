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
	"context"
	"errors"
	"testing"

	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// invoke_agent with background starts a task (in a worktree when asked)
// and says where it works; isolation other than a background worktree,
// a session without tasks, and a run nobody watches are refused.
func TestInvokeAgentBackground(t *testing.T) {
	reg, err := agents.NewRegistry()
	require.NoError(t, err)
	tests := []struct {
		name      string
		args      map[string]any
		ctx       context.Context
		noRunner  bool
		startErr  error
		wantErr   string
		wantText  string
		wantStart []string
	}{
		{"started", map[string]any{"background": true}, context.Background(), false, nil, "", "Started in the background as task-9.", []string{"helios", "build", ""}},
		{"in a worktree", map[string]any{"background": true, "isolation": "worktree"}, context.Background(), false, nil, "", "on the branch blitz/task-9", []string{"helios", "build", "worktree"}},
		{"unknown isolation", map[string]any{"background": true, "isolation": "container"}, context.Background(), false, nil, `isolation "container": only worktree`, "", nil},
		{"isolation in the foreground", map[string]any{"isolation": "worktree"}, context.Background(), false, nil, "isolation needs background: true", "", nil},
		{"no runner", map[string]any{"background": true}, context.Background(), true, nil, "background tasks are not available", "", nil},
		{"from a background task", map[string]any{"background": true}, Background(context.Background()), false, nil, "can't be started from a background task", "", nil},
		{"start fails", map[string]any{"background": true}, context.Background(), false, errors.New("too many tasks"), "too many tasks", "", []string{"helios", "build", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hooks := NewHooks(Policy{})
			runner := &fakeTasks{startErr: tt.startErr}
			if !tt.noRunner {
				hooks.SetTaskRunner(runner)
			}
			rt := toolOf(t)(NewInvokeAgentTool(reg, hooks))
			args := map[string]any{"agent_name": "helios", "prompt": "build"}
			for k, v := range tt.args {
				args[k] = v
			}
			out, err := rt.Run(createTestToolContextWith(tt.ctx), args)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStart, runner.started)
			if tt.wantErr != "" {
				assert.Contains(t, errOf(out), tt.wantErr)
				return
			}
			assert.Empty(t, errOf(out))
			assert.Equal(t, "task-9", out["task_id"])
			assert.Equal(t, "running", out["status"])
			assert.Contains(t, out["response"], tt.wantText)
		})
	}
}
