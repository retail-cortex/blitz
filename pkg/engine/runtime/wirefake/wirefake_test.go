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

package wirefake

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func post(t *testing.T, url string, body any) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	defer resp.Body.Close()
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return resp.StatusCode, out
}

func user(text string) map[string]any { return map[string]any{"role": "user", "content": text} }

// The Messages API fake refuses what Anthropic documents it refuses.
func TestAnthropicStrict(t *testing.T) {
	use := func(id string) map[string]any {
		return map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": id, "name": "read_file", "input": map[string]any{}}}}
	}
	result := func(id string) map[string]any {
		return map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": "ok"}}}
	}
	tests := []struct {
		name     string
		req      map[string]any
		refusal  string
		thinking bool
	}{
		{name: "a plain question", req: map[string]any{"messages": []any{user("hi")}}},
		{name: "a tool loop", req: map[string]any{"messages": []any{user("hi"), use("toolu_1"), result("toolu_1")}}},
		{name: "the model first", req: map[string]any{"messages": []any{map[string]any{"role": "assistant", "content": "x"}}}, refusal: "first message"},
		{name: "an empty tool_use ID", req: map[string]any{"messages": []any{user("hi"), use(""), result("")}}, refusal: "should match pattern"},
		{name: "a tool_use with no result", req: map[string]any{"messages": []any{user("hi"), use("toolu_1"), user("next")}}, refusal: "without `tool_result`"},
		{name: "a result for no tool_use", req: map[string]any{"messages": []any{user("hi"), result("toolu_9")}}, refusal: "unexpected `tool_use_id`"},
		{name: "a foreign signature", req: map[string]any{"messages": []any{user("hi"), map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "thinking", "thinking": "x", "signature": "gemini-signature-1"}}}}}, refusal: "Invalid `signature`"},
		{name: "a tool loop with thinking on and no thinking block", req: map[string]any{"messages": []any{user("hi"), use("toolu_1"), result("toolu_1")}}, thinking: true, refusal: "Expected `thinking`"},
		{name: "an image from the model", req: map[string]any{"messages": []any{user("hi"), map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "image"}}}}}, refusal: "only be in user messages"},
		{name: "a bad tool name", req: map[string]any{"messages": []any{user("hi")}, "tools": []any{map[string]any{"name": "mcp.github/search"}}}, refusal: "tools.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Anthropic(t)
			f.Script(Reply{Text: "ok"})
			tt.req["model"] = "claude-test"
			if tt.thinking {
				tt.req["thinking"] = map[string]any{"type": "adaptive"}
			}
			code, body := post(t, f.URL()+"/v1/messages", tt.req)
			if tt.refusal == "" {
				assert.Equal(t, http.StatusOK, code, "%v", body)
				assert.Empty(t, f.Refusals())
				return
			}
			assert.Equal(t, http.StatusBadRequest, code)
			require.Len(t, f.Refusals(), 1)
			assert.Contains(t, f.Refusals()[0], tt.refusal)
		})
	}
}

