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

package skillreg

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// archive is a skill's .zip of files.
func archive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := z.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	return buf.Bytes()
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// registry serves an index and its archives, and records the
// Authorization header each request had.
type registry struct {
	*httptest.Server
	mu    sync.Mutex
	index Index
	files map[string][]byte
	auth  map[string]string
}

func newRegistry(t *testing.T) *registry {
	r := &registry{files: map[string][]byte{}, auth: map[string]string{}}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.auth[req.URL.Path] = req.Header.Get("Authorization")
		if req.URL.Path == "/index.json" {
			_ = json.NewEncoder(w).Encode(r.index)
			return
		}
		if b, ok := r.files[req.URL.Path]; ok {
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(r.Close)
	return r
}

// offer adds a skill to the index, its archive at /<name>-<version>.zip.
func (r *registry) offer(name, version string, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := "/" + name + "-" + version + ".zip"
	r.files[p] = data
	r.index.Skills = append(slicesDelete(r.index.Skills, name), Entry{Name: name, Description: "does " + name, Version: version, ArchiveURL: strings.TrimPrefix(p, "/"), SHA256: sum(data)})
}

func slicesDelete(es []Entry, name string) []Entry {
	var out []Entry
	for _, e := range es {
		if e.Name != name {
			out = append(out, e)
		}
	}
	return out
}

func newStore(t *testing.T) *Store {
	dir := t.TempDir()
	return &Store{File: filepath.Join(dir, "skill-registries.toml"), UserSkills: filepath.Join(dir, "skills"), Secrets: &secrets.Memory{}}
}

func skillMD(name string) string {
	return "---\nname: " + name + "\ndescription: x\n---\nDo it.\n"
}

func TestRegistryLifecycle(t *testing.T) {
	ctx := context.Background()
	reg := newRegistry(t)
	reg.index = Index{Version: 1, Name: "Castor"}
	reg.offer("lit-review", "1.0.0", archive(t, map[string]string{"lit-review/SKILL.md": skillMD("lit-review"), "lit-review/scripts/run.py": "print(1)"}))
	reg.offer("stats", "0.1.0", archive(t, map[string]string{"SKILL.md": skillMD("stats")}))
	s := newStore(t)

	idx, err := s.Add(ctx, "", reg.URL+"/index.json", "s3cret")
	require.NoError(t, err)
	assert.Equal(t, "Castor", idx.Name)
	list, err := s.Registries()
	require.NoError(t, err)
	assert.Equal(t, []Registry{{Name: "castor", URL: reg.URL + "/index.json"}}, list, "named from the index")
	tok, err := s.Secrets.Get("skill-registry:castor")
	require.NoError(t, err)
	assert.Equal(t, "s3cret", tok, "the token is in the keychain")
	data, err := os.ReadFile(s.File)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "s3cret", "never in the file")

	found, err := s.Search(ctx, "LIT")
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "castor", found[0].Registry)
	all, err := s.Search(ctx, "")
	require.NoError(t, err)
	assert.Len(t, all, 2)

	i, err := s.Install(ctx, "lit-review", "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(s.UserSkills, "lit-review"), i.Dir)
	assert.FileExists(t, filepath.Join(i.Dir, "SKILL.md"), "its folder unwrapped")
	assert.FileExists(t, filepath.Join(i.Dir, "scripts", "run.py"))
	assert.Equal(t, "Bearer s3cret", reg.auth["/index.json"])
	assert.Equal(t, "Bearer s3cret", reg.auth["/lit-review-1.0.0.zip"], "the token goes to the registry's host")

	project := t.TempDir()
	p, err := s.Install(ctx, "castor/stats", project)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(project, ".agents", "skills", "stats"), p.Dir)
	installed, err := s.Installed()
	require.NoError(t, err)
	assert.Len(t, installed, 2)

	// A new version: update reinstalls it; the other is up to date.
	reg.offer("lit-review", "1.1.0", archive(t, map[string]string{"SKILL.md": skillMD("lit-review") + "v2"}))
	reg.offer("stats", "0.2.0", archive(t, map[string]string{"SKILL.md": skillMD("stats") + "v2"}))
	done, err := s.Update(ctx, "lit-review")
	require.NoError(t, err)
	require.Len(t, done, 1)
	assert.Equal(t, "1.1.0", done[0].Version)
	md, _ := os.ReadFile(filepath.Join(i.Dir, "SKILL.md"))
	assert.Contains(t, string(md), "v2")
	assert.NoFileExists(t, filepath.Join(i.Dir, "scripts", "run.py"), "the old version's files are gone")
	done, err = s.Update(ctx)
	require.NoError(t, err)
	require.Len(t, done, 1, "the project's skill updates in its project")
	assert.Equal(t, p.Dir, done[0].Dir)
	done, err = s.Update(ctx)
	require.NoError(t, err)
	assert.Empty(t, done, "all up to date")
	_, err = s.Update(ctx, "nope")
	assert.ErrorIs(t, err, ErrNotInstalled)

	_, err = s.Uninstall("stats", project)
	require.NoError(t, err)
	assert.NoDirExists(t, p.Dir)
	_, err = s.Uninstall("stats", project)
	assert.ErrorIs(t, err, ErrNotInstalled)

	require.NoError(t, s.Remove("castor"))
	_, err = s.Secrets.Get("skill-registry:castor")
	assert.Error(t, err, "its token goes with it")
	assert.DirExists(t, i.Dir, "installed skills stay")
	assert.ErrorIs(t, s.Remove("castor"), ErrNoRegistry)
}

