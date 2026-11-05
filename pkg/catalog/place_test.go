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

package catalog

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Publishing an item sets how it applies in its own file, the rest kept,
// and the catalog reads it so (EN-36).
func TestPlace(t *testing.T) {
	agent := []byte("---\nname: writer\ndescription: Writes briefs\nlock: default\ngroups: [old]\n---\nWrite a brief.\n")
	out, err := Place("agents/writer.md", agent, Locked, []string{"sales", "eng"})
	require.NoError(t, err)
	assert.Equal(t, "---\nname: writer\ndescription: Writes briefs\nlock: locked\ngroups: [sales, eng]\n---\nWrite a brief.\n", string(out))
	it, problems := Check("agents/writer.md", out)
	require.Empty(t, problems)
	assert.Equal(t, Locked, it.Lock)
	assert.Equal(t, []string{"sales", "eng"}, it.Groups)

	out, err = Place("agents/writer.md", agent, Default, nil)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "groups", "everyone")

	rule, err := Place("rules/short.md", []byte("Keep it short.\n"), Default, nil)
	require.NoError(t, err)
	assert.Equal(t, "---\nlock: default\n---\nKeep it short.\n", string(rule), "front matter made")

	mcp, err := Place("mcp/github.toml", []byte("url = \"https://api.githubcopilot.com/mcp/\"\n"), Locked, []string{"eng"})
	require.NoError(t, err)
	it, problems = Check("mcp/github.toml", mcp)
	require.Empty(t, problems)
	assert.Equal(t, Locked, it.Lock)
	assert.Equal(t, []string{"eng"}, it.Groups)

	// A connector's lock and groups are comments: its YAML is read
	// strictly, or by Toolbox (N26 safety-enterprise#12).
	conn := []byte("# kind: postgres\n# lock: default\nkind: source\nname: prod\ntype: postgres\n---\nkind: tool\nname: prod_q\n")
	for _, tc := range []struct {
		lock   string
		groups []string
	}{{Locked, []string{"data", "eng"}}, {Default, nil}} {
		out, err := Place("connectors/databases/prod.yaml", conn, tc.lock, tc.groups)
		require.NoError(t, err)
		assert.Contains(t, string(out), "kind: source\nname: prod\ntype: postgres\n---\nkind: tool", "its YAML as it was")
		assert.Equal(t, 1, strings.Count(string(out), "# lock:"))
		it, problems := Check("connectors/databases/prod.yaml", out)
		require.Empty(t, problems)
		assert.Equal(t, tc.lock, it.Lock)
		assert.Equal(t, tc.groups, it.Groups)
	}

	script := []byte("#!/bin/sh\necho hi\n")
	out, err = Place("skills/go/lint.sh", script, Locked, nil)
	require.NoError(t, err)
	assert.Equal(t, script, out, "a skill's other files as they are")

	for name, c := range map[string]struct {
		path, data, lock string
	}{
		"a lock of neither": {"agents/x.md", "---\nname: x\n---\nx", "maybe"},
		"not an item":       {"notes/x.md", "x", Locked},
		"bad front matter":  {"agents/x.md", "---\n: [\n---\nx", Locked},
		"a list for fields": {"agents/x.md", "---\n- a\n---\nx", Locked},
		"bad TOML":          {"mcp/x.toml", "url = ", Locked},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Place(c.path, []byte(c.data), c.lock, nil)
			assert.Error(t, err)
		})
	}
}
