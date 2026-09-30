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

// Package plugins installs and loads Blitz plugins (spec_parity_027
// §7.2): a directory with a plugin.toml bundling skills, commands, agents,
// hooks and MCP servers. Installed plugins live in
// ~/.blitz/plugins/<name>/<version>, pinned by the hash of their content: a
// plugin whose files changed after it was installed isn't loaded.
package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/retail-cortex/blitz/pkg/config"
)

// Manifest is the file every plugin has at its root.
const Manifest = "plugin.toml"

// The component directories and files a plugin may hold.
const (
	SkillsDir   = "skills"   // <name>/SKILL.md, as skills.paths
	CommandsDir = "commands" // *.md, as .blitz/commands
	AgentsDir   = "agents"   // *.md, as ~/.blitz/agents
	HooksFile   = "hooks.toml"
	MCPFile     = "mcp.toml"
)

// Meta is plugin.toml.
type Meta struct {
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
	Author      string `toml:"author"`
	Homepage    string `toml:"homepage"`
}

// hooksFile is hooks.toml: [[pre_tool]] and the other events, as [hooks].
type hooksFile = config.HooksConfig

// mcpFile is mcp.toml: [[servers]], as [mcp].
type mcpFile = config.MCPConfig

// Plugin is a plugin read from a directory.
type Plugin struct {
	Meta
	Dir string

	Skills   []string // skill names
	Commands []string // command names
	Agents   []string // agent names
	Hooks    config.HooksConfig
	MCP      []config.MCPServerConfig
}

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Read reads the plugin in dir.
func Read(dir string) (*Plugin, error) {
	p := &Plugin{Dir: dir}
	if _, err := toml.DecodeFile(filepath.Join(dir, Manifest), &p.Meta); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s has no %s: not a plugin", dir, Manifest)
		}
		return nil, fmt.Errorf("%s: %w", Manifest, err)
	}
	if !validName.MatchString(p.Name) {
		return nil, fmt.Errorf("%s: name %q: lower-case letters, digits, '.', '_' and '-'", Manifest, p.Name)
	}
	if p.Version == "" {
		p.Version = "0.0.0"
	}
	if !validName.MatchString(strings.ToLower(p.Version)) {
		return nil, fmt.Errorf("%s: version %q", Manifest, p.Version)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, SkillsDir))
	for _, e := range entries {
		if e.IsDir() && exists(filepath.Join(dir, SkillsDir, e.Name(), "SKILL.md")) {
			p.Skills = append(p.Skills, e.Name())
		}
	}
	p.Commands = mdNames(filepath.Join(dir, CommandsDir))
	p.Agents = mdNames(filepath.Join(dir, AgentsDir))
	if exists(filepath.Join(dir, HooksFile)) {
		var h hooksFile
		if _, err := toml.DecodeFile(filepath.Join(dir, HooksFile), &h); err != nil {
			return nil, fmt.Errorf("%s: %w", HooksFile, err)
		}
		p.Hooks = h
	}
	if exists(filepath.Join(dir, MCPFile)) {
		var m mcpFile
		if _, err := toml.DecodeFile(filepath.Join(dir, MCPFile), &m); err != nil {
			return nil, fmt.Errorf("%s: %w", MCPFile, err)
		}
		for _, s := range m.Servers {
			if s.Name == "" || (s.Command == "") == (s.URL == "") {
				return nil, fmt.Errorf("%s: server %q needs a name and one of command or url", MCPFile, s.Name)
			}
		}
		p.MCP = m.Servers
	}
	return p, nil
}

// Summary lists what the plugin adds, one line each, those that run code
// first: what installing it means.
func (p *Plugin) Summary() []string {
	var out []string
	for event, hooks := range p.Hooks.ByEvent() {
		for _, h := range hooks {
			out = append(out, fmt.Sprintf("runs code: %s hook: %s", event, h.Describe()))
		}
	}
	sort.Strings(out)
	for _, s := range p.MCP {
		what := s.URL
		if s.Command != "" {
			what = "runs code: " + strings.TrimSpace(s.Command+" "+strings.Join(s.Args, " "))
		}
		out = append(out, fmt.Sprintf("MCP server %s: %s", s.Name, what))
	}
	for _, s := range p.Skills {
		out = append(out, "skill: "+s+" (its scripts run code when used)")
	}
	for _, c := range p.Commands {
		out = append(out, "command: /"+c)
	}
	for _, a := range p.Agents {
		out = append(out, "agent: "+a)
	}
	return out
}

// RunsCode reports whether the plugin brings hooks or MCP commands.
func (p *Plugin) RunsCode() bool {
	if len(p.Hooks.All()) > 0 {
		return true
	}
	return slices.ContainsFunc(p.MCP, func(s config.MCPServerConfig) bool { return s.Command != "" })
}

func mdNames(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	return out
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// maxBytes bounds a plugin's size.
const maxBytes = 64 << 20

// Hash is the SHA-256 of every regular file under dir, in path order, each
// as its relative path, size and content (as workers and skills are
// pinned). Symbolic links are skipped.
func Hash(dir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	var total int64
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return "", err
		}
		if total += info.Size(); total > maxBytes {
			return "", fmt.Errorf("the plugin is larger than %d MB", maxBytes>>20)
		}
		f, err := os.Open(p)
		if err != nil {
			return "", err
		}
		rel, _ := filepath.Rel(dir, p)
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), info.Size())
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
