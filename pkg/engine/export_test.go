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

	data, err := w.RenderDocx("notes/week1.md", "# Week 1\n\n![chart](img/c.png) ![out](../../outside.png)")
	require.NoError(t, err)
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	var names []string
	for _, f := range z.File {
		names = append(names, f.Name)
	}
	assert.Contains(t, names, "word/media/image1.png", "the image beside the file")
	assert.NotContains(t, strings.Join(names, " "), "image2", "nothing outside the workspace")

	_, err = w.RenderDocx("", "# A conversation")
	assert.NoError(t, err, "no file: no images to read")
	_, err = w.RenderDocx("../x.md", "# X")
	assert.ErrorIs(t, err, ErrBadPath)
	_, err = w.RenderDocx("x.md", "\x00")
	assert.ErrorIs(t, err, docx.ErrNotText)
}
