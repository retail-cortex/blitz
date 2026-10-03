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
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/config"
)

// TestScriptHooksEmptyAndHas checks Empty and Has see the configured events.
func TestScriptHooksEmptyAndHas(t *testing.T) {
	none, _ := newHooks(t, config.HooksConfig{})
	assert.True(t, none.Empty())
	assert.False(t, none.Has("stop"))
	some, _ := newHooks(t, config.HooksConfig{Stop: []config.HookConfig{{Command: "true"}}})
	assert.False(t, some.Empty())
	assert.True(t, some.Has("stop"))

	var nilHooks *ScriptHooks
	ctx := context.Background()
	assert.False(t, nilHooks.Has("stop"))
	assert.Empty(t, nilHooks.PromptSubmit(ctx, "s", "p"))
	assert.Equal(t, Outcome{}, nilHooks.PromptSubmitContext(ctx, "s", "p"))
	assert.Equal(t, Outcome{}, nilHooks.Run(ctx, "stop", "", HookEvent{}))
	assert.Nil(t, nilHooks.List())
	nilHooks.PostTool(ctx, "s", "grep", nil, nil, nil) // a no-op
}

// TestPostToolFailureHooks checks a failed tool call (an error, or an error
// in its result) also fires post_tool_failure hooks with the message.
func TestPostToolFailureHooks(t *testing.T) {
	for name, tc := range map[string]struct {
		result  map[string]any
		toolErr error
		want    string
	}{
		"tool error":   {nil, errors.New("boom"), "boom"},
		"result error": {map[string]any{"error": "no such file"}, nil, "no such file"},
	} {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "events.jsonl")
			h, _ := newHooks(t, config.HooksConfig{
				PostTool:        []config.HookConfig{{Command: "cat >> " + out + "; echo >> " + out}},
				PostToolFailure: []config.HookConfig{{Command: "cat >> " + out + "; echo >> " + out}},
			})
			h.PostTool(context.Background(), "s", "read_file", nil, tc.result, tc.toolErr)
			require.NoError(t, h.flush(context.Background()))
			evs := readHookEvents(t, out)
			require.Len(t, evs, 2)
			assert.Equal(t, "post_tool_failure", evs[1].Event)
			assert.Equal(t, tc.want, evs[1].Error)
		})
	}
}

// TestHookEventsCarrySessionContext checks the prompt ID and the Info
// callback's context reach the hook's event.
func TestHookEventsCarrySessionContext(t *testing.T) {
	out := filepath.Join(t.TempDir(), "event.json")
	h, _ := newHooks(t, config.HooksConfig{SessionStart: []config.HookConfig{{Command: "cat > " + out}}})
	h.Info = func(_ context.Context, session string) HookInfo {
		return HookInfo{PromptID: "from-info", TranscriptPath: "/t/" + session, PermissionMode: "plan", Agent: "qa"}
	}
	h.Run(context.Background(), "session_start", "", HookEvent{SessionID: "s1"})
	evs := readHookEvents(t, out)
	require.Len(t, evs, 1)
	assert.Equal(t, "from-info", evs[0].PromptID)
	assert.Equal(t, "/t/s1", evs[0].TranscriptPath)
	assert.Equal(t, "qa", evs[0].Agent)

	h.Run(WithPromptID(context.Background(), "p-7"), "session_start", "", HookEvent{SessionID: "s1"})
	evs = readHookEvents(t, out)
	require.Len(t, evs, 1)
	assert.Equal(t, "p-7", evs[0].PromptID, "the turn's prompt ID wins over Info's")
}

// TestUnencodableHookEvents checks an event that can't be encoded fails each
// synchronous hook, and is warned about and dropped for background ones.
func TestUnencodableHookEvents(t *testing.T) {
	h, warnings := newHooks(t, config.HooksConfig{
		PreTool:  []config.HookConfig{{Command: "true"}},
		PostTool: []config.HookConfig{{Command: "true"}},
	})
	bad := map[string]any{"f": func() {}}
	h.PreTool(context.Background(), "s", "grep", bad)
	h.PostTool(context.Background(), "s", "grep", bad, nil, nil)
	require.Len(t, *warnings, 2)
	assert.Contains(t, (*warnings)[0], "could not be encoded")
	assert.Contains(t, (*warnings)[1], "could not be encoded")
}

// TestHookRepliesCombine checks a stop hook's continue and the context of
// several hooks are combined.
func TestHookRepliesCombine(t *testing.T) {
	h, _ := newHooks(t, config.HooksConfig{Stop: []config.HookConfig{
		{Command: `echo '{"continue":true,"reason":"tests fail","additional_context":"first"}'`},
		{Command: `echo '{"continue":true,"reason":"later","additional_context":"second"}'`},
	}})
	out := h.Run(context.Background(), "stop", "", HookEvent{})
	assert.True(t, out.Continue)
	assert.Equal(t, "tests fail", out.Reason, "the first reason")
	assert.Equal(t, "first\n\nsecond", out.Context)
}

// TestCommandHookForms checks an args hook runs without a shell and an exit 2
// with nothing on stderr still blocks, with a default reason.
func TestCommandHookForms(t *testing.T) {
	h, _ := newHooks(t, config.HooksConfig{PreTool: []config.HookConfig{{Args: []string{"sh", "-c", "exit 2"}}}})
	out := h.PreTool(context.Background(), "s", "grep", nil)
	assert.True(t, out.Blocked)
	assert.Equal(t, "blocked by hook", out.Reason)
}

// TestHTTPHookUnreachable checks an http hook whose server is gone fails.
func TestHTTPHookUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	h, warnings := newHooks(t, config.HooksConfig{Stop: []config.HookConfig{{Type: "http", URL: url}}})
	h.Run(context.Background(), "stop", "", HookEvent{})
	require.Len(t, *warnings, 1)
}

// TestHookFailuresKeepTheLatest checks only the last few failures of a hook
// are listed.
func TestHookFailuresKeepTheLatest(t *testing.T) {
	h, _ := newHooks(t, config.HooksConfig{Stop: []config.HookConfig{{Command: "exit 1"}}})
	for range maxHookFailures + 2 {
		h.Run(context.Background(), "stop", "", HookEvent{})
	}
	list := h.List()
	require.Len(t, list, 1)
	assert.Len(t, list[0].Failures, maxHookFailures)
}

// TestFlushAfterCloseOrCancel checks flush returns at once after Close, and
// with the context's error when it's cancelled first.
func TestFlushAfterCloseOrCancel(t *testing.T) {
	h, _ := newHooks(t, config.HooksConfig{PostTool: []config.HookConfig{{Command: "sleep 1"}}})
	h.PostTool(context.Background(), "s", "grep", nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, h.flush(ctx), context.Canceled)
	h.Close()
	assert.NoError(t, h.flush(context.Background()))
}

// TestHTTPHookReplyCutShort checks a reply that ends before its length is a
// failure, so a fail_closed hook blocks rather than letting the tool run.
func TestHTTPHookReplyCutShort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"decision":`))
	}))
	t.Cleanup(srv.Close)
	h, _ := newHooks(t, config.HooksConfig{PreTool: []config.HookConfig{{Type: "http", URL: srv.URL, FailClosed: true}}})
	out := h.PreTool(context.Background(), "s", "grep", nil)
	assert.True(t, out.Blocked, "a fail_closed hook whose reply was cut short")
	assert.Contains(t, out.Reason, "fail_closed")
}
