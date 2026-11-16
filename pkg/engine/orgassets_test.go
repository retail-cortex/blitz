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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/config"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, text := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(text), 0o644))
	}
}

// EN-80 to EN-82: the organization's assets in a workspace: available ones
// a workspace's own of a name replaces, required ones nothing replaces;
// each of the organization's agents running the skill versions it was
// published with, while others run the skill the person has.
func TestOrgAssets(t *testing.T) {
	pol := filepath.Join(t.TempDir(), "policy")
	skill := func(desc string) string {
		return "---\nname: sql-review\ndescription: " + desc + "\n---\n" + desc + "\n"
	}
	writeFiles(t, pol, map[string]string{
		"required/agents/reviewer.md":               "---\nname: reviewer\ndescription: The organization's\nskills: [sql-review@1.x]\n---\nReview.",
		"available/agents/helper.md":                "---\nname: helper\ndescription: Helps\n---\nHelp.",
		"available/skills/sql-review/SKILL.md":      skill("v2"),
		"versions/skills/sql-review@1.0.0/SKILL.md": skill("v1"),
		"versions/skills/sql-review@2.0.0/SKILL.md": skill("v2"),
		"required/workflows/release.flow.yaml":      "description: The organization's release\nnodes: [{id: a, type: read_file, path: x.md}]\n",
		"available/workflows/notes.flow.yaml":       "description: The organization's notes\nnodes: [{id: a, type: read_file, path: x.md}]\n",
		"available/workflows/org-only.flow.yaml":    "description: Only the organization's\nnodes: [{id: a, type: read_file, path: x.md}]\n",
	})
	assets := []config.OrgAsset{
		{Kind: "agent", Name: "reviewer", Version: "1.0.0", State: "required", On: true, Deps: []config.OrgDep{{Kind: "skill", Name: "sql-review", Range: "1.x", Resolved: "1.0.0"}, {Kind: "agent", Name: "helper", Resolved: "1.0.0"}}},
		{Kind: "agent", Name: "helper", Version: "1.0.0", State: "available", On: true},
		{Kind: "skill", Name: "sql-review", Version: "2.0.0", State: "available", On: true},
		{Kind: "skill", Name: "sql-review", Version: "1.0.0", State: "dependency", On: true},
	}
	b, err := json.Marshal(assets)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(pol, config.OrgItemsFile), b, 0o600))

	w, _ := openTestWith(t, func(cfg *config.Config) {
		cfg.Org = config.OrgLayer{PolicyDir: pol}
		writeFiles(t, cfg.Tools.WorkspaceDir, map[string]string{
			".agents/agents/reviewer.md":          "---\nname: reviewer\ndescription: Mine\n---\nMine.",
			".agents/workflows/notes.flow.yaml":   "description: The workspace's notes\nnodes: [{id: a, type: read_file, path: x.md}]\n",
			".agents/workflows/release.flow.yaml": "description: The workspace's release\nnodes: [{id: a, type: read_file, path: x.md}]\n",
		})
	})

	reviewer, ok := w.Agents().Get("reviewer")
	require.True(t, ok)
	assert.Equal(t, "The organization's", reviewer.Description, "required: the workspace's of its name doesn't replace it")
	_, ok = w.Agents().Get("helper")
	assert.True(t, ok)

	got, ok := w.Skills().GetFor("reviewer", "sql-review")
	require.True(t, ok)
	assert.Equal(t, "v1", got.Description, "the reviewer runs the version it was published with")
	got, ok = w.Skills().GetFor("helper", "sql-review")
	require.True(t, ok)
	assert.Equal(t, "v2", got.Description, "others run the one the person has")
	got, _ = w.Skills().Get("sql-review")
	assert.Equal(t, "v2", got.Description)
	for _, s := range w.Skills().ListFor("reviewer") {
		if s.Name == "sql-review" {
			assert.Equal(t, "v1", s.Description)
		}
	}
	assert.Empty(t, w.skillVersions(""), "no agent: none")
	assert.Empty(t, w.skillVersions("helper"), "an agent without locks")

	byName := map[string]string{}
	for _, f := range w.Workflows() {
		byName[f.Name] = f.Description
	}
	assert.Equal(t, "The workspace's notes", byName["notes"], "available: the workspace's own replaces it")
	assert.Equal(t, "The organization's release", byName["release"], "required: nothing replaces it")
	assert.Equal(t, "Only the organization's", byName["org-only"])
	assert.True(t, strings.HasPrefix(reviewer.Path, pol))

	require.NoError(t, os.Remove(filepath.Join(pol, config.OrgItemsFile)))
	assert.Empty(t, w.skillVersions("reviewer"), "nothing offered: the provider's")
}
