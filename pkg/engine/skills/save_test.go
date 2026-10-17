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

package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSkill(t *testing.T, dir, name, content string, files map[string]string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, name), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte(content), 0o644))
	for rel, body := range files {
		p := filepath.Join(dir, name, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
}

// The four kinds are told from the folder: references/ is listed, a
// script in scripts/ the front matter doesn't declare is found, and
// skills: makes a meta skill.
func TestSkillKinds(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "plain", "---\nname: plain\ndescription: d\n---\nDo it.\n", nil)
	writeSkill(t, dir, "guide", "---\nname: guide\ndescription: d\n---\n", map[string]string{"references/style.md": "s", "references/deep/more.md": "m", "notes.txt": "n"})
	writeSkill(t, dir, "tools", "---\nname: tools\ndescription: d\n---\n",
		map[string]string{"scripts/fetch.py": "print(1)", "scripts/sum.ts": "1", "scripts/README": "x", "references/a.md": "a"})
	writeSkill(t, dir, "declared", "---\nname: declared\ndescription: d\nscripts:\n  - name: fetch\n    language: python\n    relative_path: scripts/fetch.py\n---\n",
		map[string]string{"scripts/fetch.py": "import helper", "scripts/helper.py": "x = 1"})
	writeSkill(t, dir, "combo", "---\nname: combo\ndescription: d\nskills: [plain, guide]\nagents: [reviewer]\n---\n", nil)
	p, err := NewProvider()
	require.NoError(t, err)
	require.NoError(t, p.DiscoverExternal([]string{dir}))

	tests := []struct {
		name, kind string
		refs       []string
		scripts    []string
	}{
		{"plain", KindSimple, nil, nil},
		{"guide", KindReferences, []string{"references/deep/more.md", "references/style.md"}, nil},
		{"tools", KindScripts, []string{"references/a.md"}, []string{"fetch", "sum"}},
		{"declared", KindScripts, nil, []string{"fetch"}},
		{"combo", KindMeta, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, ok := p.Get(tt.name)
			require.True(t, ok)
			assert.Equal(t, tt.kind, s.Kind())
			assert.ElementsMatch(t, tt.refs, s.References)
			var names []string
			for _, sc := range s.Scripts {
				names = append(names, sc.Name)
			}
			assert.Equal(t, tt.scripts, names)
			assert.Empty(t, s.Problems)
		})
	}
	tools, _ := p.Get("tools")
	assert.Equal(t, ScriptDefinition{Name: "sum", Language: LanguageTypeScript, RelativePath: "scripts/sum.ts", Found: true}, tools.Scripts[1])
	declared, _ := p.Get("declared")
	assert.False(t, declared.Scripts[0].Found, "declared, and its helper isn't a script")
	guide, _ := p.Get("guide")
	assert.Contains(t, guide.Resources, "notes.txt")
	assert.Contains(t, guide.Resources, "references/style.md")

	combo, _ := p.Get("combo")
	assert.Equal(t, []string{"plain", "guide"}, combo.Members)
	assert.True(t, combo.OfferedTo("reviewer"))
	assert.False(t, combo.OfferedTo("blitz"))
	assert.True(t, guide.OfferedTo("anyone"), "no agents: every one")
}

func TestValidNameAndCheck(t *testing.T) {
	for name, want := range map[string]bool{"pdf": true, "pdf-tools-2": true, "": false, "PDF": false, "-pdf": false, "pdf-": false, "pdf--x": false, "pdf_x": false} {
		assert.Equal(t, want, ValidName(name), name)
	}
	reserved := func(n string) bool { return n == "builtin" }
	tests := []struct {
		name string
		spec Spec
		want int
	}{
		{"fine", Spec{Name: "ok", Description: "d"}, 0},
		{"bad name", Spec{Name: "Bad Name", Description: "d"}, 1},
		{"reserved", Spec{Name: "builtin", Description: "d"}, 1},
		{"no description", Spec{Name: "ok"}, 1},
		{"itself and a bad member", Spec{Name: "ok", Description: "d", Members: []string{"ok", "Not One"}}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Len(t, Check(tt.spec, reserved), tt.want)
		})
	}
}