// Its own signed thinking is accepted back, and a tool loop with thinking
// on goes on from it.
func TestAnthropicSignsItsThinking(t *testing.T) {
	f := Anthropic(t)
	f.Script(Reply{Thought: "plan", Calls: []Call{{Name: "read_file"}}}, Reply{Text: "done"})
	req := map[string]any{"model": "claude-test", "thinking": map[string]any{"type": "adaptive"}, "messages": []any{user("hi")}}
	code, body := post(t, f.URL()+"/v1/messages", req)
	require.Equal(t, http.StatusOK, code)
	content := list(body["content"])
	require.Len(t, content, 2)
	id := str(obj(content[1])["id"])
	req["messages"] = []any{user("hi"), map[string]any{"role": "assistant", "content": content},
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": "ok"}}}}
	code, body = post(t, f.URL()+"/v1/messages", req)
	assert.Equal(t, http.StatusOK, code, "%v", body)
	assert.Empty(t, f.Refusals())
}

// The Gemini fake refuses what Gemini documents it refuses.
func TestGeminiStrict(t *testing.T) {
	f := Gemini(t)
	sig := base64.StdEncoding.EncodeToString([]byte(f.sign("gemini")))
	text := func(role, s string) map[string]any {
		return map[string]any{"role": role, "parts": []any{map[string]any{"text": s}}}
	}
	call := func(sig string) map[string]any {
		p := map[string]any{"functionCall": map[string]any{"name": "read_file", "args": map[string]any{}}}
		if sig != "" {
			p["thoughtSignature"] = sig
		}
		return map[string]any{"role": "model", "parts": []any{p}}
	}
	response := map[string]any{"role": "user", "parts": []any{map[string]any{"functionResponse": map[string]any{"name": "read_file", "response": map[string]any{}}}}}
	tests := []struct {
		name     string
		contents []any
		refusal  string
	}{
		{name: "a plain question", contents: []any{text("user", "hi")}},
		{name: "a signed tool loop", contents: []any{text("user", "hi"), call(sig), response}},
		{name: "an unsigned call in an earlier turn", contents: []any{text("user", "hi"), call(""), response, text("model", "ok"), text("user", "next")}},
		{name: "an unsigned call in this turn", contents: []any{text("user", "hi"), call(""), response}, refusal: "missing a thought_signature"},
		{name: "a foreign signature", contents: []any{text("user", "hi"), call(base64.StdEncoding.EncodeToString([]byte("anthropic-signature-1"))), response}, refusal: "not valid"},
		{name: "a response with no call", contents: []any{text("user", "hi"), response}, refusal: "immediately after a function call"},
		{name: "a call with no response", contents: []any{text("user", "hi"), call(sig), text("user", "next")}, refusal: "immediately after a function call"},
		{name: "a bad role", contents: []any{text("assistant", "hi")}, refusal: "valid role"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(f.Refusals())
			f.Script(Reply{Text: "ok"})
			code, body := post(t, f.URL()+"/v1beta/models/gemini-test:generateContent", map[string]any{"contents": tt.contents})
			if tt.refusal == "" {
				assert.Equal(t, http.StatusOK, code, "%v", body)
				assert.Len(t, f.Refusals(), before)
				return
			}
			assert.Equal(t, http.StatusBadRequest, code)
			require.Len(t, f.Refusals(), before+1)
			assert.Contains(t, f.Refusals()[before], tt.refusal)
		})
	}
}

// The Responses API fake refuses what OpenAI documents it refuses.
func TestOpenAIStrict(t *testing.T) {
	msg := func(role, typ string) map[string]any {
		return map[string]any{"type": "message", "role": role, "content": []any{map[string]any{"type": typ, "text": "x"}}}
	}
	call := func(id string) map[string]any {
		return map[string]any{"type": "function_call", "call_id": id, "name": "read_file", "arguments": "{}"}
	}
	output := func(id string) map[string]any {
		return map[string]any{"type": "function_call_output", "call_id": id, "output": "ok"}
	}
	tests := []struct {
		name    string
		input   []any
		refusal string
	}{
		{name: "a plain question", input: []any{msg("user", "input_text")}},
		{name: "a tool loop", input: []any{msg("user", "input_text"), call("call_1"), output("call_1")}},
		{name: "an empty call_id", input: []any{msg("user", "input_text"), call(""), output("")}, refusal: "Missing required parameter"},
		{name: "an output for no call", input: []any{msg("user", "input_text"), output("call_9")}, refusal: "No tool call found"},
		{name: "a call with no output", input: []any{msg("user", "input_text"), call("call_1")}, refusal: "No tool output found"},
		{name: "an image from the assistant", input: []any{msg("user", "input_text"), msg("assistant", "input_image")}, refusal: "Supported values are: 'output_text'"},
		{name: "a foreign reasoning item", input: []any{msg("user", "input_text"), map[string]any{"type": "reasoning", "encrypted_content": "gemini-signature-1"}}, refusal: "could not be verified"},
		{name: "a part it doesn't know", input: []any{map[string]any{"type": "file_data"}}, refusal: "Invalid value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := OpenAI(t)
			f.Script(Reply{Text: "ok"})
			code, body := post(t, f.URL()+"/v1/responses", map[string]any{"model": "gpt-test", "input": tt.input})
			if tt.refusal == "" {
				assert.Equal(t, http.StatusOK, code, "%v", body)
				assert.Empty(t, f.Refusals())
				return
			}
			assert.Equal(t, http.StatusBadRequest, code)
			require.Len(t, f.Refusals(), 1)
			assert.Contains(t, f.Refusals()[0], tt.refusal)
		})
	}
}

