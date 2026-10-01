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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/auth"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// staticToken is a token provider for tests.
type staticToken string

func (s staticToken) Token(context.Context) (*auth.Token, error) {
	return &auth.Token{Value: string(s), Type: "Bearer"}, nil
}

// fakeADC makes googleCredentials return a fixed token and quota project,
// or err.
func fakeADC(t *testing.T, err error) {
	t.Helper()
	old := googleCredentials
	t.Cleanup(func() { googleCredentials = old })
	googleCredentials = func() (*auth.Credentials, error) {
		if err != nil {
			return nil, err
		}
		return auth.NewCredentials(&auth.CredentialsOptions{
			TokenProvider:          staticToken("adc-token"),
			QuotaProjectIDProvider: auth.CredentialsPropertyFunc(func(context.Context) (string, error) { return "quota-p", nil }),
		}), nil
	}
}

// Gemini goes to the Gemini API with a key, or to Vertex AI with
// Application Default Credentials.
func TestGeminiClientConfig(t *testing.T) {
	pol := retryPolicy{maxRetries: 1, stall: time.Minute}
	fakeADC(t, nil)
	for _, tc := range []struct {
		name     string
		gemini   config.GeminiConfig
		env      map[string]string
		backend  genai.Backend
		apiKey   string
		project  string
		location string
		err      string
	}{
		{name: "an API key", gemini: config.GeminiConfig{APIKey: "AIza"}, backend: genai.BackendGeminiAPI, apiKey: "AIza"},
		{name: "an API key, said so", gemini: config.GeminiConfig{Auth: config.AuthAPIKey, APIKey: "AIza"}, backend: genai.BackendGeminiAPI, apiKey: "AIza"},
		{name: "an API key ignores Vertex AI's project", gemini: config.GeminiConfig{APIKey: "AIza", ProjectID: "p", Location: "us-central1"}, backend: genai.BackendGeminiAPI, apiKey: "AIza"},
		{name: "no key leaves genai to the environment", gemini: config.GeminiConfig{ProjectID: "p"}, backend: genai.BackendUnspecified, project: "p"},
		{
			name:    "ADC with a project and location",
			gemini:  config.GeminiConfig{Auth: config.AuthADC, APIKey: "AIza-ignored", ProjectID: "p", Location: "us-central1"},
			backend: genai.BackendVertexAI, project: "p", location: "us-central1",
		},
		{
			name:    "ADC from the environment, in the global location",
			gemini:  config.GeminiConfig{Auth: config.AuthADC},
			env:     map[string]string{"GOOGLE_CLOUD_PROJECT": "env-p"},
			backend: genai.BackendVertexAI, project: "env-p", location: "global",
		},
		{name: "ADC without a project", gemini: config.GeminiConfig{Auth: config.AuthADC}, err: "needs a Google Cloud project"},
		{name: "an unknown method", gemini: config.GeminiConfig{Auth: "oauth"}, err: "unknown [llm.gemini] auth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOOGLE_CLOUD_PROJECT", tc.env["GOOGLE_CLOUD_PROJECT"])
			t.Setenv("GOOGLE_CLOUD_LOCATION", "")
			cc, err := geminiClientConfig(context.Background(), tc.gemini, pol)
			if tc.err != "" {
				assert.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.backend, cc.Backend)
			assert.Equal(t, tc.apiKey, cc.APIKey)
			assert.Equal(t, tc.project, cc.Project)
			assert.Equal(t, tc.location, cc.Location)
		})
	}
}

