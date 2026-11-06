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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

var (
	fromClaude = Origin{API: "anthropic", Provider: "anthropic", Model: "claude-opus-5-5"}
	fromGemini = Origin{API: "gemini", Provider: "gemini", Model: "gemini-3-pro"}
	fromGPT    = Origin{API: "openai", Provider: "openai", Model: "gpt-5"}
)

func TestStateTags(t *testing.T) {
	tagged := tagState(fromClaude, []byte("sig"))
	key, payload, ok := untagState(tagged)
	require.True(t, ok)
	assert.Equal(t, fromClaude.key(), key)
	assert.Equal(t, []byte("sig"), payload)
	assert.Equal(t, tagged, tagState(fromGemini, tagged), "a tagged signature is tagged once")
	_, _, ok = untagState([]byte("sig"))
	assert.False(t, ok, "an untagged signature")
	_, _, ok = untagState([]byte(stateTag + "no separator"))
	assert.False(t, ok, "a tag with no end")
}

// A model's answer records its origin, its reasoning and signatures are
// tagged with it, and its tool calls get Blitz's IDs (CA-02, CA-03).
func TestStampResponse(t *testing.T) {
	resp := &model.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
		{Text: "plan", Thought: true, ThoughtSignature: []byte("s1")},
		{Text: "unsigned reasoning", Thought: true},
		{FunctionCall: &genai.FunctionCall{Name: "read_file"}},
		{FunctionCall: &genai.FunctionCall{ID: "toolu_1", Name: "grep"}},
		{Text: "answer"},
	}}}
	stampResponse(fromClaude, resp)
	o, ok := originOf(resp)
	require.True(t, ok)
	assert.Equal(t, fromClaude, o)
	parts := resp.Content.Parts
	for i := range 2 {
		key, _, ok := untagState(parts[i].ThoughtSignature)
		assert.True(t, ok, "reasoning %d tagged", i)
		assert.Equal(t, fromClaude.key(), key)
	}
	assert.True(t, strings.HasPrefix(parts[2].FunctionCall.ID, bzPrefix), "a call without an ID gets Blitz's")
	assert.Equal(t, "toolu_1", parts[3].FunctionCall.ID, "a provider's ID is kept")
	assert.Nil(t, parts[4].ThoughtSignature, "plain text isn't provider state")

	partial := &model.LLMResponse{Partial: true, Content: &genai.Content{Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "x"}}}}}
	stampResponse(fromClaude, partial)
	assert.Empty(t, partial.Content.Parts[0].FunctionCall.ID, "only the whole answer's calls get IDs")
	stampResponse(fromClaude, nil)
}

// OpenAI's output tokens include its reasoning; usage is kept as Gemini
// counts it, reasoning apart (CA-122).
func TestStampResponseUsage(t *testing.T) {
	for _, tc := range []struct {
		o          Origin
		candidates int32
	}{{fromGPT, 200}, {fromGemini, 300}, {fromClaude, 300}} {
		t.Run(tc.o.API, func(t *testing.T) {
			resp := &model.LLMResponse{UsageMetadata: &genai.GenerateContentResponseUsageMetadata{CandidatesTokenCount: 300, ThoughtsTokenCount: 100}}
			stampResponse(tc.o, resp)
			assert.Equal(t, tc.candidates, resp.UsageMetadata.CandidatesTokenCount)
		})
	}
}

func contentsOf(req *model.LLMRequest) []*genai.Content { return req.Contents }

