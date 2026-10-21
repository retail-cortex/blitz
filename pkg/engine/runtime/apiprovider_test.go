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
	"fmt"
	"io"
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

// The gateway's answers, as its documentation shows them.
const (
	gwText     = `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"One cell, c1, alive."}]}],"usage":{"input_tokens":3,"output_tokens":4}}`
	gwToolCall = `{"status":"completed","output":[{"type":"reasoning","summary":[]},{"type":"function_call","call_id":"call_9","name":"colony_get_cells","arguments":"{}","status":"completed"}],"usage":{"input_tokens":1,"output_tokens":2}}`
	gwFailed   = `{"status":"failed","error":{"message":"quota exceeded"}}`
)

// gwRequest is what the gateway received.
type gwRequest struct {
	Method, Path string
	Query        map[string][]string
	Header       http.Header
	Body         map[string]any
}

// fakeGateway answers with scripted (status, body) pairs, then gwText,
// and records each request.
type fakeGateway struct {
	*httptest.Server
	mu      sync.Mutex
	replies [][2]any // status int, body string
	got     []gwRequest
}

func newFakeGateway(t *testing.T, replies ...[2]any) *fakeGateway {
	g := &fakeGateway{replies: replies}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		g.mu.Lock()
		g.got = append(g.got, gwRequest{r.Method, r.URL.Path, r.URL.Query(), r.Header.Clone(), body})
		status, reply := http.StatusOK, gwText
		if len(g.replies) > 0 {
			status, reply = g.replies[0][0].(int), g.replies[0][1].(string)
			g.replies = g.replies[1:]
		}
		g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGateway) requests() []gwRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]gwRequest{}, g.got...)
}

// tokenScript is a shell script printing gwtok-<n>, n counting its runs.
func tokenScript(t *testing.T) (string, func() int) {
	t.Helper()
	count := filepath.Join(t.TempDir(), "n")
	script := fmt.Sprintf(`n=$(cat %q 2>/dev/null || echo 0); n=$((n+1)); echo $n > %q; echo "gwtok-$n"`, count, count)
	return script, func() int {
		data, _ := os.ReadFile(count)
		var n int
		fmt.Sscan(string(data), &n)
		return n
	}
}

// apiTestModel builds the api provider against g with the token script.
func apiTestModel(t *testing.T, g *fakeGateway, script string, retries int, edit ...func(*config.APIConfig)) model.LLM {
	t.Helper()
	return apiTestModelNamed(t, g, "gpt-5", script, retries, edit...)
}

// apiTestModelNamed is apiTestModel for the model name.
func apiTestModelNamed(t *testing.T, g *fakeGateway, name, script string, retries int, edit ...func(*config.APIConfig)) model.LLM {
	t.Helper()
	c := config.APIConfig{
		BaseURL:     g.URL + "/gw",
		Headers:     map[string]string{"X-Api-Key": config.TokenPlaceholder, "X-Tenant": "retail"},
		TokenScript: script,
	}
	for _, e := range edit {
		e(&c)
	}
	m, err := newAPIModel(context.Background(), c, name, policyFrom(config.LLMConfig{MaxRetries: retries}))
	require.NoError(t, err)
	return m
}

// callAPI runs one call and returns its text, its function calls and its
// error.
func callAPI(ctx context.Context, m model.LLM, req *model.LLMRequest) (string, []*genai.FunctionCall, error) {
	var text string
	var calls []*genai.FunctionCall
	for resp, err := range m.GenerateContent(ctx, req, false) {
		if err != nil {
			return text, calls, err
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.FunctionCall != nil {
					calls = append(calls, p.FunctionCall)
				} else if !p.Thought {
					text += p.Text
				}
			}
		}
	}
	return text, calls, nil
}

func helloRequest() *model.LLMRequest {
	return &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("say hi", genai.RoleUser)},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText("You are terse.", genai.RoleUser),
			MaxOutputTokens:   100,
		},
	}
}

// inSession is a model call's context in session id (sessionCtx, from
// anthropic_test.go).
func inSession(id string) context.Context {
	return sessionCtx{context.Background(), id}
}

