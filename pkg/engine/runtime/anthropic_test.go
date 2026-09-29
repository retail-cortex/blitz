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
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	anthropicconfig "github.com/anthropics/anthropic-sdk-go/config"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// fakeAnthropic is a minimal Messages API: it records request bodies and
// headers and replies with the queued responses in order.
type fakeAnthropic struct {
	mu        sync.Mutex
	requests  []map[string]any
	headers   []http.Header
	responses []func(w http.ResponseWriter)
}

func (f *fakeAnthropic) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.headers = append(f.headers, r.Header.Clone())
	var respond func(http.ResponseWriter)
	if n := len(f.requests) - 1; n < len(f.responses) {
		respond = f.responses[n]
	}
	f.mu.Unlock()
	if respond == nil {
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"no more responses"}}`, 400)
		return
	}
	respond(w)
}

func jsonReply(body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}
}

func newFake(t *testing.T, responses ...func(http.ResponseWriter)) (*fakeAnthropic, []option.RequestOption) {
	t.Helper()
	f, opts := newFakeWithoutKey(t, responses...)
	return f, append(opts, option.WithAPIKey("sk-ant-test"))
}

// newFakeWithoutKey is newFake whose options carry no API key.
func newFakeWithoutKey(t *testing.T, responses ...func(http.ResponseWriter)) (*fakeAnthropic, []option.RequestOption) {
	t.Helper()
	f := &fakeAnthropic{responses: responses}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	return f, []option.RequestOption{option.WithBaseURL(srv.URL), option.WithMaxRetries(0), option.WithRequestTimeout(10 * time.Second)}
}

func message(stop string, content string) string {
	return fmt.Sprintf(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","content":[%s],
		"stop_reason":%q,"stop_sequence":null,
		"usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":400,"cache_creation_input_tokens":50}}`, content, stop)
}

func userText(s string) *genai.Content { return genai.NewContentFromText(s, genai.RoleUser) }

func collectResponses(t *testing.T, m model.LLM, req *model.LLMRequest, stream bool) ([]*model.LLMResponse, error) {
	t.Helper()
	var out []*model.LLMResponse
	for r, err := range m.GenerateContent(context.Background(), req, stream) {
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

func TestConvertContents(t *testing.T) {
	contents := []*genai.Content{
		userText("list the files"),
		{Role: genai.RoleModel, Parts: []*genai.Part{
			{Text: "reasoning", Thought: true, ThoughtSignature: []byte("sig-1")},
			{Thought: true, ThoughtSignature: []byte(redactedPrefix + "opaque")},
			{Text: "unsigned thought", Thought: true}, // dropped
			{Text: "Let me look."},
			{FunctionCall: &genai.FunctionCall{ID: "toolu_a", Name: "list_files", Args: map[string]any{"directory": "."}}},
			{FunctionCall: &genai.FunctionCall{ID: "toolu_b", Name: "grep"}},
		}},
		// ADK sends parallel results as separate user contents; they must merge.
		{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "toolu_a", Name: "list_files", Response: map[string]any{"files": []any{"a.go"}}}}}},
		{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "toolu_b", Name: "grep", Response: map[string]any{"error": "bad regex"}}}}},
	}
	msgs, err := convertContents(contents)
	require.NoError(t, err)
	raw, _ := json.Marshal(msgs)
	got := string(raw)
	require.Len(t, msgs, 3, "expected user/assistant/user, got %d messages: %s", len(msgs), got)
	for _, want := range []string{
		`{"signature":"sig-1","thinking":"reasoning","type":"thinking"}`,
		`{"data":"opaque","type":"redacted_thinking"}`,
		`{"id":"toolu_a","input":{"directory":"."},"name":"list_files","type":"tool_use"}`,
		`"input":{},"name":"grep"`,
		`"tool_use_id":"toolu_a"`,
		`"is_error":true`,
	} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, got, want, "missing %s in\n%s", want, got)
		})
	}
	assert.NotContains(t, got, "unsigned thought", "unsigned thinking must be dropped")
	n := len(msgs[2].Content)
	assert.Equal(t, 2, n, "parallel tool results should share one user message, got %d blocks", n)

	// Negative: a conversation must begin with the user.
	_, err = convertContents([]*genai.Content{{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "hi"}}}})
	assert.Error(t, err, "expected error for assistant-first conversation")
}

