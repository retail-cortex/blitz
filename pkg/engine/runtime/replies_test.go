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
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Each API's words for a request too long for its model are
// api.ErrContextLength, keeping the provider's words (CA-32, CA-111).
func TestAsContextLength(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		long bool
	}{
		{"anthropic: API error (400): prompt is too long: 212000 tokens > 200000 maximum", true},
		{"input length and `max_tokens` exceed context limit: 190000 + 64000 > 200000", true},
		{`{"code":"context_length_exceeded","message":"Your input exceeds the context window of this model."}`, true},
		{"This model's maximum context length is 128000 tokens.", true},
		{"The input token count (1100000) exceeds the maximum number of tokens allowed (1048576).", true},
		{"rate limit reached", false},
		{"overloaded", false},
	} {
		t.Run(tc.msg, func(t *testing.T) {
			err := asContextLength(errors.New(tc.msg))
			assert.Equal(t, tc.long, errors.Is(err, api.ErrContextLength))
			assert.Equal(t, tc.msg, err.Error(), "the provider's words are kept")
		})
	}
	assert.NoError(t, asContextLength(nil))
	wrapped := asContextLength(errors.New("prompt is too long"))
	assert.Same(t, wrapped, asContextLength(wrapped))
	assert.Error(t, errors.Unwrap(wrapped))
}

// Why a reply ended decides how the turn ends (CA-31).
func TestFinishError(t *testing.T) {
	for _, tc := range []struct {
		reason genai.FinishReason
		code   string
		want   error
	}{
		{genai.FinishReasonStop, "", nil},
		{genai.FinishReasonUnspecified, "", nil},
		{genai.FinishReasonMaxTokens, "", api.ErrOutputLimit},
		{genai.FinishReasonSafety, "refusal", api.ErrRefused},
		{genai.FinishReasonSafety, "", api.ErrBlocked},
		{genai.FinishReasonProhibitedContent, "", api.ErrBlocked},
		{genai.FinishReasonSPII, "", api.ErrBlocked},
		{genai.FinishReasonRecitation, "", api.ErrRecitation},
		{genai.FinishReasonMalformedFunctionCall, "", api.ErrMalformedToolCall},
		{genai.FinishReasonUnexpectedToolCall, "", api.ErrMalformedToolCall},
	} {
		t.Run(string(tc.reason)+tc.code, func(t *testing.T) {
			assert.Equal(t, tc.want, finishError(&model.LLMResponse{FinishReason: tc.reason, ErrorCode: tc.code}))
		})
	}
}

// Parts the OpenAI adapter can't read are taken out of a reply, whole or
// streamed, and kept (CA-30).
func TestSanitizeResponses(t *testing.T) {
	doc := map[string]any{"output": []any{
		map[string]any{"type": "web_search_call", "id": "ws_1"},
		map[string]any{"type": "message", "content": []any{
			map[string]any{"type": "output_text", "text": "hi"},
			map[string]any{"type": "output_audio", "data": "…"},
		}},
		map[string]any{"type": "function_call", "name": "read_file"},
	}}
	var got []string
	drop := sanitizeResponses(doc, func(m map[string]any) { got = append(got, m["type"].(string)) })
	assert.False(t, drop)
	assert.Equal(t, []string{"web_search_call", "output_audio"}, got)
	out := doc["output"].([]any)
	require.Len(t, out, 2)
	assert.Len(t, out[0].(map[string]any)["content"], 1)

	for _, tc := range []struct {
		name  string
		event map[string]any
		drop  bool
		added int
	}{
		{"an unknown item added", map[string]any{"type": "response.output_item.added", "item": map[string]any{"type": "web_search_call"}}, true, 0},
		{"an unknown item done", map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "web_search_call"}}, true, 1},
		{"a known item", map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "message", "content": []any{}}}, false, 0},
		{"an unknown content part", map[string]any{"type": "response.content_part.added", "part": map[string]any{"type": "output_audio"}}, true, 0},
		{"the completed response", map[string]any{"type": "response.completed", "response": map[string]any{"output": []any{map[string]any{"type": "web_search_call"}}}}, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			added := 0
			assert.Equal(t, tc.drop, sanitizeResponses(tc.event, func(map[string]any) { added++ }))
			assert.Equal(t, tc.added, added)
		})
	}
}

