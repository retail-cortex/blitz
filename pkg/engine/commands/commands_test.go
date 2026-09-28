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

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAndExpand(t *testing.T) {
	c, err := Parse("fix", "project", "", []byte("---\ndescription: Fix an issue\nargument-hint: <issue>\nagent: qa\nallowed-tools: Read, Grep Bash(git *)\nmode: plan\n---\nFix issue #$1 ($ARGUMENTS).\n"))
	require.NoError(t, err)
	assert.Equal(t, "Fix an issue", c.Description, "parsed %+v", c)
	assert.Equal(t, "<issue>", c.ArgumentHint, "parsed %+v", c)
	assert.Equal(t, "qa", c.Agent, "parsed %+v", c)
	assert.True(t, c.Plan, "parsed %+v", c)
	assert.Equal(t, []string{"Read", "Grep", "Bash(git *)"}, c.AllowedTools, "parsed %+v", c)
	got := c.Expand("42 urgent")
	assert.Equal(t, "Fix issue #42 (42 urgent).", got, "expand %q", got)
	plain, _ := Parse("note", "user", "", []byte("# Summarize\nSummarize the diff."))
	assert.Equal(t, "Summarize", plain.Description, "plain %+v / %q", plain, plain.Expand("focus on tests"))
	assert.Equal(t, "# Summarize\nSummarize the diff.\n\nfocus on tests", plain.Expand("focus on tests"), "plain %+v / %q", plain, plain.Expand("focus on tests"))
	assert.Equal(t, "# Summarize\nSummarize the diff.", plain.Expand(""), "plain %+v / %q", plain, plain.Expand("focus on tests"))
	two, _ := Parse("x", "user", "", []byte("$1 then $2, not $10"))
	got = two.Expand("a")
	assert.Equal(t, "a then , not a0", got, "positional %q", got)
	for _, bad := range []string{"---\nmode: fast\n---\nx", "---\ndescription: x\n", "---\n---\n   "} {
		t.Run(bad, func(t *testing.T) {
			_, err := Parse("b", "user", "", []byte(bad))
			assert.Error(t, err, "%q should fail", bad)
		})
	}
}

func TestLoadNamespacesAndBundled(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "db"), 0o755)
	os.WriteFile(filepath.Join(dir, "db", "migrate.md"), []byte("Run migrations."), 0o644)
	os.WriteFile(filepath.Join(dir, "Deploy.md"), []byte("Deploy it."), 0o644)
	os.WriteFile(filepath.Join(dir, "bad name.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)
	list, err := Load(dir, "project")
	var names []string
	for _, c := range list {
		names = append(names, c.Name)
	}
	assert.Equal(t, "db:migrate,deploy", strings.Join(names, ","), "loaded %v, err %v", names, err)
	assert.Error(t, err, "loaded %v, err", names)
	assert.Contains(t, err.Error(), "bad name", "loaded %v, err %v", names, err)
	list, err = Load(filepath.Join(dir, "missing"), "user")
	assert.Len(t, list, 0, "missing dir: %v %v", list, err)
	assert.NoError(t, err, "missing dir: %v", list)
	bundled := map[string]Command{}
	for _, c := range Bundled() {
		bundled[c.Name] = c
	}
	for _, name := range []string{"review", "security-review", "simplify", "verify"} {
		t.Run(name, func(t *testing.T) {
			_, ok := bundled[name]
			assert.True(t, ok, "bundled command %s missing", name)
		})
	}
	assert.True(t, bundled["review"].Plan, "review should be read-only (plan), simplify shouldn't")
	assert.False(t, bundled["simplify"].Plan, "review should be read-only (plan), simplify shouldn't")
}
