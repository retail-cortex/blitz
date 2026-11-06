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
	"iter"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/runtime/wirefake"
	"github.com/retail-cortex/blitz/pkg/modelfacts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Each API's words for a refusal teach their fact; anything else stays an
// error (CA-13, CA-15).
func TestRecogniseRefusal(t *testing.T) {
	tests := []struct {
		msg         string
		kind, value string
	}{
		{`registry.ollama.ai/library/gemma3 does not support tools`, modelfacts.NoTools, ""},
		{`Function calling is not enabled for models/gemma-3-27b-it`, modelfacts.NoTools, ""},
		{`400 Bad Request: tools are not supported by this model`, modelfacts.NoTools, ""},
		{`model does not support image input`, modelfacts.NoImages, ""},
		{`Image input modality is not enabled for models/gemini-x`, modelfacts.NoImages, ""},
		{`vision is not supported`, modelfacts.NoImages, ""},
		{`this model does not support streaming`, modelfacts.NoStream, ""},
		{`Unsupported parameter: 'stream' is not supported with this model.`, modelfacts.NoStream, ""},
		{`Unsupported parameter: 'temperature' is not supported with this model.`, modelfacts.NoParam, "temperature"},
		{`Invalid JSON payload received. Unknown name "topK": Cannot find field.`, modelfacts.NoParam, "top_k"},
		{`seed: Extra inputs are not permitted`, modelfacts.NoParam, "seed"},
		{`Unsupported parameter: 'reasoning.effort' is not supported with this model.`, modelfacts.NoThinking, ""},
		{`Developer instruction is not enabled for models/gemma-3`, modelfacts.NoSystem, ""},
		{`This model's maximum context length is 8192 tokens. However, your messages resulted in 9001 tokens.`, modelfacts.ContextWindow, "8192"},
		{`prompt is too long: 210000 tokens > 200000 maximum`, modelfacts.ContextWindow, "200000"},
		{`The input token count (1200000) exceeds the maximum number of tokens allowed (1048576).`, modelfacts.ContextWindow, "1048576"},
		{`response_schema is not supported`, modelfacts.NoStructured, ""},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			f, ok := recogniseRefusal(errors.New(tt.msg))
			require.True(t, ok)
			assert.Equal(t, tt.kind, f.Kind)
			assert.Equal(t, tt.value, f.Value)
			assert.NotEmpty(t, f.Why)
		})
	}
	for _, msg := range []string{"overloaded", "Unsupported parameter: 'input' is required", "rate limit exceeded", ""} {
		_, ok := recogniseRefusal(errors.New(msg))
		assert.False(t, ok, "not a refusal Blitz knows: %q", msg)
	}
	_, ok := recogniseRefusal(nil)
	assert.False(t, ok)
	assert.False(t, retryable(modelfacts.Fact{Kind: modelfacts.ContextWindow}))
	assert.True(t, retryable(modelfacts.Fact{Kind: modelfacts.NoTools}))
}

