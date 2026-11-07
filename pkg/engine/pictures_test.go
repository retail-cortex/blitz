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
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// fakePictures is a picture maker that answers with the next of answers,
// recording each call.
type fakePictures struct {
	ref     string
	info    images.Model
	calls   []runtime.PictureCall
	answers []*runtime.PictureAnswer
	err     error
}

func (f *fakePictures) Model() string      { return f.ref }
func (f *fakePictures) Info() images.Model { return f.info }
func (f *fakePictures) Host() string       { return "h" }

func (f *fakePictures) Make(_ context.Context, call runtime.PictureCall) (*runtime.PictureAnswer, error) {
	f.calls = append(f.calls, call)
	a := f.answers[0]
	if len(f.answers) > 1 {
		f.answers = f.answers[1:]
	}
	return a, f.err
}

// withPictures has the workspace make pictures with f.
func withPictures(t *testing.T, f *fakePictures) {
	t.Helper()
	newPictureMaker = func(_ context.Context, _ *config.Config, ref string) (runtime.PictureMaker, error) {
		if info, ok := images.Lookup(ref); ok && ref == f.ref {
			f.info = info
			return f, nil
		}
		return nil, errors.New("not offered")
	}
	t.Cleanup(func() { newPictureMaker = runtime.NewPictureMaker })
}

func drew(text string, data ...string) *runtime.PictureAnswer {
	a := &runtime.PictureAnswer{Text: text, Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: text}}},
		Usage: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 1120}}
	for _, d := range data {
		a.Pictures = append(a.Pictures, tools.Picture{Data: []byte(d), MIME: "image/png"})
		a.Content.Parts = append(a.Content.Parts, &genai.Part{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte(d)}})
	}
	return a
}

