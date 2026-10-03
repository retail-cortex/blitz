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

package plugins

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The default store is under ~/.blitz.
func TestDefaultStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	assert.Equal(t, filepath.Join(home, ".blitz", "plugins"), Default().Dir)
}

// A store whose index isn't TOML reports it from every operation.
func TestStoreBadIndex(t *testing.T) {
	store := &Store{Dir: files(t, t.TempDir(), map[string]string{indexFile: "not = = toml", marketsFile: "[[x"})}
	_, err := store.List()
	assert.ErrorContains(t, err, indexFile)
	_, err = store.Get("x")
	assert.Error(t, err)
	assert.Error(t, store.SetEnabled("x", true))
	assert.Error(t, store.Remove("x"))
	_, err = store.Install(&Staged{Plugin: &Plugin{Meta: Meta{Name: "x", Version: "1"}}})
	assert.Error(t, err)
	_, problems := Apply(config.DefaultConfig(), store)
	assert.Contains(t, strings.Join(problems, "\n"), indexFile)

	_, err = store.Marketplaces()
	assert.ErrorContains(t, err, marketsFile)
	assert.Error(t, store.RemoveMarketplace("x"))
	_, ok, err := store.FromMarketplace(context.Background(), "x")
	assert.True(t, ok)
	assert.Error(t, err)
	_, err = store.AddMarketplace(context.Background(), files(t, t.TempDir(), map[string]string{IndexFile: "name = \"m\"\n"}))
	assert.Error(t, err)

	_, err = (&Store{Dir: t.TempDir()}).Get("x")
	assert.ErrorIs(t, err, ErrNotInstalled)
}

// zipWith writes an archive whose entries are given in order, with their
// modes.
func zipWith(t *testing.T, entries ...zipEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.zip")
	f, err := os.Create(path)
	require.NoError(t, err)
	w := zip.NewWriter(f)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		h.SetMode(e.mode)
		fw, err := w.CreateHeader(h)
		require.NoError(t, err)
		_, err = fw.Write([]byte(e.data))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
	return path
}

type zipEntry struct {
	name, data string
	mode       os.FileMode
}

// unzip makes the archive's directories, skips its links, and finds the
// plugin at the top or in the one folder; what it can't read or write is
// reported.
func TestUnzip(t *testing.T) {
	archive := zipWith(t,
		zipEntry{name: "kit/", mode: os.ModeDir | 0o755},
		zipEntry{name: "kit/plugin.toml", data: "name = \"kit\"\n", mode: 0o644},
		zipEntry{name: "kit/link", data: "plugin.toml", mode: os.ModeSymlink | 0o777},
	)
	dir := filepath.Join(t.TempDir(), "out")
	root, err := unzip(archive, dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "kit"), root)
	assert.NoFileExists(t, filepath.Join(root, "link"), "links are skipped")

	two := zipWith(t, zipEntry{name: "a/x", mode: 0o644}, zipEntry{name: "b/y", mode: 0o644})
	root, err = unzip(two, filepath.Join(t.TempDir(), "out"))
	require.NoError(t, err)
	assert.Equal(t, "out", filepath.Base(root), "two folders: the top")

	_, err = unzip(filepath.Join(t.TempDir(), "missing.zip"), t.TempDir())
	assert.Error(t, err)

	blocked := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(blocked, "kit"), nil, 0o644))
	_, err = unzip(archive, blocked)
	assert.Error(t, err, "a file where the archive's folder goes")
	_, err = unzip(two, blocked+"/kit")
	assert.Error(t, err, "a file where the archive's root goes")

	isDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(isDir, "a", "x"), 0o755))
	_, err = unzip(two, isDir)
	assert.Error(t, err, "a directory where a file goes")

	// An archive that claims more than the limit unpacked.
	big := filepath.Join(t.TempDir(), "big.zip")
	f, err := os.Create(big)
	require.NoError(t, err)
	w := zip.NewWriter(f)
	fw, err := w.CreateRaw(&zip.FileHeader{Name: "huge", Method: zip.Store, UncompressedSize64: maxBytes + 1, CompressedSize64: 1})
	require.NoError(t, err)
	_, err = fw.Write([]byte("x"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
	_, err = unzip(big, t.TempDir())
	assert.ErrorContains(t, err, "larger than")
}

// Fetch reports downloads that fail, archives that aren't plugins, and
// git sources it can't clone.
func TestFetchFailures(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notzip.zip":
			_, _ = w.Write([]byte("not a zip"))
		case "/empty.zip":
			data, _ := os.ReadFile(zipWith(t, zipEntry{name: "readme", mode: 0o644}))
			_, _ = w.Write(data)
		}
	}))
	defer srv.Close()
	_, err := Fetch(ctx, srv.URL+"/notzip.zip")
	assert.Error(t, err)
	_, err = Fetch(ctx, srv.URL+"/empty.zip")
	assert.ErrorContains(t, err, "not a plugin")
	_, err = Fetch(ctx, "http://bad host/x.zip")
	assert.Error(t, err, "an address that isn't one")
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	_, err = Fetch(ctx, closed.URL+"/x.zip")
	assert.Error(t, err, "nothing answers")

	_, err = Fetch(ctx, filepath.Join(t.TempDir(), "missing.zip"))
	assert.Error(t, err)
	_, err = Fetch(ctx, t.TempDir())
	assert.ErrorContains(t, err, "not a plugin")
}

