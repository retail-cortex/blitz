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

// Package catalog checks an organization's catalog: a repository laid
// out as a workspace's .agents folder, whose agents, rules, skills,
// guardrails, commands, MCP servers and connectors every person's Blitz
// works under (spec_053_enterprise EN-30, EN-31). Each file is read as
// Blitz reads it, by the engine's own parsers, so a file Blitz would
// refuse can't be published. Each item may say how it applies: lock
// (locked, or a default a workspace or person may override) and groups
// (who it's for; everyone when it names none).
//
//	agents/<name>.md          an agent
//	rules/<name>.md           a rule
//	skills/<name>/SKILL.md    a skill, and its other files beside it
//	guardrails/<name>.md      a guardrail
//	commands/<name>.md        a slash command
//	mcp/<name>.toml           an MCP server: [mcp.servers]'s fields, with
//	                          secrets as {secrets:<name>}, never values
//	connectors/…/<name>.yaml  a connector's descriptor or connection, its
//	                          lock and groups in "# lock:" and "# groups:"
//	                          comments
package catalog

import (
	"bytes"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/commands"
	"github.com/retail-cortex/blitz/pkg/engine/gates"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/retail-cortex/blitz/pkg/secretscan"
)

// Kinds of item.
const (
	Agent     = "agent"
	Rule      = "rule"
	Skill     = "skill"
	SkillFile = "skill_file" // a file in a skill's folder beside SKILL.md
	Guardrail = "guardrail"
	Command   = "command"
	MCP       = "mcp"
	Connector = "connector"
)

// How an item applies (EN-31).
const (
	// Locked applies, and can't be changed or switched off.
	Locked = "locked"
	// Default applies unless a workspace or the person overrides it.
	Default = "default"
)

// Item is a catalog file as an item.
type Item struct {
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	Path   string   `json:"path"`
	Lock   string   `json:"lock"`             // Locked or Default
	Groups []string `json:"groups,omitempty"` // who it's for; everyone when empty
}

// For is whether the item applies to someone in groups.
func (it Item) For(groups []string) bool {
	if len(it.Groups) == 0 {
		return true
	}
	for _, g := range it.Groups {
		for _, h := range groups {
			if g == h {
				return true
			}
		}
	}
	return false
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// Of is what kind of item a path in the repository is, and its name; ok
// is false for a path that isn't an item (a README, policy.toml).
func Of(p string) (kind, name string, ok bool) {
	parts := strings.Split(path.Clean(p), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	base := parts[len(parts)-1]
	stem := strings.TrimSuffix(base, path.Ext(base))
	switch {
	case parts[0] == "skills" && len(parts) >= 3:
		if len(parts) == 3 && base == "SKILL.md" {
			return Skill, parts[1], true
		}
		return SkillFile, parts[1], true
	case len(parts) != 2 && parts[0] != "connectors":
		return "", "", false
	case parts[0] == "agents" && path.Ext(base) == ".md":
		return Agent, stem, true
	case parts[0] == "rules" && path.Ext(base) == ".md":
		return Rule, stem, true
	case parts[0] == "guardrails" && path.Ext(base) == ".md":
		return Guardrail, stem, true
	case parts[0] == "commands" && path.Ext(base) == ".md":
		return Command, stem, true
	case parts[0] == "mcp" && path.Ext(base) == ".toml":
		return MCP, stem, true
	case parts[0] == "connectors" && (path.Ext(base) == ".yaml" || path.Ext(base) == ".yml"):
		return Connector, stem, true
	}
	return "", "", false
}

// meta is an item's catalog fields.
type meta struct {
	Lock   string   `yaml:"lock" toml:"lock"`
	Groups []string `yaml:"groups" toml:"groups"`
}

// frontMatter is a Markdown file's YAML front matter ("" for none).
func frontMatter(data []byte) []byte {
	t := bytes.TrimLeft(data, "\uFEFF \t\r\n")
	if !bytes.HasPrefix(t, []byte("---")) {
		return nil
	}
	end := bytes.Index(t[3:], []byte("\n---"))
	if end < 0 {
		return nil
	}
	return t[3 : 3+end]
}

// Check reads the file at p (a path in the repository) as Blitz would,
// and says what's wrong with it: nothing when it can be published.
func Check(p string, data []byte) (Item, []string) {
	kind, name, ok := Of(p)
	if !ok {
		return Item{}, []string{p + ": not a catalog item (agents/, rules/, skills/, guardrails/, commands/, mcp/, connectors/)"}
	}
	it := Item{Kind: kind, Name: name, Path: p, Lock: Default}
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, p+": "+fmt.Sprintf(format, args...)) }
	if !nameRE.MatchString(name) {
		bad("the name %q: letters, digits, '.', '_' and '-', at most 64", name)
	}

	var m meta
	// YAML read whole first: into a struct, keys it doesn't have are
	// skipped, mistakes in them too.
	readYAML := func(b []byte) {
		var v any
		if err := yaml.Unmarshal(b, &v); err != nil {
			bad("%v", err)
		} else if err := yaml.Unmarshal(b, &m); err != nil {
			bad("%v", err)
		}
	}
	switch kind {
	case Agent, Rule, Guardrail, Command, Skill:
		if fm := frontMatter(data); fm != nil {
			readYAML(fm)
		}
	case MCP:
		if _, err := toml.Decode(string(data), &m); err != nil {
			bad("%v", err)
		}
	case Connector:
		readYAML(data)
		// A connector says it in comments (Place): its YAML is read
		// strictly, or by Toolbox, where a lock: key isn't allowed.
		if lock, groups, ok := connectorMeta(data); ok {
			m = meta{Lock: lock, Groups: groups}
		}
	}
	if kind != SkillFile {
		switch m.Lock {
		case "", Default:
		case Locked:
			it.Lock = Locked
		default:
			bad("lock %q: locked or default", m.Lock)
		}
		it.Groups = m.Groups
	}

	switch kind {
	case Agent:
		if _, err := agents.ParseMarkdownSpec(data); err != nil {
			bad("%v", err)
		}
	case Rule:
		if r := gates.Parse(name, p, data); r.Problem != "" {
			bad("%s", r.Problem)
		}
	case Guardrail:
		if g := gates.ParseGuardrail(name, p, data); g.Problem != "" {
			bad("%s", g.Problem)
		}
	case Command:
		if _, err := commands.Parse(name, "organization", p, data); err != nil {
			bad("%v", err)
		}
	case Skill:
		s, err := skills.ParseSkillMD(data, p)
		switch {
		case err != nil:
			bad("%v", err)
		case s.Name != name:
			bad("the skill's name %q isn't its folder's, %q", s.Name, name)
		default:
			for _, pr := range s.Problems {
				bad("%s", pr)
			}
		}
	case MCP:
		problems = append(problems, checkMCP(p, data)...)
	}
	if kind != MCP {
		// What every person gets holds no secret (an MCP server's fields
		// are checked one by one, above).
		lines := strings.Split(string(data), "\n")
		for _, f := range secretscan.Scan(p, data) {
			if !strings.Contains(lines[f.Line-1], "{secrets:") {
				bad("line %d holds what looks like a secret (%s %s): keep it in Secret Manager and name it, as {secrets:name}", f.Line, f.Kind, f.Masked)
			}
		}
	}
	return it, problems
}

