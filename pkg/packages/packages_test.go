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
package packages

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/agentpkg"
)

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(text), 0o644))
}

// archive is a GitHub archive of files, under repo-ref/ as GitHub's are.
func archive(t *testing.T, files map[string]string) []byte {
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	tw := tar.NewWriter(zw)
	for p, text := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: "skills-main/" + p, Mode: 0o644, Size: int64(len(text)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(text))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())
	return b.Bytes()
}

type registry map[string]map[string]string

func (r registry) Fetch(_ context.Context, host, kind, name, rng string) (string, map[string]string, error) {
	files, ok := r[kind+"/"+name]
	if !ok || host != "blitz.acme.com" {
		return "", nil, errors.New("not in the registry")
	}
	return "1.4.2", files, nil
}

// PK-12, PK-15: packages added from a folder, GitHub and the registry,
// each locked; verified, changed, removed; one of the workspace's own of a
// name not replaced without force; the schemes Blitz doesn't fetch said.
func TestAddAndVerify(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	root := Root(ws)
	src := t.TempDir()
	write(t, src, "tone/SKILL.md", "---\nname: tone\ndescription: Our tone\n---\nBe kind.\n")
	write(t, src, "tone/references/style.md", "Short sentences.")
	write(t, src, "agents/writer.md", "---\nname: writer\ndescription: Writes\n---\nWrite.\n")
	write(t, src, "brief.flow.yaml", "description: A brief\nnodes: []\n")
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/acme/skills/tar.gz/main" {
			http.NotFound(w, r)
			return
		}
		assert.Equal(t, "Bearer gh-token", r.Header.Get("Authorization"))
		_, _ = w.Write(archive(t, map[string]string{"sql/a/SKILL.md": "---\nname: a\ndescription: A\n---\nA.\n", "sql/b/SKILL.md": "---\nname: b\ndescription: B\n---\nB.\n", "README.md": "x"}))
	}))
	defer gh.Close()
	reg := registry{"skill/sql-review": {"skills/sql-review/SKILL.md": "---\nname: sql-review\ndescription: Reviews SQL\n---\nReview.\n"}}
	o := Options{Registry: reg, GitHubBase: gh.URL, GitHubToken: "gh-token"}

	tests := []struct {
		uri  string
		kind string
		want []string
	}{
		{"file://" + filepath.Join(src, "tone"), "", []string{"skill/tone"}},
		{filepath.Join(src, "agents", "writer.md"), "", []string{"agent/writer"}},
		{filepath.Join(src, "brief.flow.yaml"), "", []string{"workflow/brief"}},
		{"github://acme/skills/sql", "", []string{"skill/a", "skill/b"}},
		{"blitz://blitz.acme.com/skill/sql-review@1.x", "", []string{"skill/sql-review"}},
	}
	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			o := o
			o.Kind = tt.kind
			added, err := Add(ctx, root, tt.uri, o)
			require.NoError(t, err)
			var keys []string
			for _, e := range added {
				keys = append(keys, e.Kind+"/"+e.Name)
				assert.NotEmpty(t, e.SHA256)
			}
			assert.Equal(t, tt.want, keys)
		})
	}
	lock, err := agentpkg.ReadLock(filepath.Join(root, agentpkg.LockFile))
	require.NoError(t, err)
	assert.Equal(t, "1.4.2", lock.Assets["skill/sql-review"].Version)
	assert.Equal(t, "github://acme/skills@main/sql", lock.Assets["skill/a"].URI)
	b, err := os.ReadFile(filepath.Join(root, "skills", "tone", "references", "style.md"))
	require.NoError(t, err)
	assert.Equal(t, "Short sentences.", string(b))

	// Again from the same package: replaced. One of the workspace's own,
	// or from elsewhere: not, unless forced.
	_, err = Add(ctx, root, "file://"+filepath.Join(src, "tone"), o)
	assert.NoError(t, err)
	write(t, root, "rules/mine.md", "Mine.")
	write(t, src, "rules/mine.md", "Theirs.")
	_, err = Add(ctx, root, filepath.Join(src, "rules", "mine.md"), o)
	assert.ErrorIs(t, err, ErrTaken)
	o.Force = true
	_, err = Add(ctx, root, filepath.Join(src, "rules", "mine.md"), o)
	assert.NoError(t, err)
	o.Force = false

	for _, bad := range []string{"castor://tone", "mod://github.com/acme/x@v1", "maven://a:b:1"} {
		_, err := Add(ctx, root, bad, o)
		assert.ErrorIs(t, err, ErrNotFetched, bad)
	}
	_, err = Add(ctx, root, "github://acme/nope", o)
	assert.ErrorContains(t, err, "404")
	_, err = Add(ctx, root, "blitz://blitz.acme.com/skill/sql-review", Options{})
	assert.ErrorContains(t, err, "registry")
	_, err = Add(ctx, root, filepath.Join(src, "nothing"), o)
	assert.Error(t, err)
	_, err = Add(ctx, root, "blitz://other.example/skill/x", o)
	assert.Error(t, err)

	results, err := Verify(root)
	require.NoError(t, err)
	for _, r := range results {
		assert.Equal(t, Verified, r.Status, r.Key)
	}
	write(t, root, "skills/tone/SKILL.md", "---\nname: tone\n---\nChanged.\n")
	require.NoError(t, os.Remove(filepath.Join(root, "agents", "writer.md")))
	results, err = Verify(root)
	require.NoError(t, err)
	by := map[string]string{}
	for _, r := range results {
		by[r.Key] = r.Status
	}
	assert.Equal(t, Modified, by["skill/tone"])
	assert.Equal(t, Missing, by["agent/writer"])
	assert.Equal(t, Verified, by["workflow/brief"])
}