// With ADC, requests carry the credentials' token and quota project, on
// Blitz's own HTTP client, which genai doesn't sign itself; credentials
// that can't be found fail when the model is built.
func TestGeminiADCSignsRequests(t *testing.T) {
	pol := retryPolicy{maxRetries: 0, stall: time.Minute}
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]}}]}`)
	}))
	defer srv.Close()

	fakeADC(t, nil)
	cc, err := geminiClientConfig(context.Background(), config.GeminiConfig{Auth: config.AuthADC, ProjectID: "p"}, pol)
	require.NoError(t, err)
	cc.HTTPOptions.BaseURL = srv.URL
	client, err := genai.NewClient(context.Background(), cc)
	require.NoError(t, err)
	res, err := client.Models.GenerateContent(context.Background(), "gemini-3.8-flash", genai.Text("hi"), nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", res.Text())
	assert.Equal(t, "Bearer adc-token", got.Get("Authorization"))
	assert.Equal(t, "quota-p", got.Get("X-Goog-User-Project"))

	fakeADC(t, errors.New("could not find default credentials"))
	_, err = geminiClientConfig(context.Background(), config.GeminiConfig{Auth: config.AuthADC, ProjectID: "p"}, pol)
	assert.ErrorContains(t, err, "gcloud auth application-default login")
}

// buildProviderModel picks the provider's API, or, with none named, the
// first provider with credentials (Ollama on this machine when there are
// none).
func TestBuildProviderModel(t *testing.T) {
	fakeADC(t, nil)
	t.Setenv("GOOGLE_CLOUD_PROJECT", "p")
	for _, tc := range []struct {
		name     string
		provider string
		model    string
		cfg      func(*config.Config)
		want     string // providerOf the model
		wantName string
		err      string
	}{
		{name: "gemini", provider: "gemini", model: "gemini-x", cfg: func(c *config.Config) { c.LLM.Gemini.APIKey = "AIza" }, want: "gemini"},
		{name: "gemini bad auth", provider: "gemini", cfg: func(c *config.Config) { c.LLM.Gemini.Auth = "magic" }, err: "unknown [llm.gemini] auth"},
		{name: "openai key command fails", provider: "openai", cfg: func(c *config.Config) { c.LLM.OpenAI = config.OpenAIConfig{APIKeyCommand: "exit 1"} }, err: "[llm.openai] api_key_command failed"},
		{name: "claude on vertex", provider: "vertex-anthropic", model: "claude-x", want: "anthropic"},
		{name: "azure without a model", provider: "azure", err: "[llm.azure] model"},
		{name: "none: gemini key", model: "gemini-x", cfg: func(c *config.Config) { c.LLM.Gemini.APIKey = "AIza" }, want: "gemini"},
		{
			name: "none: anthropic key, its model", model: "ignored",
			cfg: func(c *config.Config) {
				c.LLM.Gemini.APIKey, c.Blitz.DefaultModel = "", ""
				c.LLM.Anthropic.APIKey, c.LLM.Anthropic.Model = "sk-ant", "claude-y"
			},
			want: "anthropic", wantName: "claude-y",
		},
		{name: "none: openai address", model: "gpt-x", cfg: func(c *config.Config) { c.LLM.OpenAI.BaseURL = "http://127.0.0.1:1/v1" }, want: "openai"},
		{name: "none: ollama", model: "llama", want: "openai"},
		{name: "unknown with a gemini key", provider: "mystery", model: "gemini-x", cfg: func(c *config.Config) { c.LLM.Gemini.APIKey = "AIza" }, want: "gemini"},
		{name: "unknown", provider: "mystery", err: "unsupported or unconfigured LLM provider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.LLM.Gemini, cfg.LLM.Anthropic, cfg.LLM.OpenAI = config.GeminiConfig{}, config.AnthropicConfig{}, config.OpenAIConfig{}
			if tc.cfg != nil {
				tc.cfg(cfg)
			}
			m, err := buildProviderModel(context.Background(), cfg, tc.provider, tc.model)
			if tc.err != "" {
				assert.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, providerOf(m))
			if tc.wantName != "" {
				assert.Equal(t, tc.wantName, m.Name())
			}
		})
	}
}

// A fallback model that can't be built is skipped, and NewModelRef builds
// one reference on its own.
func TestNewModelSkipsUnavailableFallbacks(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.Provider = "openai"
	cfg.LLM.OpenAI = config.OpenAIConfig{APIKey: "sk", BaseURL: "http://127.0.0.1:1/v1"}
	cfg.LLM.FallbackModels = []string{"gemini/gemini-x", "openai/gpt-backup"}
	cfg.LLM.Gemini = config.GeminiConfig{Auth: "magic"}
	m, err := NewModel(context.Background(), cfg, "openai/gpt-x")
	require.NoError(t, err)
	fb, ok := m.(*fallbackModel)
	require.True(t, ok, "a chain of models: %T", m)
	assert.Len(t, fb.chain, 2, "the primary and the openai backup, without gemini")

	ref, err := NewModelRef(context.Background(), cfg, "gpt-y")
	require.NoError(t, err)
	assert.Equal(t, "gpt-y", ref.Name())
}

// Which providers count as configured.
func TestConfiguredProviders(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM = config.LLMConfig{Provider: "ollama"}
	cfg.LLM.Gemini.APIKeyCommand = "echo k"
	cfg.LLM.Azure.Resource = "r"
	assert.Equal(t, []string{"gemini", "ollama", "azure"}, ConfiguredProviders(cfg))
}

// Each provider's listing: Gemini's API (models that generate content),
// the one configured Azure deployment, and failures reported per provider.
func TestListModelsProviders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.Contains(r.URL.Path, "/models") {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"models":[
			{"name":"models/gemini-x","supportedGenerationMethods":["generateContent"]},
			{"name":"models/embedder","supportedGenerationMethods":["embedContent"]},
			{"name":"models/plain"}]}`)
	}))
	defer srv.Close()
	t.Setenv("GOOGLE_GEMINI_BASE_URL", srv.URL)

	cfg := config.DefaultConfig()
	cfg.LLM = config.LLMConfig{}
	cfg.LLM.Gemini.APIKeyCommand = "echo gem-key"
	cfg.LLM.Azure.Model = "gpt-deploy"
	cfg.Pricing = map[string]config.ModelPrice{"gemini-x": {InputPerMTok: 1}}
	got := ListModels(context.Background(), cfg, "gemini", "azure", "bedrock")
	require.Len(t, got, 3)
	require.NoError(t, got[0].Err)
	var ids []string
	for _, m := range got[0].Models {
		ids = append(ids, m.ID)
	}
	assert.Equal(t, []string{"gemini-x", "plain"}, ids)
	assert.NotNil(t, got[0].Models[0].Price, "gemini-x has a price")
	assert.Equal(t, "gpt-deploy", got[1].Models[0].ID)
	assert.Contains(t, got[1].Note, "azure's console")
	assert.Empty(t, got[2].Models, "no bedrock model configured")

	for _, tc := range []struct {
		name, provider string
		cfg            func(*config.LLMConfig)
	}{
		{name: "gemini", provider: "gemini", cfg: func(c *config.LLMConfig) { c.Gemini.Auth = "magic" }},
		{name: "gemini key command", provider: "gemini", cfg: func(c *config.LLMConfig) { c.Gemini.APIKeyCommand = "exit 1" }},
		{name: "openai", provider: "openai", cfg: func(c *config.LLMConfig) { c.OpenAI.APIKeyCommand = "exit 1" }},
		{name: "anthropic", provider: "anthropic", cfg: func(c *config.LLMConfig) { c.Anthropic.Auth = "magic" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := config.DefaultConfig()
			c.LLM = config.LLMConfig{}
			tc.cfg(&c.LLM)
			_, err := listProvider(context.Background(), c, tc.provider)
			assert.Error(t, err)
		})
	}
}
