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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithMentions(t *testing.T) {
	w := openTest(t)
	dir := w.Dir()
	write := func(rel, text string) {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0o644))
	}
	write("main.go", "package main\n")
	write("docs/a b.md", "# Title")
	write("src/x.go", "package src\n")
	write("src/sub/y.go", "package sub\n")
	write("src/.hidden", "h")
	write("bin.dat", "a\x00b")
	write("big.txt", strings.Repeat("line of text\n", mentionMaxBytes/13+100))
	write("shot.png", "png")
	write(".env", "SECRET=1")
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(dir), "outside.txt"), []byte("out"), 0o644))

	cases := []struct {
		name, text string
		want       []string // in the added context
		not        []string
	}{
		{name: "no mentions", text: "explain the code", not: []string{"<mentioned-files>"}},
		{name: "a file", text: "explain @main.go please", want: []string{`<file path="main.go">` + "\npackage main\n</file>"}},
		{name: "trailing punctuation", text: "what's in @main.go?", want: []string{`<file path="main.go">`}},
		{name: "quoted, with a space, no final newline", text: `read @"docs/a b.md"`, want: []string{`<file path="docs/a b.md">` + "\n# Title\n</file>"}},
		{name: "a folder", text: "@src what's here", want: []string{`<folder path="src">` + "\nsub/\nx.go\n</folder>"}, not: []string{".hidden"}},
		{name: "a folder with a slash", text: "@src/", want: []string{`<folder path="src">`}},
		{name: "binary", text: "@bin.dat", want: []string{"(a binary file, 3 bytes: not shown)"}},
		{name: "too big: its start", text: "@big.txt", want: []string{"read_file for the rest"}},
		{name: "images are the front ends'", text: "@shot.png", not: []string{"<mentioned-files>"}},
		{name: "missing", text: "ask @someone", not: []string{"<mentioned-files>"}},
		{name: "blocked", text: "@.env", not: []string{"SECRET"}},
		{name: "outside the workspace", text: "@../outside.txt", not: []string{"out\n"}},
		{name: "an e-mail address isn't a mention", text: "mail me@main.go", not: []string{"<mentioned-files>"}},
		{name: "once each", text: "@main.go and @main.go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := w.withMentions(context.Background(), "PROMPT", c.text)
			require.True(t, strings.HasPrefix(got, "PROMPT"), "the prompt comes first:\n%s", got)
			for _, s := range c.want {
				assert.Contains(t, got, s)
			}
			for _, s := range c.not {
				assert.NotContains(t, got, s)
			}
			assert.LessOrEqual(t, strings.Count(got, `<file path="main.go">`), 1)
		})
	}
	big := w.withMentions(context.Background(), "", "@big.txt")
	assert.Less(t, len(big), mentionMaxBytes+1024, "a big file isn't sent whole")
	assert.True(t, strings.Contains(big, "line of text\n… (the first"), "cut at a line:\n%s", big[len(big)-200:])
}

// A mentioned file reaches the agent with the prompt; the transcript keeps
// the prompt as typed.
func TestMentionsReachTheAgent(t *testing.T) {
	w, llm := openTestWith(t, nil, text("it prints hi"))
	write(t, w.Dir(), "main.go", "package main // prints hi\n")
	ctx := context.Background()
	sid := newSession(t, w).ID
	_, err := w.Run(ctx, sid, api.Turn{Text: "what does @main.go do?"}, ignore)
	require.NoError(t, err)
	sent := lastUserText(llm)
	assert.Contains(t, sent, "what does @main.go do?")
	assert.Contains(t, sent, `<file path="main.go">`+"\npackage main // prints hi\n</file>")
	assert.Equal(t, []string{"user: what does @main.go do?", "model: it prints hi"}, transcript(t, w))
}
