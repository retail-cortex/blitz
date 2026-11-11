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
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// The other APIs' answers, as their documentation shows them.
const (
	gwChatText   = `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"llama-4","choices":[{"index":0,"message":{"role":"assistant","content":"One cell, c1, alive."},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`
	gwGeminiText = `{"candidates":[{"content":{"role":"model","parts":[{"text":"One cell, c1, alive."}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4,"totalTokenCount":7}}`
	gwGeminiCall = `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"read_file","args":{"path":"README.md"}},"thoughtSignature":"c2ln"}]},"finishReason":"STOP"}]}`
)

// Each model's requests are written in its API and go to that API's path
// under base_url, signed alike; nothing comes from the environment
// (MDL-112, MDL-113).
func TestAPIProviderRoutes(t *testing.T) {
	for _, k := range []string{"GOOGLE_API_KEY", "GEMINI_API_KEY", "OPENAI_API_KEY"} {
		t.Setenv(k, "key-from-the-environment")
	}
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "us-central1")
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "true")
	cases := map[string]struct {
		model, reply string
		edit         func(*config.APIConfig)
		path         string
		provider     string
		body         string // a field only that API's body has
	}{
		"chat completions": {"llama-4", gwChatText, func(c *config.APIConfig) { c.APIs = map[string]string{"*": config.GatewayChatCompletions} },
			"/gw/chat/completions", "openai", "messages"},
		"anthropic-compatible": {"glm-5", gwClaudeText, func(c *config.APIConfig) {
			c.APIs = map[string]string{"glm-5": config.GatewayMessages}
			c.Paths = map[string]string{config.GatewayMessages: "/v1/messages"}
		}, "/gw/v1/messages", "anthropic", "max_tokens"},
		"gemini, by its name": {"gemini-3-pro", gwGeminiText, nil, "/gw/models/gemini-3-pro:generateContent", "gemini", "contents"},
		"vertex": {"gemini-3-pro", gwGeminiText, func(c *config.APIConfig) { c.APIs = map[string]string{"gemini-3-pro": config.GatewayVertex} },
			"/gw/publishers/google/models/gemini-3-pro:generateContent", "gemini", "contents"},
		"vertex, the gateway's path": {"gemini-3-pro", gwGeminiText, func(c *config.APIConfig) {
			c.APIs = map[string]string{"*": config.GatewayVertex}
			c.Paths = map[string]string{config.GatewayVertex: "/v1/projects/p1/locations/global/publishers/google/models/{model}:{method}"}
		}, "/gw/v1/projects/p1/locations/global/publishers/google/models/gemini-3-pro:generateContent", "gemini", "contents"},
		"responses, elsewhere": {"gpt-6-luna", gwText, func(c *config.APIConfig) {
			c.Paths = map[string]string{config.GatewayResponses: "/openai/v1/responses"}
		},
			"/gw/openai/v1/responses", "openai", "input"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			g := newFakeGateway(t, [2]any{http.StatusOK, tc.reply})
			edits := []func(*config.APIConfig){func(c *config.APIConfig) { c.BaseURL += "?tenant=retail" }}
			if tc.edit != nil {
				edits = append(edits, tc.edit)
			}
			m := apiTestModelNamed(t, g, tc.model, "echo tok-routes-12345", 0, edits...)
			assert.Equal(t, tc.provider, providerOf(m))
			text, _, err := callAPI(context.Background(), m, helloRequest())
			require.NoError(t, err)
			assert.Equal(t, "One cell, c1, alive.", text)
			got := g.requests()
			require.Len(t, got, 1, "the model call alone")
			r := got[0]
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, tc.path, r.Path)
			assert.Equal(t, []string{"retail"}, r.Query["tenant"], "base_url's query")
			assert.Empty(t, r.Query["key"])
			assert.Equal(t, "tok-routes-12345", r.Header.Get("X-Api-Key"))
			assert.Equal(t, "retail", r.Header.Get("X-Tenant"))
			for _, h := range []string{"Authorization", "X-Goog-Api-Key"} {
				assert.Empty(t, r.Header.Get(h), h)
			}
			assert.NotNil(t, r.Body[tc.body], "%s's body", name)
		})
	}
}

