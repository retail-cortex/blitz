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
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
)

// Configuring Blitz by asking the agent (spec_067_cli_parity CLP-30–35):
// an MCP server set up from its docs page with its env, headers and
// secrets, the settings the agent may change, and what they are.

// placeholder is a secret named in a value the agent gives: ${NAME}, or
// Blitz's own {secrets:NAME}.
var placeholder = regexp.MustCompile(`\$\{([A-Za-z0-9_.-]{1,64})\}|\{secrets:(?:(?:global|workspace):)?([A-Za-z0-9_.-]{1,64})\}`)

// secretLike are env and header names whose values are secrets whatever
// the agent writes for them.
var secretLike = regexp.MustCompile(`(?i)(token|secret|password|passwd|api[_-]?key|apikey|authorization|credential|private[_-]?key|bearer)`)

// secretName is a secret's name made from a server's and a variable's:
// GITHUB_TOKEN from github and TOKEN, MY_SERVER_AUTHORIZATION from
// my-server and Authorization.
func secretName(server, key string) string {
	clean := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToUpper(s) {
			switch {
			case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
				b.WriteRune(r)
			default:
				b.WriteByte('_')
			}
		}
		return strings.Trim(b.String(), "_")
	}
	k, srv := clean(key), clean(server)
	if strings.HasPrefix(k, srv+"_") || k == srv {
		return k
	}
	return srv + "_" + k
}

// mcpValues are an MCP server's env or header values as written: every
// secret as {secrets:NAME}, the secrets they name, and the names whose
// literal value was dropped (a secret is never taken as an argument:
// DRV-05).
func mcpValues(server string, in map[string]any) (out map[string]string, names, dropped []string) {
	if len(in) == 0 {
		return nil, nil, nil
	}
	out = map[string]string{}
	for _, k := range slices.Sorted(maps.Keys(in)) {
		v := fmt.Sprint(in[k])
		if placeholder.MatchString(v) {
			out[k] = placeholder.ReplaceAllStringFunc(v, func(m string) string {
				sub := placeholder.FindStringSubmatch(m)
				name := sub[1] + sub[2]
				names = append(names, name)
				return "{secrets:" + name + "}"
			})
			continue
		}
		if secretLike.MatchString(k) && v != "" {
			name := secretName(server, k)
			names = append(names, name)
			dropped = append(dropped, k)
			prefix := ""
			if strings.EqualFold(k, "Authorization") && strings.HasPrefix(strings.ToLower(v), "bearer ") {
				prefix = v[:7]
			}
			out[k] = prefix + "{secrets:" + name + "}"
			continue
		}
		out[k] = v
	}
	slices.Sort(names)
	return out, slices.Compact(names), dropped
}

// mcpArgs is the MCP server an mcp add or edit describes, the secrets it
// needs, and the fields dropped as secret.
func mcpArgs(args map[string]any) (config.MCPServerConfig, []string, []string, error) {
	s := config.MCPServerConfig{
		Name: str(args, "name"), Command: str(args, "command"), Args: list(args, "args"), URL: str(args, "url"),
		Prefix: str(args, "prefix"), Agents: list(args, "agents"), Tools: list(args, "tools"),
	}
	if !validMCPName(s.Name) {
		return s, nil, nil, fmt.Errorf("an MCP server's name is letters, digits, '.', '-' and '_', not %q", s.Name)
	}
	if (s.Command == "") == (s.URL == "") {
		return s, nil, nil, errors.New("give either command (with args) or url")
	}
	if t, ok := args["timeout_seconds"].(float64); ok && t > 0 {
		s.TimeoutSeconds = int(t)
	}
	env, _ := args["env"].(map[string]any)
	headers, _ := args["headers"].(map[string]any)
	if len(headers) > 0 && s.URL == "" {
		return s, nil, nil, errors.New("headers are for a server at an address (url); a command's server takes env")
	}
	var names, dropped []string
	var n, d []string
	s.Env, n, d = mcpValues(s.Name, env)
	names, dropped = append(names, n...), append(dropped, d...)
	s.Headers, n, d = mcpValues(s.Name, headers)
	names, dropped = append(names, n...), append(dropped, d...)
	slices.Sort(names)
	return s, slices.Compact(names), dropped, nil
}

var mcpNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func validMCPName(name string) bool { return mcpNameRE.MatchString(name) }

