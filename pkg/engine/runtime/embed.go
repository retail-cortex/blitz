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
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/retail-cortex/blitz/pkg/config"
	"google.golang.org/genai"
)

// embedBatch is the most texts one embedding call takes (Gemini's limit
// is 100, OpenAI's 2048).
const embedBatch = 64

// maxEmbedChars cuts a text before it's embedded (a chunk is about 2 KB;
// a title and summary add a little).
const maxEmbedChars = 8000

// Embedder turns texts into vectors with a provider's embedding model:
// the semantic half of workspace search (spec_search_035 SRCH-50).
type Embedder struct {
	ref   string
	embed func(ctx context.Context, texts []string) ([][]float32, error)
}

// Model is the embedding model, as "provider/model".
func (e *Embedder) Model() string { return e.ref }

// Embed embeds texts, in batches, in order.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += embedBatch {
		batch := texts[start:min(start+embedBatch, len(texts))]
		cut := make([]string, len(batch))
		for i, t := range batch {
			if len(t) > maxEmbedChars {
				t = t[:maxEmbedChars]
			}
			if strings.TrimSpace(t) == "" {
				t = "(empty)"
			}
			cut[i] = t
		}
		vecs, err := e.embed(ctx, cut)
		if err != nil {
			return nil, err
		}
		if len(vecs) != len(cut) {
			return nil, fmt.Errorf("%s returned %d embeddings for %d texts", e.ref, len(vecs), len(cut))
		}
		out = append(out, vecs...)
	}
	return out, nil
}

// geminiEmbedAPI is the part of genai's Models an embedder uses.
type geminiEmbedAPI interface {
	EmbedContent(ctx context.Context, model string, contents []*genai.Content, config *genai.EmbedContentConfig) (*genai.EmbedContentResponse, error)
}

// openAIEmbedAPI is the part of openai's client an embedder uses.
type openAIEmbedAPI interface {
	New(ctx context.Context, body openai.EmbeddingNewParams, opts ...option.RequestOption) (*openai.CreateEmbeddingResponse, error)
}

// NewEmbedder builds the embedder for ref ("provider/model", the
// search.embedding_model setting): Gemini (the Gemini API or Vertex AI,
// as [llm.gemini] signs in), OpenAI, or an OpenAI-compatible server such
// as Ollama. nil without a model.
func NewEmbedder(ctx context.Context, cfg *config.Config, ref string) (*Embedder, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	provider, name := ParseModelRef(ref, cfg.LLM.Provider)
	full := provider + "/" + name
	pol := policyFrom(cfg.LLM)
	switch provider {
	case "gemini":
		cc, err := geminiClientConfig(ctx, cfg.LLM.Gemini, pol)
		if err != nil {
			return nil, err
		}
		client, err := genai.NewClient(ctx, cc)
		if err != nil {
			return nil, err
		}
		return geminiEmbedder(full, name, client.Models), nil
	case "openai", "ollama":
		key, base, opts, err := openAIKey(ctx, cfg.LLM.OpenAI, provider, pol)
		if err != nil {
			return nil, err
		}
		opts = append(opts, option.WithAPIKey(key))
		if base != "" {
			opts = append(opts, option.WithBaseURL(base))
		}
		client := openai.NewClient(opts...)
		return openAIEmbedder(full, name, &client.Embeddings), nil
	}
	return nil, fmt.Errorf("%s can't embed text: use a Gemini, OpenAI or Ollama embedding model", ref)
}

func geminiEmbedder(ref, model string, api geminiEmbedAPI) *Embedder {
	return &Embedder{ref: ref, embed: func(ctx context.Context, texts []string) ([][]float32, error) {
		contents := make([]*genai.Content, len(texts))
		for i, t := range texts {
			contents[i] = genai.NewContentFromText(t, genai.RoleUser)
		}
		res, err := api.EmbedContent(ctx, model, contents, nil)
		if err != nil {
			return nil, err
		}
		out := make([][]float32, len(res.Embeddings))
		for i, e := range res.Embeddings {
			if e == nil || len(e.Values) == 0 {
				return nil, errors.New("an empty embedding")
			}
			out[i] = e.Values
		}
		return out, nil
	}}
}

func openAIEmbedder(ref, model string, api openAIEmbedAPI) *Embedder {
	return &Embedder{ref: ref, embed: func(ctx context.Context, texts []string) ([][]float32, error) {
		res, err := api.New(ctx, openai.EmbeddingNewParams{Model: model, Input: openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts}})
		if err != nil {
			return nil, err
		}
		out := make([][]float32, len(texts))
		for _, d := range res.Data {
			if d.Index < 0 || int(d.Index) >= len(out) || len(d.Embedding) == 0 {
				return nil, fmt.Errorf("an embedding out of place (%d)", d.Index)
			}
			v := make([]float32, len(d.Embedding))
			for j, f := range d.Embedding {
				v[j] = float32(f)
			}
			out[d.Index] = v
		}
		for i, v := range out {
			if v == nil {
				return nil, fmt.Errorf("no embedding for text %d", i)
			}
		}
		return out, nil
	}}
}
