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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUniversalConstructorNameValidation(t *testing.T) {
	base := t.TempDir()
	ucDir := filepath.Join(base, "uc")
	rt := toolOf(t)(NewUniversalConstructorTool(ucDir, allowAll(), nil, nil))

	// Negative: traversal and separator names are rejected and nothing is written.
	for _, name := range []string{"../evil", "a/b", "..", ".hidden", "sp ace", strings.Repeat("x", 65)} {
		out := runTool(t, rt, map[string]any{"action": "create", "tool_name": name, "code": "echo hi"})
		assert.NotEqual(t, "", errOf(out), "expected rejection for tool_name %q", name)
	}
	_, err := os.Stat(filepath.Join(base, "evil.sh"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "traversal wrote outside the tools directory")
	// Negative: unsupported language and missing code.
	out := runTool(t, rt, map[string]any{"action": "create", "tool_name": "x", "code": "1", "language": "ruby"})
	assert.NotEqual(t, "", errOf(out), "expected unsupported language error")
	out = runTool(t, rt, map[string]any{"action": "create", "tool_name": "x"})
	assert.NotEqual(t, "", errOf(out), "expected missing code error")
	out = runTool(t, rt, map[string]any{"action": "bogus"})
	assert.NotEqual(t, "", errOf(out), "expected unknown action error")
}

func TestUniversalConstructorCreateRun(t *testing.T) {
	ucDir := filepath.Join(t.TempDir(), "uc")
	rt := toolOf(t)(NewUniversalConstructorTool(ucDir, allowAll(), nil, nil))

	out := runTool(t, rt, map[string]any{
		"action": "create", "tool_name": "count_args", "language": "bash",
		"code": "echo \"$# [$1] [$2]\"", "description": "counts args",
	})
	require.Equal(t, true, out["success"], "create failed: %v", out)
	info, err := os.Stat(filepath.Join(ucDir, "count_args.sh"))
	assert.NoError(t, err, "expected owner-only script, got %v", info)
	assert.Equal(t, fs.FileMode(0o700), info.Mode().Perm(), "expected owner-only script, got %v %v", info, err)

	// Positive: args are split into separate argv entries.
	out = runTool(t, rt, map[string]any{"action": "run", "tool_name": "count_args", "args": "one two"})
	assert.Equal(t, "2 [one] [two]", strings.TrimSpace(out["result"].(string)), "unexpected run output %v", out)

	list := runTool(t, rt, map[string]any{"action": "list"})
	tools, _ := list["tools"].([]any)
	assert.Len(t, tools, 1, "expected 1 listed tool, got %v", list)

	// Negative: running an unknown tool, and a failing tool.
	out = runTool(t, rt, map[string]any{"action": "run", "tool_name": "nope"})
	assert.NotEqual(t, "", errOf(out), "expected error for unknown tool")
	runTool(t, rt, map[string]any{"action": "create", "tool_name": "fails", "code": "exit 4"})
	out = runTool(t, rt, map[string]any{"action": "run", "tool_name": "fails"})
	assert.NotEqual(t, true, out["success"], "expected failing tool to report failure")
}

func TestUniversalConstructorApproval(t *testing.T) {
	ucDir := filepath.Join(t.TempDir(), "uc")

	denied, reqs := approverHooks(false)
	rt := toolOf(t)(NewUniversalConstructorTool(ucDir, denied, nil, nil))
	out := runTool(t, rt, map[string]any{"action": "create", "tool_name": "t1", "code": "echo hi"})
	assert.Contains(t, errOf(out), "not approved", "expected denial, got %v", out)
	_, err := os.Stat(filepath.Join(ucDir, "t1.sh"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "denied tool was written to disk")
	assert.Len(t, *reqs, 1, "approval request should show the code as a diff: %v", *reqs)
	assert.Contains(t, (*reqs)[0].Diff, "+echo hi", "approval request should show the code as a diff: %v", *reqs)

	// Create approved, run denied: separate approvals for write and execution.
	h := NewHooks(Policy{})
	h.SetApprover(func(_ context.Context, r api.ApprovalRequest) (api.Decision, error) {
		if r.Kind == api.ActionWrite {
			return api.DecisionOnce, nil
		}
		return api.DecisionDeny, nil
	})
	rt = toolOf(t)(NewUniversalConstructorTool(ucDir, h, nil, nil))
	out = runTool(t, rt, map[string]any{"action": "create", "tool_name": "t2", "code": "touch ran"})
	require.Equal(t, true, out["success"], "approved create failed: %v", out)
	out = runTool(t, rt, map[string]any{"action": "run", "tool_name": "t2"})
	assert.Contains(t, errOf(out), "not approved", "expected run to be denied, got %v", out)
}

func TestInvokeAgentTool(t *testing.T) {
	reg, err := agents.NewRegistry()
	require.NoError(t, err)
	hooks := NewHooks(Policy{})
	rt := toolOf(t)(NewInvokeAgentTool(reg, hooks))

	// Negative: no invoker wired must be an explicit error, not a fake success.
	out := runTool(t, rt, map[string]any{"agent_name": "helios", "prompt": "do it"})
	assert.Contains(t, errOf(out), "not available", "expected unavailable error, got %v", out)
	assert.Equal(t, "", out["response"], "expected unavailable error, got %v", out)
	// Negative: unknown agent and empty prompt.
	assert.NotEmpty(t, errOf(runTool(t, rt, map[string]any{"agent_name": "ghost", "prompt": "x"})), "an unknown agent is an error")
	assert.NotEmpty(t, errOf(runTool(t, rt, map[string]any{"agent_name": "helios", "prompt": "  "})), "an empty prompt is an error")

	// Positive: invoker result is returned; errors surface.
	var gotAgent, gotPrompt string
	hooks.SetSubagentInvoker(func(ctx context.Context, name, prompt string) (string, error) {
		gotAgent, gotPrompt = name, prompt
		if prompt == "fail" {
			return "", errors.New("boom")
		}
		return "done: " + prompt, nil
	})
	out = runTool(t, rt, map[string]any{"agent_name": "helios", "prompt": "build"})
	assert.Equal(t, "done: build", out["response"], "unexpected invoke result %v", out)
	assert.Equal(t, "helios", gotAgent, "unexpected invoke result %v", out)
	assert.Equal(t, "build", gotPrompt, "unexpected invoke result %v", out)
	out = runTool(t, rt, map[string]any{"agent_name": "helios", "prompt": "fail"})
	assert.Contains(t, errOf(out), "boom", "expected invoker error, got %v", out)
}

func TestListAgentsFilter(t *testing.T) {
	reg, err := agents.NewRegistry()
	require.NoError(t, err)
	rt := toolOf(t)(NewListAgentsTool(reg))
	all, _ := runTool(t, rt, map[string]any{})["agents"].([]any)
	filtered, _ := runTool(t, rt, map[string]any{"filter": "HELI"})["agents"].([]any)
	none, _ := runTool(t, rt, map[string]any{"filter": "zzz"})["agents"].([]any)
	assert.GreaterOrEqual(t, len(all), 7, "filter not applied: all=%d filtered=%d none=%d", len(all), len(filtered), len(none))
	assert.Len(t, filtered, 1, "filter not applied: all=%d filtered=%d none=%d", len(all), len(filtered), len(none))
	assert.Len(t, none, 0, "filter not applied: all=%d filtered=%d none=%d", len(all), len(filtered), len(none))
}

func TestAskUserQuestionTool(t *testing.T) {
	hooks := NewHooks(Policy{})
	rt := toolOf(t)(NewAskUserQuestionTool(hooks))

	// Negative: no prompter => explicit error instead of reading stdin directly.
	out := runTool(t, rt, map[string]any{"question": "?"})
	assert.NotEqual(t, "", errOf(out), "expected error without prompter")
	assert.NotEmpty(t, errOf(runTool(t, rt, map[string]any{"question": ""})), "an empty question is an error")

	hooks.SetUserPrompter(func(ctx context.Context, q string, opts []string) (string, error) {
		if q == "fail?" {
			return "", errors.New("eof")
		}
		return q + "->" + strings.Join(opts, "|"), nil
	})
	out = runTool(t, rt, map[string]any{"question": "pick", "options": []string{"a", "b"}})
	assert.Equal(t, "pick->a|b", out["answer"], "unexpected answer %v", out)
	out = runTool(t, rt, map[string]any{"question": "fail?"})
	assert.NotEqual(t, "", errOf(out), "expected prompter error to surface")
}

func TestUniversalConstructorPersistence(t *testing.T) {
	ucDir := filepath.Join(t.TempDir(), "uc")
	first := toolOf(t)(NewUniversalConstructorTool(ucDir, allowAll(), nil, nil))
	runTool(t, first, map[string]any{"action": "create", "tool_name": "greet", "language": "python", "code": "import sys\nprint('hi', sys.argv[1])", "description": "says hi"})
	info, err := os.Stat(filepath.Join(ucDir, "greet.json"))
	require.NoError(t, err, "manifest missing or not owner-only: %v", info)
	require.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "manifest missing or not owner-only: %v %v", info, err)

	// A new process sees and can run the tool.
	second := toolOf(t)(NewUniversalConstructorTool(ucDir, allowAll(), nil, nil))
	list := runTool(t, second, map[string]any{"action": "list"})
	tools, _ := list["tools"].([]any)
	require.Len(t, tools, 1, "persisted tool not listed: %v", list)
	require.Contains(t, tools[0].(string), "greet (python): says hi", "persisted tool not listed: %v", list)
	out := runTool(t, second, map[string]any{"action": "run", "tool_name": "greet", "args": "puppy"})
	assert.Contains(t, fmt.Sprint(out["result"]), "hi puppy", "persisted tool did not run: %v", out)

	// Delete removes script and manifest; a fresh load no longer sees it.
	out = runTool(t, second, map[string]any{"action": "delete", "tool_name": "greet"})
	require.Equal(t, true, out["success"], "delete: %v", out)
	for _, f := range []string{"greet.py", "greet.json"} {
		_, err := os.Stat(filepath.Join(ucDir, f))
		assert.ErrorIs(t, err, fs.ErrNotExist, "%s not removed", f)
	}
	third := toolOf(t)(NewUniversalConstructorTool(ucDir, allowAll(), nil, nil))
	tools, _ = runTool(t, third, map[string]any{"action": "list"})["tools"].([]any)
	assert.Len(t, tools, 0, "deleted tool reloaded: %v", tools)
	out = runTool(t, third, map[string]any{"action": "delete", "tool_name": "greet"})
	assert.NotEqual(t, "", errOf(out), "deleting a missing tool should fail")
	denied, _ := approverHooks(false)
	runTool(t, first, map[string]any{"action": "create", "tool_name": "keep", "code": "echo k"})
	guarded := toolOf(t)(NewUniversalConstructorTool(ucDir, denied, nil, nil))
	out = runTool(t, guarded, map[string]any{"action": "delete", "tool_name": "keep"})
	assert.Contains(t, errOf(out), "not approved", "delete should need approval: %v", out)
	_, err = os.Stat(filepath.Join(ucDir, "keep.sh"))
	assert.NoError(t, err, "denied delete removed the script")
}

func TestUniversalConstructorRejectsTamperedManifests(t *testing.T) {
	ucDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "evil.sh")
	writeFile(t, outside, "echo pwned")
	write := func(name, body string) { writeFile(t, filepath.Join(ucDir, name), body) }

	write("traverse.json", `{"name":"../../evil","language":"bash"}`)
	write("badlang.json", `{"name":"badlang","language":"ruby"}`)
	write("badlang.rb", "puts 1")
	write("alias.json", `{"name":"alias","language":"sh"}`) // non-canonical language
	write("alias.sh", "echo a")
	write("mismatch.json", `{"name":"other","language":"bash"}`) // name differs from file
	write("other.sh", "echo o")
	write("noscript.json", `{"name":"noscript","language":"bash"}`)
	write("garbage.json", `{not json`)
	write("link.json", `{"name":"link","language":"bash"}`)
	require.NoError(t, os.Symlink(outside, filepath.Join(ucDir, "link.sh")))
	write("good.json", `{"name":"good","language":"bash","description":"fine"}`)
	write("good.sh", "echo good")

	rt := toolOf(t)(NewUniversalConstructorTool(ucDir, allowAll(), nil, nil))
	tools, _ := runTool(t, rt, map[string]any{"action": "list"})["tools"].([]any)
	assert.Len(t, tools, 1, "only the valid manifest should load, got %v", tools)
	assert.True(t, strings.HasPrefix(tools[0].(string), "good "), "only the valid manifest should load, got %v", tools)
	out := runTool(t, rt, map[string]any{"action": "run", "tool_name": "link"})
	assert.NotEqual(t, "", errOf(out), "symlinked script must not be runnable")
}
