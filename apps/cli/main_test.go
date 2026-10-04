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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestResolvePrompt(t *testing.T) {
	stdin := func(s string) *strings.Reader { return strings.NewReader(s) }
	cases := []struct {
		name        string
		flag        string
		args        []string
		tty         bool
		interactive bool
		in          string
		want        string
		wantStdin   bool
		wantErr     bool
	}{
		{name: "flag", flag: "fix it", tty: true, want: "fix it"},
		{name: "args", args: []string{"fix", "it"}, tty: true, want: "fix it"},
		{name: "dash reads stdin", flag: "-", tty: true, in: "from stdin\n", want: "from stdin", wantStdin: true},
		{name: "piped stdin", in: "piped", want: "piped", wantStdin: true},
		{name: "args frame piped", args: []string{"review", "this"}, in: "diff text\n", want: "review this\n\ndiff text", wantStdin: true},
		{name: "args with empty pipe", args: []string{"hello"}, in: "", want: "hello", wantStdin: true},
		{name: "empty pipe no args", in: "  \n", wantErr: true, wantStdin: true},
		{name: "tty no prompt = REPL", tty: true, want: ""},
		{name: "interactive ignores pipe", interactive: true, in: "x", want: ""},
		{name: "empty dash", flag: "-", in: "  ", wantErr: true, wantStdin: true},
		{name: "flag and args", flag: "a", args: []string{"b"}, tty: true, wantErr: true},
	}
	for _, c := range cases {
		got, used, err := resolvePrompt(c.flag, c.args, c.tty, c.interactive, stdin(c.in))
		if !assert.Equal(t, c.wantErr, err != nil, "%s: err = %v", c.name, err) {
			continue
		}
		assert.False(t, err == nil && got != c.want, "%s: prompt = %q, want %q", c.name, got, c.want)
		assert.False(t, err == nil && used != c.wantStdin, "%s: stdinUsed = %v, want %v", c.name, used, c.wantStdin)
	}
	// "-" with args: args frame the piped content.
	got, _, _ := resolvePrompt("-", []string{"summarize"}, true, false, stdin("body"))
	assert.Equal(t, "summarize\n\nbody", got, "dash with args = %q", got)
}

func TestExitCodes(t *testing.T) {
	cases := map[int]error{
		exitOK:          nil,
		exitFailure:     errors.New("boom"),
		exitUsage:       withCode(exitUsage, errors.New("bad flag")),
		exitMaxTurns:    api.ErrMaxTurns,
		exitInterrupted: context.Canceled,
		exitBlocked:     withCode(exitBlocked, errors.New("hook")),
	}
	for want, err := range cases {
		got := exitCodeFor(err)
		assert.Equal(t, want, got, "exitCodeFor(%v) = %d, want %d", err, got, want)
	}
	assert.NoError(t, withCode(3, nil), "withCode(nil) must be nil")
}

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	stopPrivate() // as main does
	return out.String(), err
}

func TestRootFlagValidation(t *testing.T) {
	isolate(t)
	for name, args := range map[string][]string{
		"unknown flag":   {"--nope"},
		"bad format":     {"--output-format", "xml", "hi"},
		"json no prompt": {"--output-format", "json", "-i"},
		"negative turns": {"--max-turns", "-1", "hi"},
		"bad dir":        {"-d", "/definitely/not/here", "hi"},
		"plan no prompt": {"--plan", "-i"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runCLI(t, args...)
			assert.Equal(t, exitUsage, exitCodeFor(err), "%s: exit code %d (%v), want %d", name, exitCodeFor(err), err, exitUsage)
		})
	}
	out, err := runCLI(t, "config", "path")
	assert.NoError(t, err, "config path: %q", out)
	assert.True(t, strings.HasSuffix(strings.TrimSpace(out), ".env.toml"), "config path: %q %v", out, err)
}

