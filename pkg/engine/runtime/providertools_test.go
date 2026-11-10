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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestProviderToolsFor(t *testing.T) {
	for _, tc := range []struct {
		o    Origin
		want []string
	}{
		{Origin{"gemini", "gemini", "gemini-3-pro"}, []string{ptWebSearch, ptWebFetch, ptCodeExecution}},
		{Origin{"gemini", "vertex", "gemini-3-flash"}, []string{ptWebSearch, ptWebFetch, ptCodeExecution}},
		{Origin{"gemini", "gemini", "gemini-2.5-pro"}, nil},
		{Origin{"anthropic", "anthropic", "claude-opus-5-5"}, []string{ptWebSearch, ptWebFetch, ptCodeExecution}},
		{Origin{"anthropic", "bedrock", "claude-opus-5-5"}, nil},
		{Origin{"openai", "openai", "gpt-5"}, []string{ptWebSearch}},
		{Origin{"openai", "ollama", "gpt-5"}, nil},
		{Origin{"openai", "openai", "llama3"}, nil},
	} {
		assert.Equal(t, tc.want, providerToolsFor(tc.o), tc.o.key())
	}
}

func TestParseProviderToolsOff(t *testing.T) {
	all, off := ParseProviderToolsOff([]string{"read_file", "-provider:code_execution", " -provider:web_fetch "}, "-")
	assert.False(t, all)
	assert.Equal(t, map[string]bool{ptCodeExecution: true, ptWebFetch: true}, off)
	all, off = ParseProviderToolsOff([]string{"provider:*"}, "")
	assert.True(t, all)
	assert.Nil(t, off)
	all, off = ParseProviderToolsOff(nil, "")
	assert.False(t, all)
	assert.Nil(t, off)
}

func declaring(names ...string) *model.LLMRequest {
	t := &genai.Tool{}
	for _, n := range names {
		t.FunctionDeclarations = append(t.FunctionDeclarations, &genai.FunctionDeclaration{Name: n, Description: n})
	}
	return &model.LLMRequest{Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{t}}}
}

// Provider tools go only where Blitz's rules would let the client tool
// (CA-81).
func TestProviderToolsToUse(t *testing.T) {
	claudeO := Origin{"anthropic", "anthropic", "claude-opus-5-5"}
	planning := context.WithValue(context.Background(), runStateKey{}, &runState{planOnly: true})
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		req   *model.LLMRequest
		agent *ProviderToolPolicy
		want  []string
	}{
		{"every client tool offered", context.Background(), declaring("web_search", "web_fetch", "run_shell_command"), nil, []string{ptWebSearch, ptWebFetch, ptCodeExecution}},
		{"nothing offered", context.Background(), declaring("read_file"), nil, nil},
		{"search the agent lists, with no search provider", context.Background(), declaring("read_file"), &ProviderToolPolicy{Wants: map[string]bool{"web_search": true}}, []string{ptWebSearch}},
		{"fetch the agent lists, but off in the settings", context.Background(), declaring("read_file"), &ProviderToolPolicy{Wants: map[string]bool{"web_fetch": true}}, nil},
		{"plan mode", planning, declaring("web_search", "web_fetch", "run_shell_command"), nil, []string{ptWebSearch, ptWebFetch}},
		{"all off", WithProviderToolPolicy(context.Background(), ProviderToolPolicy{Off: true}), declaring("web_search"), nil, nil},
		{"one off by the settings", WithProviderToolPolicy(context.Background(), ProviderToolPolicy{Disabled: map[string]bool{ptWebFetch: true}}), declaring("web_search", "web_fetch"), nil, []string{ptWebSearch}},
		{"one off by the agent", context.Background(), declaring("web_search", "web_fetch"), &ProviderToolPolicy{Disabled: map[string]bool{ptWebSearch: true}}, []string{ptWebFetch}},
		{"all off by the agent", context.Background(), declaring("web_search"), &ProviderToolPolicy{Off: true}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.ctx
			if tc.agent != nil {
				ctx = context.WithValue(ctx, agentProviderToolsKey{}, *tc.agent)
			}
			assert.Equal(t, tc.want, providerToolsToUse(ctx, claudeO, tc.req))
		})
	}
	assert.Nil(t, providerToolsToUse(context.Background(), claudeO, nil))
}

