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
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// fakeImagen answers generateImages with resp, recording what it was sent.
type fakeImagen struct {
	model, prompt string
	cfg           *genai.GenerateImagesConfig
	resp          *genai.GenerateImagesResponse
	err           error
}

func (f *fakeImagen) GenerateImages(_ context.Context, model, prompt string, cfg *genai.GenerateImagesConfig) (*genai.GenerateImagesResponse, error) {
	f.model, f.prompt, f.cfg = model, prompt, cfg
	return f.resp, f.err
}

// A Gemini turn: the conversation, the settings as the API takes them,
// and what came back (spec_058_pictures PIC-60).
func TestGeminiPictures(t *testing.T) {
	info, _ := images.Lookup("gemini-nano-banana-2.1")
	resp := answer(
		&genai.Part{Text: "Planning the skyline.", Thought: true},
		&genai.Part{Thought: true, ThoughtSignature: []byte("sig")},
		&genai.Part{Text: "A city at dusk."},
		&genai.Part{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte("PNG1")}},
	)
	resp.Candidates[0].GroundingMetadata = &genai.GroundingMetadata{GroundingChunks: []*genai.GroundingChunk{{Web: &genai.GroundingChunkWeb{Title: "Skylines", URI: "https://example.com/s"}}, {}}}
	f := &fakeImageModel{resp: resp}
	g := &geminiPictures{gen: f, name: info.ID, info: info, host: "h"}
	assert.Equal(t, "gemini/gemini-nano-banana-2.1", g.Model())
	assert.Equal(t, info, g.Info())
	assert.Equal(t, "h", g.Host())

	history := []*genai.Content{genai.NewContentFromText("a city", genai.RoleUser), genai.NewContentFromText("ok", genai.RoleModel)}
	user := genai.NewContentFromText("darker sky", genai.RoleUser)
	ans, err := g.Make(context.Background(), PictureCall{History: history, User: user, AspectRatio: "16:9", Size: "2K", Thinking: "HIGH", Search: true, SearchImages: true, Format: "image/jpeg", Quality: 80, People: "ALLOW_ADULT"})
	require.NoError(t, err)
	require.Len(t, f.contents, 3)
	assert.Same(t, user, f.contents[2])
	assert.Equal(t, []string{"TEXT", "IMAGE"}, f.cfg.ResponseModalities)
	q := int32(80)
	assert.Equal(t, &genai.ImageConfig{AspectRatio: "16:9", ImageSize: "2K", PersonGeneration: "ALLOW_ADULT", OutputMIMEType: "image/jpeg", OutputCompressionQuality: &q}, f.cfg.ImageConfig)
	assert.Equal(t, genai.ThinkingLevelHigh, f.cfg.ThinkingConfig.ThinkingLevel)
	assert.True(t, f.cfg.ThinkingConfig.IncludeThoughts)
	require.Len(t, f.cfg.Tools, 1)
	assert.NotNil(t, f.cfg.Tools[0].GoogleSearch.SearchTypes.ImageSearch)
	assert.Equal(t, "Planning the skyline.", ans.Thoughts)
	assert.Equal(t, "A city at dusk.", ans.Text)
	require.Len(t, ans.Pictures, 1)
	assert.Equal(t, []byte("PNG1"), ans.Pictures[0].Data)
	assert.Equal(t, []PictureSource{{Title: "Skylines", URL: "https://example.com/s"}}, ans.Sources)
	assert.Same(t, resp.Candidates[0].Content, ans.Content, "the turn as it answered, signatures and all")

	// Nothing set: no image config, no tools; web search alone.
	_, err = g.Make(context.Background(), PictureCall{User: user, Search: true})
	require.NoError(t, err)
	assert.Nil(t, f.cfg.ImageConfig)
	assert.Nil(t, f.cfg.ThinkingConfig)
	assert.Nil(t, f.cfg.Tools[0].GoogleSearch.SearchTypes)

	// A model that always thinks shares its thoughts unasked.
	pro, _ := images.Lookup("gemini-3-pro-image")
	g.info = pro
	_, err = g.Make(context.Background(), PictureCall{User: user})
	require.NoError(t, err)
	assert.True(t, f.cfg.ThinkingConfig.IncludeThoughts)

	// No picture: why, with what came.
	for _, tc := range []struct {
		name string
		resp *genai.GenerateContentResponse
		want string
	}{
		{"its words", answer(&genai.Part{Text: "I can't draw that."}), "I can't draw that."},
		{"a stop", &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{FinishReason: "IMAGE_SAFETY"}}}, "it stopped: IMAGE_SAFETY"},
		{"blocked", &genai.GenerateContentResponse{PromptFeedback: &genai.GenerateContentResponsePromptFeedback{BlockReason: "OTHER"}}, "the prompt was blocked: OTHER"},
		{"nothing", &genai.GenerateContentResponse{}, "no reason given"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.resp = tc.resp
			ans, err := g.Make(context.Background(), PictureCall{User: user})
			assert.ErrorContains(t, err, tc.want)
			assert.NotNil(t, ans)
		})
	}
	f.err = errors.New("quota")
	_, err = g.Make(context.Background(), PictureCall{User: user})
	assert.ErrorContains(t, err, "quota")
}