// --dir names the workspace; the process's working directory stays put.
func TestDirFlagDoesNotChangeTheWorkingDirectory(t *testing.T) {
	isolate(t)
	before, _ := os.Getwd()
	ws := t.TempDir()
	cfg, err := loadConfig(&globalFlags{dir: ws})
	require.NoError(t, err)
	after, _ := os.Getwd()
	assert.Equal(t, before, after, "working directory changed to %s", after)
	assert.Equal(t, ws, cfg.Tools.WorkspaceDir, "workspace %q, want %q", cfg.Tools.WorkspaceDir, ws)
	file := filepath.Join(ws, "f")
	os.WriteFile(file, nil, 0o600)
	_, err = loadConfig(&globalFlags{dir: file})
	assert.Equal(t, exitUsage, exitCodeFor(err), "a file as --dir: %v", err)
}

// isolate points HOME and config at temp dirs, and hides API keys.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MODENV_PREFIX", "")
	// No real model: a key in the developer's environment would make tests
	// call the provider (slowly, and billed). Tests that need a key set one.
	for _, k := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "LLM_PROVIDER"} {
		t.Setenv(k, "")
	}
	t.Chdir(t.TempDir())
	useTestServices(t)
	return home
}

// useTestServices gives the test a socket of its own, with no service on
// it, and starts the services the CLI asks for (the shared one, a run's
// own) in this process, stopping them when the test ends.
func useTestServices(t *testing.T) {
	t.Helper()
	short := func() string { // socket paths must be short
		d, err := os.MkdirTemp("/tmp", "bt")
		require.NoError(t, err)
		t.Cleanup(func() { os.RemoveAll(d) })
		return filepath.Join(d, "s.sock")
	}
	t.Setenv("BLITZ_SOCKET", short())
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	run := func(sock string, serve func(context.Context, string) error) error {
		done := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(done)
			serve(ctx, sock)
		}()
		return waitService(context.Background(), sock, done)
	}
	oldShared, oldPrivate, oldDirs := startShared, startPrivate, serviceDirs
	serviceDirs = func() []string { return nil } // PATH only: not the runfiles' real blitzd
	startShared = func(_ context.Context, sock string) error { return run(sock, servicetest.Run) }
	startPrivate = func(_ context.Context, o serviceOptions) (string, error) {
		sock := short()
		ro := servicetest.RunOverrides{Model: o.Model, Agent: o.Agent, Agency: o.Agency, PluginDirs: o.PluginDirs, AddDirs: o.AddDirs, SessionDir: o.SessionDir, TrustProject: o.TrustProject, AppendSystemPrompt: o.AppendSystemPrompt}
		pctx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(done)
			servicetest.RunWith(pctx, sock, o.Config, ro)
		}()
		keepPrivate(func() { stop(); <-done })
		return sock, waitService(context.Background(), sock, done)
	}
	t.Cleanup(func() {
		cancel()
		wg.Wait()
		startShared, startPrivate, serviceDirs = oldShared, oldPrivate, oldDirs
	})
}

func TestConfigInitAndShow(t *testing.T) {
	home := isolate(t)
	t.Setenv("GEMINI_API_KEY", "AIzaSyTESTKEY-1234567890abcdefghijklmnop")
	_, err := runCLI(t, "config", "init")
	require.NoError(t, err)
	path := filepath.Join(home, ".blitz", ".env.toml")
	info, err := os.Stat(path)
	require.NoError(t, err, "template not written owner-only: %v", info)
	require.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "template not written owner-only: %v %v", info, err)
	// Negative: refuses to overwrite without --force.
	_, overwriteErr := runCLI(t, "config", "init")
	assert.Equal(t, exitUsage, exitCodeFor(overwriteErr), "overwriting without --force is a usage error: %v", overwriteErr)
	_, forceErr := runCLI(t, "config", "init", "--force")
	assert.NoError(t, forceErr, "--force")
	// The template is valid config.
	cfg, err := config.Load("")
	require.NoError(t, err, "template does not load")
	assert.Equal(t, "auto", cfg.Sandbox.Shell, "template values not applied: %+v", cfg.Sandbox)
	assert.True(t, cfg.Memory.Enabled, "template values not applied: %+v", cfg.Sandbox)
	out, err := runCLI(t, "config", "show")
	require.NoError(t, err)
	assert.NotContains(t, out, "TESTKEY-1234567890", "config show leaked or failed to mask the key:\n%s", out)
	assert.Contains(t, out, "AIz…mnop", "config show leaked or failed to mask the key:\n%s", out)
}