// Gemini's code execution refuses a call with a file it can't open, so a
// call with one goes without it (and Claude's isn't touched).
func TestProviderToolsToUseGeminiFiles(t *testing.T) {
	geminiO := Origin{"gemini", "gemini", "gemini-3.8-flash"}
	with := func(parts ...*genai.Part) *model.LLMRequest {
		req := declaring("web_search", "run_shell_command")
		req.Contents = []*genai.Content{nil, {Role: genai.RoleUser, Parts: append([]*genai.Part{nil, {Text: "read this"}}, parts...)}}
		return req
	}
	all := []string{ptWebSearch, ptCodeExecution}
	for _, tc := range []struct {
		name string
		req  *model.LLMRequest
		want []string
	}{
		{"no files", with(), all},
		{"a PDF", with(&genai.Part{InlineData: &genai.Blob{MIMEType: "application/pdf"}}), []string{ptWebSearch}},
		{"an uploaded PDF", with(&genai.Part{FileData: &genai.FileData{MIMEType: "Application/PDF"}}), []string{ptWebSearch}},
		{"a WebP picture", with(&genai.Part{InlineData: &genai.Blob{MIMEType: "image/webp"}}), []string{ptWebSearch}},
		{"PNG, CSV and XML", with(
			&genai.Part{InlineData: &genai.Blob{MIMEType: "image/png"}},
			&genai.Part{InlineData: &genai.Blob{MIMEType: "text/csv; charset=utf-8"}},
			&genai.Part{FileData: &genai.FileData{MIMEType: "application/xml"}},
			&genai.Part{InlineData: &genai.Blob{}},
		), all},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, providerToolsToUse(context.Background(), geminiO, tc.req))
		})
	}
	claudeO := Origin{"anthropic", "anthropic", "claude-opus-5-5"}
	assert.Equal(t, all, providerToolsToUse(context.Background(), claudeO, with(&genai.Part{InlineData: &genai.Blob{MIMEType: "application/pdf"}})))
}

// The provider's tools take the client tools' places, never beside them;
// a client tool the history calls stays declared, saying not to call it
// (CA-80, CA-82).
func TestWithProviderTools(t *testing.T) {
	req := declaring("web_search", "web_fetch", "run_shell_command", "read_file")
	req.Contents = []*genai.Content{{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "web_fetch"}}}}}
	got := withProviderTools(req, []string{ptWebSearch, ptWebFetch, ptCodeExecution})
	var names []string
	for _, d := range got.Config.Tools[0].FunctionDeclarations {
		names = append(names, d.Name)
		if d.Name == "web_fetch" {
			assert.Contains(t, d.Description, "Don't call it")
		}
	}
	assert.Equal(t, []string{"web_fetch", "run_shell_command", "read_file"}, names, "search goes; fetch stays for its history; the shell isn't replaced")
	assert.Equal(t, []string{ptWebSearch, ptWebFetch, ptCodeExecution}, builtinKinds(got.Config))
	assert.Len(t, req.Config.Tools, 1, "the shared request was changed")
	assert.Same(t, req, withProviderTools(req, nil))

	only := withProviderTools(declaring("web_search"), []string{ptWebSearch})
	assert.Equal(t, []string{ptWebSearch}, builtinKinds(only.Config), "a tool left with no declarations goes")
	assert.Len(t, only.Config.Tools, 1)

	stripped := withoutBuiltins(got.Config)
	assert.Empty(t, builtinKinds(stripped))
	assert.Len(t, stripped.Tools, 1)
	assert.Same(t, req.Config, withoutBuiltins(req.Config))
	assert.Nil(t, builtinKinds(nil))
}

