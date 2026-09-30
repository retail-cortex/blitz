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
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"gopkg.in/yaml.v3"
)

// Output styles (spec_parity_027 PAR-MEM-21): instructions on how the agent
// writes its answers, added to its instructions. Built in: default (none),
// concise and explanatory; the user's are Markdown files in
// ~/.blitz/styles/<name>.md, with an optional description in frontmatter,
// and win over a built-in of the same name.

//go:embed styles/*.md
var builtinStyles embed.FS

// DefaultStyle adds nothing.
const DefaultStyle = "default"

type style struct {
	name, description, source, text string
}

// StylesDir is where the user's styles are.
func StylesDir() string { return config.ExpandHome("~/.blitz/styles") }

// allStyles are the styles by name: the built-ins, then the user's.
func allStyles() map[string]style {
	out := map[string]style{DefaultStyle: {name: DefaultStyle, description: "No style: the agent's own way of answering", source: "built-in"}}
	entries, _ := builtinStyles.ReadDir("styles")
	for _, e := range entries {
		data, err := builtinStyles.ReadFile("styles/" + e.Name())
		if err == nil {
			s := parseStyle(strings.TrimSuffix(e.Name(), ".md"), data)
			s.source = "built-in"
			out[s.name] = s
		}
	}
	files, _ := filepath.Glob(filepath.Join(StylesDir(), "*.md"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		s := parseStyle(strings.TrimSuffix(filepath.Base(f), ".md"), data)
		s.source = f
		out[s.name] = s
	}
	return out
}

func parseStyle(name string, data []byte) style {
	s := style{name: strings.ToLower(name)}
	text := data
	if rest, ok := bytes.CutPrefix(data, []byte("---\n")); ok {
		if head, body, ok := bytes.Cut(rest, []byte("\n---\n")); ok {
			var fm struct {
				Description string `yaml:"description"`
			}
			if yaml.Unmarshal(head, &fm) == nil {
				s.description = fm.Description
			}
			text = body
		}
	}
	s.text = strings.TrimSpace(string(text))
	return s
}

// ListStyles are the output styles, by name, with which is in use.
func (w *Workspace) ListStyles() []api.StyleInfo {
	var out []api.StyleInfo
	active := w.styleName()
	for _, s := range allStyles() {
		out = append(out, api.StyleInfo{Name: s.name, Description: s.description, Source: s.source, Active: s.name == active})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (w *Workspace) styleName() string {
	w.styleMu.Lock()
	defer w.styleMu.Unlock()
	if w.style == "" {
		return DefaultStyle
	}
	return w.style
}

// setStyle uses the output style name ("" or default: none) from the next
// turn.
func (w *Workspace) setStyle(ctx context.Context, name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = DefaultStyle
	}
	if _, ok := allStyles()[name]; !ok {
		return &api.InvalidSettingError{Err: fmt.Errorf("no output style %q (/style lists them)", name)}
	}
	w.styleMu.Lock()
	w.style = name
	w.styleMu.Unlock()
	return w.engine.SetInstructions(ctx, w.instructions())
}

// styleText is the style in use's instructions ("" for none, or one that
// has gone).
func (w *Workspace) styleText() string {
	name := w.styleName()
	if name == DefaultStyle {
		return ""
	}
	s, ok := allStyles()[name]
	if !ok || s.text == "" {
		return ""
	}
	return "\n\n" + s.text
}