func TestConvertToolsSchemas(t *testing.T) {
	tools := []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{
		{Name: "json_schema", Description: "raw schema", ParametersJsonSchema: map[string]any{
			"type": "object", "$schema": "https://json-schema.org/draft/2020-12/schema",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []string{"path"}, "additionalProperties": false,
		}},
		{Name: "genai_schema", Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{
			"n":    {Type: genai.TypeInteger, Description: "count"},
			"tags": {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString}},
		}, Required: []string{"n"}}},
		{Name: "no_params"},
	}}}
	out, err := convertTools(tools)
	require.NoError(t, err)
	raw, _ := json.Marshal(out)
	got := string(raw)
	for _, want := range []string{
		`"name":"json_schema"`, `"description":"raw schema"`, `"required":["path"]`, `"additionalProperties":false`,
		`"n":{"description":"count","type":"integer"}`, `"items":{"type":"string"}`, `"type":"array"`,
		`"name":"no_params"`,
	} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, got, want, "missing %s in\n%s", want, got)
		})
	}
	assert.NotContains(t, got, "INTEGER", "schema not normalised: %s", got)
	assert.NotContains(t, got, "$schema", "schema not normalised: %s", got)
}

func TestAnthropicRequestShape(t *testing.T) {
	temp := float32(0.2)
	req := func(name string) *model.LLMRequest {
		return &model.LLMRequest{
			Model:    name,
			Contents: []*genai.Content{userText("hi")},
			Config: &genai.GenerateContentConfig{
				SystemInstruction: genai.NewContentFromText("You are a puppy.", genai.RoleUser),
				Temperature:       &temp,
				MaxOutputTokens:   4096,
			},
		}
	}
	f, opts := newFake(t, jsonReply(message("end_turn", `{"type":"text","text":"a"}`)), jsonReply(message("end_turn", `{"type":"text","text":"b"}`)), jsonReply(message("end_turn", `{"type":"text","text":"c"}`)))
	m := mustAnthropicModel(t, config.AnthropicConfig{APIKey: "sk-ant-test"}, "claude-opus-5", opts...)

	_, err := collectResponses(t, m, req("claude-opus-5"), false)
	require.NoError(t, err)
	r := f.requests[0]
	assert.Equal(t, "claude-opus-5", r["model"], "model/max_tokens: %v", r)
	assert.Equal(t, float64(4096), r["max_tokens"], "model/max_tokens: %v", r)
	_, ok := r["temperature"]
	assert.False(t, ok, "temperature must not be sent to claude-opus-5 (400 on current models)")
	sys, _ := json.Marshal(r["system"])
	assert.Contains(t, string(sys), `"cache_control":{"type":"ephemeral"}`, "system not cached: %s", sys)
	assert.Contains(t, string(sys), "You are a puppy.", "system not cached: %s", sys)
	assert.Equal(t, "default", r["fallbacks"], "default fallbacks not enabled: %v / %q", r["fallbacks"], f.headers[0].Get("Anthropic-Beta"))
	assert.Contains(t, f.headers[0].Get("Anthropic-Beta"), "server-side-fallback-2026-07-01", "default fallbacks not enabled: %v / %q", r["fallbacks"], f.headers[0].Get("Anthropic-Beta"))
	assert.Equal(t, "sk-ant-test", f.headers[0].Get("X-Api-Key"), "api key header missing")

	// Older model: temperature allowed, no fallbacks.
	collectResponses(t, m, req("claude-haiku-4-5"), false)
	assert.NotNil(t, f.requests[1]["temperature"], "haiku request: %v", f.requests[1])
	assert.Nil(t, f.requests[1]["fallbacks"], "haiku request: %v", f.requests[1])

	// Fallbacks off.
	off := mustAnthropicModel(t, config.AnthropicConfig{Fallbacks: "off"}, "claude-opus-5", opts...)
	collectResponses(t, off, req("claude-opus-5"), false)
	assert.Nil(t, f.requests[2]["fallbacks"], "fallbacks should be omitted when off")
}

