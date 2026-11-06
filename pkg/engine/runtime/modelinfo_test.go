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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/retail-cortex/blitz/pkg/modelfacts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

// tempLearned gives the test its own learned facts, and puts the
// registry's overlays back after it.
func tempLearned(t *testing.T) *modelfacts.Store {
	t.Helper()
	models.mu.Lock()
	prevStore, prevListed, prevCat, prevPricing, prevTold := models.store, models.listed, models.catalog, models.pricing, models.told
	models.listed, models.catalog, models.told = map[string]listedFacts{}, map[string]enterprise.Model{}, map[string]bool{}
	models.mu.Unlock()
	s := modelfacts.Open(filepath.Join(t.TempDir(), "models.learned.toml"))
	SetLearnedStore(s)
	t.Cleanup(func() {
		models.mu.Lock()
		models.store, models.listed, models.catalog, models.pricing, models.told = prevStore, prevListed, prevCat, prevPricing, prevTold
		models.mu.Unlock()
	})
	return s
}

func TestCanonicalModel(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"claude-opus-5-5", "claude-opus-5-5"},
		{"models/gemini-3-pro", "gemini-3-pro"},
		{"us.anthropic.claude-sonnet-5-v1:0", "claude-sonnet-5"},
		{"global.anthropic.claude-opus-4-1-20250805-v1:0", "claude-opus-4-1-20250805"},
		{"anthropic.claude-3-5-sonnet-20240620-v1:0", "claude-3-5-sonnet-20240620"},
		{"claude-opus-4-1@20250805", "claude-opus-4-1"},
		{"projects/p/locations/l/publishers/anthropic/models/claude-opus-5@20260101", "claude-opus-5"},
		{"  GPT-5  ", "gpt-5"},
	} {
		assert.Equal(t, tc.want, canonicalModel(tc.in), tc.in)
	}
	assert.True(t, bedrockID("us.anthropic.claude-sonnet-5-v1:0"))
	assert.True(t, bedrockID("anthropic.claude-3-haiku"))
	assert.False(t, bedrockID("claude-sonnet-5"))
}

// The table knows the families Blitz ships for; Bedrock's and Vertex AI's
// IDs read as the model they name (CA-10).
func TestInfoFromTheTable(t *testing.T) {
	tempLearned(t)
	tests := []struct {
		api, provider, model string
		thinking             ThinkingMode
		sampling, effort     bool
		window, output       int
		guess                bool
	}{
		{"anthropic", "anthropic", "claude-opus-5-5", ThinkingAlways, false, true, 200_000, 64_000, false},
		{"anthropic", "bedrock", "us.anthropic.claude-opus-5-5-v1:0", ThinkingAlways, false, true, 200_000, 64_000, false},
		{"anthropic", "vertex-anthropic", "claude-sonnet-4-5@20250929", ThinkingBudget, true, false, 200_000, 64_000, false},
		{"anthropic", "anthropic", "claude-3-5-haiku", ThinkingNone, true, false, 200_000, 8_192, false},
		{"gemini", "gemini", "gemini-3-pro", ThinkingBudget, true, true, 1_048_576, 65_536, false},
		{"gemini", "gemini", "gemini-2.0-flash", ThinkingNone, true, false, 1_048_576, 8_192, false},
		{"openai", "openai", "gpt-5", ThinkingAdaptive, false, true, 400_000, 128_000, false},
		{"openai", "openai", "o3-mini", ThinkingAdaptive, false, true, 200_000, 100_000, false},
		{"openai", "openai", "gpt-4o", ThinkingNone, true, false, 128_000, 16_384, false},
		{"openai", "ollama", "llama3.2", ThinkingNone, true, false, unknownWindow, 8_192, true},
	}
	for _, tt := range tests {
		t.Run(tt.provider+"/"+tt.model, func(t *testing.T) {
			m := Info(tt.api, tt.provider, tt.model)
			assert.Equal(t, tt.thinking, m.Thinking)
			assert.Equal(t, tt.sampling, m.Sampling)
			assert.Equal(t, tt.effort, m.Effort)
			assert.Equal(t, tt.window, m.ContextWindow)
			assert.Equal(t, tt.output, m.MaxOutput)
			assert.Equal(t, tt.guess, m.Guess(factContext))
			assert.Equal(t, ToolsNative, m.Tools)
			assert.True(t, m.Streams)
		})
	}
	assert.Equal(t, "gemini", Info("", "vertex", "gemini-3-pro").API, "the provider's usual API")
	assert.Equal(t, "anthropic", Info("", "bedrock", "x").API)
	assert.Equal(t, "openai", Info("", "", "x").API)
}

