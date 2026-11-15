// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package agentpkg

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PK-10: each scheme read as Castor reads it, and written back.
func TestParseURI(t *testing.T) {
	tests := []struct {
		in   string
		want URI
		out  string
	}{
		{"blitz://blitz.acme.com/skill/sql-review@1.x", URI{Scheme: SchemeBlitz, Host: "blitz.acme.com", Kind: "skill", Name: "sql-review", Target: "skill/sql-review", Ref: "1.x"}, "blitz://blitz.acme.com/skill/sql-review@1.x"},
		{"blitz://blitz.acme.com/agent/writer", URI{Scheme: SchemeBlitz, Host: "blitz.acme.com", Kind: "agent", Name: "writer", Target: "agent/writer"}, "blitz://blitz.acme.com/agent/writer"},
		{"github://acme/skills@v2/sql", URI{Scheme: SchemeGitHub, Target: "acme/skills", Ref: "v2", Subpath: "sql"}, "github://acme/skills@v2/sql"},
		{"https://github.com/acme/skills/tree/dev/a/b", URI{Scheme: SchemeGitHub, Target: "acme/skills", Ref: "dev", Subpath: "a/b"}, "github://acme/skills@dev/a/b"},
		{"github://acme/skills:v3/x", URI{Scheme: SchemeGitHub, Target: "acme/skills", Ref: "v3", Subpath: "x"}, "github://acme/skills@v3/x"},
		{"github://acme/skills/sql", URI{Scheme: SchemeGitHub, Target: "acme/skills", Ref: "main", Subpath: "sql"}, "github://acme/skills@main/sql"},
		{"github://acme/skills", URI{Scheme: SchemeGitHub, Target: "acme/skills", Ref: "main"}, "github://acme/skills@main"},
		{"file://./skills/tone", URI{Scheme: SchemeFile, Target: "./skills/tone"}, "file://./skills/tone"},
		{"../tone", URI{Scheme: SchemeFile, Target: "../tone"}, "file://../tone"},
		{"castor://skills/tone", URI{Scheme: SchemeCastor, Target: "tone", Kind: "skill", Name: "tone"}, "castor://tone"},
		{"cstr://tone", URI{Scheme: SchemeCastor, Target: "tone", Kind: "skill", Name: "tone"}, "castor://tone"},
		{"mod://github.com/acme/skills@v1.2.0/sql", URI{Scheme: SchemeGo, Target: "github.com/acme/skills", Ref: "v1.2.0", Subpath: "sql"}, "mod://github.com/acme/skills@v1.2.0/sql"},
		{"go://github.com/acme/skills/sql", URI{Scheme: SchemeGo, Target: "github.com/acme/skills", Subpath: "sql"}, "mod://github.com/acme/skills/sql"},
		{"go://example.com/x", URI{Scheme: SchemeGo, Target: "example.com/x"}, "mod://example.com/x"},
		{"maven://com.acme:skills:1.0/sql", URI{Scheme: SchemeMaven, Target: "com.acme:skills:1.0", Ref: "1.0", Subpath: "sql"}, "maven://com.acme:skills:1.0/sql"},
		{"mvn://com.acme:skills", URI{Scheme: SchemeMaven, Target: "com.acme:skills"}, "maven://com.acme:skills"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseURI(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.out, got.String())
		})
	}
	for _, bad := range []string{"", "blitz:///skill/x", "blitz://h/plugin/x", "blitz://h/skill/Bad", "blitz://h/skill/x@nope"} {
		_, err := ParseURI(bad)
		assert.Error(t, err, bad)
	}
}

// PK-11, PK-12: assets read from and written to their folders, a lock
// read and written.
func TestAssetsAndLock(t *testing.T) {
	root := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0o644))
	}
	write("agents/a.md", "A")
	write("evals/e.md", "E")
	write("skills/s/SKILL.md", "S")
	write("skills/s/refs/r.md", "R")
	write("skills/empty/notes.md", "no SKILL.md")
	write("skills/big/SKILL.md", string(make([]byte, MaxFile+1)))
	write("workflows/w.flow.yaml", "nodes: []")
	write("workflows/notes.txt", "not one")
	got, skipped := ReadAssets(root, "agent", "skill", "workflow", "eval", "rule")
	assert.Equal(t, map[string]string{"skills/s/SKILL.md": "S", "skills/s/refs/r.md": "R"}, got["skill/s"])
	assert.Equal(t, map[string]string{"evals/e.md": "E"}, got["eval/e"])
	assert.Contains(t, got, "workflow/w")
	assert.Contains(t, skipped["skill/empty"], "no SKILL.md")
	assert.Contains(t, skipped["skill/big"], "over 1 MB")

	require.NoError(t, WriteAsset(root, "skill", "s", map[string]string{"skills/s/SKILL.md": "S2"}))
	files, why := ReadAsset(root, "skill", "s")
	assert.Empty(t, why)
	assert.Equal(t, map[string]string{"skills/s/SKILL.md": "S2"}, files, "replaced: refs/r.md gone")
	assert.Error(t, WriteAsset(root, "agent", "a", map[string]string{"agents/b.md": "x"}))
	assert.Error(t, WriteAsset(root, "skill", "s", map[string]string{"skills/s/../../x": "x"}))
	require.NoError(t, RemoveAsset(root, "agent", "a"))
	require.NoError(t, RemoveAsset(root, "agent", "a"), "gone already")
	assert.True(t, Owns("skill", "s", "skills/s/x"))
	assert.False(t, Owns("skill", "s", "skills/s/"))
	assert.False(t, Owns("rule", "r", "rules/x.md"))
	assert.Equal(t, "rules/r.md", MainFile("rule", "r"))

	path := filepath.Join(root, LockFile)
	l, err := ReadLock(path)
	require.NoError(t, err)
	assert.Empty(t, l.Assets)
	l.Assets["skill/s"] = LockEntry{Kind: "skill", Name: "s", URI: "github://acme/x@main", SHA256: "abc", Added: time.Unix(0, 0).UTC()}
	l.Assets["agent/a"] = LockEntry{Kind: "agent", Name: "a"}
	require.NoError(t, l.Write(path))
	back, err := ReadLock(path)
	require.NoError(t, err)
	assert.Equal(t, l, back)
	assert.Equal(t, []string{"agent/a", "skill/s"}, back.Keys())
	require.NoError(t, os.WriteFile(path, []byte("{nope"), 0o644))
	_, err = ReadLock(path)
	assert.Error(t, err)
}