// Each fake reports usage as its API does: Gemini's candidates leave out
// its thoughts, OpenAI's output tokens include its reasoning.
func TestUsageAsEachAPIReportsIt(t *testing.T) {
	u := Usage{Input: 1000, Cached: 400, Output: 300, Reasoning: 100}
	g := Gemini(t)
	g.Script(Reply{Text: "ok", Usage: u})
	_, body := post(t, g.URL()+"/v1beta/models/m:generateContent", map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}}}})
	meta := obj(body["usageMetadata"])
	assert.EqualValues(t, 200, meta["candidatesTokenCount"])
	assert.EqualValues(t, 100, meta["thoughtsTokenCount"])

	o := OpenAI(t)
	o.Script(Reply{Text: "ok", Usage: u})
	_, body = post(t, o.URL()+"/v1/responses", map[string]any{"model": "m", "input": "hi"})
	usage := obj(body["usage"])
	assert.EqualValues(t, 300, usage["output_tokens"])
	assert.EqualValues(t, 100, obj(usage["output_tokens_details"])["reasoning_tokens"])
}

var search = &Search{Query: "blitz agent", Results: []Source{{Title: "Blitz", URL: "https://example.com/blitz"}}}

// Each fake searches with its provider's own tool only when it's offered,
// and answers in its API's form: Claude's server tool blocks and
// citations, Gemini's grounding, OpenAI's web_search_call and URL
// citations.
func TestProviderSearch(t *testing.T) {
	a := Anthropic(t)
	a.Script(Reply{Search: search, Text: "Blitz is an agent."}, Reply{Search: search, Text: "No tool offered."})
	_, body := post(t, a.URL()+"/v1/messages", map[string]any{"model": "claude-test", "messages": []any{user("hi")}, "tools": []any{map[string]any{"type": "web_search_20250305", "name": "web_search"}}})
	content := list(body["content"])
	require.Len(t, content, 3)
	assert.Equal(t, "server_tool_use", str(obj(content[0])["type"]))
	assert.Equal(t, "web_search_tool_result", str(obj(content[1])["type"]))
	assert.Len(t, list(obj(content[2])["citations"]), 1)
	assert.EqualValues(t, 1, obj(obj(body["usage"])["server_tool_use"])["web_search_requests"])
	_, body = post(t, a.URL()+"/v1/messages", map[string]any{"model": "claude-test", "messages": []any{user("hi")}})
	assert.Len(t, list(body["content"]), 1, "no search without the tool")

	g := Gemini(t)
	g.Script(Reply{Search: search, Text: "Blitz is an agent."})
	_, body = post(t, g.URL()+"/v1beta/models/gemini-3-pro:generateContent", map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}}}, "tools": []any{map[string]any{"googleSearch": map[string]any{}}}})
	grounding := obj(obj(list(body["candidates"])[0])["groundingMetadata"])
	assert.Equal(t, []any{"blitz agent"}, grounding["webSearchQueries"])
	assert.Len(t, list(grounding["groundingChunks"]), 1)

	o := OpenAI(t)
	o.Script(Reply{Search: search, Text: "Blitz is an agent."})
	_, body = post(t, o.URL()+"/v1/responses", map[string]any{"model": "gpt-test", "input": "hi", "tools": []any{map[string]any{"type": "web_search"}}})
	out := list(body["output"])
	require.Len(t, out, 2)
	assert.Equal(t, "web_search_call", str(obj(out[0])["type"]))
	ann := list(obj(list(obj(out[1])["content"])[0])["annotations"])
	assert.Equal(t, "url_citation", str(obj(ann[0])["type"]))
}