// Requests carry the configured headers with the token, never an OpenAI
// setting from the environment, and a body like the gateway's own example:
// not stored, not streamed (MDL-101, MDL-104).
func TestAPIProviderRequests(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-from-env")
	t.Setenv("OPENAI_ORG_ID", "org-from-env")
	t.Setenv("OPENAI_PROJECT_ID", "proj-from-env")
	g := newFakeGateway(t)
	script, runs := tokenScript(t)
	m := apiTestModel(t, g, script, 0)

	text, _, err := callAPI(context.Background(), m, helloRequest())
	require.NoError(t, err)
	assert.Equal(t, "One cell, c1, alive.", text)

	got := g.requests()
	require.Len(t, got, 1)
	r := got[0]
	assert.Equal(t, http.MethodPost, r.Method)
	assert.Equal(t, "/gw/responses", r.Path)
	assert.Equal(t, "gwtok-1", r.Header.Get("X-Api-Key"))
	assert.Equal(t, "retail", r.Header.Get("X-Tenant"))
	assert.Empty(t, r.Header.Get("Authorization"), "the SDK's key or OPENAI_API_KEY reached the gateway")
	assert.Empty(t, r.Header.Get("OpenAI-Organization"))
	assert.Empty(t, r.Header.Get("OpenAI-Project"))
	assert.Equal(t, false, r.Body["store"])
	assert.Equal(t, false, r.Body["stream"])
	assert.Equal(t, "gpt-5", r.Body["model"])
	assert.Equal(t, "You are terse.", r.Body["instructions"])
	assert.EqualValues(t, 100, r.Body["max_output_tokens"])
	assert.NotEmpty(t, r.Body["input"])
	assert.Equal(t, 1, runs())
}

// A session keeps its token; another session runs the script for its own
// (MDL-103).
func TestAPIProviderSessions(t *testing.T) {
	g := newFakeGateway(t)
	script, runs := tokenScript(t)
	m := apiTestModel(t, g, script, 0)
	for _, session := range []string{"s1", "s1", "s2", "s1"} {
		_, _, err := callAPI(inSession(session), m, helloRequest())
		require.NoError(t, err, session)
	}
	var keys []string
	for _, r := range g.requests() {
		keys = append(keys, r.Header.Get("X-Api-Key"))
	}
	assert.Equal(t, []string{"gwtok-1", "gwtok-1", "gwtok-2", "gwtok-1"}, keys)
	assert.Equal(t, 2, runs())
}

// A function call is read from among the gateway's items (reasoning
// ignored), and its result goes back with the same call_id (MDL-106).
func TestAPIProviderToolCall(t *testing.T) {
	g := newFakeGateway(t, [2]any{http.StatusOK, gwToolCall})
	script, _ := tokenScript(t)
	m := apiTestModel(t, g, script, 0)

	text, calls, err := callAPI(context.Background(), m, helloRequest())
	require.NoError(t, err)
	assert.Empty(t, text, "the reasoning item was shown")
	require.Len(t, calls, 1)
	assert.Equal(t, "colony_get_cells", calls[0].Name)
	assert.Equal(t, "call_9", calls[0].ID)

	next := helloRequest()
	next.Contents = append(next.Contents,
		&genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: calls[0]}}},
		&genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
			ID: "call_9", Name: "colony_get_cells", Response: map[string]any{"cells": []any{"c1"}}}}}})
	_, _, err = callAPI(context.Background(), m, next)
	require.NoError(t, err)
	got := g.requests()
	require.Len(t, got, 2)
	input, _ := json.Marshal(got[1].Body["input"])
	assert.Contains(t, string(input), `"type":"function_call_output"`)
	assert.Contains(t, string(input), `"call_id":"call_9"`)
}