func mustAnthropicModel(t *testing.T, cfg config.AnthropicConfig, name string, opts ...option.RequestOption) *anthropicModel {
	t.Helper()
	m, err := newAnthropicModel(context.Background(), cfg, name, opts...)
	require.NoError(t, err)
	return m
}

// auth = "oauth" sends an `ant auth login` profile's token, never an API
// key from the settings or the environment.
func TestAnthropicOAuthProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTHROPIC_CONFIG_DIR", dir)
	t.Setenv("ANTHROPIC_PROFILE", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-from-env")
	expires := time.Now().Add(time.Hour)
	for _, p := range []struct{ name, token string }{{"default", "tok-default"}, {"work", "tok-work"}} {
		require.NoError(t, anthropicconfig.SaveProfile(dir, p.name, &anthropicconfig.Config{AuthenticationInfo: anthropicconfig.NewUserOAuthAuthentication("client")}))
		require.NoError(t, anthropicconfig.WriteCredentials(anthropicconfig.ProfileCredentialsPath(dir, p.name), anthropicconfig.Credentials{AccessToken: p.token, RefreshToken: "refresh", ExpiresAt: &expires}))
	}
	req := &model.LLMRequest{Contents: []*genai.Content{userText("hi")}}

	for _, tc := range []struct {
		name    string
		profile string
		token   string
	}{
		{"the active profile", "", "tok-default"},
		{"a named profile", "work", "tok-work"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, opts := newFakeWithoutKey(t, jsonReply(message("end_turn", `{"type":"text","text":"ok"}`)))
			m := mustAnthropicModel(t, config.AnthropicConfig{Auth: config.AuthOAuth, Profile: tc.profile, APIKey: "sk-ant-in-settings"}, "claude-opus-5", opts...)
			_, err := collectResponses(t, m, req, false)
			require.NoError(t, err)
			assert.Equal(t, "Bearer "+tc.token, f.headers[0].Get("Authorization"))
			assert.Empty(t, f.headers[0].Get("X-Api-Key"), "an API key was sent with the OAuth token")
		})
	}

	_, err := newAnthropicModel(context.Background(), config.AnthropicConfig{Auth: config.AuthOAuth, Profile: "missing"}, "claude-opus-5")
	assert.ErrorContains(t, err, "ant auth login")
	_, err = newAnthropicModel(context.Background(), config.AnthropicConfig{Auth: "password"}, "claude-opus-5")
	assert.ErrorContains(t, err, "unknown [llm.anthropic] auth")
}