// Gemini's API through the gateway: a stream goes to streamGenerateContent
// as server-sent events; tool calls keep their signatures; a rejected
// token is fetched again; errors are the gateway's (MDL-105, MDL-113).
func TestAPIProviderGemini(t *testing.T) {
	t.Run("streamed", func(t *testing.T) {
		g := newFakeGateway(t, [2]any{http.StatusOK, "data: " + gwGeminiText + "\n\n"})
		m := apiTestModelNamed(t, g, "gemini-3-pro", "echo tok-gemini-12345", 0)
		var text string
		for resp, err := range m.GenerateContent(context.Background(), helloRequest(), true) {
			require.NoError(t, err)
			if resp != nil && resp.Content != nil && !resp.Partial {
				for _, p := range resp.Content.Parts {
					text += p.Text
				}
			}
		}
		assert.Equal(t, "One cell, c1, alive.", text)
		r := g.requests()[0]
		assert.Equal(t, "/gw/models/gemini-3-pro:streamGenerateContent", r.Path)
		assert.Equal(t, []string{"sse"}, r.Query["alt"])
	})

	t.Run("tool call", func(t *testing.T) {
		g := newFakeGateway(t, [2]any{http.StatusOK, gwGeminiCall})
		req := helloRequest()
		req.Config.Tools = []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "read_file", Description: "Reads a file."}}}}
		_, calls, err := callAPI(context.Background(), apiTestModelNamed(t, g, "gemini-3-pro", "echo tok-gemini-12345", 0), req)
		require.NoError(t, err)
		require.Len(t, calls, 1)
		assert.Equal(t, "read_file", calls[0].Name)
		assert.Equal(t, map[string]any{"path": "README.md"}, calls[0].Args)
	})

	t.Run("rejected token", func(t *testing.T) {
		g := newFakeGateway(t, [2]any{http.StatusUnauthorized, `{"error":{"code":401,"message":"expired","status":"UNAUTHENTICATED"}}`}, [2]any{http.StatusOK, gwGeminiText})
		script, runs := tokenScript(t)
		text, _, err := callAPI(context.Background(), apiTestModelNamed(t, g, "gemini-3-pro", script, 0), helloRequest())
		require.NoError(t, err)
		assert.Equal(t, "One cell, c1, alive.", text)
		assert.Equal(t, 2, runs())
		assert.Equal(t, "gwtok-2", g.requests()[1].Header.Get("X-Api-Key"))
	})

	t.Run("error", func(t *testing.T) {
		g := newFakeGateway(t, [2]any{http.StatusBadRequest, `{"error":{"code":400,"message":"model not served here","status":"INVALID_ARGUMENT"}}`})
		_, _, err := callAPI(context.Background(), apiTestModelNamed(t, g, "gemini-3-pro", "echo tok-gemini-12345", 0), helloRequest())
		require.Error(t, err)
		assert.Equal(t, "[llm.api] HTTP 400 Bad Request: model not served here", err.Error())
		assert.NotContains(t, err.Error(), "tok-gemini-12345")
	})
}

