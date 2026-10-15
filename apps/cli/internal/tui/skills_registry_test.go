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
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/retail-cortex/blitz/pkg/skillreg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /skills registry, search and install, against a local registry.
func TestSkillsFromRegistries(t *testing.T) {
	app := newTestApp(t, nil)
	home := t.TempDir()
	store := &skillreg.Store{File: filepath.Join(home, "r.toml"), UserSkills: filepath.Join(home, "skills"), Secrets: &secrets.Memory{}}
	saved := skillRegistries
	skillRegistries = func() *skillreg.Store { return store }
	t.Cleanup(func() { skillRegistries = saved })

	out := captureStdout(t, func() { handleSkillsCommand([]string{"registry"}, app) })
	assert.Contains(t, out, "No skill registries")
	out = captureStdout(t, func() { handleSkillsCommand([]string{"search", "lit"}, app) })
	assert.NotContains(t, out, "In your registries", "none added: no registry section")

	reg := t.TempDir()
	var zbuf bytes.Buffer
	z := zip.NewWriter(&zbuf)
	f, _ := z.Create("SKILL.md")
	_, _ = f.Write([]byte("---\nname: lit-review\ndescription: Reviews literature\n---\nRead.\n"))
	require.NoError(t, z.Close())
	sum := sha256.Sum256(zbuf.Bytes())
	require.NoError(t, os.WriteFile(filepath.Join(reg, "lit.zip"), zbuf.Bytes(), 0o644))
	idx, _ := json.Marshal(skillreg.Index{Version: 1, Name: "Castor", Skills: []skillreg.Entry{{Name: "lit-review", Description: "Reviews literature", Version: "1.0.0", ArchiveURL: "lit.zip", SHA256: hex.EncodeToString(sum[:])}}})
	require.NoError(t, os.WriteFile(filepath.Join(reg, "index.json"), idx, 0o644))
	_, err := store.Add(t.Context(), "", filepath.Join(reg, "index.json"), "")
	require.NoError(t, err)

	out = captureStdout(t, func() { handleSkillsCommand([]string{"registry"}, app) })
	assert.Contains(t, out, "castor")
	out = captureStdout(t, func() { handleSkillsCommand([]string{"search", "literature"}, app) })
	assert.Contains(t, out, "In your registries (1)")
	assert.Contains(t, out, "castor/lit-review")
	assert.Contains(t, out, "/skills install")

	out = captureStdout(t, func() { handleSkillsCommand([]string{"install"}, app) })
	assert.Contains(t, out, "Usage: /skills install")
	out = captureStdout(t, func() { handleSkillsCommand([]string{"install", "ghost"}, app) })
	assert.Contains(t, out, "✗")
	out = captureStdout(t, func() { handleSkillsCommand([]string{"install", "castor/lit-review", "--project"}, app) })
	assert.Contains(t, out, "Installed lit-review 1.0.0 from castor")
	assert.FileExists(t, filepath.Join(app.Workspace.Dir(), ".agents", "skills", "lit-review", "SKILL.md"))
	_, ok := app.Workspace.Skill("lit-review")
	assert.True(t, ok, "the open workspace sees it at once")

	// A registry that can't be read is reported; a broken file too.
	_, err = store.Add(t.Context(), "gone", filepath.Join(reg, "index.json"), "")
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(reg, "index.json")))
	out = captureStdout(t, func() { handleSkillsCommand([]string{"search", "x"}, app) })
	assert.Contains(t, out, "registry castor")
	require.NoError(t, os.WriteFile(store.File, []byte("[[registries"), 0o600))
	out = captureStdout(t, func() { handleSkillsCommand([]string{"registry"}, app) })
	assert.Contains(t, out, "✗")
}