// Saving keeps the front matter's other fields, in order; renames the
// folder; refuses a stale hash and a name taken; and removes a field the
// form empties.
func TestSave(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "pdf", "---\nname: pdf\nlicense: MIT\ndescription: Old\nagents: [a]\nmetadata:\n  team: docs\n---\nOld body.\n", map[string]string{"references/r.md": "r"})
	old, err := os.ReadFile(filepath.Join(dir, "pdf", "SKILL.md"))
	require.NoError(t, err)

	_, _, err = Save(dir, Spec{Name: "pdf", Description: "New"}, "pdf", "sha256:stale", nil)
	assert.ErrorIs(t, err, ErrChanged)
	writeSkill(t, dir, "taken", "---\nname: taken\ndescription: d\n---\n", nil)
	_, probs, err := Save(dir, Spec{Name: "taken", Description: "d"}, "pdf", "", nil)
	require.NoError(t, err)
	assert.Len(t, probs, 1, "the name is taken")
	_, _, err = Save(dir, Spec{Name: "x", Description: "d"}, "missing", "", nil)
	assert.ErrorIs(t, err, ErrUnknownSkill)

	p, probs, err := Save(dir, Spec{Name: "pdf-tools", Description: "Work with PDFs", Tools: "read_file  grep", Members: []string{"taken"}, Instructions: "New body."}, "pdf", FileHash(old), nil)
	require.NoError(t, err)
	require.Empty(t, probs)
	assert.Equal(t, filepath.Join(dir, "pdf-tools", "SKILL.md"), p)
	assert.NoDirExists(t, filepath.Join(dir, "pdf"))
	assert.FileExists(t, filepath.Join(dir, "pdf-tools", "references", "r.md"), "the folder moved with its files")
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "---\nname: pdf-tools\nlicense: MIT\ndescription: Work with PDFs\nmetadata:\n    team: docs\nallowed-tools: read_file grep\nskills: [taken]\n---\n\nNew body.\n", string(data))
	s, err := ParseSkillMD(data, p)
	require.NoError(t, err)
	assert.Equal(t, []string{"taken"}, s.Members)
	assert.Empty(t, s.Agents, "emptied: removed")

	p, probs, err = Save(dir, Spec{Name: "fresh", Description: "New one"}, "", "", nil)
	require.NoError(t, err)
	require.Empty(t, probs)
	data, _ = os.ReadFile(p)
	assert.Equal(t, "---\nname: fresh\ndescription: New one\n---\n\n", string(data))
}

// A skill's files go in references/ or scripts/, never outside its folder.
func TestSkillFiles(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "pdf", "---\nname: pdf\ndescription: d\n---\n", nil)
	require.NoError(t, WriteFile(dir, "pdf", "references/deep/guide.md", []byte("g")))
	assert.FileExists(t, filepath.Join(dir, "pdf", "references", "deep", "guide.md"))
	require.NoError(t, WriteFile(dir, "pdf", "scripts/run.py", []byte("print(1)")))
	require.NoError(t, DeleteFile(dir, "pdf", "scripts/run.py"))
	assert.NoFileExists(t, filepath.Join(dir, "pdf", "scripts", "run.py"))

	for _, rel := range []string{"../escape.md", "SKILL.md", "other/x.md", "references", "/etc/passwd", "references/../../x"} {
		t.Run(rel, func(t *testing.T) {
			assert.Error(t, WriteFile(dir, "pdf", rel, []byte("x")))
		})
	}
	assert.ErrorIs(t, WriteFile(dir, "nope", "references/a.md", nil), ErrUnknownSkill)
	assert.ErrorIs(t, WriteFile(dir, "Bad Name", "references/a.md", nil), ErrUnknownSkill)

	require.NoError(t, Delete(dir, "pdf"))
	assert.NoDirExists(t, filepath.Join(dir, "pdf"))
	assert.ErrorIs(t, Delete(dir, "pdf"), ErrUnknownSkill)
	assert.ErrorIs(t, Delete(dir, "../x"), ErrUnknownSkill)
}