func TestInstallRefusals(t *testing.T) {
	ctx := context.Background()
	reg := newRegistry(t)
	reg.index = Index{Version: 1, Name: "r"}
	good := archive(t, map[string]string{"SKILL.md": skillMD("ok")})
	reg.offer("ok", "1", good)
	reg.offer("escape", "1", archive(t, map[string]string{"SKILL.md": skillMD("escape"), "../../evil": "x"}))
	reg.offer("misnamed", "1", archive(t, map[string]string{"SKILL.md": skillMD("other")}))
	reg.offer("empty", "1", archive(t, map[string]string{"README.md": "no skill"}))
	reg.offer("notzip", "1", []byte("plain text"))
	reg.offer("tampered", "1", good)
	reg.files["/tampered-1.zip"] = archive(t, map[string]string{"SKILL.md": skillMD("tampered") + "evil"})
	reg.index.Skills = append(reg.index.Skills, Entry{Name: "nohash", Version: "1", ArchiveURL: "ok-1.zip"}, Entry{Name: "Bad Name", Version: "1", ArchiveURL: "ok-1.zip", SHA256: sum(good)})

	s := newStore(t)
	_, err := s.Add(ctx, "r", reg.URL+"/index.json", "")
	require.NoError(t, err)
	tests := []struct{ ref, want string }{
		{"escape", "leaves the archive"},
		{"misnamed", `doesn't name the skill "misnamed"`},
		{"empty", "no SKILL.md"},
		{"notzip", "not a .zip"},
		{"tampered", "doesn't match the hash"},
		{"nohash", "gives no SHA-256"},
		{"Bad Name", "isn't a valid name"},
		{"missing", "no registry offers"},
		{"other/ok", "no such registry"},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			_, err := s.Install(ctx, tt.ref, "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.NoFileExists(t, filepath.Join(filepath.Dir(s.UserSkills), "evil"))
		})
	}
	entries, _ := os.ReadDir(s.UserSkills)
	assert.Empty(t, entries, "nothing left behind")

	// A skill folder it didn't install is never replaced.
	require.NoError(t, os.MkdirAll(filepath.Join(s.UserSkills, "ok"), 0o755))
	_, err = s.Install(ctx, "ok", "")
	assert.ErrorIs(t, err, ErrExists)

	// Two registries offering a name: say which.
	_, err = s.Add(ctx, "r2", reg.URL+"/index.json", "")
	require.NoError(t, err)
	_, err = s.Install(ctx, "ok", "")
	assert.ErrorIs(t, err, ErrAmbiguous)
}