// auth = "adc" runs Claude on Vertex AI: the request goes to the model's
// Vertex path with Application Default Credentials' token and quota
// project, no API key, and no server-side fallback (Vertex has none).
func TestClaudeOnVertex(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-from-env")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "")
	fakeADC(t, nil)
	var (
		path    string
		headers http.Header
		body    map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, headers = r.URL.Path, r.Header.Clone()
		json.NewDecoder(r.Body).Decode(&body)
		if strings.HasSuffix(r.URL.Path, ":streamRawPredict") {
			sseReply(
				`{"type":"message_start","message":{"id":"msg_v","type":"message","role":"assistant","model":"claude-opus-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"streamed"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`,
				`{"type":"message_stop"}`,
			)(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, message("end_turn", `{"type":"text","text":"from vertex"}`))
	}))
	defer srv.Close()
	pol := retryPolicy{maxRetries: 0, stall: time.Minute}
	cfg := config.AnthropicConfig{Auth: config.AuthADC, ProjectID: "claude-p", APIKey: "sk-ant-in-settings", BaseURL: srv.URL}

	m := mustAnthropicModel(t, cfg, "claude-opus-5", pol.anthropicOptions()...)
	out, err := collectResponses(t, m, &model.LLMRequest{Contents: []*genai.Content{userText("hi")}}, false)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "from vertex", out[0].Content.Parts[0].Text)
	assert.Equal(t, "/v1/projects/claude-p/locations/global/publishers/anthropic/models/claude-opus-5:rawPredict", path)
	assert.Equal(t, "Bearer adc-token", headers.Get("Authorization"))
	assert.Equal(t, "quota-p", headers.Get("X-Goog-User-Project"))
	assert.Empty(t, headers.Get("X-Api-Key"), "an API key was sent to Vertex AI")
	assert.NotContains(t, body, "fallbacks", "server-side fallback isn't on Vertex AI")
	assert.Contains(t, body, "anthropic_version")

	// Streaming goes to the model's streaming path.
	_, err = collectResponses(t, m, &model.LLMRequest{Contents: []*genai.Content{userText("hi")}}, true)
	require.NoError(t, err)
	assert.Equal(t, "/v1/projects/claude-p/locations/global/publishers/anthropic/models/claude-opus-5:streamRawPredict", path)
	assert.Equal(t, "Bearer adc-token", headers.Get("Authorization"))

	for _, tc := range []struct {
		name string
		cfg  config.AnthropicConfig
		adc  error
		err  string
	}{
		{"no project", config.AnthropicConfig{Auth: config.AuthADC}, nil, "anthropic on Vertex AI with Application Default Credentials needs a Google Cloud project"},
		{"no credentials", config.AnthropicConfig{Auth: config.AuthADC, ProjectID: "p"}, errors.New("could not find default credentials"), "gcloud auth application-default login"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeADC(t, tc.adc)
			_, err := newAnthropicModel(context.Background(), tc.cfg, "claude-opus-5")
			assert.ErrorContains(t, err, tc.err)
		})
	}
}

func TestAnthropicResponseConversion(t *testing.T) {
	_, opts := newFake(t,
		jsonReply(message("tool_use", `{"type":"thinking","thinking":"hmm","signature":"sig-9"},{"type":"text","text":"Checking."},{"type":"tool_use","id":"toolu_1","name":"list_files","input":{"recursive":true}}`)),
		jsonReply(message("refusal", `{"type":"text","text":"I can't"}`)),
		jsonReply(message("max_tokens", `{"type":"text","text":"cut"}`)),
	)
	m := mustAnthropicModel(t, config.AnthropicConfig{}, "claude-opus-5", opts...)
	req := &model.LLMRequest{Contents: []*genai.Content{userText("go")}}

	out, err := collectResponses(t, m, req, false)
	require.NoError(t, err, "%v", out)
	require.Len(t, out, 1, "%v %v", out, err)
	r := out[0]
	parts := r.Content.Parts
	require.Len(t, parts, 3, "parts %+v", parts)
	require.True(t, parts[0].Thought, "parts %+v", parts)
	require.Equal(t, "sig-9", string(parts[0].ThoughtSignature), "parts %+v", parts)
	require.Equal(t, "Checking.", parts[1].Text, "parts %+v", parts)
	fc := parts[2].FunctionCall
	assert.NotNil(t, fc, "function call %+v", fc)
	assert.Equal(t, "toolu_1", fc.ID, "function call %+v", fc)
	assert.Equal(t, "list_files", fc.Name, "function call %+v", fc)
	assert.Equal(t, true, fc.Args["recursive"], "function call %+v", fc)
	u := r.UsageMetadata
	assert.Equal(t, int32(550), u.PromptTokenCount, "usage/finish %+v %v", u, r.FinishReason)
	assert.Equal(t, int32(400), u.CachedContentTokenCount, "usage/finish %+v %v", u, r.FinishReason)
	assert.Equal(t, int32(20), u.CandidatesTokenCount, "usage/finish %+v %v", u, r.FinishReason)
	assert.Equal(t, genai.FinishReasonStop, r.FinishReason, "usage/finish %+v %v", u, r.FinishReason)

	out, _ = collectResponses(t, m, req, false)
	assert.Equal(t, genai.FinishReasonSafety, out[0].FinishReason, "refusal not surfaced: %+v", out[0])
	assert.Contains(t, out[0].Content.Parts[len(out[0].Content.Parts)-1].Text, "declined", "refusal not surfaced: %+v", out[0])
	out, _ = collectResponses(t, m, req, false)
	assert.Equal(t, genai.FinishReasonMaxTokens, out[0].FinishReason, "max_tokens finish: %v", out[0].FinishReason)
}

