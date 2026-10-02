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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// recorder keeps each request's generation config; failing makes it fail
// before answering (so a fallback takes over).
type recorder struct {
	name    string
	failing bool
	mu      sync.Mutex
	configs []*genai.GenerateContentConfig
}

func (r *recorder) Name() string { return r.name }
func (r *recorder) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		r.mu.Lock()
		r.configs = append(r.configs, req.Config)
		r.mu.Unlock()
		if r.failing {
			yield(nil, errors.New(r.name+" is down"))
			return
		}
		yield(&model.LLMResponse{Content: genai.NewContentFromText("from "+r.name, genai.RoleModel)}, nil)
	}
}

func (r *recorder) last(t *testing.T) *genai.GenerateContentConfig {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEqual(t, 0, len(r.configs), "%s got no config", r.name)
	require.NotNil(t, r.configs[len(r.configs)-1], "%s got no config", r.name)
	return r.configs[len(r.configs)-1]
}

func ptr[T any](v T) *T { return &v }

func lookupOf(m map[string]config.ModelSettings) settingsLookup {
	return func(name string) (config.ModelSettings, bool) { s, ok := m[name]; return s, ok }
}

func TestEachFallbackModelGetsItsOwnSettings(t *testing.T) {
	primary := &recorder{name: "gemini-3.8-flash", failing: true}
	backup := &recorder{name: "gpt-5"}
	chain := newFallbackModel([]model.LLM{withModelSettings(primary, "gemini"), withModelSettings(backup, "openai")})
	ctx := withSettingsLookup(context.Background(), lookupOf(map[string]config.ModelSettings{
		"gemini-3.8-flash": {Temperature: ptr(0.9), Seed: ptr(7)},
		"gpt-5":            {TopP: ptr(0.5), Seed: ptr(7)}, // OpenAI can't take a seed
	}))
	global := &genai.GenerateContentConfig{Temperature: genai.Ptr[float32](0.2), MaxOutputTokens: 8192}
	for _, err := range chain.GenerateContent(ctx, &model.LLMRequest{Config: global}, false) {
		require.NoError(t, err)
	}

	p := primary.last(t)
	assert.Equal(t, float32(0.9), *p.Temperature, "primary: temperature %v seed %v max %d", *p.Temperature, *p.Seed, p.MaxOutputTokens)
	assert.Equal(t, int32(7), *p.Seed, "primary: temperature %v seed %v max %d", *p.Temperature, *p.Seed, p.MaxOutputTokens)
	assert.Equal(t, int32(8192), p.MaxOutputTokens, "primary: temperature %v seed %v max %d", *p.Temperature, *p.Seed, p.MaxOutputTokens)
	b := backup.last(t)
	assert.Equal(t, float32(0.2), *b.Temperature, "backup must keep the global temperature, get its top_p and no seed: %+v", b)
	assert.Equal(t, float32(0.5), *b.TopP, "backup must keep the global temperature, get its top_p and no seed: %+v", b)
	assert.Nil(t, b.Seed, "backup must keep the global temperature, get its top_p and no seed: %+v", b)
	assert.Equal(t, int32(8192), b.MaxOutputTokens, "backup must keep the global temperature, get its top_p and no seed: %+v", b)
	assert.Equal(t, float32(0.2), *global.Temperature, "the shared request config was changed: %+v", global)
	assert.Nil(t, global.Seed, "the shared request config was changed: %+v", global)
	assert.Nil(t, global.TopP, "the shared request config was changed: %+v", global)
}

func TestSettingsWithoutLookupOrEntryLeaveTheRequestAlone(t *testing.T) {
	r := &recorder{name: "m"}
	m := withModelSettings(r, "gemini")
	cfg := &genai.GenerateContentConfig{MaxOutputTokens: 5}
	for _, ctx := range []context.Context{
		context.Background(), // e.g. doctor
		withSettingsLookup(context.Background(), lookupOf(map[string]config.ModelSettings{"other": {Seed: ptr(1)}})),
	} {
		for range m.GenerateContent(ctx, &model.LLMRequest{Config: cfg}, false) {
		}
		require.Same(t, cfg, r.last(t), "request was copied or changed without settings for the model")
	}
}

