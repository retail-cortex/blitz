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
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/rrmcguinness/modenv/pkg/modenv"
)

// decodeError is how toml reports a value that doesn't fit its setting:
// the line, the message and the key.
var decodeError = regexp.MustCompile(`^toml: line (\d+)(?: \(last key "([^"]*)"\))?: (.*)$`)

// Problem is something wrong with a settings file, at a line when it has
// one (Line 0: the file as a whole).
type Problem struct {
	Line, Col int
	// Error problems stop the file from being saved; warnings don't.
	Error   bool
	Message string
}

// decodeSettings decodes the settings file at path over cfg: that file
// alone (no .env.<runtime>.toml or .env.local.toml beside it), with
// modenv's xor: values anywhere in it turned back into their text.
func decodeSettings(path string, cfg *Config) (toml.MetaData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return toml.MetaData{}, err
	}
	text := string(data)
	if strings.Contains(text, "xor:") {
		var m map[string]any
		if _, err := toml.Decode(text, &m); err != nil {
			return toml.MetaData{}, fmt.Errorf("%s: %w", path, err)
		}
		if err := decryptAll(m); err != nil {
			return toml.MetaData{}, fmt.Errorf("%s: %w", path, err)
		}
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(m); err != nil {
			return toml.MetaData{}, fmt.Errorf("%s: %w", path, err)
		}
		text = buf.String()
	}
	md, err := toml.Decode(text, cfg)
	if err != nil {
		return md, fmt.Errorf("%s: %w", path, err)
	}
	return md, nil
}

// decryptAll turns modenv's xor: values in v, at any depth, back into
// their text.
func decryptAll(v any) error {
	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			if s, ok := x.(string); ok && strings.HasPrefix(s, "xor:") {
				plain, err := modenv.DecryptSecret(s)
				if err != nil {
					return fmt.Errorf("%s: %w", k, err)
				}
				v[k] = plain
				continue
			}
			if err := decryptAll(x); err != nil {
				return err
			}
		}
	case []map[string]any:
		for _, x := range v {
			if err := decryptAll(x); err != nil {
				return err
			}
		}
	case []any:
		for i, x := range v {
			if s, ok := x.(string); ok && strings.HasPrefix(s, "xor:") {
				plain, err := modenv.DecryptSecret(s)
				if err != nil {
					return err
				}
				v[i] = plain
				continue
			}
			if err := decryptAll(x); err != nil {
				return err
			}
		}
	}
	return nil
}

// unknownSettings are the warnings for the keys in the settings file at
// path that Blitz doesn't know (CFG-48): loading ignores them, so a typo
// such as [permissions] denny is said out loud, not dropped.
func unknownSettings(path string, md toml.MetaData) []string {
	var out []string
	for _, k := range md.Undecoded() {
		out = append(out, fmt.Sprintf("%s: unknown setting %s (ignored)", path, k))
	}
	return out
}

