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

package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// list_or_search_skills lists every skill, or those matching a query;
// activate_skill reports an unknown skill, and both need a provider.
func TestSkillToolsListAndActivate(t *testing.T) {
	dir := t.TempDir()
	for name, desc := range map[string]string{"pdf-tools": "Work with PDF files", "charts": "Draw charts"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, name), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: "+desc+"\nversion: 1.2.0\ntags: [docs]\n---\nUse it.\n"), 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "scripted"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripted", "SKILL.md"), []byte("---\nname: scripted\ndescription: Has a script\nscripts:\n  - name: sh\n    description: Says hi\n    language: typescript\n    inline_code: echo hi\n---\n"), 0o644))
	prov, _ := skills.NewProvider()
	require.NoError(t, prov.DiscoverExternal([]string{dir}))

	list := toolOf(t)(NewListSkillsTool(prov))
	for _, c := range []struct {
		query string
		want  []string
	}{
		{query: "", want: []string{"charts", "pdf-tools", "scripted"}},
		{query: "pdf", want: []string{"pdf-tools"}},
		{query: "nothing-like-it", want: nil},
	} {
		t.Run("list "+c.query, func(t *testing.T) {
			out := runTool(t, list, map[string]any{"query": c.query})
			found, _ := out["skills"].([]any)
			var names []string
			for _, s := range found {
				names = append(names, s.(map[string]any)["name"].(string))
			}
			assert.Subset(t, names, c.want, "%v", out)
			assert.EqualValues(t, len(names), out["count"])
			if c.want == nil {
				assert.Empty(t, names)
			}
		})
	}

	activate := toolOf(t)(NewActivateSkillTool(prov, nil))
	pyOnly := config.DefaultConfig().Skills.Policy
	pyOnly.Languages = []string{"python"}
	strict := toolOf(t)(NewActivateSkillTool(prov, &pyOnly))
	out := runTool(t, activate, map[string]any{"skill_name": "pdf-tools"})
	assert.Equal(t, "", errOf(out))
	assert.Contains(t, out["instructions"], "Use it.")
	out = runTool(t, strict, map[string]any{"skill_name": "scripted"})
	scripts, _ := out["scripts"].([]any)
	require.Len(t, scripts, 1, "%v", out)
	sh := scripts[0].(map[string]any)
	assert.Equal(t, false, sh["allowed"])
	assert.Equal(t, "Says hi", sh["description"])
	assert.Contains(t, sh["blocked"], "isn't in skills.policy.languages", "a policy for Python only")
	out = runTool(t, activate, map[string]any{"skill_name": "nope"})
	assert.Contains(t, errOf(out), "skill 'nope' not found")

	for name, rt := range map[string]runnerTool{
		"list":     toolOf(t)(NewListSkillsTool(nil)),
		"activate": toolOf(t)(NewActivateSkillTool(nil, nil)),
	} {
		t.Run(name+" without a provider", func(t *testing.T) {
			args := map[string]any{"skill_name": "x"}
			if name == "list" {
				args = map[string]any{"query": "x"}
			}
			assert.Equal(t, "skills provider not initialized", errOf(runTool(t, rt, args)))
		})
	}
}