func TestRegistryURLsAndTokens(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for _, bad := range []string{"http://example.com/index.json", "ftp://example.com/i.json"} {
		_, err := s.Add(ctx, "x", bad, "")
		assert.Error(t, err, bad)
	}

	// A local registry: an index file with archives beside it.
	dir := t.TempDir()
	data := archive(t, map[string]string{"SKILL.md": skillMD("local")})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "local.zip"), data, 0o644))
	idx, _ := json.Marshal(Index{Version: 1, Name: "Mine", Skills: []Entry{{Name: "local", Version: "1", ArchiveURL: "local.zip", SHA256: sum(data)}}})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"), idx, 0o644))
	_, err := s.Add(ctx, "", filepath.Join(dir, "index.json"), "")
	require.NoError(t, err)
	i, err := s.Install(ctx, "mine/local", "")
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(i.Dir, "SKILL.md"))

	// A wrong version, and not JSON.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "v2.json"), []byte(`{"version":2}`), 0o644))
	_, err = s.Add(ctx, "", filepath.Join(dir, "v2.json"), "")
	assert.ErrorContains(t, err, "index version 2")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`<html>`), 0o644))
	_, err = s.Add(ctx, "", filepath.Join(dir, "bad.json"), "")
	assert.ErrorContains(t, err, "not a skills index")

	// The token never leaves the registry's host.
	other := newRegistry(t)
	other.files["/far.zip"] = data
	reg := newRegistry(t)
	reg.index = Index{Version: 1, Name: "r", Skills: []Entry{{Name: "local", Version: "1", ArchiveURL: other.URL + "/far.zip", SHA256: sum(data)}}}
	_, err = s.Add(ctx, "tok", reg.URL+"/index.json?key=abc", "t0k")
	require.NoError(t, err)
	project := t.TempDir()
	_, err = s.Install(ctx, "tok/local", project)
	require.NoError(t, err)
	assert.Equal(t, "Bearer t0k", reg.auth["/index.json"])
	assert.Empty(t, other.auth["/far.zip"], "not sent to another host")

	// Errors name the URL without its query.
	reg.index.Skills[0].ArchiveURL = "/gone.zip"
	_, err = s.Update(ctx)
	assert.NoError(t, err, "same hash: nothing to do")
	reg.index.Skills[0].SHA256 = sum([]byte("new"))
	_, err = s.Update(ctx)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "key=abc")
	assert.Contains(t, err.Error(), "HTTP 404")

	// A registry that can't be read is reported; the others still answer.
	reg.Close()
	found, err := s.Search(ctx, "local")
	assert.Error(t, err)
	assert.NotEmpty(t, found, "the local registry still answers")
}

// The limits, unusual archives, and a broken settings file.
func TestLimitsAndOddArchives(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, data, 0o644))
		return p
	}
	offer := func(entries ...Entry) string {
		b, _ := json.Marshal(Index{Version: 1, Name: "Local", Skills: entries})
		return write("index.json", b)
	}
	// Folders, a link, and the archive given by an absolute path.
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	_, _ = z.Create("odd/")
	f, _ := z.Create("odd/SKILL.md")
	_, _ = f.Write([]byte(skillMD("odd")))
	_, _ = z.Create("odd/data/")
	lh := &zip.FileHeader{Name: "odd/link", Method: zip.Store}
	lh.SetMode(os.ModeSymlink | 0o777)
	link, _ := z.CreateHeader(lh)
	_, _ = link.Write([]byte("/etc/passwd"))
	require.NoError(t, z.Close())
	odd := buf.Bytes()
	abs := write("odd.zip", odd)
	big := archive(t, map[string]string{"SKILL.md": skillMD("big"), "data.bin": strings.Repeat("x", 4096)})
	write("big.zip", big)
	many := map[string]string{"SKILL.md": skillMD("many")}
	for i := range 5 {
		many[filepath.Join("f", string(rune('a'+i)))] = "x"
	}
	manyZip := archive(t, many)
	write("many.zip", manyZip)
	index := offer(
		Entry{Name: "odd", Version: "1", ArchiveURL: abs, SHA256: sum(odd)},
		Entry{Name: "big", Version: "1", ArchiveURL: "big.zip", SHA256: sum(big)},
		Entry{Name: "many", Version: "1", ArchiveURL: "many.zip", SHA256: sum(manyZip)},
	)

	s := newStore(t)
	_, err := s.Add(ctx, "Bad Name", index, "")
	assert.ErrorContains(t, err, "use lowercase letters")
	_, err = s.Add(ctx, "", index, "")
	require.NoError(t, err)
	i, err := s.Install(ctx, "odd", "")
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(i.Dir, "SKILL.md"))
	assert.NoFileExists(t, filepath.Join(i.Dir, "link"), "links aren't unpacked")

	saved := []int64{maxArchive, maxUnpack}
	savedFiles := maxFiles
	t.Cleanup(func() { maxArchive, maxUnpack, maxFiles = saved[0], saved[1], savedFiles })
	maxUnpack = 1024
	_, err = s.Install(ctx, "big", "")
	assert.ErrorContains(t, err, "unpacked")
	maxUnpack, maxArchive = saved[1], 100
	_, err = s.Install(ctx, "big", "")
	assert.ErrorContains(t, err, "larger than")
	maxArchive, maxFiles = saved[0], 3
	_, err = s.Install(ctx, "many", "")
	assert.ErrorContains(t, err, "more than 3 files")
	maxFiles = savedFiles

	// Update: a registry gone, a skill no longer offered, an index unreadable.
	_, err = s.Install(ctx, "many", "")
	require.NoError(t, err)
	offer(Entry{Name: "odd", Version: "2", ArchiveURL: abs, SHA256: sum(odd)})
	_, err = s.Update(ctx)
	assert.ErrorContains(t, err, "no longer offers it")
	write("index.json", []byte("{"))
	_, err = s.Update(ctx)
	assert.ErrorContains(t, err, "not a skills index")
	_, err = s.Install(ctx, "local/odd", "")
	assert.ErrorContains(t, err, "registry local")
	require.NoError(t, s.Remove("local"))
	_, err = s.Update(ctx)
	assert.ErrorIs(t, err, ErrNoRegistry)

	// A settings file that isn't TOML stops everything, saying so.
	require.NoError(t, os.WriteFile(s.File, []byte("[[registries"), 0o600))
	_, err = s.Registries()
	assert.ErrorContains(t, err, "skill-registries.toml")
	_, err = s.Installed()
	assert.Error(t, err)
	_, err = s.Search(ctx, "")
	assert.Error(t, err)
	_, err = s.Install(ctx, "x", "")
	assert.Error(t, err)
	_, err = s.Uninstall("x", "")
	assert.Error(t, err)
	assert.Error(t, s.Remove("x"))
	_, err = s.Update(ctx)
	assert.Error(t, err)
	_, err = s.Add(ctx, "", filepath.Join(dir, "missing.json"), "")
	assert.Error(t, err)
}

