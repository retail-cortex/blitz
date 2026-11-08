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
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
)

// Only models that hold a conversation are chat models; a name Blitz
// doesn't know is kept (spec_061_models MOD-02).
func TestChatModel(t *testing.T) {
	for id, want := range map[string]bool{
		"gemini-4-carbon":                   true,
		"gemini-3.8-flash":                  true,
		"claude-opus-5-5":                   true,
		"gpt-5":                             true,
		"qwen2.5-coder:7b":                  true,
		"something-new-2027":                true,
		"text-embedding-004":                false,
		"gemini-embedding-001":              false,
		"imagen-4.0-generate-001":           false,
		"gemini-3.1-flash-image":            false,
		"gemini-2.0-flash-image-generation": false,
		"veo-3.0-generate":                  false,
		"lyria-2":                           false,
		"gemini-2.5-flash-preview-tts":      false,
		"gemini-2.5-flash-native-audio":     false,
		"gpt-image-1":                       false,
		"dall-e-3":                          false,
		"whisper-1":                         false,
		"gpt-4o-transcribe":                 false,
		"omni-moderation-latest":            false,
		"gpt-4o-realtime-preview":           false,
		"aqa":                               false,
		"learnlm-2.0-flash":                 false,
	} {
		t.Run(id, func(t *testing.T) { assert.Equal(t, want, ChatModel(id)) })
	}
}

// Each provider's chat models come newest family first, then by name.
func TestChatModelsOrder(t *testing.T) {
	in := []ProviderModels{
		{Provider: "gemini", Models: []ListedModel{{ID: "gemini-2.5-pro"}, {ID: "gemini-3.8-flash"}, {ID: "text-embedding-004"}, {ID: "gemini-4-carbon"}, {ID: "gemini-3.8-pro"}, {ID: "gemini-3"}}},
		{Provider: "anthropic", Models: []ListedModel{{ID: "claude-haiku-4-5"}, {ID: "claude-opus-5-5"}, {ID: "claude-3-5-sonnet-20241022"}}},
		{Provider: "openai", Err: assert.AnError},
	}
	got := ChatModels(in)
	ids := func(pm ProviderModels) []string {
		var out []string
		for _, m := range pm.Models {
			out = append(out, m.ID)
		}
		return out
	}
	assert.Equal(t, []string{"gemini-4-carbon", "gemini-3.8-flash", "gemini-3.8-pro", "gemini-3", "gemini-2.5-pro"}, ids(got[0]))
	assert.Equal(t, []string{"claude-opus-5-5", "claude-haiku-4-5", "claude-3-5-sonnet-20241022"}, ids(got[1]))
	assert.Equal(t, assert.AnError, got[2].Err, "a provider's error stays")
	assert.Len(t, in[0].Models, 6, "the input isn't changed")
}

// The providers not set up are offered, unless an organization keeps the
// person's own out (MOD-06).
func TestNotSetUp(t *testing.T) {
	cfg := config.DefaultConfig()
	listed := []ProviderModels{{Provider: "gemini"}, {Provider: "ollama"}}
	assert.Equal(t, []string{"anthropic", "openai"}, NotSetUp(cfg, listed))
	cfg.Enterprise.URL = "https://blitz.example.com"
	assert.Empty(t, NotSetUp(cfg, listed))
	cfg.Org.Policy.Models.OwnProviders = true
	assert.Equal(t, []string{"gemini", "anthropic", "openai", "ollama"}, NotSetUp(cfg, nil))
}

// Ollama is listed when its base_url is set, or when it answers on this
// machine, unless the organization keeps the person's own out (MOD-03).
func TestListModelsFindsOllama(t *testing.T) {
	was := ollamaAnswers
	t.Cleanup(func() { ollamaAnswers = was })
	// A fake Ollama: its OpenAI-compatible list, at an address set the way
	// older settings set it ([llm.openai] base_url, used for Ollama).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"qwen3:14b","object":"model"}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	providers := func(cfg *config.Config) []string {
		var out []string
		for _, pm := range ListModels(context.Background(), cfg) {
			out = append(out, pm.Provider)
		}
		return out
	}
	ollamaAnswers = func(context.Context) bool { return false }
	cfg := config.DefaultConfig()
	cfg.LLM.Gemini.APIKey = ""
	assert.NotContains(t, providers(cfg), "ollama")

	cfg.LLM.Ollama.BaseURL = "http://127.0.0.1:1/v1"
	assert.Contains(t, ConfiguredProviders(cfg), "ollama", "a base_url sets it up")

	cfg.LLM.Ollama.BaseURL = ""
	cfg.LLM.OpenAI.BaseURL = srv.URL + "/v1"
	ollamaAnswers = func(context.Context) bool { return true }
	got := ListModels(context.Background(), cfg)
	i := slices.IndexFunc(got, func(pm ProviderModels) bool { return pm.Provider == "ollama" })
	if assert.GreaterOrEqual(t, i, 0, "it answers on this machine") {
		assert.NoError(t, got[i].Err)
		assert.Equal(t, "qwen3:14b", got[i].Models[0].ID)
	}
	assert.Equal(t, 1, countOf(providers(cfg), "ollama"))

	cfg.Enterprise.URL = "https://blitz.example.com"
	assert.NotContains(t, providers(cfg), "ollama", "the organization's models only")
}

