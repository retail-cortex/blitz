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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadReply(t *testing.T) {
	tests := []struct {
		name, event, text string
		want              Outcome
		msg               string
	}{
		{name: "block", event: "pre_tool", text: `{"decision":"block"}`, want: Outcome{Blocked: true, Reason: "blocked by hook"}},
		{name: "deny on a tool is a block", event: "pre_tool", text: `{"decision":"deny","reason":"no"}`, want: Outcome{Blocked: true, Reason: "no"}},
		{name: "deny on a permission request", event: "permission_request", text: `{"decision":"Deny"}`, want: Outcome{Decision: "deny"}},
		{name: "allow with new arguments", event: "pre_tool", text: `{"decision":"allow","updated_args":{"path":"b"}}`, want: Outcome{Decision: "allow", UpdatedArgs: map[string]any{"path": "b"}}},
		{name: "ask", event: "pre_tool", text: `{"decision":"ask","reason":"why"}`, want: Outcome{Decision: "ask", Reason: "why"}},
		{name: "context and a message", event: "stop", text: `{"continue":true,"additional_context":"c","system_message":" hi "}`, want: Outcome{Continue: true, Context: "c"}, msg: "hi"},
		{name: "plain text is context at session start", event: "session_start", text: "tabs", want: Outcome{Context: "tabs"}},
		{name: "and nothing elsewhere", event: "pre_tool", text: "tabs", want: Outcome{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, msg, err := readReply(tt.event, tt.text)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.msg, msg)
		})
	}
}