// What the record says a model refuses is left out of its requests (CA-13).
func TestApplyFacts(t *testing.T) {
	sys := genai.NewContentFromText("be terse", genai.RoleUser)
	req := func() *model.LLMRequest {
		return &model.LLMRequest{
			Config: &genai.GenerateContentConfig{
				SystemInstruction: sys,
				Temperature:       genai.Ptr[float32](0.2), TopP: genai.Ptr[float32](0.9), TopK: genai.Ptr[float32](3), Seed: genai.Ptr[int32](7),
				ThinkingConfig: &genai.ThinkingConfig{ThinkingBudget: genai.Ptr[int32](100)},
				Tools:          []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "read_file"}}}, {GoogleSearch: &genai.GoogleSearch{}}},
			},
			Contents: []*genai.Content{
				{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "look"}, {InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{1}}}}},
				{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "read_file", Args: map[string]any{"path": "a"}}}}},
				{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{Name: "read_file", Response: map[string]any{"ok": true}}}}},
			},
		}
	}
	full := Info("openai", "openai", "gpt-4o")
	same, stream := applyFacts(full, req(), true)
	assert.True(t, stream)
	assert.Len(t, same.Config.Tools, 2, "nothing refused, nothing changed")

	limited := full
	limited.Tools, limited.Vision, limited.Streams = ToolsNone, false, false
	limited.Refused = []string{"temperature", "top_p", "top_k", "seed"}
	limited.Thinking = ThinkingNone
	limited.Learned = []modelfacts.Fact{{Kind: modelfacts.NoThinking}, {Kind: modelfacts.NoSystem}}
	orig := req()
	got, stream := applyFacts(limited, orig, true)
	assert.False(t, stream)
	require.Len(t, got.Config.Tools, 1, "the provider's own tools stay")
	assert.NotNil(t, got.Config.Tools[0].GoogleSearch)
	assert.Nil(t, got.Config.Temperature)
	assert.Nil(t, got.Config.TopP)
	assert.Nil(t, got.Config.TopK)
	assert.Nil(t, got.Config.Seed)
	assert.Nil(t, got.Config.ThinkingConfig)
	assert.Nil(t, got.Config.SystemInstruction)
	assert.Equal(t, "be terse", got.Contents[0].Parts[0].Text, "the system instructions open the first user turn")
	assert.NotNil(t, got.Contents[0].Parts[2].Text, "the image is a note")
	assert.Nil(t, got.Contents[0].Parts[2].InlineData)
	assert.Equal(t, `[Called read_file with {"path":"a"}]`, got.Contents[1].Parts[0].Text)
	assert.Equal(t, `[read_file returned {"ok":true}]`, got.Contents[2].Parts[0].Text)
	assert.NotNil(t, orig.Config.Temperature, "the shared request was changed")
	assert.NotNil(t, orig.Contents[0].Parts[1].InlineData)

	none, _ := applyFacts(limited, nil, true)
	assert.Nil(t, none)
	assert.Len(t, foldSystem(sys, nil), 1, "no user turn: one with the instructions")
	assert.Empty(t, foldSystem(&genai.Content{}, nil))
}

// refusing answers first with a refusal, then as a model that can.
type refusing struct {
	name     string
	refusal  error
	requests []*model.LLMRequest
	streams  []bool
}

func (m *refusing) Name() string { return m.name }

func (m *refusing) GenerateContent(_ context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.requests, m.streams = append(m.requests, req), append(m.streams, stream)
		if len(m.requests) == 1 && m.refusal != nil {
			yield(nil, m.refusal)
			return
		}
		yield(&model.LLMResponse{Content: genai.NewContentFromText("ok", genai.RoleModel), TurnComplete: true}, nil)
	}
}

// A refusal Blitz knows is learned, the call goes again without what was
// refused, once, and the person is told once (CA-13, CA-14).
func TestSettingsModelLearns(t *testing.T) {
	s := tempLearned(t)
	tools := &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "read_file"}}}}}
	run := func(inner *refusing, stream bool) ([]*model.LLMResponse, error) {
		m := &settingsModel{inner: inner, provider: "openai", configured: "ollama"}
		var out []*model.LLMResponse
		var last error
		for resp, err := range m.GenerateContent(t.Context(), &model.LLMRequest{Config: tools, Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}}, stream) {
			if err != nil {
				last = err
				continue
			}
			out = append(out, resp)
		}
		return out, last
	}

	inner := &refusing{name: "gemma3", refusal: errors.New("registry.ollama.ai/library/gemma3 does not support tools")}
	out, err := run(inner, true)
	require.NoError(t, err)
	require.Len(t, inner.requests, 2, "sent again once")
	assert.Nil(t, inner.requests[1].Config.Tools, "without tools")
	require.Len(t, out, 1)
	assert.Equal(t, []string{"gemma3 can't use tools: it answers without them from now on."}, out[0].CustomMetadata[LearnedKey])
	facts := s.Facts("ollama", "gemma3", time.Now())
	require.Len(t, facts, 1)
	assert.Equal(t, modelfacts.NoTools, facts[0].Kind)

	again := &refusing{name: "gemma3"}
	out, err = run(again, true)
	require.NoError(t, err)
	require.Len(t, again.requests, 1)
	assert.Nil(t, again.requests[0].Config.Tools, "learned: never offered tools again")
	assert.Nil(t, out[0].CustomMetadata[LearnedKey], "told once")

	streaming := &refusing{name: "streamless", refusal: errors.New("this model does not support streaming")}
	_, err = run(streaming, true)
	require.NoError(t, err)
	assert.Equal(t, []bool{true, false}, streaming.streams)

	unknown := &refusing{name: "flaky", refusal: errors.New("overloaded")}
	_, err = run(unknown, false)
	assert.EqualError(t, err, "overloaded", "not a refusal Blitz knows: an error, never resent")
	assert.Len(t, unknown.requests, 1)

	window := &refusing{name: "small", refusal: errors.New("This model's maximum context length is 8192 tokens.")}
	_, err = run(window, false)
	require.Error(t, err, "a window can't be met by resending")
	assert.Contains(t, err.Error(), "small holds 8192 tokens")
	assert.Len(t, window.requests, 1)
	assert.Equal(t, 8192, Info("openai", "ollama", "small").ContextWindow)
}

