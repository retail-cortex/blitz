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
// from or match, its shape and size ("" for the model's own), and how many
// (0 or 1 for one; more only for a model that makes several).
type PaintRequest struct {
	Prompt      string
	References  []Reference
	AspectRatio string // "1:1", "16:9", …
	Size        string // "1K", "2K" or "4K"
	Count       int
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
	// More are the request's other pictures, when it asked for several.
	More []Picture
}

// CatalogPainter is a painter that says what its model can do, from the
// picture catalog (spec_058_pictures PIC-81): generate_image then takes
// the model's references, ratios, sizes and count instead of its own
// fixed ones.
type CatalogPainter interface {
	Painter
	Info() images.Model
}

// KeptPictures is what generate_image drew, for keeping as a Pictures
// session (PIC-81): the call's settings, its references and its pictures
// with where they were written.
type KeptPictures struct {
	Session     string
	Model       string
	Prompt      string
	AspectRatio string
	Size        string
	References  []Reference
	RefPaths    []string
	Pictures    []Picture
	Paths       []string
}

// PictureKeeper keeps what generate_image drew as a Pictures session and
// returns its ID ("" when it couldn't).
type PictureKeeper func(ctx context.Context, k KeptPictures) string

// Limits on generate_image's reference pictures: a painter that doesn't say
// its model's (CatalogPainter) takes maxReferences.
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
	keep    PictureKeeper
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
	r.paint.paintState = paintState{painter: p, err: err, billed: billed, keep: r.paint.keep}
}

// SetPictureKeeper keeps what generate_image draws as Pictures sessions
// (PIC-81), so the person can open them in the panel (PIC-54).
func (r *Registry) SetPictureKeeper(k PictureKeeper) {
	if r.paint == nil {
		return
	}
	r.paint.mu.Lock()
	defer r.paint.mu.Unlock()
	r.paint.keep = k
}

// PainterErr is why the [images] model can't be used; nil when it can, or
// when there's none.
func (r *Registry) PainterErr() error { return r.paint.get().err }

// GenerateImageInput defines arguments for generate_image.
type GenerateImageInput struct {
	Prompt      string   `json:"prompt" jsonschema:"What the picture shows, in detail: subject, style, composition, colours, any text in it"`
	Path        string   `json:"path" jsonschema:"Where to write the picture, e.g. images/poster.png; the extension follows what the model makes"`
	Images      []string `json:"images,omitempty" jsonschema:"Workspace pictures (PNG, JPEG, WebP) to edit or to match in style or subject, up to the image model's limit (14 for Gemini's larger models, 16 for OpenAI's, none for Imagen)"`
	AspectRatio string   `json:"aspect_ratio,omitempty" jsonschema:"The shape: 1:1, 2:3, 3:2, 3:4, 4:3, 4:5, 5:4, 9:16, 16:9, 21:9, or what else the image model takes (panoramic 1:4, 4:1, 1:8, 8:1 on some); the model's default when empty"`
	Size        string   `json:"size,omitempty" jsonschema:"0.5K, 1K (the default), 2K or 4K, as the image model takes; larger costs more"`
	Count       int      `json:"count,omitempty" jsonschema:"How many pictures, 1 to 4, for an image model that makes several at once (Imagen, OpenAI's); 1 when empty. Each after the first is written beside path, numbered (poster-2.png)"`
	Overwrite   bool     `json:"overwrite,omitempty" jsonschema:"Replace the file if it already exists"`
}

