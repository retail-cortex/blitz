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

package engine

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adkmodel "google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// Reasoning a provider signed is kept as it was written, for the model to
// read back; a secret in it is masked where it's shown (spec_056 CA-29).
func TestEventsMaskReasoning(t *testing.T) {
	redact.Remember("sk-event-test-secret-1")
	part := &genai.Part{Text: "the key is sk-event-test-secret-1", Thought: true, ThoughtSignature: []byte("signed")}
	ev := &adksession.Event{Author: "blitz", LLMResponse: adkmodel.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{part}}}}
	out := events(ev)
	require.Len(t, out, 1)
	require.NotNil(t, out[0].Text)
	assert.True(t, out[0].Text.Thought)
	assert.NotContains(t, out[0].Text.Text, "sk-event-test-secret-1")
	assert.Equal(t, "the key is sk-event-test-secret-1", part.Text, "the stored reasoning is unchanged")
}

// A file the model made is an event of its own; the user's attachments
// are shown with their prompt (spec_056 CA-71).
func TestEventsModelFile(t *testing.T) {
	ref := &genai.Part{FileData: &genai.FileData{FileURI: images.URIScheme + "abc", MIMEType: "image/png", DisplayName: "gemini-3-pro-1a2b3c4d.png"}}
	for _, tc := range []struct {
		role string
		want []api.Event
	}{
		{genai.RoleModel, []api.Event{{Author: "blitz", File: &api.File{URI: images.URIScheme + "abc", Name: "gemini-3-pro-1a2b3c4d.png", MIME: "image/png"}}}},
		{genai.RoleUser, nil},
	} {
		t.Run(tc.role, func(t *testing.T) {
			ev := &adksession.Event{Author: "blitz", LLMResponse: adkmodel.LLMResponse{Content: &genai.Content{Role: tc.role, Parts: []*genai.Part{ref}}}}
			assert.Equal(t, tc.want, events(ev))
		})
	}
}

func TestReadStoredFile(t *testing.T) {
	w := openTest(t)
	img, err := w.tools.Images().PutMade("made.png", "image/png", []byte("png"))
	require.NoError(t, err)
	data, mime, err := w.ReadStoredFile(img.URI())
	require.NoError(t, err)
	assert.Equal(t, []byte("png"), data)
	assert.Equal(t, "image/png", mime)

	_, _, err = w.ReadStoredFile(images.URIScheme + strings.Repeat("0", 64))
	assert.ErrorIs(t, err, fs.ErrNotExist)
	_, _, err = w.ReadStoredFile("not a reference")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}