// An Imagen turn: the prompt alone, its settings, 1 to 4 pictures (PIC-61).
func TestImagenPictures(t *testing.T) {
	info, _ := images.Lookup("imagen-4.0-generate-preview")
	f := &fakeImagen{resp: &genai.GenerateImagesResponse{GeneratedImages: []*genai.GeneratedImage{
		{Image: &genai.Image{ImageBytes: []byte("J1"), MIMEType: "image/jpeg"}},
		{Image: &genai.Image{ImageBytes: []byte("P2")}},
		{RAIFilteredReason: "filtered"},
		nil,
	}}}
	g := &geminiPictures{img: f, name: info.ID, info: info}
	seed := int32(7)
	ans, err := g.Make(context.Background(), PictureCall{Prompt: "a fox", Count: 3, AspectRatio: "4:3", Size: "2K", Avoid: "blur", People: "DONT_ALLOW", Seed: &seed, Enhance: true, Format: "image/jpeg", Quality: 70})
	require.NoError(t, err)
	assert.Equal(t, "a fox", f.prompt)
	q := int32(70)
	assert.Equal(t, &genai.GenerateImagesConfig{NumberOfImages: 3, AspectRatio: "4:3", ImageSize: "2K", NegativePrompt: "blur", PersonGeneration: "DONT_ALLOW", OutputMIMEType: "image/jpeg", Seed: &seed, EnhancePrompt: true, OutputCompressionQuality: &q}, f.cfg)
	require.Len(t, ans.Pictures, 2)
	assert.Equal(t, "image/jpeg", ans.Pictures[0].MIME)
	assert.Equal(t, "image/png", ans.Pictures[1].MIME, "PNG when it doesn't say")

	f.resp = &genai.GenerateImagesResponse{GeneratedImages: []*genai.GeneratedImage{{RAIFilteredReason: "people"}}}
	_, err = g.Make(context.Background(), PictureCall{Prompt: "x"})
	assert.ErrorContains(t, err, "(people)")
	f.resp = &genai.GenerateImagesResponse{}
	_, err = g.Make(context.Background(), PictureCall{Prompt: "x"})
	assert.ErrorContains(t, err, "no reason given")
	f.err = errors.New("quota")
	_, err = g.Make(context.Background(), PictureCall{Prompt: "x"})
	assert.ErrorContains(t, err, "quota")
}

func TestNewPictureMaker(t *testing.T) {
	cfg := &config.Config{}
	_, err := NewPictureMaker(context.Background(), cfg, "gemini/gemini-4-argon")
	assert.ErrorContains(t, err, "isn't an image model")
	_, err = NewPictureMaker(context.Background(), cfg, "anthropic/claude-opus-5-5")
	assert.ErrorContains(t, err, "can't make pictures")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	_, err = NewPictureMaker(context.Background(), cfg, "gemini/gemini-3.1-flash-image")
	assert.ErrorContains(t, err, "needs an API key")
	cfg.LLM.Gemini.APIKey = "k"
	m, err := NewPictureMaker(context.Background(), cfg, "gemini/gemini-3.1-flash-image")
	require.NoError(t, err)
	assert.Equal(t, "gemini/gemini-3.1-flash-image", m.Model())
	assert.Equal(t, "generativelanguage.googleapis.com", m.Host())
}

// An organization's image model: one picture, its ratio and size; the
// controls its gateway doesn't pass are gone from what it can do (PIC-63).
func TestOrgPictures(t *testing.T) {
	s := newOrgService(t, [2]any{200, gwPicture})
	cfg := signedIn(t, s)
	m, err := NewPictureMaker(context.Background(), cfg, "enterprise/fast")
	require.NoError(t, err)
	assert.Equal(t, "enterprise/fast", m.Model())
	assert.Empty(t, m.Info().Thinking)
	assert.Empty(t, m.Info().Search)
	ans, err := m.Make(context.Background(), PictureCall{Prompt: "a fox", AspectRatio: "16:9"})
	require.NoError(t, err)
	require.Len(t, ans.Pictures, 1)
	assert.Equal(t, "A fox.", ans.Text)

	s2 := newOrgService(t, [2]any{200, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"No."}]}]}`})
	m2, err := NewPictureMaker(context.Background(), signedIn(t, s2), "enterprise/fast")
	require.NoError(t, err)
	ans, err = m2.Make(context.Background(), PictureCall{Prompt: "x"})
	assert.Error(t, err)
	require.NotNil(t, ans)
	assert.Empty(t, ans.Pictures)
}
