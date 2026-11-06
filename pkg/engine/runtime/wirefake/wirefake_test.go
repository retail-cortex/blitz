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

// Each fake refuses a request too long in its API's words, ends a reply as
// scripted, and sends parts Blitz has no form for as its API sends them.
func TestRepliesAsEachAPIGivesThem(t *testing.T) {
	a, g, o := Anthropic(t), Gemini(t), OpenAI(t)
	msgs := map[string]any{"model": "m", "messages": []any{user("hi")}}
	gemini := map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}}}}
	responses := map[string]any{"model": "m", "input": "hi"}

	a.Script(Reply{TooLong: true}, Reply{Text: "no", Finish: "refusal", Extra: []map[string]any{{"type": "server_tool_use", "id": "s1"}}})
	code, body := post(t, a.URL()+"/v1/messages", msgs)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, str(obj(body["error"])["message"]), "prompt is too long")
	_, body = post(t, a.URL()+"/v1/messages", msgs)
	assert.Equal(t, "refusal", body["stop_reason"])
	assert.Equal(t, "server_tool_use", str(obj(list(body["content"])[1])["type"]))

	g.Script(Reply{TooLong: true}, Reply{Finish: "RECITATION", Text: "x"})
	code, body = post(t, g.URL()+"/v1beta/models/m:generateContent", gemini)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, str(obj(body["error"])["message"]), "exceeds the maximum number of tokens")
	_, body = post(t, g.URL()+"/v1beta/models/m:generateContent", gemini)
	assert.Equal(t, "RECITATION", obj(list(body["candidates"])[0])["finishReason"])

	o.Script(Reply{TooLong: true}, Reply{Text: "x", Finish: "content_filter", Extra: []map[string]any{{"type": "web_search_call"}}})
	code, body = post(t, o.URL()+"/v1/responses", responses)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "context_length_exceeded", obj(body["error"])["code"])
	_, body = post(t, o.URL()+"/v1/responses", responses)
	assert.Equal(t, "incomplete", body["status"])
	assert.Equal(t, "web_search_call", str(obj(list(body["output"])[1])["type"]))
	assert.Zero(t, a.Streamed()+g.Streamed()+o.Streamed())
}