// The gateway's other answers: failures on a 200, "text" content, and an
// answer with nothing in it (MDL-106).
func TestAPIProviderResponses(t *testing.T) {
	cases := map[string]struct {
		reply, want, wantErr string
	}{
		"failed on a 200":         {reply: gwFailed, wantErr: "quota exceeded"},
		"error under completed":   {reply: `{"status":"completed","error":{"message":"policy says no"},"output":[]}`, wantErr: "policy says no"},
		"text content":            {reply: `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"text","text":"plain text"}]}]}`, want: "plain text"},
		"several items, in order": {reply: `{"status":"completed","output":[{"type":"reasoning","summary":[]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"a"},{"type":"text","text":"b"}]}]}`, want: "ab"},
		"no output":               {reply: `{"status":"completed","output":[]}`, wantErr: "output"},
		"neither text nor call":   {reply: `{"status":"completed","output":[{"type":"reasoning","summary":[]}]}`, wantErr: "text"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			g := newFakeGateway(t, [2]any{http.StatusOK, tc.reply})
			script, _ := tokenScript(t)
			text, _, err := callAPI(context.Background(), apiTestModel(t, g, script, 0), helloRequest())
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, text)
		})
	}
}

// A rejected token is fetched again and the request sent once more; a
// second rejection says what to do, never as a model response (MDL-105).
func TestAPIProviderRejectedToken(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Run("then accepted", func(t *testing.T) {
				g := newFakeGateway(t, [2]any{status, `token expired`})
				script, runs := tokenScript(t)
				text, _, err := callAPI(context.Background(), apiTestModel(t, g, script, 0), helloRequest())
				require.NoError(t, err)
				assert.Equal(t, "One cell, c1, alive.", text)
				got := g.requests()
				require.Len(t, got, 2)
				assert.Equal(t, "gwtok-1", got[0].Header.Get("X-Api-Key"))
				assert.Equal(t, "gwtok-2", got[1].Header.Get("X-Api-Key"))
				assert.Equal(t, false, got[1].Body["store"], "the retry lost its body")
				assert.Equal(t, 2, runs())
			})
			t.Run("rejected again", func(t *testing.T) {
				g := newFakeGateway(t, [2]any{status, `nope`}, [2]any{status, `{"error":{"message":"nope"}}`})
				script, runs := tokenScript(t)
				_, _, err := callAPI(context.Background(), apiTestModel(t, g, script, 0), helloRequest())
				assert.EqualError(t, err, fmt.Sprintf("[llm.api] the API rejected the token (HTTP %d). Check the token script, sign in again if needed, then retry", status))
				assert.Len(t, g.requests(), 2, "retried more than once")
				assert.Equal(t, 2, runs())
			})
		})
	}
}

// A 5xx drops the token, so the SDK's retry carries a fresh one (MDL-105).
func TestAPIProviderServerError(t *testing.T) {
	g := newFakeGateway(t, [2]any{http.StatusBadGateway, `upstream down`})
	script, runs := tokenScript(t)
	text, _, err := callAPI(context.Background(), apiTestModel(t, g, script, 1), helloRequest())
	require.NoError(t, err)
	assert.Equal(t, "One cell, c1, alive.", text)
	got := g.requests()
	require.Len(t, got, 2)
	assert.Equal(t, "gwtok-2", got[1].Header.Get("X-Api-Key"))
	assert.Equal(t, 2, runs())
}

// A failing script ends the call before any request is sent (MDL-103).
func TestAPIProviderScriptFails(t *testing.T) {
	g := newFakeGateway(t)
	m := apiTestModel(t, g, "echo 'no sign-in' >&2; exit 1", 0)
	_, _, err := callAPI(context.Background(), m, helloRequest())
	assert.ErrorContains(t, err, "[llm.api] token script: shell failed: exit status 1: no sign-in")
	assert.Empty(t, g.requests())
}

