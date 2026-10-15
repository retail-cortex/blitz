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

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/retail-cortex/blitz/pkg/skillreg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skillsCLI runs `blitz skills …` against store, with input on stdin.
func skillsCLI(t *testing.T, store *skillreg.Store, input string, args ...string) (string, error) {
	t.Helper()
	cmd := newSkillsCommandWith(store)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestSkillsCommand(t *testing.T) {
	home := t.TempDir()
	keys := &secrets.Memory{}
	store := &skillreg.Store{File: filepath.Join(home, "skill-registries.toml"), UserSkills: filepath.Join(home, "skills"), Secrets: keys}

	// A local registry: an index and an archive beside it.
	reg := t.TempDir()
	var zbuf bytes.Buffer
	z := zip.NewWriter(&zbuf)
	f, _ := z.Create("SKILL.md")
	_, _ = f.Write([]byte("---\nname: lit-review\ndescription: Reviews literature\n---\nRead.\n"))
	require.NoError(t, z.Close())
	sum := sha256.Sum256(zbuf.Bytes())
	require.NoError(t, os.WriteFile(filepath.Join(reg, "lit.zip"), zbuf.Bytes(), 0o644))
	idx, _ := json.Marshal(skillreg.Index{Version: 1, Name: "Castor", Skills: []skillreg.Entry{{Name: "lit-review", Description: "Reviews literature", Version: "1.0.0", ArchiveURL: "lit.zip", SHA256: hex.EncodeToString(sum[:])}}})
	index := filepath.Join(reg, "index.json")
	require.NoError(t, os.WriteFile(index, idx, 0o644))

	out, err := skillsCLI(t, store, "", "registry", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No registries")
	out, err = skillsCLI(t, store, "", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No skills installed")

	_, err = skillsCLI(t, store, "\n", "registry", "add", index, "--token-stdin")
	assert.ErrorContains(t, err, "no token")
	out, err = skillsCLI(t, store, "t0k\n", "registry", "add", index, "--token-stdin")
	require.NoError(t, err)
	assert.Contains(t, out, "Added Castor: 1 skills")
	tok, _ := keys.Get("skill-registry:castor")
	assert.Equal(t, "t0k", tok)
	out, _ = skillsCLI(t, store, "", "registry", "list")
	assert.Contains(t, out, "castor  "+index)

	out, err = skillsCLI(t, store, "", "search", "literature")
	require.NoError(t, err)
	assert.Contains(t, out, "castor/lit-review  1.0.0  Reviews literature")
	out, _ = skillsCLI(t, store, "", "search", "nothing-like-it")
	assert.Contains(t, out, "No skills found.")

	project := t.TempDir()
	t.Chdir(project)
	out, err = skillsCLI(t, store, "", "install", "castor/lit-review", "--project")
	require.NoError(t, err)
	assert.Contains(t, out, "Installed lit-review 1.0.0 from castor")
	assert.FileExists(t, filepath.Join(project, ".agents", "skills", "lit-review", "SKILL.md"))
	_, err = skillsCLI(t, store, "", "install", "ghost")
	assert.ErrorContains(t, err, "no registry offers")

	out, err = skillsCLI(t, store, "", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "lit-review  1.0.0  castor")
	out, err = skillsCLI(t, store, "", "update")
	require.NoError(t, err)
	assert.Contains(t, out, "Everything is up to date.")

	out, err = skillsCLI(t, store, "", "uninstall", "lit-review")
	require.NoError(t, err)
	assert.Contains(t, out, "Removed lit-review")
	_, err = skillsCLI(t, store, "", "uninstall", "lit-review")
	assert.ErrorContains(t, err, "not installed from a registry")

	out, err = skillsCLI(t, store, "", "registry", "remove", "castor")
	require.NoError(t, err)
	assert.Contains(t, out, "Removed castor.")
	_, err = skillsCLI(t, store, "", "registry", "remove", "castor")
	assert.ErrorContains(t, err, "no such registry")
}

// A settings file that can't be read is reported by every command.
func TestSkillsCommandBrokenStore(t *testing.T) {
	home := t.TempDir()
	store := &skillreg.Store{File: filepath.Join(home, "skill-registries.toml"), UserSkills: filepath.Join(home, "skills"), Secrets: &secrets.Memory{}}
	require.NoError(t, os.WriteFile(store.File, []byte("[[registries"), 0o600))
	for _, args := range [][]string{
		{"registry", "list"}, {"registry", "add", filepath.Join(home, "nope.json")}, {"registry", "remove", "x"},
		{"search"}, {"install", "x"}, {"update"}, {"uninstall", "x"}, {"list"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := skillsCLI(t, store, "", args...)
			assert.Error(t, err)
		})
	}
	_, err := readToken(strings.NewReader("  tok \n"))
	assert.NoError(t, err)
	assert.NotNil(t, newSkillsCommand())
}