// ownMCP is the workspace's own MCP server named name, and the file it's
// in (false when it has none of that name).
func (w *Workspace) ownMCP(name string) (config.MCPServerConfig, string, bool, error) {
	own, _, path, err := config.ScopeMCP(w.cfg.Dir, w.Dir())
	if err != nil {
		return config.MCPServerConfig{}, "", false, err
	}
	i := slices.IndexFunc(own, func(s config.MCPServerConfig) bool { return s.Name == name })
	if i < 0 {
		return config.MCPServerConfig{}, path, false, nil
	}
	return own[i], path, true, nil
}

// cmdMCPSave is mcp add and mcp edit: an MCP server of this workspace, set
// up from what the agent read (its docs page, say), asked with its
// settings in full and where they came from, always (DRV-75); its secrets
// are asked of the person after, hidden, and kept as Blitz secrets the
// settings name ({secrets:NAME}). Refused where the organization's policy
// keeps people's own servers out (DRV-72).
func (w *Workspace) cmdMCPSave(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	if o := w.settings().Org; o.Active() && !o.Policy.MCP.Own {
		return nil, errors.New("your organization doesn't allow MCP servers of your own")
	}
	s, names, dropped, err := mcpArgs(args)
	if err != nil {
		return nil, err
	}
	old, path, exists, err := w.ownMCP(s.Name)
	if err != nil {
		return nil, err
	}
	edit := c.Name == "mcp edit"
	switch {
	case edit && !exists:
		return nil, fmt.Errorf("this workspace has no MCP server %q of its own to change (mcp list; mcp add adds one)", s.Name)
	case !edit && exists:
		return nil, fmt.Errorf("this workspace already has an MCP server %q: mcp edit changes it", s.Name)
	case !edit && slices.ContainsFunc(w.ListMCPServers(), func(m api.MCPServer) bool { return m.Name == s.Name }):
		return nil, fmt.Errorf("an MCP server %q comes from your global settings or the project: give this one another name", s.Name)
	}
	if edit {
		s.Disabled = old.Disabled
	}
	scope := config.SecretScope(w.cfg.Dir, w.Dir())
	vault := config.Vault(w.cfg.Dir)
	var ask []string // the secrets not kept yet
	for _, n := range names {
		if _, ok := vault.Lookup(scope, n); !ok {
			ask = append(ask, n)
		}
	}
	what := "connects to " + s.URL
	if s.Command != "" {
		what = "runs: " + strings.Join(append([]string{s.Command}, s.Args...), " ")
	}
	verb := "Add"
	if edit {
		verb = "Change"
	}
	detail := fmt.Sprintf("%s this workspace's MCP server %s, which %s", verb, s.Name, what)
	if src := str(args, "source"); src != "" {
		detail += "; its settings are from " + src
	}
	if len(ask) > 0 {
		detail += "; then you're asked for " + strings.Join(ask, ", ") + ", kept as Blitz secrets"
	}
	rel := mcpFileShown(path)
	oldText := ""
	if edit {
		oldText = config.MCPServerTOML(old)
	}
	if err := w.askChange(ctx, c, detail, fileDiff(rel, oldText, config.MCPServerTOML(s)), ask...); err != nil {
		return nil, err
	}
	for _, n := range ask {
		v, err := w.askSecret(ctx, n, n+" for the MCP server "+s.Name)
		if err != nil {
			return nil, err
		}
		if err := vault.Set(scope, n, v); err != nil {
			return nil, err
		}
	}
	if edit {
		path, err = config.ReplaceMCPServer(w.cfg.Dir, w.Dir(), s.Name, s)
	} else {
		path, err = config.AddMCPServer(w.cfg.Dir, w.Dir(), s)
	}
	if err != nil {
		return nil, err
	}
	if err := w.reloadMCPFromSettings(ctx); err != nil {
		return nil, err
	}
	out := map[string]any{"file": path, "next": "mcp test checks it; the agents have its tools from the next turn"}
	if edit {
		out["changed"] = s.Name
	} else {
		out["added"] = s.Name
	}
	if len(names) > 0 {
		out["secrets"] = names
	}
	if len(dropped) > 0 {
		out["note"] = "secret values aren't taken as arguments: " + strings.Join(dropped, ", ") + " were asked of the person"
	}
	if s.URL != "" && len(s.Headers) == 0 {
		out["sign_in"] = "if it signs in with OAuth, the person runs: blitz mcp login " + s.Name
	}
	if tools, err := w.tools.MCP().ToolNames(ctx, s.Name); err != nil {
		out["test"] = "it didn't answer yet: " + err.Error()
	} else {
		out["tools"] = tools
	}
	return out, nil
}

