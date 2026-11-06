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
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime/wirefake"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// The conformance suite's attachments and model-made files
// (spec_056_conversation_adapters CA-60–CA-64, CA-70–CA-73).

func mediaConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := conformanceConfig(t)
	cfg.Images.Dir = t.TempDir()
	return cfg
}

// pngOf is a small picture.
func pngOf(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}

// wireText is everything a recorded request says, as JSON.
func wireText(req map[string]any) string {
	b, _ := json.Marshal(req)
	return string(b)
}

// eachSwitch runs check for every ordered pair of APIs.
func eachSwitch(t *testing.T, check func(t *testing.T, from, to string)) {
	names := []string{"gemini", "anthropic", "openai"}
	for _, from := range names {
		for _, to := range names {
			if from != to {
				t.Run(from+" to "+to, func(t *testing.T) { check(t, from, to) })
			}
		}
	}
}

// An attachment of each kind goes on to the next model as it takes it:
// a picture to all three, a PDF as a document or its text, a text file as
// text, audio to Gemini and as a note to the others (CA-61, CA-63).
func TestConformanceAttachmentsAcrossSwitches(t *testing.T) {
	eachSwitch(t, func(t *testing.T, from, to string) {
		cfg := mediaConfig(t)
		ws := wires(t, cfg)
		first, next := ws[from], ws[to]
		eng := conformanceEngine(t, cfg, modelFor(t, cfg, first))
		store := eng.media.storeOf()
		require.NotNil(t, store)
		pic, err := store.PutMade("chart.png", "image/png", pngOf(t))
		require.NoError(t, err)
		notes, err := store.PutMade("notes.txt", "text/plain", []byte("the plan"))
		require.NoError(t, err)
		clip, err := store.PutMade("clip.mp3", "audio/mpeg", []byte("ID3fake audio"))
		require.NoError(t, err)
		attach := WithAttachments(images.Part(pic), images.Part(notes), images.Part(clip))

		first.fake.Script(wirefake.Reply{Text: "A chart, notes and a clip."})
		require.NoError(t, eng.Execute(context.Background(), "s", "What did I send?", func(*session.Event) error { return nil }, attach))
		require.Empty(t, first.fake.Refusals())

		require.NoError(t, eng.SetModel(context.Background(), modelFor(t, cfg, next)))
		next.fake.Script(wirefake.Reply{Text: "Still the same."})
		_, err = collect(t, eng, "s", "And now?")
		require.NoError(t, err)
		assert.Empty(t, next.fake.Refusals())
		sent := wireText(next.fake.Requests()[0])
		assert.Contains(t, sent, "the plan", "the text file's content")
		if to == "gemini" {
			assert.Contains(t, sent, "audio/mpeg", "Gemini hears the clip")
		} else {
			assert.Contains(t, sent, "clip.mp3: this model can't take audio", "the others are told")
		}
		assert.NotContains(t, sent, imageMarkerPrefix, "no marker is left")

		got := unseenAttachments(eng.sessionContents(t, "s"), store, acceptedFor(modelFor(t, cfg, next), false), false)
		if to == "gemini" {
			assert.Empty(t, got)
		} else {
			assert.Equal(t, []string{"clip.mp3"}, got)
		}
	})
}

// A screenshot a tool took goes on to the next model with its result, as
// each API takes a tool result's picture (CA-62).
func TestConformanceToolScreenshotAcrossSwitches(t *testing.T) {
	eachSwitch(t, func(t *testing.T, from, to string) {
		cfg := mediaConfig(t)
		require.NoError(t, os.WriteFile(filepath.Join(cfg.Tools.WorkspaceDir, "shot.png"), pngOf(t), 0o644))
		ws := wires(t, cfg)
		first, next := ws[from], ws[to]
		eng := conformanceEngine(t, cfg, modelFor(t, cfg, first))

		first.fake.Script(
			wirefake.Reply{Thought: "Look at it.", Calls: []wirefake.Call{{Name: "view_image", Args: map[string]any{"path": "shot.png"}}}},
			wirefake.Reply{Text: "A red dot."},
		)
		_, err := collect(t, eng, "s", "What's in shot.png?")
		require.NoError(t, err, "%v", first.fake.Refusals())
		require.Zero(t, first.fake.Remaining())

		require.NoError(t, eng.SetModel(context.Background(), modelFor(t, cfg, next)))
		next.fake.Script(wirefake.Reply{Text: "Still a red dot."})
		_, err = collect(t, eng, "s", "Look again.")
		require.NoError(t, err)
		assert.Empty(t, next.fake.Refusals())
		sent := wireText(next.fake.Requests()[0])
		switch to {
		case "gemini":
			assert.Contains(t, sent, "inlineData")
		case "anthropic":
			assert.Contains(t, sent, `"type":"image"`)
		case "openai":
			assert.Contains(t, sent, "input_image")
		}
	})
}

// A picture a model made is kept in the store, never in the conversation
// as bytes, and the next model reads it as the user's: Claude and OpenAI
// take files only from the user (CA-70, CA-72).
func TestConformanceModelMadeImage(t *testing.T) {
	for _, to := range []string{"gemini", "anthropic", "openai"} {
		t.Run("gemini to "+to, func(t *testing.T) {
			cfg := mediaConfig(t)
			ws := wires(t, cfg)
			g, next := ws["gemini"], ws[to]
			eng := conformanceEngine(t, cfg, modelFor(t, cfg, g))

			g.fake.Script(wirefake.Reply{Text: "Here's a dot.", Files: []wirefake.File{{MIME: "image/png", Data: pngOf(t)}}})
			evs, err := collect(t, eng, "s", "Draw a dot.")
			require.NoError(t, err, "%v", g.fake.Refusals())
			var ref *genai.FileData
			for _, ev := range evs {
				if ev.Content == nil {
					continue
				}
				for _, p := range ev.Content.Parts {
					assert.Nil(t, p.InlineData, "a model's picture is kept as bytes in the conversation")
					if p.FileData != nil {
						ref = p.FileData
					}
				}
			}
			require.NotNil(t, ref, "the picture is referred to")
			assert.True(t, strings.HasPrefix(ref.FileURI, images.URIScheme))
			assert.True(t, strings.HasPrefix(ref.DisplayName, "gemini-3-pro-"), "named for the model that made it: %s", ref.DisplayName)

			if to != "gemini" {
				require.NoError(t, eng.SetModel(context.Background(), modelFor(t, cfg, next)))
			}
			next.fake.Script(wirefake.Reply{Text: "A red dot."})
			_, err = collect(t, eng, "s", "What did you draw?")
			require.NoError(t, err)
			assert.Empty(t, next.fake.Refusals())
			sent := wireText(next.fake.Requests()[len(next.fake.Requests())-1])
			if to != "gemini" {
				assert.Contains(t, sent, "which a model made earlier in this conversation")
			}
		})
	}
}

// sessionContents is the session's conversation, as a model would read it.
func (e *Engine) sessionContents(t *testing.T, sessionID string) []*genai.Content {
	t.Helper()
	resp, err := e.sessions.Get(context.Background(), &session.GetRequest{AppName: appName, UserID: "user", SessionID: sessionID})
	require.NoError(t, err)
	var out []*genai.Content
	for ev := range resp.Session.Events().All() {
		if ev.Content != nil {
			out = append(out, ev.Content)
		}
	}
	return out
}