// A session of turns: kept with its pictures, the conversation sent back
// with each Gemini turn, references labelled by role (spec_058 §6, PIC-60).
func TestMakePicture(t *testing.T) {
	w := openTest(t)
	f := &fakePictures{ref: "gemini/gemini-3.1-flash-image", answers: []*runtime.PictureAnswer{drew("A city.", "PNG-1"), drew("Darker.", "PNG-2")}}
	withPictures(t, f)
	ctx := context.Background()
	writeTree(t, w, map[string]string{"refs/logo.png": "\x89PNG\r\n\x1a\nlogo"})

	models := w.PictureModels(ctx)
	require.Len(t, models, 1, "only what's offered")
	assert.True(t, models[0].Default)
	assert.Positive(t, models[0].OutputPerMTok)

	_, _, err := w.MakePicture(ctx, PictureRequest{Model: f.ref, Prompt: "a city", Count: 2}, false)
	assert.ErrorIs(t, err, ErrPictureRequest)
	assert.ErrorContains(t, err, "at most 1 picture")

	s, cost, err := w.MakePicture(ctx, PictureRequest{Model: f.ref, Prompt: "a city at dusk, glass walkways", AspectRatio: "16:9", Size: "2K", Search: true, SearchImages: true,
		References: []PictureRef{{Path: "refs/logo.png", Role: images.RoleStyle}, {Data: []byte("RIFF....WEBPVP8 "), MIME: "image/webp", Name: "me.webp", Role: images.RoleCharacter}}}, false)
	require.NoError(t, err)
	assert.Positive(t, cost.CostUSD)
	assert.Equal(t, "a city at dusk, glass walkways", s.Name)
	require.Len(t, s.Turns, 1)
	t1 := s.Turns[0]
	assert.Equal(t, "image", t1.Search)
	require.Len(t, t1.References, 2)
	assert.Equal(t, "image/png", t1.References[0].MIME, "sniffed")
	assert.Equal(t, "refs/logo.png", t1.References[0].Path)
	require.Len(t, t1.Pictures, 1)
	data, mime, err := w.ReadPicture(s.ID, t1.Pictures[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "PNG-1", string(data))
	assert.Equal(t, "image/png", mime)
	ref, _, err := w.ReadPicture(s.ID, t1.References[1].File)
	require.NoError(t, err)
	assert.Equal(t, "RIFF....WEBPVP8 ", string(ref))
	assert.Nil(t, t1.Answer.Parts[1].InlineData.Data, "pictures are kept as files, not in the session")
	call := f.calls[0]
	assert.Empty(t, call.History)
	assert.Equal(t, "Style reference 1 (take its look):", call.User.Parts[1].Text)
	assert.Equal(t, "Character reference 1 (keep this person or creature the same):", call.User.Parts[3].Text)

	// The next turn refines the picture: the conversation goes back, pictures and all.
	s, _, err = w.MakePicture(ctx, PictureRequest{Session: s.ID, Model: f.ref, Prompt: "make the sky darker", StartFrom: t1.Pictures[0].ID}, false)
	require.NoError(t, err)
	require.Len(t, s.Turns, 2)
	call = f.calls[1]
	require.Len(t, call.History, 2)
	assert.Equal(t, "PNG-1", string(call.History[1].Parts[1].InlineData.Data))
	assert.Equal(t, "PNG-1", string(call.References[0].Data), "the picture to change, first")
	assert.Equal(t, t1.Pictures[0].ID, s.Turns[1].StartFrom)

	// An answer without a picture is kept, saying why.
	f.answers = []*runtime.PictureAnswer{{Text: "I can't draw that."}}
	f.err = errors.New("the model didn't draw it (I can't draw that.)")
	s, _, err = w.MakePicture(ctx, PictureRequest{Session: s.ID, Model: f.ref, Prompt: "something"}, false)
	require.NoError(t, err)
	assert.Contains(t, s.Turns[2].Problem, "can't draw that")
	f.answers, f.err = []*runtime.PictureAnswer{nil}, errors.New("quota")
	_, _, err = w.MakePicture(ctx, PictureRequest{Session: s.ID, Model: f.ref, Prompt: "x"}, false)
	assert.ErrorContains(t, err, "quota", "nothing came: an error, nothing kept")
	f.err = nil

	// An estimate asks nothing and keeps nothing.
	n := len(f.calls)
	_, est, err := w.MakePicture(ctx, PictureRequest{Session: s.ID, Model: f.ref, Prompt: "again", Size: "4K"}, true)
	require.NoError(t, err)
	assert.Len(t, f.calls, n)
	assert.True(t, est.Priced)
	assert.Positive(t, est.CostUSD)
	again, err := w.PictureSession(s.ID)
	require.NoError(t, err)
	assert.Len(t, again.Turns, 3)

	list, err := w.PictureSessions()
	require.NoError(t, err)
	require.Len(t, list, 1)
	renamed, err := w.RenamePictureSession(s.ID, " Skyline ")
	require.NoError(t, err)
	assert.Equal(t, "Skyline", renamed.Name)

	_, _, err = w.MakePicture(ctx, PictureRequest{Model: "gemini/gemini-3-pro-image", Prompt: "x"}, false)
	assert.ErrorContains(t, err, "not offered")
	_, _, err = w.MakePicture(ctx, PictureRequest{Session: "nope", Model: f.ref, Prompt: "x"}, false)
	assert.ErrorIs(t, err, ErrNoPictureSession)
	_, _, err = w.MakePicture(ctx, PictureRequest{Model: f.ref, Prompt: "x", References: []PictureRef{{Path: "../outside.png"}}}, false)
	assert.Error(t, err)
	_, _, err = w.MakePicture(ctx, PictureRequest{Session: s.ID, Model: f.ref, Prompt: "x", References: []PictureRef{{Picture: "p-000000000000"}}}, false)
	assert.ErrorIs(t, err, ErrNoPicture)
}

// Saving into the workspace: named and numbered, never overwriting; a
// picture deleted keeps its place in the conversation (PIC-51, PIC-53).
func TestSavePicture(t *testing.T) {
	w := openTest(t)
	f := &fakePictures{ref: "gemini/gemini-nano-banana-2.1", answers: []*runtime.PictureAnswer{drew("", "PNG-1"), drew("", "PNG-2")}}
	withPictures(t, f)
	ctx := context.Background()
	s, _, err := w.MakePicture(ctx, PictureRequest{Model: f.ref, Prompt: "A City Skyline, at dusk!"}, false)
	require.NoError(t, err)
	pic := s.Turns[0].Pictures[0].ID

	p1, err := w.SavePicture(ctx, s.ID, pic, "")
	require.NoError(t, err)
	assert.Equal(t, "pictures/a-city-skyline-at-dusk-1.png", p1)
	p2, err := w.SavePicture(ctx, s.ID, pic, "")
	require.NoError(t, err)
	assert.Equal(t, "pictures/a-city-skyline-at-dusk-2.png", p2, "numbered, never overwriting")
	got, err := os.ReadFile(filepath.Join(w.Dir(), filepath.FromSlash(p1)))
	require.NoError(t, err)
	assert.Equal(t, "PNG-1", string(got))
	p3, err := w.SavePicture(ctx, s.ID, pic, "art/hero")
	require.NoError(t, err)
	assert.Equal(t, "art/hero.png", p3, "its extension added")
	_, err = w.SavePicture(ctx, s.ID, pic, "art/hero.png")
	assert.ErrorIs(t, err, fs.ErrExist)
	p4, err := w.SavePicture(ctx, s.ID, pic, "art/")
	require.NoError(t, err)
	assert.Equal(t, "art/a-city-skyline-at-dusk-1.png", p4)
	again, err := w.PictureSession(s.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{p1, p2, p3, p4}, again.Turns[0].Pictures[0].Saved)

	s, _, err = w.MakePicture(ctx, PictureRequest{Session: s.ID, Model: f.ref, Prompt: "more"}, false)
	require.NoError(t, err)
	s, err = w.DeletePicture(s.ID, pic)
	require.NoError(t, err)
	assert.True(t, s.Turns[0].Pictures[0].Deleted)
	_, _, err = w.ReadPicture(s.ID, pic)
	assert.ErrorIs(t, err, ErrNoPicture)
	_, err = w.SavePicture(ctx, s.ID, pic, "")
	assert.ErrorIs(t, err, ErrNoPicture)
	_, err = w.DeletePicture(s.ID, pic)
	assert.ErrorIs(t, err, ErrNoPicture)
	_, _, err = w.MakePicture(ctx, PictureRequest{Session: s.ID, Model: f.ref, Prompt: "next"}, false)
	require.NoError(t, err)
	hist := f.calls[len(f.calls)-1].History
	require.Len(t, hist, 4)
	assert.Len(t, hist[1].Parts, 1, "the deleted picture is left out")
	assert.Len(t, hist[3].Parts, 2, "the next turn's stays")

	require.NoError(t, w.DeletePictureSession(s.ID))
	assert.ErrorIs(t, w.DeletePictureSession(s.ID), ErrNoPictureSession)
	_, err = w.PictureSession("../x")
	assert.ErrorIs(t, err, ErrNoPictureSession)
	list, err := w.PictureSessions()
	require.NoError(t, err)
	assert.Empty(t, list)
}

// Imagen turns stand alone, priced by the picture (PIC-42, PIC-70).
func TestMakePictureImagen(t *testing.T) {
	w := openTest(t)
	f := &fakePictures{ref: "gemini/imagen-3.0-generate-002", answers: []*runtime.PictureAnswer{{Pictures: []tools.Picture{{Data: []byte("J1"), MIME: "image/jpeg"}, {Data: []byte("J2"), MIME: "image/jpeg"}}}}}
	withPictures(t, f)
	ctx := context.Background()
	_, est, err := w.MakePicture(ctx, PictureRequest{Model: f.ref, Prompt: "a fox", Count: 2}, true)
	require.NoError(t, err)
	assert.InDelta(t, 0.06, est.CostUSD, 1e-9)
	s, _, err := w.MakePicture(ctx, PictureRequest{Model: f.ref, Prompt: "a fox", Count: 2, Avoid: "blur", UseSeed: true, Seed: 7}, false)
	require.NoError(t, err)
	assert.InDelta(t, 0.06, s.Turns[0].CostUSD, 1e-9)
	assert.Equal(t, int32(7), *f.calls[0].Seed)
	assert.Nil(t, f.calls[0].User, "no conversation")
	assert.Equal(t, ".jpg", filepath.Ext(s.Turns[0].Pictures[1].File))
	p, err := w.SavePicture(ctx, s.ID, s.Turns[0].Pictures[1].ID, "")
	require.NoError(t, err)
	assert.Equal(t, "pictures/a-fox-1.jpg", p)
}

// The edges: the [images] model first, a broken session skipped, the
// history trimmed oldest first, names and types.
func TestPictureEdges(t *testing.T) {
	w := openTest(t)
	f := &fakePictures{ref: "gemini/gemini-nano-banana-2.1", answers: []*runtime.PictureAnswer{drew("", "PNG-1"), drew("", "PNG-2"), drew("", "PNG-3")}}
	withPictures(t, f)
	ctx := context.Background()
	w.cfg.Images.Model = "gemini/gemini-nano-banana-2.1"
	models := w.PictureModels(ctx)
	require.Len(t, models, 1)
	assert.True(t, models[0].Default)
	w.cfg.Images.Model = "openai/dall-e"
	assert.Len(t, w.PictureModels(ctx), 1, "one that can't be made isn't offered")

	_, est, err := w.MakePicture(ctx, PictureRequest{Model: f.ref, Prompt: "x", Search: true}, true)
	require.NoError(t, err)
	assert.Greater(t, est.CostUSD, 0.014, "the search's price on top")

	// Each turn's pictures go back; past the budget, the oldest turn first.
	s, _, err := w.MakePicture(ctx, PictureRequest{Model: f.ref, Prompt: "one", References: []PictureRef{{Data: []byte("\x89PNG\r\n\x1a\nref"), MIME: "image/png; q=1", Role: images.RoleStyle}}}, false)
	require.NoError(t, err)
	assert.Equal(t, "image/png", s.Turns[0].References[0].MIME, "its parameters dropped")
	s, _, err = w.MakePicture(ctx, PictureRequest{Session: s.ID, Model: f.ref, Prompt: "two"}, false)
	require.NoError(t, err)
	pictureHistoryBytes = 8
	t.Cleanup(func() { pictureHistoryBytes = 40 << 20 })
	_, _, err = w.MakePicture(ctx, PictureRequest{Session: s.ID, Model: f.ref, Prompt: "three"}, false)
	require.NoError(t, err)
	assert.Len(t, f.calls[len(f.calls)-1].History, 2, "only the last turn fits")

	_, _, err = w.MakePicture(ctx, PictureRequest{Model: f.ref, Prompt: "x", References: []PictureRef{{Path: "missing.png"}}}, false)
	assert.ErrorIs(t, err, fs.ErrNotExist)

	// A broken session isn't listed, and says why when asked for.
	broken := filepath.Join(w.picturesDir(), "pic-ffffffffffff")
	require.NoError(t, os.MkdirAll(broken, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(broken, "session.json"), []byte("{"), 0o600))
	list, err := w.PictureSessions()
	require.NoError(t, err)
	assert.Len(t, list, 1)
	_, err = w.PictureSession("pic-ffffffffffff")
	assert.ErrorContains(t, err, "picture session pic-ffffffffffff")
	_, err = w.RenamePictureSession("pic-000000000000", "x")
	assert.ErrorIs(t, err, ErrNoPictureSession)
	_, err = w.DeletePicture("pic-000000000000", "p")
	assert.ErrorIs(t, err, ErrNoPictureSession)
	_, err = w.SavePicture(ctx, "pic-000000000000", "p", "")
	assert.ErrorIs(t, err, ErrNoPictureSession)
	assert.ErrorIs(t, w.savePictureSession(PictureSession{ID: "../x"}), ErrNoPictureSession)

	assert.Equal(t, ".gif", pictureExt("image/gif"))
	assert.Equal(t, ".webp", pictureExt("image/webp"))
	assert.Equal(t, "one two three four five six seven eight …", pictureName("one two three four five six seven eight nine ten"))
	assert.Equal(t, "Pictures", pictureName("  "))
	assert.Equal(t, "picture", pictureSlug("!!!"))
	assert.Equal(t, "art", func() string { w.cfg.Images.Folder = "/art/"; return w.PicturesFolder() }())
	assert.Nil(t, withoutPictureData(nil))
}

// A model that draws a draft while it thinks (Nano Banana 2.1): the draft
// isn't one of the pictures, and the next turn sends the final picture
// back in its place, never an empty one (the owner's session, 2026-11-07).
func TestPictureHistoryWithThoughtDrafts(t *testing.T) {
	w := openTest(t)
	answer := &runtime.PictureAnswer{
		Pictures: []tools.Picture{{Data: []byte("FINAL"), MIME: "image/jpeg"}},
		Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
			{Text: "Defining the scene", Thought: true},
			{InlineData: &genai.Blob{MIMEType: "image/jpeg", Data: []byte("DRAFT")}, Thought: true},
			{Text: "Confirming", Thought: true},
			{InlineData: &genai.Blob{MIMEType: "image/jpeg", Data: []byte("FINAL")}, ThoughtSignature: []byte("sig")},
		}},
	}
	f := &fakePictures{ref: "gemini/gemini-nano-banana-2.1", answers: []*runtime.PictureAnswer{answer, drew("", "NEXT")}}
	withPictures(t, f)
	ctx := context.Background()
	s, _, err := w.MakePicture(ctx, PictureRequest{Model: f.ref, Prompt: "a wine glass at sunset"}, false)
	require.NoError(t, err)
	require.Len(t, s.Turns[0].Answer.Parts, 3, "the draft isn't kept")
	_, _, err = w.MakePicture(ctx, PictureRequest{Session: s.ID, Model: f.ref, Prompt: "fuller glass", StartFrom: s.Turns[0].Pictures[0].ID}, false)
	require.NoError(t, err)
	back := f.calls[1].History[1]
	var images [][]byte
	for _, p := range back.Parts {
		if p.InlineData != nil {
			images = append(images, p.InlineData.Data)
		}
	}
	assert.Equal(t, [][]byte{[]byte("FINAL")}, images, "the final picture, where the model put it")
	assert.Equal(t, []byte("sig"), back.Parts[2].ThoughtSignature, "its signature with it")

	// A session kept before this fix (the draft's place in it) goes back the same.
	old := s
	old.Turns[0].Answer.Parts = append([]*genai.Part{{InlineData: &genai.Blob{MIMEType: "image/jpeg"}, Thought: true}}, old.Turns[0].Answer.Parts...)
	require.NoError(t, w.savePictureSession(old))
	hist := w.pictureHistory(old)
	n := 0
	for _, p := range hist[1].Parts {
		if p.InlineData != nil {
			n++
			assert.Equal(t, "FINAL", string(p.InlineData.Data))
		}
	}
	assert.Equal(t, 1, n)
}
