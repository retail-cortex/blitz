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

package config

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// blitz mcp add, remove, enable and disable edit the [[mcp.servers]]
// entries of the global settings file in place, keeping everything else as
// written, comments included (spec_parity_027 PAR-MCP-01).

// ErrNoMCPServer: no [[mcp.servers]] entry has that name.
var ErrNoMCPServer = errors.New("no MCP server by that name")

// AddMCPServer appends s as a [[mcp.servers]] entry of the global settings
// (prefixDir as for Load). A server of the same name is refused.
func AddMCPServer(prefixDir string, s MCPServerConfig) (string, error) {
	if s.Name == "" || (s.Command == "") == (s.URL == "") {
		return "", errors.New("an MCP server needs a name and either a command or a URL")
	}
	exists := false
	path, err := editConfigFile(ConfigDir(prefixDir), func(doc string) string {
		if _, _, ok := mcpBlock(doc, s.Name); ok {
			exists = true
			return doc
		}
		if doc != "" && !strings.HasSuffix(doc, "\n") {
			doc += "\n"
		}
		return doc + "\n" + mcpServerTOML(s)
	}, func(m map[string]any) error { return oneNamed(m, s.Name) })
	if err == nil && exists {
		return "", fmt.Errorf("an MCP server named %q is already configured", s.Name)
	}
	return path, err
}

// RemoveMCPServer deletes the entry named name.
func RemoveMCPServer(prefixDir, name string) (string, error) {
	var found bool
	path, err := editConfigFile(ConfigDir(prefixDir), func(doc string) string {
		start, end, ok := mcpBlock(doc, name)
		if !ok {
			return doc
		}
		found = true
		lines := strings.Split(doc, "\n")
		return strings.Join(append(lines[:start:start], lines[end:]...), "\n")
	}, func(map[string]any) error { return nil })
	if err == nil && !found {
		return "", fmt.Errorf("%w: %s", ErrNoMCPServer, name)
	}
	return path, err
}

// SetMCPServerDisabled turns the entry named name off (disabled = true) or
// on again.
func SetMCPServerDisabled(prefixDir, name string, disabled bool) (string, error) {
	var found bool
	path, err := editConfigFile(ConfigDir(prefixDir), func(doc string) string {
		start, end, ok := mcpBlock(doc, name)
		if !ok {
			return doc
		}
		found = true
		lines := strings.Split(doc, "\n")
		line := "disabled = " + strconv.FormatBool(disabled)
		for i := start + 1; i < end; i++ {
			if k, _, ok := strings.Cut(strings.TrimSpace(lines[i]), "="); ok && strings.TrimSpace(k) == "disabled" {
				lines[i] = line
				return strings.Join(lines, "\n")
			}
		}
		return strings.Join(slices.Insert(lines, start+1, line), "\n")
	}, func(map[string]any) error { return nil })
	if err == nil && !found {
		return "", fmt.Errorf("%w: %s", ErrNoMCPServer, name)
	}
	return path, err
}

// mcpBlock finds the [[mcp.servers]] entry named name: its header's line,
// and the line after its last (sub-tables such as [mcp.servers.env]
// belong to it).
func mcpBlock(doc, name string) (start, end int, ok bool) {
	lines := strings.Split(doc, "\n")
	for i := 0; i < len(lines); i++ {
		m := tableRE.FindStringSubmatch(lines[i])
		if m == nil || m[1] != "mcp.servers" || !strings.HasPrefix(strings.TrimSpace(lines[i]), "[[") {
			continue
		}
		j := i + 1
		for ; j < len(lines); j++ {
			if n := tableRE.FindStringSubmatch(lines[j]); n != nil && !strings.HasPrefix(n[1], "mcp.servers.") {
				break
			}
		}
		var entry struct {
			Name string `toml:"name"`
		}
		body := strings.Join(lines[i+1:j], "\n")
		if cut := strings.Index(body, "\n["); cut >= 0 { // its sub-tables aren't needed
			body = body[:cut]
		}
		if _, err := toml.Decode(body, &entry); err == nil && entry.Name == name {
			// Trailing blank lines stay with the next entry.
			for j > i+1 && strings.TrimSpace(lines[j-1]) == "" {
				j--
			}
			return i, j, true
		}
		i = j - 1
	}
	return 0, 0, false
}

// oneNamed checks that exactly one [[mcp.servers]] entry is named name.
func oneNamed(m map[string]any, name string) error {
	mcp, _ := m["mcp"].(map[string]any)
	list, _ := mcp["servers"].([]map[string]any)
	n := 0
	for _, s := range list {
		if s["name"] == name {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("an MCP server named %q is already configured", name)
	}
	return nil
}

// mcpServerTOML is s as a [[mcp.servers]] entry.
func mcpServerTOML(s MCPServerConfig) string {
	var b strings.Builder
	b.WriteString("[[mcp.servers]]\n")
	fmt.Fprintf(&b, "name = %s\n", strconv.Quote(s.Name))
	if s.Command != "" {
		fmt.Fprintf(&b, "command = %s\n", strconv.Quote(s.Command))
		if len(s.Args) > 0 {
			fmt.Fprintf(&b, "args = %s\n", tomlList(s.Args))
		}
	} else {
		fmt.Fprintf(&b, "url = %s\n", strconv.Quote(s.URL))
	}
	if len(s.Env) > 0 {
		fmt.Fprintf(&b, "env = %s\n", tomlInline(s.Env))
	}
	if len(s.Headers) > 0 {
		fmt.Fprintf(&b, "headers = %s\n", tomlInline(s.Headers))
	}
	if s.Prefix != "" {
		fmt.Fprintf(&b, "prefix = %s\n", strconv.Quote(s.Prefix))
	}
	if len(s.Agents) > 0 {
		fmt.Fprintf(&b, "agents = %s\n", tomlList(s.Agents))
	}
	return b.String()
}

func tomlList(v []string) string {
	q := make([]string, len(v))
	for i, s := range v {
		q[i] = strconv.Quote(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

func tomlInline(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = strconv.Quote(k) + " = " + strconv.Quote(m[k])
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}
