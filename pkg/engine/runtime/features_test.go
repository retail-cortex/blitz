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

package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	cpsession "github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestUsageEstimate(t *testing.T) {
	tr := NewUsageTracker(map[string]config.ModelPrice{"gemini-3.8-flash": {InputPerMTok: 1, OutputPerMTok: 10, CachedInputPerMTok: 0.1}})
	m := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1_000_000, CachedContentTokenCount: 500_000, CandidatesTokenCount: 100_000, ThoughtsTokenCount: 100_000}

	u := tr.Estimate("gemini-3.8-flash-001", m, 0) // prefix match
	want := 0.5*1 + 0.5*0.1 + 0.2*10
	assert.True(t, u.Priced, "estimate %+v, want cost %v", u, want)
	assert.LessOrEqual(t, abs(u.CostUSD-want), 1e-9, "estimate %+v, want cost %v", u, want)
	assert.Equal(t, int64(200_000), u.Output, "estimate %+v, want cost %v", u, want)
	assert.Equal(t, int64(1_000_000), u.LastPrompt, "estimate %+v, want cost %v", u, want)
	u = tr.Estimate("unknown-model", m, 0)
	assert.False(t, u.Priced, "unknown model should be unpriced: %+v", u)
	assert.Equal(t, float64(0), u.CostUSD, "unknown model should be unpriced: %+v", u)

	tr.Record("s", "gemini-3.8-flash", m)
	tr.Record("s", "gemini-3.8-flash", &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10})
	s := tr.Session("s")
	assert.Equal(t, 2, s.Calls, "accumulated %+v", s)
	assert.Equal(t, int64(10), s.LastPrompt, "accumulated %+v", s)
	assert.Equal(t, int64(1_000_010), s.Input, "accumulated %+v", s)
	tr.Record("s", "mystery", m)
	assert.False(t, tr.Session("s").Priced, "session with an unpriced call must be marked unpriced")
	z := tr.Session("none")
	assert.Equal(t, 0, z.Calls, "empty session %+v", z)
	assert.True(t, z.Priced, "empty session %+v", z)
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

type fixtureOpts struct {
	cfg  func(*config.Config)
	opts []Option
}

func newEngineWith(t *testing.T, fo fixtureOpts, responses ...*genai.Content) engineFixture {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.UCToolsDir = t.TempDir()
	cfg.Images.Dir = t.TempDir()
	cfg.Tools.ApprovalsFile = filepath.Join(t.TempDir(), "approvals.json")
	cfg.Blitz.AutoApprove = true
	if fo.cfg != nil {
		fo.cfg(cfg)
	}
	agentReg, _ := agents.NewRegistry()
	skillProv, _ := skills.NewProvider()
	toolReg, err := tools.NewRegistry(cfg, agentReg, skillProv)
	require.NoError(t, err)
	t.Cleanup(func() { toolReg.Close() })
	llm := NewMockLLM("gemini-3.8-flash", responses...)
	eng, err := NewEngine(context.Background(), cfg, agentReg, skillProv, toolReg, llm, fo.opts...)
	require.NoError(t, err)
	return engineFixture{eng: eng, llm: llm, tools: toolReg, cfg: cfg}
}

func toolCall(name string, args map[string]any) *genai.Content {
	return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: name, Args: args}}}}
}

func functionResponses(t *testing.T, eng *Engine, sid, prompt string, opts ...ExecOption) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	err := eng.Execute(context.Background(), sid, prompt, func(ev *session.Event) error {
		if ev.Content != nil {
			for _, p := range ev.Content.Parts {
				if p.FunctionResponse != nil {
					out[p.FunctionResponse.Name] = p.FunctionResponse.Response
				}
			}
		}
		return nil
	}, opts...)
	return out, err
}