// The gateway's models are what it lists at models_path, asked with the
// headers and the token; any shape servers give it; the configured models
// when it lists none or can't (MDL-114).
func TestAPIModelList(t *testing.T) {
	serve := func(t *testing.T, status int, body string) (*httptest.Server, *http.Request) {
		var got http.Request
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = *r.Clone(context.Background())
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(s.Close)
		return s, &got
	}
	list := func(t *testing.T, base string, edit ...func(*config.APIConfig)) ([]string, error) {
		c := config.APIConfig{BaseURL: base + "/gw?tenant=retail", Model: "gpt-6-luna", Models: []string{"claude-sonnet-5-5"}, Headers: map[string]string{"X-Api-Key": "Bearer " + config.TokenPlaceholder}, TokenScript: "echo tok-list-12345"}
		for _, e := range edit {
			e(&c)
		}
		return listAPIModels(context.Background(), c, policyFrom(config.LLMConfig{}))
	}
	shapes := map[string]string{
		"openai":    `{"object":"list","data":[{"id":"gpt-6-luna","object":"model"},{"id":"claude-sonnet-5-5","object":"model"}]}`,
		"anthropic": `{"data":[{"id":"gpt-6-luna","type":"model"},{"id":"claude-sonnet-5-5","type":"model"}],"has_more":false}`,
		"gemini":    `{"models":[{"name":"models/gpt-6-luna","supportedGenerationMethods":["generateContent"]},{"name":"models/text-embedding-5","supportedGenerationMethods":["embedContent"]},{"name":"publishers/google/models/claude-sonnet-5-5"}]}`,
		"ollama":    `{"models":[{"name":"gpt-6-luna","model":"gpt-6-luna"},{"name":"claude-sonnet-5-5","model":"claude-sonnet-5-5"}]}`,
		"names":     `["gpt-6-luna","claude-sonnet-5-5","gpt-6-luna"]`,
	}
	for name, body := range shapes {
		t.Run(name, func(t *testing.T) {
			s, got := serve(t, http.StatusOK, body)
			ids, err := list(t, s.URL)
			require.NoError(t, err)
			assert.Equal(t, []string{"gpt-6-luna", "claude-sonnet-5-5"}, ids)
			assert.Equal(t, http.MethodGet, got.Method)
			assert.Equal(t, "/gw/models", got.URL.Path)
			assert.Equal(t, "retail", got.URL.Query().Get("tenant"))
			assert.Equal(t, "Bearer tok-list-12345", got.Header.Get("X-Api-Key"))
			assert.Empty(t, got.Header.Get("Authorization"))
		})
	}

	t.Run("its own path", func(t *testing.T) {
		s, got := serve(t, http.StatusOK, shapes["openai"])
		_, err := list(t, s.URL, func(c *config.APIConfig) { c.ModelsPath = "/v1/models" })
		require.NoError(t, err)
		assert.Equal(t, "/gw/v1/models", got.URL.Path)
	})

	t.Run("none", func(t *testing.T) {
		ids, err := list(t, "http://127.0.0.1:1", func(c *config.APIConfig) { c.ModelsPath = config.NoModelsList })
		require.NoError(t, err)
		assert.Equal(t, []string{"gpt-6-luna", "claude-sonnet-5-5"}, ids, "model, then models")

		ids, err = list(t, "http://127.0.0.1:1", func(c *config.APIConfig) {
			c.ModelsPath = config.NoModelsList
			c.APIs = map[string]string{"llama-4": config.GatewayChatCompletions, "glm-5": config.GatewayMessages, "gpt-6-luna": config.GatewayResponses, "*": config.GatewayResponses}
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"gpt-6-luna", "claude-sonnet-5-5", "glm-5", "llama-4"}, ids, "then the models apis names, sorted, * aside")
	})

	failures := map[string]struct {
		status int
		body   string
		want   string
	}{
		"not found":     {http.StatusNotFound, `{"error":{"message":"no route"}}`, "[llm.api] HTTP 404 Not Found: no route"},
		"refused token": {http.StatusUnauthorized, `{}`, "[llm.api] the API rejected the token (HTTP 401)"},
		"not JSON":      {http.StatusOK, `<html>`, "isn't JSON: set models_path, or none"},
		"empty":         {http.StatusOK, `{"data":[]}`, "lists no models"},
	}
	for name, tc := range failures {
		t.Run(name, func(t *testing.T) {
			s, _ := serve(t, tc.status, tc.body)
			ids, err := list(t, s.URL)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Equal(t, []string{"gpt-6-luna", "claude-sonnet-5-5"}, ids, "the configured models, beside the error")
		})
	}
}

// The model list and the gateway's models share one token: listing
// doesn't run the script again for a call, nor a call for a listing
// (MDL-103, MDL-114).
func TestAPIModelListSharesToken(t *testing.T) {
	g := newFakeGateway(t, [2]any{http.StatusOK, `{"data":[{"id":"gpt-5"}]}`}, [2]any{http.StatusOK, `{"data":[{"id":"gpt-5"}]}`})
	script, runs := tokenScript(t)
	c := config.APIConfig{BaseURL: g.URL + "/gw", Headers: map[string]string{"X-Api-Key": config.TokenPlaceholder}, TokenScript: script}
	pol := policyFrom(config.LLMConfig{})
	for range 2 {
		_, err := listAPIModels(context.Background(), c, pol)
		require.NoError(t, err)
	}
	m, err := newAPIModel(context.Background(), c, "gpt-5", pol)
	require.NoError(t, err)
	_, _, err = callAPI(context.Background(), m, helloRequest())
	require.NoError(t, err)
	assert.Equal(t, 1, runs())
	for _, r := range g.requests() {
		assert.Equal(t, "gwtok-1", r.Header.Get("X-Api-Key"), r.Path)
	}
}
