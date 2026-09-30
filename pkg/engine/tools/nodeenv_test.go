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

func TestNodeEnvsKeysAndMarkers(t *testing.T) {
	m := NewNodeEnvs(t.TempDir(), config.NPMPolicy{})
	assert.Equal(t, m.Key("/n", []string{"b@1", "a"}), m.Key("/n", []string{"a", " b@1 ", "a"}), "order, spaces and repeats don't matter")
	assert.NotEqual(t, m.Key("/n", []string{"a"}), m.Key("/other", []string{"a"}), "another node is another environment")
	assert.Contains(t, m.InstallCommand([]string{"zod@3"}), "--registry https://registry.npmjs.org -- zod@3")

	e, ready := m.Lookup("/n", []string{"zod"})
	assert.False(t, ready)
	require.NoError(t, os.MkdirAll(e.Dir, 0o700))
	require.NoError(t, m.writeMarker(e))
	got, ready := m.Lookup("/n", []string{"zod"})
	assert.True(t, ready)
	assert.Equal(t, filepath.Join(e.Dir, "node_modules"), got.Modules())
	used := m.touch(got, "demo")
	assert.Equal(t, []string{"demo"}, used.Skills)
	assert.Equal(t, []string{"demo"}, m.touch(used, "demo").Skills, "a skill is listed once")
}

func TestNPMCLI(t *testing.T) {
	prefix := t.TempDir()
	node := filepath.Join(prefix, "bin", "node")
	cli := filepath.Join(prefix, "lib", "node_modules", "npm", "bin", "npm-cli.js")
	require.NoError(t, os.MkdirAll(filepath.Dir(cli), 0o755))
	t.Setenv("PATH", t.TempDir()) // no npm on PATH: the one beside node
	_, err := npmCLI(node)
	assert.ErrorContains(t, err, "npm not found")
	require.NoError(t, os.WriteFile(cli, nil, 0o644))
	got, err := npmCLI(node)
	require.NoError(t, err)
	assert.Equal(t, cli, got)
}

func TestSystemNodeMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := SystemNode()
	assert.ErrorContains(t, err, "node not found")
}

func TestMaterializeTypeScript(t *testing.T) {
	skillsDir := t.TempDir()
	dir := filepath.Join(skillsDir, "demo")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "scripts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: demo\nscripts:\n  - name: file\n    language: typescript\n    relative_path: scripts/a.ts\n  - name: inline\n    language: typescript\n    inline_code: \"console.log(1)\"\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "a.ts"), []byte("export {}"), 0o644))
	prov, _ := skills.NewProvider()
	require.NoError(t, prov.DiscoverExternal([]string{skillsDir}))
	skill, ok := prov.Get("demo")
	require.True(t, ok)
	modules := t.TempDir()

	for i, name := range []string{"file", "inline"} {
		t.Run(name, func(t *testing.T) {
			path, root, cleanup, err := materializeTypeScript(skill, skill.Scripts[i], modules)
			require.NoError(t, err)
			defer cleanup()
			b, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.NotEmpty(t, b)
			link, err := os.Readlink(filepath.Join(root, "node_modules"))
			require.NoError(t, err)
			assert.Equal(t, modules, link, "the packages sit beside the script")
			cleanup()
			_, err = os.Stat(root)
			assert.True(t, os.IsNotExist(err), "cleanup leaves nothing")
		})
	}
	_, _, _, err := materializeTypeScript(skill, skills.ScriptDefinition{Name: "none"}, "")
	assert.ErrorContains(t, err, "no source")
}