func sseReply(events ...string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			var m map[string]any
			json.Unmarshal([]byte(e), &m)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", m["type"], e)
		}
	}
}

func TestAnthropicStreaming(t *testing.T) {
	_, opts := newFake(t, sseReply(
		`{"type":"message_start","message":{"id":"msg_s","type":"message","role":"assistant","model":"claude-opus-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_s","name":"grep","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"query\":"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"TODO\"}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":42}}`,
		`{"type":"message_stop"}`,
	))
	m := mustAnthropicModel(t, config.AnthropicConfig{}, "claude-opus-5", opts...)
	out, err := collectResponses(t, m, &model.LLMRequest{Contents: []*genai.Content{userText("go")}}, true)
	require.NoError(t, err)
	require.Len(t, out, 3, "expected 2 partials + final, got %d: %+v", len(out), out)
	require.True(t, out[0].Partial, "expected 2 partials + final, got %d: %+v", len(out), out)
	require.True(t, out[1].Partial, "expected 2 partials + final, got %d: %+v", len(out), out)
	require.False(t, out[2].Partial, "expected 2 partials + final, got %d: %+v", len(out), out)
	assert.Equal(t, "Hello", out[0].Content.Parts[0].Text+out[1].Content.Parts[0].Text, "partial text wrong")
	final := out[2]
	assert.Equal(t, "Hello", final.Content.Parts[0].Text, "final text %q", final.Content.Parts[0].Text)
	fc := final.Content.Parts[1].FunctionCall
	assert.NotNil(t, fc, "streamed tool call %+v", fc)
	assert.Equal(t, "toolu_s", fc.ID, "streamed tool call %+v", fc)
	assert.Equal(t, "TODO", fc.Args["query"], "streamed tool call %+v", fc)
	assert.Equal(t, int32(42), final.UsageMetadata.CandidatesTokenCount, "streamed usage %+v", final.UsageMetadata)
	assert.Equal(t, int32(10), final.UsageMetadata.PromptTokenCount, "streamed usage %+v", final.UsageMetadata)
}

func TestAnthropicErrors(t *testing.T) {
	status := func(code int) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			io.WriteString(w, `{"type":"error","error":{"type":"x","message":"nope"}}`)
		}
	}
	_, opts := newFake(t, status(401), status(429), status(529))
	m := mustAnthropicModel(t, config.AnthropicConfig{}, "claude-opus-5", opts...)
	req := &model.LLMRequest{Contents: []*genai.Content{userText("x")}}
	for _, want := range []string{"authentication failed", "rate limited", "API error (529)"} {
		t.Run(want, func(t *testing.T) {
			_, err := collectResponses(t, m, req, false)
			assert.Error(t, err, "want %q, got", want)
			assert.Contains(t, err.Error(), want, "want %q, got %v", want, err)
		})
	}
	// Empty requests are rejected before any network call.
	_, err := collectResponses(t, m, &model.LLMRequest{}, false)
	assert.Error(t, err, "expected error for empty request")
}