// Another model's provider tool calls become client calls paired with
// their results; the maker's own stay as they are (CA-82).
func TestAsClientCalls(t *testing.T) {
	claudeO := Origin{"anthropic", "anthropic", "claude-opus-5-5"}
	geminiO := Origin{"gemini", "gemini", "gemini-3-pro"}
	own := tagState(claudeO, []byte(`{"type":"server_tool_use"}`))
	call := providerCall(ptWebSearch, "srvtoolu_1", map[string]any{"query": "q"}, own)
	result := providerResult(ptWebSearch, "srvtoolu_1", "Blitz — https://b", tagState(claudeO, []byte(`{}`)))
	code := &genai.Part{ExecutableCode: &genai.ExecutableCode{Code: "print(1)", Language: genai.LanguagePython}, PartMetadata: map[string]any{providerToolKey: ptCodeExecution, providerOriginKey: geminiO.key(), callIDKey: "bz-c1"}}
	ran := &genai.Part{CodeExecutionResult: &genai.CodeExecutionResult{Outcome: genai.OutcomeOK, Output: "1"}, PartMetadata: map[string]any{providerToolKey: ptCodeExecution, providerOriginKey: geminiO.key(), callIDKey: "bz-c1"}}
	history := []*genai.Content{
		genai.NewContentFromText("hi", genai.RoleUser),
		{Role: genai.RoleModel, Parts: []*genai.Part{call, result, {Text: "Found it."}}},
		{Role: genai.RoleModel, Parts: []*genai.Part{code, ran}},
	}

	got, named := asClientCalls(claudeO, history)
	assert.Same(t, call, got[1].Parts[0], "Claude's own search stays")
	assert.Equal(t, map[string]bool{"provider_tool": true}, named)
	require.Len(t, got, 4)
	assert.Equal(t, "provider_tool", got[2].Parts[0].FunctionCall.Name)
	assert.Equal(t, "bz-c1", got[2].Parts[0].FunctionCall.ID)
	assert.Equal(t, map[string]any{"tool": ptCodeExecution, "language": "PYTHON", "code": "print(1)"}, got[2].Parts[0].FunctionCall.Args)
	assert.Equal(t, genai.RoleUser, got[3].Role)
	assert.Equal(t, map[string]any{"outcome": "OUTCOME_OK", "output": "1"}, got[3].Parts[0].FunctionResponse.Response)

	got, named = asClientCalls(geminiO, history)
	assert.Equal(t, map[string]bool{"web_search": true}, named)
	require.Len(t, got, 5)
	assert.Equal(t, &genai.FunctionCall{ID: "bz-p-srvtoolu_1", Name: "web_search", Args: map[string]any{"query": "q"}}, got[1].Parts[0].FunctionCall)
	assert.Equal(t, "bz-p-srvtoolu_1", got[2].Parts[0].FunctionResponse.ID)
	assert.Equal(t, "Found it.", got[3].Parts[0].Text)
	assert.Same(t, code, got[4].Parts[0], "Gemini's own code stays")

	plain := []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}
	got, named = asClientCalls(claudeO, plain)
	assert.Equal(t, plain, got)
	assert.Nil(t, named)
}

// Gemini's own search, kept from its grounding, isn't sent back to it.
func TestAsClientCallsDropsGeminisGrounding(t *testing.T) {
	geminiO := Origin{"gemini", "gemini", "gemini-3-pro"}
	resp := &model.LLMResponse{
		Content: genai.NewContentFromText("Blitz is an agent.", genai.RoleModel),
		GroundingMetadata: &genai.GroundingMetadata{
			WebSearchQueries:  []string{"blitz"},
			GroundingChunks:   []*genai.GroundingChunk{{Web: &genai.GroundingChunkWeb{URI: "https://b", Title: "Blitz"}}, nil},
			GroundingSupports: []*genai.GroundingSupport{{Segment: &genai.Segment{Text: "Blitz is"}, GroundingChunkIndices: []int32{0, 7}}},
		},
	}
	geminiProviderTools(geminiO, resp)
	require.Len(t, resp.Content.Parts, 3)
	assert.Equal(t, []Citation{{Title: "Blitz", URL: "https://b", Text: "Blitz is"}}, Citations(resp))
	got, _ := asClientCalls(geminiO, []*genai.Content{resp.Content})
	require.Len(t, got, 1)
	assert.Equal(t, "Blitz is an agent.", got[0].Parts[0].Text)
}

