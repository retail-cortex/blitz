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

// Saving a skill and writing its files fail with what went wrong.
func TestSaveErrors(t *testing.T) {
	dir := t.TempDir()
	spec := Spec{Name: "pdf", Description: "d"}
	_, _, err := Save(dir, spec, "../x", "", nil)
	assert.ErrorIs(t, err, ErrUnknownSkill, "a previous name that isn't one")

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "odd", "SKILL.md"), 0o755))
	_, _, err = Save(dir, Spec{Name: "odd", Description: "d"}, "odd", "", nil)
	assert.Error(t, err, "SKILL.md that can't be read")
	assert.NotErrorIs(t, err, ErrUnknownSkill)

	writeSkill(t, dir, "list", "---\n- a\n---\n", nil)
	_, _, err = Save(dir, Spec{Name: "list", Description: "d"}, "list", "", nil)
	assert.ErrorContains(t, err, "SKILL.md's")
	_, err = Render([]byte("---\n- a\n---\n"), spec)
	assert.Error(t, err)

	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	_, _, err = Save(filepath.Join(file, "sub"), spec, "", "", nil)
	assert.Error(t, err, "a folder that can't be made")

	writeSkill(t, dir, "flat", "---\nname: flat\ndescription: d\n---\n", map[string]string{"references": ""})
	assert.Error(t, WriteFile(dir, "flat", "references/a.md", []byte("x")), "references/ is a file")
	assert.ErrorIs(t, DeleteFile(dir, "gone", "references/a.md"), ErrUnknownSkill)

	ro := filepath.Join(dir, "ro")
	writeSkill(t, ro, "locked", "---\nname: locked\ndescription: d\n---\n", nil)
	require.NoError(t, os.Chmod(filepath.Join(ro, "locked"), 0o555))
	t.Cleanup(func() { os.Chmod(filepath.Join(ro, "locked"), 0o755) })
	if f, err := os.Create(filepath.Join(ro, "locked", "probe")); err == nil {
		f.Close()
		return
	}
	_, _, err = Save(ro, Spec{Name: "locked", Description: "new"}, "locked", "", nil)
	assert.Error(t, err, "SKILL.md's folder can't be written")
	require.NoError(t, os.Chmod(ro, 0o555))
	t.Cleanup(func() { os.Chmod(ro, 0o755) })
	_, _, err = Save(ro, Spec{Name: "moved2", Description: "new"}, "locked", "", nil)
	assert.Error(t, err, "the folder can't be renamed")
}
