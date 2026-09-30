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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// What a prompt's @mentions may add to it.
const (
	mentionMaxFiles   = 20       // mentions read per prompt
	mentionMaxBytes   = 64 << 10 // of one file
	mentionMaxTotal   = 256 << 10
	mentionMaxEntries = 200 // of one folder's listing
)

// withMentions adds what the prompt's @paths name (text as typed) to the
// prompt for the agent: a file's content (up to mentionMaxBytes, else its
// start and a note to read the rest), a folder's listing. Images are the
// front ends' (they attach them); paths that don't exist, or that the
// workspace doesn't let the agent read (outside it, blocked), are left as
// written. The transcript keeps the text as typed.
func (w *Workspace) withMentions(ctx context.Context, prompt, text string) string {
	ws := w.tools.Workspace()
	var b strings.Builder
	n, total := 0, 0
	for _, p := range images.MentionedPaths(text) {
		if n == mentionMaxFiles || total >= mentionMaxTotal {
			break
		}
		// @server:uri names an MCP server's resource (PAR-MCP-02).
		if content, ok, err := w.tools.MCP().ResourceMention(ctx, p); ok {
			block := fmt.Sprintf("<resource name=%q>\n", p)
			if err != nil {
				block += "(couldn't be read: " + err.Error() + ")\n"
			} else {
				block += textutil.Ellipsize(content, min(mentionMaxBytes, mentionMaxTotal-total)) + "\n"
			}
			block += "</resource>\n"
			b.WriteString(block)
			n++
			total += len(block)
			continue
		}
		if images.IsImagePath(p) {
			continue
		}
		rel, err := ws.Rel(strings.TrimSuffix(p, "/"))
		if err != nil {
			continue
		}
		info, err := ws.Stat(rel)
		if err != nil {
			continue
		}
		var block string
		if info.IsDir() {
			block = w.mentionFolder(rel)
		} else {
			block = w.mentionFile(rel, info.Size(), min(mentionMaxBytes, mentionMaxTotal-total))
		}
		if block == "" {
			continue
		}
		b.WriteString(block)
		n++
		total += len(block)
	}
	if b.Len() == 0 {
		return prompt
	}
	return prompt + "\n\n<mentioned-files>\nThe user mentioned these; they're as they are now.\n" + b.String() + "</mentioned-files>"
}

// mentionFile is a file's block: its text, or its first limit bytes and
// a note, or a note for a binary file.
func (w *Workspace) mentionFile(rel string, size int64, limit int) string {
	f, err := w.tools.Workspace().Open(rel)
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)))
	if err != nil {
		return ""
	}
	head := fmt.Sprintf("<file path=%q>\n", rel)
	switch {
	case bytes.IndexByte(data, 0) >= 0:
		return head + fmt.Sprintf("(a binary file, %d bytes: not shown)\n</file>\n", size)
	case size > int64(len(data)):
		// Cut at a line, so the last line isn't half a line.
		if i := bytes.LastIndexByte(data, '\n'); i > 0 {
			data = data[:i+1]
		}
		return head + string(data) + fmt.Sprintf("… (the first %d of %d bytes: read_file for the rest)\n</file>\n", len(data), size)
	}
	text := string(data)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return head + text + "</file>\n"
}

// mentionFolder is a folder's block: its entries, folders marked with a
// slash, hidden and blocked ones left out.
func (w *Workspace) mentionFolder(rel string) string {
	abs, err := w.tools.Workspace().Abs(rel)
	if err != nil {
		return ""
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return ""
	}
	ws := w.tools.Workspace()
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := ws.Rel(filepath.Join(rel, e.Name())); err != nil {
			continue // blocked
		}
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	slices.Sort(names)
	more := ""
	if len(names) > mentionMaxEntries {
		more = fmt.Sprintf("… and %d more\n", len(names)-mentionMaxEntries)
		names = names[:mentionMaxEntries]
	}
	list := strings.Join(names, "\n")
	if list != "" {
		list += "\n"
	}
	return fmt.Sprintf("<folder path=%q>\n%s%s</folder>\n", rel, list, more)
}
