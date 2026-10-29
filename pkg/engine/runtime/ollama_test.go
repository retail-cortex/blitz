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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/retail-cortex/blitz/pkg/config"
)

// withBase points a client at url with key, sending X-Case for the test
// server to answer by.
func withBase(url, key, c string) []option.RequestOption {
	return []option.RequestOption{option.WithBaseURL(url), option.WithAPIKey(key), option.WithMaxRetries(0), option.WithHeader("X-Case", c)}
}

func hiRequest() *model.LLMRequest {
	return &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}}
}

// With no model chosen, Ollama's first model is used, found on the first
// call; none, or Ollama not answering, says what to do.
func TestOllamaAuto(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ids     []string
		listErr error
		want    string // the model used, or the error
	}{
		{"the first it has", []string{"qwen2.5-coder:7b", "gemma3:12b"}, nil, "qwen2.5-coder:7b"},
		{"none pulled", nil, nil, "has no models: pull one"},
		{"not running", nil, errors.New("connection refused"), "open the Ollama app or run `ollama serve`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newOllamaAuto("ollama", "http://localhost:11434/v1", nil)
			assert.Equal(t, "ollama", a.Name(), "before the first call")
			a.list = func(context.Context) ([]string, error) { return tc.ids, tc.listErr }
			a.build = func(_ context.Context, name string) (model.LLM, error) {
				return NewMockLLM(name, genai.NewContentFromText("hello", genai.RoleModel)), nil
			}
			var got []string
			var err error
			for resp, e := range a.GenerateContent(context.Background(), hiRequest(), false) {
				if e != nil {
					err = e
					break
				}
				got = append(got, resp.Content.Parts[0].Text)
			}
			if len(tc.ids) == 0 {
				assert.ErrorContains(t, err, tc.want)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []string{"hello"}, got)
			assert.Equal(t, tc.want, a.Name(), "kept")
		})
	}
}

// The provider's own message reaches the error, which the SDK leaves out;
// Ollama's 404 says which model it lacks and what to do; the key never
// shows.
func TestProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-Case") == "404" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"message":"model 'gpt-4o' not found","type":"not_found_error"}}`)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"Incorrect API key provided: sk-secret-key-123456","type":"invalid_request_error"}}`)
	}))
	defer srv.Close()
	call := func(c string) error {
		t.Helper()
		client := openai.NewClient(withBase(srv.URL, "sk-secret-key-123456", c)...)
		_, err := client.Models.Get(context.Background(), "gpt-4o")
		require.Error(t, err)
		return err
	}

	err := providerError(call("404"), "ollama", "gpt-4o", "ollama")
	assert.ErrorContains(t, err, `Ollama doesn't have the model "gpt-4o"`)
	assert.ErrorContains(t, err, "ollama pull gpt-4o")
	var apiErr *openai.Error
	assert.ErrorAs(t, err, &apiErr, "still the SDK's error")

	err = providerError(call("401"), "openai", "gpt-4o", "sk-secret-key-123456")
	assert.ErrorContains(t, err, "Incorrect API key provided")
	assert.NotContains(t, err.Error(), "sk-secret-key-123456")

	plain := errors.New("boom")
	assert.Equal(t, plain, providerError(plain, "openai", "m", ""), "not an API error")
	assert.Equal(t, plain, providerError(plain, "", "m", ""), "no provider")
}

// Ollama's model is its own, else one older settings set in [llm.openai];
// OpenAI's default never reaches it.
func TestOllamaModelName(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.Provider = "ollama"
	assert.Empty(t, cfg.ModelName(), "not gpt-4o")
	cfg.LLM.OpenAI.Model = "qwen3-coder"
	assert.Equal(t, "qwen3-coder", cfg.ModelName(), "older settings")
	cfg.LLM.Ollama.Model = "qwen3:1.7b"
	assert.Equal(t, "qwen3:1.7b", cfg.ModelName())
}

// Against a server speaking Ollama's API: the model list picks the model,
// and the call goes to it, through the text tool-call parsing.
func TestOllamaAutoServer(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			fmt.Fprint(w, `{"object":"list","data":[{"id":"qwen3:8b","object":"model"},{"id":"gemma3:12b","object":"model"}]}`)
		case "/v1/responses":
			var body struct{ Model string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			asked = body.Model
			fmt.Fprint(w, `{"id":"resp_1","model":"qwen3:8b","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	a := newOllamaAuto("ollama", srv.URL+"/v1", []option.RequestOption{option.WithMaxRetries(0)})
	var text string
	for resp, err := range a.GenerateContent(context.Background(), hiRequest(), false) {
		require.NoError(t, err)
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				text += p.Text
			}
		}
	}
	assert.Equal(t, "qwen3:8b", asked, "the first model it lists")
	assert.Equal(t, "hello", text)
	assert.Equal(t, "qwen3:8b", a.Name())
}
