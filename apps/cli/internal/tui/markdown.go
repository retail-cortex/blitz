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

package tui

import (
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
)

// markdownStream renders streamed Markdown block by block: text is buffered
// until a block is complete (a blank line outside a code fence, or a closing
// fence), then that block is rendered, so output appears progressively
// without re-rendering what was already printed.
type markdownStream struct {
	r       *glamour.TermRenderer
	out     io.Writer
	pending strings.Builder
	scan    blockScan // how far pending has been read, so a write reads only what's new
}

func newMarkdownStream(out io.Writer, theme string, width int) (*markdownStream, error) {
	opts := []glamour.TermRendererOption{glamour.WithEmoji()}
	if theme == "" || theme == "auto" {
		// glamour's own auto-detection queries the terminal, which would read
		// from stdin underneath the line editor; use the environment instead.
		theme = themeFromEnv()
	}
	opts = append(opts, glamour.WithStandardStyle(theme))
	if width > 20 {
		opts = append(opts, glamour.WithWordWrap(min(width-4, 120)))
	}
	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return nil, err
	}
	return &markdownStream{r: r, out: out}, nil
}

// themeFromEnv picks "light" when COLORFGBG reports a light background
// (e.g. "0;15"), else "dark".
func themeFromEnv() string {
	v := os.Getenv("COLORFGBG")
	if i := strings.LastIndexByte(v, ';'); i >= 0 {
		switch v[i+1:] {
		case "7", "15":
			return "light"
		}
	}
	return "dark"
}

// Write adds streamed text and renders any completed blocks.
func (m *markdownStream) Write(text string) {
	m.pending.WriteString(text)
	buf := m.pending.String()
	if cut := m.scan.next(buf); cut > 0 {
		m.render(buf[:cut])
		m.pending.Reset()
		m.pending.WriteString(buf[cut:])
		m.scan.pos -= cut
	}
}

// Flush renders whatever remains.
func (m *markdownStream) Flush() {
	if m.pending.Len() > 0 {
		m.render(m.pending.String())
		m.pending.Reset()
	}
}

func (m *markdownStream) render(block string) {
	if strings.TrimSpace(block) == "" {
		return
	}
	out, err := m.r.Render(block)
	if err != nil {
		io.WriteString(m.out, block)
		return
	}
	io.WriteString(m.out, strings.TrimLeft(strings.TrimRight(out, "\n "), "\n")+"\n")
}

// completeBlocks returns the length of the longest prefix of s made of whole
// Markdown blocks, or 0 if no block is complete yet.
func completeBlocks(s string) int {
	var b blockScan
	return b.next(s)
}

// blockScan reads a growing buffer for whole Markdown blocks. It keeps
// where it stopped and whether it's inside a fence, so a long code block
// streamed in small pieces is read once, not again on every piece.
type blockScan struct {
	pos       int // the end of the last whole line read
	inFence   bool
	prevBlank bool
}

// next reads s's new whole lines and returns the length of the longest
// prefix made of whole blocks, or 0 if no new block is complete. A caller
// that drops that prefix from its buffer takes it off pos too.
func (b *blockScan) next(s string) int {
	cut := 0
	for {
		nl := strings.IndexByte(s[b.pos:], '\n')
		if nl < 0 {
			return cut
		}
		line := s[b.pos : b.pos+nl]
		end := b.pos + nl + 1
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			if b.inFence {
				cut = end // a closed fence ends a block
			}
			b.inFence = !b.inFence
		case !b.inFence && trimmed == "" && !b.prevBlank:
			cut = end
		}
		b.prevBlank = trimmed == ""
		b.pos = end
	}
}