func TestGeminiProviderTools(t *testing.T) {
	geminiO := Origin{"gemini", "gemini", "gemini-3-pro"}
	resp := &model.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
		{ExecutableCode: &genai.ExecutableCode{Code: "1"}},
		{CodeExecutionResult: &genai.CodeExecutionResult{Output: "1"}},
	}}, GroundingMetadata: &genai.GroundingMetadata{GroundingChunks: []*genai.GroundingChunk{{Web: &genai.GroundingChunkWeb{URI: "https://b"}}}}}
	geminiProviderTools(geminiO, resp)
	assert.Equal(t, resp.Content.Parts[2].PartMetadata[callIDKey], resp.Content.Parts[3].PartMetadata[callIDKey], "the code and its result pair")
	assert.Equal(t, map[string]int{ptCodeExecution: 1, ptWebSearch: 1}, resp.CustomMetadata[ProviderToolRequestsKey])
	assert.Equal(t, []Citation{{URL: "https://b"}}, Citations(resp), "chunks without supports still cite")

	for _, r := range []*model.LLMResponse{nil, {Partial: true, Content: &genai.Content{}}, {}} {
		geminiProviderTools(geminiO, r)
	}
}

func TestCitationsAndRequests(t *testing.T) {
	resp := &model.LLMResponse{}
	addCitations(resp, nil)
	assert.Nil(t, resp.CustomMetadata)
	addCitations(resp, []Citation{{URL: "a"}, {URL: ""}, {URL: "a"}})
	assert.Equal(t, []Citation{{URL: "a"}}, Citations(resp))

	// A stored event's metadata comes back as JSON.
	b, err := json.Marshal(resp.CustomMetadata)
	require.NoError(t, err)
	var back map[string]any
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, []Citation{{URL: "a"}}, Citations(&model.LLMResponse{CustomMetadata: back}))
	assert.Nil(t, Citations(nil))

	countRequests(resp, ptWebSearch, 0)
	assert.Nil(t, resp.CustomMetadata[ProviderToolRequestsKey])
	countRequests(resp, ptWebSearch, 2)
	countRequests(resp, ptWebSearch, 1)
	assert.Equal(t, map[string]int{ptWebSearch: 3}, resp.CustomMetadata[ProviderToolRequestsKey])
	other := &model.LLMResponse{}
	countRequests(other, ptWebFetch, 1)
	assert.Equal(t, map[string]int{ptWebFetch: 1}, other.CustomMetadata[ProviderToolRequestsKey])
}

func TestDeclareCalled(t *testing.T) {
	cfg := declaring("read_file").Config
	assert.Same(t, cfg, declareCalled(cfg, map[string]bool{"read_file": true}))
	got := declareCalled(cfg, map[string]bool{"web_search": true, "provider_tool": true})
	require.Len(t, got.Tools, 2)
	assert.Equal(t, "provider_tool", got.Tools[1].FunctionDeclarations[0].Name)
	assert.Equal(t, "web_search", got.Tools[1].FunctionDeclarations[1].Name)
	assert.Len(t, cfg.Tools, 1)
	assert.Len(t, declareCalled(nil, map[string]bool{"web_search": true}).Tools, 1)
}

func TestProviderToolPart(t *testing.T) {
	name, id, args, call, ok := ProviderToolPart(providerCall(ptWebFetch, "srvtoolu_2", map[string]any{"url": "u"}, nil))
	assert.True(t, ok && call)
	assert.Equal(t, "web_fetch", name)
	assert.Equal(t, "bz-p-srvtoolu_2", id)
	assert.Equal(t, map[string]any{"url": "u"}, args)
	name, _, args, call, ok = ProviderToolPart(providerResult(ptCodeExecution, "bz-x", "out", nil))
	assert.True(t, ok)
	assert.False(t, call)
	assert.Equal(t, "provider_tool", name)
	assert.Equal(t, map[string]any{"result": "out"}, args)
	name, _, args, _, _ = ProviderToolPart(providerCall(ptCodeExecution, "x", map[string]any{"code": "1"}, nil))
	assert.Equal(t, "provider_tool", name)
	assert.Equal(t, map[string]any{"tool": ptCodeExecution, "input": map[string]any{"code": "1"}}, args)
	_, _, _, _, ok = ProviderToolPart(&genai.Part{Text: "x"})
	assert.False(t, ok)
}

