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
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newHooks(t *testing.T, cfg config.HooksConfig) (*ScriptHooks, *[]string) {
	t.Helper()
	h, err := NewScriptHooks(cfg, nil, t.TempDir(), nil)
	require.NoError(t, err)
	var warnings []string
	h.Warn = func(s string) { warnings = append(warnings, s) }
	t.Cleanup(h.Close)
	return h, &warnings
}

func TestScriptHookBlocking(t *testing.T) {
	ctx := context.Background()
	h, _ := newHooks(t, config.HooksConfig{PreTool: []config.HookConfig{
		{Match: "run_shell_command", Command: `echo "no shell today" >&2; exit 2`},
		{Match: "delete_*", Command: `echo '{"decision":"block","reason":"deletes need review"}'`},
		{Match: "", Command: "exit 0"},
	}})
	r := h.PreTool(ctx, "s", "run_shell_command", nil)
	assert.Equal(t, "no shell today", r, "exit 2 block reason = %q", r)
	r = h.PreTool(ctx, "s", "delete_file", nil)
	assert.Equal(t, "deletes need review", r, "JSON block reason = %q", r)
	r = h.PreTool(ctx, "s", "read_file", nil)
	assert.Equal(t, "", r, "unexpected block %q", r)
}

func TestScriptHookReceivesEvent(t *testing.T) {
	out := filepath.Join(t.TempDir(), "event.json")
	h, _ := newHooks(t, config.HooksConfig{
		PostTool:     []config.HookConfig{{Command: "cat > " + out}},
		PromptSubmit: []config.HookConfig{{Command: "cat >> " + out}},
	})
	h.PostTool(context.Background(), "sess", "grep", map[string]any{"query": "x"}, map[string]any{"total_matches": 1}, nil)
	require.NoError(t, h.flush(context.Background()))
	b, _ := os.ReadFile(out)
	for _, want := range []string{`"event":"post_tool"`, `"tool":"grep"`, `"session_id":"sess"`, `"query":"x"`, `"total_matches":1`} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, string(b), want, "hook stdin missing %s: %s", want, b)
		})
	}
	r := h.PromptSubmit(context.Background(), "sess", "hello there")
	assert.Equal(t, "", r, "prompt hook blocked: %q", r)
	b, _ = os.ReadFile(out)
	assert.Contains(t, string(b), `"prompt":"hello there"`, "prompt event not delivered: %s", b)
}

func TestScriptHookFailures(t *testing.T) {
	ctx := context.Background()
	// Fail-open: error is warned about, action proceeds.
	h, warnings := newHooks(t, config.HooksConfig{PreTool: []config.HookConfig{{Command: "exit 1"}}})
	r := h.PreTool(ctx, "s", "x", nil)
	assert.Equal(t, "", r, "fail-open: reason=%q warnings=%v", r, *warnings)
	assert.Len(t, *warnings, 1, "fail-open: reason=%q warnings=%v", r, *warnings)
	// Fail-closed: error blocks.
	h, _ = newHooks(t, config.HooksConfig{PreTool: []config.HookConfig{{Command: "exit 1", FailClosed: true}}})
	r = h.PreTool(ctx, "s", "x", nil)
	assert.Contains(t, r, "fail_closed", "fail-closed reason = %q", r)
	// Timeout counts as a failure.
	h, warnings = newHooks(t, config.HooksConfig{PreTool: []config.HookConfig{{Command: "sleep 10", TimeoutSeconds: 1}}})
	r = h.PreTool(ctx, "s", "x", nil)
	assert.Equal(t, "", r, "timeout: reason=%q warnings=%v", r, *warnings)
	assert.Len(t, *warnings, 1, "timeout: reason=%q warnings=%v", r, *warnings)
	assert.Contains(t, (*warnings)[0], "timed out", "timeout: reason=%q warnings=%v", r, *warnings)
	// Invalid configs are rejected up front.
	for _, bad := range []config.HooksConfig{
		{PreTool: []config.HookConfig{{Command: " "}}},
		{PostTool: []config.HookConfig{{Match: "[", Command: "true"}}},
	} {
		_, err := NewScriptHooks(bad, nil, ".", nil)
		assert.Error(t, err, "expected error for %+v", bad)
	}
	var nilHooks *ScriptHooks
	assert.Equal(t, "", nilHooks.PreTool(ctx, "", "x", nil), "nil hooks should be a no-op")
	assert.True(t, nilHooks.Empty(), "nil hooks should be a no-op")
}