func kinds(list []Accept) []images.Kind {
	var out []images.Kind
	for _, a := range list {
		out = append(out, a.Kind)
	}
	return out
}

// What a model takes follows its wire API and what's known of its vision
// (CA-60, CA-64); Claude on Bedrock gets PDFs as text (IMG-51).
func TestInfoAccepts(t *testing.T) {
	tempLearned(t)
	gem := Info("gemini", "gemini", "gemini-3-pro").Accepts(true)
	assert.Equal(t, []images.Kind{images.KindImage, images.KindDocument, images.KindText, images.KindAudio, images.KindVideo}, kinds(gem))
	assert.True(t, gem[3].Upload)
	viaGateway := Info("openai", "api", "gemini-3-pro").Accepts(false)
	assert.Equal(t, []images.Kind{images.KindImage, images.KindDocument, images.KindText}, kinds(viaGateway), "a gateway's Gemini follows the gateway's API")
	assert.Zero(t, viaGateway[1].Inline, "PDFs as text")

	claude := Info("anthropic", "anthropic", "claude-opus-5-5").Accepts(false)
	assert.Positive(t, claude[1].Inline, "Claude reads PDFs")
	bedrock := Info("anthropic", "bedrock", "us.anthropic.claude-opus-5-5-v1:0").Accepts(false)
	assert.Zero(t, bedrock[1].Inline, "Claude on Bedrock gets their text")

	setListed("ollama", "llama3.2", listedFacts{Vision: ptr(false), Tools: ptr(false), ContextWindow: 131_072})
	local := Info("openai", "ollama", "llama3.2")
	assert.NotContains(t, kinds(local.Accepts(false)), images.KindImage, "no vision, no images")
	assert.Equal(t, ToolsNone, local.Tools)
	assert.Equal(t, 131_072, local.ContextWindow)
	assert.False(t, local.Guess(factContext))
	assert.False(t, local.Guess(factVision))
}

// The organization's catalog, the settings' prices and what was learned
// each win over the table (CA-11).
func TestInfoOverlays(t *testing.T) {
	s := tempLearned(t)
	setCatalog(enterprise.Model{ID: "org-claude", Capabilities: map[string]any{"tools": true, "images": false, "thinking": true, "context": float64(500_000)}, MaxOutputTokens: 32_000, Price: &enterprise.Price{InputPerMTok: 2, OutputPerMTok: 9}})
	org := Info("anthropic", "enterprise", "org-claude")
	assert.Equal(t, 500_000, org.ContextWindow)
	assert.Equal(t, 32_000, org.MaxOutput)
	assert.False(t, org.Vision)
	assert.Equal(t, ThinkingAdaptive, org.Thinking)
	require.NotNil(t, org.Price)
	assert.InDelta(t, 9.0, org.Price.OutputPerMTok, 1e-9)
	assert.Empty(t, org.Guesses)
	_, err := s.Learn("enterprise", "org-claude", modelfacts.Fact{Kind: modelfacts.NoTools, At: time.Now()})
	require.NoError(t, err)
	assert.Equal(t, ToolsNative, Info("anthropic", "enterprise", "org-claude").Tools, "the organization's catalog wins over what was learned (CA-14)")

	setCatalog(enterprise.Model{ID: "org-small", Capabilities: map[string]any{"tools": false, "images": true, "thinking": false}})
	small := Info("openai", "enterprise", "org-small")
	assert.Equal(t, ToolsNone, small.Tools)
	assert.Equal(t, ThinkingNone, small.Thinking)

	setPricing(map[string]config.ModelPrice{"llama3.2": {InputPerMTok: 0.1, OutputPerMTok: 0.2}})
	assert.NotNil(t, Info("openai", "ollama", "llama3.2").Price)
	assert.Nil(t, Info("openai", "ollama", "other").Price)

	now := time.Now()
	for _, f := range []modelfacts.Fact{
		{Kind: modelfacts.NoTools}, {Kind: modelfacts.NoImages}, {Kind: modelfacts.NoStream},
		{Kind: modelfacts.NoParam, Value: "seed"}, {Kind: modelfacts.NoThinking},
		{Kind: modelfacts.ContextWindow, Value: "8192"}, {Kind: modelfacts.MaxOutput, Value: "2048"},
	} {
		f.At = now
		_, err := s.Learn("api", "local-model", f)
		require.NoError(t, err)
	}
	m := Info("openai", "api", "local-model")
	assert.Equal(t, ToolsNone, m.Tools)
	assert.False(t, m.Vision)
	assert.False(t, m.Streams)
	assert.True(t, m.refuses("seed"))
	assert.Equal(t, ThinkingNone, m.Thinking)
	assert.Equal(t, 8192, m.ContextWindow)
	assert.Equal(t, 2048, m.MaxOutput)
	assert.Len(t, m.Learned, 7)

	_, err = s.Learn("api", "odd", modelfacts.Fact{Kind: modelfacts.ContextWindow, Value: "nonsense", At: now})
	require.NoError(t, err)
	assert.Equal(t, unknownWindow, Info("openai", "api", "odd").ContextWindow, "a window that isn't a number teaches nothing")
}