func TestSanitizeStream(t *testing.T) {
	in := "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"web_search_call\"}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"web_search_call\",\"id\":\"ws_1\"}}\n\n" +
		": a comment\n\n"
	var kept []map[string]any
	out, err := io.ReadAll(sanitizeStream(io.NopCloser(strings.NewReader(in)), func(m map[string]any) { kept = append(kept, m) }))
	require.NoError(t, err)
	assert.NotContains(t, string(out), "web_search_call")
	assert.Contains(t, string(out), `"delta":"hi"`)
	assert.Contains(t, string(out), ": a comment")
	require.Len(t, kept, 1)
	assert.Equal(t, "ws_1", kept[0]["id"])
}

// The middleware works only on a call that asked for it, on a 200 from
// the Responses API.
func TestOpenAIOutputMiddleware(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"output":[{"type":"web_search_call"},{"type":"message","content":[]}]}`)
	}))
	defer srv.Close()
	call := func(ctx context.Context, path, body string) (string, string) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+path, strings.NewReader(body))
		resp, err := openAIOutputMiddleware(req, http.DefaultClient.Do)
		require.NoError(t, err)
		b, _ := io.ReadAll(resp.Body)
		return string(b), resp.Header.Get("Content-Type")
	}
	ctx, u := withUnshown(context.Background())
	got, typ := call(ctx, "/v1/responses", `{"model":"m"}`)
	assert.NotContains(t, got, "web_search_call")
	assert.Equal(t, "application/json", typ)
	assert.Len(t, u.take(), 1)
	got, _ = call(context.Background(), "/v1/responses", `{}`)
	assert.NotContains(t, got, "web_search_call", "taken out with nothing to keep it")
	got, _ = call(ctx, "/v1/models", ``)
	assert.Contains(t, got, "web_search_call", "another endpoint")

	// A streamed request answered whole goes on as the stream's last event.
	got, typ = call(ctx, "/v1/responses", `{"model":"m","stream":true}`)
	assert.Equal(t, "text/event-stream", typ)
	assert.True(t, strings.HasPrefix(got, "event: response.completed\ndata: {"), got)
	assert.Contains(t, got, `"type":"message"`)
}

func TestUnshownTypes(t *testing.T) {
	resp := &model.LLMResponse{}
	attachUnshown(resp, nil)
	assert.Nil(t, resp.CustomMetadata)
	attachUnshown(resp, []map[string]any{{"type": "a"}, {"type": "b"}})
	attachUnshown(resp, []map[string]any{{"type": "a"}})
	assert.Equal(t, []string{"a", "b"}, unshownTypes(resp))
	read := &model.LLMResponse{CustomMetadata: map[string]any{UnshownKey: []any{map[string]any{"type": "c"}}}}
	assert.Equal(t, []string{"c"}, unshownTypes(read), "as read back from a session")
	assert.Nil(t, unshownTypes(nil))
	var u *unshownParts
	u.add(map[string]any{})
	assert.Nil(t, u.take())
}

// callScript answers each call from its own list: an error, or responses.
type callScript struct {
	name  string
	calls []func(stream bool) ([]*model.LLMResponse, error)
	seen  []bool // stream, per call
	reqs  []*model.LLMRequest
}

func (m *callScript) Name() string { return m.name }

func (m *callScript) GenerateContent(_ context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		i := len(m.seen)
		m.seen, m.reqs = append(m.seen, stream), append(m.reqs, req)
		resps, err := m.calls[i](stream)
		for _, r := range resps {
			if !yield(r, nil) {
				return
			}
		}
		if err != nil {
			yield(nil, err)
		}
	}
}

func whole(text string) *model.LLMResponse {
	return &model.LLMResponse{Content: genai.NewContentFromText(text, genai.RoleModel), TurnComplete: true}
}

// A model that refuses to stream is called whole, at once and from then on
// (CA-53).
func TestStreamingModelLearnsRefusals(t *testing.T) {
	name := "refuses-streams-" + t.Name()
	inner := &callScript{name: name, calls: []func(bool) ([]*model.LLMResponse, error){
		func(bool) ([]*model.LLMResponse, error) {
			return nil, errors.New(`400 Bad Request: {"error":{"message":"Streaming is not supported for this model"}}`)
		},
		func(bool) ([]*model.LLMResponse, error) { return []*model.LLMResponse{whole("a")}, nil },
		func(bool) ([]*model.LLMResponse, error) { return []*model.LLMResponse{whole("b")}, nil },
	}}
	m := &streamingModel{inner: inner}
	assert.True(t, streamsFor(name))
	for range m.GenerateContent(context.Background(), &model.LLMRequest{}, true) {
	}
	assert.False(t, streamsFor(name))
	for range m.GenerateContent(context.Background(), &model.LLMRequest{}, true) {
	}
	assert.Equal(t, []bool{true, false, false}, inner.seen)
	assert.Equal(t, name, m.Name())

	// Another error, or one after output, isn't a refusal to stream.
	other := &callScript{name: "streams-" + t.Name(), calls: []func(bool) ([]*model.LLMResponse, error){
		func(bool) ([]*model.LLMResponse, error) { return nil, errors.New("overloaded") },
		func(bool) ([]*model.LLMResponse, error) {
			return []*model.LLMResponse{{Partial: true}}, errors.New("streaming is not supported")
		},
	}}
	om := &streamingModel{inner: other}
	for range om.GenerateContent(context.Background(), &model.LLMRequest{}, true) {
	}
	for range om.GenerateContent(context.Background(), &model.LLMRequest{}, true) {
	}
	assert.True(t, streamsFor(other.name))
}

// A tool call that can't be read is asked for again once, with a note
// (CA-31).
func TestRetryMalformed(t *testing.T) {
	malformed := &model.LLMResponse{FinishReason: genai.FinishReasonMalformedFunctionCall}
	inner := &callScript{name: "m", calls: []func(bool) ([]*model.LLMResponse, error){
		func(bool) ([]*model.LLMResponse, error) { return []*model.LLMResponse{malformed}, nil },
		func(bool) ([]*model.LLMResponse, error) { return []*model.LLMResponse{whole("ok")}, nil },
	}}
	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}}
	var got []*model.LLMResponse
	for resp, err := range retryMalformed(req, func(r *model.LLMRequest) iter.Seq2[*model.LLMResponse, error] {
		return inner.GenerateContent(context.Background(), r, false)
	}) {
		require.NoError(t, err)
		got = append(got, resp)
	}
	require.Len(t, got, 1)
	assert.Equal(t, "ok", got[0].Content.Parts[0].Text)
	require.Len(t, inner.reqs, 2)
	assert.Len(t, inner.reqs[1].Contents, 2, "the second call carries the note")
	assert.Len(t, req.Contents, 1, "the shared request was changed")
}

func TestCompactedRetry(t *testing.T) {
	assert.False(t, compactedRetry(context.Background()))
	assert.True(t, compactedRetry(withCompactedRetry(context.Background())))
}

// A chain doesn't move on from a context too long until it's been
// summarised to fit (CA-111).
func TestFallbackWaitsForTheSummary(t *testing.T) {
	tooLong := func(bool) ([]*model.LLMResponse, error) {
		return nil, asContextLength(errors.New("prompt is too long"))
	}
	answer := func(bool) ([]*model.LLMResponse, error) { return []*model.LLMResponse{whole("ok")}, nil }
	primary := &callScript{name: "p", calls: []func(bool) ([]*model.LLMResponse, error){tooLong, tooLong}}
	backup := &callScript{name: "b", calls: []func(bool) ([]*model.LLMResponse, error){answer}}
	chain := newFallbackModel([]model.LLM{primary, backup})
	var err error
	for _, e := range chain.GenerateContent(context.Background(), &model.LLMRequest{}, false) {
		err = e
	}
	assert.ErrorIs(t, err, api.ErrContextLength)
	assert.Empty(t, backup.seen, "the chain moved on before the summary")

	var got []*model.LLMResponse
	for resp, err := range chain.GenerateContent(withCompactedRetry(context.Background()), &model.LLMRequest{}, false) {
		require.NoError(t, err)
		got = append(got, resp)
	}
	assert.Len(t, backup.seen, 1, "after the summary, the chain moves on")
	require.NotEmpty(t, got)
}
