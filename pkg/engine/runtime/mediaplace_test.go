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
	"testing"

	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Files go where each API takes them (CA-62, CA-72).
func TestPlaceMedia(t *testing.T) {
	made := &genai.Part{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte("png"), DisplayName: "gemini-3-pro-1a2b3c4d.png"}}
	history := func() []*genai.Content {
		return []*genai.Content{
			genai.NewContentFromText("draw", genai.RoleUser),
			{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "here"}, made}},
			genai.NewContentFromText("what is it?", genai.RoleUser),
		}
	}
	for _, o := range []Origin{fromClaude, fromGPT} {
		t.Run(o.API, func(t *testing.T) {
			got := placeMedia(o, history())
			require.Len(t, got, 3)
			assert.Equal(t, []*genai.Part{{Text: "here"}}, got[1].Parts, "the file left the model's turn")
			parts := got[2].Parts
			require.Len(t, parts, 3)
			assert.Equal(t, "what is it?", parts[0].Text)
			assert.Equal(t, "[gemini-3-pro-1a2b3c4d.png, which a model made earlier in this conversation:]", parts[1].Text)
			assert.Equal(t, []byte("png"), parts[2].InlineData.Data)
			assert.Empty(t, parts[2].InlineData.DisplayName)
		})
	}
	t.Run("gemini", func(t *testing.T) {
		got := placeMedia(fromGemini, history())
		require.Len(t, got[1].Parts, 2, "Gemini keeps the file in its turn")
		assert.Empty(t, got[1].Parts[1].InlineData.DisplayName, "without the name Gemini refuses")
		assert.Equal(t, "gemini-3-pro-1a2b3c4d.png", made.InlineData.DisplayName, "the request's part, not the session's, changes")
	})
	t.Run("the model's turn last", func(t *testing.T) {
		got := placeMedia(fromClaude, history()[:2])
		require.Len(t, got, 3)
		assert.Equal(t, genai.RoleUser, got[2].Role)
	})
	t.Run("a turn of nothing but the file", func(t *testing.T) {
		cs := []*genai.Content{genai.NewContentFromText("draw", genai.RoleUser), {Role: genai.RoleModel, Parts: []*genai.Part{made}}, genai.NewContentFromText("next", genai.RoleUser)}
		got := placeMedia(fromGPT, cs)
		require.Len(t, got, 2)
		assert.Len(t, got[1].Parts, 3)
	})
}

// A file reference the API can't fetch becomes a note, never dropped and
// never an error.
func TestPlaceMediaUnreachableFiles(t *testing.T) {
	cs := []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{
		{FileData: &genai.FileData{FileURI: "https://generativelanguage.googleapis.com/v1beta/files/abc", DisplayName: "talk.mp4"}},
		{FileData: &genai.FileData{FileURI: images.URIScheme + "abc", DisplayName: "chart.png"}},
		{FileData: &genai.FileData{FileURI: "gs://bucket/x.pdf"}},
	}}}
	got := placeMedia(fromClaude, cs)
	require.Len(t, got[0].Parts, 3)
	assert.Equal(t, "[talk.mp4: a file this model can't open]", got[0].Parts[0].Text)
	assert.Equal(t, "[chart.png isn't available to this model]", got[0].Parts[1].Text)
	assert.Equal(t, "[gs://bucket/x.pdf: a file this model can't open]", got[0].Parts[2].Text)
}

// What a model makes is stored and referred to, named for the model, its
// signature kept; what can't be kept becomes a note (CA-70).
func TestKeepMade(t *testing.T) {
	store, err := images.OpenStore(t.TempDir())
	require.NoError(t, err)
	resp := func(mime string) *model.LLMResponse {
		r := &model.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
			{Text: "here"},
			{InlineData: &genai.Blob{MIMEType: mime, Data: []byte("bytes")}, ThoughtSignature: []byte("sig")},
		}}}
		stampResponse(fromGemini, r)
		return r
	}
	r := resp("image/png")
	keepMade(store, r)
	ref := r.Content.Parts[1]
	require.NotNil(t, ref.FileData)
	assert.Regexp(t, `^gemini-3-pro-[0-9a-f]{8}\.png$`, ref.FileData.DisplayName)
	assert.Equal(t, "image/png", ref.FileData.MIMEType)
	assert.NotEmpty(t, ref.ThoughtSignature, "the signature stays with the file")
	data, mime, err := store.Get(ref.FileData.FileURI)
	require.NoError(t, err)
	assert.Equal(t, "image/png", mime)
	assert.Equal(t, []byte("bytes"), data)

	r = resp("application/x-unknown")
	keepMade(store, r)
	assert.Contains(t, r.Content.Parts[1].Text, "couldn't keep")

	r = resp("image/png")
	keepMade(nil, r)
	assert.Contains(t, r.Content.Parts[1].Text, "wasn't kept")

	partial := &model.LLMResponse{Partial: true, Content: &genai.Content{Parts: []*genai.Part{{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte("x")}}}}}
	keepMade(store, partial)
	assert.NotNil(t, partial.Content.Parts[0].InlineData, "only the whole answer's files are kept")
	keepMade(store, nil)
}

func TestAttachmentRef(t *testing.T) {
	for _, tc := range []struct {
		part      *genai.Part
		uri, name string
	}{
		{&genai.Part{FileData: &genai.FileData{FileURI: images.URIScheme + "a", DisplayName: "a.png"}}, images.URIScheme + "a", "a.png"},
		{&genai.Part{FunctionResponse: &genai.FunctionResponse{Response: map[string]any{images.ToolResultKey: images.URIScheme + "b", "path": "dir/shot.png"}}}, images.URIScheme + "b", "shot.png"},
		{&genai.Part{FileData: &genai.FileData{FileURI: "https://x"}}, "", ""},
		{&genai.Part{Text: "hi"}, "", ""},
		{nil, "", ""},
	} {
		uri, name := attachmentRef(tc.part)
		assert.Equal(t, tc.uri, uri)
		assert.Equal(t, tc.name, name)
	}
}