// GenerateImageOutput describes the picture written.
type GenerateImageOutput struct {
	Path string `json:"path"`
	// Paths are every picture written, when there were several.
	Paths []string `json:"paths,omitempty"`
	// PictureSession is the Pictures session they're kept in (PIC-81), for
	// Open in Pictures (PIC-54).
	PictureSession string `json:"picture_session,omitempty"`
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
			count := max(input.Count, 1)
			info, catalog := catalogOf(painter)
			switch {
			case prompt == "":
				return fail("prompt is empty: describe the picture")
			case !allowNetwork:
				return fail("network access is disabled (sandbox.allow_network = false)")
			case catalog:
				// The model's own limits (PIC-81); the references' types and
				// sizes are checked as they're read.
				if len(input.Images) > info.MaxReferences {
					if info.MaxReferences == 0 {
						return fail(fmt.Sprintf("%s doesn't start from pictures: leave images empty", painter.Model()))
					}
					return fail(fmt.Sprintf("%s takes at most %d pictures to start from", painter.Model(), info.MaxReferences))
				}
				if problems := info.Check(images.Request{Prompt: prompt, Count: count, AspectRatio: input.AspectRatio, Size: size}); len(problems) > 0 {
					return fail(strings.Join(problems, "; "))
				}
			case input.AspectRatio != "" && !aspectRatios[input.AspectRatio]:
				return fail("aspect_ratio is one of 1:1, 2:3, 3:2, 3:4, 4:3, 9:16, 16:9, 21:9")
			case size != "" && size != "1K" && size != "2K" && size != "4K":
				return fail("size is 1K, 2K or 4K")
			case len(input.Images) > maxReferences:
				return fail(fmt.Sprintf("at most %d pictures to start from", maxReferences))
			case count > 1:
				return fail(fmt.Sprintf("%s makes one picture at a time: leave count empty", painter.Model()))
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
			if count > 1 {
				detail = fmt.Sprintf("Draw %d pictures with %s → %s: %s", count, painter.Model(), rel, clip(prompt, 160))
			}
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

			pic, err := painter.Paint(ctx, PaintRequest{Prompt: prompt, References: refs, AspectRatio: input.AspectRatio, Size: size, Count: count})
			if billed != nil && pic != nil {
				billed(context.WithoutCancel(ctx), sessionOf(ctx), painter.Model(), pic.Usage)
			}
			if err != nil {
				return fail(fmt.Sprintf("drawing failed: %v", err))
			}

			// Each picture is written beside path, numbered after the first;
			// the extension follows what the model made.
			pics := append([]Picture{*pic}, pic.More...)
			var outs []string
			var notes []string
			for i, p := range pics {
				out := rel
				if i > 0 {
					out = fmt.Sprintf("%s-%d%s", strings.TrimSuffix(rel, path.Ext(rel)), i+1, path.Ext(rel))
				}
				if ext := pictureExt(p.MIME); ext != "" && !sameExt(path.Ext(out), ext) {
					out = strings.TrimSuffix(out, path.Ext(out)) + ext
					if i == 0 {
						notes = append(notes, fmt.Sprintf("%s made a %s picture, so it is %s.", painter.Model(), strings.TrimPrefix(ext, "."), out))
					}
				}
				if out != rel {
					if _, err := ws.Stat(out); err == nil && !input.Overwrite {
						return fail(fmt.Sprintf("file '%s' already exists; set overwrite=true to replace it", out))
					}
					if effect, rule := hooks.Rules().Decide(RuleWrite, []string{out}); effect == EffectDeny {
						return fail(fmt.Sprintf("%s is denied by the permission rule deny %s", out, rule))
					}
				}
				if err := writePicture(ctx, ws, out, p.Data, input.Overwrite); err != nil {
					return fail(err.Error())
				}
				outs = append(outs, out)
			}
			done := GenerateImageOutput{Path: outs[0], MIME: pic.MIME, Bytes: len(pic.Data), Model: painter.Model(), Text: strings.TrimSpace(pic.Text), Note: strings.Join(notes, " "), Success: true}
			if len(outs) > 1 {
				done.Paths = outs
			}
			if load != nil {
				if m, err := load(outs[0]); err == nil {
					done.ImageURI, done.MIME = m.URI(), m.MIME
				}
			}
			if st.keep != nil {
				done.PictureSession = st.keep(context.WithoutCancel(ctx), KeptPictures{Session: sessionOf(ctx), Model: painter.Model(), Prompt: prompt,
					AspectRatio: input.AspectRatio, Size: size, References: refs, RefPaths: read, Pictures: pics, Paths: outs})
			}
			return done, nil
		},
	)
}

// catalogOf is what painter's model can do, when it says (PIC-81).
func catalogOf(painter Painter) (images.Model, bool) {
	if c, ok := painter.(CatalogPainter); ok {
		return c.Info(), true
	}
	return images.Model{}, false
}

// writePicture writes data to rel, locked, replacing a file only with
// overwrite.
func writePicture(ctx context.Context, ws *Workspace, rel string, data []byte, overwrite bool) error {
	unlock, err := ws.lockPaths(ctx, rel)
	if err != nil {
		return err
	}
	defer unlock()
	if overwrite {
		err = ws.WriteFileAtomic(ctx, rel, data)
	} else {
		err = ws.CreateExclusive(ctx, rel, data)
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("file '%s' already exists; set overwrite=true to replace it", rel)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to write file: %v", err)
	}
	return nil
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