// testEnv opens a workspace around a mock model.
func testEnv(t *testing.T, responses ...*genai.Content) *engine.Workspace {
	return testEnvWith(t, nil, responses...)
}

func testEnvWith(t *testing.T, mutate func(*config.Config), responses ...*genai.Content) *engine.Workspace {
	t.Helper()
	isolate(t)
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Session.StorageDir = t.TempDir()
	cfg.Tools.ApprovalsFile = filepath.Join(t.TempDir(), "a.json")
	cfg.Blitz.AutoApprove = true
	if mutate != nil {
		mutate(cfg)
	}
	llm := runtime.NewMockLLM("gemini-3.8-flash", responses...)
	llm.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 10}
	e, err := engine.Open(context.Background(), cfg, engine.Options{Model: llm})
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })
	return e
}

func toolCall(name string, args map[string]any) *genai.Content {
	return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: name, Args: args}}}}
}

func TestOneShotJSON(t *testing.T) {
	e := testEnv(t, toolCall("list_files", map[string]any{}), genai.NewContentFromText("all done", genai.RoleModel))
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	var out bytes.Buffer
	err := runOneShot(context.Background(), e, oneShotOptions{prompt: "list", sessionID: sess.ID, format: formatJSON, stdout: &out})
	require.NoError(t, err)
	var res runResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &res), "stdout is not a single JSON object: %v\n%s", err, out.String())
	assert.Equal(t, "result", res.Type, "result %+v", res)
	assert.Equal(t, "all done", res.Result, "result %+v", res)
	assert.False(t, res.IsError, "result %+v", res)
	assert.Equal(t, 0, res.ExitCode, "result %+v", res)
	assert.Equal(t, sess.ID, res.SessionID, "result %+v", res)
	assert.Len(t, res.ToolCalls, 1, "tool calls %+v", res.ToolCalls)
	assert.Equal(t, "list_files", res.ToolCalls[0].Name, "tool calls %+v", res.ToolCalls)
	assert.NotNil(t, res.ToolCalls[0].Result, "tool calls %+v", res.ToolCalls)
	assert.Equal(t, 2, res.Usage.ModelCalls, "usage %+v cost %v", res.Usage, res.CostUSD)
	assert.Equal(t, int64(200), res.Usage.InputTokens, "usage %+v cost %v", res.Usage, res.CostUSD)
	assert.NotNil(t, res.CostUSD, "usage %+v cost %v", res.Usage, res.CostUSD)
}

func TestOneShotStreamJSONAndMaxTurns(t *testing.T) {
	loop := []*genai.Content{}
	for i := 0; i < 5; i++ {
		loop = append(loop, toolCall("list_files", map[string]any{}))
	}
	e := testEnv(t, loop...)
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	var out bytes.Buffer
	err := runOneShot(context.Background(), e, oneShotOptions{prompt: "loop", sessionID: sess.ID, format: formatStreamJSON, maxTurns: 2, stdout: &out})
	require.Equal(t, exitMaxTurns, exitCodeFor(err), "expected max-turns exit, got %v", err)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var types []string
	for _, l := range lines {
		t.Run(l, func(t *testing.T) {
			var m map[string]any
			require.NoError(t, json.Unmarshal([]byte(l), &m), "line is not JSON: %q", l)
			types = append(types, m["type"].(string))
		})
	}
	assert.Equal(t, "session", types[0], "event types %v", types)
	assert.Equal(t, "result", types[len(types)-1], "event types %v", types)
	assert.Contains(t, strings.Join(types, ","), "tool_call,tool_result", "event types %v", types)
	var res runResult
	json.Unmarshal([]byte(lines[len(lines)-1]), &res)
	assert.True(t, res.IsError, "final result %+v", res)
	assert.Equal(t, exitMaxTurns, res.ExitCode, "final result %+v", res)
}

func TestOneShotPromptHookBlocks(t *testing.T) {
	e := testEnvWith(t, func(c *config.Config) {
		c.Hooks.PromptSubmit = []config.HookConfig{{Command: `echo "no secrets in prompts" >&2; exit 2`}}
	}, genai.NewContentFromText("should not run", genai.RoleModel))
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	var out bytes.Buffer
	err := runOneShot(context.Background(), e, oneShotOptions{prompt: "my password is x", sessionID: sess.ID, format: formatJSON, stdout: &out})
	assert.Equal(t, exitBlocked, exitCodeFor(err), "expected blocked exit, got %v", err)
	assert.Contains(t, err.Error(), "no secrets in prompts", "expected blocked exit, got %v", err)
	assert.Contains(t, out.String(), `"is_error":true`, "result should report the block: %s", out.String())
}

