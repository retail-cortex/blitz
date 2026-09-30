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
	"iter"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// fastSettings makes the watcher look often, for the test.
func fastSettings(t *testing.T) {
	old := settingsPoll
	settingsPoll = 10 * time.Millisecond
	t.Cleanup(func() { settingsPoll = old })
}

func writeSettings(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

// A rule written to a settings file by anyone applies at once, as do a
// project's deny rules and approvals remembered elsewhere.
func TestSettingsChangesApplyLive(t *testing.T) {
	fastSettings(t)
	w, _ := openTestWith(t, nil)
	heard := make(chan struct{}, 8)
	defer w.OnSettingsChanged(func() { heard <- struct{}{} })()
	decide := func(kind, target string) tools.Effect {
		e, _ := w.tools.Rules().Decide(kind, []string{target})
		return e
	}
	wait := func(what string, ok func() bool) {
		t.Helper()
		require.Eventually(t, ok, 5*time.Second, 10*time.Millisecond, what)
		select {
		case <-heard:
		case <-time.After(5 * time.Second):
			t.Fatalf("no listener heard of %s", what)
		}
	}
	rules, approvals := w.settingsFiles()

	writeSettings(t, rules[0], "[permissions]\ndeny = [\"write(secret/**)\"]\n")
	wait("a global deny rule", func() bool { return decide(tools.RuleWrite, "secret/a.txt") == tools.EffectDeny })

	writeSettings(t, rules[1], "[permissions]\nask = [\"write(docs/**)\"]\n")
	wait("a workspace ask rule", func() bool { return decide(tools.RuleWrite, "docs/a.md") == tools.EffectAsk })

	writeSettings(t, filepath.Join(w.Dir(), ".blitz", "settings.toml"), "[permissions]\ndeny = [\"web(*.evil.test)\"]\n")
	wait("a project deny rule", func() bool { return decide(tools.RuleWeb, "x.evil.test") == tools.EffectDeny })

	// Removed: gone at once too.
	writeSettings(t, rules[0], "[permissions]\n")
	wait("a removed rule", func() bool { return decide(tools.RuleWrite, "secret/a.txt") != tools.EffectDeny })

	// Another process remembers an approval.
	other, err := tools.OpenApprovalStore(approvals)
	require.NoError(t, err)
	require.NoError(t, other.Add("cmd:make", "make"))
	wait("an approval remembered elsewhere", func() bool { return w.tools.Hooks().Store().Has("cmd:make") })
}

// callbackLLM runs before(n) ahead of its nth model call.
type callbackLLM struct {
	*runtime.MockLLM
	before func(n int)
}

func (c *callbackLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	c.before(c.Calls())
	return c.MockLLM.GenerateContent(ctx, req, stream)
}

// A turn already running obeys a rule added while it runs, from its next
// action.
func TestRunningTurnObeysANewRule(t *testing.T) {
	fastSettings(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	create := func(p string) *genai.Content {
		return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": p, "content": "x"}}}}}
	}
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Session.StorageDir = t.TempDir()
	cfg.Tools.ApprovalsFile = filepath.Join(t.TempDir(), "a.json")
	var w *Workspace
	llm := &callbackLLM{MockLLM: runtime.NewMockLLM("gemini-3.8-flash", create("before.txt"), create("after.txt"), genai.NewContentFromText("done", genai.RoleModel))}
	llm.before = func(n int) {
		if n != 1 { // after the first file, mid-turn
			return
		}
		rules, _ := w.settingsFiles()
		writeSettings(t, rules[0], "[permissions]\ndeny = [\"write(after.txt)\"]\n")
		require.Eventually(t, func() bool {
			e, _ := w.tools.Rules().Decide(tools.RuleWrite, []string{"after.txt"})
			return e == tools.EffectDeny
		}, 5*time.Second, 10*time.Millisecond)
	}
	var err error
	w, err = Open(context.Background(), cfg, Options{Model: llm, NewModel: mockModels})
	require.NoError(t, err)
	defer w.Close()
	w.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionOnce, nil }, nil)
	s, _ := w.NewSession()
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "make two files"}, ignore)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(w.Dir(), "before.txt"))
	assert.NoFileExists(t, filepath.Join(w.Dir(), "after.txt"), "the new deny rule didn't reach the running turn")
}
