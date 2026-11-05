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

// Package commands loads custom slash commands: Markdown files whose body
// is a prompt, from the user's and the project's command directories, plus
// the bundled ones. A command runs as a turn; its frontmatter can choose
// the agent, the model, the permitted tools and plan mode.
package commands

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/frontmatter"
	"gopkg.in/yaml.v3"
)

//go:embed builtin/*.md
var builtinFS embed.FS

// Command is one slash command.
type Command struct {
	Name         string // without the slash; may be namespaced, "db:migrate"
	Description  string
	ArgumentHint string
	// Agent and Model run the command's turn as another agent or on another
	// model ("" for the session's).
	Agent string
	Model string
	// AllowedTools limits the turn to these tools: nil is no limit, and
	// empty (an allowed-tools with nothing in it) is no tools.
	AllowedTools []string
	// Plan runs the command as a plan: tools that change anything are
	// refused.
	Plan bool
	Body string
	// Source is where it came from: "bundled", "user", "project", or
	// "skill".
	Source string
	Path   string
}

// meta is a command's front matter. A key it doesn't have is an error
// (a misspelt allowed-tools would otherwise allow every tool).
type meta struct {
	Description  string    `yaml:"description"`
	ArgumentHint string    `yaml:"argument-hint"`
	Agent        string    `yaml:"agent"`
	Model        string    `yaml:"model"`
	AllowedTools yaml.Node `yaml:"allowed-tools"`
	Mode         string    `yaml:"mode"`
	// DisableModelInvocation is Claude Code's, accepted and ignored: a
	// command here runs only when typed.
	DisableModelInvocation any `yaml:"disable-model-invocation"`
	// Lock and Groups are how an organization's catalog applies the
	// command (spec_053 EN-30); read by the catalog, accepted here.
	Lock   string   `yaml:"lock"`
	Groups []string `yaml:"groups"`
}

// validName is a command name: letters, digits, "-", "_", "." and ":"
// between namespace parts.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(:[a-z0-9][a-z0-9._-]*)*$`)

// ValidName reports whether name can be a command's.
func ValidName(name string) bool { return validName.MatchString(name) }

// Parse reads a command file's frontmatter (optional) and body.
func Parse(name, source, path string, data []byte) (Command, error) {
	c := Command{Name: name, Source: source, Path: path}
	front, body, ok := frontmatter.Split(data)
	if !ok && bytes.HasPrefix(bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\uFEFF"))), []byte("---")) {
		return c, errors.New("missing the --- line that ends the frontmatter")
	}
	if ok {
		var fm meta
		if err := frontmatter.Strict(front, &fm); err != nil {
			return c, fmt.Errorf("frontmatter: %w", err)
		}
		c.Description, c.ArgumentHint, c.Agent, c.Model = fm.Description, fm.ArgumentHint, fm.Agent, fm.Model
		var err error
		if c.AllowedTools, err = toolList(fm.AllowedTools); err != nil {
			return c, err
		}
		switch strings.ToLower(strings.TrimSpace(fm.Mode)) {
		case "", "default":
		case "plan":
			c.Plan = true
		default:
			return c, fmt.Errorf("mode %q: only plan is supported", fm.Mode)
		}
	}
	c.Body = strings.TrimSpace(string(body))
	if c.Body == "" {
		return c, errors.New("the command has no prompt")
	}
	if c.Description == "" {
		c.Description = firstLine(c.Body)
	}
	return c, nil
}

