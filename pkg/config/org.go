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
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// OrgLayer is the organization's catalog on this computer, as the Blitz
// service last fetched and checked it (spec_053_enterprise EN-30 to
// EN-35): its default items, which a workspace or the person may
// override, its locked ones, which apply over theirs, and its policy.
// Never read from a file: it's set as the settings load, from
// [enterprise] url's folder.
type OrgLayer struct {
	// Defaults and Locked are the layers' folders, laid out as a
	// workspace's .agents folder; "" when there's no catalog.
	Defaults, Locked string
	// PolicyDir holds the rules and guardrails the person's policy
	// switches on (rules/, guardrails/), which apply as locked items do
	// (EN-38); "" when there's no organization.
	PolicyDir string
	Policy    OrgPolicy
	// Explain says, by setting ("connectors.own"), which level of the
	// organization's policy decided it (EN-39); empty from a service
	// before /api/v1/policy.
	Explain map[string]PolicyDecision
	// Expires is when Blitz stops working under it unrenewed (EN-33).
	Expires time.Time
	// Problem is why the organization's policy couldn't be read ("" when
	// it was): a service is set but its policy.json is missing or
	// damaged. Blitz then works under the strictest policy (nothing of
	// the person's own allowed), and the organization's work waits, as
	// past an expiry (EN-33).
	Problem string
}

// LockOf is how the catalog applies the item at path: "locked" or
// "default" for one of its files, "" for any other.
func (o OrgLayer) LockOf(path string) string {
	switch {
	case !o.Active() || path == "":
		return ""
	case strings.HasPrefix(path, o.Locked+string(filepath.Separator)),
		o.PolicyDir != "" && strings.HasPrefix(path, o.PolicyDir+string(filepath.Separator)):
		return "locked"
	case strings.HasPrefix(path, o.Defaults+string(filepath.Separator)):
		return "default"
	}
	return ""
}

// PluginsOff reports whether the person's own plugins (installed, from a
// marketplace or a folder) don't load: the organization's policy says
// [plugins] own = false, or it couldn't be read (EN-35).
func (o OrgLayer) PluginsOff() bool {
	own := o.Policy.Plugins.Own
	return o.Active() && (o.Problem != "" || own != nil && !*own)
}

// Active reports whether a catalog applies.
func (o OrgLayer) Active() bool { return o.Defaults != "" || o.Locked != "" || o.PolicyDir != "" }

// PolicyDecision is how the organization decided one setting of the
// person's policy (the service's spec_012_policy PO-11): its value, the
// level (default, global, group, person), who (groups' names, the
// person's email), and whether the organization locked it.
type PolicyDecision struct {
	Value  any      `json:"value"`
	Level  string   `json:"level"`
	By     []string `json:"by,omitempty"`
	Locked bool     `json:"locked,omitempty"`
}

// Why says who decided setting path, for a refusal: "the organization's
// policy", "the policy for Data science", "your own policy", the default;
// "the organization's policy" when the service didn't say.
func (o OrgLayer) Why(path string) string {
	d, ok := o.Explain[path]
	if !ok {
		return "the organization's policy"
	}
	switch d.Level {
	case "group":
		return "the policy for " + strings.Join(d.By, ", ")
	case "person":
		return "the policy an admin set for you"
	case "default":
		return "the organization's policy (its default)"
	}
	return "the organization's policy"
}

// OwnProvider is a provider the organization lets people use themselves.
type OwnProvider struct {
	Allowed bool `json:"allowed"`
	// Models are the models allowed, as patterns ("llama3*"); every model
	// when none.
	Models []string `json:"models,omitempty"`
}

// OwnProvider says whether the policy lets people use provider (one of
// Providers) themselves, and which of its models (every one when none,
// EN-35): by provider from a service that says (Models.Own), else
// own_providers for every one.
func (p OrgPolicy) OwnProvider(provider string) (bool, []string) {
	if o, ok := p.Models.Own[provider]; ok {
		return o.Allowed, o.Models
	}
	return p.Models.OwnProviders, nil
}

// AnyOwnProvider reports whether the policy lets people use any provider
// of their own.
func (p OrgPolicy) AnyOwnProvider() bool {
	if p.Models.OwnProviders {
		return true
	}
	for _, o := range p.Models.Own {
		if o.Allowed {
			return true
		}
	}
	return false
}

// ModelAllowed reports whether model matches one of patterns (every
// model when there are none): shell-style globs, such as "llama3*" or
// "qwen2.5-coder:*".
func ModelAllowed(model string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pat := range patterns {
		if ok, _ := path.Match(pat, model); ok {
			return true
		}
	}
	return false
}

