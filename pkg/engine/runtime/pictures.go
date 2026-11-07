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
	"github.com/retail-cortex/blitz/pkg/images"
	"google.golang.org/genai"
)

// PictureCall is one Pictures turn (spec_058_pictures §8): for a Gemini
// model, the conversation so far and the user's turn (its text and
// references, labelled by role); for Imagen and an organization's model,
// the prompt and references alone; and the settings, checked against the
// catalog before.
type PictureCall struct {
	History    []*genai.Content
	User       *genai.Content
	Prompt     string
	References []tools.Reference

	Count        int
	AspectRatio  string
	Size         string
	Thinking     string
	Search       bool
	SearchImages bool
	Avoid        string
	People       string
	Seed         *int32
	Enhance      bool
	Format       string
	Quality      int
}

// PictureAnswer is what a model made: its pictures, what it said and
// thought, the pages it grounded on, its turn as it answered (with thought
// signatures, for the conversation), and its usage.
type PictureAnswer struct {
	Pictures []tools.Picture
	Text     string
	Thoughts string
	Sources  []PictureSource
	// Searches counts the grounding searches the model made.
	Searches int
	Content  *genai.Content
	Usage    *genai.GenerateContentResponseUsageMetadata
}

// PictureSource is a page a grounded turn used.
type PictureSource struct {
	Title string
	URL   string
}

// PictureMaker makes pictures for Pictures (spec_058).
type PictureMaker interface {
	// Model names the model, "provider/model"; Info is what it can do.
	Model() string
	Info() images.Model
	// Host is the API's host.
	Host() string
	// Make makes what call asks for. A turn without a picture is an error
	// saying why, with what came (its text, its usage) in the answer.
	Make(ctx context.Context, call PictureCall) (*PictureAnswer, error)
}

// imagenModels is the SDK's Imagen call, for tests.
type imagenModels interface {
	GenerateImages(ctx context.Context, model, prompt string, config *genai.GenerateImagesConfig) (*genai.GenerateImagesResponse, error)
}

// NewPictureMaker is the picture maker for ref ("provider/model"): a
// Gemini image model or Imagen with the Gemini provider's credentials, or
// one of the organization's image models (one turn at a time, its ratio
// and size: GW-04b).
func NewPictureMaker(ctx context.Context, cfg *config.Config, ref string) (PictureMaker, error) {
	provider, name := ParseModelRef(strings.TrimSpace(ref), cfg.LLM.Provider)
	info, ok := images.Lookup(name)
	switch provider {
	case "enterprise":
		p, err := newOrgPainter(ctx, cfg, name)
		if err != nil {
			return nil, err
		}
		if !ok || info.Kind != images.KindGemini {
			// An alias (or a kind the gateway doesn't serve): a Gemini image model's most careful values.
			info, _ = images.Lookup("image")
			info.ID, info.Name = name, name
		}
		info.Thinking, info.ThinkingAlways, info.Search, info.Formats, info.People, info.MaxPictures = nil, false, "", nil, false, 1
		return &orgPictures{painter: p, info: info}, nil
	case "gemini":
		if !ok {
			return nil, fmt.Errorf("%s isn't an image model", ref)
		}
	default:
		return nil, fmt.Errorf("%s can't make pictures: choose a Gemini or Imagen model, or one of your organization's", ref)
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
	return &geminiPictures{gen: client.Models, img: client.Models, name: name, info: info, host: host}, nil
}

// geminiPictures makes pictures with the Gemini API: generateContent for a
// Gemini image model, generateImages for Imagen.
type geminiPictures struct {
	gen  geminiModels
	img  imagenModels
	name string
	info images.Model
	host string
}

func (g *geminiPictures) Model() string      { return "gemini/" + g.name }
func (g *geminiPictures) Info() images.Model { return g.info }
func (g *geminiPictures) Host() string       { return g.host }

func (g *geminiPictures) Make(ctx context.Context, call PictureCall) (*PictureAnswer, error) {
	if g.info.Kind == images.KindImagen {
		return g.imagen(ctx, call)
	}
	gc := &genai.GenerateContentConfig{ResponseModalities: []string{string(genai.ModalityText), string(genai.ModalityImage)}}
	ic := &genai.ImageConfig{AspectRatio: call.AspectRatio, ImageSize: call.Size, PersonGeneration: call.People}
	if call.Format != "" && call.Format != "image/png" {
		ic.OutputMIMEType = call.Format
		if call.Quality > 0 {
			q := int32(call.Quality)
			ic.OutputCompressionQuality = &q
		}
	}
	if *ic != (genai.ImageConfig{}) {
		gc.ImageConfig = ic
	}
	if call.Thinking != "" {
		gc.ThinkingConfig = &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevel(call.Thinking), IncludeThoughts: true}
	} else if g.info.ThinkingAlways {
		gc.ThinkingConfig = &genai.ThinkingConfig{IncludeThoughts: true}
	}
	if call.Search {
		gs := &genai.GoogleSearch{}
		if call.SearchImages {
			gs.SearchTypes = &genai.SearchTypes{WebSearch: &genai.WebSearch{}, ImageSearch: &genai.ImageSearch{}}
		}
		gc.Tools = []*genai.Tool{{GoogleSearch: gs}}
	}
	contents := append(append([]*genai.Content{}, call.History...), call.User)
	resp, err := g.gen.GenerateContent(ctx, g.name, contents, gc)
	if err != nil {
		return nil, err
	}
	out := &PictureAnswer{Usage: resp.UsageMetadata}
	for _, c := range resp.Candidates {
		if c == nil || c.Content == nil {
			continue
		}
		if out.Content == nil {
			out.Content = c.Content
		}
		for _, part := range c.Content.Parts {
			switch {
			case part == nil:
			case part.Thought && part.Text != "":
				out.Thoughts += part.Text
			case part.Thought:
			case part.InlineData != nil && strings.HasPrefix(part.InlineData.MIMEType, "image/") && len(part.InlineData.Data) > 0:
				out.Pictures = append(out.Pictures, tools.Picture{Data: part.InlineData.Data, MIME: part.InlineData.MIMEType})
			case part.Text != "":
				out.Text += part.Text
			}
		}
		if gm := c.GroundingMetadata; gm != nil {
			out.Searches += len(gm.WebSearchQueries) + len(gm.ImageSearchQueries)
			for _, ch := range gm.GroundingChunks {
				if ch != nil && ch.Web != nil && ch.Web.URI != "" {
					out.Sources = append(out.Sources, PictureSource{Title: ch.Web.Title, URL: ch.Web.URI})
				}
			}
		}
	}
	if len(out.Pictures) == 0 {
		return out, errors.New("the model didn't draw it (" + whyNoPicture(resp, out.Text) + ")")
	}
	return out, nil
}