func TestSettingSupported(t *testing.T) {
	cases := []struct {
		provider, model, key string
		want                 bool
	}{
		{"gemini", "gemini-3.8-flash", "seed", true},
		{"gemini", "gemini-3.8-flash", "top_p", true},
		{"openai", "gpt-5", "seed", false},
		{"ollama", "qwen2.5-coder:7b", "temperature", true},
		{"anthropic", "claude-sonnet-5", "temperature", false},
		{"anthropic", "claude-haiku-4-5", "temperature", true},
		{"anthropic", "claude-haiku-4-5", "top_p", false},
		{"anthropic", "claude-sonnet-5", "max_tokens", true},
		{"anthropic", "claude-sonnet-5", "seed", false},
		{"anthropic", "claude-opus-5-5", "reasoning_effort", true},
		{"anthropic", "claude-haiku-4-5", "reasoning_effort", false},
		{"anthropic", "claude-haiku-4-5", "thinking_budget", true},
		{"anthropic", "claude-3-5-haiku", "thinking_budget", false},
		{"gemini", "gemini-2.5-pro", "reasoning_effort", false},
		{"gemini", "models/gemini-3.8-flash", "reasoning_effort", true},
		{"openai", "gpt-5", "reasoning_effort", true},
	}
	for _, c := range cases {
		got := SettingSupported(c.provider, c.model, c.key)
		assert.Equal(t, c.want, got, "%s %s %s = %v", c.provider, c.model, c.key, got)
	}
}

// Thinking can't be turned off (thinking_budget 0) on models that always
// think; any other budget applies.
func TestSettingApplies(t *testing.T) {
	budget := func(n int) config.ModelSettings { return config.ModelSettings{ThinkingBudget: &n} }
	cases := []struct {
		provider, model string
		s               config.ModelSettings
		want            bool
	}{
		{"anthropic", "claude-opus-5-5", budget(0), false},
		{"anthropic", "claude-fable-5", budget(0), false},
		{"anthropic", "claude-opus-5-5", budget(8000), true},
		{"anthropic", "claude-sonnet-5", budget(0), true},
		{"anthropic", "claude-haiku-4-5", budget(0), true},
		{"anthropic", "claude-3-5-haiku", budget(2048), false},
		{"gemini", "gemini-3.8-flash", budget(0), true},
	}
	for _, c := range cases {
		t.Run(c.model+"/"+fmt.Sprint(*c.s.ThinkingBudget), func(t *testing.T) {
			assert.Equal(t, c.want, SettingApplies(c.provider, c.model, "thinking_budget", c.s))
		})
	}
}

func TestEngineAppliesModelSettingsAndChangesTakeEffect(t *testing.T) {
	rec := &recorder{name: "gemini-3.8-flash"}
	f := newEngineWith(t, fixtureOpts{cfg: func(c *config.Config) {
		c.ModelSettings = map[string]config.ModelSettings{
			"gemini/gemini-3.8-flash": {Temperature: ptr(1.1)}, // a provider prefix is ignored
			"unused":                  {},
		}
	}})
	require.NoError(t, f.eng.SetModel(context.Background(), withModelSettings(rec, "gemini")))
	_, err := collect(t, f.eng, "s", "hi")
	require.NoError(t, err)
	got := rec.last(t)
	require.Equal(t, float32(1.1), *got.Temperature, "first call: temperature %v, max %d", *got.Temperature, got.MaxOutputTokens)
	require.Equal(t, int32(f.cfg.Blitz.MaxTokens), got.MaxOutputTokens, "first call: temperature %v, max %d", *got.Temperature, got.MaxOutputTokens)
	all := f.eng.AllModelSettings()
	require.Len(t, all, 1, "AllModelSettings = %v", all)
	require.NotNil(t, all["gemini-3.8-flash"].Temperature, "AllModelSettings = %v", all)

	f.eng.SetModelSettings("gemini-3.8-flash", config.ModelSettings{MaxTokens: ptr(100)})
	_, err = collect(t, f.eng, "s", "again")
	require.NoError(t, err)
	got = rec.last(t)
	require.Equal(t, int32(100), got.MaxOutputTokens, "after change: max %d temperature %v", got.MaxOutputTokens, *got.Temperature)
	require.Equal(t, float32(f.cfg.Blitz.Temperature), *got.Temperature, "after change: max %d temperature %v", got.MaxOutputTokens, *got.Temperature)

	f.eng.SetModelSettings("gemini-3.8-flash", config.ModelSettings{})
	s := f.eng.ModelSettings("gemini-3.8-flash")
	require.True(t, s.IsZero(), "not removed: %+v", s)
}

func TestSubagentUsesModelSettings(t *testing.T) {
	sub := &recorder{name: "claude-haiku-4-5"}
	f := newEngineWith(t, fixtureOpts{
		cfg: func(c *config.Config) {
			c.ModelSettings = map[string]config.ModelSettings{"claude-haiku-4-5": {Temperature: ptr(0.3)}}
		},
		opts: []Option{WithAgentModel("qa", withModelSettings(sub, "anthropic"))},
	})
	// Called directly, as a hook would, without a run context.
	_, err := f.eng.InvokeSubagent(context.Background(), "qa", "review")
	require.NoError(t, err)
	got := sub.last(t)
	require.NotNil(t, got.Temperature, "sub-agent temperature %v", got.Temperature)
	require.Equal(t, float32(0.3), *got.Temperature, "sub-agent temperature %v", got.Temperature)
}

