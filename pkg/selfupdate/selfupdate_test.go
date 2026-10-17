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

package selfupdate

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareVersions(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want int // sign
	}{
		{"v0.2.0", "0.1.9", 1},
		{"v0.1.0", "v0.1.0", 0},
		{"v0.1.0-rc.1", "v0.1.0", -1},
		{"v0.1.0", "v0.1.0-rc.2", 1},
		{"v0.1.0-rc.2", "v0.1.0-rc.1", 1},
		{"v1.0.0", "v0.99.99", 1},
	} {
		t.Run(tt.a+" "+tt.b, func(t *testing.T) {
			got := Compare(tt.a, tt.b)
			assert.Equal(t, tt.want, sign(got))
		})
	}
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

// CheckSum needs the file's line in checksums.txt.
func TestCheckSum(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.tar.gz")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	sums := filepath.Join(dir, "checksums.txt")
	require.NoError(t, os.WriteFile(sums, []byte("abc  other.tar.gz\nmalformed\n"), 0o644))
	assert.ErrorContains(t, CheckSum(sums, file), "a.tar.gz isn't in checksums.txt")
	assert.Error(t, CheckSum(filepath.Join(dir, "missing.txt"), file), "no checksums file")
	require.NoError(t, os.WriteFile(sums, []byte("abc  missing.tar.gz\n"), 0o644))
	assert.Error(t, CheckSum(sums, filepath.Join(dir, "missing.tar.gz")), "no archive")
}

// ExtractPrograms reads Windows' zip archives as well as tarballs, and
// refuses what isn't an archive.
func TestExtractPrograms(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "blitz.zip")
	f, err := os.Create(archive)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	_, err = zw.Create("blitz_x/")
	require.NoError(t, err)
	for _, p := range []string{"blitz_x/" + ProgramName("blitz"), "blitz_x/" + ProgramName("blitzd"), "blitz_x/README.md"} {
		w, err := zw.Create(p)
		require.NoError(t, err)
		fmt.Fprint(w, "new "+filepath.Base(p))
	}
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
	out := t.TempDir()
	files, err := ExtractPrograms(archive, out, "blitz", "blitzd")
	require.NoError(t, err)
	assert.Len(t, files, 2)
	data, err := os.ReadFile(files["blitzd"])
	require.NoError(t, err)
	assert.Equal(t, "new "+ProgramName("blitzd"), string(data))

	for name, content := range map[string]string{"bad.zip": "not a zip", "bad.tar.gz": "not gzip"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(dir, name)
			require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
			_, err := ExtractPrograms(p, out, "blitz", "blitzd")
			assert.Error(t, err)
		})
	}
	t.Run("missing", func(t *testing.T) {
		_, err := ExtractPrograms(filepath.Join(dir, "none.tar.gz"), out, "blitz", "blitzd")
		assert.Error(t, err)
	})
	t.Run("truncated tar", func(t *testing.T) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		gz.Write([]byte("this is not a tar header but long enough to be read as one, almost"))
		gz.Close()
		p := filepath.Join(dir, "trunc.tar.gz")
		require.NoError(t, os.WriteFile(p, buf.Bytes(), 0o644))
		_, err := ExtractPrograms(p, out, "blitz", "blitzd")
		assert.Error(t, err)
	})
	t.Run("unwritable", func(t *testing.T) {
		_, err := ExtractPrograms(archive, filepath.Join(dir, "no", "such"), "blitz", "blitzd")
		assert.Error(t, err)
	})
}

// ReplaceFiles fails cleanly when it can't read or place a new file, and
// then leaves every destination as it was: both programs or neither.
func TestReplaceFiles(t *testing.T) {
	for name, tc := range map[string]struct {
		pairs func(dir string) [][2]string
	}{
		"a missing source": {pairs: func(dir string) [][2]string {
			return [][2]string{{filepath.Join(dir, "src"), filepath.Join(dir, "blitzd")}, {filepath.Join(dir, "missing"), filepath.Join(dir, "blitz")}}
		}},
		"no folder to stage in": {pairs: func(dir string) [][2]string {
			return [][2]string{{filepath.Join(dir, "src"), filepath.Join(dir, "blitzd")}, {filepath.Join(dir, "src"), filepath.Join(dir, "no", "blitz")}}
		}},
		// A folder in the way: staging works, the second program can't be
		// placed after the first was.
		"the second rename fails": {pairs: func(dir string) [][2]string {
			return [][2]string{{filepath.Join(dir, "src"), filepath.Join(dir, "blitzd")}, {filepath.Join(dir, "src"), filepath.Join(dir, "busy")}}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "src"), []byte("new"), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "blitzd"), []byte("old blitzd"), 0o755))
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "busy", "child"), 0o755))
			assert.Error(t, ReplaceFiles(tc.pairs(dir)))
			data, err := os.ReadFile(filepath.Join(dir, "blitzd"))
			require.NoError(t, err)
			assert.Equal(t, "old blitzd", string(data), "the first program is put back")
			for _, left := range []string{"blitzd.new", "blitzd.old", "busy.new", "blitz.new"} {
				assert.NoFileExists(t, filepath.Join(dir, left))
			}
		})
	}
}