// download refuses a file it can't create.
func TestDownloadCantCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) }))
	defer srv.Close()
	assert.Error(t, download(context.Background(), srv.URL, filepath.Join(t.TempDir(), "no", "such", "dir")))
}

// copyTree leaves out links and reports what it can't read or write.
func TestCopyTree(t *testing.T) {
	src := files(t, t.TempDir(), map[string]string{"a": "a", "d/b": "b"})
	require.NoError(t, os.Symlink("a", filepath.Join(src, "link")))
	dst := filepath.Join(t.TempDir(), "out")
	require.NoError(t, copyTree(src, dst))
	assert.FileExists(t, filepath.Join(dst, "d", "b"))
	assert.NoFileExists(t, filepath.Join(dst, "link"))

	assert.Error(t, copyTree(filepath.Join(t.TempDir(), "missing"), t.TempDir()))
	blocked := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(blocked, "a"), 0o755)) // a directory where a file goes
	assert.Error(t, copyTree(src, blocked))
	if os.Geteuid() != 0 { // root reads anything
		require.NoError(t, os.Chmod(filepath.Join(src, "a"), 0))
		assert.Error(t, copyTree(src, t.TempDir()), "a file it can't read")
		_, err := Hash(src)
		assert.Error(t, err, "a file it can't hash")
		require.NoError(t, os.Chmod(filepath.Join(src, "a"), 0o644))
	}
}

// Install reports a plugin it can't copy or put in place.
func TestInstallFailures(t *testing.T) {
	st, err := Fetch(context.Background(), sample(t))
	require.NoError(t, err)
	store := &Store{Dir: t.TempDir()}

	missing := &Staged{Plugin: &Plugin{Meta: st.Meta, Dir: filepath.Join(t.TempDir(), "gone")}}
	_, err = store.Install(missing)
	assert.Error(t, err, "nothing to copy")

	// A file where the plugin's folder goes.
	require.NoError(t, os.WriteFile(filepath.Join(store.Dir, st.Name), nil, 0o644))
	_, err = store.Install(st)
	assert.Error(t, err, "a file where the plugin's folder goes")
}

// Hash skips .git and reports a directory that isn't there.
func TestHash(t *testing.T) {
	dir := files(t, t.TempDir(), map[string]string{"a": "a"})
	h1, err := Hash(dir)
	require.NoError(t, err)
	files(t, dir, map[string]string{".git/HEAD": "x"})
	h2, err := Hash(dir)
	require.NoError(t, err)
	assert.Equal(t, h1, h2, ".git doesn't count")
	_, err = Hash(filepath.Join(dir, "missing"))
	assert.Error(t, err)
}

