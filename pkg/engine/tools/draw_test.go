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

package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// pngHeader is enough of a PNG for content sniffing.
const pngHeader = "\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"

// fakePainter "draws" a PNG naming the prompt, or a picture of mime.
type fakePainter struct {
	mime string
	got  []PaintRequest
	fail error
}

func (f *fakePainter) Model() string { return "gemini/image" }
func (f *fakePainter) Host() string  { return "generativelanguage.googleapis.com" }
func (f *fakePainter) Paint(_ context.Context, req PaintRequest) (*Picture, error) {
	f.got = append(f.got, req)
	usage := &genai.GenerateContentResponseUsageMetadata{CandidatesTokenCount: 1120}
	if f.fail != nil {
		return &Picture{Usage: usage}, f.fail
	}
	mime := f.mime
	if mime == "" {
		mime = "image/png"
	}
	return &Picture{Data: []byte(pngHeader + req.Prompt), MIME: mime, Text: " Here it is. ", Usage: usage}, nil
}

func TestGenerateImage(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		args    map[string]any
		mime    string
		network bool
		fail    error
		noModel error
		wantOut string
		wantErr string
		check   func(t *testing.T, f *fakePainter, out map[string]any)
	}{
		{name: "a picture", args: map[string]any{"prompt": "a red fox at dawn", "path": "images/fox.png", "aspect_ratio": "16:9", "size": "2k"}, network: true, wantOut: "images/fox.png",
			check: func(t *testing.T, f *fakePainter, out map[string]any) {
				assert.Equal(t, PaintRequest{Prompt: "a red fox at dawn", AspectRatio: "16:9", Size: "2K"}, f.got[0])
				assert.Equal(t, "gemini/image", out["model"])
				assert.Equal(t, "Here it is.", out["text"])
				assert.Equal(t, "image/png", out["mime_type"])
			}},
		{name: "from workspace pictures", files: map[string]string{"logo.png": pngHeader + "old"}, args: map[string]any{"prompt": "make it blue", "path": "logo-blue.png", "images": []any{"logo.png"}}, network: true, wantOut: "logo-blue.png",
			check: func(t *testing.T, f *fakePainter, _ map[string]any) {
				require.Len(t, f.got[0].References, 1)
				assert.Equal(t, "image/png", f.got[0].References[0].MIME)
				assert.Equal(t, pngHeader+"old", string(f.got[0].References[0].Data))
			}},
		{name: "the extension follows the picture", args: map[string]any{"prompt": "x", "path": "o.png"}, mime: "image/jpeg", network: true, wantOut: "o.jpg",
			check: func(t *testing.T, _ *fakePainter, out map[string]any) {
				assert.Contains(t, out["note"], "made a jpg picture, so it is o.jpg")
			}},
		{name: ".jpeg is a JPEG's", args: map[string]any{"prompt": "x", "path": "o.jpeg"}, mime: "image/jpeg", network: true, wantOut: "o.jpeg"},
		{name: "the new name is taken", files: map[string]string{"o.jpg": "old"}, args: map[string]any{"prompt": "x", "path": "o.png"}, mime: "image/jpeg", network: true, wantErr: "'o.jpg' already exists"},
		{name: "no prompt", args: map[string]any{"prompt": " ", "path": "o.png"}, network: true, wantErr: "prompt is empty"},
		{name: "a strange shape", args: map[string]any{"prompt": "x", "path": "o.png", "aspect_ratio": "5:1"}, network: true, wantErr: "aspect_ratio is one of"},
		{name: "a strange size", args: map[string]any{"prompt": "x", "path": "o.png", "size": "8K"}, network: true, wantErr: "size is 1K, 2K or 4K"},
		{name: "too many pictures", args: map[string]any{"prompt": "x", "path": "o.png", "images": []any{"a", "b", "c", "d"}}, network: true, wantErr: "at most 3"},
		{name: "a missing picture", args: map[string]any{"prompt": "x", "path": "o.png", "images": []any{"nope.png"}}, network: true, wantErr: "failed to read nope.png"},
		{name: "not a picture", files: map[string]string{"n.txt": "notes"}, args: map[string]any{"prompt": "x", "path": "o.png", "images": []any{"n.txt"}}, network: true, wantErr: "isn't a PNG, JPEG or WebP"},
		{name: "a picture outside", args: map[string]any{"prompt": "x", "path": "o.png", "images": []any{"../a.png"}}, network: true, wantErr: "outside"},
		{name: "network off", args: map[string]any{"prompt": "x", "path": "o.png"}, wantErr: "sandbox.allow_network"},
		{name: "exists", files: map[string]string{"o.png": "old"}, args: map[string]any{"prompt": "x", "path": "o.png"}, network: true, wantErr: "already exists"},
		{name: "overwrite", files: map[string]string{"o.png": "old"}, args: map[string]any{"prompt": "x", "path": "o.png", "overwrite": true}, network: true, wantOut: "o.png"},
		{name: "outside", args: map[string]any{"prompt": "x", "path": "../o.png"}, network: true, wantErr: "outside"},
		{name: "the workspace itself", args: map[string]any{"prompt": "x", "path": "."}, network: true, wantErr: "must name a file"},
		{name: "the model fails", args: map[string]any{"prompt": "x", "path": "o.png"}, network: true, fail: errors.New("quota"), wantErr: "drawing failed: quota"},
		{name: "no image model", args: map[string]any{"prompt": "x", "path": "o.png"}, network: true, noModel: errors.New("no key"), wantErr: "drawing isn't available: no key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ws, dir := newTestWorkspace(t)
			for name, content := range tc.files {
				writeFile(t, filepath.Join(dir, name), content)
			}
			f := &fakePainter{mime: tc.mime, fail: tc.fail}
			var billed []int32
			p := &paint{paintState: paintState{painter: f, billed: func(_ context.Context, _, model string, u *genai.GenerateContentResponseUsageMetadata) {
				assert.Equal(t, "gemini/image", model)
				billed = append(billed, u.CandidatesTokenCount)
			}}}
			if tc.noModel != nil {
				p = &paint{paintState: paintState{err: tc.noModel}}
			}
			out := runTool(t, toolOf(t)(NewGenerateImageTool(ws, allowAll(), p, tc.network, nil)), tc.args)
			if tc.wantErr != "" {
				assert.Contains(t, errOf(out), tc.wantErr)
				if tc.fail != nil {
					assert.Equal(t, []int32{1120}, billed, "a failed call's usage is still billed")
				}
				return
			}
			require.Empty(t, errOf(out))
			assert.Equal(t, tc.wantOut, out["path"])
			data, err := os.ReadFile(filepath.Join(dir, tc.wantOut))
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(string(data), pngHeader))
			assert.Equal(t, []int32{1120}, billed)
			if tc.check != nil {
				tc.check(t, f, out)
			}
		})
	}
}

