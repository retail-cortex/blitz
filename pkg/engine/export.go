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
	"path"

	"github.com/retail-cortex/blitz/pkg/docx"
	"github.com/retail-cortex/blitz/pkg/engine/mdpdf"
)

// maxDocxImage bounds an image a Word document may include.
const maxDocxImage = 20 << 20

// RenderDocx writes markdown (a Markdown file's text, as the editor has it,
// or a conversation's) as a Word document, for the desktop app's Export as
// Word and /export (FS-75). Its images are read beside docPath, the file
// it belongs to ("" for none), as the editor reads them: the agent's path
// rules bind the agent, not the user.
func (w *Workspace) RenderDocx(docPath, markdown string) ([]byte, error) {
	dir := "."
	if docPath != "" {
		_, slash, err := userPath(docPath)
		if err != nil {
			return nil, err
		}
		dir = path.Dir(slash)
	}
	return docx.Render([]byte(markdown), docx.Options{
		PageSize: mdpdf.PageSizeFor(w.cfg.PDF.PageSize),
		ReadImage: func(dest string) ([]byte, error) {
			rel, _, err := userPath(path.Join(dir, dest))
			if err != nil {
				return nil, err
			}
			data, _, err := w.files().UserReadFile(rel, maxDocxImage)
			return data, err
		},
	})
}