// Read reports a manifest that isn't TOML, a bad version and a bad mcp.toml.
func TestReadErrors(t *testing.T) {
	for name, tree := range map[string]map[string]string{
		"plugin.toml": {Manifest: "name = = x"},
		"version":     {Manifest: "name = \"a\"\nversion = \"1 2\"\n"},
		"mcp.toml":    {Manifest: "name = \"a\"", MCPFile: "[[servers]\n"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Read(files(t, t.TempDir(), tree))
			assert.ErrorContains(t, err, name)
		})
	}
}

// Apply reports an installed plugin that no longer reads, though its hash
// matches, and fills the plugin's root into its MCP servers' environment.
func TestApplyBrokenAndEnv(t *testing.T) {
	store := &Store{Dir: t.TempDir()}
	dir := filepath.Join(store.Dir, "bad", "1")
	files(t, dir, map[string]string{Manifest: "name = \"other\"\nversion = \"1 2\"\n"})
	h, err := Hash(dir)
	require.NoError(t, err)
	require.NoError(t, store.save([]Installed{{Name: "bad", Version: "1", Hash: h, Enabled: true}}))
	loaded, problems := Apply(config.DefaultConfig(), store)
	assert.Empty(t, loaded)
	assert.Contains(t, strings.Join(problems, "\n"), "plugin bad: plugin.toml: version")

	got := rootedServer(config.MCPServerConfig{Env: map[string]string{"ROOT": RootVar + "/x"}}, "/p")
	assert.Equal(t, map[string]string{"ROOT": "/p/x"}, got.Env)
}

// Marketplaces can come from git and over HTTP (relative sources then are
// URLs beside the index); several offering a name must be told apart, and
// one that is gone, or names a source that is, is reported.
func TestMarketplaceSources(t *testing.T) {
	ctx := context.Background()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}
	kitZip, err := os.ReadFile(zipOf(t, map[string]string{"kit/plugin.toml": "name = \"kit\"\n"}))
	require.NoError(t, err)
	staged, err := Fetch(ctx, zipOf(t, map[string]string{"kit/plugin.toml": "name = \"kit\"\n"}))
	require.NoError(t, err)
	hash, err := Hash(staged.Dir)
	require.NoError(t, err)
	staged.Discard()
	index := "name = \"web\"\n[[plugins]]\nname = \"kit\"\nsource = \"kit.zip\"\nhash = \"" + hash + "\"\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/m/" + IndexFile:
			_, _ = w.Write([]byte(index))
		case "/m/kit.zip":
			_, _ = w.Write(kitZip)
		case "/bad/" + IndexFile:
			_, _ = w.Write([]byte("name = \"Not Valid\"\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	store := &Store{Dir: t.TempDir()}
	m, err := store.AddMarketplace(ctx, srv.URL+"/m/"+IndexFile)
	require.NoError(t, err)
	assert.Equal(t, "web", m.Name)
	st, ok, err := store.FromMarketplace(ctx, "kit")
	require.True(t, ok)
	require.NoError(t, err)
	assert.Equal(t, "kit@web", st.Source)
	st.Discard()

	_, err = store.AddMarketplace(ctx, srv.URL+"/bad/"+IndexFile)
	assert.ErrorContains(t, err, "name")
	_, err = store.AddMarketplace(ctx, srv.URL+"/none/"+IndexFile)
	assert.ErrorContains(t, err, "HTTP 404")

	// The same plugin from a git marketplace.
	repo := files(t, filepath.Join(t.TempDir(), "market.git"), map[string]string{
		IndexFile:         "name = \"git-market\"\n[[plugins]]\nname = \"kit\"\nsource = \"kit\"\nhash = \"" + hash + "\"\n[[plugins]]\nname = \"gone\"\nsource = \"gone\"\nhash = \"x\"\n",
		"kit/plugin.toml": "name = \"kit\"\n",
	})
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", "x"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	_, err = store.AddMarketplace(ctx, repo)
	require.NoError(t, err)
	_, _, err = store.FromMarketplace(ctx, "kit")
	assert.ErrorContains(t, err, "several marketplaces offer kit")
	st, _, err = store.FromMarketplace(ctx, "kit@git-market")
	require.NoError(t, err)
	st.Discard()
	_, _, err = store.FromMarketplace(ctx, "gone@git-market")
	assert.ErrorContains(t, err, "not a plugin")
	_, err = store.AddMarketplace(ctx, filepath.Join(t.TempDir(), "nowhere.git"))
	assert.ErrorContains(t, err, "git clone")

	// A marketplace that's gone.
	local := files(t, t.TempDir(), map[string]string{IndexFile: "name = \"local\"\n"})
	_, err = store.AddMarketplace(ctx, local)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(local))
	_, _, err = store.FromMarketplace(ctx, "kit@local")
	assert.ErrorContains(t, err, "marketplace local")
}

