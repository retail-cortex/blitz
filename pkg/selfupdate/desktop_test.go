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
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindInstall(t *testing.T) {
	for _, tc := range []struct {
		name, exe, goos string
		want            Install
		err             error
	}{
		{"macOS", "/Applications/Blitz.app/Contents/Resources/app/bin/blitz-tray", "darwin", Install{Root: "/Applications/Blitz.app", Program: "/Applications/Blitz.app"}, nil},
		{"Windows", `/Users/me/Blitz/resources/app/bin/blitz-tray.exe`, "windows", Install{Root: "/Users/me/Blitz", Program: "/Users/me/Blitz/Blitz.exe"}, nil},
		{"Linux, a tarball", "/home/me/blitz-desktop/resources/app/bin/blitz-tray", "linux", Install{Root: "/home/me/blitz-desktop", Program: "/home/me/blitz-desktop/blitz-desktop"}, nil},
		{"Linux, a package", "/usr/lib/blitz-desktop/resources/app/bin/blitz-tray", "linux", Install{Root: "/usr/lib/blitz-desktop", Program: "/usr/lib/blitz-desktop/blitz-desktop", Manager: "your system's package manager"}, nil},
		{"a development build", "/work/bazel-bin/apps/tray/blitz-tray", "darwin", Install{}, ErrNotInstalled},
		{"macOS, not in a bundle", "/tmp/x/Resources/app/bin/blitz-tray", "darwin", Install{}, ErrNotInstalled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FindInstall(tc.exe, tc.goos)
			assert.ErrorIs(t, err, tc.err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDesktopAsset(t *testing.T) {
	for _, tc := range []struct{ goos, goarch, want string }{
		{"darwin", "arm64", "Blitz_0.6.6_macos_arm64.dmg"},
		{"darwin", "amd64", "Blitz_0.6.6_macos_x64.dmg"},
		{"windows", "amd64", "Blitz_0.6.6_windows_x64.zip"},
		{"linux", "arm64", "blitz-desktop_0.6.6_linux_arm64.tar.gz"},
	} {
		got, err := DesktopAsset(tc.goos, tc.goarch, "0.6.6")
		require.NoError(t, err)
		assert.Equal(t, tc.want, got)
	}
	_, err := DesktopAsset("windows", "arm64", "0.6.6")
	assert.Error(t, err)
}

// zipOf and tarOf make packages from name → content ("->target" makes a
// link in a tarball).
func zipOf(t *testing.T, files map[string]string) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, body := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		fmt.Fprint(w, body)
	}
	require.NoError(t, zw.Close())
	return b.Bytes()
}

func tarOf(t *testing.T, files map[string]string) []byte {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "blitz-desktop/", Mode: 0o755, Typeflag: tar.TypeDir}))
	for name, body := range files {
		if link, ok := strings.CutPrefix(body, "->"); ok {
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Linkname: link, Typeflag: tar.TypeSymlink}))
			continue
		}
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o4755, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return b.Bytes()
}

// serveRelease serves files as tag v9.9.9's, and returns its source.
func serveRelease(t *testing.T, files map[string][]byte) Source {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return Source{API: srv.URL + "/api", Download: srv.URL + "/download"}
}

// fakeVerify makes the signature check answer err, and records what it
// was given.
func fakeVerify(t *testing.T, err error) *string {
	var got string
	old := VerifyPackage
	t.Cleanup(func() { VerifyPackage = old })
	VerifyPackage = func(_ context.Context, r io.Reader, bundle []byte, _ string) error {
		data, _ := io.ReadAll(r)
		got = string(data[:min(len(data), 2)]) + "|" + string(bundle)
		return err
	}
	return &got
}

