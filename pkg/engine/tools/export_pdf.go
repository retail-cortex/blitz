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
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/pkg/docx"
	"github.com/retail-cortex/blitz/pkg/engine/mdpdf"
	"github.com/retail-cortex/blitz/pkg/markdown"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// ExportInput defines arguments for export_pdf and export_docx.
type ExportInput struct {
	Path      string `json:"path,omitempty" jsonschema:"The Markdown file to export (.md or .markdown); or give markdown instead"`
	Markdown  string `json:"markdown,omitempty" jsonschema:"Markdown to export directly, for a document of something that isn't a file (a summary of the conversation); needs output. Don't write a .md file first."`
	Output    string `json:"output,omitempty" jsonschema:"Where to write the document; default the file's name with the format's extension (required with markdown)"`
	Overwrite bool   `json:"overwrite,omitempty" jsonschema:"Replace the document if it already exists"`
}

// ExportOutput holds an export's result.
type ExportOutput struct {
	Path    string `json:"path"`
	Output  string `json:"output,omitempty"`
	Pages   int    `json:"pages,omitempty"`
	Bytes   int    `json:"bytes,omitempty"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ExportPDFInput and ExportPDFOutput are export_pdf's input and output.
type (
	ExportPDFInput  = ExportInput
	ExportPDFOutput = ExportOutput
)

// maxExportImage bounds an image a document may include.
const maxExportImage = 20 << 20

// ExportPDFOutputPath is where export_pdf writes: output, or path with
// its extension replaced by .pdf ("" with neither).
func ExportPDFOutputPath(p, output string) string { return exportOutputPath(p, output, ".pdf") }

// ExportDocxOutputPath is where export_docx writes: output, or path with
// its extension replaced by .docx ("" with neither).
func ExportDocxOutputPath(p, output string) string { return exportOutputPath(p, output, ".docx") }

func exportOutputPath(p, output, ext string) string {
	if output != "" || p == "" {
		return output
	}
	return strings.TrimSuffix(p, path.Ext(p)) + ext
}

// exportFormat is what an export tool writes.
type exportFormat struct {
	name        string // the tool's
	ext         string // ".pdf"
	noun        string // "PDF"
	description string
	// render writes the document, with how big it is for the approval
	// ("3 pages, 84 KB").
	render func(md []byte, readImage func(string) ([]byte, error)) (data []byte, size string, pages int, err error)
}

func isMarkdownPath(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// NewExportPDFTool typesets a Markdown file as a PDF beside it (or at
// output), or Markdown given directly as a PDF at output, so a PDF of
// something that isn't a file (a summary of the conversation) leaves no
// Markdown file behind. It goes through the same checks, approval and
// checkpoint as create_file. pageSize is a [pdf] page_size setting.
func NewExportPDFTool(ws *Workspace, hooks *Hooks, pageSize string) (tool.Tool, error) {
	return newExportTool(ws, hooks, exportFormat{
		name: "export_pdf", ext: ".pdf", noun: "PDF",
		description: "Make a PDF to print or share from Markdown: headings (with an outline), lists, tables, highlighted code and the images it links in the workspace. " +
			"Give path to export an existing Markdown file, or markdown (with output) for a PDF of anything else, such as a summary of the conversation: then don't write a Markdown file first, only the PDF is wanted. " +
			"Raw HTML isn't drawn and Mermaid diagrams print as their code.",
		render: func(md []byte, read func(string) ([]byte, error)) ([]byte, string, int, error) {
			res, err := mdpdf.Render(md, mdpdf.Options{PageSize: mdpdf.PageSizeFor(pageSize), ReadImage: read})
			if err != nil {
				return nil, "", 0, err
			}
			return res.PDF, fmt.Sprintf("%d pages, %s", res.Pages, humanSize(len(res.PDF))), res.Pages, nil
		},
	})
}

// NewExportDocxTool writes a Markdown file as a Word document beside it
// (or at output), or Markdown given directly at output, as
// NewExportPDFTool does for PDFs (FS-75).
func NewExportDocxTool(ws *Workspace, hooks *Hooks, pageSize string) (tool.Tool, error) {
	return newExportTool(ws, hooks, exportFormat{
		name: "export_docx", ext: ".docx", noun: "Word document",
		description: "Make a Word document (.docx) to edit or share from Markdown: headings in Word's heading styles (so its navigation pane and table of contents work), lists, tables, code, links and the images it links in the workspace. " +
			"Give path to export an existing Markdown file, or markdown (with output) for a document of anything else, such as a summary of the conversation: then don't write a Markdown file first. " +
			"Raw HTML is left out and Mermaid diagrams are kept as their code.",
		render: func(md []byte, read func(string) ([]byte, error)) ([]byte, string, int, error) {
			data, err := docx.Render(md, docx.Options{PageSize: mdpdf.PageSizeFor(pageSize), ReadImage: read})
			return data, humanSize(len(data)), 0, err
		},
	})
}

func newExportTool(ws *Workspace, hooks *Hooks, f exportFormat) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{Name: f.name, Description: f.description},
		func(ctx agent.Context, input ExportInput) (ExportOutput, error) {
			fail := func(msg string) (ExportOutput, error) {
				return ExportOutput{Path: input.Path, Error: msg}, nil
			}
			var src string // the Markdown file, "" for markdown given
			switch {
			case input.Path != "" && input.Markdown != "":
				return fail("give path or markdown, not both")
			case input.Path == "" && input.Markdown == "":
				return fail("give path (a Markdown file) or markdown")
			case input.Markdown != "" && input.Output == "":
				return fail("output is needed with markdown: where to write the " + f.noun)
			}
			if input.Path != "" {
				var err error
				if src, err = ws.Rel(input.Path); err != nil {
					return fail(err.Error())
				}
				if !isMarkdownPath(src) {
					return fail(f.name + " takes a Markdown file (.md or .markdown)")
				}
			}
			out := exportOutputPath(input.Path, input.Output, f.ext)
			if !strings.EqualFold(path.Ext(out), f.ext) {
				return fail("output must end in " + f.ext)
			}
			rel, err := ws.WritablePath(out)
			if err != nil {
				return fail(err.Error())
			}
			data := []byte(input.Markdown)
			if src != "" {
				if data, err = ws.ReadFile(src); err != nil {
					return fail(fmt.Sprintf("failed to read %s: %v", input.Path, err))
				}
			}
			if err := mdpdf.CheckText(data); err != nil {
				return fail(fmt.Sprintf("%s: %v", cmp.Or(input.Path, "markdown"), err))
			}
			// Images are relative to the Markdown file, or to the document
			// for markdown given, resolved as the preview resolves them, and
			// read as the file tools read: inside the workspace, never a
			// blocked path.
			docPath := filepath.ToSlash(cmp.Or(src, rel))
			doc, size, pages, err := f.render(data, func(dest string) ([]byte, error) {
				local, ok := markdown.LocalPath(dest, docPath)
				if !ok {
					return nil, fmt.Errorf("%s: not in the workspace", dest)
				}
				p, err := ws.Rel(local)
				if err != nil {
					return nil, err
				}
				return ws.ReadFileLimit(p, maxExportImage)
			})
			if err != nil {
				return fail(fmt.Sprintf("failed to write %s as a %s: %v", cmp.Or(input.Path, "the markdown"), f.noun, err))
			}

			unlock, err := ws.lockPaths(ctx, rel)
			if err != nil {
				return fail(err.Error())
			}
			defer unlock()
			verb := "Create"
			// Whether it exists is decided by Stat: a document too large to
			// read is still there.
			info, statErr := ws.Stat(rel)
			existed := statErr == nil
			if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
				return fail(fmt.Sprintf("failed to check %s: %v", rel, statErr))
			}
			var existing []byte
			var readErr error
			if existed {
				if !input.Overwrite {
					return fail(fmt.Sprintf("file '%s' already exists; set overwrite=true to replace it", out))
				}
				verb = "Overwrite"
				existing, readErr = ws.ReadFile(rel)
			}
			from := ""
			if src != "" {
				from = " from " + src
			}
			detail := fmt.Sprintf("%s %s%s (%s)", verb, rel, from, size)
			if err := hooks.Approve(ctx, writeApproval(ws, f.name, detail, "", rel)); err != nil {
				return fail(err.Error())
			}
			if existed {
				if readErr == nil {
					err = ws.unchanged(rel, existed, existing)
				} else if now, serr := ws.Stat(rel); serr != nil || now.Size() != info.Size() || !now.ModTime().Equal(info.ModTime()) {
					err = fmt.Errorf("%s %w", rel, errChangedDuringApproval) // too large to compare: its size and time
				}
				if err != nil {
					return fail(err.Error())
				}
				err = ws.WriteFileAtomic(ctx, rel, doc)
			} else {
				err = ws.CreateExclusive(ctx, rel, doc)
				if errors.Is(err, fs.ErrExist) {
					return fail(fmt.Sprintf("file '%s' already exists; set overwrite=true to replace it", out))
				}
			}
			if err != nil {
				return fail(fmt.Sprintf("failed to write file: %v", err))
			}
			return ExportOutput{Path: input.Path, Output: rel, Pages: pages, Bytes: len(doc), Success: true}, nil
		},
	)
}

// humanSize is a byte count for people: "512 B", "84 KB", "2.1 MB".
func humanSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