// ReplaceFiles puts every new file in place, and leaves nothing beside.
func TestReplaceFilesReplacesAll(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"blitz", "blitzd"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f+".src"), []byte("new "+f), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte("old "+f), 0o755))
	}
	require.NoError(t, ReplaceFiles([][2]string{
		{filepath.Join(dir, "blitzd.src"), filepath.Join(dir, "blitzd")},
		{filepath.Join(dir, "blitz.src"), filepath.Join(dir, "blitz")},
		{filepath.Join(dir, "blitz.src"), filepath.Join(dir, "fresh")},
	}))
	for f, want := range map[string]string{"blitz": "new blitz", "blitzd": "new blitzd", "fresh": "new blitz"} {
		data, err := os.ReadFile(filepath.Join(dir, f))
		require.NoError(t, err)
		assert.Equal(t, want, string(data))
		assert.NoFileExists(t, filepath.Join(dir, f+".old"))
		assert.NoFileExists(t, filepath.Join(dir, f+".new"))
	}
}

// Latest reads the latest release's tag, and says what's wrong otherwise.
func TestLatest(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       int
		want, err  string
	}{
		{"a tag", `{"tag_name": "v1.2.3"}`, 200, "v1.2.3", ""},
		{"no tag", `{}`, 200, "", "no release tag"},
		{"not found", ``, 404, "", "HTTP 404"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/releases/latest", r.URL.Path)
				w.WriteHeader(tc.code)
				fmt.Fprint(w, tc.body)
			}))
			t.Cleanup(srv.Close)
			got, err := Source{API: srv.URL}.Latest(context.Background())
			if tc.err != "" {
				assert.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// GitHub's releases, or a local folder to try an update against.
func TestGitHub(t *testing.T) {
	t.Setenv("BLITZ_UPDATE_URL", "")
	assert.Equal(t, "https://github.com/retail-cortex/blitz/releases/download/v1.0.0/checksums.txt", GitHub().File("v1.0.0", "checksums.txt"))
	t.Setenv("BLITZ_UPDATE_URL", "http://127.0.0.1:8000/")
	assert.Equal(t, Source{API: "http://127.0.0.1:8000/api", Download: "http://127.0.0.1:8000/download"}, GitHub())
}

// What installed a program, when it was a package manager.
func TestPackageManager(t *testing.T) {
	assert.Contains(t, PackageManager("/opt/homebrew/Cellar/blitz/1/bin/blitz"), "Homebrew")
	assert.Contains(t, PackageManager("/usr/bin/blitz"), "package manager")
	assert.Empty(t, PackageManager("/Users/me/.local/bin/blitz"))
}

// Fetch says what stopped it: an address that isn't one, a file it can't
// write, an answer that isn't the file.
func TestFetchFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, "data")
	}))
	t.Cleanup(srv.Close)
	ctx := context.Background()
	assert.Error(t, Fetch(ctx, "http://bad host/x", filepath.Join(t.TempDir(), "x")))
	assert.ErrorContains(t, Fetch(ctx, srv.URL+"/missing", filepath.Join(t.TempDir(), "x")), "HTTP 404")
	assert.Error(t, Fetch(ctx, srv.URL+"/file", filepath.Join(t.TempDir(), "no", "such", "x")))
	to := filepath.Join(t.TempDir(), "x")
	require.NoError(t, Fetch(ctx, srv.URL+"/file", to))
	data, err := os.ReadFile(to)
	require.NoError(t, err)
	assert.Equal(t, "data", string(data))
}
