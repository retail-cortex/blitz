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

package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/retail-cortex/blitz/pkg/config"
	"gopkg.in/yaml.v3"
)

// Importers turn another agent's plugin or extension into a Blitz plugin
// (spec_parity_027 PAR-PLG-04): what maps is converted, what doesn't is
// reported.

// Report is what an import did.
type Report struct {
	Plugin  Meta
	Out     string
	Added   []string // what the plugin now has
	Skipped []string // what didn't map, and why
}

// claudeTools maps Claude Code's tool names to Blitz's.
var claudeTools = map[string]string{
	"Bash": "run_shell_command", "Read": "read_file", "Write": "create_file", "Edit": "replace_in_file",
	"MultiEdit": "replace_in_file", "Glob": "glob", "Grep": "grep", "LS": "list_files", "WebFetch": "web_fetch",
	"WebSearch": "web_search", "Task": "invoke_agent", "TodoWrite": "todo", "NotebookEdit": "replace_in_file",
}

// claudeEvents maps Claude Code's hook events to Blitz's.
var claudeEvents = map[string]string{
	"PreToolUse": "pre_tool", "PostToolUse": "post_tool", "UserPromptSubmit": "prompt_submit",
	"SessionStart": "session_start", "SessionEnd": "session_end", "Stop": "stop", "SubagentStop": "subagent_stop",
	"PreCompact": "pre_compact", "Notification": "notification", "PostToolUseFailure": "post_tool_failure",
	"SubagentStart": "subagent_start", "PermissionRequest": "permission_request",
}

