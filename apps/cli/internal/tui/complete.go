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

package tui

import (
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/i18n"
)

// sourceTTL is how long a source's values are kept for the hint shown as
// you type; Tab asks again after it.
const sourceTTL = 10 * time.Second

// Completer completes slash commands at every level of the command tree,
// their flags and values, /config's key=value, and @path and @db:
// references (spec_067_cli_parity CLP-11). It also gives the hint shown
// after the cursor as you type (CLP-12) and a fuzzy match for Tab
// (CLP-13).
type Completer struct {
	mu        sync.RWMutex
	workspace string
	tree      []*Command
	// mentions are the databases' @db: references, without the @
	// (spec_059_data DAT-45).
	mentions func() []string
	// custom are the custom commands' names, asked each time.
	custom  func() []string
	sources map[Source]func() []string
	cache   map[Source]cached
	now     func() time.Time
}

type cached struct {
	values []string
	at     time.Time
}

// NewCompleter creates a completer of the built-in commands, rooted at
// workspace.
func NewCompleter(workspace string) *Completer {
	return &Completer{workspace: workspace, tree: Commands, sources: map[Source]func() []string{}, cache: map[Source]cached{}, now: time.Now}
}

// SetWorkspace makes completions of paths relative to dir (after /cd).
func (c *Completer) SetWorkspace(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workspace = dir
}

// Mentions registers a function supplying @db: references beside the
// workspace's paths, without the @; it's asked only when what's typed
// could be one (@, @d, @db:…).
func (c *Completer) Mentions(f func() []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mentions = f
}

// Custom registers a function supplying the custom commands' names, asked
// each time so commands added since start-up complete too.
func (c *Completer) Custom(f func() []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.custom = f
}

// Source registers what an argument source's values are.
func (c *Completer) Source(s Source, f func() []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sources[s] = f
	delete(c.cache, s)
}

// Do implements readline.AutoCompleter: the candidates' suffixes for the
// word before the cursor, and that word's length.
func (c *Completer) Do(line []rune, pos int) ([][]rune, int) {
	cands, current := c.complete(string(line[:pos]), true)
	return suffixes(cands, current), len([]rune(current))
}

// complete is every whole word the word being typed could become, and
// that word. ask false uses only what's known without asking a source
// (the hint shown as you type).
func (c *Completer) complete(before string, ask bool) ([]string, string) {
	words := strings.Fields(before)
	endsWithSpace := before == "" || strings.HasSuffix(before, " ")
	current := ""
	if !endsWithSpace && len(words) > 0 {
		current = words[len(words)-1]
	}
	switch {
	case strings.HasPrefix(current, "@"):
		return c.mentionsFor(current), current
	case !strings.HasPrefix(before, "/"):
		return nil, current
	case len(words) <= 1 && !endsWithSpace:
		return c.commandNames(current, ask), current
	}
	cmd := findIn(c.tree, strings.TrimPrefix(words[0], "/"))
	if cmd == nil {
		return nil, current // a custom command: its arguments are its own
	}
	done := words[1:]
	if !endsWithSpace {
		done = done[:len(done)-1]
	}
	node, arg, used := walk(cmd, done)
	var out []string
	for _, s := range node.Subs {
		if arg == 0 {
			out = append(out, s.Name+" ")
		}
	}
	for _, f := range node.Flags {
		if !slices.Contains(used, f) {
			out = append(out, f+" ")
		}
	}
	if arg == 0 || len(node.Subs) == 0 {
		for _, ch := range node.Choices {
			out = append(out, ch+" ")
		}
	}
	if len(node.Args) > 0 {
		src := node.Args[min(arg, len(node.Args)-1)]
		out = append(out, c.values(src, current, ask)...)
	}
	return out, current
}

// walk follows the finished words down the tree: the command or
// sub-command they reach, how many arguments of it are given, and the
// flags used.
func walk(cmd *Command, done []string) (node *Command, arg int, used []string) {
	node = cmd
	for _, w := range done {
		switch {
		case strings.HasPrefix(w, "--"):
			used = append(used, w)
		case arg == 0 && findIn(node.Subs, w) != nil:
			node = findIn(node.Subs, w)
		default:
			arg++
		}
	}
	return node, arg, used
}

// commandNames are the slash commands (and custom ones) for what's typed;
// an alias only when it alone matches.
func (c *Completer) commandNames(current string, ask bool) []string {
	typed := strings.ToLower(strings.TrimPrefix(current, "/"))
	var out, aliases []string
	for _, cmd := range c.tree {
		out = append(out, "/"+cmd.Name+" ")
		for _, a := range cmd.Aliases {
			if strings.HasPrefix(a, typed) && !strings.HasPrefix(cmd.Name, typed) {
				aliases = append(aliases, "/"+a+" ")
			}
		}
	}
	c.mu.RLock()
	custom := c.custom
	c.mu.RUnlock()
	if custom != nil && ask {
		for _, n := range custom() {
			out = append(out, "/"+n+" ")
		}
	}
	return append(out, aliases...)
}

// values are a source's values, as whole words.
func (c *Completer) values(src Source, current string, ask bool) []string {
	switch src {
	case SrcPaths, SrcDirs:
		if !ask {
			return nil
		}
		var out []string
		for _, p := range c.paths(current) {
			if src == SrcDirs && !strings.HasSuffix(p, "/") {
				continue
			}
			out = append(out, p)
		}
		return out
	case SrcThemes:
		return spaced(themes)
	case SrcCommands:
		var out []string
		for _, cmd := range c.tree {
			out = append(out, cmd.Name+" ")
		}
		return out
	case SrcConfig:
		return c.configValues(current, ask)
	}
	return spaced(c.sourceValues(src, ask))
}

