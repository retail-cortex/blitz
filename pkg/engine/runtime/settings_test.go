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
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
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
	if len(r.configs) == 0 || r.configs[len(r.configs)-1] == nil {
		t.Fatalf("%s got no config", r.name)
	}
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
		if err != nil {
			t.Fatal(err)
		}
	}

	p := primary.last(t)
	if *p.Temperature != 0.9 || *p.Seed != 7 || p.MaxOutputTokens != 8192 {
		t.Errorf("primary: temperature %v seed %v max %d", *p.Temperature, *p.Seed, p.MaxOutputTokens)
	}
	b := backup.last(t)
	if *b.Temperature != 0.2 || *b.TopP != 0.5 || b.Seed != nil || b.MaxOutputTokens != 8192 {
		t.Errorf("backup must keep the global temperature, get its top_p and no seed: %+v", b)
	}
	if *global.Temperature != 0.2 || global.Seed != nil || global.TopP != nil {
		t.Errorf("the shared request config was changed: %+v", global)
	}
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
		if r.last(t) != cfg {
			t.Fatal("request was copied or changed without settings for the model")
		}
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
		if got := SettingSupported(c.provider, c.model, c.key); got != c.want {
			t.Errorf("%s %s %s = %v", c.provider, c.model, c.key, got)
		}
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
	if err := f.eng.SetModel(context.Background(), withModelSettings(rec, "gemini")); err != nil {
		t.Fatal(err)
	}
	if _, err := collect(t, f.eng, "s", "hi"); err != nil {
		t.Fatal(err)
	}
	if got := rec.last(t); *got.Temperature != 1.1 || got.MaxOutputTokens != int32(f.cfg.Blitz.MaxTokens) {
		t.Fatalf("first call: temperature %v, max %d", *got.Temperature, got.MaxOutputTokens)
	}
	if all := f.eng.AllModelSettings(); len(all) != 1 || all["gemini-3.8-flash"].Temperature == nil {
		t.Fatalf("AllModelSettings = %v", all)
	}

	f.eng.SetModelSettings("gemini-3.8-flash", config.ModelSettings{MaxTokens: ptr(100)})
	if _, err := collect(t, f.eng, "s", "again"); err != nil {
		t.Fatal(err)
	}
	if got := rec.last(t); got.MaxOutputTokens != 100 || *got.Temperature != float32(f.cfg.Blitz.Temperature) {
		t.Fatalf("after change: max %d temperature %v", got.MaxOutputTokens, *got.Temperature)
	}

	f.eng.SetModelSettings("gemini-3.8-flash", config.ModelSettings{})
	if s := f.eng.ModelSettings("gemini-3.8-flash"); !s.IsZero() {
		t.Fatalf("not removed: %+v", s)
	}
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
	if _, err := f.eng.InvokeSubagent(context.Background(), "qa", "review"); err != nil {
		t.Fatal(err)
	}
	if got := sub.last(t); got.Temperature == nil || *got.Temperature != 0.3 {
		t.Fatalf("sub-agent temperature %v", got.Temperature)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	ctx := withSettingsLookup(context.Background(), lookupOf(map[string]config.ModelSettings{
		"gpt-test": {Temperature: ptr(0.25), TopP: ptr(0.75), MaxTokens: ptr(321), Seed: ptr(42)},
	}))
	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}}
	for _, err := range m.GenerateContent(ctx, req, false) {
		if err != nil {
			t.Fatal(err)
		}
	}
	if body["temperature"] != 0.25 || body["top_p"] != 0.75 || body["max_output_tokens"] != 321.0 {
		t.Fatalf("request body: temperature %v top_p %v max_output_tokens %v", body["temperature"], body["top_p"], body["max_output_tokens"])
	}
	if _, ok := body["seed"]; ok {
		t.Fatal("seed was sent")
	}
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
		if s := f.eng.ModelSettings("gpt-5"); s.Seed == nil || *s.Seed != 2 {
			t.Fatalf("seed %v", s.Seed)
		}
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
		if (got == nil) != (c.want == nil) || got != nil && (got.ThinkingLevel != c.want.ThinkingLevel ||
			(got.ThinkingBudget == nil) != (c.want.ThinkingBudget == nil) ||
			got.ThinkingBudget != nil && *got.ThinkingBudget != *c.want.ThinkingBudget) {
			t.Errorf("%s %s %+v: thinking %+v, want %+v", c.provider, c.model, c.s, got, c.want)
		}
	}
}

// The session's effort (/effort) wins over a model's own, and applies to
// models with no settings at all.
func TestSessionEffortOverridesModelSettings(t *testing.T) {
	rec := &recorder{name: "gemini-3.8-flash"}
	f := newEngineWith(t, fixtureOpts{cfg: func(c *config.Config) {
		c.ModelSettings = map[string]config.ModelSettings{"gemini-3.8-flash": {ReasoningEffort: ptr("low"), Temperature: ptr(0.5)}}
	}})
	if err := f.eng.SetModel(context.Background(), withModelSettings(rec, "gemini")); err != nil {
		t.Fatal(err)
	}
	level := func() genai.ThinkingLevel {
		t.Helper()
		if _, err := collect(t, f.eng, "s", "hi"); err != nil {
			t.Fatal(err)
		}
		if tc := rec.last(t).ThinkingConfig; tc != nil {
			return tc.ThinkingLevel
		}
		return ""
	}
	if got := level(); got != genai.ThinkingLevelLow {
		t.Fatalf("model setting: %q", got)
	}
	f.eng.SetEffort("high")
	if got := level(); got != genai.ThinkingLevelHigh || *rec.last(t).Temperature != 0.5 || f.eng.Effort() != "high" {
		t.Fatalf("session effort: %q (the model's other settings must stay)", got)
	}
	if s := f.eng.ModelSettings("gemini-3.8-flash"); *s.ReasoningEffort != "low" {
		t.Errorf("the session effort leaked into the saved settings: %v", *s.ReasoningEffort)
	}
	f.eng.SetModelSettings("gemini-3.8-flash", config.ModelSettings{})
	if got := level(); got != genai.ThinkingLevelHigh {
		t.Fatalf("session effort without model settings: %q", got)
	}
	f.eng.SetEffort("")
	if got := level(); got != "" {
		t.Fatalf("after clearing: %q", got)
	}
}