// A conversation is compacted at its model's point unless token_threshold
// was changed from its default (CA-110).
func TestCompactionPoint(t *testing.T) {
	tempLearned(t)
	gem := Info("gemini", "gemini", "gemini-3-pro")
	assert.Equal(t, (1_048_576-65_536)-(1_048_576-65_536)/10, gem.CompactionPoint(config.DefaultTokenThreshold))
	assert.Equal(t, 50_000, gem.CompactionPoint(50_000), "a threshold the person set")
	unknown := Info("openai", "ollama", "mystery")
	assert.Equal(t, config.DefaultTokenThreshold, unknown.CompactionPoint(config.DefaultTokenThreshold), "a guessed window keeps the setting")
	tight := ModelInfo{ContextWindow: 8_000, MaxOutput: 16_000}
	assert.Equal(t, 3_600, tight.CompactionPoint(config.DefaultTokenThreshold), "output larger than the window: half of it")
}

// The engine compacts at its model's point, and the meter reads it.
func TestEngineCompactionThreshold(t *testing.T) {
	tempLearned(t)
	cfg := conformanceConfig(t)
	eng := conformanceEngine(t, cfg, &settingsModel{inner: namedModel{"gemini-3-pro"}, provider: "gemini"})
	want := Info("gemini", "gemini", "gemini-3-pro").CompactionPoint(config.DefaultTokenThreshold)
	assert.Equal(t, want, eng.CompactionThreshold())
	assert.Equal(t, want, eng.compactionCfg.TokenThreshold)

	cfg.Context.TokenThreshold = 40_000
	require.NoError(t, eng.SetModel(t.Context(), &settingsModel{inner: namedModel{"gemini-3-pro"}, provider: "gemini"}))
	assert.Equal(t, 40_000, eng.CompactionThreshold(), "a threshold the person set")
}

// A model built by Blitz has a record, a fallback chain its primary's.
func TestInfoOf(t *testing.T) {
	tempLearned(t)
	a := &settingsModel{inner: namedModel{"claude-opus-5-5"}, provider: "anthropic"}
	b := &settingsModel{inner: namedModel{"gemini-3-pro"}, provider: "gemini"}
	info, ok := infoOf(&imageModel{inner: newFallbackModel([]model.LLM{a, b})})
	require.True(t, ok)
	assert.Equal(t, "claude-opus-5-5", info.Name)
	_, ok = infoOf(namedModel{"x"})
	assert.False(t, ok)
}

// Ollama's /api/show says whether a model calls tools and reads images,
// and its context length (CA-11).
func TestShowOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case r.URL.Path != "/api/show":
			http.NotFound(w, r)
		case body["model"] == "tooled":
			_ = json.NewEncoder(w).Encode(map[string]any{"capabilities": []string{"completion", "tools"}, "model_info": map[string]any{"llama.context_length": 131072, "general.name": "x"}})
		case body["model"] == "old":
			_ = json.NewEncoder(w).Encode(map[string]any{"model_info": map[string]any{}})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	f, ok := showOllama(t.Context(), srv.URL+"/v1", "tooled")
	require.True(t, ok)
	assert.True(t, *f.Tools)
	assert.False(t, *f.Vision)
	assert.Equal(t, 131072, f.ContextWindow)
	_, ok = showOllama(t.Context(), srv.URL+"/v1", "old")
	assert.False(t, ok, "an older Ollama says nothing of capabilities")
	_, ok = showOllama(t.Context(), srv.URL+"/v1", "missing")
	assert.False(t, ok)
	_, ok = showOllama(t.Context(), "http://127.0.0.1:1/v1", "x")
	assert.False(t, ok, "Ollama not running")

	tempLearned(t)
	listOllama(srv.URL+"/v1", "tooled")
	assert.Equal(t, 131072, Info("openai", "ollama", "tooled").ContextWindow)
}