func TestModelNameResolution(t *testing.T) {
	cfg := config.DefaultConfig()
	for provider, want := range map[string]string{"gemini": "gemini-3.8-flash", "anthropic": "claude-opus-5", "openai": "gpt-4o", "ollama": "gpt-4o"} {
		t.Run(provider, func(t *testing.T) {
			cfg.LLM.Provider = provider
			got := cfg.ModelName()
			assert.Equal(t, want, got, "%s: ModelName = %q, want %q", provider, got, want)
		})
	}
	cfg.Blitz.DefaultModel = "claude-sonnet-5"
	assert.Equal(t, "claude-sonnet-5", cfg.ModelName(), "default_model should override the provider model")

	cfg = config.DefaultConfig()
	cfg.LLM.Provider = "anthropic"
	m, err := NewModel(context.Background(), cfg, "")
	require.NoError(t, err, "NewModel(anthropic) = %v,", m)
	require.Equal(t, "claude-opus-5", m.Name(), "NewModel(anthropic) = %v, %v", m, err)
	m2, _ := NewModel(context.Background(), cfg, "claude-haiku-4-5")
	assert.Equal(t, "claude-haiku-4-5", m2.Name(), "override ignored")
}

// TestAnthropicEngineToolLoop drives a full tool round trip through the ADK
// engine: the second request must carry the assistant's thinking block with
// its signature and the tool_result for the same tool_use ID.
func TestAnthropicEngineToolLoop(t *testing.T) {
	f, opts := newFake(t,
		jsonReply(message("tool_use", `{"type":"thinking","thinking":"need files","signature":"sig-loop"},{"type":"tool_use","id":"toolu_loop","name":"list_files","input":{}}`)),
		jsonReply(message("end_turn", `{"type":"text","text":"Found them."}`)),
	)
	cfg := config.DefaultConfig()
	cfg.LLM.Provider = "anthropic"
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = filepath.Join(t.TempDir(), "a.json")
	agentReg, _ := agents.NewRegistry()
	skillProv, _ := skills.NewProvider()
	reg, err := tools.NewRegistry(cfg, agentReg, skillProv)
	require.NoError(t, err)
	defer reg.Close()
	llm := mustAnthropicModel(t, cfg.LLM.Anthropic, "claude-opus-5", opts...)
	eng, err := NewEngine(context.Background(), cfg, agentReg, skillProv, reg, llm)
	require.NoError(t, err)

	var text strings.Builder
	err = eng.Execute(context.Background(), "s", "list files", func(ev *session.Event) error {
		if ev.Content != nil && !ev.Partial {
			for _, p := range ev.Content.Parts {
				if !p.Thought {
					text.WriteString(p.Text)
				}
			}
		}
		return nil
	})
	require.NoError(t, err)
	assert.Contains(t, text.String(), "Found them.", "final text %q", text.String())
	require.Len(t, f.requests, 2, "expected 2 API calls, got %d", len(f.requests))
	second, _ := json.Marshal(f.requests[1]["messages"])
	for _, want := range []string{`"signature":"sig-loop"`, `"id":"toolu_loop"`, `"tool_use_id":"toolu_loop"`} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, string(second), want, "second request missing %s:\n%s", want, second)
		})
	}
	tools, _ := json.Marshal(f.requests[0]["tools"])
	assert.Contains(t, string(tools), `"name":"list_files"`, "tools not sent: %s", tools)
	assert.Contains(t, string(tools), `"input_schema"`, "tools not sent: %s", tools)
	u := eng.Usage("s")
	assert.Equal(t, 2, u.Calls, "usage not recorded/priced: %+v", u)
	assert.Equal(t, int64(800), u.Cached, "usage not recorded/priced: %+v", u)
	assert.Equal(t, int64(100), u.CacheWrite, "usage not recorded/priced: %+v", u)
	assert.True(t, u.Priced, "usage not recorded/priced: %+v", u)
}