// Expired reports whether the catalog is past its expiry at now, or its
// policy couldn't be read: the organization's work waits until it's
// fetched again (EN-33).
func (o OrgLayer) Expired(now time.Time) bool {
	return o.Active() && (o.Problem != "" || !o.Expires.IsZero() && now.After(o.Expires))
}

// OrgPolicy is the organization's policy as Blitz applies it (EN-35):
// what pkg/enterprise decodes from the signed manifest and writes to
// policy.json, read back here.
type OrgPolicy struct {
	Models struct {
		// OwnProviders lets people use their own providers beside the
		// organization's models: every one, as the service said before
		// Own (and still says when every one is allowed whole).
		OwnProviders bool `json:"own_providers"`
		// Own says by provider which ones people may use and which of
		// their models (the service's spec_012_policy PO-21).
		Own map[string]OwnProvider `json:"own,omitempty"`
	} `json:"models"`
	Tools struct {
		// Off are tools no agent may use.
		Off []string `json:"off"`
		// ShellCommands lets people run their own ! commands.
		ShellCommands bool `json:"shell_commands"`
	} `json:"tools"`
	MCP struct {
		// Own lets people add their own MCP servers.
		Own bool `json:"own"`
	} `json:"mcp"`
	// Connectors says whether people may have connections of their own,
	// and which kinds anyone's may be (every kind when none).
	Connectors struct {
		Own   bool     `json:"own"`
		Kinds []string `json:"kinds"`
	} `json:"connectors"`
	// Plugins says whether people's own plugins load (allowed when the
	// policy doesn't say, as from a service that predates it).
	Plugins struct {
		Own *bool `json:"own,omitempty"`
	} `json:"plugins"`
	Builtins struct {
		// Off are Blitz's built-in agents, skills and guardrails switched
		// off, by name.
		Off []string `json:"off"`
	} `json:"builtins"`
}

// EnterpriseDir is the organization's folder for the service at raw
// (EN-54): <settings>/enterprise/<the service's host>, so another's never
// mixes in; "" for no service.
func EnterpriseDir(prefixDir, raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return filepath.Join(ConfigDir(prefixDir), "enterprise", strings.ReplaceAll(u.Host, ":", "_"))
}

// orgState is policy.json, as the service writes it beside the catalog.
type orgState struct {
	Policy  OrgPolicy                 `json:"policy"`
	Explain map[string]PolicyDecision `json:"explain,omitempty"`
	Expires time.Time                 `json:"expires"`
}

// applyOrg puts the organization's catalog in cfg, when the service has
// one on this computer: its layers, its policy, and its MCP servers, the
// policy's tools denied for every agent.
func applyOrg(cfg *Config, prefixDir string) {
	orgLayers.Delete(ConfigDir(prefixDir))
	dir := EnterpriseDir(prefixDir, cfg.Enterprise.URL)
	if dir == "" {
		return
	}
	layers := filepath.Join(dir, "catalog")
	cfg.Org = OrgLayer{Defaults: filepath.Join(layers, "default"), Locked: filepath.Join(layers, "locked"), PolicyDir: filepath.Join(dir, "policy")}
	path := filepath.Join(dir, "policy.json")
	var st orgState
	raw, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(raw, &st)
	}
	if err != nil {
		// Never without the organization's rules (EN-33): with no policy
		// to go by, the strictest one (the zero policy allows nothing of
		// the person's own), and its work waits.
		st = orgState{}
		cfg.Org.Problem = err.Error()
		if errors.Is(err, os.ErrNotExist) {
			cfg.Org.Problem = "not fetched yet"
		}
		cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("your organization's policy (%s) can't be read (%s): nothing of your own (providers, MCP servers, plugins, connections, ! commands) is allowed, and its work waits until Blitz fetches it again; sign in under Settings › Providers › Organization or with blitz enterprise login", path, cfg.Org.Problem))
	}
	cfg.Org.Policy, cfg.Org.Explain, cfg.Org.Expires = st.Policy, st.Explain, st.Expires
	orgLayers.Store(ConfigDir(prefixDir), cfg.Org)
	for _, tool := range st.Policy.Tools.Off {
		if !slices.Contains(cfg.Permissions.Deny, tool) {
			cfg.Permissions.Deny = append(cfg.Permissions.Deny, tool)
		}
	}
	var problems []string
	cfg.MCP.Servers, problems = orgMCP(cfg.MCP.Servers, cfg.Org)
	cfg.Warnings = append(cfg.Warnings, problems...)
}

