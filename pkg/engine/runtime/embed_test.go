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
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// fakeGeminiEmbed answers with one vector per text: its length, then 1.
type fakeGeminiEmbed struct {
	batches [][]string
	err     error
	short   bool
}

func (f *fakeGeminiEmbed) EmbedContent(_ context.Context, model string, contents []*genai.Content, _ *genai.EmbedContentConfig) (*genai.EmbedContentResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	var texts []string
	res := &genai.EmbedContentResponse{}
	for _, c := range contents {
		texts = append(texts, c.Parts[0].Text)
		res.Embeddings = append(res.Embeddings, &genai.ContentEmbedding{Values: []float32{float32(len(c.Parts[0].Text)), 1}})
	}
	f.batches = append(f.batches, texts)
	if f.short {
		res.Embeddings = res.Embeddings[:len(res.Embeddings)-1]
	}
	return res, nil
}

// fakeOpenAIEmbed answers out of order, as the API may.
type fakeOpenAIEmbed struct {
	err  error
	hole bool
}

func (f *fakeOpenAIEmbed) New(_ context.Context, body openai.EmbeddingNewParams, _ ...option.RequestOption) (*openai.CreateEmbeddingResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	texts := body.Input.OfArrayOfStrings
	res := &openai.CreateEmbeddingResponse{}
	for i := len(texts) - 1; i >= 0; i-- {
		if f.hole && i == 0 {
			continue
		}
		res.Data = append(res.Data, openai.Embedding{Index: int64(i), Embedding: []float64{float64(len(texts[i])), 2}})
	}
	return res, nil
}

// Texts are embedded in batches, cut to size (an empty one made
// non-empty), and come back in order, whatever order the provider answers.
func TestEmbedder(t *testing.T) {
	ctx := context.Background()
	texts := make([]string, embedBatch+2)
	for i := range texts {
		texts[i] = strings.Repeat("x", i+1)
	}
	texts[1] = "  "
	texts[2] = strings.Repeat("y", maxEmbedChars+50)

	g := &fakeGeminiEmbed{}
	e := geminiEmbedder("gemini/text-embedding-004", "text-embedding-004", g)
	assert.Equal(t, "gemini/text-embedding-004", e.Model())
	vecs, err := e.Embed(ctx, texts)
	require.NoError(t, err)
	require.Len(t, vecs, len(texts))
	assert.Len(t, g.batches, 2, "two batches")
	assert.Equal(t, "(empty)", g.batches[0][1])
	assert.Equal(t, float32(maxEmbedChars), vecs[2][0], "cut")
	assert.Equal(t, float32(1), vecs[0][0])

	o := openAIEmbedder("openai/text-embedding-3-small", "text-embedding-3-small", &fakeOpenAIEmbed{})
	vecs, err = o.Embed(ctx, []string{"a", "bbb"})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{1, 2}, {3, 2}}, vecs, "in the texts' order")

	for name, tc := range map[string]struct {
		e    *Embedder
		want string
	}{
		"gemini fails":     {geminiEmbedder("g", "m", &fakeGeminiEmbed{err: errors.New("quota")}), "quota"},
		"gemini too few":   {geminiEmbedder("g", "m", &fakeGeminiEmbed{short: true}), "returned 1 embeddings for 2 texts"},
		"openai fails":     {openAIEmbedder("o", "m", &fakeOpenAIEmbed{err: errors.New("401")}), "401"},
		"openai left one":  {openAIEmbedder("o", "m", &fakeOpenAIEmbed{hole: true}), "no embedding for text 0"},
		"gemini empty":     {geminiEmbedder("g", "m", emptyGemini{}), "an empty embedding"},
		"openai misplaced": {openAIEmbedder("o", "m", misplacedOpenAI{}), "out of place"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tc.e.Embed(ctx, []string{"a", "b"})
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// No model is no embedder; a provider without embeddings is refused; a
// Gemini or OpenAI-compatible one is built.
func TestNewEmbedder(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultConfig()
	cfg.LLM.Gemini.APIKey = "k"
	cfg.LLM.OpenAI.APIKey = "sk"
	e, err := NewEmbedder(ctx, cfg, " ")
	require.NoError(t, err)
	assert.Nil(t, e)
	_, err = NewEmbedder(ctx, cfg, "anthropic/claude-x")
	assert.ErrorContains(t, err, "can't embed text")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	keyless := config.DefaultConfig()
	_, err = NewEmbedder(ctx, keyless, "gemini/text-embedding-004")
	assert.Error(t, err, "Gemini without a key")
	for _, ref := range []string{"gemini/text-embedding-004", "openai/text-embedding-3-small"} {
		e, err := NewEmbedder(ctx, cfg, ref)
		require.NoError(t, err, ref)
		assert.Equal(t, ref, e.Model())
	}
}

// emptyGemini answers with empty vectors.
type emptyGemini struct{}

func (emptyGemini) EmbedContent(context.Context, string, []*genai.Content, *genai.EmbedContentConfig) (*genai.EmbedContentResponse, error) {
	return &genai.EmbedContentResponse{Embeddings: []*genai.ContentEmbedding{{}, {}}}, nil
}

// misplacedOpenAI answers for a text that wasn't sent.
type misplacedOpenAI struct{}

func (misplacedOpenAI) New(context.Context, openai.EmbeddingNewParams, ...option.RequestOption) (*openai.CreateEmbeddingResponse, error) {
	return &openai.CreateEmbeddingResponse{Data: []openai.Embedding{{Index: 7, Embedding: []float64{1}}}}, nil
}
