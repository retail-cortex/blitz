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

package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillsReportThePolicyVerdict(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Skills.Policy.Languages = []string{"typescript"} })
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "tidy"), 0o755)
	os.WriteFile(filepath.Join(dir, "tidy", "SKILL.md"), []byte("---\nname: tidy\ndescription: tidies imports\nscripts:\n  - name: run\n    language: python\n    inline_code: print(1)\n    dependencies: [\"six==1.16.0\"]\n---\n"), 0o644)
	require.NoError(t, w.Skills().DiscoverExternal([]string{dir}))
	s, ok := w.Skill("tidy")
	require.True(t, ok, "skill %+v %v", s, ok)
	require.Equal(t, "tidies imports", s.Description, "skill %+v %v", s, ok)
	require.Len(t, s.Scripts, 1, "skill %+v %v", s, ok)
	sc := s.Scripts[0]
	assert.Equal(t, "python", sc.Language, "script %+v", sc)
	assert.Equal(t, "inline", sc.Source, "script %+v", sc)
	assert.Equal(t, []string{"six==1.16.0"}, sc.Deps, "script %+v", sc)
	assert.Greater(t, sc.Timeout, time.Duration(0), "script %+v", sc)
	// python isn't among the policy's languages.
	assert.False(t, sc.Allowed, "verdict %+v, tier %q", sc, s.Tier)
	assert.NotEqual(t, 0, len(sc.Reasons), "verdict %+v, tier %q", sc, s.Tier)
	assert.False(t, s.Runnable(), "verdict %+v, tier %q", sc, s.Tier)
	assert.NotEqual(t, "", s.Tier, "verdict %+v, tier %q", sc, s.Tier)
	_, ok = w.Skill("nope")
	assert.False(t, ok, "found a missing skill")
	i := slices.IndexFunc(w.ListSkills(), func(s api.SkillInfo) bool { return s.Name == "tidy" })
	assert.GreaterOrEqual(t, i, 0, "tidy not listed")
	got := w.SearchSkills("imports")
	assert.Len(t, got, 1, "search %+v", got)
	assert.Equal(t, "tidy", got[0].Name, "search %+v", got)
}

func TestActiveAgentTools(t *testing.T) {
	w := openTest(t)
	at := w.ActiveAgentTools()
	find := func(name string) (api.ToolInfo, bool) {
		i := slices.IndexFunc(at.Tools, func(t api.ToolInfo) bool { return t.Name == name })
		if i < 0 {
			return api.ToolInfo{}, false
		}
		return at.Tools[i], true
	}
	assert.Equal(t, "blitz", at.Agent, "agent %q, tools unsorted", at.Agent)
	assert.True(t, slices.IsSortedFunc(at.Tools, func(a, b api.ToolInfo) int { return strings.Compare(a.Name, b.Name) }), "agent %q, tools unsorted", at.Agent)
	r, ok := find("read_file")
	assert.True(t, ok, "read_file %+v %v", r, ok)
	assert.True(t, r.PlanAllowed, "read_file %+v %v", r, ok)
	assert.NotEqual(t, "", r.Description, "read_file %+v %v", r, ok)
	c, ok := find("create_file")
	assert.True(t, ok, "create_file %+v %v", c, ok)
	assert.False(t, c.PlanAllowed, "create_file %+v %v", c, ok)
	assert.True(t, mcpOfferedTo(nil, "a"), "MCP offer rule")
	assert.True(t, mcpOfferedTo([]string{"*"}, "a"), "MCP offer rule")
	assert.False(t, mcpOfferedTo([]string{"b"}, "a"), "MCP offer rule")
}

// writeEnv makes a script environment in ~/.blitz/envs; ready ones have
// their marker.
func writeEnv(t *testing.T, key string, ready bool, deps ...string) {
	t.Helper()
	dir := filepath.Join(config.ExpandHome("~/.blitz/envs"), key)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib", "pkg.py"), []byte("x = 1\n"), 0o644))
	if ready {
		marker, err := json.Marshal(map[string]any{"key": key, "deps": deps})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".blitz-env.json"), marker, 0o644))
	}
}

// Script environments are listed, removed one by one, and pruned: kept
// only when complete and needed by a script the policy lets run.
func TestScriptEnvironments(t *testing.T) {
	w := openTest(t)
	writeEnv(t, "stale", true, "old==1")
	writeEnv(t, "partial", false)
	needed := ""
	if python, err := tools.SystemPython(); err == nil {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "fmt"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "fmt", "SKILL.md"), []byte("---\nname: fmt\ndescription: formats\nscripts:\n  - name: run\n    language: python\n    inline_code: print(1)\n    dependencies: [\"six==1.16.0\"]\n---\n"), 0o644))
		require.NoError(t, w.Skills().DiscoverExternal([]string{dir}))
		needed = w.tools.SkillScripts().Envs().Key(python, []string{"six==1.16.0"})
		writeEnv(t, needed, true, "six==1.16.0")
	}

	envs, err := w.ListEnvs()
	require.NoError(t, err)
	keys := map[string]bool{}
	for _, e := range envs {
		keys[e.Key] = e.Ready
		assert.Positive(t, e.Size, "%s has files", e.Key)
	}
	assert.Equal(t, map[string]bool{"stale": true, "partial": false}, map[string]bool{"stale": keys["stale"], "partial": keys["partial"]})

	assert.Error(t, w.RemoveEnv("../escape"), "an invalid key")
	require.NoError(t, w.RemoveEnv("stale"))
	writeEnv(t, "stale2", true, "old==1")

	res, err := w.PruneEnvs()
	require.NoError(t, err)
	assert.Equal(t, 2, res.Removed, "the incomplete and the unneeded")
	assert.Positive(t, res.Freed)
	assert.Empty(t, res.Failed)
	envs, err = w.ListEnvs()
	require.NoError(t, err)
	if needed != "" {
		require.Len(t, envs, 1)
		assert.Equal(t, needed, envs[0].Key, "the needed environment stays")
	} else {
		assert.Empty(t, envs)
	}
}

// With skills off there are no script environments to manage.
func TestScriptEnvironmentsNeedSkills(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Skills.Enabled = false })
	_, err := w.ListEnvs()
	assert.ErrorIs(t, err, api.ErrScriptsDisabled)
	assert.ErrorIs(t, w.RemoveEnv("x"), api.ErrScriptsDisabled)
	_, err = w.PruneEnvs()
	assert.ErrorIs(t, err, api.ErrScriptsDisabled)
	assert.Nil(t, w.ListMCPServers(), "no MCP servers configured")
}

// A skill's information carries its tool requirements.
func TestSkillInfoTools(t *testing.T) {
	w := openTest(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "deploy"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy", "SKILL.md"), []byte("---\nname: deploy\ndescription: deploys\ntool_requirements:\n  - name: execute_command\n    description: runs the deploy\n---\nDeploy.\n"), 0o644))
	require.NoError(t, w.Skills().DiscoverExternal([]string{dir}))
	s, ok := w.Skill("deploy")
	require.True(t, ok)
	require.Len(t, s.Tools, 1)
	assert.Equal(t, api.SkillTool{Name: "execute_command", Why: "runs the deploy"}, s.Tools[0])
	assert.Equal(t, "", s.Tier, "no scripts, no tier")
}