func TestStage(t *testing.T) {
	ctx := context.Background()
	t.Run("Windows: the zip, checked against its own bundle", func(t *testing.T) {
		got := fakeVerify(t, nil)
		pkg := zipOf(t, map[string]string{"Blitz/Blitz.exe": "new", "Blitz/resources/app/package.json": "{}"})
		src := serveRelease(t, map[string][]byte{"Blitz_9.9.9_windows_x64.zip": pkg, "Blitz_9.9.9_windows_x64.zip.sigstore.json": []byte("bundle")})
		root, err := Stage(ctx, src, "v9.9.9", "windows", "amd64", t.TempDir(), "")
		require.NoError(t, err)
		assert.Equal(t, "PK|bundle", *got, "the package's own bytes and bundle")
		data, err := os.ReadFile(filepath.Join(root, "Blitz.exe"))
		require.NoError(t, err)
		assert.Equal(t, "new", string(data))
	})
	t.Run("Linux: the tarball, links kept", func(t *testing.T) {
		fakeVerify(t, nil)
		pkg := tarOf(t, map[string]string{"blitz-desktop/blitz-desktop": "new", "blitz-desktop/libx.so.1": "lib", "blitz-desktop/libx.so": "->libx.so.1"})
		src := serveRelease(t, map[string][]byte{"blitz-desktop_9.9.9_linux_amd64.tar.gz": pkg, "blitz-desktop_9.9.9_linux_amd64.tar.gz.sigstore.json": []byte("b")})
		root, err := Stage(ctx, src, "v9.9.9", "linux", "amd64", t.TempDir(), "")
		require.NoError(t, err)
		info, err := os.Stat(filepath.Join(root, "blitz-desktop"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "no setuid without root")
		link, err := os.Readlink(filepath.Join(root, "libx.so"))
		require.NoError(t, err)
		assert.Equal(t, "libx.so.1", link)
	})
	t.Run("macOS: Blitz.app out of the disk image", func(t *testing.T) {
		fakeVerify(t, nil)
		old := unpackDMG
		t.Cleanup(func() { unpackDMG = old })
		unpackDMG = func(_ context.Context, dmg, dst string) error {
			assert.True(t, strings.HasSuffix(dmg, "Blitz_9.9.9_macos_arm64.dmg"))
			return os.MkdirAll(filepath.Join(dst, "Contents"), 0o755)
		}
		src := serveRelease(t, map[string][]byte{"Blitz_9.9.9_macos_arm64.dmg": []byte("dmg"), "Blitz_9.9.9_macos_arm64.dmg.sigstore.json": []byte("b")})
		root, err := Stage(ctx, src, "v9.9.9", "darwin", "arm64", t.TempDir(), "")
		require.NoError(t, err)
		assert.DirExists(t, filepath.Join(root, "Contents"))
	})
	t.Run("a signature that doesn't verify unpacks nothing", func(t *testing.T) {
		fakeVerify(t, errors.New("the release's signature doesn't verify"))
		src := serveRelease(t, map[string][]byte{"Blitz_9.9.9_windows_x64.zip": zipOf(t, nil), "Blitz_9.9.9_windows_x64.zip.sigstore.json": []byte("b")})
		dir := t.TempDir()
		_, err := Stage(ctx, src, "v9.9.9", "windows", "amd64", dir, "")
		assert.ErrorContains(t, err, "nothing was installed")
		assert.NoDirExists(t, filepath.Join(dir, "new"))
	})
	t.Run("no package for this machine, or none downloaded", func(t *testing.T) {
		fakeVerify(t, nil)
		_, err := Stage(ctx, Source{}, "v9.9.9", "plan9", "amd64", t.TempDir(), "")
		assert.ErrorContains(t, err, "no desktop package")
		_, err = Stage(ctx, serveRelease(t, nil), "v9.9.9", "windows", "amd64", t.TempDir(), "")
		assert.ErrorContains(t, err, "HTTP 404")
	})
}

// A package can't put anything outside its folder.
func TestUnpackStaysInside(t *testing.T) {
	dir := t.TempDir()
	evilZip := filepath.Join(dir, "evil.zip")
	require.NoError(t, os.WriteFile(evilZip, zipOf(t, map[string]string{"../escape": "x"}), 0o600))
	assert.ErrorContains(t, unzip(evilZip, filepath.Join(dir, "out")), "outside it")
	evilTar := filepath.Join(dir, "evil.tar.gz")
	require.NoError(t, os.WriteFile(evilTar, tarOf(t, map[string]string{"blitz-desktop/x": "->../../../etc/passwd"}), 0o600))
	assert.ErrorContains(t, untar(evilTar, filepath.Join(dir, "out2")), "link outside it")
	abs := filepath.Join(dir, "abs.tar.gz")
	require.NoError(t, os.WriteFile(abs, tarOf(t, map[string]string{"/etc/x": "y"}), 0o600))
	assert.ErrorContains(t, untar(abs, filepath.Join(dir, "out3")), "outside it")
	assert.NoFileExists(t, filepath.Join(dir, "escape"))
}

func TestApply(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "Blitz")
	staged := filepath.Join(dir, "updates", "new", "Blitz")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Blitz.exe"), []byte("old"), 0o755))
	require.NoError(t, os.MkdirAll(staged, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(staged, "Blitz.exe"), []byte("new"), 0o755))

	require.NoError(t, Apply(staged, root))
	data, err := os.ReadFile(filepath.Join(root, "Blitz.exe"))
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
	assert.NoDirExists(t, root+".old", "the old one removed")

	assert.ErrorContains(t, Apply(staged, root), "the update isn't there")
	data, _ = os.ReadFile(filepath.Join(root, "Blitz.exe"))
	assert.Equal(t, "new", string(data), "left as it was")
	assert.ErrorContains(t, Apply(root, filepath.Join(dir, "missing")), "moving the app aside")
}

