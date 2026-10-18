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

// Package frontmatter reads and edits the YAML front matter of Markdown
// files (SKILL.md, rules) the way Studio's forms do: the fields a form
// edits are set, or removed when empty, and every other field is kept,
// in its order.
package frontmatter

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Split is a Markdown file's front matter (without its --- lines) and
// body; ok is false when it has none.
func Split(data []byte) (front, body []byte, ok bool) {
	t := bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\ufeff")))
	if !bytes.HasPrefix(t, []byte("---")) {
		return nil, t, false
	}
	rest := t[3:]
	i := bytes.Index(rest, []byte("\n---"))
	if i < 0 {
		return nil, t, false
	}
	return rest[:i], bytes.TrimSpace(rest[i+4:]), true
}

// Fields is a front matter mapping, edited in place.
type Fields struct{ m *yaml.Node }

// Edit is old (nil: a new file) with its front matter changed by set and
// body as its body: front matter, a blank line, then body.
func Edit(old []byte, body string, set func(Fields)) ([]byte, error) {
	var doc yaml.Node
	if front, _, ok := Split(old); ok {
		if err := yaml.Unmarshal(front, &doc); err != nil {
			return nil, fmt.Errorf("the front matter: %w", err)
		}
	}
	if len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		return nil, errors.New("the front matter isn't a mapping")
	}
	set(Fields{m})
	front, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(front)
	b.WriteString("---\n\n")
	if body = strings.TrimSpace(body); body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.Bytes(), nil
}

// index is where key's value is, or -1.
func (f Fields) index(key string) int {
	for i := 0; i+1 < len(f.m.Content); i += 2 {
		if f.m.Content[i].Value == key {
			return i + 1
		}
	}
	return -1
}

// set puts v as key's value (added at the end when new), or removes key
// when v is nil.
func (f Fields) set(key string, v *yaml.Node) {
	i := f.index(key)
	switch {
	case v == nil && i >= 0:
		f.m.Content = append(f.m.Content[:i-1], f.m.Content[i+1:]...)
	case v == nil:
	case i >= 0:
		f.m.Content[i] = v
	default:
		f.m.Content = append(f.m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, v)
	}
}

// String sets key to value, or removes it when value is "".
func (f Fields) String(key, value string) {
	if value == "" {
		f.set(key, nil)
		return
	}
	f.set(key, &yaml.Node{Kind: yaml.ScalarNode, Value: value})
}

// Int sets key to n, or removes it when n is 0.
func (f Fields) Int(key string, n int) {
	if n == 0 {
		f.set(key, nil)
		return
	}
	f.set(key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(n)})
}

// Bool sets key to true, or removes it when b is false.
func (f Fields) Bool(key string, b bool) {
	if !b {
		f.set(key, nil)
		return
	}
	f.set(key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
}

// Map sets key to m, in its keys' sorted order, or removes it when m is
// empty.
func (f Fields) Map(key string, m map[string]any) {
	if len(m) == 0 {
		f.set(key, nil)
		return
	}
	var n yaml.Node
	if err := n.Encode(m); err != nil {
		f.set(key, nil)
		return
	}
	f.set(key, &n)
}

// List sets key to values, written on one line, or removes it when
// there are none.
func (f Fields) List(key string, values []string) {
	if len(values) == 0 {
		f.set(key, nil)
		return
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, v := range values {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: v})
	}
	f.set(key, seq)
}