func TestHelpers(t *testing.T) {
	assert.Equal(t, "https://h.example/a/b.zip", must(resolve("https://h.example/a/index.json", "b.zip")))
	assert.Equal(t, "https://cdn.example/b.zip", must(resolve("https://h.example/index.json", "https://cdn.example/b.zip")))
	assert.Equal(t, "/x/b.zip", must(resolve("/x/index.json", "/x/b.zip")))
	assert.Equal(t, "https://h.example/p", redact("https://user:pw@h.example/p?token=1"))
	assert.Equal(t, "::not a url", redact("::not a url"))
	assert.True(t, sameHost("https://h.example/a", "https://h.example/b"))
	assert.False(t, sameHost("https://h.example/a", "http://h.example/a"))
	assert.NoError(t, checkURL(`C:\registry\index.json`), "a Windows path")
	assert.NoError(t, checkURL("http://localhost:8080/i.json"))
	assert.False(t, (&Store{}).HasToken("x"), "no keychain: no token")
	assert.NotNil(t, (&Store{}).client())
	d := Default()
	assert.Equal(t, "skill-registries.toml", filepath.Base(d.File))
}

func must(s string, err error) string {
	if err != nil {
		panic(err)
	}
	return s
}

// failingKeys is a keychain that refuses to keep anything.
type failingKeys struct{ secrets.Memory }

func (failingKeys) Set(string, string) error { return errors.New("keychain locked") }

func TestStoreFailures(t *testing.T) {
	ctx := context.Background()
	reg := newRegistry(t)
	reg.index = Index{Version: 1, Name: "r"}
	reg.offer("ok", "1", archive(t, map[string]string{"SKILL.md": skillMD("ok")}))

	s := newStore(t)
	s.Secrets = &failingKeys{}
	s.Client = &http.Client{}
	_, err := s.Add(ctx, "", reg.URL+"/index.json", "tok")
	assert.ErrorContains(t, err, "keychain locked")

	// The settings folder can't be written.
	s = newStore(t)
	require.NoError(t, os.Chmod(filepath.Dir(s.File), 0o500))
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(s.File), 0o700) })
	_, err = s.Add(ctx, "", reg.URL+"/index.json", "")
	assert.Error(t, err)

	// The settings file's folder is a file.
	s = newStore(t)
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(s.File), "f"), nil, 0o600))
	s.File = filepath.Join(filepath.Dir(s.File), "f", "r.toml")
	assert.Error(t, s.save(state{}))

	// A URL that can't be requested, and a registry that stopped answering.
	_, err = s.get(ctx, "https://h.example/\x7f", "", "", 10)
	assert.Error(t, err)
	s = newStore(t)
	_, err = s.Add(ctx, "", reg.URL+"/index.json", "")
	require.NoError(t, err)
	reg.Close()
	_, err = s.Install(ctx, "ok", "")
	assert.Error(t, err)

	_, err = resolve("https://h.example/%zz/index.json", "a.zip")
	assert.Error(t, err)
	_, err = resolve("https://h.example/index.json", "%zz")
	assert.Error(t, err)
}
