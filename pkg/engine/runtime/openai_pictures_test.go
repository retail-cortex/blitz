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
	"encoding/base64"
	"errors"
	"io"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeOpenAIImages answers each call with pictures of its own, and keeps
// what it was asked.
type fakeOpenAIImages struct {
	gen   *openai.ImageGenerateParams
	edit  *openai.ImageEditParams
	refs  int
	empty bool
	bad   bool // an empty picture, then one that isn't base64
	err   error
}

func (f *fakeOpenAIImages) answer(n int64) (*openai.ImagesResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	res := &openai.ImagesResponse{Usage: openai.ImagesResponseUsage{InputTokens: 50, OutputTokens: 1000, TotalTokens: 1050}}
	if f.bad {
		res.Data = []openai.Image{{}, {B64JSON: "!!"}}
		return res, nil
	}
	if !f.empty {
		for range n {
			res.Data = append(res.Data, openai.Image{B64JSON: base64.StdEncoding.EncodeToString([]byte("PNG"))})
		}
	}
	return res, nil
}

func (f *fakeOpenAIImages) Generate(_ context.Context, body openai.ImageGenerateParams, _ ...option.RequestOption) (*openai.ImagesResponse, error) {
	f.gen = &body
	return f.answer(body.N.Value)
}

func (f *fakeOpenAIImages) Edit(_ context.Context, body openai.ImageEditParams, _ ...option.RequestOption) (*openai.ImagesResponse, error) {
	f.edit = &body
	for _, r := range body.Image.OfFileArray {
		if b, _ := io.ReadAll(r); len(b) > 0 {
			f.refs++
		}
	}
	return f.answer(body.N.Value)
}

// OpenAI's image models draw from a prompt, or from references through an
// edit, at the ratio's size, in the format asked; Pictures and
// generate_image both use them (spec_058_pictures PIC-04).
func TestOpenAIPictures(t *testing.T) {
	fake := &fakeOpenAIImages{}
	d := &openAIDrawer{api: fake, name: "gpt-image-2", host: "api.openai.com"}
	info, ok := images.Lookup("gpt-image-2")
	require.True(t, ok)
	pictures := &openAIPictures{openAIDrawer: d, info: info}
	assert.Equal(t, "openai/gpt-image-2", pictures.Model())

	ans, err := pictures.Make(context.Background(), PictureCall{Prompt: "a poster", Count: 2, AspectRatio: "3:2", Format: "image/webp"})
	require.NoError(t, err)
	require.Len(t, ans.Pictures, 2)
	assert.Equal(t, []byte("PNG"), ans.Pictures[0].Data)
	assert.Equal(t, "image/webp", ans.Pictures[0].MIME)
	assert.Equal(t, openai.ImageGenerateParamsSize1536x1024, fake.gen.Size)
	assert.Equal(t, openai.ImageGenerateParamsOutputFormatWebP, fake.gen.OutputFormat)
	assert.Equal(t, openai.ImageModel("gpt-image-2"), fake.gen.Model)
	assert.EqualValues(t, 1000, ans.Usage.CandidatesTokenCount)

	ans, err = pictures.Make(context.Background(), PictureCall{Prompt: "make it blue", References: []tools.Reference{{MIME: "image/png", Data: []byte("a")}, {MIME: "image/jpeg", Data: []byte("b")}}})
	require.NoError(t, err)
	require.NotNil(t, fake.edit, "references: an edit")
	assert.Equal(t, 2, fake.refs)
	assert.Equal(t, "image/png", ans.Pictures[0].MIME, "PNG unless asked")

	painter := &openAIPainter{d}
	pic, err := painter.Paint(context.Background(), tools.PaintRequest{Prompt: "a logo", AspectRatio: "1:1"})
	require.NoError(t, err)
	assert.Equal(t, []byte("PNG"), pic.Data)
	assert.NotNil(t, pic.Usage)
	assert.Equal(t, openai.ImageGenerateParamsSize1024x1024, fake.gen.Size)

	// generate_image on the catalog (PIC-81): its model's entry, and several at once.
	assert.Equal(t, info, painter.Info())
	pic, err = painter.Paint(context.Background(), tools.PaintRequest{Prompt: "logos", Count: 3})
	require.NoError(t, err)
	assert.EqualValues(t, 3, fake.gen.N.Value)
	assert.Len(t, pic.More, 2, "the request's other pictures")

	fake.empty = true
	_, err = painter.Paint(context.Background(), tools.PaintRequest{Prompt: "nothing"})
	assert.ErrorContains(t, err, "didn't draw it")
	fake.empty, fake.bad = false, true
	_, err = pictures.Make(context.Background(), PictureCall{Prompt: "x"})
	assert.ErrorContains(t, err, "didn't decode")
	assert.Equal(t, info, pictures.Info())
	assert.Equal(t, "openai/gpt-image-2", painter.Model())
	fake.bad, fake.err = false, errors.New("400 content policy")
	_, err = pictures.Make(context.Background(), PictureCall{Prompt: "x"})
	assert.ErrorContains(t, err, "content policy")
}

// OpenAI's image models are in the catalog, priced per picture; they need
// the OpenAI key, and other names aren't drawn with them.
func TestOpenAIImageModels(t *testing.T) {
	m, ok := images.Lookup("openai/gpt-image-2.5-sunburst")
	require.True(t, ok)
	assert.Equal(t, images.KindOpenAI, m.Kind)
	assert.True(t, m.PricedPerPicture())
	assert.Equal(t, "openai", m.ProviderOf())
	future, ok := images.Lookup("gpt-image-3")
	require.True(t, ok, "a newer one, assumed")
	assert.True(t, future.Assumed)
	gem, _ := images.Lookup("gemini-3.1-flash-image")
	assert.Equal(t, "gemini", gem.ProviderOf())
	assert.False(t, gem.PricedPerPicture())

	cfg := config.DefaultConfig()
	_, err := NewPictureMaker(context.Background(), cfg, "openai/gpt-image-2")
	assert.ErrorContains(t, err, "need an OpenAI API key")
	cfg.LLM.OpenAI.APIKey = "sk-test"
	mk, err := NewPictureMaker(context.Background(), cfg, "openai/gpt-image-2")
	require.NoError(t, err)
	assert.Equal(t, "api.openai.com", mk.Host())
	_, err = NewPictureMaker(context.Background(), cfg, "openai/gpt-6.1-sol")
	assert.ErrorContains(t, err, "isn't one of OpenAI's image models")
	cfg.Images.Model = "openai/gpt-image-1"
	p, err := NewPainter(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "openai/gpt-image-1", p.Model())
}

// Ollama's own address replaces OpenAI's for Ollama's models; each
// provider's chosen menu models are its own (MOD-42).
func TestOllamaEndpointAndChosenModels(t *testing.T) {
	_, base, _, err := openAIKey(context.Background(), config.LLMConfig{Ollama: config.OllamaConfig{BaseURL: "http://gpu:11434/v1"}}, "ollama", retryPolicy{})
	require.NoError(t, err)
	assert.Equal(t, "http://gpu:11434/v1", base)
	cfg := config.DefaultConfig()
	cfg.LLM.Gemini.Models, cfg.LLM.Anthropic.Models, cfg.LLM.OpenAI.Models, cfg.LLM.Ollama.Models = []string{"g"}, []string{"a"}, []string{"o"}, []string{"l"}
	for p, want := range map[string][]string{"gemini": {"g"}, "anthropic": {"a"}, "openai": {"o"}, "ollama": {"l"}, "azure": nil} {
		assert.Equal(t, want, chosenModels(cfg, p), p)
	}
}