// The placeholder in base_url's path and query is the token, escaped; the
// query reaches every request; errors never quote the token (MDL-101,
// MDL-108).
func TestAPIProviderTokenInURL(t *testing.T) {
	g := newFakeGateway(t, [2]any{http.StatusBadRequest, `{"error":{"message":"bad"}}`})
	script, _ := tokenScript(t)
	m := apiTestModel(t, g, script, 0, func(c *config.APIConfig) {
		c.BaseURL = g.URL + "/t/" + config.TokenPlaceholder + "?sig=" + config.TokenPlaceholder + "&v=1"
		c.Headers = map[string]string{"X-Tenant": "retail"}
	})
	_, _, err := callAPI(context.Background(), m, helloRequest())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "gwtok-1", "the token is in the error")
	_, _, err = callAPI(context.Background(), m, helloRequest())
	require.NoError(t, err)
	r := g.requests()[1]
	assert.Equal(t, "/t/gwtok-1/responses", r.Path)
	assert.Equal(t, []string{"gwtok-1"}, r.Query["sig"])
	assert.Equal(t, []string{"1"}, r.Query["v"])
}

// Another HTTP error says why the gateway refused, as it said it, with any
// token masked, even when the SDK can't read the reply (MDL-105, MDL-108).
func TestAPIProviderErrorReason(t *testing.T) {
	cases := map[string]struct {
		reply string
		want  string
	}{
		"error.message": {`{"error":{"message":"model claude-x is not available"}}`, "[llm.api] HTTP 400 Bad Request: model claude-x is not available"},
		"other JSON":    {`{"detail":"unknown field: store"}`, `[llm.api] HTTP 400 Bad Request: {"detail":"unknown field: store"}`},
		"not JSON":      {`<html>Bad Request</html>`, "[llm.api] HTTP 400 Bad Request: <html>Bad Request</html>"},
		// The redactor remembers tokens of 8 characters or more.
		"token echoed": {`{"error":{"message":"bad key gw-token-reason-1"}}`, "[llm.api] HTTP 400 Bad Request: bad key "},
		// The gateway's own 500, which the SDK can't read as an error.
		"unreadable": {`{"timestamp":1,"status":500,"error":"Internal Server Error","path":"/gw/v1beta/models/gemini-3.8-flash@001:responses"}`,
			`[llm.api] HTTP 500 Internal Server Error: {"timestamp":1,"status":500,"error":"Internal Server Error","path":"/gw/v1beta/models/gemini-3.8-flash@001:responses"}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			status := http.StatusBadRequest
			if name == "unreadable" {
				status = http.StatusInternalServerError
			}
			g := newFakeGateway(t, [2]any{status, tc.reply})
			_, _, err := callAPI(context.Background(), apiTestModel(t, g, "echo gw-token-reason-1", 0), helloRequest())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, err.Error(), "gw-token-reason-1", "the token is in the error")
			assert.NotContains(t, err.Error(), "openai", "the adapter's prefix")
		})
	}
}

// Responses requests leave out what the model refuses: sampling for models
// known to reject it (Claude's are the anthropic adapter's, below), and a field the gateway refused once, for that model from then
// on; a refusal of a field the request needs isn't retried (MDL-104b).
func TestAPIProviderRefusedFields(t *testing.T) {
	sampled := func() *model.LLMRequest {
		req := helloRequest()
		req.Config.Temperature = genai.Ptr[float32](0.2)
		req.Config.TopP = genai.Ptr[float32](0.9)
		return req
	}
	refusal := `{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model.","type":"invalid_request_error","param":"temperature","code":"unsupported_parameter"}}`

	t.Run("known", func(t *testing.T) {
		for name, drops := range map[string]bool{"gpt-5.6-sol": true, "o3-mini": true, "gpt-4.1": false, "gemini-3.8-flash": false} {
			g := newFakeGateway(t)
			_, _, err := callAPI(context.Background(), apiTestModelNamed(t, g, name, "echo tok-12345678", 0), sampled())
			require.NoError(t, err, name)
			body := g.requests()[0].Body
			assert.Equal(t, !drops, body["temperature"] != nil, "%s: temperature", name)
			assert.Equal(t, !drops, body["top_p"] != nil, "%s: top_p", name)
		}
	})

	t.Run("learned", func(t *testing.T) {
		g := newFakeGateway(t, [2]any{http.StatusBadRequest, refusal})
		m := apiTestModelNamed(t, g, "gateway-alias", "echo tok-12345678", 0)
		text, _, err := callAPI(context.Background(), m, sampled())
		require.NoError(t, err)
		assert.Equal(t, "One cell, c1, alive.", text)
		_, _, err = callAPI(context.Background(), m, sampled())
		require.NoError(t, err)
		got := g.requests()
		require.Len(t, got, 3)
		assert.NotNil(t, got[0].Body["temperature"])
		for _, r := range got[1:] {
			assert.Nil(t, r.Body["temperature"])
			assert.NotNil(t, r.Body["top_p"], "only the refused field")
		}
	})

	t.Run("by message", func(t *testing.T) {
		g := newFakeGateway(t, [2]any{http.StatusBadRequest, `{"error":{"message":"Unsupported value: 'top_p' does not support 0.9 with this model."}}`})
		_, _, err := callAPI(context.Background(), apiTestModelNamed(t, g, "gateway-alias", "echo tok-12345678", 0), sampled())
		require.NoError(t, err)
		got := g.requests()
		require.Len(t, got, 2)
		assert.Nil(t, got[1].Body["top_p"])
	})

	t.Run("needed", func(t *testing.T) {
		g := newFakeGateway(t, [2]any{http.StatusBadRequest, `{"error":{"message":"Unsupported parameter: 'input'","param":"input","code":"unsupported_parameter"}}`})
		_, _, err := callAPI(context.Background(), apiTestModelNamed(t, g, "gateway-alias", "echo tok-12345678", 0), sampled())
		assert.ErrorContains(t, err, "[llm.api] HTTP 400 Bad Request: Unsupported parameter: 'input'")
		assert.Len(t, g.requests(), 1)
	})
}

// Claude's answers through the gateway, as it gave them (Anthropic's
// Messages API, from Vertex AI).
const (
	gwClaudeText    = `{"content":[{"text":"One cell, c1, alive.","type":"text"}],"id":"msg_vrtx_1","model":"claude-sonnet-5-5","role":"assistant","stop_reason":"end_turn","type":"message","usage":{"input_tokens":11,"output_tokens":17}}`
	gwClaudeToolUse = `{"content":[{"id":"toolu_vrtx_1","input":{"path":"README.md"},"name":"read_file","type":"tool_use"}],"id":"msg_vrtx_2","model":"claude-sonnet-5-5","role":"assistant","stop_reason":"tool_use","type":"message","usage":{"input_tokens":378,"output_tokens":49}}`
	gwClaudeRefusal = `{"type":"error","error":{"type":"invalid_request_error","message":"messages: Field required"}}`
)

// A Claude model's requests are Anthropic's Messages API, sent where the
// gateway takes the others and signed the same way; its answers are read
// as Anthropic's; its settings are Anthropic's (MDL-100b, MDL-107).
func TestAPIProviderClaude(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-from-the-environment")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	toolRequest := func() *model.LLMRequest {
		req := helloRequest()
		req.Config.Temperature = genai.Ptr[float32](0.2)
		req.Config.Tools = []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name: "read_file", Description: "Reads a file.",
			ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
		}}}}
		return req
	}

	t.Run("text", func(t *testing.T) {
		g := newFakeGateway(t, [2]any{http.StatusOK, gwClaudeText})
		m := apiTestModelNamed(t, g, "claude-sonnet-5-5", "echo tok-claude-12345", 0)
		assert.Equal(t, "anthropic", providerOf(m))
		text, _, err := callAPI(context.Background(), m, toolRequest())
		require.NoError(t, err)
		assert.Equal(t, "One cell, c1, alive.", text)
		got := g.requests()
		require.Len(t, got, 1)
		r := got[0]
		assert.Equal(t, "/gw/responses", r.Path)
		assert.Empty(t, r.Query["beta"])
		assert.Equal(t, "tok-claude-12345", r.Header.Get("X-Api-Key"))
		assert.Equal(t, "retail", r.Header.Get("X-Tenant"))
		assert.Empty(t, r.Header.Get("Authorization"))
		assert.Equal(t, "claude-sonnet-5-5", r.Body["model"])
		assert.NotNil(t, r.Body["messages"], "a Messages body")
		assert.NotNil(t, r.Body["max_tokens"])
		for _, k := range []string{"input", "store", "temperature"} {
			assert.Nil(t, r.Body[k], k)
		}
		tools, _ := r.Body["tools"].([]any)
		require.Len(t, tools, 1)
		tool := tools[0].(map[string]any)
		assert.Equal(t, "read_file", tool["name"])
		assert.NotNil(t, tool["input_schema"], "Claude's tool shape")
		assert.NotEqual(t, "function", tool["type"])
	})

	t.Run("tool use", func(t *testing.T) {
		g := newFakeGateway(t, [2]any{http.StatusOK, gwClaudeToolUse})
		_, calls, err := callAPI(context.Background(), apiTestModelNamed(t, g, "claude-sonnet-5-5", "echo tok-claude-12345", 0), toolRequest())
		require.NoError(t, err)
		require.Len(t, calls, 1)
		assert.Equal(t, "read_file", calls[0].Name)
		assert.Equal(t, map[string]any{"path": "README.md"}, calls[0].Args)
	})

	t.Run("refused", func(t *testing.T) {
		g := newFakeGateway(t, [2]any{http.StatusBadRequest, gwClaudeRefusal})
		_, _, err := callAPI(context.Background(), apiTestModelNamed(t, g, "claude-sonnet-5-5", "echo tok-claude-12345", 0), toolRequest())
		require.Error(t, err)
		assert.Equal(t, "[llm.api] HTTP 400 Bad Request: messages: Field required", err.Error())
	})

	t.Run("rejected token", func(t *testing.T) {
		g := newFakeGateway(t, [2]any{http.StatusUnauthorized, `{"error":{"message":"expired"}}`}, [2]any{http.StatusOK, gwClaudeText})
		script, runs := tokenScript(t)
		text, _, err := callAPI(context.Background(), apiTestModelNamed(t, g, "claude-sonnet-5-5", script, 0), toolRequest())
		require.NoError(t, err)
		assert.Equal(t, "One cell, c1, alive.", text)
		assert.Equal(t, 2, runs())
		assert.Equal(t, "gwtok-2", g.requests()[1].Header.Get("X-Api-Key"))
	})
}

