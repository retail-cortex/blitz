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
	"net/url"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"google.golang.org/genai"
)

// NewPainter builds the [images] model's painter (spec_006_filetools
// FS-80): a Gemini image model, through the Gemini API or Vertex AI as
// [llm.gemini] signs in. nil without a model; an error for a provider that
// can't draw or credentials that don't work.
func NewPainter(ctx context.Context, cfg *config.Config) (tools.Painter, error) {
	ref := strings.TrimSpace(cfg.Images.Model)
	if ref == "" {
		return nil, nil
	}
	provider, name := ParseModelRef(ref, cfg.LLM.Provider)
	if provider != "gemini" {
		return nil, fmt.Errorf("%s can't draw pictures: set [images] model to a Gemini image model, like gemini/gemini-3.1-flash-image", ref)
	}
	cc, err := geminiClientConfig(ctx, cfg.LLM.Gemini, policyFrom(cfg.LLM))
	if err != nil {
		return nil, err
	}
	client, err := genai.NewClient(ctx, cc)
	if err != nil {
		return nil, err
	}
	host := "generativelanguage.googleapis.com"
	if cc.Backend == genai.BackendVertexAI {
		host = "aiplatform.googleapis.com"
	}
	if u, err := url.Parse(cc.HTTPOptions.BaseURL); err == nil && u.Host != "" {
		host = u.Hostname()
	}
	return &geminiPainter{models: client.Models, model: name, host: host}, nil
}

// geminiPainter draws with a Gemini image model, which answers with the
// picture inline (and sometimes a line of text).
type geminiPainter struct {
	models geminiModels
	model  string
	host   string
}

func (p *geminiPainter) Model() string { return "gemini/" + p.model }
func (p *geminiPainter) Host() string  { return p.host }

func (p *geminiPainter) Paint(ctx context.Context, req tools.PaintRequest) (*tools.Picture, error) {
	parts := []*genai.Part{{Text: req.Prompt}}
	for _, ref := range req.References {
		parts = append(parts, &genai.Part{InlineData: &genai.Blob{MIMEType: ref.MIME, Data: ref.Data}})
	}
	gc := &genai.GenerateContentConfig{ResponseModalities: []string{string(genai.ModalityText), string(genai.ModalityImage)}}
	if req.AspectRatio != "" || req.Size != "" {
		gc.ImageConfig = &genai.ImageConfig{AspectRatio: req.AspectRatio, ImageSize: req.Size}
	}
	resp, err := p.models.GenerateContent(ctx, p.model, []*genai.Content{genai.NewContentFromParts(parts, genai.RoleUser)}, gc)
	if err != nil {
		return nil, err
	}
	pic := &tools.Picture{Usage: resp.UsageMetadata}
	for _, c := range resp.Candidates {
		if c == nil || c.Content == nil {
			continue
		}
		for _, part := range c.Content.Parts {
			switch {
			case part == nil || part.Thought:
			case part.InlineData != nil && strings.HasPrefix(part.InlineData.MIMEType, "image/") && len(part.InlineData.Data) > 0:
				if pic.Data == nil {
					pic.Data, pic.MIME = part.InlineData.Data, part.InlineData.MIMEType
				}
			case part.Text != "":
				pic.Text += part.Text
			}
		}
	}
	if pic.Data == nil {
		why := strings.TrimSpace(pic.Text)
		if why == "" && len(resp.Candidates) > 0 && resp.Candidates[0] != nil && resp.Candidates[0].FinishReason != "" {
			why = "it stopped: " + string(resp.Candidates[0].FinishReason)
		}
		if why == "" && resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
			why = "the prompt was blocked: " + string(resp.PromptFeedback.BlockReason)
		}
		if why == "" {
			why = "no reason given"
		}
		return pic, errors.New("the model answered without a picture (" + why + ")")
	}
	return pic, nil
}
