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

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// fakeImageModel answers with resp, recording what it was sent.
type fakeImageModel struct {
	model    string
	contents []*genai.Content
	cfg      *genai.GenerateContentConfig
	resp     *genai.GenerateContentResponse
	err      error
}

func (f *fakeImageModel) GenerateContent(_ context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
	f.model, f.contents, f.cfg = model, contents, cfg
	return f.resp, f.err
}

func answer(parts ...*genai.Part) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{
		Candidates:    []*genai.Candidate{{Content: &genai.Content{Parts: parts}}},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 1120},
	}
}

func TestGeminiPainter(t *testing.T) {
	f := &fakeImageModel{resp: answer(
		&genai.Part{Text: "thinking", Thought: true},
		&genai.Part{Text: "A fox."},
		&genai.Part{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte("PNG1")}},
		&genai.Part{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte("PNG2")}},
	)}
	p := &geminiPainter{models: f, model: "gemini-3.1-flash-image", host: "h"}
	assert.Equal(t, "gemini/gemini-3.1-flash-image", p.Model())
	assert.Equal(t, "h", p.Host())

	pic, err := p.Paint(context.Background(), tools.PaintRequest{Prompt: "a fox", References: []tools.Reference{{MIME: "image/jpeg", Data: []byte("JPG")}}, AspectRatio: "16:9", Size: "2K"})
	require.NoError(t, err)
	assert.Equal(t, "PNG1", string(pic.Data), "the first picture")
	assert.Equal(t, "image/png", pic.MIME)
	assert.Equal(t, "A fox.", pic.Text, "without the thoughts")
	assert.EqualValues(t, 1120, pic.Usage.CandidatesTokenCount)

	assert.Equal(t, "gemini-3.1-flash-image", f.model)
	require.Len(t, f.contents, 1)
	parts := f.contents[0].Parts
	require.Len(t, parts, 2)
	assert.Equal(t, "a fox", parts[0].Text)
	assert.Equal(t, "JPG", string(parts[1].InlineData.Data))
	assert.Equal(t, []string{"TEXT", "IMAGE"}, f.cfg.ResponseModalities)
	assert.Equal(t, &genai.ImageConfig{AspectRatio: "16:9", ImageSize: "2K"}, f.cfg.ImageConfig)

	_, _ = p.Paint(context.Background(), tools.PaintRequest{Prompt: "x"})
	assert.Nil(t, f.cfg.ImageConfig, "the model's own shape and size")
}

func TestGeminiPainterFails(t *testing.T) {
	for _, tc := range []struct {
		name string
		resp *genai.GenerateContentResponse
		err  error
		want string
	}{
		{"an error", nil, errors.New("quota"), "quota"},
		{"text only", answer(&genai.Part{Text: "I can't draw that."}), nil, "without a picture (I can't draw that.)"},
		{"stopped", &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{FinishReason: genai.FinishReasonSafety}}}, nil, "it stopped: SAFETY"},
		{"blocked", &genai.GenerateContentResponse{PromptFeedback: &genai.GenerateContentResponsePromptFeedback{BlockReason: genai.BlockedReasonProhibitedContent}}, nil, "the prompt was blocked: PROHIBITED_CONTENT"},
		{"nothing", &genai.GenerateContentResponse{Candidates: []*genai.Candidate{nil}}, nil, "no reason given"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &geminiPainter{models: &fakeImageModel{resp: tc.resp, err: tc.err}, model: "m"}
			_, err := p.Paint(context.Background(), tools.PaintRequest{Prompt: "x"})
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestNewPainter(t *testing.T) {
	tests := []struct {
		name    string
		cfg     func(*config.Config)
		model   string
		wantErr string
	}{
		{"none", func(c *config.Config) {}, "", ""},
		{"gemini", func(c *config.Config) {
			c.Images.Model = "gemini/gemini-3.1-flash-image"
			c.LLM.Gemini.APIKey = "k"
		}, "gemini/gemini-3.1-flash-image", ""},
		{"a provider that can't draw", func(c *config.Config) { c.Images.Model = "openai/gpt-image-1" }, "", "can't draw pictures"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			tc.cfg(cfg)
			p, err := NewPainter(context.Background(), cfg)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			if tc.model == "" {
				assert.Nil(t, p)
				return
			}
			assert.Equal(t, tc.model, p.Model())
			assert.Equal(t, "generativelanguage.googleapis.com", p.Host())
		})
	}
}

func TestRecordImage(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{})
	f.eng.RecordImage(context.Background(), "s", "gemini/gemini-3.1-flash-image",
		&genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1_000_000, CandidatesTokenCount: 1_000_000})
	u := f.eng.Usage("s")
	assert.True(t, u.Priced)
	assert.InDelta(t, 60.50, u.CostUSD, 0.001, "priced by [pricing]")
	assert.Zero(t, u.LastPrompt, "a picture isn't the conversation's prompt")
	c := f.eng.usage.RecordMedia(context.Background(), "t", api.PurposeImage, "gemini-3.1-flash-image", nil)
	assert.Equal(t, api.PurposeImage, c.Purpose, "for the session's ledger")
}
