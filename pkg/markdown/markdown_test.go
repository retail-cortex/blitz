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

package markdown

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/ast"
)

func TestParse(t *testing.T) {
	doc, src := Parse([]byte("---\ntitle: x\n---\n# A *b* &amp; \\*c\\* `d`\n\ntext <b>raw</b> https://example.com\nline\n\n```go\nx := 1\ny := 2\n```\n"))
	assert.Equal(t, "A b & *c* d", FirstHeading(doc, src))
	para := doc.FirstChild().NextSibling()
	assert.Equal(t, "text raw https://example.com line", PlainText(para, src))
	code := para.NextSibling()
	assert.Equal(t, []string{"x := 1", "y := 2"}, Lines(code, src))
	assert.Equal(t, ast.KindFencedCodeBlock, code.Kind())
	empty, src := Parse([]byte("no heading"))
	assert.Empty(t, FirstHeading(empty, src))
}