func TestEngineRecordsUsage(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent("hi"))
	f.llm.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1000, CandidatesTokenCount: 200}
	_, err := collect(t, f.eng, "s1", "hello")
	require.NoError(t, err)
	u := f.eng.Usage("s1")
	assert.Equal(t, 1, u.Calls, "usage %+v", u)
	assert.Equal(t, int64(1000), u.Input, "usage %+v", u)
	assert.Equal(t, int64(200), u.Output, "usage %+v", u)
	assert.True(t, u.Priced, "usage %+v", u)
	assert.Greater(t, u.CostUSD, float64(0), "usage %+v", u)
	assert.Equal(t, 0, f.eng.Usage("other").Calls, "usage leaked across sessions")
}

func TestEngineMaxTurns(t *testing.T) {
	loop := []*genai.Content{}
	for i := 0; i < 6; i++ {
		loop = append(loop, toolCall("list_files", map[string]any{}))
	}
	f := newEngineWith(t, fixtureOpts{}, loop...)
	_, err := functionResponses(t, f.eng, "s", "loop forever", WithMaxTurns(2))
	require.ErrorIs(t, err, api.ErrMaxTurns, "expected ErrMaxTurns, got %v", err)
	assert.Equal(t, 2, f.llm.Calls(), "expected exactly 2 model calls, got %d", f.llm.Calls())
	// Positive: without a limit the run finishes.
	g := newEngineWith(t, fixtureOpts{}, toolCall("list_files", map[string]any{}), textContent("done"))
	_, err = functionResponses(t, g.eng, "s", "once", WithMaxTurns(5))
	assert.NoError(t, err, "run within limit failed")
}

func TestEngineResumesPersistedSession(t *testing.T) {
	dir := t.TempDir()
	svc1, _ := cpsession.NewPersistentService(dir)
	f1 := newEngineWith(t, fixtureOpts{opts: []Option{WithSessionService(svc1)}}, textContent("noted"))
	_, err := collect(t, f1.eng, "resume-me", "the secret word is pineapple")
	require.NoError(t, err)

	// A brand-new engine (new process) with the same store and session ID.
	svc2, _ := cpsession.NewPersistentService(dir)
	f2 := newEngineWith(t, fixtureOpts{opts: []Option{WithSessionService(svc2)}}, textContent("pineapple"))
	_, err = collect(t, f2.eng, "resume-me", "what was the word?")
	require.NoError(t, err)
	var history strings.Builder
	for _, c := range f2.llm.Requests[0].Contents {
		for _, p := range c.Parts {
			history.WriteString(p.Text + "|")
		}
	}
	assert.Contains(t, history.String(), "pineapple", "resumed session lost history: %s", history.String())
	assert.Contains(t, history.String(), "noted", "resumed session lost history: %s", history.String())
}

func TestEngineInstructions(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{opts: []Option{WithInstructions("\nPROJECT RULE: use tabs")}}, textContent("ok"))
	collect(t, f.eng, "s", "hi")
	sys := systemText(f.llm)
	assert.Contains(t, sys, "PROJECT RULE: use tabs", "instructions missing from system prompt")
	f.eng.SetInstructions(context.Background(), "\nNEW RULE")
	collect(t, f.eng, "s2", "hi")
	sys = systemText(f.llm)
	assert.Contains(t, sys, "NEW RULE", "SetInstructions not applied")
	assert.NotContains(t, sys, "PROJECT RULE", "SetInstructions not applied")
}