// swapIn puts the staged copy in place, and keeps the old one whole when
// it can't.
func TestSwapIn(t *testing.T) {
	cases := map[string]struct {
		old, staged bool
		wantErr     bool
		want        string
	}{
		"new":            {staged: true, want: "new"},
		"replaces":       {old: true, staged: true, want: "new"},
		"restores":       {old: true, wantErr: true, want: "old"},
		"nothing at all": {wantErr: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			dst, staging := filepath.Join(dir, "p"), filepath.Join(dir, "p.new")
			for path, on := range map[string]bool{dst: c.old, staging: c.staged} {
				if on {
					require.NoError(t, os.MkdirAll(path, 0o700))
					content := "old"
					if path == staging {
						content = "new"
					}
					require.NoError(t, os.WriteFile(filepath.Join(path, "f"), []byte(content), 0o600))
				}
			}
			err := swapIn(staging, dst)
			if c.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if c.want != "" {
				b, err := os.ReadFile(filepath.Join(dst, "f"))
				require.NoError(t, err)
				assert.Equal(t, c.want, string(b))
			}
			assert.NoDirExists(t, dst+".old", "the old copy is gone or back in place")
		})
	}
}

// swapIn changes nothing when the old copy can't be moved aside, or a
// stale one from before can't be cleared.
func TestSwapInRefuses(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores permissions")
	}
	cases := map[string]func(t *testing.T, dir, dst string){
		"old copy can't move": func(t *testing.T, dir, _ string) {
			require.NoError(t, os.Chmod(dir, 0o500))
		},
		"stale copy can't be cleared": func(t *testing.T, _, dst string) {
			require.NoError(t, os.MkdirAll(dst+".old", 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dst+".old", "f"), nil, 0o600))
			require.NoError(t, os.Chmod(dst+".old", 0o500))
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			dst, staging := filepath.Join(dir, "p"), filepath.Join(dir, "p.new")
			for path, content := range map[string]string{dst: "old", staging: "new"} {
				require.NoError(t, os.MkdirAll(path, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(path, "f"), []byte(content), 0o600))
			}
			setup(t, dir, dst)
			t.Cleanup(func() { os.Chmod(dir, 0o700); os.Chmod(dst+".old", 0o700) })

			assert.Error(t, swapIn(staging, dst))
			b, err := os.ReadFile(filepath.Join(dst, "f"))
			require.NoError(t, err)
			assert.Equal(t, "old", string(b), "the installed copy is untouched")
		})
	}
}

// writeFile leaves the old file and no temporary behind when writing fails.
func TestStoreWriteFile(t *testing.T) {
	store := &Store{Dir: t.TempDir()}
	require.NoError(t, store.saveMarkets([]Known{{Name: "a", URL: "https://a"}}))
	err := store.writeFile(marketsFile, func(w io.Writer) error {
		io.WriteString(w, "half")
		return errors.New("disk full")
	})
	assert.Error(t, err)
	list, err := store.Marketplaces()
	require.NoError(t, err)
	assert.Equal(t, []Known{{Name: "a", URL: "https://a"}}, list)
	entries, err := os.ReadDir(store.Dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file left")
}
