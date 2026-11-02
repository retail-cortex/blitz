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

package catalog

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// Place sets how the item at p (a path in the catalog) applies: its lock
// (Locked or Default) and groups (none: everyone), in a Markdown file's
// front matter, made when it has none, or an MCP server's TOML; any other
// file (a skill's script) is as it was. The rest of the file is kept
// (spec_053_enterprise EN-36, Publish to organization).
func Place(p string, data []byte, lock string, groups []string) ([]byte, error) {
	if lock != Locked && lock != Default {
		return nil, fmt.Errorf("lock %q: locked or default", lock)
	}
	kind, _, ok := Of(p)
	switch {
	case !ok:
		return nil, fmt.Errorf("%s isn't a catalog item's path", p)
	case kind == MCP:
		return placeTOML(data, lock, groups)
	case kind == SkillFile || kind == Connector:
		return data, nil
	}
	return placeMarkdown(data, lock, groups)
}

// placeMarkdown sets lock and groups in the front matter, the other keys
// in their order.
func placeMarkdown(data []byte, lock string, groups []string) ([]byte, error) {
	t := bytes.TrimLeft(data, "\uFEFF \t\r\n")
	var doc yaml.Node
	body := t
	if fm := frontMatter(data); fm != nil {
		if err := yaml.Unmarshal(fm, &doc); err != nil {
			return nil, fmt.Errorf("its front matter: %w", err)
		}
		// The body starts after the closing ---, its line and newline.
		rest := t[3+len(fm)+4:]
		body = bytes.TrimPrefix(bytes.TrimPrefix(rest, []byte("\r")), []byte("\n"))
	}
	if len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("its front matter isn't a map of fields")
	}
	set := func(key string, value *yaml.Node) {
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == key {
				if value == nil {
					m.Content = append(m.Content[:i], m.Content[i+2:]...)
				} else {
					m.Content[i+1] = value
				}
				return
			}
		}
		if value != nil {
			m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
		}
	}
	set("lock", &yaml.Node{Kind: yaml.ScalarNode, Value: lock})
	var g *yaml.Node
	if len(groups) > 0 {
		g = &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, name := range groups {
			g.Content = append(g.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: name})
		}
	}
	set("groups", g)
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return []byte("---\n" + out.String() + "---\n" + string(body)), nil
}

// placeTOML sets lock and groups among an MCP server's fields.
func placeTOML(data []byte, lock string, groups []string) ([]byte, error) {
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("its TOML: %w", err)
	}
	m["lock"] = lock
	delete(m, "groups")
	if len(groups) > 0 {
		m["groups"] = groups
	}
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(m); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}