// CheckSettings checks a settings file's text without saving it: TOML
// errors, values of the wrong type, invalid permission rules (errors);
// settings Blitz doesn't know, likely typos, API keys written as plain
// text and skill policy settings it can't honour (warnings).
func CheckSettings(text string) []Problem {
	var cfg Config
	md, err := toml.Decode(text, &cfg)
	if err != nil {
		p := Problem{Error: true, Message: err.Error()}
		if pe, ok := errors.AsType[toml.ParseError](err); ok {
			p.Line, p.Col, p.Message = pe.Position.Line, pe.Position.Col, pe.Message
		} else if m := decodeError.FindStringSubmatch(p.Message); m != nil {
			// A value of the wrong type.
			p.Line, _ = strconv.Atoi(m[1])
			p.Message = m[3]
			if m[2] != "" {
				p.Message = m[2] + ": " + m[3]
			}
		}
		// An error at the end of the last line is reported on the next.
		p.Line = min(p.Line, strings.Count(strings.TrimRight(text, "\n"), "\n")+1)
		return []Problem{p}
	}
	var out []Problem
	for effect, list := range map[string][]string{"allow": cfg.Permissions.Allow, "ask": cfg.Permissions.Ask, "deny": cfg.Permissions.Deny} {
		for _, r := range list {
			if _, err := validRule(effect, r); err != nil {
				out = append(out, Problem{Line: lineOf(text, strconv.Quote(r)), Error: true, Message: fmt.Sprintf("[permissions] %s: %v", effect, err)})
			}
		}
	}
	for _, k := range md.Undecoded() {
		out = append(out, Problem{Line: keyLine(text, k), Message: fmt.Sprintf("unknown setting %s", k)})
	}
	for _, p := range KeyedProviders {
		if md.IsDefined("llm", p, "api_key") {
			if src := keySource(str(lookup(mustMap(text), "llm", p, "api_key"))); src == KeyPlain || src == KeyObfuscated {
				out = append(out, Problem{Line: keyLine(text, toml.Key{"llm", p, "api_key"}), Message: fmt.Sprintf("the %s API key is in the file as %s text: set it in the form to keep it in the keychain", p, src)})
			}
		}
	}
	if p := strings.ToLower(cfg.LLM.Provider); md.IsDefined("llm", "provider") && p != "" && !ValidProvider(p) {
		out = append(out, Problem{Line: keyLine(text, toml.Key{"llm", "provider"}), Error: true, Message: fmt.Sprintf("llm.provider: unknown provider %q (%s)", cfg.LLM.Provider, ProviderList())})
	}
	// The background tasks' limits, as far as the file sets them.
	bg := DefaultConfig().Tools
	for key, set := range map[string]func(){
		"max_background_agents":      func() { bg.MaxBackgroundAgents = cfg.Tools.MaxBackgroundAgents },
		"background_agent_timeout":   func() { bg.BackgroundAgentTimeout = cfg.Tools.BackgroundAgentTimeout },
		"background_agent_max_turns": func() { bg.BackgroundAgentMaxTurns = cfg.Tools.BackgroundAgentMaxTurns },
	} {
		if md.IsDefined("tools", key) {
			set()
			if _, err := bg.BackgroundLimits(); err != nil {
				out = append(out, Problem{Line: keyLine(text, toml.Key{"tools", key}), Error: true, Message: err.Error()})
			}
			bg = DefaultConfig().Tools
		}
	}
	for _, msg := range cfg.Skills.Policy.Problems() {
		out = append(out, Problem{Line: keyLine(text, toml.Key{"skills", "policy"}), Message: msg})
	}
	for _, msg := range fixChoices(&cfg, false) {
		key, _, _ := strings.Cut(msg, " ")
		out = append(out, Problem{Line: keyLine(text, splitKey(key)), Message: msg})
	}
	for _, msg := range cfg.Workers.Policy.Problems() {
		out = append(out, Problem{Line: keyLine(text, toml.Key{"workers", "policy"}), Error: true, Message: msg})
	}
	// By line, errors first on a line.
	slices.SortStableFunc(out, func(a, b Problem) int {
		if c := cmp.Compare(a.Line, b.Line); c != 0 {
			return c
		}
		return cmp.Compare(boolRank(a.Error), boolRank(b.Error))
	})
	return out
}

func boolRank(b bool) int {
	if b {
		return 0
	}
	return 1
}

// String is the problem as "line N: message".
func (p Problem) String() string {
	if p.Line == 0 {
		return p.Message
	}
	return fmt.Sprintf("line %d: %s", p.Line, p.Message)
}

// lineOf is the first line (from 1) containing s, or 0.
func lineOf(text, s string) int {
	for i, l := range strings.Split(text, "\n") {
		if strings.Contains(l, s) {
			return i + 1
		}
	}
	return 0
}

// keyLine is the line (from 1) where key is set, or its table's header, or
// 0. It follows [table] and [[array]] headers and dotted keys; it doesn't
// look inside inline tables.
func keyLine(text string, key toml.Key) int {
	want := key.String()
	var table toml.Key
	for i, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case l == "" || strings.HasPrefix(l, "#"):
			continue
		case strings.HasPrefix(l, "["):
			name := strings.Trim(strings.SplitN(strings.TrimLeft(l, "["), "]", 2)[0], " ")
			table = splitKey(name)
			if table.String() == want {
				return i + 1
			}
		default:
			k, _, ok := strings.Cut(l, "=")
			if !ok {
				continue
			}
			full := append(append(toml.Key{}, table...), splitKey(strings.TrimSpace(k))...)
			if full.String() == want || strings.HasPrefix(want, full.String()+".") {
				return i + 1
			}
		}
	}
	return 0
}

// splitKey splits a dotted TOML key, unquoting quoted parts.
func splitKey(s string) toml.Key {
	var out toml.Key
	for s != "" {
		s = strings.TrimSpace(s)
		var part string
		if s[0] == '"' || s[0] == '\'' {
			end := strings.IndexByte(s[1:], s[0])
			if end < 0 {
				return append(out, s)
			}
			part, s = s[1:end+1], s[end+2:]
		} else {
			part, s, _ = strings.Cut(s, ".")
			s = "." + s
			if s == "." {
				s = ""
			}
		}
		out = append(out, strings.TrimSpace(part))
		s = strings.TrimPrefix(strings.TrimSpace(s), ".")
	}
	return out
}