func TestHookValidation(t *testing.T) {
	tests := []struct {
		name    string
		hook    config.HookConfig
		wantErr string
	}{
		{name: "command", hook: config.HookConfig{Command: "true"}},
		{name: "args", hook: config.HookConfig{Args: []string{"true"}}},
		{name: "http", hook: config.HookConfig{Type: "http", URL: "https://hooks.example/x"}},
		{name: "prompt", hook: config.HookConfig{Type: "prompt", Prompt: "no rm -rf"}},
		{name: "nothing to run", hook: config.HookConfig{}, wantErr: "needs command or args"},
		{name: "both", hook: config.HookConfig{Command: "a", Args: []string{"b"}}, wantErr: "use one"},
		{name: "http without a url", hook: config.HookConfig{Type: "http", URL: "ftp://x"}, wantErr: "http(s) url"},
		{name: "prompt without a prompt", hook: config.HookConfig{Type: "prompt"}, wantErr: "needs a prompt"},
		{name: "unknown type", hook: config.HookConfig{Type: "carrier-pigeon"}, wantErr: "unknown type"},
		{name: "bad if", hook: config.HookConfig{Command: "true", If: "teleport(x)"}, wantErr: "if:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewScriptHooks(config.HooksConfig{PreTool: []config.HookConfig{tt.hook}}, nil, t.TempDir(), nil)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestHookIf(t *testing.T) {
	tests := []struct {
		rule, tool string
		args       map[string]any
		want       bool
	}{
		{"shell(git push *)", "run_shell_command", map[string]any{"command": "git push origin main"}, true},
		{"shell(git push *)", "run_shell_command", map[string]any{"command": "git status"}, false},
		{"write(*.go)", "create_file", map[string]any{"path": "main.go"}, true},
		{"write(*.go)", "read_file", map[string]any{"path": "main.go"}, false},
		{"delete(tmp/*)", "delete_file", map[string]any{"path": "tmp/x"}, true},
		{"read(*.env)", "grep", map[string]any{"path": "prod.env"}, true},
		{"web(*.example.com)", "web_fetch", map[string]any{"url": "https://api.example.com/v1"}, true},
		{"web(*.example.com)", "browser", map[string]any{"action": "read"}, false},
		{"skill(pdf)", "activate_skill", map[string]any{"skill_name": "pdf"}, true},
		{"agent(qa)", "invoke_agent", map[string]any{"agent_name": "qa"}, true},
		{"web_search", "web_search", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.rule+" "+tt.tool, func(t *testing.T) {
			h, _ := newHooks(t, config.HooksConfig{PreTool: []config.HookConfig{{If: tt.rule, Command: "true"}}})
			assert.Equal(t, tt.want, h.anyApplies("pre_tool", tt.tool, tt.args))
		})
	}
}

func TestHTTPAndPromptHooks(t *testing.T) {
	var got struct {
		auth, event string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.auth = r.Header.Get("Authorization")
		var ev map[string]any
		json.Unmarshal(body, &ev)
		got.event, _ = ev["event"].(string)
		if ev["tool"] == "delete_file" {
			http.Error(w, "down for maintenance", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"decision":"block","reason":"from the web","system_message":"checked by the policy server"}`)
	}))
	defer srv.Close()
	t.Setenv("HOOK_TOKEN", "t0ken")
	t.Setenv("OTHER_SECRET", "nope")

	h, warnings := newHooks(t, config.HooksConfig{
		PreTool: []config.HookConfig{
			{Match: "run_shell_command", Type: "http", URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer $HOOK_TOKEN $OTHER_SECRET"}, AllowedEnvVars: []string{"HOOK_TOKEN"}},
			{Match: "delete_file", Type: "http", URL: srv.URL},
			{Match: "read_file", Type: "prompt", Prompt: "never read .env files", Model: "gemini/judge"},
		},
	})
	var shown []string
	h.Notify = func(_ context.Context, m string) { shown = append(shown, m) }
	ctx := context.Background()

	out := h.PreTool(ctx, "s", "run_shell_command", map[string]any{"command": "ls"})
	assert.Equal(t, Outcome{Blocked: true, Reason: "from the web"}, out)
	assert.Equal(t, "Bearer t0ken ${OTHER_SECRET}", got.auth, "only allowed variables are put in headers")
	assert.Equal(t, "pre_tool", got.event)
	assert.Equal(t, []string{"checked by the policy server"}, shown)

	out = h.PreTool(ctx, "s", "delete_file", nil)
	assert.False(t, out.Blocked, "an http failure fails open")
	require.Len(t, *warnings, 1)
	assert.Contains(t, (*warnings)[0], "HTTP 503: down for maintenance")

	out = h.PreTool(ctx, "s", "read_file", nil)
	assert.False(t, out.Blocked)
	assert.Contains(t, (*warnings)[1], "no model to judge")
	var judged []string
	h.Judge = func(_ context.Context, model, prompt string, event []byte) (string, error) {
		judged = append(judged, model+": "+prompt)
		return `{"decision":"ask","reason":"it might be .env"}`, nil
	}
	out = h.PreTool(ctx, "s", "read_file", map[string]any{"path": ".env"})
	assert.Equal(t, Outcome{Decision: "ask", Reason: "it might be .env"}, out)
	assert.Equal(t, []string{"gemini/judge: never read .env files"}, judged)

	// /hooks lists them, with their failures.
	var failures []string
	for _, info := range h.List() {
		for _, f := range info.Failures {
			failures = append(failures, info.Runs+": "+f.Error)
		}
	}
	assert.Equal(t, []string{"POST " + srv.URL + ": HTTP 503: down for maintenance", "prompt: never read .env files: no model to judge prompt hooks"}, failures)
}

// Later pre_tool hooks see the arguments an earlier one changed.
func TestUpdatedArgsChain(t *testing.T) {
	h, _ := newHooks(t, config.HooksConfig{PreTool: []config.HookConfig{
		{Command: `echo '{"updated_args":{"command":"ls -la"}}'`},
		{If: "shell(ls -la)", Command: `echo '{"decision":"allow"}'`},
		{Command: `echo '{"updated_args":{"command":"rm -rf /"}}'`},
	}})
	out := h.PreTool(context.Background(), "s", "run_shell_command", map[string]any{"command": "ls"})
	assert.Equal(t, map[string]any{"command": "ls -la"}, out.UpdatedArgs, "the first change wins")
	assert.Equal(t, "allow", out.Decision, "the second hook matched the changed command")
}

type callCtx struct {
	context.Context
	id string
}

func (c callCtx) FunctionCallID() string { return c.id }

// A pre_tool hook's allow grants the call's approvals, but not past deny
// or ask rules.
func TestCallGrants(t *testing.T) {
	hooks, reqs := decisionHooks(api.DecisionDeny)
	rules, err := NewPermissionRules(config.PermissionsConfig{Deny: []string{"write(*.key)"}, Ask: []string{"write(*.lock)"}}, "test")
	require.NoError(t, err)
	hooks.SetRules(rules)
	ctx := callCtx{context.Background(), "call-1"}
	write := func(p string) api.ApprovalRequest {
		return api.ApprovalRequest{Tool: "create_file", Kind: api.ActionWrite, Detail: p, Targets: []string{p}}
	}
	assert.Error(t, hooks.Approve(ctx, write("a.txt")), "not granted: the user says no")
	hooks.GrantCall("call-1")
	assert.NoError(t, hooks.Approve(ctx, write("a.txt")), "granted")
	assert.Error(t, hooks.Approve(callCtx{context.Background(), "call-2"}, write("a.txt")), "another call")
	assert.ErrorContains(t, hooks.Approve(ctx, write("id.key")), "deny")
	assert.Error(t, hooks.Approve(ctx, write("go.lock")), "an ask rule still asks")
	hooks.EndCall("call-1")
	assert.Error(t, hooks.Approve(ctx, write("a.txt")))
	assert.Len(t, *reqs, 4, "asked: before the grant, the other call, the ask rule, after it ended")
	assert.Equal(t, "", CallID(context.Background()))
}