// ImportClaude converts the Claude Code plugin in src (.claude-plugin/
// plugin.json, commands/, agents/, skills/, hooks/hooks.json, .mcp.json)
// into a Blitz plugin in out.
func ImportClaude(src, out string) (*Report, error) {
	var manifest struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Description string `json:"description"`
		Author      struct {
			Name string `json:"name"`
		} `json:"author"`
		Homepage string `json:"homepage"`
	}
	if err := readJSON(filepath.Join(src, ".claude-plugin", "plugin.json"), &manifest); err != nil {
		return nil, fmt.Errorf("not a Claude Code plugin: %w", err)
	}
	r := &Report{Plugin: Meta{Name: slug(manifest.Name), Version: manifest.Version, Description: manifest.Description, Author: manifest.Author.Name, Homepage: manifest.Homepage}, Out: out}
	if err := r.start(); err != nil {
		return nil, err
	}
	r.copyDir(src, "commands", CommandsDir, rootVars)
	r.copySkills(src)
	r.convertAgents(filepath.Join(src, "agents"))

	var hooks struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := readJSON(filepath.Join(src, "hooks", "hooks.json"), &hooks); err == nil {
		var h config.HooksConfig
		byEvent := map[string]*[]config.HookConfig{
			"pre_tool": &h.PreTool, "post_tool": &h.PostTool, "prompt_submit": &h.PromptSubmit, "session_start": &h.SessionStart,
			"session_end": &h.SessionEnd, "stop": &h.Stop, "subagent_stop": &h.SubagentStop, "pre_compact": &h.PreCompact,
			"notification": &h.Notification, "post_tool_failure": &h.PostToolFailure, "subagent_start": &h.SubagentStart,
			"permission_request": &h.PermissionRequest,
		}
		events := sortedKeysOf(hooks.Hooks)
		for _, ev := range events {
			event, ok := claudeEvents[ev]
			if !ok {
				r.Skipped = append(r.Skipped, "hook event "+ev+": Blitz has no such event")
				continue
			}
			for _, m := range hooks.Hooks[ev] {
				matches, unmapped := toolMatches(m.Matcher)
				if len(unmapped) > 0 {
					r.Skipped = append(r.Skipped, fmt.Sprintf("%s hook matcher %q: no Blitz tool for %s", ev, m.Matcher, strings.Join(unmapped, ", ")))
					if len(matches) == 0 {
						continue
					}
				}
				for _, hk := range m.Hooks {
					if hk.Type != "" && hk.Type != "command" {
						r.Skipped = append(r.Skipped, fmt.Sprintf("%s hook of type %s", ev, hk.Type))
						continue
					}
					for _, match := range matches {
						*byEvent[event] = append(*byEvent[event], config.HookConfig{Match: match, Command: rootVars(hk.Command), TimeoutSeconds: hk.Timeout})
						r.Added = append(r.Added, fmt.Sprintf("%s hook: %s", event, hk.Command))
					}
				}
			}
		}
		if err := writeTOML(filepath.Join(out, HooksFile), h); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		r.Skipped = append(r.Skipped, "hooks/hooks.json: "+err.Error())
	}

	var mcp struct {
		Servers map[string]struct {
			Type    string            `json:"type"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := readJSON(filepath.Join(src, ".mcp.json"), &mcp); err == nil {
		var servers []config.MCPServerConfig
		for _, name := range sortedKeysOf(mcp.Servers) {
			s := mcp.Servers[name]
			if s.Type == "sse" {
				r.Skipped = append(r.Skipped, "MCP server "+name+": SSE transport (Blitz speaks stdio and streamable HTTP)")
				continue
			}
			servers = append(servers, config.MCPServerConfig{
				Name: name, Command: rootVars(s.Command), Args: rootVarsAll(s.Args), Env: rootVarsMap(s.Env), URL: s.URL, Headers: s.Headers,
			})
			r.Added = append(r.Added, "MCP server: "+name)
		}
		if err := writeTOML(filepath.Join(out, MCPFile), config.MCPConfig{Servers: servers}); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		r.Skipped = append(r.Skipped, ".mcp.json: "+err.Error())
	}
	for _, other := range []string{"output-styles", "lsp", ".lsp.json", "settings.json"} {
		if exists(filepath.Join(src, other)) {
			r.Skipped = append(r.Skipped, other+": not supported by Blitz plugins")
		}
	}
	return r, r.finish()
}

// ImportGemini converts the Gemini CLI extension in src
// (gemini-extension.json, commands/**/*.toml) into a Blitz plugin in out.
func ImportGemini(src, out string) (*Report, error) {
	var manifest struct {
		Name            string   `json:"name"`
		Version         string   `json:"version"`
		Description     string   `json:"description"`
		ContextFileName any      `json:"contextFileName"`
		ExcludeTools    []string `json:"excludeTools"`
		MCPServers      map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			Cwd     string            `json:"cwd"`
			URL     string            `json:"url"`
			HTTPURL string            `json:"httpUrl"`
			Headers map[string]string `json:"headers"`
			Timeout int               `json:"timeout"`
		} `json:"mcpServers"`
	}
	if err := readJSON(filepath.Join(src, "gemini-extension.json"), &manifest); err != nil {
		return nil, fmt.Errorf("not a Gemini CLI extension: %w", err)
	}
	r := &Report{Plugin: Meta{Name: slug(manifest.Name), Version: manifest.Version, Description: manifest.Description}, Out: out}
	if err := r.start(); err != nil {
		return nil, err
	}
	var servers []config.MCPServerConfig
	for _, name := range sortedKeysOf(manifest.MCPServers) {
		s := manifest.MCPServers[name]
		url := s.HTTPURL
		if url == "" && s.URL != "" {
			r.Skipped = append(r.Skipped, "MCP server "+name+": SSE transport (Blitz speaks stdio and streamable HTTP)")
			continue
		}
		if s.Cwd != "" {
			r.Skipped = append(r.Skipped, "MCP server "+name+": cwd (it runs in the workspace)")
		}
		servers = append(servers, config.MCPServerConfig{
			Name: name, Command: geminiVars(s.Command), Args: geminiVarsAll(s.Args), Env: s.Env, URL: url, Headers: s.Headers,
			TimeoutSeconds: s.Timeout / 1000,
		})
		r.Added = append(r.Added, "MCP server: "+name)
	}
	if len(servers) > 0 {
		if err := writeTOML(filepath.Join(out, MCPFile), config.MCPConfig{Servers: servers}); err != nil {
			return nil, err
		}
	}
	// commands/a/b.toml is /a:b in Gemini; here a-b.
	root := filepath.Join(src, "commands")
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".toml") {
			return nil
		}
		var c struct {
			Description string `toml:"description"`
			Prompt      string `toml:"prompt"`
		}
		rel, _ := filepath.Rel(root, p)
		name := slug(strings.ReplaceAll(strings.TrimSuffix(filepath.ToSlash(rel), ".toml"), "/", "-"))
		if _, err := toml.DecodeFile(p, &c); err != nil || strings.TrimSpace(c.Prompt) == "" {
			r.Skipped = append(r.Skipped, "command "+rel+": no prompt")
			return nil
		}
		body := strings.ReplaceAll(c.Prompt, "{{args}}", "$ARGUMENTS")
		if regexp.MustCompile(`!\{|@\{`).MatchString(body) {
			r.Skipped = append(r.Skipped, "command "+rel+": !{…} and @{…} are kept as text (Blitz doesn't run them)")
		}
		md := "---\ndescription: " + yamlString(c.Description) + "\n---\n" + body + "\n"
		if err := write(filepath.Join(out, CommandsDir, name+".md"), md); err == nil {
			r.Added = append(r.Added, "command: /"+name)
		}
		return nil
	})
	if manifest.ContextFileName != nil || exists(filepath.Join(src, "GEMINI.md")) {
		r.Skipped = append(r.Skipped, "context file: plugins don't add instructions (put it in the project's AGENTS.md or BLITZ.md)")
	}
	if len(manifest.ExcludeTools) > 0 {
		r.Skipped = append(r.Skipped, "excludeTools: use deny rules in your settings instead")
	}
	return r, r.finish()
}

func (r *Report) start() error {
	if !validName.MatchString(r.Plugin.Name) {
		return fmt.Errorf("the plugin's name %q can't be a Blitz plugin's", r.Plugin.Name)
	}
	if r.Plugin.Version == "" {
		r.Plugin.Version = "0.0.0"
	}
	if exists(r.Out) {
		if entries, _ := os.ReadDir(r.Out); len(entries) > 0 {
			return fmt.Errorf("%s exists and isn't empty", r.Out)
		}
	}
	return os.MkdirAll(r.Out, 0o755)
}

func (r *Report) finish() error {
	if err := writeTOML(filepath.Join(r.Out, Manifest), r.Plugin); err != nil {
		return err
	}
	sort.Strings(r.Skipped)
	_, err := Read(r.Out) // what it made must load
	return err
}

// copyDir copies src/from/*.md to out/to, rewriting each with fix.
func (r *Report) copyDir(src, from, to string, fix func(string) string) {
	for _, name := range mdNames(filepath.Join(src, from)) {
		data, err := os.ReadFile(filepath.Join(src, from, name+".md"))
		if err != nil {
			continue
		}
		if err := write(filepath.Join(r.Out, to, name+".md"), fix(string(data))); err == nil {
			r.Added = append(r.Added, strings.TrimSuffix(to, "s")+": "+name)
		}
	}
}

func (r *Report) copySkills(src string) {
	entries, _ := os.ReadDir(filepath.Join(src, "skills"))
	for _, e := range entries {
		dir := filepath.Join(src, "skills", e.Name())
		if !e.IsDir() || !exists(filepath.Join(dir, "SKILL.md")) {
			continue
		}
		if err := copyTree(dir, filepath.Join(r.Out, SkillsDir, e.Name())); err == nil {
			r.Added = append(r.Added, "skill: "+e.Name())
		}
	}
}

// convertAgents rewrites Claude Code subagents' frontmatter as Blitz's:
// tools mapped to Blitz's names; model and colour dropped.
func (r *Report) convertAgents(dir string) {
	for _, name := range mdNames(dir) {
		data, err := os.ReadFile(filepath.Join(dir, name+".md"))
		if err != nil {
			continue
		}
		head, body, ok := splitFrontmatter(data)
		var fm map[string]any
		if !ok || yaml.Unmarshal(head, &fm) != nil {
			r.Skipped = append(r.Skipped, "agent "+name+": no frontmatter")
			continue
		}
		meta := map[string]any{"name": fm["name"], "description": fm["description"]}
		if meta["name"] == nil {
			meta["name"] = name
		}
		if tools, ok := fm["tools"]; ok {
			var list []string
			switch t := tools.(type) {
			case string:
				list = strings.Split(t, ",")
			case []any:
				for _, v := range t {
					list = append(list, fmt.Sprint(v))
				}
			}
			mapped, unmapped := mapTools(list)
			meta["tools"] = mapped
			if len(unmapped) > 0 {
				r.Skipped = append(r.Skipped, fmt.Sprintf("agent %s: no Blitz tool for %s", name, strings.Join(unmapped, ", ")))
			}
		}
		if m, ok := fm["model"].(string); ok && m != "" && m != "inherit" {
			r.Skipped = append(r.Skipped, fmt.Sprintf("agent %s: model %q (pin one with /pin_model)", name, m))
		}
		out, _ := yaml.Marshal(meta)
		if err := write(filepath.Join(r.Out, AgentsDir, name+".md"), "---\n"+string(out)+"---\n"+rootVars(string(body))); err == nil {
			r.Added = append(r.Added, "agent: "+name)
		}
	}
}

// toolMatches turns a Claude Code matcher ("Edit|Write", "*", "") into
// Blitz tool-name globs, and names what doesn't map.
func toolMatches(matcher string) (matches, unmapped []string) {
	m := strings.TrimSpace(matcher)
	if m == "" || m == "*" || m == ".*" {
		return []string{""}, nil
	}
	parts := strings.Split(m, "|")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "mcp__") {
			matches = append(matches, strings.ReplaceAll(strings.TrimPrefix(p, "mcp__"), ".*", "*"))
			continue
		}
		if t, ok := claudeTools[p]; ok {
			if !contains(matches, t) {
				matches = append(matches, t)
			}
			continue
		}
		unmapped = append(unmapped, p)
	}
	return matches, unmapped
}

func mapTools(list []string) (mapped, unmapped []string) {
	for _, t := range list {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		base, _, _ := strings.Cut(t, "(")
		if b, ok := claudeTools[base]; ok {
			if !contains(mapped, b) {
				mapped = append(mapped, b)
			}
		} else {
			unmapped = append(unmapped, t)
		}
	}
	return mapped, unmapped
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func splitFrontmatter(data []byte) (head, body []byte, ok bool) {
	rest, found := bytes.CutPrefix(bytes.TrimLeft(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), "\n"), []byte("---\n"))
	if !found {
		return nil, data, false
	}
	head, body, ok = bytes.Cut(rest, []byte("\n---"))
	if !ok {
		return nil, data, false
	}
	return head, bytes.TrimLeft(bytes.TrimPrefix(body, []byte("\n")), "\n"), true
}

func rootVars(s string) string { return strings.ReplaceAll(s, "${CLAUDE_PLUGIN_ROOT}", RootVar) }

func rootVarsAll(list []string) []string {
	var out []string
	for _, v := range list {
		out = append(out, rootVars(v))
	}
	return out
}

func rootVarsMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := map[string]string{}
	for k, v := range m {
		out[k] = rootVars(v)
	}
	return out
}

func geminiVars(s string) string {
	return strings.ReplaceAll(s, "${extensionPath}", RootVar)
}

func geminiVarsAll(list []string) []string {
	var out []string
	for _, v := range list {
		out = append(out, geminiVars(v))
	}
	return out
}

var slugChars = regexp.MustCompile(`[^a-z0-9._-]+`)

func slug(s string) string {
	return strings.Trim(slugChars.ReplaceAllString(strings.ToLower(s), "-"), "-.")
}

func yamlString(s string) string {
	b, _ := json.Marshal(s) // a JSON string is a YAML string
	return string(b)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func write(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func writeTOML(path string, v any) error {
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(v); err != nil {
		return err
	}
	return write(path, b.String())
}

func sortedKeysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
