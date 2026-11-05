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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rrmcguinness/modenv/pkg/modenv"
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
		// N26 engine-sub-2#7.
		{name: "job durations that aren't", text: "[workers.policy]\ndefault_timeout = \"30 minutes\"\n", want: []Problem{
			{Line: 1, Error: true, Message: `workers.policy.default_timeout = "30 minutes" isn't a duration`},
		}},
		{name: "background limits that aren't", text: "[tools]\nbackground_agent_timeout = \"30 min\"\nbackground_agent_max_turns = 0\n", want: []Problem{
			{Line: 2, Error: true, Message: "tools.background_agent_timeout is a length of time"},
			{Line: 3, Error: true, Message: "tools.background_agent_max_turns is a task's model calls"},
		}},
		{name: "a misspelt provider", text: "[llm]\nprovider = \"antropic\"\n", want: []Problem{{Line: 2, Error: true, Message: `llm.provider: unknown provider "antropic"`}}},
		{name: "background limits", text: "[tools]\nbackground_agent_timeout = \"2h\"\nmax_background_agents = 2\n"},
		{name: "a word that isn't a choice", text: "[blitz]\nplan_review = \"agent_decides\"\n[ui]\neditor = \"Vim\"\n", want: []Problem{{Line: 2, Message: `blitz.plan_review = "agent_decides" isn't one of`}}},
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

// The background tasks' limits are read from one place, and a value that
// isn't one is reported, naming its key (N26 engine-runtime#3).
func TestBackgroundLimits(t *testing.T) {
	cases := []struct {
		name string
		edit func(*ToolsConfig)
		want BackgroundLimits
		errs []string
	}{
		{name: "the defaults", want: BackgroundLimits{Agents: 4, Timeout: 30 * time.Minute, MaxTurns: 50}},
		{name: "an empty timeout is the default", edit: func(c *ToolsConfig) { c.BackgroundAgentTimeout = "" }, want: BackgroundLimits{Agents: 4, Timeout: 30 * time.Minute, MaxTurns: 50}},
		{name: "set", edit: func(c *ToolsConfig) {
			c.MaxBackgroundAgents, c.BackgroundAgentTimeout, c.BackgroundAgentMaxTurns = 2, "2h", 9
		}, want: BackgroundLimits{Agents: 2, Timeout: 2 * time.Hour, MaxTurns: 9}},
		{name: "a timeout that isn't one", edit: func(c *ToolsConfig) { c.BackgroundAgentTimeout = "30 min" }, errs: []string{"tools.background_agent_timeout", `"30 min"`}},
		{name: "a negative timeout", edit: func(c *ToolsConfig) { c.BackgroundAgentTimeout = "-1m" }, errs: []string{"tools.background_agent_timeout"}},
		{name: "no model calls", edit: func(c *ToolsConfig) { c.BackgroundAgentMaxTurns = 0 }, errs: []string{"tools.background_agent_max_turns", "not 0"}},
		{name: "no tasks", edit: func(c *ToolsConfig) { c.MaxBackgroundAgents = 0 }, errs: []string{"tools.max_background_agents", "not 0"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tc := DefaultConfig().Tools
			if c.edit != nil {
				c.edit(&tc)
			}
			got, err := tc.BackgroundLimits()
			if c.errs == nil {
				require.NoError(t, err)
				assert.Equal(t, c.want, got)
				return
			}
			require.Error(t, err)
			for _, e := range c.errs {
				assert.ErrorContains(t, err, e)
			}
		})
	}
}