// An update that can be neither moved nor copied in leaves the app as it
// was: put back from where it was moved aside.
func TestApplyPutsTheAppBack(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permissions don't stop root")
	}
	dir := t.TempDir()
	root := filepath.Join(dir, "Blitz")
	updates := filepath.Join(dir, "updates")
	staged := filepath.Join(updates, "Blitz")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Blitz.exe"), []byte("old"), 0o755))
	require.NoError(t, os.MkdirAll(staged, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(staged, "Blitz.exe"), nil, 0o000))
	require.NoError(t, os.Chmod(updates, 0o555)) // no moving it out
	t.Cleanup(func() { os.Chmod(updates, 0o755) })

	assert.ErrorContains(t, Apply(staged, root), "putting the update in place")
	data, err := os.ReadFile(filepath.Join(root, "Blitz.exe"))
	require.NoError(t, err)
	assert.Equal(t, "old", string(data))
	assert.NoDirExists(t, root+".old")
}

// Across disks the update is copied in: folders, files and links.
func TestCopyTree(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "a", "f"), []byte("x"), 0o750))
	require.NoError(t, os.Symlink("a/f", filepath.Join(src, "l")))
	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, copyTree(src, dst))
	info, err := os.Stat(filepath.Join(dst, "a", "f"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750), info.Mode().Perm())
	link, err := os.Readlink(filepath.Join(dst, "l"))
	require.NoError(t, err)
	assert.Equal(t, "a/f", link)
	assert.Error(t, copyTree(filepath.Join(t.TempDir(), "none"), dst))
}

// On macOS, Blitz.app comes out of a real disk image (hdiutil).
func TestHdiutilCopy(t *testing.T) {
	if _, err := exec.LookPath("hdiutil"); err != nil {
		t.Skip("macOS's hdiutil")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "Blitz.app", "Contents"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "Blitz.app", "Contents", "Info.plist"), []byte("plist"), 0o644))
	dmg := filepath.Join(dir, "Blitz.dmg")
	if out, err := exec.Command("hdiutil", "create", "-quiet", "-srcfolder", src, "-format", "UDZO", "-ov", dmg).CombinedOutput(); err != nil {
		t.Skipf("hdiutil can't make an image here: %s", out)
	}
	dst := filepath.Join(dir, "out", "Blitz.app")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, hdiutilCopy(context.Background(), dmg, dst))
	data, err := os.ReadFile(filepath.Join(dst, "Contents", "Info.plist"))
	require.NoError(t, err)
	assert.Equal(t, "plist", string(data))
	assert.ErrorContains(t, hdiutilCopy(context.Background(), filepath.Join(dir, "none.dmg"), dst), "opening the disk image")
}