// ErrOrgMCP is why an MCP server added after the settings load isn't
// used: the organization's policy doesn't allow MCP servers of the
// person's own (EN-35).
var ErrOrgMCP = errors.New("your organization's policy doesn't allow MCP servers of your own")

// ErrOrgPlugins is why the person's own plugins aren't loaded: the
// organization's policy turns them off (EN-35).
var ErrOrgPlugins = errors.New("your organization turns off your own plugins")

// ErrMCPNameTaken is why an MCP server added after the settings load isn't
// used: one of its name is there already, and that one stays.
var ErrMCPNameTaken = errors.New("an MCP server of that name exists already")

// AddMCPServer adds s to the MCP servers from a source that comes after
// the settings load, and so after the organization's policy applies (a
// trusted project's, a plugin's): every such source goes through here, so
// the policy holds for them all (EN-35). It's refused with ErrOrgMCP when
// the organization doesn't allow the person's own servers, and with
// ErrMCPNameTaken when one of its name is there already.
func (c *Config) AddMCPServer(s MCPServerConfig) error {
	if c.Org.Active() && !c.Org.Policy.MCP.Own {
		return fmt.Errorf("%w (%s decides it)", ErrOrgMCP, c.Org.Why("mcp.own"))
	}
	if slices.ContainsFunc(c.MCP.Servers, func(u MCPServerConfig) bool { return u.Name == s.Name }) {
		return ErrMCPNameTaken
	}
	c.MCP.Servers = append(c.MCP.Servers, s)
	return nil
}

// orgMCP is the MCP servers with the organization's: a locked one in
// place of the person's of its name, a default one unless they have one;
// the person's own are dropped when the policy doesn't allow them. The
// problems are the organization's files that couldn't be read.
func orgMCP(own []MCPServerConfig, o OrgLayer) ([]MCPServerConfig, []string) {
	var out []MCPServerConfig
	var problems []string
	if o.Policy.MCP.Own {
		out = slices.Clone(own)
	}
	add := func(dir string, locked bool) {
		entries, err := os.ReadDir(filepath.Join(dir, "mcp"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			problems = append(problems, fmt.Sprintf("your organization's MCP servers (%s) can't be read: %v", filepath.Join(dir, "mcp"), err))
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
				continue
			}
			var s MCPServerConfig
			if _, err := toml.DecodeFile(filepath.Join(dir, "mcp", e.Name()), &s); err != nil {
				problems = append(problems, fmt.Sprintf("your organization's MCP server %s isn't used: %v", filepath.Join(dir, "mcp", e.Name()), err))
				continue
			}
			if s.Name == "" {
				s.Name = strings.TrimSuffix(e.Name(), ".toml")
			}
			i := slices.IndexFunc(out, func(x MCPServerConfig) bool { return x.Name == s.Name })
			switch {
			case i < 0:
				out = append(out, s)
			case locked:
				out[i] = s
			}
		}
	}
	add(o.Locked, true)
	add(o.Defaults, false)
	return out, problems
}

// around is subfolder sub of the organization's layers, around the
// person's and the workspace's folders: its defaults first and its
// locked items last, for lists where a later folder's item wins (agents,
// skills, commands).
func (o OrgLayer) around(sub string, dirs []string) []string {
	if !o.Active() {
		return dirs
	}
	out := append([]string{filepath.Join(o.Defaults, sub)}, dirs...)
	return append(out, filepath.Join(o.Locked, sub))
}

// LockedFirst is subfolders of the organization's layers around dirs for
// lists where an earlier folder's item wins (rules): its locked items
// first and its defaults last.
func (o OrgLayer) LockedFirst(dirs []string, subs ...string) []string {
	if !o.Active() {
		return dirs
	}
	var out []string
	// The person's policy's rules and guardrails first (EN-38), then the
	// catalog's locked ones.
	for _, s := range subs {
		if o.PolicyDir != "" {
			out = append(out, filepath.Join(o.PolicyDir, s))
		}
		out = append(out, filepath.Join(o.Locked, s))
	}
	out = append(out, dirs...)
	for _, s := range subs {
		out = append(out, filepath.Join(o.Defaults, s))
	}
	return out
}

// orgLayers is each settings folder's organization layer, as its settings
// last loaded it, for what reads the catalog without the settings to hand
// (connectors).
var orgLayers sync.Map

// OrgLayerFor is the organization layer the settings in prefixDir last
// loaded ("" for ~/.blitz); an empty one when there's none.
func OrgLayerFor(prefixDir string) OrgLayer {
	if o, ok := orgLayers.Load(ConfigDir(prefixDir)); ok {
		return o.(OrgLayer)
	}
	return OrgLayer{}
}