// A fallback chain skips a member that can't use tools when the call
// offers them and another can; with none that can, the first answers
// without them (CA-103).
func TestFallbackSkipsToollessModels(t *testing.T) {
	tempLearned(t)
	setListed("ollama", "toolless", listedFacts{Tools: ptr(false)})
	setListed("ollama", "also-toolless", listedFacts{Tools: ptr(false)})
	withTools := &model.LLMRequest{Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "read_file"}}}}}}

	first := &refusing{name: "toolless"}
	second := &refusing{name: "claude-opus-5-5"}
	chain := newFallbackModel([]model.LLM{
		&settingsModel{inner: first, provider: "openai", configured: "ollama"},
		&settingsModel{inner: second, provider: "anthropic"},
	})
	for range chain.GenerateContent(t.Context(), withTools, false) {
	}
	assert.Empty(t, first.requests, "skipped: it can't use the tools offered")
	assert.Len(t, second.requests, 1)

	for range chain.GenerateContent(t.Context(), &model.LLMRequest{}, false) {
	}
	assert.Len(t, first.requests, 1, "no tools offered: it answers")

	a, b := &refusing{name: "toolless"}, &refusing{name: "also-toolless"}
	both := newFallbackModel([]model.LLM{
		&settingsModel{inner: a, provider: "openai", configured: "ollama"},
		&settingsModel{inner: b, provider: "openai", configured: "ollama"},
	})
	for range both.GenerateContent(t.Context(), withTools, false) {
	}
	require.Len(t, a.requests, 1, "none can: the first answers, without tools")
	assert.Nil(t, a.requests[0].Config.Tools)
}

// Through the real adapter and engine: a model refusing tools in its
// server's words is learned, and the turn goes on without them.
func TestConformanceLearnsARefusal(t *testing.T) {
	tempLearned(t)
	cfg := conformanceConfig(t)
	ws := wires(t, cfg)
	o := ws["openai"]
	eng := conformanceEngine(t, cfg, modelFor(t, cfg, o))
	o.fake.Script(wirefake.Reply{Status: 400, Refusal: "gpt-5 does not support tools"}, wirefake.Reply{Text: "Hello."})
	_, err := collect(t, eng, "s", "hello")
	require.NoError(t, err)
	reqs := o.fake.Requests()
	require.Len(t, reqs, 2)
	assert.NotEmpty(t, reqs[0]["tools"])
	assert.Empty(t, reqs[1]["tools"], "sent again without tools")
	assert.Equal(t, ToolsNone, Info("openai", "openai", "gpt-5").Tools)
}

// The output limit a provider states is kept with what's learned of the
// model, so another wrapper (another workspace, a restart) starts from it.
func TestOutputCapIsKept(t *testing.T) {
	s := tempLearned(t)
	_, err := s.Learn("anthropic", "claude-x", modelfacts.Fact{Kind: modelfacts.MaxOutput, Value: "4096", At: time.Now()})
	require.NoError(t, err)
	m := &outputCapModel{inner: namedModel{"claude-x"}, provider: "anthropic"}
	got := m.clamp(&model.LLMRequest{Config: &genai.GenerateContentConfig{MaxOutputTokens: 65536}})
	assert.EqualValues(t, 4096, got.Config.MaxOutputTokens)
	plain := &outputCapModel{inner: namedModel{"claude-x"}}
	assert.EqualValues(t, 65536, plain.clamp(&model.LLMRequest{Config: &genai.GenerateContentConfig{MaxOutputTokens: 65536}}).Config.MaxOutputTokens)
}
