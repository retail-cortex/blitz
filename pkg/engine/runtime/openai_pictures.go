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
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/images"
	"google.golang.org/genai"
)

// OpenAI's image models (spec_058_pictures PIC-04): pictures from a prompt
// (images/generations), or starting from references (images/edits), for
// Pictures and the agent's generate_image, with the OpenAI provider's key.

// openAIImageAPI is the SDK's image calls, for tests.
type openAIImageAPI interface {
	Generate(ctx context.Context, body openai.ImageGenerateParams, opts ...option.RequestOption) (*openai.ImagesResponse, error)
	Edit(ctx context.Context, body openai.ImageEditParams, opts ...option.RequestOption) (*openai.ImagesResponse, error)
}

// openAIDrawer makes pictures with one OpenAI image model.
type openAIDrawer struct {
	api  openAIImageAPI
	name string
	host string
	// key is masked out of OpenAI's messages, which errors carry.
	key string
}

// newOpenAIDrawer is the OpenAI image model name with cfg's OpenAI key; an
// error without one.
func newOpenAIDrawer(ctx context.Context, cfg *config.Config, name string) (*openAIDrawer, error) {
	if cfg.LLM.OpenAI.APIKey == "" && cfg.LLM.OpenAI.APIKeyCommand == "" {
		return nil, errors.New("OpenAI's image models need an OpenAI API key: set one in Settings › Providers")
	}
	key, base, opts, err := openAIKey(ctx, cfg.LLM, "openai", policyFrom(cfg.LLM))
	if err != nil {
		return nil, err
	}
	opts = append(opts, option.WithAPIKey(key))
	host := "api.openai.com"
	if base != "" {
		opts = append(opts, option.WithBaseURL(base))
		if u, err := url.Parse(base); err == nil && u.Host != "" {
			host = u.Hostname()
		}
	}
	client := openai.NewClient(opts...)
	return &openAIDrawer{api: &client.Images, name: name, host: host, key: key}, nil
}

// openAISizes are the sizes an aspect ratio asks for ("" and others: the
// model's own choice).
var openAISizes = map[string]openai.ImageGenerateParamsSize{
	"1:1": openai.ImageGenerateParamsSize1024x1024,
	"3:2": openai.ImageGenerateParamsSize1536x1024,
	"2:3": openai.ImageGenerateParamsSize1024x1536,
}

// openAIFormats are the output formats by MIME type.
var openAIFormats = map[string]openai.ImageGenerateParamsOutputFormat{
	"image/png":  openai.ImageGenerateParamsOutputFormatPNG,
	"image/jpeg": openai.ImageGenerateParamsOutputFormatJPEG,
	"image/webp": openai.ImageGenerateParamsOutputFormatWebP,
}

// draw makes count pictures of prompt, from refs when there are any (an
// edit), at ratio, as format ("" for PNG).
func (d *openAIDrawer) draw(ctx context.Context, prompt string, refs []tools.Reference, count int, ratio, format string) ([]tools.Picture, *genai.GenerateContentResponseUsageMetadata, error) {
	count = max(count, 1)
	size := openAISizes[ratio]
	if size == "" {
		size = openai.ImageGenerateParamsSizeAuto
	}
	outFormat := openAIFormats[format]
	if outFormat == "" {
		outFormat, format = openai.ImageGenerateParamsOutputFormatPNG, "image/png"
	}
	var resp *openai.ImagesResponse
	var err error
	if len(refs) == 0 {
		resp, err = d.api.Generate(ctx, openai.ImageGenerateParams{Prompt: prompt, Model: openai.ImageModel(d.name), N: openai.Int(int64(count)), Size: size, OutputFormat: outFormat})
	} else {
		files := make([]io.Reader, len(refs))
		for i, r := range refs {
			files[i] = openai.File(bytes.NewReader(r.Data), "reference-"+fmt.Sprint(i+1)+extOf(r.MIME), r.MIME)
		}
		resp, err = d.api.Edit(ctx, openai.ImageEditParams{Image: openai.ImageEditParamsImageUnion{OfFileArray: files}, Prompt: prompt,
			Model: openai.ImageModel(d.name), N: openai.Int(int64(count)), Size: openai.ImageEditParamsSize(size), OutputFormat: openai.ImageEditParamsOutputFormat(outFormat)})
	}
	if err != nil {
		// OpenAI's reason (a safety refusal, a size it doesn't take), which
		// the SDK's error leaves out.
		return nil, nil, providerError(err, "openai", d.name, d.key)
	}
	var pics []tools.Picture
	for _, im := range resp.Data {
		if im.B64JSON == "" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(im.B64JSON)
		if err != nil {
			return nil, nil, fmt.Errorf("a picture didn't decode: %w", err)
		}
		pics = append(pics, tools.Picture{Data: data, MIME: format})
	}
	usage := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: int32(resp.Usage.InputTokens), CandidatesTokenCount: int32(resp.Usage.OutputTokens), TotalTokenCount: int32(resp.Usage.TotalTokens)}
	if len(pics) == 0 {
		return nil, usage, errors.New("the model didn't draw it (no picture came back)")
	}
	return pics, usage, nil
}

// openAIPictures is Pictures' maker for an OpenAI image model: one request
// a turn, its references (if any) the pictures it starts from.
type openAIPictures struct {
	*openAIDrawer
	info images.Model
}

func (o *openAIPictures) Model() string      { return "openai/" + o.name }
func (o *openAIPictures) Info() images.Model { return o.info }
func (o *openAIPictures) Host() string       { return o.host }

func (o *openAIPictures) Make(ctx context.Context, call PictureCall) (*PictureAnswer, error) {
	pics, usage, err := o.draw(ctx, call.Prompt, call.References, call.Count, call.AspectRatio, call.Format)
	out := &PictureAnswer{Pictures: pics, Usage: usage}
	if err != nil && pics == nil {
		return out, err
	}
	return out, nil
}

// openAIPainter is generate_image's painter for an OpenAI image model.
type openAIPainter struct{ *openAIDrawer }

func (p *openAIPainter) Model() string { return "openai/" + p.name }
func (p *openAIPainter) Host() string  { return p.host }

// Info is the model's catalog entry: generate_image takes its references,
// shapes and count (spec_058 PIC-81).
func (p *openAIPainter) Info() images.Model {
	m, _ := images.Lookup(p.name)
	return m
}

func (p *openAIPainter) Paint(ctx context.Context, req tools.PaintRequest) (*tools.Picture, error) {
	pics, usage, err := p.draw(ctx, req.Prompt, req.References, req.Count, req.AspectRatio, "")
	if err != nil {
		return nil, err
	}
	pic := pics[0]
	pic.Usage, pic.More = usage, pics[1:]
	return &pic, nil
}
