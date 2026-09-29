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
	"math"
	"strconv"
	"strings"
)

// ModelSettings are generation settings for one model, from
// [model_settings."<model>"]. Unset (nil) fields fall back to the global
// blitz.temperature and max_tokens, or the provider's default.
type ModelSettings struct {
	// Temperature is the sampling temperature.
	Temperature *float64 `toml:"temperature"`
	// MaxTokens caps the tokens in one response.
	MaxTokens *int `toml:"max_tokens"`
	// TopP is nucleus sampling: only the most likely tokens up to this
	// probability.
	TopP *float64 `toml:"top_p"`
	// Seed makes sampling repeatable where the provider allows it.
	Seed *int `toml:"seed"`
	// ReasoningEffort is how hard the model thinks: minimal, low, medium,
	// high or max (providers map it to their own levels).
	ReasoningEffort *string `toml:"reasoning_effort"`
	// ThinkingBudget is the most tokens the model may spend thinking; 0
	// turns thinking off where the model allows it.
	ThinkingBudget *int `toml:"thinking_budget"`
}

// Efforts are the reasoning effort levels, lowest first.
var Efforts = []string{"minimal", "low", "medium", "high", "max"}

// ParseEffort reads an effort level (xhigh counts as max).
func ParseEffort(s string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(s))
	if e == "xhigh" {
		e = "max"
	}
	for _, x := range Efforts {
		if e == x {
			return e, nil
		}
	}
	return "", fmt.Errorf("reasoning effort %q: use %s", s, strings.Join(Efforts, ", "))
}

// ModelSettingKeys are the settings a model can have, in display order.
var ModelSettingKeys = []string{"temperature", "max_tokens", "top_p", "seed", "reasoning_effort", "thinking_budget"}

// IsZero reports whether no setting is set.
func (s ModelSettings) IsZero() bool {
	return s.Temperature == nil && s.MaxTokens == nil && s.TopP == nil && s.Seed == nil && s.ReasoningEffort == nil && s.ThinkingBudget == nil
}

// Get returns key's value as written in TOML, and whether it is set.
func (s ModelSettings) Get(key string) (string, bool) {
	switch key {
	case "temperature":
		return formatFloat(s.Temperature)
	case "top_p":
		return formatFloat(s.TopP)
	case "max_tokens":
		return formatInt(s.MaxTokens)
	case "seed":
		return formatInt(s.Seed)
	case "reasoning_effort":
		if s.ReasoningEffort == nil {
			return "", false
		}
		return strconv.Quote(*s.ReasoningEffort), true
	case "thinking_budget":
		return formatInt(s.ThinkingBudget)
	}
	return "", false
}

// Set parses and validates text for key; "" clears the setting.
func (s *ModelSettings) Set(key, text string) error {
	text = strings.TrimSpace(text)
	switch key {
	case "temperature":
		return setFloat(&s.Temperature, key, text, 0, 2, true)
	case "top_p":
		return setFloat(&s.TopP, key, text, 0, 1, false)
	case "max_tokens":
		return setInt(&s.MaxTokens, key, text, 1)
	case "seed":
		return setInt(&s.Seed, key, text, math.MinInt32)
	case "reasoning_effort":
		if text = strings.Trim(text, `"`); text == "" {
			s.ReasoningEffort = nil
			return nil
		}
		e, err := ParseEffort(text)
		if err != nil {
			return err
		}
		s.ReasoningEffort = &e
		return nil
	case "thinking_budget":
		return setInt(&s.ThinkingBudget, key, text, 0)
	}
	return fmt.Errorf("unknown setting %q (known: %s)", key, strings.Join(ModelSettingKeys, ", "))
}

func setFloat(dst **float64, key, text string, lo, hi float64, loInclusive bool) error {
	if text == "" {
		*dst = nil
		return nil
	}
	v, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(v) || v > hi || v < lo || (!loInclusive && v == lo) {
		open := "["
		if !loInclusive {
			open = "("
		}
		return fmt.Errorf("%s must be a number in %s%g, %g]", key, open, lo, hi)
	}
	*dst = &v
	return nil
}

func setInt(dst **int, key, text string, lo int64) error {
	if text == "" {
		*dst = nil
		return nil
	}
	v, err := strconv.ParseInt(text, 10, 32) // the APIs take 32-bit values
	if err != nil || v < lo {
		return fmt.Errorf("%s must be a whole number from %d to %d", key, lo, math.MaxInt32)
	}
	n := int(v)
	*dst = &n
	return nil
}

func formatFloat(v *float64) (string, bool) {
	if v == nil {
		return "", false
	}
	s := strconv.FormatFloat(*v, 'f', -1, 64)
	if !strings.ContainsAny(s, ".e") {
		s += ".0" // keep it a TOML float
	}
	return s, true
}

func formatInt(v *int) (string, bool) {
	if v == nil {
		return "", false
	}
	return strconv.Itoa(*v), true
}

// SaveModelSettings writes s as [model_settings."<model>"] in dir/.env.toml:
// set keys are written, unset ones removed, and the table is dropped when
// nothing is left. Other lines, and their comments, are kept.
func SaveModelSettings(dir, model string, s ModelSettings) (string, error) {
	if strings.TrimSpace(model) == "" {
		return "", errors.New("no model name")
	}
	table := "model_settings." + tomlKey(model)
	return editConfigFile(dir,
		func(doc string) string {
			for _, key := range ModelSettingKeys {
				if v, ok := s.Get(key); ok {
					doc = setTOMLKey(doc, table, key, v)
				} else {
					doc = removeTOMLKey(doc, table, key)
				}
			}
			return removeEmptyTable(doc, table)
		},
		func(check map[string]any) error {
			all, _ := check["model_settings"].(map[string]any)
			got, _ := all[model].(map[string]any)
			for _, key := range ModelSettingKeys {
				want, set := s.Get(key)
				v, present := got[key]
				if set != present || (set && !sameValue(v, want)) {
					return fmt.Errorf("could not update [%s] %s", table, key)
				}
			}
			return nil
		})
}

// sameValue reports whether a decoded TOML value equals text as written
// (a number, or a quoted string).
func sameValue(v any, text string) bool {
	if str, ok := v.(string); ok {
		want, err := strconv.Unquote(text)
		return err == nil && str == want
	}
	return sameNumber(v, text)
}

// sameNumber reports whether a decoded TOML number equals text.
func sameNumber(v any, text string) bool {
	want, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return false
	}
	switch n := v.(type) {
	case int64:
		return float64(n) == want
	case float64:
		return n == want
	}
	return false
}

// removeEmptyTable deletes [table]'s header when only blank lines and
// comments follow it, up to the next table. Comments are kept.
func removeEmptyTable(doc, table string) string {
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		m := tableRE.FindStringSubmatch(l)
		if m == nil || m[1] != table || strings.HasPrefix(strings.TrimSpace(l), "[[") {
			continue
		}
		for _, next := range lines[i+1:] {
			if tableRE.MatchString(next) {
				break
			}
			if t := strings.TrimSpace(next); t != "" && !strings.HasPrefix(t, "#") {
				return doc
			}
		}
		lines = append(lines[:i], lines[i+1:]...)
		if i > 0 && i < len(lines) && strings.TrimSpace(lines[i-1]) == "" && strings.TrimSpace(lines[i]) == "" {
			lines = append(lines[:i], lines[i+1:]...) // don't leave a double blank line
		}
		return strings.Join(lines, "\n")
	}
	return doc
}
