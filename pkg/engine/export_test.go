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
	"archive/zip"
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/docx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Export as Word: the editor's text, its images read beside the file.
func TestRenderDocx(t *testing.T) {
	w := openTest(t)
	var pic bytes.Buffer
	require.NoError(t, png.Encode(&pic, image.NewGray(image.Rect(0, 0, 4, 4))))
	write(t, w.Dir(), "notes/img/c.png", pic.String())

	data, err := w.RenderDocx("notes/week1.md", "# Week 1\n\n![chart](img/c.png) ![out](../../outside.png) ![root](/notes/img/c.png) ![encoded](img%2Fc.png)\n\n```mermaid\ngraph LR\n  a --> b\n```\n\n```mermaid\ngraph TD\n  x\n```", map[string]Diagram{"graph LR\n  a --> b": {PNG: pic.Bytes(), Width: 200}})
	require.NoError(t, err)
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	var names []string
	for _, f := range z.File {
		names = append(names, f.Name)
	}
	assert.Contains(t, names, "word/media/image1.png", "the image beside the file")
	assert.Contains(t, names, "word/media/image2.png", "from the workspace's root, as the preview reads it")
	assert.Contains(t, names, "word/media/image3.png", "percent-encoded")
	assert.Contains(t, names, "word/media/image4.png", "the drawn diagram")
	assert.NotContains(t, strings.Join(names, " "), "image5", "nothing outside the workspace; an undrawn diagram stays code")

	_, err = w.RenderDocx("", "# A conversation", nil)
	assert.NoError(t, err, "no file: no images to read")
	_, err = w.RenderDocx("../x.md", "# X", nil)
	assert.ErrorIs(t, err, ErrBadPath)
	_, err = w.RenderDocx("x.md", "\x00", nil)
	assert.ErrorIs(t, err, docx.ErrNotText)
}
