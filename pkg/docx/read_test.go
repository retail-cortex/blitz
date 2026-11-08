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

package docx

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A document written from Markdown reads back as Markdown with its
// headings, emphasis, list items and table.
func TestToMarkdown(t *testing.T) {
	src := "# Report\n\nSome **bold** and *italic* text.\n\n## Items\n\n- first\n- second\n\n| Name | Count |\n| --- | --- |\n| apples | 3 |\n"
	data, err := Render([]byte(src), Options{})
	require.NoError(t, err)
	md, err := ToMarkdown(data)
	require.NoError(t, err)
	for _, want := range []string{"# Report", "**bold**", "*italic*", "## Items", "- first", "- second", "| **Name** | **Count** |", "| apples | 3 |"} {
		assert.Contains(t, md, want, md)
	}
	_, err = ToMarkdown([]byte("not a zip"))
	assert.ErrorIs(t, err, ErrNotDocx)
}

// A document whose text is over the limit is too large to preview, not
// "not a Word document"; one at the limit reads.
func TestToMarkdownTooLarge(t *testing.T) {
	data, err := Render([]byte("# Report\n\n"+strings.Repeat("words ", 2000)), Options{})
	require.NoError(t, err)
	saved := maxDocumentXML
	t.Cleanup(func() { maxDocumentXML = saved })
	cases := []struct {
		name  string
		limit int64
		want  error
	}{
		{"over the limit", 1024, ErrTooLarge},
		{"within it", saved, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			maxDocumentXML = tc.limit
			_, err := ToMarkdown(data)
			if tc.want == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tc.want)
			assert.NotErrorIs(t, err, ErrNotDocx)
		})
	}
}

// A Word document's line breaks and tabs inside a paragraph read as
// Markdown's hard line break and a tab, and a zip without the document
// part isn't a Word document.
func TestToMarkdownBreaksAndTabs(t *testing.T) {
	zipped := func(name, body string) []byte {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		f, err := z.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(body))
		require.NoError(t, err)
		require.NoError(t, z.Close())
		return b.Bytes()
	}
	doc := `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>Line one</w:t><w:br/><w:t>Name</w:t><w:tab/><w:t>Count</w:t></w:r></w:p>
</w:body></w:document>`
	md, err := ToMarkdown(zipped("word/document.xml", doc))
	require.NoError(t, err)
	assert.Equal(t, "Line one  \nName\tCount", strings.TrimSpace(md))

	_, err = ToMarkdown(zipped("word/other.xml", doc))
	assert.ErrorIs(t, err, ErrNotDocx)
}

func TestParaAndTable(t *testing.T) {
	tests := []struct {
		style string
		list  bool
		want  string
	}{
		{"Title", false, "# x"},
		{"Heading3", false, "### x"},
		{"Heading7", false, "x"},
		{"ListParagraph", false, "- x"},
		{"", true, "- x"},
		{"", false, "x"},
	}
	for _, tt := range tests {
		p := &readPara{style: tt.style, list: tt.list}
		p.text.WriteString("x")
		assert.Equal(t, tt.want, paraMarkdown(p), tt.style)
	}
	assert.Equal(t, "", paraMarkdown(&readPara{}))
	assert.Equal(t, "| a | b |\n| --- | --- |\n| c\\|d |  |", tableMarkdown([][]string{{"a", "b"}, {"c|d"}}))
	assert.Equal(t, "", tableMarkdown(nil))
}