// configKeyValues are /config's keys and the sources of their values.
var configKeyValues = map[string]Source{
	"model": SrcModels, "agent": SrcAgents, "style": SrcStyles, "personality": SrcPersonalities, "locale": SrcLocales,
}

// configValues complete /config's key=value: the keys, then a key's
// values.
func (c *Completer) configValues(current string, ask bool) []string {
	key, _, ok := strings.Cut(current, "=")
	if !ok {
		var out []string
		for _, k := range sortedConfigKeys() {
			out = append(out, k+"=")
		}
		return out
	}
	var vals []string
	switch key {
	case "mode", "permission_mode":
		vals = modes
	case "effort":
		vals = efforts
	case "agency":
		vals = agency
	default:
		if src, ok := configKeyValues[key]; ok {
			vals = c.sourceValues(src, ask)
		}
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		out = append(out, key+"="+v+" ")
	}
	return out
}

// sourceValues asks a registered source, keeping its answer a while; ask
// false gives only a kept answer.
func (c *Completer) sourceValues(src Source, ask bool) []string {
	c.mu.RLock()
	f, kept := c.sources[src], c.cache[src]
	c.mu.RUnlock()
	if f == nil {
		return nil
	}
	if !ask || c.now().Sub(kept.at) < sourceTTL {
		return kept.values
	}
	vals := f()
	c.mu.Lock()
	c.cache[src] = cached{values: vals, at: c.now()}
	c.mu.Unlock()
	return vals
}

// mentionsFor completes an @ reference: the databases' (after @, @d or
// @db:) and the workspace's paths.
func (c *Completer) mentionsFor(current string) []string {
	c.mu.RLock()
	mentions := c.mentions
	c.mu.RUnlock()
	var out []string
	if ref := current[1:]; mentions != nil && (strings.HasPrefix("db:", ref) || strings.HasPrefix(ref, "db:")) {
		out = mentions()
	}
	out = append(out, c.paths(current[1:])...)
	for i := range out {
		out[i] = "@" + out[i]
	}
	return out
}

// spaced is words, each followed by a space.
func spaced(words []string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = w + " "
	}
	return out
}

func suffixes(candidates []string, prefix string) [][]rune {
	sort.Strings(candidates)
	var out [][]rune
	seen := map[string]bool{}
	for _, cand := range candidates {
		if strings.HasPrefix(cand, prefix) && cand != prefix && !seen[cand] {
			seen[cand] = true
			out = append(out, []rune(cand[len(prefix):]))
		}
	}
	return out
}

// Fuzzy is the line with its last word replaced by the one candidate it
// matches by words or letters in order (/perms → /permissions, /pmod →
// /pin_model) when no candidate starts with it (CLP-13); false when there
// isn't exactly one.
func (c *Completer) Fuzzy(before string) (string, bool) {
	cands, current := c.complete(before, true)
	if current == "" || len(suffixes(slices.Clone(cands), current)) > 0 {
		return "", false
	}
	var match []string
	for _, cand := range cands {
		if subsequence(strings.ToLower(current), strings.ToLower(cand)) && !slices.Contains(match, cand) {
			match = append(match, cand)
		}
	}
	if len(match) != 1 {
		return "", false
	}
	return before[:len(before)-len(current)] + match[0], true
}

// subsequence reports whether every rune of a is in b, in order.
func subsequence(a, b string) bool {
	rb := []rune(b)
	i := 0
	for _, r := range a {
		for i < len(rb) && rb[i] != r {
			i++
		}
		if i == len(rb) {
			return false
		}
		i++
	}
	return true
}

// Suggest is what's shown dimmed after the cursor as you type a slash
// command (CLP-12): ghost, the rest of the word when the candidates
// agree on more of it (→ or Tab takes it), or else hint, the command's
// usage and what it does, once its name is typed.
func (c *Completer) Suggest(before string) (ghost, hint string) {
	if !strings.HasPrefix(before, "/") || strings.Contains(before, "\n") {
		return "", ""
	}
	cands, current := c.complete(before, false)
	if current != "" {
		if s := suffixes(cands, current); len(s) > 0 {
			common := string(s[0])
			for _, x := range s[1:] {
				common = commonPrefix(common, string(x))
			}
			if g := strings.TrimRight(common, " "); g != "" {
				return g, ""
			}
		}
	}
	words := strings.Fields(before)
	if len(words) == 0 {
		return "", ""
	}
	cmd := findIn(c.tree, strings.TrimPrefix(words[0], "/"))
	if cmd == nil || len(words) > 1 && !strings.HasSuffix(before, " ") {
		return "", ""
	}
	if len(words) > 1 || cmd.Help == "" {
		return "", "" // the hint is the command's, before its arguments
	}
	sep := " "
	if !strings.HasSuffix(before, " ") {
		sep = "  "
	}
	if cmd.Usage != "" {
		hint = sep + cmd.Usage + " — " + i18n.T(cmd.Help)
	} else {
		hint = sep + "— " + i18n.T(cmd.Help)
	}
	return "", hint
}

func commonPrefix(a, b string) string {
	ra, rb := []rune(a), []rune(b)
	n := 0
	for n < len(ra) && n < len(rb) && ra[n] == rb[n] {
		n++
	}
	return string(ra[:n])
}
