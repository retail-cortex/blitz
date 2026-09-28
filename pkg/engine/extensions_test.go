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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
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