// Each model reads only the provider state it wrote, restored to its own
// form; another's reasoning is dropped and its signatures stripped (CA-20).
func TestAdaptRequestState(t *testing.T) {
	history := func() *model.LLMRequest {
		return &model.LLMRequest{Contents: []*genai.Content{
			genai.NewContentFromText("hi", genai.RoleUser),
			{Role: genai.RoleModel, Parts: []*genai.Part{
				{Text: "claude thinks", Thought: true, ThoughtSignature: tagState(fromClaude, []byte("c-sig"))},
				{Text: "old reasoning", Thought: true, ThoughtSignature: []byte("untagged")},
				{Text: "said", ThoughtSignature: tagState(fromGemini, []byte("g-sig"))},
				{ThoughtSignature: tagState(fromGemini, []byte("lone"))},
			}},
		}}
	}
	tests := []struct {
		o    Origin
		want []*genai.Part
	}{
		{fromClaude, []*genai.Part{{Text: "claude thinks", Thought: true, ThoughtSignature: []byte("c-sig")}, {Text: "said"}}},
		{fromGemini, []*genai.Part{{Text: "said", ThoughtSignature: []byte("g-sig")}, {ThoughtSignature: []byte("lone")}}},
		{fromGPT, []*genai.Part{{Text: "said"}}},
		{Origin{API: "anthropic", Provider: "bedrock", Model: "claude-opus-5-5"}, []*genai.Part{{Text: "said"}}},
	}
	for _, tt := range tests {
		t.Run(tt.o.key(), func(t *testing.T) {
			req := history()
			got := adaptRequest(tt.o, req)
			require.Len(t, contentsOf(got), 2)
			assert.Equal(t, tt.want, got.Contents[1].Parts)
			assert.Len(t, req.Contents[1].Parts, 4, "the shared request was changed")
		})
	}
	assert.Nil(t, adaptRequest(fromClaude, nil))
}

// Tool calls and their results pair by IDs each API takes (CA-23): Gemini
// gets none of Blitz's, the others get one for a call ADK left without.
func TestAdaptRequestIDs(t *testing.T) {
	history := func() *model.LLMRequest {
		return &model.LLMRequest{Contents: []*genai.Content{
			genai.NewContentFromText("hi", genai.RoleUser),
			{Role: genai.RoleModel, Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{ID: "bz-1", Name: "a"}},
				{FunctionCall: &genai.FunctionCall{Name: "b"}},
				{FunctionCall: &genai.FunctionCall{Name: "c"}},
			}},
			{Role: genai.RoleUser, Parts: []*genai.Part{
				{FunctionResponse: &genai.FunctionResponse{ID: "bz-1", Name: "a"}},
				{FunctionResponse: &genai.FunctionResponse{Name: "c"}},
				{FunctionResponse: &genai.FunctionResponse{Name: "b"}},
			}},
		}}
	}
	got := adaptRequest(fromGemini, history())
	for _, p := range got.Contents[1].Parts {
		assert.Empty(t, p.FunctionCall.ID, "Gemini gets no IDs of Blitz's")
	}
	assert.Empty(t, got.Contents[2].Parts[0].FunctionResponse.ID)

	got = adaptRequest(fromClaude, history())
	calls, results := got.Contents[1].Parts, got.Contents[2].Parts
	assert.Equal(t, "bz-1", calls[0].FunctionCall.ID)
	assert.Equal(t, "bz-legacy-1", calls[1].FunctionCall.ID)
	assert.Equal(t, "bz-legacy-2", calls[2].FunctionCall.ID)
	assert.Equal(t, "bz-1", results[0].FunctionResponse.ID)
	assert.Equal(t, "bz-legacy-2", results[1].FunctionResponse.ID, "paired by name")
	assert.Equal(t, "bz-legacy-1", results[2].FunctionResponse.ID, "paired by name")
}

// A conversation that opens with the model (a compaction summary) opens
// with the user for Anthropic (CA-04, CA-21); the others take it as it is.
func TestAdaptRequestOpensWithTheUser(t *testing.T) {
	history := func() *model.LLMRequest {
		return &model.LLMRequest{Contents: []*genai.Content{
			{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "summary"}, {Text: "hidden", Thought: true, ThoughtSignature: tagState(fromClaude, nil)}}},
			genai.NewContentFromText("next", genai.RoleUser),
		}}
	}
	got := adaptRequest(fromClaude, history())
	assert.Equal(t, genai.RoleUser, got.Contents[0].Role)
	require.Len(t, got.Contents[0].Parts, 1)
	assert.Equal(t, "[Earlier in this conversation]\nsummary", got.Contents[0].Parts[0].Text)
	assert.Equal(t, genai.RoleModel, adaptRequest(fromGemini, history()).Contents[0].Role)

	empty := &model.LLMRequest{Contents: []*genai.Content{{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "bz-1", Name: "a"}}}}}}
	assert.Equal(t, "[Earlier in this conversation]", adaptRequest(fromClaude, empty).Contents[0].Parts[0].Text)
}
