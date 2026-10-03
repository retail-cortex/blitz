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
	"fmt"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/retail-cortex/blitz/pkg/pdftext"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// Images returns the image store, or nil when images are disabled.
func (r *Registry) Images() *images.Store { return r.images }

// LoadImage reads an image file or a PDF through the workspace sandbox (so
// blocked and out-of-workspace paths are refused), prepares it for the
// model and stores it. The audit log records the path and hash, not the
// content.
func (r *Registry) LoadImage(path string) (*images.Image, error) {
	if r.images == nil {
		return nil, api.ErrImagesDisabled
	}
	rel, err := r.workspace.Rel(path)
	if err != nil {
		return nil, err
	}
	limit := r.imageOpts.MaxInput
	if pdftext.IsPDFPath(rel) {
		limit = images.MaxDocumentBytes
	}
	data, err := r.workspace.ReadFileLimit(rel, limit)
	if err != nil {
		return nil, err
	}
	img, err := r.storeImage(rel, data)
	if err != nil {
		return nil, err
	}
	r.hooks.Audit().Log(audit.Entry{Kind: audit.KindAttachment, Detail: attachmentDetail(rel, img)})
	return img, nil
}

// AddImage prepares and stores image bytes that didn't come from a file
// (the clipboard). name is only for display.
func (r *Registry) AddImage(name string, data []byte) (*images.Image, error) {
	if r.images == nil {
		return nil, api.ErrImagesDisabled
	}
	img, err := r.storeImage(name, data)
	if err != nil {
		return nil, err
	}
	r.hooks.Audit().Log(audit.Entry{Kind: audit.KindAttachment, Detail: attachmentDetail(name, img)})
	return img, nil
}

func (r *Registry) storeImage(name string, data []byte) (*images.Image, error) {
	img, err := images.Prepare(name, data, r.imageOpts)
	if err != nil {
		return nil, err
	}
	if err := r.images.Put(img); err != nil {
		return nil, fmt.Errorf("store image: %w", err)
	}
	return img, nil
}

func attachmentDetail(source string, img *images.Image) string {
	if img.IsDocument() {
		return fmt.Sprintf("%s sha256=%s %s %d pages %d bytes", source, img.SHA256, img.MIME, img.Pages, len(img.Data))
	}
	return fmt.Sprintf("%s sha256=%s %s %d×%d %d bytes", source, img.SHA256, img.MIME, img.Width, img.Height, len(img.Data))
}

// ViewImageInput defines arguments for view_image.
type ViewImageInput struct {
	Path string `json:"path" jsonschema:"The absolute or workspace-relative path of a PNG, JPEG, GIF or WebP image"`
}

// ViewImageOutput describes the image; the picture itself follows the result.
type ViewImageOutput struct {
	Path     string `json:"path"`
	ImageURI string `json:"image_uri,omitempty"`
	MIME     string `json:"mime_type,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Resized  bool   `json:"resized,omitempty"`
	Note     string `json:"note,omitempty"`
	Error    string `json:"error,omitempty"`
}

// NewViewImageTool lets the model look at an image in the workspace:
// screenshots, diagrams, UI mock-ups, test output.
func NewViewImageTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "view_image",
			Description: "Look at an image file (PNG, JPEG, GIF, WebP) in the workspace, such as a screenshot, diagram or UI mock-up. The picture is shown to you right after this tool's result.",
		},
		func(ctx agent.Context, input ViewImageInput) (ViewImageOutput, error) {
			img, err := r.LoadImage(input.Path)
			if err != nil {
				return ViewImageOutput{Path: input.Path, Error: fmt.Sprintf("cannot view image: %v", err)}, nil
			}
			return ViewImageOutput{
				Path: input.Path, ImageURI: img.URI(), MIME: img.MIME,
				Width: img.Width, Height: img.Height, Resized: img.Resized,
				Note: "The image follows this result.",
			}, nil
		},
	)
}