// toolList reads allowed-tools as a list or a space/comma-separated
// string: nil when there's no such key (no limit), empty when it holds
// nothing (no tools), and an error when it's something else, never all
// tools.
func toolList(n yaml.Node) ([]string, error) {
	out := []string{}
	switch {
	case n.Kind == 0: // no allowed-tools
		return nil, nil
	case n.Kind == yaml.ScalarNode && n.Tag == "!!null":
	case n.Kind == yaml.ScalarNode && n.Tag == "!!str":
		// Split on commas and spaces outside parentheses: "Read, Bash(git *)".
		t := n.Value
		depth, start := 0, 0
		flush := func(end int) {
			if w := strings.TrimSpace(t[start:end]); w != "" {
				out = append(out, w)
			}
		}
		for i, r := range t {
			switch {
			case r == '(':
				depth++
			case r == ')' && depth > 0:
				depth--
			case (r == ',' || r == ' ') && depth == 0:
				flush(i)
				start = i + 1
			}
		}
		flush(len(t))
	case n.Kind == yaml.SequenceNode:
		for _, x := range n.Content {
			if x.Kind != yaml.ScalarNode || x.Tag != "!!str" {
				return nil, fmt.Errorf("allowed-tools: line %d: a tool's name, not %s", x.Line, x.ShortTag())
			}
			if s := strings.TrimSpace(x.Value); s != "" {
				out = append(out, s)
			}
		}
	default:
		return nil, fmt.Errorf("allowed-tools: line %d: a list of tools or a comma-separated string, not %s", n.Line, n.ShortTag())
	}
	return out, nil
}

// ToolList reads a skill's allowed-tools (a space- or comma-separated
// string, "Bash(git *)" kept whole) as toolList does: "" is nil, no limit.
func ToolList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	out, _ := toolList(yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s})
	return out
}

func firstLine(s string) string {
	for line := range strings.Lines(s) {
		if t := strings.TrimSpace(strings.TrimLeft(line, "# ")); t != "" {
			if len(t) > 80 {
				t = t[:79] + "…"
			}
			return t
		}
	}
	return ""
}

// Expand returns the command's prompt for args: $ARGUMENTS is the whole
// argument string, $1…$9 the words. Without $ARGUMENTS or $1, arguments
// are appended.
func (c Command) Expand(args string) string {
	args = strings.TrimSpace(args)
	words := strings.Fields(args)
	body := c.Body
	used := strings.Contains(body, "$ARGUMENTS")
	body = strings.ReplaceAll(body, "$ARGUMENTS", args)
	for i := 9; i >= 1; i-- { // $10 isn't a thing; go down so $1 doesn't eat $1x
		key := "$" + strconv.Itoa(i)
		if strings.Contains(body, key) {
			used = true
			v := ""
			if i <= len(words) {
				v = words[i-1]
			}
			body = strings.ReplaceAll(body, key, v)
		}
	}
	if !used && args != "" {
		body += "\n\n" + args
	}
	return body
}

// subcommands are the words a command may be spelled with a subcommand:
// /skills update is /skills-update (front ends keep /skills, /agents and
// /rules for themselves).
var subcommands = map[string]bool{"skills": true, "agents": true, "rules": true}

// Alias returns the command a line "/name args" runs, and its arguments:
// "skills update x" is ("skills-update", "x"), and "agents update" is
// ("agents-update", ""); anything else is as it was.
func Alias(name, args string) (string, string) {
	sub, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
	if n := strings.ToLower(name); subcommands[n] && strings.EqualFold(sub, "update") {
		return n + "-update", strings.TrimSpace(rest)
	}
	return name, args
}

// Bundled returns the commands built into Blitz.
func Bundled() []Command {
	entries, _ := fs.ReadDir(builtinFS, "builtin")
	var out []Command
	for _, e := range entries {
		data, _ := builtinFS.ReadFile("builtin/" + e.Name()) // a failed read parses as an empty command and is skipped
		if c, err := Parse(strings.TrimSuffix(e.Name(), ".md"), "bundled", "", data); err == nil {
			out = append(out, c)
		}
	}
	return out
}

// Load reads the commands under dir: every *.md file, named after its path
// with subdirectories as namespaces (db/migrate.md is db:migrate).
// Problems are returned alongside what loaded.
func Load(dir, source string) ([]Command, error) {
	var out []Command
	var errs []error
	// The callback never fails, so neither does the walk.
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			errs = append(errs, err) // an unreadable folder's commands are missing
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		name := strings.ToLower(strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel)))
		name = strings.ReplaceAll(name, "/", ":")
		if !ValidName(name) {
			errs = append(errs, fmt.Errorf("%s: %q isn't a command name (letters, digits, - _ .)", p, name))
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		c, err := Parse(name, source, p, data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			return nil
		}
		out = append(out, c)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, errors.Join(errs...)
}
