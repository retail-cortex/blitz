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
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/retail-cortex/blitz/pkg/pdftext"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// readPDF is read_file on a PDF: its text, paged like any file's. ok is
// false when the file isn't a PDF after all, for read_file to read as it
// is.
func readPDF(ctx context.Context, ws *Workspace, input ReadFileInput) (ReadFileOutput, bool) {
	fail := func(err error) (ReadFileOutput, bool) {
		return ReadFileOutput{Path: input.Path, Error: fmt.Sprintf("failed to read file: %v", err)}, true
	}
	rel, err := ws.Rel(input.Path)
	if err != nil {
		return fail(err)
	}
	data, err := ws.ReadFileLimit(rel, images.MaxDocumentBytes)
	if err != nil {
		return fail(err)
	}
	if !pdftext.IsPDF(data) {
		return ReadFileOutput{}, false
	}
	text, pages, err := pdfTexts.get(ctx, data)
	if err != nil {
		return fail(err)
	}
	out, err := pageLines(strings.NewReader(text), input)
	if err != nil {
		return fail(err)
	}
	out.Path = input.Path
	out.Note = fmt.Sprintf("The text taken from a %s PDF: layout, figures and equations may be lost; view_document shows the pages themselves.", pageCount(pages))
	return out, true
}

func pageCount(n int) string {
	if n == 1 {
		return "1-page"
	}
	return fmt.Sprintf("%d-page", n)
}

// pdfTextCache keeps the last few PDFs' text, so paging through one with
// read_file doesn't read it again each time.
type pdfTextCache struct {
	mu    sync.Mutex
	order [][32]byte
	docs  map[[32]byte]pdfDoc
}

type pdfDoc struct {
	text  string
	pages int
}

const pdfTextCacheSize = 8

var pdfTexts = &pdfTextCache{docs: map[[32]byte]pdfDoc{}}

func (c *pdfTextCache) get(ctx context.Context, data []byte) (string, int, error) {
	key := sha256.Sum256(data)
	c.mu.Lock()
	d, ok := c.docs[key]
	c.mu.Unlock()
	if ok {
		return d.text, d.pages, nil
	}
	text, pages, err := pdftext.Text(ctx, data, 0)
	if err != nil {
		return "", 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.docs[key]; !ok {
		if len(c.order) == pdfTextCacheSize {
			delete(c.docs, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, key)
		c.docs[key] = pdfDoc{text, pages}
	}
	return text, pages, nil
}

// ViewDocumentInput defines arguments for view_document.
type ViewDocumentInput struct {
	Path string `json:"path" jsonschema:"The absolute or workspace-relative path of a PDF"`
}

// ViewDocumentOutput describes the PDF; the document itself follows the
// result.
type ViewDocumentOutput struct {
	Path     string `json:"path"`
	ImageURI string `json:"image_uri,omitempty"`
	Pages    int    `json:"pages,omitempty"`
	Bytes    int    `json:"bytes,omitempty"`
	Note     string `json:"note,omitempty"`
	Error    string `json:"error,omitempty"`
}

// NewViewDocumentTool lets the model read a PDF in the workspace as it is,
// pages, figures and tables, where the model can (as its text where it
// can't): papers, slides, scanned notes.
func NewViewDocumentTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "view_document",
			Description: "Read a PDF in the workspace, such as a paper, slides or a report, with its figures and tables. The document is shown to you right after this tool's result (as its text if you can't read PDFs).",
		},
		func(ctx agent.Context, input ViewDocumentInput) (ViewDocumentOutput, error) {
			doc, err := r.LoadImage(input.Path)
			if err == nil && !doc.IsDocument() {
				err = fmt.Errorf("%s isn't a PDF: use view_image, view_media or read_file", input.Path)
			}
			if err != nil {
				return ViewDocumentOutput{Path: input.Path, Error: fmt.Sprintf("cannot view document: %v", err)}, nil
			}
			return ViewDocumentOutput{
				Path: input.Path, ImageURI: doc.URI(), Pages: doc.Pages, Bytes: len(doc.Data),
				Note: "The document follows this result.",
			}, nil
		},
	)
}

// ViewMediaInput defines arguments for view_media.
type ViewMediaInput struct {
	Path string `json:"path" jsonschema:"The absolute or workspace-relative path of an audio or video file"`
}

// ViewMediaOutput describes the audio or video; the file itself follows
// the result.
type ViewMediaOutput struct {
	Path     string  `json:"path"`
	ImageURI string  `json:"image_uri,omitempty"`
	MIME     string  `json:"mime_type,omitempty"`
	Seconds  float64 `json:"seconds,omitempty"`
	Bytes    int     `json:"bytes,omitempty"`
	Note     string  `json:"note,omitempty"`
	Error    string  `json:"error,omitempty"`
}

// NewViewMediaTool lets a model that takes audio or video (Gemini) listen
// to or watch a file in the workspace: a lecture, a recording, a screen
// capture of a bug. Others are told it can't.
func NewViewMediaTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "view_media",
			Description: "Listen to an audio file or watch a video in the workspace (MP3, WAV, M4A, FLAC, Ogg; MP4, MOV, WebM, AVI and more), if your model takes them. The file is given to you right after this tool's result.",
		},
		func(ctx agent.Context, input ViewMediaInput) (ViewMediaOutput, error) {
			m, err := r.LoadImage(input.Path)
			if err == nil && m.Kind != images.KindAudio && m.Kind != images.KindVideo {
				err = fmt.Errorf("%s isn't audio or video: use view_image, view_document or read_file", input.Path)
			}
			if err != nil {
				return ViewMediaOutput{Path: input.Path, Error: fmt.Sprintf("cannot open it: %v", err)}, nil
			}
			return ViewMediaOutput{
				Path: input.Path, ImageURI: m.URI(), MIME: m.MIME, Seconds: m.Seconds, Bytes: max(len(m.Data), m.Size),
				Note: "The file follows this result.",
			}, nil
		},
	)
}
