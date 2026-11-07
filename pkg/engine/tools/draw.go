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
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/images"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"
)

// Painter draws pictures with an image model (runtime.NewPainter).
type Painter interface {
	// Model names the image model, "provider/model".
	Model() string
	// Host is the API's host, for web permission rules.
	Host() string
	// Paint returns the picture req describes.
	Paint(ctx context.Context, req PaintRequest) (*Picture, error)
}

// PaintRequest is a picture to draw: what it shows, the pictures to start
// from or match, and its shape and size ("" for the model's own).
type PaintRequest struct {
	Prompt      string
	References  []Reference
	AspectRatio string // "1:1", "16:9", …
	Size        string // "1K", "2K" or "4K"
}

// Reference is a picture a PaintRequest starts from.
type Reference struct {
	MIME string
	Data []byte
}

// Picture is a picture drawn.
type Picture struct {
	Data []byte
	MIME string
	// Text is what the model said with it (or instead of it).
	Text string
	// Usage is what the model reported, for /cost (nil: none).
	Usage *genai.GenerateContentResponseUsageMetadata
}

// Limits on generate_image's reference pictures.
const (
	maxReferences     = 3
	maxReferenceBytes = 7 << 20
)

// referenceTypes are the pictures an image model starts from.
var referenceTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true}

// paint is generate_image's state, as speech is generate_audio's.
type paint struct {
	mu sync.Mutex
	paintState
}

// paintState is paint's state at one moment.
type paintState struct {
	painter Painter
	err     error
	billed  SpeechBilling
}