// Loading says what the settings files hold that it doesn't use: unknown
// keys (a typo in a rule's list), [mcp] off in the global file and
// [enterprise] in a workspace's (CFG-48). It reads .env.toml alone, never
// a .env.local.toml or .env.<runtime>.toml beside it (CFG-02).
func TestLoadWarnings(t *testing.T) {
	cases := []struct {
		name            string
		global, overlay string
		extra           map[string]string // other files in the settings folder
		want            []string          // warnings, as substrings, in order
		check           func(t *testing.T, cfg *Config)
	}{
		{name: "known settings", global: "[permissions]\ndeny = [\"shell(rm*)\"]\n"},
		{
			name: "a typo in the global file", global: "[permissions]\ndenny = [\"shell(git push --force*)\"]\n",
			want: []string{".env.toml: unknown setting permissions.denny (ignored)"},
		},
		{
			name: "a typo in the workspace's", overlay: "[sandbox]\nblocked_path = [\"/x\"]\n",
			want: []string{"unknown setting sandbox.blocked_path (ignored)"},
		},
		{
			name: "a word that isn't a choice", global: "[blitz]\nplan_review = \"Always\"\n[ui]\nnotify = \"loud\"\n",
			want: []string{`ui.notify = "loud" isn't one of both, bell, desktop, off: "both" is used`},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, PlanReviewAlways, cfg.Blitz.PlanReview, "another case is the word")
				assert.Equal(t, "both", cfg.UI.Notify, "the default")
			},
		},
		{
			name: "mcp off in the global file", global: "[mcp]\noff = [\"github\"]\n",
			want: []string{"[mcp] off is ignored here"},
		},
		{
			name: "enterprise in the workspace's", overlay: "[enterprise]\nurl = \"https://evil.example\"\n",
			want: []string{"[enterprise] is ignored here"},
			check: func(t *testing.T, cfg *Config) {
				assert.Empty(t, cfg.Enterprise.URL)
			},
		},
		{
			name: "local and runtime files aren't read", global: "[llm]\nprovider = \"gemini\"\n",
			extra: map[string]string{".env.local.toml": "[llm]\nprovider = \"openai\"\n", ".env.dev.toml": "[llm]\nprovider = \"anthropic\"\n"},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "gemini", cfg.LLM.Provider)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keysEnv(t)
			t.Setenv("MODENV_RUNTIME", "dev")
			writeSettings(t, tc.global)
			for name, text := range tc.extra {
				require.NoError(t, os.WriteFile(filepath.Join(ConfigDir(""), name), []byte(text), 0o600))
			}
			ws := t.TempDir()
			if tc.overlay != "" {
				dir := WorkspaceSettingsDir("", ws)
				require.NoError(t, os.MkdirAll(dir, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.toml"), []byte(tc.overlay), 0o600))
			}
			cfg, err := LoadWorkspace("", ws)
			require.NoError(t, err)
			require.Len(t, cfg.Warnings, len(tc.want), "%q", cfg.Warnings)
			for i, w := range tc.want {
				assert.Contains(t, cfg.Warnings[i], w)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}

// Every credential the settings hold resolves keychain: references and
// xor: values, not only the three keyed providers' (CFG-43).
func TestLoadResolvesEveryCredential(t *testing.T) {
	_, store := keysEnv(t)
	require.NoError(t, store.Set("global/llm.azure.api_key", "az-secret"))
	writeSettings(t, "[llm.azure]\napi_key = \"keychain:global/llm.azure.api_key\"\n[web]\nsearch_api_key = \""+modenv.EncryptSecret("brave-secret")+"\"\n"+
		"[[mcp.servers]]\nname = \"db\"\ncommand = \"db\"\nenv = { TOKEN = \""+modenv.EncryptSecret("tok")+"\" }\n")
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, "az-secret", cfg.LLM.Azure.APIKey)
	assert.Equal(t, "brave-secret", cfg.Web.SearchAPIKey)
	require.Len(t, cfg.MCP.Servers, 1)
	assert.Equal(t, "tok", cfg.MCP.Servers[0].Env["TOKEN"], "xor: is decoded anywhere, as modenv did")

	writeSettings(t, "[web]\nsearch_api_key = \"xor:zz\"\n")
	_, err = Load("")
	assert.Error(t, err, "an xor: value that doesn't decode")
}