func systemText(m *MockLLM) string {
	req := m.Requests[len(m.Requests)-1]
	var sb strings.Builder
	if req.Config != nil && req.Config.SystemInstruction != nil {
		for _, p := range req.Config.SystemInstruction.Parts {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

func TestEngineAuditsToolCalls(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, toolCall("list_files", map[string]any{}), textContent("done"))
	dir := t.TempDir()
	log, _ := audit.Open(dir, nil)
	f.tools.SetAudit(log)
	_, err := functionResponses(t, f.eng, "s", "list")
	require.NoError(t, err)
	log.Close()
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	require.Len(t, files, 1, "audit files %v", files)
	b, _ := os.ReadFile(files[0])
	for _, want := range []string{`"kind":"tool_call"`, `"kind":"tool_result"`, `"tool":"list_files"`} {
		assert.Contains(t, string(b), want, "audit log missing %s:\n%s", want, b)
	}
}

func TestEnginePreToolHookBlocks(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{cfg: func(c *config.Config) {
		c.Hooks.PreTool = []config.HookConfig{{Match: "list_*", Command: `echo "listing is forbidden" >&2; exit 2`}}
	}}, toolCall("list_files", map[string]any{}), toolCall("read_file", map[string]any{"path": "nope"}), textContent("done"))

	resps, err := functionResponses(t, f.eng, "s", "go")
	require.NoError(t, err)
	msg, _ := resps["list_files"]["error"].(string)
	assert.Contains(t, msg, "listing is forbidden", "hook did not block list_files: %v", resps["list_files"])
	// Non-matching tools run normally (read_file fails on its own, not via the hook).
	msg, _ = resps["read_file"]["error"].(string)
	assert.NotContains(t, msg, "hook", "hook applied to non-matching tool: %v", msg)
}

func TestUsageCacheWrites(t *testing.T) {
	tr := NewUsageTracker(map[string]config.ModelPrice{
		"claude-opus-5": {InputPerMTok: 5, OutputPerMTok: 25, CachedInputPerMTok: 0.5, CacheWritePerMTok: 6.25},
		"no-write-rate": {InputPerMTok: 1, OutputPerMTok: 2},
	})
	// 1M prompt tokens: 400k cache reads, 100k cache writes, 500k plain; 10k output.
	m := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1_000_000, CachedContentTokenCount: 400_000, CandidatesTokenCount: 10_000}
	u := tr.Estimate("claude-opus-5", m, 100_000)
	want := 0.5*5 + 0.4*0.5 + 0.1*6.25 + 0.01*25
	assert.LessOrEqual(t, abs(u.CostUSD-want), 1e-9, "cost %v want %v (%+v)", u.CostUSD, want, u)
	assert.Equal(t, int64(100_000), u.CacheWrite, "cost %v want %v (%+v)", u.CostUSD, want, u)
	// Without a write rate, writes bill at the input rate.
	u = tr.Estimate("no-write-rate", m, 100_000)
	assert.LessOrEqual(t, abs(u.CostUSD-(0.6*1+0.01*2)), 1e-9, "fallback write rate: %v", u.CostUSD)
	// Nonsense write counts are clamped to the uncached prompt.
	u = tr.Estimate("claude-opus-5", m, 5_000_000)
	assert.Equal(t, int64(600_000), u.CacheWrite, "clamp: %d", u.CacheWrite)
	assert.True(t, tr.HasPrice("claude-opus-5-preview"), "HasPrice prefix logic")
	assert.False(t, tr.HasPrice("gpt-9"), "HasPrice prefix logic")
}

func TestEnginePricesServedModelAndCacheWrites(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent("hi"))
	f.llm.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1_000_000}
	// Configured model is gemini-3.8-flash, but a fallback model served it.
	f.llm.ServedBy = "claude-opus-5"
	f.llm.Metadata = map[string]any{CacheWriteTokensKey: int64(1_000_000)}
	collect(t, f.eng, "s", "go")
	u := f.eng.Usage("s")
	assert.LessOrEqual(t, abs(u.CostUSD-6.25), 1e-9, "expected opus cache-write pricing ($6.25), got $%v %+v", u.CostUSD, u)
	assert.Equal(t, int64(1_000_000), u.CacheWrite, "expected opus cache-write pricing ($6.25), got $%v %+v", u.CostUSD, u)
	// An unknown served model falls back to the configured model's price.
	g := newEngineWith(t, fixtureOpts{}, textContent("hi"))
	g.llm.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1_000_000}
	g.llm.ServedBy = "gemini-3.8-flash-exp-0927"
	collect(t, g.eng, "s", "go")
	want := config.DefaultPricingAt(time.Now())["gemini-3.8-flash"].InputPerMTok // 1M input tokens
	u = g.eng.Usage("s")
	assert.True(t, u.Priced, "prefix-priced served model: %+v", u)
	assert.LessOrEqual(t, abs(u.CostUSD-want), 1e-9, "prefix-priced served model: %+v", u)
}