func TestGenerateImageApproval(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "logo.png"), pngHeader)
	cp := newTestCheckpoints(ws, 0)
	f := &fakePainter{}

	h, reqs := approverHooks(false)
	out := runTool(t, toolOf(t)(NewGenerateImageTool(ws, h, &paint{paintState: paintState{painter: f}}, true, nil)),
		map[string]any{"prompt": strings.Repeat("a fox ", 40), "path": "fox.png", "images": []any{"logo.png"}})
	assert.NotEmpty(t, errOf(out))
	require.Len(t, *reqs, 1)
	req := (*reqs)[0]
	assert.Equal(t, api.ActionNetwork, req.Kind)
	assert.Equal(t, "image:gemini/image", req.Key)
	assert.Equal(t, []string{"generativelanguage.googleapis.com"}, req.Targets)
	assert.True(t, strings.HasPrefix(req.Detail, "Draw with gemini/image → fox.png: a fox"), req.Detail)
	assert.True(t, strings.HasSuffix(req.Detail, "… (from logo.png)"), "a long prompt clipped: %s", req.Detail)
	assert.Empty(t, f.got, "nothing is sent before approval")

	cp.Begin("turn")
	h, _ = approverHooks(true)
	out = runTool(t, toolOf(t)(NewGenerateImageTool(ws, h, &paint{paintState: paintState{painter: f}}, true, nil)), map[string]any{"prompt": "x", "path": "fox.png"})
	require.Empty(t, errOf(out))
	assert.FileExists(t, filepath.Join(dir, "fox.png"))
	_, err := cp.Undo("", false)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "fox.png"), "undone with the turn")
}

