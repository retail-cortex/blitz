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
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PK-14: the workspace's assets checked as Blitz reads them, in the
// service: a workflow with a step type Blitz hasn't is a problem; a
// skill's scripts aren't read as catalog files; no description is worth
// fixing.
func TestValidateAssets(t *testing.T) {
	w, _ := openTestWith(t, nil)
	root := filepath.Join(w.Dir(), ".agents")
	for rel, text := range map[string]string{
		"workflows/bad.flow.yaml":    "description: Bad\nnodes: [{id: a, type: nope}]\n",
		"skills/tone/SKILL.md":       "---\nname: tone\ndescription: Our tone\n---\nBe kind.\n",
		"skills/tone/scripts/run.sh": "#!/bin/sh\n",
		"agents/writer.md":           "---\nname: writer\n---\nWrite.\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0o644))
	}
	found, err := w.ValidateAssets(context.Background())
	require.NoError(t, err)
	by := map[string][]string{}
	warn := map[string][]string{}
	for _, f := range found {
		by[f.Key], warn[f.Key] = f.Problems, f.Warnings
	}
	assert.NotEmpty(t, by["workflow/bad"])
	assert.NotContains(t, by, "skill/tone", "fine, its script not read as a catalog file")
	assert.Contains(t, warn["agent/writer"][0], "no description")
	checked, _ := CatalogCheck("skills/tone/scripts/run.sh", nil)
	assert.False(t, checked)
	checked, _ = CatalogCheck("notes.txt", nil)
	assert.False(t, checked)
}