// The settings reach the wire through the real OpenAI adapter, and a seed
// (which that adapter rejects) is left out instead of failing the call.
func TestOpenAIRequestCarriesModelSettings(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, openAIOK)
	}))
	t.Cleanup(srv.Close)
	cfg := config.DefaultConfig()
	cfg.LLM.Provider, cfg.LLM.OpenAI.BaseURL, cfg.LLM.OpenAI.APIKey, cfg.LLM.MaxRetries = "openai", srv.URL+"/v1", "sk-test", 0
	m, err := NewModel(context.Background(), cfg, "gpt-test")
	require.NoError(t, err)
	ctx := withSettingsLookup(context.Background(), lookupOf(map[string]config.ModelSettings{
		"gpt-test": {Temperature: ptr(0.25), TopP: ptr(0.75), MaxTokens: ptr(321), Seed: ptr(42)},
	}))
	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}}
	for _, err := range m.GenerateContent(ctx, req, false) {
		require.NoError(t, err)
	}
	require.Equal(t, 0.25, body["temperature"], "request body: temperature %v top_p %v max_output_tokens %v", body["temperature"], body["top_p"], body["max_output_tokens"])
	require.Equal(t, 0.75, body["top_p"], "request body: temperature %v top_p %v max_output_tokens %v", body["temperature"], body["top_p"], body["max_output_tokens"])
	require.Equal(t, 321.0, body["max_output_tokens"], "request body: temperature %v top_p %v max_output_tokens %v", body["temperature"], body["top_p"], body["max_output_tokens"])
	_, ok := body["seed"]
	require.False(t, ok, "seed was sent")
}

// When both a bare and a "provider/" key name the same model, the bare one
// wins, whatever the map order.
func TestBareModelSettingsKeyWins(t *testing.T) {
	for range 20 {
		f := newEngineWith(t, fixtureOpts{cfg: func(c *config.Config) {
			c.ModelSettings = map[string]config.ModelSettings{
				"openai/gpt-5": {Seed: ptr(1)},
				"gpt-5":        {Seed: ptr(2)},
			}
		}})
		s := f.eng.ModelSettings("gpt-5")
		require.NotNil(t, s.Seed, "seed %v", s.Seed)
		require.Equal(t, 2, *s.Seed, "seed %v", s.Seed)
	}
}

// The effort becomes genai's thinking level (max has no genai level, so it
// is high there), a budget the thinking budget; Gemini can't take both.
func TestReasoningSettingsBecomeThinkingConfig(t *testing.T) {
	cases := []struct {
		provider, model string
		s               config.ModelSettings
		want            *genai.ThinkingConfig
	}{
		{"gemini", "gemini-3.8-flash", config.ModelSettings{ReasoningEffort: ptr("max")}, &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelHigh}},
		{"gemini", "gemini-3.8-flash", config.ModelSettings{ReasoningEffort: ptr("low"), ThinkingBudget: ptr(2048)}, &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}},
		{"gemini", "gemini-2.5-pro", config.ModelSettings{ReasoningEffort: ptr("low"), ThinkingBudget: ptr(2048)}, &genai.ThinkingConfig{ThinkingBudget: genai.Ptr[int32](2048)}},
		{"openai", "gpt-5", config.ModelSettings{ReasoningEffort: ptr("minimal")}, &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelMinimal}},
		{"openai", "gpt-5", config.ModelSettings{ThinkingBudget: ptr(0)}, &genai.ThinkingConfig{ThinkingBudget: genai.Ptr[int32](0)}},
		{"anthropic", "claude-3-5-haiku-latest", config.ModelSettings{ReasoningEffort: ptr("high"), ThinkingBudget: ptr(0)}, nil},
	}
	for _, c := range cases {
		r := &recorder{name: c.model}
		m := withModelSettings(r, c.provider)
		ctx := withSettingsLookup(context.Background(), lookupOf(map[string]config.ModelSettings{c.model: c.s}))
		for range m.GenerateContent(ctx, &model.LLMRequest{Config: &genai.GenerateContentConfig{}}, false) {
		}
		got := r.last(t).ThinkingConfig
		assert.Equal(t, (c.want == nil), (got == nil), "%s %s %+v: thinking %+v, want %+v", c.provider, c.model, c.s, got, c.want)
		assert.False(t, got != nil && (got.ThinkingLevel != c.want.ThinkingLevel ||
			(got.ThinkingBudget == nil) != (c.want.ThinkingBudget == nil) ||
			got.ThinkingBudget != nil && *got.ThinkingBudget != *c.want.ThinkingBudget), "%s %s %+v: thinking %+v, want %+v", c.provider, c.model, c.s, got, c.want)
	}
}

