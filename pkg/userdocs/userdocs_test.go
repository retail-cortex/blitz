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

package userdocs

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var sample = fstest.MapFS{
	"_index.md":               {Data: []byte("---\ntitle: Blitz\n---\n\nWelcome to Blitz.\n")},
	"guide/mcp.md":            {Data: []byte("---\ntitle: \"MCP servers\"\nweight: 3\n---\n\nConnect tools over MCP.\n\n## Add a server\n\nRun `blitz mcp add github https://example.com/mcp`, or ask the agent.\n\n```sh\n# not a heading\n```\n\n## Sign in\n\nSome servers use OAuth: `blitz mcp login <name>`.\n")},
	"guide/models.md":         {Data: []byte("---\ntitle: Models\n---\n\n## Providers\n\nSet a key with [the config command](../products/cli.md): `blitz config set-key gemini`.\n")},
	"releases/blitz-1.0.0.md": {Data: []byte("---\ntitle: Blitz 1.0.0\n---\n\nMCP servers got faster.\n")},
	"guide/image.png":         {Data: []byte("not markdown")},
}

func TestParse(t *testing.T) {
	ix := Load(sample)
	require.Len(t, ix.Pages, 4, "Markdown only")
	p, ok := ix.Find("guide/mcp")
	require.True(t, ok)
	assert.Equal(t, "MCP servers", p.Title)
	var heads []string
	for _, s := range p.Sections {
		heads = append(heads, s.Heading)
	}
	assert.Equal(t, []string{"", "Add a server", "Sign in"}, heads, "a # in a code block isn't a heading")
	assert.Equal(t, "add-a-server", p.Sections[1].Anchor)
	assert.NotContains(t, p.Body, "weight:", "no front matter")
}

func TestURLs(t *testing.T) {
	ix := Load(sample)
	home, _ := ix.Find("_index")
	assert.Equal(t, Site, home.URL())
	mcp, _ := ix.Find("mcp")
	assert.Equal(t, Site+"guide/mcp/", mcp.URL())
	hit := Hit{Page: mcp, Section: mcp.Sections[2]}
	assert.Equal(t, Site+"guide/mcp/#sign-in", hit.URL())
}

func TestSearch(t *testing.T) {
	ix := Load(sample)
	cases := map[string]struct {
		query, page, heading string
	}{
		"words in a heading":    {"how do I add an MCP server?", "guide/mcp", "Add a server"},
		"plurals":               {"providers", "guide/models", "Providers"},
		"a command's name":      {"oauth login", "guide/mcp", "Sign in"},
		"a guide before a note": {"mcp servers", "guide/mcp", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			hits := ix.Search(c.query, 3)
			require.NotEmpty(t, hits)
			assert.Equal(t, c.page, hits[0].Page.Path)
			if c.heading != "" {
				assert.Equal(t, c.heading, hits[0].Section.Heading)
			}
		})
	}
	assert.Empty(t, ix.Search("the and of", 3), "stop words find nothing")
	assert.Empty(t, ix.Search("xylophone", 3))
	assert.Len(t, ix.Search("mcp", 1), 1, "at most n")
}

func TestFind(t *testing.T) {
	ix := Load(sample)
	for _, topic := range []string{"guide/mcp", "mcp", "MCP servers", "guide/mcp.md", "/guide/mcp/"} {
		p, ok := ix.Find(topic)
		require.True(t, ok, topic)
		assert.Equal(t, "guide/mcp", p.Path, topic)
	}
	_, ok := ix.Find("nowhere")
	assert.False(t, ok)
	_, ok = ix.Find("")
	assert.False(t, ok)
}

func TestSnippetAndExcerpt(t *testing.T) {
	ix := Load(sample)
	hits := ix.Search("set a key", 1)
	require.NotEmpty(t, hits)
	assert.Equal(t, "Set a key with the config command: blitz config set-key gemini.", hits[0].Snippet(200), "links as their text, no code marks")
	assert.True(t, strings.HasSuffix(hits[0].Snippet(10), "…"))
	assert.Equal(t, "Run blitz mcp add <name> <url>; ui.status_line stays.", plain("> Run **`blitz mcp add <name> <url>`**; `ui.status_line` stays."), "names keep their _ and <>")
	ex := Excerpt(ix.Search("mcp server", 3), 10_000)
	assert.Contains(t, ex, "## MCP servers › Add a server")
	assert.Contains(t, ex, Site+"guide/mcp/#add-a-server")
	assert.LessOrEqual(t, len([]rune(Excerpt(ix.Search("mcp server", 3), 250))), 252, "cut to the limit")
}

// The docs the program carries are there and searchable.
func TestBuiltinDocs(t *testing.T) {
	ix := Docs()
	require.Greater(t, len(ix.Pages), 50)
	_, ok := ix.Find("guide/mcp")
	assert.True(t, ok)
	hits := ix.Search("add an MCP server", 5)
	require.NotEmpty(t, hits)
	var paths []string
	for _, h := range hits {
		paths = append(paths, h.Page.Path)
	}
	assert.Contains(t, paths, "guide/mcp", "found: %v", paths)
}