// Claude's server tool blocks are stored as calls and results, their
// blocks kept for Claude, their results as text for any other model.
func TestAnthropicServerTools(t *testing.T) {
	var msg anthropic.BetaMessage
	require.NoError(t, json.Unmarshal([]byte(`{"id":"m","type":"message","role":"assistant","model":"claude-opus-5-5","stop_reason":"end_turn",
		"content":[
			{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"blitz"}},
			{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[{"type":"web_search_result","url":"https://b","title":"Blitz","encrypted_content":"e"}]},
			{"type":"server_tool_use","id":"srvtoolu_2","name":"code_execution","input":{"code":"1"}},
			{"type":"code_execution_tool_result","tool_use_id":"srvtoolu_2","content":{"type":"code_execution_result","stdout":"1","stderr":"","return_code":0}},
			{"type":"text","text":"Blitz is an agent.","citations":[{"type":"web_search_result_location","url":"https://b","title":"Blitz","cited_text":"an agent","encrypted_index":"i"}]}],
		"usage":{"input_tokens":10,"output_tokens":5,"server_tool_use":{"web_search_requests":1,"web_fetch_requests":0}}}`), &msg))
	resp := messageToResponse(&msg)
	parts := resp.Content.Parts
	require.Len(t, parts, 5)
	assert.Equal(t, "srvtoolu_1", parts[0].ToolCall.ID)
	assert.Equal(t, map[string]any{"query": "blitz"}, parts[0].ToolCall.Args)
	assert.Contains(t, string(parts[0].ThoughtSignature), `"server_tool_use"`)
	assert.Equal(t, "Blitz — https://b", parts[1].ToolResponse.Response["result"])
	assert.Equal(t, ptCodeExecution, parts[2].PartMetadata[providerToolKey])
	assert.Equal(t, "stdout:\n1", parts[3].ToolResponse.Response["result"])
	assert.Equal(t, []Citation{{Title: "Blitz", URL: "https://b", Text: "an agent"}}, Citations(resp))
	assert.Equal(t, map[string]int{ptWebSearch: 1, ptCodeExecution: 1}, resp.CustomMetadata[ProviderToolRequestsKey])

	// Sent back to Claude, the blocks go as they came.
	o := Origin{"anthropic", "anthropic", "claude-opus-5-5"}
	stampResponse(o, resp)
	adapted := adaptRequest(o, &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser), resp.Content}})
	msgs, err := convertContents(adapted.Contents)
	require.NoError(t, err)
	body, err := json.Marshal(msgs)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"web_search_tool_result"`)
	assert.Contains(t, string(body), `"encrypted_content":"e"`)
}

func TestServerToolText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`[{"title":"A","url":"https://a"},{"url":"https://b"}]`, "A — https://a\n— https://b"},
		{`{"type":"web_search_tool_result_error","error_code":"max_uses_exceeded"}`, "Error: max_uses_exceeded"},
		{`{"type":"web_fetch_result","url":"https://a","content":{"source":{"data":"page"}}}`, "https://a\npage"},
		{`{"stdout":"","stderr":"boom"}`, "stderr:\nboom"},
		{`{"other":1}`, `{"other":1}`},
		{`"text"`, `"text"`},
	} {
		assert.Equal(t, tc.want, serverToolText(json.RawMessage(tc.in)), tc.in)
	}
	assert.Nil(t, anthropicServerToolResult("text", `{}`))
	assert.Nil(t, anthropicServerToolResult("web_search_tool_result", ""))
	assert.Nil(t, anthropicServerToolResult("web_search_tool_result", "{"))
	assert.Equal(t, ptWebFetch, serverToolKind("web_fetch_tool_result"))
}

func TestConvertToolsServerTools(t *testing.T) {
	got, err := convertTools([]*genai.Tool{builtinTool(ptWebSearch), builtinTool(ptWebFetch), builtinTool(ptCodeExecution)})
	require.NoError(t, err)
	body, err := json.Marshal(got)
	require.NoError(t, err)
	for _, typ := range []string{"web_search_20250305", "web_fetch_20250910", "code_execution_20250825"} {
		assert.Contains(t, string(body), typ)
	}
}

// OpenAI's web search: added to the request by the middleware, its item
// and the answer's URL citations taken out of the reply for the sink.
func TestOpenAIProviderToolsMiddleware(t *testing.T) {
	reply := `{"id":"r","output":[
		{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"blitz","sources":[{"type":"url","url":"https://b"}]}},
		{"type":"message","content":[{"type":"output_text","text":"Blitz is an agent.","annotations":[{"type":"url_citation","url":"https://b","title":"Blitz","start_index":0,"end_index":5},{"type":"file_citation"}]}]}]}`
	var sent map[string]any
	next := func(req *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(b, &sent)
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		_, _ = rec.WriteString(reply)
		return rec.Result(), nil
	}
	sink := &openAISink{kinds: []string{ptWebSearch}}
	req := httptest.NewRequest(http.MethodPost, "https://api.openai.com/v1/responses", strings.NewReader(`{"model":"gpt-5","tools":[{"type":"function","name":"f"}]}`))
	req = req.WithContext(context.WithValue(req.Context(), openAISinkKey{}, sink))
	resp, err := openAIProviderToolsMiddleware(req, next)
	require.NoError(t, err)
	assert.Len(t, sent["tools"], 2)
	out, _ := io.ReadAll(resp.Body)
	assert.NotContains(t, string(out), "web_search_call", "ADK never sees the item")

	answer := &model.LLMResponse{Content: genai.NewContentFromText("Blitz is an agent.", genai.RoleModel)}
	sink.apply(answer)
	require.Len(t, answer.Content.Parts, 3)
	assert.Equal(t, map[string]any{"query": "blitz"}, answer.Content.Parts[0].ToolCall.Args)
	assert.Equal(t, "https://b", answer.Content.Parts[1].ToolResponse.Response["result"])
	assert.Equal(t, []Citation{{Title: "Blitz", URL: "https://b", Text: "Blitz"}}, Citations(answer))
	assert.Equal(t, map[string]int{ptWebSearch: 1}, answer.CustomMetadata[ProviderToolRequestsKey])

	// Without a sink, or for another endpoint, requests pass as they are.
	plain := httptest.NewRequest(http.MethodPost, "https://api.openai.com/v1/responses", strings.NewReader(`{}`))
	_, err = openAIProviderToolsMiddleware(plain, next)
	require.NoError(t, err)
	var nilSink *openAISink
	nilSink.apply(answer)
	assert.Equal(t, []byte("not json"), sink.take([]byte("not json")))
}

func TestProviderToolResult(t *testing.T) {
	parts, _, ok := providerToolResult(map[string]any{"type": "web_search_call", "status": "failed"})
	assert.True(t, ok)
	assert.Equal(t, "Search failed.", parts[1].ToolResponse.Response["result"])
	assert.NotEmpty(t, parts[0].ToolCall.ID)
	_, _, ok = providerToolResult(map[string]any{"type": "function_call"})
	assert.False(t, ok)
	assert.Equal(t, "", span("abc", json.Number("2"), json.Number("1")))
	assert.Equal(t, "b", span("abc", 1.0, 2.0))
	assert.Equal(t, -1, index("x"))
}

// The Gemini API refuses its built-in tools beside function calling unless
// asked for the server-side invocations; Vertex AI takes no such field.
func TestServerSideInvocations(t *testing.T) {
	t.Run("built-in tools beside functions", func(t *testing.T) {
		req := withProviderTools(declaring("web_search", "read_file"), []string{ptWebSearch})
		req.Config.ToolConfig = &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAuto}}
		got := withServerSideInvocations(req)
		require.NotNil(t, got.Config.ToolConfig.IncludeServerSideToolInvocations)
		assert.True(t, *got.Config.ToolConfig.IncludeServerSideToolInvocations)
		assert.Equal(t, genai.FunctionCallingConfigModeAuto, got.Config.ToolConfig.FunctionCallingConfig.Mode, "the rest of the tool config kept")
		assert.Nil(t, req.Config.ToolConfig.IncludeServerSideToolInvocations, "the shared request is unchanged")
	})
	for name, req := range map[string]*model.LLMRequest{
		"functions only":      declaring("read_file"),
		"built-in tools only": withProviderTools(declaring("web_search"), []string{ptWebSearch}),
		"no config":           {},
		"none":                nil,
	} {
		t.Run(name, func(t *testing.T) {
			assert.Same(t, req, withServerSideInvocations(req))
		})
	}
	t.Run("only on the Gemini API", func(t *testing.T) {
		for _, tc := range []struct {
			configured string
			vertexAI   bool
			want       bool
		}{{"", false, true}, {"vertex", false, false}, {"gemini", true, false}} {
			inner := &refusing{name: "gemini-3.8-flash"}
			m := &settingsModel{inner: inner, provider: "gemini", configured: tc.configured, vertexAI: tc.vertexAI}
			for range m.GenerateContent(t.Context(), declaring("web_search", "read_file"), false) {
			}
			require.Len(t, inner.requests, 1)
			cfg := inner.requests[0].Config
			require.NotEmpty(t, builtinKinds(cfg), tc.configured)
			on := cfg.ToolConfig != nil && cfg.ToolConfig.IncludeServerSideToolInvocations != nil && *cfg.ToolConfig.IncludeServerSideToolInvocations
			assert.Equal(t, tc.want, on, tc.configured)
		}
	})
}

// With server-side invocations, Gemini's answer carries its own tool calls
// and results: they're tagged and kept for Gemini, read as client calls by
// any other model, and no search parts are made from the grounding.
func TestGeminiOwnToolParts(t *testing.T) {
	geminiO := Origin{"gemini", "gemini", "gemini-3.8-flash"}
	claudeO := Origin{"anthropic", "anthropic", "claude-opus-5-5"}
	call := &genai.Part{ToolCall: &genai.ToolCall{ID: "s1", ToolType: genai.ToolTypeGoogleSearchWeb, Args: map[string]any{"queries": []any{"recipes"}}}, ThoughtSignature: []byte("sig")}
	result := &genai.Part{ToolResponse: &genai.ToolResponse{ID: "s1", ToolType: genai.ToolTypeGoogleSearchWeb, Response: map[string]any{"result": "found"}}}
	fetch := &genai.Part{ToolCall: &genai.ToolCall{ID: "u1", ToolType: genai.ToolTypeURLContext, Args: map[string]any{"url": "https://a"}}}
	maps := &genai.Part{ToolCall: &genai.ToolCall{ID: "m1", ToolType: genai.ToolTypeGoogleMaps}}
	resp := &model.LLMResponse{
		Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{call, result, fetch, maps, genai.NewPartFromText("Here.")}},
		GroundingMetadata: &genai.GroundingMetadata{WebSearchQueries: []string{"recipes"},
			GroundingChunks: []*genai.GroundingChunk{{Web: &genai.GroundingChunkWeb{URI: "https://b", Title: "B"}}}},
	}
	geminiProviderTools(geminiO, resp)
	require.Len(t, resp.Content.Parts, 5, "no search parts made from the grounding")
	assert.Equal(t, ptWebSearch, call.PartMetadata[providerToolKey])
	assert.Equal(t, ptWebSearch, result.PartMetadata[providerToolKey])
	assert.Equal(t, ptWebFetch, fetch.PartMetadata[providerToolKey])
	assert.Nil(t, maps.PartMetadata[providerToolKey], "a tool Blitz doesn't offer has no kind")
	assert.Equal(t, geminiO.key(), maps.PartMetadata[providerOriginKey], "but is Gemini's own")
	assert.Equal(t, map[string]int{ptWebSearch: 1, ptWebFetch: 1}, resp.CustomMetadata[ProviderToolRequestsKey])
	assert.Equal(t, []Citation{{Title: "B", URL: "https://b"}}, Citations(resp), "the grounding still cites")

	history := []*genai.Content{genai.NewContentFromText("find recipes", genai.RoleUser), resp.Content}
	got, named := asClientCalls(geminiO, history)
	assert.Empty(t, named)
	assert.Equal(t, []*genai.Part{call, result, fetch, maps}, got[1].Parts[:4], "Gemini gets its own back, signature and all")

	got, named = asClientCalls(claudeO, history)
	assert.True(t, named[ptWebSearch])
	assert.True(t, named[ptWebFetch])
	require.NotNil(t, got[1].Parts[0].FunctionCall)
	assert.Equal(t, ptWebSearch, got[1].Parts[0].FunctionCall.Name, "another model reads a call of the client tool")
	require.NotNil(t, got[2].Parts[0].FunctionResponse)
	assert.Equal(t, map[string]any{"result": "found"}, got[2].Parts[0].FunctionResponse.Response)
}