// whyNoPicture is why an answer came without a picture.
func whyNoPicture(resp *genai.GenerateContentResponse, text string) string {
	switch {
	case strings.TrimSpace(text) != "":
		return strings.TrimSpace(text)
	case len(resp.Candidates) > 0 && resp.Candidates[0] != nil && resp.Candidates[0].FinishReason != "":
		return "it stopped: " + string(resp.Candidates[0].FinishReason)
	case resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "":
		return "the prompt was blocked: " + string(resp.PromptFeedback.BlockReason)
	}
	return "no reason given"
}

// imagen makes Imagen's pictures: the prompt alone, 1 to 4.
func (g *geminiPictures) imagen(ctx context.Context, call PictureCall) (*PictureAnswer, error) {
	ic := &genai.GenerateImagesConfig{
		NumberOfImages: int32(max(call.Count, 1)), AspectRatio: call.AspectRatio, ImageSize: call.Size,
		NegativePrompt: call.Avoid, PersonGeneration: genai.PersonGeneration(call.People), OutputMIMEType: call.Format,
		Seed: call.Seed, EnhancePrompt: call.Enhance,
	}
	if call.Format == "image/jpeg" && call.Quality > 0 {
		q := int32(call.Quality)
		ic.OutputCompressionQuality = &q
	}
	resp, err := g.img.GenerateImages(ctx, g.name, call.Prompt, ic)
	if err != nil {
		return nil, err
	}
	out := &PictureAnswer{}
	var filtered []string
	for _, im := range resp.GeneratedImages {
		switch {
		case im == nil:
		case im.Image != nil && len(im.Image.ImageBytes) > 0:
			mime := im.Image.MIMEType
			if mime == "" {
				mime = "image/png"
			}
			out.Pictures = append(out.Pictures, tools.Picture{Data: im.Image.ImageBytes, MIME: mime})
		case im.RAIFilteredReason != "":
			filtered = append(filtered, im.RAIFilteredReason)
		}
	}
	if len(out.Pictures) == 0 {
		why := "no reason given"
		if len(filtered) > 0 {
			why = strings.Join(filtered, "; ")
		}
		return out, errors.New("the model didn't draw it (" + why + ")")
	}
	return out, nil
}

// orgPictures makes pictures with one of the organization's image models,
// through its gateway: one turn, one picture, its ratio and size (GW-04b).
type orgPictures struct {
	painter tools.Painter
	info    images.Model
}

func (o *orgPictures) Model() string      { return o.painter.Model() }
func (o *orgPictures) Info() images.Model { return o.info }
func (o *orgPictures) Host() string       { return o.painter.Host() }

func (o *orgPictures) Make(ctx context.Context, call PictureCall) (*PictureAnswer, error) {
	pic, err := o.painter.Paint(ctx, tools.PaintRequest{Prompt: call.Prompt, References: call.References, AspectRatio: call.AspectRatio, Size: call.Size})
	if pic == nil {
		return nil, err
	}
	out := &PictureAnswer{Text: pic.Text, Usage: pic.Usage}
	if err == nil {
		out.Pictures = []tools.Picture{{Data: pic.Data, MIME: pic.MIME}}
	}
	return out, err
}