func countOf(list []string, v string) int {
	return len(slices.DeleteFunc(slices.Clone(list), func(s string) bool { return s != v }))
}

// A major provider's latest models: each family's newest, stable over a
// preview or snapshot, of the families within one major version of the
// newest (spec_061_models MOD-43).
func TestLatestModels(t *testing.T) {
	ids := func(ms []ListedModel) []string {
		var out []string
		for _, m := range ms {
			out = append(out, m.ID)
		}
		return out
	}
	listed := func(ids ...string) []ListedModel {
		var out []ListedModel
		for _, id := range ids {
			out = append(out, ListedModel{ID: id})
		}
		return out
	}
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "gemini", in: []string{"gemini-4-carbon", "gemini-4-argon", "gemini-3.8-pro", "gemini-3.8-flash", "gemini-3.5-flash", "gemini-3.5-flash-lite", "gemini-2.5-pro", "gemini-2.5-flash-preview-05-20", "gemini-2.0-flash"},
			want: []string{"gemini-4-carbon", "gemini-4-argon", "gemini-3.8-pro", "gemini-3.8-flash", "gemini-3.5-flash-lite"}},
		{name: "a preview and its stable model", in: []string{"gemini-4-carbon-preview-09-2026", "gemini-4-carbon", "gemini-4-argon-exp"}, want: []string{"gemini-4-carbon", "gemini-4-argon-exp"}},
		{name: "claude", in: []string{"claude-opus-5-5", "claude-opus-5", "claude-sonnet-5-5", "claude-sonnet-5", "claude-haiku-4-5", "claude-haiku-4-5-20251001", "claude-3-5-sonnet-20241022", "claude-opus-4-8"},
			want: []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-4-5"}},
		{name: "openai", in: []string{"gpt-6.1-sol", "gpt-6.1-sol-mini", "gpt-6-sol", "gpt-5", "gpt-5-mini", "gpt-4o", "gpt-4o-2024-08-06", "o3", "chatgpt-4o-latest"},
			want: []string{"gpt-6.1-sol", "gpt-6.1-sol-mini", "gpt-5", "gpt-5-mini"}},
		{name: "names without versions", in: []string{"auto", "gemini-4-carbon"}, want: []string{"auto", "gemini-4-carbon"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assert.Equal(t, c.want, ids(LatestModels(listed(c.in...)))) })
	}
}

// The menus list each provider's chosen models, else the latest for the
// major ones and all for the rest, and the model in use (MOD-44).
func TestMenuModels(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.Provider = "gemini"
	cfg.LLM.OpenAI.Models = []string{"gpt-5-mini", "gpt-retired"}
	pms := []ProviderModels{
		{Provider: "gemini", Models: []ListedModel{{ID: "gemini-4-carbon"}, {ID: "gemini-3.8-flash"}, {ID: "gemini-2.0-flash"}}},
		{Provider: "openai", Models: []ListedModel{{ID: "gpt-6.1-sol"}, {ID: "gpt-5-mini", Price: &config.ModelPrice{InputPerMTok: 1}}}},
		{Provider: "ollama", Models: []ListedModel{{ID: "qwen3:1.7b"}, {ID: "llama2:7b"}}},
	}
	got := MenuModels(cfg, pms, "gemini-2.0-flash")
	ids := func(pm ProviderModels) (out []string) {
		for _, m := range pm.Models {
			out = append(out, m.ID)
		}
		return out
	}
	assert.Equal(t, []string{"gemini-4-carbon", "gemini-3.8-flash", "gemini-2.0-flash"}, ids(got[0]), "the latest, and the one in use")
	assert.Equal(t, []string{"gpt-5-mini", "gpt-retired"}, ids(got[1]), "the chosen ones, in their order")
	assert.NotNil(t, got[1].Models[0].Price, "a chosen model keeps what the provider says of it")
	assert.Equal(t, []string{"qwen3:1.7b", "llama2:7b"}, ids(got[2]), "Ollama's, all")
}

// Only the active providers are listed; with no list, every one set up
// (MOD-40).
func TestActiveProviders(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.Gemini.APIKey, cfg.LLM.OpenAI.APIKey, cfg.LLM.Anthropic.APIKey = "g", "o", "a"
	assert.Equal(t, []string{"gemini", "anthropic", "openai"}, ConfiguredProviders(cfg))
	cfg.LLM.Providers = []string{"openai", "ollama"}
	assert.Equal(t, []string{"openai"}, ConfiguredProviders(cfg), "active and set up")
}
