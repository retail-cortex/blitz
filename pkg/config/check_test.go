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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckSettings(t *testing.T) {
	// The tools package checks rules; here, a rule is valid when its
	// parentheses close.
	orig := ValidatePermissionRule
	ValidatePermissionRule = func(_, rule string) (string, error) {
		if !strings.HasSuffix(rule, ")") {
			return "", errors.New("unclosed")
		}
		return rule, nil
	}
	t.Cleanup(func() { ValidatePermissionRule = orig })
	cases := []struct {
		name, text string
		want       []Problem // Message compared as a prefix
	}{
		{name: "valid", text: "[llm]\nprovider = \"gemini\"\n[llm.gemini]\nmodel = \"gemini-3.8-flash\"\n"},
		{name: "empty", text: ""},
		{name: "broken TOML at its line", text: "[llm]\nprovider = \"gemini\"\nmax_retries = = 3\n", want: []Problem{{Line: 3, Error: true, Message: "expected value"}}},
		{name: "unclosed table", text: "# settings\n[llm\n", want: []Problem{{Line: 2, Error: true}}},
		{name: "wrong type", text: "[llm]\nmax_retries = \"three\"\n", want: []Problem{{Line: 2, Error: true, Message: "llm.max_retries: incompatible types"}}},
		{
			name: "unknown settings at their lines",
			text: "[blitz]\ntemprature = 0.3\n\n[llm.gemini]\nmodle = \"x\"\n[nope]\n",
			want: []Problem{{Line: 2, Message: "unknown setting blitz.temprature"}, {Line: 5, Message: "unknown setting llm.gemini.modle"}, {Line: 6, Message: "unknown setting nope"}},
		},
		{name: "dotted keys", text: "llm.gemini.modle = \"x\"\n", want: []Problem{{Line: 1, Message: "unknown setting llm.gemini.modle"}}},
		{
			name: "invalid rule at its line",
			text: "[permissions]\nallow = [\n  \"shell(ls)\",\n  \"shell(\",\n]\n",
			want: []Problem{{Line: 4, Error: true, Message: "[permissions] allow:"}},
		},
		{name: "plain-text key", text: "[llm.openai]\napi_key = \"sk-abc123\"\n", want: []Problem{{Line: 2, Message: "the openai API key is in the file as plain text"}}},
		{name: "skill policy", text: "[skills.policy]\nsandbox = \"docker\"\n", want: []Problem{{Line: 1, Message: "skills.policy.sandbox"}}},
		{
			name: "errors first on a line, then by line",
			text: "[permissions]\ndeny = [\"shell(\"]\nextra = 1\n",
			want: []Problem{{Line: 2, Error: true, Message: "[permissions] deny:"}, {Line: 3, Message: "unknown setting permissions.extra"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CheckSettings(c.text)
			require.Len(t, got, len(c.want), "problems: %v", got)
			for i, w := range c.want {
				assert.Equal(t, w.Line, got[i].Line, "%v", got[i])
				assert.Equal(t, w.Error, got[i].Error, "%v", got[i])
				assert.True(t, strings.HasPrefix(got[i].Message, w.Message), "message %q, want %q…", got[i].Message, w.Message)
			}
		})
	}
}

func TestProblemString(t *testing.T) {
	assert.Equal(t, "line 3: bad", Problem{Line: 3, Message: "bad"}.String())
	assert.Equal(t, "bad", Problem{Message: "bad"}.String())
}

func TestSplitKey(t *testing.T) {
	for in, want := range map[string][]string{
		"llm":                       {"llm"},
		"llm.gemini":                {"llm", "gemini"},
		` model_settings . "a.b" `:  {"model_settings", "a.b"},
		`pricing.'claude-opus-5'.x`: {"pricing", "claude-opus-5", "x"},
	} {
		t.Run(in, func(t *testing.T) { assert.Equal(t, want, []string(splitKey(in))) })
	}
}