// mcpFileShown is where a workspace's MCP servers are kept, as an approval
// shows it: under ~ when it's there.
func mcpFileShown(path string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(filepath.Join("~", rel))
		}
	}
	return filepath.ToSlash(path)
}

// cmdMCPRemove removes an MCP server of the workspace's own, asked with
// what goes; its secrets stay, for the person to remove (secret list).
func (w *Workspace) cmdMCPRemove(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	old, path, exists, err := w.ownMCP(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("this workspace has no MCP server %q of its own (mcp list): a global one is removed in Settings, or with blitz mcp remove", name)
	}
	if err := w.askChange(ctx, c, "Remove this workspace's MCP server "+name, fileDiff(mcpFileShown(path), config.MCPServerTOML(old), "")); err != nil {
		return nil, err
	}
	if _, err := config.RemoveMCPServer(w.cfg.Dir, w.Dir(), name); err != nil {
		return nil, err
	}
	return map[string]any{"removed": name}, w.reloadMCPFromSettings(ctx)
}

// cmdMCPTest connects to an MCP server and lists its tools.
func (w *Workspace) cmdMCPTest(ctx context.Context, _ api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	if !slices.ContainsFunc(w.ListMCPServers(), func(m api.MCPServer) bool { return m.Name == name }) {
		return nil, fmt.Errorf("there's no MCP server %q (mcp list)", name)
	}
	tools, err := w.tools.MCP().ToolNames(ctx, name)
	if err != nil {
		return map[string]any{"name": name, "ok": false, "error": err.Error()}, nil
	}
	return map[string]any{"name": name, "ok": true, "tools": tools}, nil
}

// agentSettings are the settings keys settings set may change for the
// agent (CLP-32): how Blitz looks and searches and which models it uses.
// Not what loosens its limits (the mode, agency, jobs' policy), where its
// requests go (base URLs, auth, projects), where it writes (folders), or
// the person's own (profile.*): the person sets those.
func agentSettable(key string) error {
	switch {
	case strings.HasPrefix(key, "ui."), strings.HasPrefix(key, "search."),
		key == "llm.provider", key == "llm.providers", key == "blitz.default_model", key == "blitz.default_agent",
		key == "audio.model", key == "audio.voice", key == "images.model", key == "health.model",
		strings.HasPrefix(key, "llm.") && (strings.HasSuffix(key, ".model") || strings.HasSuffix(key, ".models")):
		return nil
	}
	return fmt.Errorf("%s is the person's to change: they run blitz config set %s <value>, or use Settings (settings reference says what it does)", key, key)
}

// cmdSettingsReference is what settings there are: with a key or the
// start of some, each one's type, default and what it does, and whether
// settings set changes it; with none, the sections.
func (w *Workspace) cmdSettingsReference(_ context.Context, _ api.BlitzCommand, args map[string]any) (any, error) {
	key := strings.TrimSpace(str(args, "key"))
	ref := config.Reference()
	if key == "" {
		count := map[string]int{}
		for _, s := range ref {
			section, _, _ := strings.Cut(s.Key, ".")
			count[section]++
		}
		sections := slices.Collect(maps.Keys(count))
		sort.Strings(sections)
		out := make([]map[string]any, 0, len(sections))
		for _, sec := range sections {
			out = append(out, map[string]any{"section": sec, "keys": count[sec]})
		}
		return map[string]any{"sections": out, "next": "settings reference with a section (ui) or key (ui.theme)"}, nil
	}
	var out []map[string]any
	for _, s := range ref {
		if s.Key != key && !strings.HasPrefix(s.Key, strings.TrimSuffix(key, ".")+".") {
			continue
		}
		item := map[string]any{"key": s.Key, "type": s.Type, "doc": s.Doc, "agent_can_set": agentSettable(s.Key) == nil && config.Settable(s.Key)}
		if s.Default != "" {
			item["default"] = s.Default
		}
		out = append(out, item)
		if len(out) == 60 {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no setting is %q (settings reference lists the sections)", key)
	}
	return out, nil
}