// The picture written and the pictures sent are decided by the file rules.
func TestGenerateImageRules(t *testing.T) {
	for _, tc := range []struct {
		name, path, mime, wantErr string
		images                    []any
		mustAsk                   bool
	}{
		{name: "write denied", path: "docs/o.png", wantErr: "denied by the permission rule deny write(docs/**)"},
		{name: "read denied", path: "o.png", images: []any{"secret/a.png"}, wantErr: "reading secret/a.png is denied"},
		{name: "asked", path: "art/o.png", mustAsk: true},
		{name: "neither", path: "o.png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, dir := newTestWorkspace(t)
			writeFile(t, filepath.Join(dir, "secret", "a.png"), pngHeader)
			f := &fakePainter{mime: tc.mime}
			h, reqs := approverHooks(true)
			h.SetRules(rulesOf(t, nil, []string{"write(art/**)"}, []string{"write(docs/**)", "read(secret/**)"}))
			args := map[string]any{"prompt": "x", "path": tc.path}
			if tc.images != nil {
				args["images"] = tc.images
			}
			out := runTool(t, toolOf(t)(NewGenerateImageTool(ws, h, &paint{paintState: paintState{painter: f}}, true, nil)), args)
			if tc.wantErr != "" {
				assert.Contains(t, errOf(out), tc.wantErr)
				assert.Empty(t, *reqs, "refused before anything is asked")
				assert.Empty(t, f.got)
				return
			}
			require.Empty(t, errOf(out))
			require.Len(t, *reqs, 1)
			assert.Equal(t, tc.mustAsk, (*reqs)[0].MustAsk)
		})
	}
	t.Run("the renamed file is denied", func(t *testing.T) {
		ws, _ := newTestWorkspace(t)
		h, _ := approverHooks(true)
		h.SetRules(rulesOf(t, nil, nil, []string{"write(*.jpg)"}))
		out := runTool(t, toolOf(t)(NewGenerateImageTool(ws, h, &paint{paintState: paintState{painter: &fakePainter{mime: "image/jpeg"}}}, true, nil)), map[string]any{"prompt": "x", "path": "o.png"})
		assert.Contains(t, errOf(out), "o.jpg is denied")
	})
}

// generate_image is always registered; agents have it only while there's
// an image model.
func TestGenerateImageRegistered(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	r, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	defer r.Close()
	names := func() []string {
		var out []string
		for _, tl := range r.GetToolsForAgent([]string{"generate_image", "read_file"}) {
			out = append(out, tl.Name())
		}
		return out
	}
	assert.Equal(t, []string{"read_file"}, names())

	r.SetPainter(&fakePainter{}, nil, nil)
	assert.Equal(t, []string{"generate_image", "read_file"}, names())
	assert.NoError(t, r.PainterErr())

	r.SetPainter(nil, errors.New("no key"), nil)
	assert.Equal(t, []string{"read_file"}, names())
	assert.EqualError(t, r.PainterErr(), "no key")

	var bare Registry
	bare.SetPainter(&fakePainter{}, nil, nil)
	assert.NoError(t, bare.PainterErr())
}

// With the attachment store, the result refers to the picture, so the
// model sees what it drew and the desktop app shows it.
func TestGenerateImageRefersToTheStoredFile(t *testing.T) {
	ws, _ := newTestWorkspace(t)
	var loaded string
	load := func(path string) (*images.Image, error) {
		loaded = path
		return &images.Image{SHA256: strings.Repeat("b", 64), MIME: "image/png"}, nil
	}
	out := runTool(t, toolOf(t)(NewGenerateImageTool(ws, allowAll(), &paint{paintState: paintState{painter: &fakePainter{}}}, true, load)), map[string]any{"prompt": "x", "path": "a.png"})
	require.Empty(t, errOf(out))
	assert.Equal(t, "a.png", loaded)
	assert.Equal(t, images.URIScheme+strings.Repeat("b", 64), out["image_uri"])
}

func TestGenerateImageTargets(t *testing.T) {
	got := CallTargets("generate_image", map[string]any{"path": "o.png", "images": []any{"a.png", 3, "b.png"}})
	assert.Equal(t, []string{"a.png", "b.png"}, got[RuleRead])
	assert.Equal(t, []string{"o.png"}, got[RuleWrite])
}
