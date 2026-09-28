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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func sig(s string) *genai.Part { return &genai.Part{ThoughtSignature: []byte(s)} }

// A part holding only a thought signature is folded into the part before
// it: Vertex AI refuses such parts now and then.
func TestAttachLoneSignatures(t *testing.T) {
	call := &genai.Part{FunctionCall: &genai.FunctionCall{Name: "ls"}, ThoughtSignature: []byte("c")}
	for _, tc := range []struct {
		name string
		in   []*genai.Part
		want []*genai.Part
	}{
		{"after text", []*genai.Part{{Text: "answer"}, sig("s")}, []*genai.Part{{Text: "answer", ThoughtSignature: []byte("s")}}},
		{"a function call keeps its own", []*genai.Part{call}, []*genai.Part{call}},
		{"after a signed part", []*genai.Part{call, sig("s")}, []*genai.Part{call}},
		{"with nothing before it", []*genai.Part{sig("s"), {Text: "x"}}, []*genai.Part{{Text: "x"}}},
		{"a thought is kept", []*genai.Part{{Text: "hmm", Thought: true, ThoughtSignature: []byte("t")}}, []*genai.Part{{Text: "hmm", Thought: true, ThoughtSignature: []byte("t")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orig := &genai.Content{Role: "model", Parts: tc.in}
			before := len(orig.Parts)
			req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", "user"), orig}}
			got := attachLoneSignatures(req)
			require.Len(t, got.Contents, 2)
			assert.Equal(t, tc.want, got.Contents[1].Parts)
			assert.Len(t, orig.Parts, before, "the shared request was changed")
		})
	}
	// Nothing to fold: the same request.
	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", "user")}}
	assert.Same(t, req, attachLoneSignatures(req))
	// A content of nothing but a lone signature goes.
	only := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", "user"), {Role: "model", Parts: []*genai.Part{sig("s")}}}}
	assert.Len(t, attachLoneSignatures(only).Contents, 1)
}

// Only Gemini requests are changed, on their way to the model.
func TestSettingsModelFoldsSignaturesForGemini(t *testing.T) {
	for _, tc := range []struct {
		provider string
		lone     bool
	}{{"gemini", false}, {"anthropic", true}} {
		t.Run(tc.provider, func(t *testing.T) {
			inner := NewMockLLM("m", genai.NewContentFromText("ok", genai.RoleModel))
			m := withModelSettings(inner, tc.provider)
			req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", "user"), {Role: "model", Parts: []*genai.Part{{Text: "a"}, sig("s")}}}}
			for range m.GenerateContent(context.Background(), req, false) {
			}
			require.Len(t, inner.Requests, 1)
			assert.Equal(t, tc.lone, slicesContainsLone(inner.Requests[0].Contents))
		})
	}
}