// The session's effort (/effort) wins over a model's own, and applies to
// models with no settings at all.
func TestSessionEffortOverridesModelSettings(t *testing.T) {
	rec := &recorder{name: "gemini-3.8-flash"}
	f := newEngineWith(t, fixtureOpts{cfg: func(c *config.Config) {
		c.ModelSettings = map[string]config.ModelSettings{"gemini-3.8-flash": {ReasoningEffort: ptr("low"), Temperature: ptr(0.5)}}
	}})
	require.NoError(t, f.eng.SetModel(context.Background(), withModelSettings(rec, "gemini")))
	level := func() genai.ThinkingLevel {
		t.Helper()
		_, err := collect(t, f.eng, "s", "hi")
		require.NoError(t, err)
		if tc := rec.last(t).ThinkingConfig; tc != nil {
			return tc.ThinkingLevel
		}
		return ""
	}
	got := level()
	require.Equal(t, genai.ThinkingLevelLow, got, "model setting: %q", got)
	f.eng.SetEffort("high")
	got = level()
	require.Equal(t, genai.ThinkingLevelHigh, got, "session effort: %q (the model's other settings must stay)", got)
	require.Equal(t, float32(0.5), *rec.last(t).Temperature, "session effort: %q (the model's other settings must stay)", got)
	require.Equal(t, "high", f.eng.Effort(), "session effort: %q (the model's other settings must stay)", got)
	s := f.eng.ModelSettings("gemini-3.8-flash")
	assert.Equal(t, "low", *s.ReasoningEffort, "the session effort leaked into the saved settings: %v", *s.ReasoningEffort)
	f.eng.SetModelSettings("gemini-3.8-flash", config.ModelSettings{})
	got = level()
	require.Equal(t, genai.ThinkingLevelHigh, got, "session effort without model settings: %q", got)
	f.eng.SetEffort("")
	got = level()
	require.Equal(t, genai.ThinkingLevel(""), got, "after clearing: %q", got)
}

// An agent's own model settings (its frontmatter's) win over the model's
// and the session's effort, as the main agent and as a sub-agent, and only
// for that agent.
func TestAgentModelSettingsWin(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tuned.md"), []byte("---\nname: tuned\ndescription: d\ntools: []\ntemperature: 0.2\neffort: minimal\n---\nprompt\n"), 0o644))
	main := &recorder{name: "gemini-3.8-flash"}
	sub := &recorder{name: "gemini-3.8-pro"}
	f := newEngineWith(t, fixtureOpts{
		agentDir: dir,
		cfg: func(c *config.Config) {
			c.ModelSettings = map[string]config.ModelSettings{"gemini-3.8-flash": {Temperature: ptr(0.9), TopP: ptr(0.5)}}
		},
		opts: []Option{WithAgentModel("tuned", withModelSettings(sub, "gemini"))},
	})
	require.NoError(t, f.eng.SetModel(context.Background(), withModelSettings(main, "gemini")))
	f.eng.SetEffort("high")

	_, err := f.eng.InvokeSubagent(context.Background(), "tuned", "go")
	require.NoError(t, err)
	got := sub.last(t)
	assert.Equal(t, float32(0.2), *got.Temperature)
	assert.Equal(t, genai.ThinkingLevelMinimal, got.ThinkingConfig.ThinkingLevel, "the agent's effort over the session's")

	require.NoError(t, f.eng.SetActiveAgent(context.Background(), "blitz"))
	_, err = collect(t, f.eng, "s", "hi")
	require.NoError(t, err)
	got = main.last(t)
	assert.Equal(t, float32(0.9), *got.Temperature, "another agent keeps the model's settings")
	assert.Equal(t, genai.ThinkingLevelHigh, got.ThinkingConfig.ThinkingLevel)

	require.NoError(t, f.eng.Unpin(context.Background(), "tuned"))
	require.NoError(t, f.eng.SetActiveAgent(context.Background(), "tuned"))
	_, err = collect(t, f.eng, "s2", "hi")
	require.NoError(t, err)
	got = main.last(t)
	assert.Equal(t, float32(0.2), *got.Temperature, "as the main agent, on the shared model")
	assert.Equal(t, float32(0.5), *got.TopP, "settings it doesn't set stay the model's")
}

// Settings for providers Blitz doesn't map are passed on as written.
func TestSettingSupportedUnknownProvider(t *testing.T) {
	assert.True(t, SettingSupported("bedrock", "x", "seed"))
	assert.False(t, SettingSupported("gemini", "models/gemini-2.5-pro", "reasoning_effort"))
}