// Static headers need no script; settings the model can't use fail the
// build; the model is OpenAI's for settings (MDL-100, MDL-107).
func TestAPIProviderBuild(t *testing.T) {
	g := newFakeGateway(t)
	m := apiTestModel(t, g, "", 0, func(c *config.APIConfig) { c.Headers = map[string]string{"X-Api-Key": "static"} })
	_, _, err := callAPI(context.Background(), m, helloRequest())
	require.NoError(t, err)
	assert.Equal(t, "static", g.requests()[0].Header.Get("X-Api-Key"))
	assert.Equal(t, "openai", providerOf(m))

	_, err = newAPIModel(context.Background(), config.APIConfig{BaseURL: g.URL}, "gpt-5", policyFrom(config.LLMConfig{}))
	assert.ErrorContains(t, err, "at least one header")

	cfg := config.DefaultConfig()
	cfg.LLM.API = config.APIConfig{BaseURL: g.URL, Model: "gpt-5", Headers: map[string]string{"X-Api-Key": "k"}}
	built, err := buildProviderModel(context.Background(), cfg, "api", "gpt-5")
	require.NoError(t, err)
	assert.Equal(t, "gpt-5", built.Name())
	provider, name := ParseModelRef("api/gpt-5", "gemini")
	assert.Equal(t, "api", provider)
	assert.Equal(t, "gpt-5", name)
	assert.False(t, SettingSupported("api", "gpt-5", "seed"))

	listed := ListModels(context.Background(), cfg, "api")
	require.Len(t, listed, 1)
	require.NoError(t, listed[0].Err)
	assert.Equal(t, "gpt-5", listed[0].Models[0].ID)
	assert.Equal(t, "the configured model", listed[0].Note)
	assert.Contains(t, ConfiguredProviders(cfg), "api")
}
