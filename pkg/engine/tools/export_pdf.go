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
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/mdpdf"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// ExportPDFInput defines arguments for export_pdf.
type ExportPDFInput struct {
	Path      string `json:"path" jsonschema:"The Markdown file to export (.md or .markdown)"`
	Output    string `json:"output,omitempty" jsonschema:"Where to write the PDF; default the same name with .pdf"`
	Overwrite bool   `json:"overwrite,omitempty" jsonschema:"Replace the PDF if it already exists"`
}

// ExportPDFOutput holds the export's result.
type ExportPDFOutput struct {
	Path    string `json:"path"`
	Output  string `json:"output,omitempty"`
	Pages   int    `json:"pages,omitempty"`
	Bytes   int    `json:"bytes,omitempty"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// maxPDFImage bounds an image a document may include.
const maxPDFImage = 20 << 20

// ExportPDFOutputPath is where export_pdf writes: output, or path with
// its extension replaced by .pdf.
func ExportPDFOutputPath(p, output string) string {
	if output != "" {
		return output
	}
	return strings.TrimSuffix(p, path.Ext(p)) + ".pdf"
}

func isMarkdownPath(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// NewExportPDFTool typesets a Markdown file as a PDF beside it (or at
// output), through the same checks, approval and checkpoint as
// create_file. pageSize is a [pdf] page_size setting.
func NewExportPDFTool(ws *Workspace, hooks *Hooks, pageSize string) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name: "export_pdf",
			Description: "Export a Markdown file as a PDF to print or share: headings (with an outline), lists, tables, highlighted code and the images it links in the workspace. " +
				"Raw HTML isn't drawn and Mermaid diagrams print as their code.",
		},
		func(ctx agent.Context, input ExportPDFInput) (ExportPDFOutput, error) {
			fail := func(msg string) (ExportPDFOutput, error) {
				return ExportPDFOutput{Path: input.Path, Error: msg}, nil
			}
			src, err := ws.Rel(input.Path)
			if err != nil {
				return fail(err.Error())
			}
			if !isMarkdownPath(src) {
				return fail("export_pdf takes a Markdown file (.md or .markdown)")
			}
			out := ExportPDFOutputPath(input.Path, input.Output)
			if !strings.EqualFold(path.Ext(out), ".pdf") {
				return fail("output must end in .pdf")
			}
			rel, err := ws.WritablePath(out)
			if err != nil {
				return fail(err.Error())
			}
			data, err := ws.ReadFile(src)
			if err != nil {
				return fail(fmt.Sprintf("failed to read %s: %v", input.Path, err))
			}
			if err := mdpdf.CheckText(data); err != nil {
				return fail(fmt.Sprintf("%s: %v", input.Path, err))
			}
			dir := path.Dir(src)
			res, err := mdpdf.Render(data, mdpdf.Options{
				PageSize: mdpdf.PageSizeFor(pageSize),
				// Images are read as the file tools read: inside the
				// workspace, never a blocked path.
				ReadImage: func(dest string) ([]byte, error) {
					p, err := ws.Rel(path.Join(dir, dest))
					if err != nil {
						return nil, err
					}
					return ws.ReadFileLimit(p, maxPDFImage)
				},
			})
			if err != nil {
				return fail(fmt.Sprintf("failed to typeset %s: %v", input.Path, err))
			}

			unlock, err := ws.lockPaths(ctx, rel)
			if err != nil {
				return fail(err.Error())
			}
			defer unlock()
			verb := "Create"
			existing, readErr := ws.ReadFile(rel)
			existed := readErr == nil
			if existed {
				if !input.Overwrite {
					return fail(fmt.Sprintf("file '%s' already exists; set overwrite=true to replace it", out))
				}
				verb = "Overwrite"
			}
			detail := fmt.Sprintf("%s %s from %s (%d pages, %s)", verb, rel, src, res.Pages, humanSize(len(res.PDF)))
			if err := hooks.Approve(ctx, writeApproval(ws, "export_pdf", detail, "", rel)); err != nil {
				return fail(err.Error())
			}
			if existed {
				if err := ws.unchanged(rel, existed, existing); err != nil {
					return fail(err.Error())
				}
				err = ws.WriteFileAtomic(ctx, rel, res.PDF)
			} else {
				err = ws.CreateExclusive(ctx, rel, res.PDF)
				if errors.Is(err, fs.ErrExist) {
					return fail(fmt.Sprintf("file '%s' already exists; set overwrite=true to replace it", out))
				}
			}
			if err != nil {
				return fail(fmt.Sprintf("failed to write file: %v", err))
			}
			return ExportPDFOutput{Path: input.Path, Output: rel, Pages: res.Pages, Bytes: len(res.PDF), Success: true}, nil
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
