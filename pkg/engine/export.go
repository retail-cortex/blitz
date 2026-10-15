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
	"fmt"
	"strings"

	"github.com/retail-cortex/blitz/pkg/docx"
	"github.com/retail-cortex/blitz/pkg/engine/mdpdf"
	"github.com/retail-cortex/blitz/pkg/markdown"
)

// maxDocxImage bounds an image a Word document may include.
const maxDocxImage = 20 << 20

// Diagram is a ```mermaid block drawn as a PNG by the caller, at Width
// pixels.
type Diagram struct {
	PNG   []byte
	Width int
}

// RenderDocx writes markdown (a Markdown file's text, as the editor has it,
// or a conversation's) as a Word document, for the desktop app's Export as
// Word and /export (FS-75). Its images are read beside docPath, the file
// it belongs to ("" for none), as the editor reads them: the agent's path
// rules bind the agent, not the user. diagrams are its ```mermaid blocks
// as the caller drew them, by their text; one not there stays as code.
func (w *Workspace) RenderDocx(docPath, md string, diagrams map[string]Diagram) ([]byte, error) {
	slash := ""
	if docPath != "" {
		var err error
		if _, slash, err = userPath(docPath); err != nil {
			return nil, err
		}
	}
	return docx.Render([]byte(md), docx.Options{
		PageSize: mdpdf.PageSizeFor(w.cfg.PDF.PageSize),
		Diagram: func(_, source string) ([]byte, int, bool) {
			d, ok := diagrams[strings.TrimSpace(source)]
			return d.PNG, d.Width, ok && len(d.PNG) > 0
		},
		// Pictures resolve as the preview resolves them (encoded paths,
		// "/" for the workspace's root).
		ReadImage: func(dest string) ([]byte, error) {
			local, ok := markdown.LocalPath(dest, slash)
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrBadPath, dest)
			}
			rel, _, err := userPath(local)
			if err != nil {
				return nil, err
			}
			data, _, err := w.files().UserReadFile(rel, maxDocxImage)
			return data, err
		},
	})
}
