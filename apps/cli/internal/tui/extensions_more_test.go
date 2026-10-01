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
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /envs, /skills, /permissions, /model_settings, /mode and /theme report
// each outcome a workspace can give them.
func TestExtensionCommandOutputs(t *testing.T) {
	notDir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notDir, nil, 0o600))
	runCommandCases(t, map[string]commandCase{
		"envs disabled": {
			setup: func(s *stubBackend) { s.listEnvs = func() ([]api.Env, error) { return nil, api.ErrScriptsDisabled } },
			line:  "/envs", want: []string{"Skills integration is not enabled."},
		},
		"envs sizes": {
			setup: func(s *stubBackend) {
				s.listEnvs = func() ([]api.Env, error) {
					return []api.Env{{Key: "big", Size: 3 << 30, Ready: true}, {Key: "mid", Size: 5 << 20, Ready: true}}, nil
				}
			},
			line: "/envs", want: []string{"3.0 GB", "5.0 MB"},
		},
		"prune disabled": {
			setup: func(s *stubBackend) {
				s.pruneEnvs = func() (api.PruneResult, error) { return api.PruneResult{}, api.ErrScriptsDisabled }
			},
			line: "/envs prune", want: []string{"Skills integration is not enabled."},
		},
		"prune with failures": {
			setup: func(s *stubBackend) {
				s.pruneEnvs = func() (api.PruneResult, error) {
					return api.PruneResult{Removed: 1, Failed: []api.EnvError{{Key: "k1", Err: errBoom}}}, nil
				}
			},
			line: "/envs prune", want: []string{"Could not remove k1: boom", "Removed 1 environment(s)"},
		},
		"remove disabled": {
			setup: func(s *stubBackend) { s.removeEnv = func(string) error { return api.ErrScriptsDisabled } },
			line:  "/envs remove k1", want: []string{"Skills integration is not enabled."},
		},
		"skill blocked": {
			setup: func(s *stubBackend) {
				s.listSkills = func() []api.SkillInfo {
					return []api.SkillInfo{
						{Name: "a", Scripts: []api.SkillScript{{Name: "run"}}, Blocked: []string{"scripts are off"}},
						{Name: "b", Scripts: []api.SkillScript{{Name: "run", Reasons: []string{"needs network"}}}},
					}
				}
			},
			line: "/skills", want: []string{"blocked: scripts are off", "blocked: needs network"},
		},
		"skill shown": {
			setup: func(s *stubBackend) {
				s.skill = func(name string) (api.SkillInfo, bool) {
					return api.SkillInfo{Name: name, Compatibility: "linux only", Tier: "ask", Bypass: true, Network: true,
						Scripts: []api.SkillScript{{Name: "run", Allowed: true}}}, true
				}
			},
			line: "/skills show pdf", want: []string{"linux only", "ask (bypass)", "Network: allowed"},
		},
		"skill without scripts": {
			setup: func(s *stubBackend) {
				s.skill = func(name string) (api.SkillInfo, bool) { return api.SkillInfo{Name: name}, true }
			},
			line: "/skills show notes", want: []string{"No scripts."},
		},
		"rule saved to the workspace, next start": {
			setup: func(s *stubBackend) {
				s.addPermission = func(effect, rule string, save api.Scope) (api.PermissionChange, error) {
					return api.PermissionChange{Rule: rule, NextStart: save == api.ScopeWorkspace, Saved: api.Saved{Path: "/w/.blitz/settings.toml"}}, nil
				}
			},
			line: "/permissions deny read(.env) --workspace", want: []string{"saved; read rules apply from the next start", "/w/.blitz/settings.toml"},
		},
		"rule fails": {
			setup: func(s *stubBackend) {
				s.addPermission = func(string, string, api.Scope) (api.PermissionChange, error) { return api.PermissionChange{}, errBoom }
			},
			line: "/permissions allow shell(ls) --save", want: []string{"Couldn't change the rules: boom"},
		},
		"rule removed and saved": {
			setup: func(s *stubBackend) {
				s.removePermission = func(rule string, _ api.Scope) (api.PermissionChange, error) {
					return api.PermissionChange{Rule: rule, Saved: api.Saved{Path: "/cfg/.env.toml"}}, nil
				}
			},
			line: "/permissions remove shell(ls) --save", want: []string{"Removed shell(ls)", "/cfg/.env.toml"},
		},
		"unknown verb": {line: "/permissions grant shell(ls)", want: []string{"Usage: /permissions"}},
		"settings for a bad model": {
			setup: func(s *stubBackend) {
				s.modelSettings = func(string) (api.ModelSettingsInfo, error) { return api.ModelSettingsInfo{}, api.ErrBadModelRef }
			},
			line: "/model_settings ???", want: []string{"Usage: /model_settings"},
		},
		"settings fail": {
			setup: func(s *stubBackend) {
				s.updateSettings = func(string, bool, []api.Setting) (api.ModelSettingsChange, error) {
					return api.ModelSettingsChange{}, errBoom
				}
			},
			line: "/model_settings gpt-5 seed=1", want: []string{"✗ boom"},
		},
		"settings with a global max": {
			setup: func(s *stubBackend) {
				s.modelSettings = func(ref string) (api.ModelSettingsInfo, error) {
					return api.ModelSettingsInfo{Model: ref, GlobalMaxTokens: 4096}, nil
				}
			},
			line: "/model_settings gpt-5", want: []string{"(global: 4096)"},
		},
		"bypass unavailable": {
			setup: func(s *stubBackend) {
				s.setPermission = func(string) (string, error) { return "", api.ErrBypassNeedsSandbox }
			},
			line: "/mode bypass", want: []string{"Bypass mode needs the OS sandbox"},
		},
		"mode fails": {
			setup: func(s *stubBackend) { s.setPermission = func(string) (string, error) { return "", errBoom } },
			line:  "/mode plan", want: []string{"Failed to apply setting: boom"},
		},
		"bypass": {
			setup: func(s *stubBackend) { s.setPermission = func(m string) (string, error) { return m, nil } },
			line:  "/mode bypass", want: []string{Red + Bold + "Permission mode: bypass"},
		},
	})
}

// /theme --save reports a config file it can't write.
func TestThemeSaveFails(t *testing.T) {
	app, _ := stubApp(t, "")
	app.ConfigDir = filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(app.ConfigDir, nil, 0o600))
	t.Cleanup(func() { plainDiffs.Store(false) })
	assert.Contains(t, runCmd(t, app, "/theme dark --save"), "✗ ")
}

// /search web says when there's no web access, search isn't set up, it
// fails, or finds nothing; none of these start a turn.
func TestSearchWebFailures(t *testing.T) {
	cases := map[string]struct {
		provider error
		search   error
		want     string
	}{
		"no web":      {provider: errBoom, want: "/search web needs web access"},
		"not set up":  {search: api.ErrNoSearch, want: "Web search isn't set up"},
		"fails":       {search: errBoom, want: "Search failed: boom"},
		"nothing new": {want: "No readable results for go"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			app, s := stubApp(t, "")
			turns := recordTurns(s)
			s.searchProvider = func() (string, error) { return "brave", c.provider }
			s.searchWeb = func(string) (api.WebSearch, error) { return api.WebSearch{}, c.search }
			out, err := runScript(t, app, lines("/search web go"))
			require.NoError(t, err)
			assert.Contains(t, out, c.want)
			assert.Empty(t, *turns)
		})
	}
}

// FormatToolCall shows a search's query.
func TestFormatToolCallShowsTheQuery(t *testing.T) {
	assert.Contains(t, FormatToolCall("web_search", map[string]any{"query": "go generics"}), "go generics")
}