// PK-13, PK-14: the assets compiled into one manifest (and Castor's), and
// checked: what Blitz refuses, what's worth fixing.
func TestCompileAndValidate(t *testing.T) {
	ws := t.TempDir()
	root := Root(ws)
	write(t, root, "skills/tone/SKILL.md", "---\nname: tone\ndescription: Our tone\n---\nBe kind. See [the style](references/style.md) and [gone](references/gone.md).\n")
	write(t, root, "skills/tone/references/style.md", "Short.")
	write(t, root, "agents/writer.md", "---\nname: writer\nskills: [tone@1.x]\n---\nWrite.\n")
	write(t, root, "workflows/brief.flow.yaml", "description: A brief\nnodes: [{id: a, type: nope}]\n")
	write(t, root, "evals/cases.md", "| ID | Prompt | Output |\n|---|---|---|\n| a | b | c |\n")
	write(t, root, "skills/Bad Name/SKILL.md", "x")

	found := Validate(root)
	by := map[string]Finding{}
	for _, f := range found {
		by[f.Key] = f
	}
	assert.Contains(t, by["skill/tone"].Warnings, "skills/tone/SKILL.md links to references/gone.md, which it hasn't")
	assert.Contains(t, by["agent/writer"].Warnings[0], "no description")
	assert.NotEmpty(t, by["workflow/brief"].Problems, "a step type Blitz hasn't")
	assert.NotEmpty(t, by["skill/Bad Name"].Problems)
	assert.NotContains(t, by, "eval/cases", "fine")

	wrote, err := Compile(root, true)
	require.NoError(t, err)
	assert.Len(t, wrote, 2)
	var m agentpkg.Manifest
	b, err := os.ReadFile(filepath.Join(root, agentpkg.ManifestFile))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, 1, m.Schema)
	assert.Equal(t, "Our tone", m.Assets["skill/tone"].Description)
	assert.Equal(t, "file://skills/tone/SKILL.md", m.Assets["skill/tone"].SourceURI)
	assert.Equal(t, []agentpkg.Dep{{Name: "tone", Version: "1.x"}}, m.Assets["agent/writer"].Deps)
	var castor map[string]map[string]any
	b, err = os.ReadFile(filepath.Join(root, "skills_manifest.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &castor))
	assert.Contains(t, castor["tone"]["instructions"], "Be kind.")
	assert.Equal(t, map[string]any{"style.md": "Short."}, castor["tone"]["references"])
	assert.Equal(t, m.Assets["skill/tone"].SHA256, castor["tone"]["sha256_hash"])
	assert.Equal(t, "", Description("rule", "no front matter"))
	head, body := frontMatter("---\nunclosed")
	assert.Empty(t, head)
	assert.Equal(t, "---\nunclosed", body)
}