// Effort goes to output_config.effort on models that take it; a thinking
// budget turns thinking on (at least 1024, below max_tokens, without a
// temperature) or, at 0, off. Models that think adaptively take no budget:
// it turns adaptive thinking on at the effort it stands for (unless one is
// set), and 0 isn't sent to those that always think.
func TestAnthropicReasoning(t *testing.T) {
	temp := float32(0.2)
	req := func(name string, budget *int32) *model.LLMRequest {
		cfg := &genai.GenerateContentConfig{Temperature: &temp, MaxOutputTokens: 4096}
		if budget != nil {
			cfg.ThinkingConfig = &genai.ThinkingConfig{ThinkingBudget: budget}
		}
		return &model.LLMRequest{Model: name, Contents: []*genai.Content{userText("hi")}, Config: cfg}
	}
	cases := []struct {
		model    string
		effort   string
		budget   *int32
		effortIs any
		thinking string
		maxTok   float64
		temp     bool
	}{
		{"claude-opus-5-5", "max", nil, "max", "", 4096, false},
		{"claude-opus-5-5", "minimal", genai.Ptr[int32](0), "low", "", 4096, false},
		{"claude-fable-5", "", genai.Ptr[int32](0), nil, "", 4096, false},
		{"claude-sonnet-5", "", genai.Ptr[int32](0), nil, `{"type":"disabled"}`, 4096, false},
		{"claude-opus-5-5", "", genai.Ptr[int32](8000), "medium", `{"type":"adaptive"}`, 4096, false},
		{"claude-opus-5-5", "high", genai.Ptr[int32](2000), "high", `{"type":"adaptive"}`, 4096, false},
		{"claude-fable-5", "", genai.Ptr[int32](2048), "low", `{"type":"adaptive"}`, 4096, false},
		{"claude-sonnet-5", "", genai.Ptr[int32](100000), "max", `{"type":"adaptive"}`, 4096, false},
		{"claude-opus-4-7", "", genai.Ptr[int32](30000), "high", `{"type":"adaptive"}`, 4096, false},
		{"claude-haiku-4-5", "high", genai.Ptr[int32](100), nil, `{"budget_tokens":1024,"type":"enabled"}`, 4096, false},
		{"claude-haiku-4-5", "", genai.Ptr[int32](8000), nil, `{"budget_tokens":8000,"type":"enabled"}`, 8000 + anthropicDefaultMaxTokens, false},
		{"claude-haiku-4-5", "", nil, nil, "", 4096, true},
		{"claude-3-5-haiku-latest", "high", genai.Ptr[int32](2048), nil, "", 4096, true},
	}
	var replies []func(http.ResponseWriter)
	for range cases {
		replies = append(replies, jsonReply(message("end_turn", `{"type":"text","text":"a"}`)))
	}
	f, opts := newFake(t, replies...)
	m := mustAnthropicModel(t, config.AnthropicConfig{Fallbacks: "off"}, "claude-opus-5-5", opts...)
	for i, c := range cases {
		ctx := context.Background()
		if c.effort != "" {
			ctx = context.WithValue(ctx, effortKey{}, c.effort)
		}
		for _, err := range m.GenerateContent(ctx, req(c.model, c.budget), false) {
			require.NoError(t, err)
		}
		r := f.requests[i]
		var effort any
		if oc, ok := r["output_config"].(map[string]any); ok {
			effort = oc["effort"]
		}
		thinking := ""
		if th, ok := r["thinking"]; ok {
			b, _ := json.Marshal(th)
			thinking = string(b)
		}
		_, hasTemp := r["temperature"]
		assert.Equal(t, c.effortIs, effort, "%s effort=%q budget=%v: effort %v thinking %s max_tokens %v temperature %v", c.model, c.effort, c.budget, effort, thinking, r["max_tokens"], hasTemp)
		assert.Equal(t, c.thinking, thinking, "%s effort=%q budget=%v: effort %v thinking %s max_tokens %v temperature %v", c.model, c.effort, c.budget, effort, thinking, r["max_tokens"], hasTemp)
		assert.Equal(t, c.maxTok, r["max_tokens"], "%s effort=%q budget=%v: effort %v thinking %s max_tokens %v temperature %v", c.model, c.effort, c.budget, effort, thinking, r["max_tokens"], hasTemp)
		assert.Equal(t, c.temp, hasTemp, "%s effort=%q budget=%v: effort %v thinking %s max_tokens %v temperature %v", c.model, c.effort, c.budget, effort, thinking, r["max_tokens"], hasTemp)
	}
}