// The fakes refuse provider tools used as their APIs refuse them.
func TestProviderToolsStrict(t *testing.T) {
	assistant := func(blocks ...any) map[string]any { return map[string]any{"role": "assistant", "content": blocks} }
	use := map[string]any{"type": "server_tool_use", "id": "srvtoolu_1", "name": "web_search", "input": map[string]any{"query": "q"}}
	result := func(enc string) map[string]any {
		return map[string]any{"type": "web_search_tool_result", "tool_use_id": "srvtoolu_1", "content": []any{map[string]any{"type": "web_search_result", "url": "u", "title": "t", "encrypted_content": enc}}}
	}
	for _, tc := range []struct {
		name, refusal string
		req           map[string]any
	}{
		{"an unknown server tool", "does not match any of the expected tags", map[string]any{"messages": []any{user("hi")}, "tools": []any{map[string]any{"type": "web_search_99", "name": "web_search"}}}},
		{"a result with no server_tool_use", "must match a `server_tool_use`", map[string]any{"messages": []any{user("hi"), assistant(result("x"))}}},
		{"a result it didn't give", "Invalid `encrypted_content`", map[string]any{"messages": []any{user("hi"), assistant(use, result("forged"))}}},
		{"a bad server_tool_use ID", "srvtoolu_", map[string]any{"messages": []any{user("hi"), assistant(map[string]any{"type": "server_tool_use", "id": "bz-1", "name": "web_search", "input": map[string]any{}})}}},
	} {
		t.Run("anthropic "+tc.name, func(t *testing.T) {
			f := Anthropic(t)
			tc.req["model"] = "claude-test"
			code, _ := post(t, f.URL()+"/v1/messages", tc.req)
			assert.Equal(t, http.StatusBadRequest, code)
			require.Len(t, f.Refusals(), 1)
			assert.Contains(t, f.Refusals()[0], tc.refusal)
		})
	}
	text := map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}}
	for _, tc := range []struct {
		name, model, refusal string
		body                 map[string]any
	}{
		{"search with functions before Gemini 3", "gemini-2.5-pro", "function calling is unsupported", map[string]any{"contents": []any{text}, "tools": []any{map[string]any{"googleSearch": map[string]any{}}, map[string]any{"functionDeclarations": []any{map[string]any{"name": "f"}}}}}},
		{"a tool it doesn't know", "gemini-3-pro", "Unknown name", map[string]any{"contents": []any{text}, "tools": []any{map[string]any{"fileSearch": map[string]any{}}}}},
		{"a server tool call sent back", "gemini-3-pro", "Unknown name", map[string]any{"contents": []any{text, map[string]any{"role": "model", "parts": []any{map[string]any{"toolCall": map[string]any{}}}}}}},
	} {
		t.Run("gemini "+tc.name, func(t *testing.T) {
			f := Gemini(t)
			code, _ := post(t, f.URL()+"/v1beta/models/"+tc.model+":generateContent", tc.body)
			assert.Equal(t, http.StatusBadRequest, code)
			require.Len(t, f.Refusals(), 1)
			assert.Contains(t, f.Refusals()[0], tc.refusal)
		})
	}
	o := OpenAI(t)
	code, _ := post(t, o.URL()+"/v1/responses", map[string]any{"model": "m", "input": "hi", "tools": []any{map[string]any{"type": "code_interpreter"}}})
	assert.Equal(t, http.StatusBadRequest, code)
	require.Len(t, o.Refusals(), 1)
}
