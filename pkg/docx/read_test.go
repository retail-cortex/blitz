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
