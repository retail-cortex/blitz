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
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Maths as the desktop app's preview reads it (remark-math): $…$ inline,
// $$…$$ displayed, on a line of its own or across lines. A $ that opens
// must be followed by something other than a space, one that closes
// preceded by something other than a space and not followed by a digit,
// so prices ($5 and $10) stay text; \$ is a dollar sign.

// KindMath is an inline formula's kind.
var KindMath = ast.NewNodeKind("Math")

// Math is a formula in a paragraph: its LaTeX, and whether it was written
// $$…$$ (displayed) in the paragraph's text.
type Math struct {
	ast.BaseInline
	Source  string
	Display bool
}

// Kind implements ast.Node.
func (n *Math) Kind() ast.NodeKind { return KindMath }

// Dump implements ast.Node.
func (n *Math) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{"Source": n.Source}, nil)
}

// KindMathBlock is a displayed formula's kind.
var KindMathBlock = ast.NewNodeKind("MathBlock")

// MathBlock is a displayed formula: $$ on its own line, the LaTeX, $$.
type MathBlock struct {
	ast.BaseBlock
	Source string
	done   bool // written $$…$$ on one line: nothing more to read
}

// Kind implements ast.Node.
func (n *MathBlock) Kind() ast.NodeKind { return KindMathBlock }

// IsRaw implements ast.Node: its lines are LaTeX, not Markdown.
func (n *MathBlock) IsRaw() bool { return true }

// Dump implements ast.Node.
func (n *MathBlock) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{"Source": n.Source}, nil)
}

type mathExtension struct{}

// mathExt adds the maths parsers to a goldmark.
var mathExt goldmark.Extender = mathExtension{}

func (mathExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(util.Prioritized(inlineMath{}, 501)),
		parser.WithBlockParsers(util.Prioritized(blockMath{}, 701)),
	)
}

type inlineMath struct{}

func (inlineMath) Trigger() []byte { return []byte{'$'} }

func (inlineMath) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	if block.PrecendingCharacter() == '$' {
		return nil // inside a run of $ that didn't open a formula
	}
	line, _ := block.PeekLine()
	fence := run(line, 0)
	if fence > 2 {
		return nil
	}
	body := line[fence:]
	if len(body) == 0 || isSpace(body[0]) {
		return nil
	}
	for i := 0; i < len(body); i++ {
		switch {
		case body[i] == '\\':
			i++ // an escaped character, \$ among them
		case body[i] == '$':
			n := run(body, i)
			// A closing run is as long as the opening one, after something
			// other than a space, and (for $) before something other than
			// a digit.
			if n == fence && !isSpace(body[i-1]) && (fence == 2 || i+n >= len(body) || body[i+n] < '0' || body[i+n] > '9') {
				block.Advance(fence + i + n)
				return &Math{Source: string(body[:i]), Display: fence == 2}
			}
			i += n - 1
		}
	}
	return nil
}

// run is how many $ there are at line[i:].
func run(line []byte, i int) int {
	n := 0
	for i+n < len(line) && line[i+n] == '$' {
		n++
	}
	return n
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

type blockMath struct{}

func (blockMath) Trigger() []byte { return []byte{'$'} }

func (blockMath) Open(_ ast.Node, reader text.Reader, _ parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, []byte("$$")) {
		return nil, parser.NoChildren
	}
	rest := bytes.TrimSpace(trimmed[2:])
	n := &MathBlock{}
	if len(rest) > 0 {
		// $$…$$ on one line is a block only when nothing follows it.
		if !bytes.HasSuffix(rest, []byte("$$")) || len(rest) < 3 {
			return nil, parser.NoChildren
		}
		n.Source, n.done = string(bytes.TrimSpace(rest[:len(rest)-2])), true
		reader.AdvanceToEOL()
		return n, parser.NoChildren
	}
	reader.AdvanceToEOL()
	return n, parser.NoChildren
}

func (blockMath) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	n := node.(*MathBlock)
	line, _ := reader.PeekLine()
	if line == nil || n.done {
		return parser.Close
	}
	trimmed := bytes.TrimSpace(line)
	if end := bytes.Index(trimmed, []byte("$$")); end >= 0 && end == len(trimmed)-2 {
		n.Source = string(bytes.TrimSpace([]byte(n.Source + " " + string(trimmed[:end]))))
		reader.AdvanceToEOL()
		return parser.Close
	}
	if n.Source != "" {
		n.Source += "\n"
	}
	n.Source += string(bytes.TrimRight(line, "\r\n"))
	reader.AdvanceToEOL()
	return parser.Continue | parser.NoChildren
}

func (blockMath) Close(node ast.Node, _ text.Reader, _ parser.Context) {
	n := node.(*MathBlock)
	n.Source = string(bytes.TrimSpace([]byte(n.Source)))
}

func (blockMath) CanInterruptParagraph() bool { return true }

func (blockMath) CanAcceptIndentedLine() bool { return false }
