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
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pkg = `{
  "name": "blitz-vscode",
  "displayName": "Blitz",
  "description": "The agent <in> VS Code",
  "publisher": "retail-cortex",
  "version": "0.1.0",
  "categories": ["AI", "Other"],
  "engines": { "vscode": "^1.95.0" },
  "repository": { "url": "https://github.com/retail-cortex/blitz" },
  "devDependencies": { "typescript": "5.9.3" }
}
`

// unzip reads an archive's files.
func unzip(t *testing.T, path string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer r.Close()
	out := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		require.NoError(t, err)
		b, err := io.ReadAll(rc)
		rc.Close()
		require.NoError(t, err)
		out[f.Name] = string(b)
	}
	return out
}

func TestPack(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
		return p
	}
	pj := write("package.json", pkg)
	write("dist/extension.js", "exports.activate = () => {}")
	write("page/assets/index.css", "body{}")
	readme := write("README.md", "# Blitz")
	license := write("LICENSE", "Apache")
	out := filepath.Join(dir, "blitz.vsix")

	require.NoError(t, run(pj, out, "v1.2.3", adds{
		"extension/dist=" + filepath.Join(dir, "dist"),
		"extension/media/page=" + filepath.Join(dir, "page"),
		"extension/README.md=" + readme,
		"extension/LICENSE.txt=" + license,
	}))
	files := unzip(t, out)
	for _, name := range []string{"extension.vsixmanifest", "[Content_Types].xml", "extension/package.json", "extension/dist/extension.js", "extension/media/page/assets/index.css", "extension/README.md", "extension/LICENSE.txt"} {
		assert.Contains(t, files, name)
	}
	m := files["extension.vsixmanifest"]
	for _, want := range []string{`Id="blitz-vscode"`, `Version="1.2.3"`, `Publisher="retail-cortex"`, "<DisplayName>Blitz</DisplayName>", "The agent &lt;in&gt; VS Code", `Value="^1.95.0"`, "<Categories>AI,Other</Categories>", "extension/README.md", "<License>extension/LICENSE.txt</License>", "Links.Source"} {
		assert.Contains(t, m, want)
	}
	assert.Contains(t, files["extension/package.json"], `"version": "1.2.3"`, "the version is stamped")
	assert.Contains(t, files["extension/package.json"], `"typescript": "5.9.3"`, "the rest is as written")
	types := files["[Content_Types].xml"]
	for _, want := range []string{`Extension=".js"`, `Extension=".css" ContentType="text/css"`, `Extension=".vsixmanifest"`, `Extension=".md"`, `Extension=".txt"`} {
		assert.Contains(t, types, want)
	}
	assert.False(t, strings.Contains(types, `Extension=""`))

	// The same inputs make the same archive.
	again := filepath.Join(dir, "again.vsix")
	require.NoError(t, run(pj, again, "v1.2.3", adds{"extension/dist=" + filepath.Join(dir, "dist"), "extension/media/page=" + filepath.Join(dir, "page"), "extension/README.md=" + readme, "extension/LICENSE.txt=" + license}))
	a, _ := os.ReadFile(out)
	b, _ := os.ReadFile(again)
	assert.Equal(t, a, b, "the archive isn't reproducible")
}

func TestPackRefuses(t *testing.T) {
	dir := t.TempDir()
	pj := filepath.Join(dir, "package.json")
	require.NoError(t, os.WriteFile(pj, []byte(pkg), 0o644))
	bad := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte(`{"name":"x"}`), 0o644))
	tests := []struct {
		name, pkg, out string
		add            adds
		err            string
	}{
		{"no out", pj, "", nil, "required"},
		{"missing fields", bad, filepath.Join(dir, "x.vsix"), nil, "engines.vscode"},
		{"no source", pj, filepath.Join(dir, "x.vsix"), adds{"extension/a=" + filepath.Join(dir, "missing")}, "missing"},
		{"not a pair", pj, filepath.Join(dir, "x.vsix"), adds{"extension/a"}, "archive/path=source"},
		{"twice", pj, filepath.Join(dir, "x.vsix"), adds{"extension/package.json=" + pj}, "added twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(tt.pkg, tt.out, "", tt.add)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.err)
		})
	}
}