func TestMaskSecret(t *testing.T) {
	for in, want := range map[string]string{"": "", "short": "*****", "sk-abcdefghijklmnop": "sk-…mnop"} {
		t.Run(in, func(t *testing.T) {
			got := maskSecret(in)
			assert.Equal(t, want, got, "maskSecret(%q) = %q, want %q", in, got, want)
		})
	}
}

func TestOneShotFailsWithoutModel(t *testing.T) {
	home := isolate(t)
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	os.MkdirAll(filepath.Join(home, ".blitz"), 0o700)
	os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[llm]\nprovider = \"nonexistent\"\n"), 0o600)
	_, err := runCLI(t, "--output-format", "json", "hello")
	assert.Equal(t, exitFailure, exitCodeFor(err), "expected model failure, got %v", err)
	assert.Contains(t, err.Error(), "model initialization failed", "expected model failure, got %v", err)
}

func TestOneShotPlanRefusesEdits(t *testing.T) {
	e := testEnv(t,
		toolCall("create_file", map[string]any{"path": "x.txt", "content": "x"}),
		genai.NewContentFromText("1. make x.txt", genai.RoleModel))
	sess, _ := e.Storage().CreateSession("", "t", "blitz")
	var out bytes.Buffer
	require.NoError(t, runOneShot(context.Background(), e, oneShotOptions{prompt: "add x.txt", sessionID: sess.ID, format: formatJSON, plan: true, stdout: &out}))
	_, err := os.Stat(filepath.Join(e.Tools().Workspace().Dir(), "x.txt"))
	require.Error(t, err, "--plan created a file")
	var res runResult
	err = json.Unmarshal(out.Bytes(), &res)
	require.NoError(t, err, "result %+v", res)
	require.Equal(t, "1. make x.txt", res.Result, "result %+v %v", res, err)
	toolErr, _ := res.ToolCalls[0].Result["error"].(string)
	require.Contains(t, toolErr, "plan mode", "create_file result %+v", res.ToolCalls[0])
}

// exec runs one prompt, like `blitz <prompt>`, and never the REPL.
func TestExecCommand(t *testing.T) {
	isolate(t) // no API key: the model can't be built
	ws := t.TempDir()
	_, err := runCLIWithInput(t, "", "-d", ws, "exec")
	assert.Equal(t, exitUsage, exitCodeFor(err), "exec without a prompt: %v", err)
	assert.Contains(t, err.Error(), "no prompt", "exec without a prompt: %v", err)
	// With a prompt it's a one-shot run in the -d workspace: here it fails
	// on the missing model, which only a one-shot run reports as an error.
	_, err = runCLI(t, "-d", ws, "exec", "--output-format", "json", "hello")
	assert.Equal(t, exitFailure, exitCodeFor(err), "exec with a prompt: %v", err)
	assert.Contains(t, err.Error(), "model initialization failed", "exec with a prompt: %v", err)
	_, err = runCLI(t, "exec", "--interactive", "hi")
	assert.Equal(t, exitUsage, exitCodeFor(err), "exec has no --interactive: %v", err)
}

func TestLicenseCommand(t *testing.T) {
	isolate(t)
	for args, want := range map[string]string{
		"license":             "The full license:    blitz license full",
		"license full":        "Apache License",
		"license third-party": "THIRD-PARTY NOTICES",
	} {
		t.Run(args, func(t *testing.T) {
			out, err := runCLI(t, strings.Fields(args)...)
			assert.NoError(t, err, "%s: %v, no %q in %.200s", args, err, want, out)
			assert.Contains(t, out, want, "%s: %v, no %q in %.200s", args, err, want, out)
		})
	}
	_, err := runCLI(t, "license", "bogus")
	assert.Equal(t, exitUsage, exitCodeFor(err), "bogus: %v", err)
}
