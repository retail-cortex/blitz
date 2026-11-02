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
	"net/url"
	"os"
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
	Policy           OrgPolicy
	// Expires is when Blitz stops working under it unrenewed (EN-33).
	Expires time.Time
}

// LockOf is how the catalog applies the item at path: "locked" or
// "default" for one of its files, "" for any other.
func (o OrgLayer) LockOf(path string) string {
	switch {
	case !o.Active() || path == "":
		return ""
	case strings.HasPrefix(path, o.Locked+string(filepath.Separator)):
		return "locked"
	case strings.HasPrefix(path, o.Defaults+string(filepath.Separator)):
		return "default"
	}
	return ""
}

// Active reports whether a catalog applies.
func (o OrgLayer) Active() bool { return o.Defaults != "" || o.Locked != "" }

// Expired reports whether the catalog is past its expiry at now: the
// organization's work waits until it's fetched again (EN-33).
func (o OrgLayer) Expired(now time.Time) bool {
	return o.Active() && !o.Expires.IsZero() && now.After(o.Expires)
}

// OrgPolicy is the organization's policy as Blitz applies it (EN-35).
type OrgPolicy struct {
	Models struct {
		OwnProviders bool `json:"own_providers"`
	} `json:"models"`
	Tools struct {
		Off           []string `json:"off"`
		ShellCommands bool     `json:"shell_commands"`
	} `json:"tools"`
	MCP struct {
		Own bool `json:"own"`
	} `json:"mcp"`
	// Connectors says whether people may have connections of their own,
	// and which kinds anyone's may be (every kind when none).
	Connectors struct {
		Own   bool     `json:"own"`
		Kinds []string `json:"kinds"`
	} `json:"connectors"`
	Builtins struct {
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
	Policy  OrgPolicy `json:"policy"`
	Expires time.Time `json:"expires"`
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
	raw, err := os.ReadFile(filepath.Join(dir, "policy.json"))
	if err != nil {
		return
	}
	var st orgState
	if json.Unmarshal(raw, &st) != nil {
		return
	}
	layers := filepath.Join(dir, "catalog")
	cfg.Org = OrgLayer{Defaults: filepath.Join(layers, "default"), Locked: filepath.Join(layers, "locked"), Policy: st.Policy, Expires: st.Expires}
	orgLayers.Store(ConfigDir(prefixDir), cfg.Org)
	for _, tool := range st.Policy.Tools.Off {
		if !slices.Contains(cfg.Permissions.Deny, tool) {
			cfg.Permissions.Deny = append(cfg.Permissions.Deny, tool)
		}
	}
	cfg.MCP.Servers = orgMCP(cfg.MCP.Servers, cfg.Org)
}

// orgMCP is the MCP servers with the organization's: a locked one in
// place of the person's of its name, a default one unless they have one;
// the person's own are dropped when the policy doesn't allow them.
func orgMCP(own []MCPServerConfig, o OrgLayer) []MCPServerConfig {
	var out []MCPServerConfig
	if o.Policy.MCP.Own {
		out = slices.Clone(own)
	}
	add := func(dir string, locked bool) {
		entries, _ := os.ReadDir(filepath.Join(dir, "mcp"))
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
				continue
			}
			var s MCPServerConfig
			if _, err := toml.DecodeFile(filepath.Join(dir, "mcp", e.Name()), &s); err != nil {
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
	return out
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
	for _, s := range subs {
		out = append(out, filepath.Join(o.Locked, s))
	}
	out = append(out, dirs...)
	for _, s := range subs {
		out = append(out, filepath.Join(o.Defaults, s))
	}
	return out
}

// Around is the exported form of around, for the engine's own lists
// (commands).
func (o OrgLayer) Around(sub string, dirs []string) []string { return o.around(sub, dirs) }

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