func (p *paint) get() paintState {
	if p == nil {
		return paintState{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paintState
}

// SetPainter gives generate_image its image model (nil: none), or why it
// can't be used (err), with what hears of each call's usage. Agents have
// generate_image only while there's a painter.
func (r *Registry) SetPainter(p Painter, err error, billed SpeechBilling) {
	if r.paint == nil {
		return
	}
	r.paint.mu.Lock()
	defer r.paint.mu.Unlock()
	r.paint.paintState = paintState{painter: p, err: err, billed: billed}
}

// PainterErr is why the [images] model can't be used; nil when it can, or
// when there's none.
func (r *Registry) PainterErr() error { return r.paint.get().err }

// GenerateImageInput defines arguments for generate_image.
type GenerateImageInput struct {
	Prompt      string   `json:"prompt" jsonschema:"What the picture shows, in detail: subject, style, composition, colours, any text in it"`
	Path        string   `json:"path" jsonschema:"Where to write the picture, e.g. images/poster.png; the extension follows what the model makes"`
	Images      []string `json:"images,omitempty" jsonschema:"Up to 3 workspace pictures (PNG, JPEG, WebP) to edit or to match in style or subject"`
	AspectRatio string   `json:"aspect_ratio,omitempty" jsonschema:"1:1, 2:3, 3:2, 3:4, 4:3, 9:16, 16:9 or 21:9; the model's default when empty"`
	Size        string   `json:"size,omitempty" jsonschema:"1K (the default), 2K or 4K; larger costs more"`
	Overwrite   bool     `json:"overwrite,omitempty" jsonschema:"Replace the file if it already exists"`
}

// GenerateImageOutput describes the picture written.
type GenerateImageOutput struct {
	Path string `json:"path"`
	// ImageURI is the picture's reference in the attachment store: the
	// model sees it after the result, and the desktop app shows it.
	ImageURI string `json:"image_uri,omitempty"`
	MIME     string `json:"mime_type,omitempty"`
	Bytes    int    `json:"bytes,omitempty"`
	Model    string `json:"model,omitempty"`
	// Text is what the image model said with the picture.
	Text    string `json:"text,omitempty"`
	Note    string `json:"note,omitempty"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

var aspectRatios = map[string]bool{"1:1": true, "2:3": true, "3:2": true, "3:4": true, "4:3": true, "9:16": true, "16:9": true, "21:9": true}

// NewGenerateImageTool draws a picture with the [images] image model and
// writes it into the workspace, optionally starting from workspace
// pictures (spec_006_filetools FS-80). One approval covers the call (it
// sends the prompt and those pictures to the provider) and the file it
// writes, which a checkpoint can undo. With load (the attachment store's,
// nil without one) the result refers to the stored picture, so the model
// sees what it made.
func NewGenerateImageTool(ws *Workspace, hooks *Hooks, p *paint, allowNetwork bool, load func(path string) (*images.Image, error)) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name: "generate_image",
			Description: "Draw a picture with an image model and save it in the workspace: an illustration, a poster, a logo, a diagram-like drawing, a photo-like scene. " +
				"Give images to edit a picture or match its style. Describe the picture fully in prompt; you see the result, so you can try again with a changed prompt.",
		},
		func(ctx agent.Context, input GenerateImageInput) (GenerateImageOutput, error) {
			fail := func(msg string) (GenerateImageOutput, error) {
				return GenerateImageOutput{Path: input.Path, Error: msg}, nil
			}
			st := p.get()
			painter, billed := st.painter, st.billed
			if painter == nil {
				why := "no image model is set ([images] model)"
				if st.err != nil {
					why = st.err.Error()
				}
				return fail("drawing isn't available: " + why)
			}
			prompt := strings.TrimSpace(input.Prompt)
			size := strings.ToUpper(strings.TrimSpace(input.Size))
			switch {
			case prompt == "":
				return fail("prompt is empty: describe the picture")
			case input.AspectRatio != "" && !aspectRatios[input.AspectRatio]:
				return fail("aspect_ratio is one of 1:1, 2:3, 3:2, 3:4, 4:3, 9:16, 16:9, 21:9")
			case size != "" && size != "1K" && size != "2K" && size != "4K":
				return fail("size is 1K, 2K or 4K")
			case len(input.Images) > maxReferences:
				return fail(fmt.Sprintf("at most %d pictures to start from", maxReferences))
			case !allowNetwork:
				return fail("network access is disabled (sandbox.allow_network = false)")
			}

			var refs []Reference
			var read []string
			for _, in := range input.Images {
				rel, err := ws.Rel(in)
				if err != nil {
					return fail(err.Error())
				}
				data, err := ws.ReadFileLimit(rel, maxReferenceBytes+1)
				if err != nil {
					return fail(fmt.Sprintf("failed to read %s: %v", in, err))
				}
				if len(data) > maxReferenceBytes {
					return fail(fmt.Sprintf("%s is over 7 MB", in))
				}
				mime := http.DetectContentType(data)
				if !referenceTypes[mime] {
					return fail(fmt.Sprintf("%s isn't a PNG, JPEG or WebP picture", in))
				}
				refs = append(refs, Reference{MIME: mime, Data: data})
				read = append(read, rel)
			}

			rel, err := ws.WritablePath(input.Path)
			if err != nil {
				return fail(err.Error())
			}
			if rel == "." {
				return fail("path must name a file")
			}
			if _, err := ws.Stat(rel); err == nil && !input.Overwrite {
				return fail(fmt.Sprintf("file '%s' already exists; set overwrite=true to replace it", input.Path))
			}

			// The file is a write, the pictures sent reads: the rules decide
			// them as for the file tools, deny refusing and ask asking.
			if effect, rule := hooks.Rules().Decide(RuleRead, read); effect == EffectDeny && len(read) > 0 {
				return fail(fmt.Sprintf("reading %s is denied by the permission rule deny %s", strings.Join(read, ", "), rule))
			}
			effect, rule := hooks.Rules().Decide(RuleWrite, []string{rel})
			if effect == EffectDeny {
				return fail(fmt.Sprintf("%s is denied by the permission rule deny %s", rel, rule))
			}
			detail := fmt.Sprintf("Draw with %s → %s: %s", painter.Model(), rel, clip(prompt, 160))
			if len(read) > 0 {
				detail += " (from " + strings.Join(read, ", ") + ")"
			}
			if err := hooks.Approve(ctx, api.ApprovalRequest{
				Tool: "generate_image", Kind: api.ActionNetwork, Detail: detail,
				Key: "image:" + painter.Model(), KeyLabel: "pictures with " + painter.Model(), Targets: []string{painter.Host()},
				MustAsk: effect == EffectAsk,
			}); err != nil {
				return fail(err.Error())
			}

			pic, err := painter.Paint(ctx, PaintRequest{Prompt: prompt, References: refs, AspectRatio: input.AspectRatio, Size: size})
			if billed != nil && pic != nil {
				billed(context.WithoutCancel(ctx), sessionOf(ctx), painter.Model(), pic.Usage)
			}
			if err != nil {
				return fail(fmt.Sprintf("drawing failed: %v", err))
			}

			// The extension follows what the model made.
			out, note := rel, ""
			if ext := pictureExt(pic.MIME); ext != "" && !sameExt(path.Ext(out), ext) {
				out = strings.TrimSuffix(out, path.Ext(out)) + ext
				note = fmt.Sprintf("%s made a %s picture, so it is %s.", painter.Model(), strings.TrimPrefix(ext, "."), out)
				if _, err := ws.Stat(out); err == nil && !input.Overwrite {
					return fail(fmt.Sprintf("file '%s' already exists; set overwrite=true to replace it", out))
				}
				if effect, rule := hooks.Rules().Decide(RuleWrite, []string{out}); effect == EffectDeny {
					return fail(fmt.Sprintf("%s is denied by the permission rule deny %s", out, rule))
				}
			}
			unlock, err := ws.lockPaths(ctx, out)
			if err != nil {
				return fail(err.Error())
			}
			defer unlock()
			if input.Overwrite {
				err = ws.WriteFileAtomic(ctx, out, pic.Data)
			} else {
				err = ws.CreateExclusive(ctx, out, pic.Data)
				if errors.Is(err, fs.ErrExist) {
					return fail(fmt.Sprintf("file '%s' already exists; set overwrite=true to replace it", out))
				}
			}
			if err != nil {
				return fail(fmt.Sprintf("failed to write file: %v", err))
			}
			done := GenerateImageOutput{Path: out, MIME: pic.MIME, Bytes: len(pic.Data), Model: painter.Model(), Text: strings.TrimSpace(pic.Text), Note: note, Success: true}
			if load != nil {
				if m, err := load(out); err == nil {
					done.ImageURI, done.MIME = m.URI(), m.MIME
				}
			}
			return done, nil
		},
	)
}

// pictureExt is the extension for a picture of type mime ("" for one
// Blitz doesn't name).
func pictureExt(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ""
}

// sameExt reports whether ext is want, taking .jpeg for .jpg.
func sameExt(ext, want string) bool {
	ext = strings.ToLower(ext)
	return ext == want || (ext == ".jpeg" && want == ".jpg")
}

// clip is s cut to n runes, with an ellipsis when cut.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
