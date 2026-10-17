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

// Package textutil holds small string helpers shared by tools and the TUI.
package textutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHTMLToText(t *testing.T) {
	got := HTMLToText("<div>a</div><div>b</div><p></p><p></p><p>c</p><noscript>x</noscript>")
	assert.Equal(t, "a\nb\nc", got, "HTMLToText = %q", got)
	got = HTMLToText("not html at all")
	assert.Equal(t, "not html at all", got, "plain text = %q", got)
	assert.Equal(t, "a\n\nb", collapseBlankLines("a\n\n\n\nb"), "runs of blank lines become one")
}
