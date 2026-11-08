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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/images"
	"google.golang.org/genai"
)

// NewPainter builds the [images] model's painter (spec_006_filetools
// FS-80): a Gemini image model, through the Gemini API or Vertex AI as
// [llm.gemini] signs in, or one of the organization's image models through
// its service (MDL-95a). nil without a model; an error for a provider that
// can't draw or credentials that don't work.
func NewPainter(ctx context.Context, cfg *config.Config) (tools.Painter, error) {
	ref := strings.TrimSpace(cfg.Images.Model)
	if ref == "" {
		return nil, nil
	}
	provider, name := ParseModelRef(ref, cfg.LLM.Provider)
	if provider == "enterprise" {
		return newOrgPainter(ctx, cfg, name)
	}
	if provider == "openai" { // spec_058_pictures PIC-04
		if m, ok := images.Lookup(name); !ok || m.Kind != images.KindOpenAI {
			return nil, fmt.Errorf("%s isn't one of OpenAI's image models: set [images] model to one, like openai/gpt-image-2", ref)
		}
		d, err := newOpenAIDrawer(ctx, cfg, name)
		if err != nil {
			return nil, err
		}
		return &openAIPainter{d}, nil
	}
	if provider != "gemini" {
		return nil, fmt.Errorf("%s can't draw pictures: set [images] model to a Gemini or OpenAI image model, like gemini/gemini-3.1-flash-image or openai/gpt-image-2, or one of your organization's", ref)
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
	info, known := images.Lookup(name)
	if known && info.Kind == images.KindImagen { // spec_058 PIC-81: Imagen draws through generateImages
		return &imagenPainter{pictures: &geminiPictures{img: client.Models, name: name, info: info, host: host}}, nil
	}
	if known && info.Kind == images.KindGemini {
		return &catalogPainter{Painter: &geminiPainter{models: client.Models, model: name, host: host}, info: info}, nil
	}
	return &geminiPainter{models: client.Models, model: name, host: host}, nil
}

// catalogPainter is a painter whose model the catalog knows: generate_image
// takes its references, shapes, sizes and count (PIC-81).
type catalogPainter struct {
	tools.Painter
	info images.Model
}

func (c *catalogPainter) Info() images.Model { return c.info }

// imagenPainter draws with Imagen for generate_image: the prompt alone, 1
// to 4 pictures (PIC-81).
type imagenPainter struct{ pictures *geminiPictures }

func (p *imagenPainter) Model() string      { return p.pictures.Model() }
func (p *imagenPainter) Host() string       { return p.pictures.Host() }
func (p *imagenPainter) Info() images.Model { return p.pictures.Info() }

func (p *imagenPainter) Paint(ctx context.Context, req tools.PaintRequest) (*tools.Picture, error) {
	ans, err := p.pictures.imagen(ctx, PictureCall{Prompt: req.Prompt, Count: req.Count, AspectRatio: req.AspectRatio, Size: req.Size})
	if err != nil {
		return nil, err
	}
	pic := ans.Pictures[0]
	pic.More = ans.Pictures[1:]
	return &pic, nil
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

// newOrgPainter is the organization's image model name, drawing through
// its service's Responses API with an image_generation tool (the
// enterprise gateway's GW-04b), signed with the person's session.
func newOrgPainter(ctx context.Context, cfg *config.Config, name string) (tools.Painter, error) {
	p, m, err := enterpriseProvider(ctx, cfg, name)
	if err != nil {
		return nil, err
	}
	switch {
	case m.ID != name:
		return nil, fmt.Errorf("your organization doesn't offer %s: choose one of its image models", name)
	case p.format != formatResponses:
		return nil, fmt.Errorf("%s can't draw pictures: choose one of your organization's Gemini image models", name)
	}
	u, err := url.Parse(p.cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	return &orgPainter{p: p, model: m.ID, url: p.cfg.BaseURL + "/responses", host: u.Hostname(), http: policyFrom(cfg.LLM).httpClient()}, nil
}

// orgPainter draws with one of the organization's image models.
type orgPainter struct {
	p     *apiProvider
	model string
	url   string
	host  string
	http  *http.Client
}

func (o *orgPainter) Model() string { return "enterprise/" + o.model }
func (o *orgPainter) Host() string  { return o.host }

func (o *orgPainter) Paint(ctx context.Context, req tools.PaintRequest) (*tools.Picture, error) {
	content := []map[string]string{{"type": "input_text", "text": req.Prompt}}
	for _, ref := range req.References {
		content = append(content, map[string]string{"type": "input_image", "image_url": "data:" + ref.MIME + ";base64," + base64.StdEncoding.EncodeToString(ref.Data)})
	}
	tool := map[string]string{"type": "image_generation"}
	if req.AspectRatio != "" {
		tool["aspect_ratio"] = req.AspectRatio
	}
	if req.Size != "" {
		tool["image_size"] = req.Size
	}
	body, err := json.Marshal(map[string]any{
		"model": o.model, "stream": false, "store": false, "tools": []any{tool},
		"input": []any{map[string]any{"type": "message", "role": "user", "content": content}},
	})
	if err != nil {
		return nil, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, o.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	res, err := o.p.middleware(hr, o.http.Do)
	if err != nil {
		return nil, apiError(err, nil, true)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, apiError(err, nil, true)
	}
	if res.StatusCode/100 != 2 {
		return nil, apiError(fmt.Errorf("HTTP %d", res.StatusCode), &errorReply{status: res.StatusCode, body: string(data)}, true)
	}
	return orgPicture(data)
}

// orgPicture is the picture in a Responses API answer: its first image
// generation call, the text of its messages, and its usage.
func orgPicture(data []byte) (*tools.Picture, error) {
	var r struct {
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
		Output []struct {
			Type    string `json:"type"`
			Result  string `json:"result"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			Input  int32 `json:"input_tokens"`
			Output int32 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, errors.New("your organization's service: its answer couldn't be read")
	}
	pic := &tools.Picture{Usage: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: r.Usage.Input, CandidatesTokenCount: r.Usage.Output, TotalTokenCount: r.Usage.Input + r.Usage.Output}}
	if r.Status == "failed" {
		msg := "the model's answer failed"
		if r.Error != nil && r.Error.Message != "" {
			msg = r.Error.Message
		}
		return pic, errors.New("your organization's service: " + msg)
	}
	for _, item := range r.Output {
		switch item.Type {
		case "image_generation_call":
			if pic.Data != nil {
				continue
			}
			b, err := base64.StdEncoding.DecodeString(item.Result)
			if err != nil || len(b) == 0 {
				continue
			}
			pic.Data, pic.MIME = b, http.DetectContentType(b)
		case "message":
			for _, c := range item.Content {
				pic.Text += c.Text
			}
		}
	}
	if pic.Data == nil {
		why := strings.TrimSpace(pic.Text)
		if why == "" {
			why = "no reason given"
		}
		return pic, errors.New("the model answered without a picture (" + why + ")")
	}
	return pic, nil
}