// The disk image's copy, its programs replaced: hdiutil attaches it, ditto
// copies Blitz.app, hdiutil lets it go, whatever happened.
func TestHdiutilCopySteps(t *testing.T) {
	old := command
	t.Cleanup(func() { command = old })
	for _, tc := range []struct {
		name, fail, err string
	}{
		{"copied", "", ""},
		{"an image that won't open", "hdiutil attach", "opening the disk image"},
		{"a copy that fails", "ditto", "copying Blitz.app"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ran []string
			command = func(_ context.Context, name string, args ...string) ([]byte, error) {
				ran = append(ran, name+" "+args[0])
				if tc.fail != "" && strings.HasPrefix(name+" "+args[0], tc.fail) {
					return []byte("no"), errors.New("exit 1")
				}
				return nil, nil
			}
			err := hdiutilCopy(context.Background(), "x.dmg", filepath.Join(t.TempDir(), "Blitz.app"))
			if tc.err == "" {
				require.NoError(t, err)
				assert.Equal(t, []string{"hdiutil attach", "ditto " + ran[1][len("ditto "):], "hdiutil detach"}, ran)
				return
			}
			assert.ErrorContains(t, err, tc.err)
		})
	}
}

// What can go wrong unpacking or staging says so.
func TestStageAndUnpackFailures(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	notZip := filepath.Join(dir, "bad.zip")
	require.NoError(t, os.WriteFile(notZip, []byte("not a zip"), 0o600))
	assert.Error(t, unzip(notZip, filepath.Join(dir, "o1")))
	notTar := filepath.Join(dir, "bad.tar.gz")
	require.NoError(t, os.WriteFile(notTar, []byte("not gzip"), 0o600))
	assert.Error(t, untar(notTar, filepath.Join(dir, "o2")))
	assert.Error(t, untar(filepath.Join(dir, "none.tar.gz"), filepath.Join(dir, "o3")))
	truncated := filepath.Join(dir, "trunc.tar.gz")
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	gz.Write([]byte("this is not a tar header but long enough to be read as one, almost"))
	gz.Close()
	require.NoError(t, os.WriteFile(truncated, b.Bytes(), 0o600))
	assert.Error(t, untar(truncated, filepath.Join(dir, "o4")))

	// A zip's folders are made.
	withDir := filepath.Join(dir, "dir.zip")
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	_, err := zw.Create("Blitz/locales/")
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, os.WriteFile(withDir, zb.Bytes(), 0o600))
	require.NoError(t, unzip(withDir, filepath.Join(dir, "o5")))
	assert.DirExists(t, filepath.Join(dir, "o5", "Blitz", "locales"))

	// Where nothing can be written.
	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	fakeVerify(t, nil)
	_, err = Stage(ctx, serveRelease(t, nil), "v9.9.9", "windows", "amd64", filepath.Join(file, "under"), "")
	assert.Error(t, err)
	assert.Error(t, writeFile(filepath.Join(file, "x"), strings.NewReader("x"), 0o600))
	assert.Error(t, unzip(withDir, filepath.Join(file, "o")))
}

// Unpacking where nothing can be written: a folder, a file and a link of
// a tarball, and a zip's file.
func TestUnpackWhereNothingCanBeWritten(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	under := filepath.Join(file, "out")
	for name, files := range map[string]map[string]string{
		"a folder": {},
		"a file":   {"f": "x"},
		"a link":   {"l": "->f"},
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(dir, name+".tar.gz")
			var b bytes.Buffer
			gz := gzip.NewWriter(&b)
			tw := tar.NewWriter(gz)
			if len(files) == 0 {
				require.NoError(t, tw.WriteHeader(&tar.Header{Name: "d/", Mode: 0o755, Typeflag: tar.TypeDir}))
			}
			for n, body := range files {
				if link, ok := strings.CutPrefix(body, "->"); ok {
					require.NoError(t, tw.WriteHeader(&tar.Header{Name: n, Linkname: link, Typeflag: tar.TypeSymlink}))
					continue
				}
				require.NoError(t, tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}))
				tw.Write([]byte(body))
			}
			tw.Close()
			gz.Close()
			require.NoError(t, os.WriteFile(p, b.Bytes(), 0o600))
			assert.Error(t, untar(p, under))
		})
	}
	z := filepath.Join(dir, "f.zip")
	require.NoError(t, os.WriteFile(z, zipOf(t, map[string]string{"f": "x"}), 0o600))
	assert.Error(t, unzip(z, under))
}