// keyFlag is a command-line option that takes a secret (--api-key,
// --token=…).
var keyFlag = regexp.MustCompile(`(?i)^--?[a-z0-9_-]*(?:token|key|secret|password|passwd|credential)[a-z0-9_-]*$`)

// keyParam is a URL's query parameter that holds a secret.
var keyParam = regexp.MustCompile(`(?i)token|key|secret|password|passwd|sig|auth|credential`)

// secretRef is a value that names a secret rather than holding one.
var secretRef = regexp.MustCompile(`^\{secrets:[A-Za-z0-9_.-]+\}$`)

// checkMCP checks an MCP server's file: [mcp.servers]'s fields, and no
// secret written in it.
func checkMCP(p string, data []byte) []string {
	var problems []string
	var s struct {
		config.MCPServerConfig
		meta
	}
	md, err := toml.Decode(string(data), &s)
	if err != nil {
		return nil // reported with its meta
	}
	for _, k := range md.Undecoded() {
		problems = append(problems, fmt.Sprintf("%s: unknown setting %q", p, k.String()))
	}
	switch {
	case s.Command == "" && s.URL == "":
		problems = append(problems, p+": a command (a stdio server) or a url (an HTTP one)")
	case s.Command != "" && s.URL != "":
		problems = append(problems, p+": a command or a url, not both")
	}
	values := map[string]string{}
	for k, v := range s.Env {
		values["env "+k] = v
	}
	for k, v := range s.Headers {
		values["header "+k] = v
	}
	for i, a := range s.Args {
		if name, v, ok := strings.Cut(a, "="); ok && keyFlag.MatchString(name) {
			values[fmt.Sprintf("args[%d] %s", i, name)] = v
			continue
		}
		if i > 0 && keyFlag.MatchString(s.Args[i-1]) && !secretRef.MatchString(a) {
			problems = append(problems, fmt.Sprintf("%s: args[%d], after %s, holds a secret: name one in Secret Manager instead, as {secrets:name}", p, i, s.Args[i-1]))
			continue
		}
		values[fmt.Sprintf("args[%d]", i)] = a
	}
	if u, err := url.Parse(s.URL); err == nil && s.URL != "" {
		if _, ok := u.User.Password(); ok {
			problems = append(problems, fmt.Sprintf("%s: url holds a password: name one in Secret Manager instead, in a header, as {secrets:name}", p))
		}
		for k, vs := range u.Query() {
			for _, v := range vs {
				if keyParam.MatchString(k) && v != "" && !secretRef.MatchString(v) {
					problems = append(problems, fmt.Sprintf("%s: url's %s holds a secret: name one in Secret Manager instead, as {secrets:name}", p, k))
				}
			}
		}
	}
	for where, v := range values {
		if strings.Contains(v, "{secrets:") && !secretRef.MatchString(v) {
			problems = append(problems, fmt.Sprintf("%s: %s: a secret is its whole value, as {secrets:name}", p, where))
		} else if !secretRef.MatchString(v) && redact.New().String(v) != v {
			problems = append(problems, fmt.Sprintf("%s: %s holds a secret: name one in Secret Manager instead, as {secrets:name}", p, where))
		}
	}
	return problems
}
