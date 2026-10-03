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
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
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
			got := compareVersions(tt.a, tt.b)
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

// fakeRelease serves v9.9.9 for this machine, with checksums (or a bad
// one).
func fakeRelease(t *testing.T, badSum bool) {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	dir := fmt.Sprintf("blitz_%s_%s/", goruntime.GOOS, goruntime.GOARCH)
	for _, p := range []string{"blitz", "blitzd", "README.md"} {
		body := []byte("new " + p)
		tw.WriteHeader(&tar.Header{Name: dir + p, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write(body)
	}
	tw.Close()
	gz.Close()
	name := fmt.Sprintf("blitz_9.9.9_%s_%s.tar.gz", goruntime.GOOS, goruntime.GOARCH)
	sum := sha256.Sum256(archive.Bytes())
	hexSum := hex.EncodeToString(sum[:])
	if badSum {
		hexSum = hex.EncodeToString(make([]byte, 32))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			fmt.Fprint(w, `{"tag_name": "v9.9.9"}`)
		case "/download/v9.9.9/" + name:
			w.Write(archive.Bytes())
		case "/download/v9.9.9/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", hexSum, name)
		case "/download/v9.9.9/checksums.txt.sigstore.json":
			fmt.Fprint(w, "{}")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldAPI, oldDL, oldExe, oldCosign, oldVersion := releasesAPI, releasesDownload, executable, lookCosign, version
	t.Cleanup(func() {
		releasesAPI, releasesDownload, executable, lookCosign, version = oldAPI, oldDL, oldExe, oldCosign, oldVersion
	})
	releasesAPI, releasesDownload = srv.URL+"/api", srv.URL+"/download"
	lookCosign = func() (string, error) { return "", errors.New("not found") }
}

// installed makes blitz and blitzd in a folder, as an installation.
func installed(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, p := range []string{"blitz", "blitzd"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, p), []byte("old "+p), 0o755))
	}
	exe := filepath.Join(dir, "blitz")
	executable = func() (string, error) { return exe, nil }
	return dir
}

func TestUpdate(t *testing.T) {
	isolate(t)
	fakeRelease(t, false)
	dir := installed(t)
	version = "0.1.0"

	out, err := runCLI(t, "update", "--check")
	require.NoError(t, err)
	assert.Contains(t, out, "Blitz v9.9.9 is available (this is 0.1.0)")

	_, err = runCLI(t, "update")
	assert.Equal(t, exitUsage, exitCodeFor(err))
	assert.ErrorContains(t, err, "cosign isn't installed")

	out, err = runCLI(t, "update", "--skip-signature")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Updated to Blitz v9.9.9")
	for _, p := range []string{"blitz", "blitzd"} {
		data, _ := os.ReadFile(filepath.Join(dir, p))
		assert.Equal(t, "new "+p, string(data))
	}

	version = "9.9.9"
	out, err = runCLI(t, "update")
	require.NoError(t, err)
	assert.Contains(t, out, "Blitz 9.9.9 is the latest release.")

	version = "dev"
	_, err = runCLI(t, "update")
	assert.ErrorContains(t, err, "development build")
}

func TestUpdateRefuses(t *testing.T) {
	isolate(t)
	fakeRelease(t, true)
	dir := installed(t)
	version = "0.1.0"
	_, err := runCLI(t, "update", "--skip-signature")
	assert.ErrorContains(t, err, "checksum doesn't match")
	data, _ := os.ReadFile(filepath.Join(dir, "blitz"))
	assert.Equal(t, "old blitz", string(data), "nothing was installed")

	executable = func() (string, error) { return "/opt/homebrew/Cellar/blitz/0.1.0/bin/blitz", nil }
	_, err = runCLI(t, "update", "--skip-signature")
	assert.ErrorContains(t, err, "Homebrew")
}

// cosignScript makes lookCosign find a cosign that exits with code.
func cosignScript(t *testing.T, code int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cosign")
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf("#!/bin/sh\necho checked >&2\nexit %d\n", code)), 0o755))
	lookCosign = func() (string, error) { return path, nil }
}

// With cosign installed, the checksums' signature decides: a good one
// installs, a bad one installs nothing.
func TestUpdateChecksTheSignature(t *testing.T) {
	isolate(t)
	fakeRelease(t, false)
	dir := installed(t)
	version = "0.1.0"

	cosignScript(t, 1)
	_, err := runCLI(t, "update")
	assert.ErrorContains(t, err, "signature doesn't verify")
	data, _ := os.ReadFile(filepath.Join(dir, "blitz"))
	assert.Equal(t, "old blitz", string(data), "nothing was installed")

	cosignScript(t, 0)
	out, err := runCLI(t, "update")
	require.NoError(t, err, out)
	assert.Contains(t, out, "The checksums are signed by Blitz's release workflow.")
	assert.Contains(t, out, "Updated to Blitz v9.9.9")
}

// --check and --version, and the ways finding or fetching a release fails.
func TestUpdateReleaseLookups(t *testing.T) {
	isolate(t)
	fakeRelease(t, false)
	installed(t)

	t.Run("latest already", func(t *testing.T) {
		version = "9.9.9"
		out, err := runCLI(t, "update", "--check")
		require.NoError(t, err)
		assert.Contains(t, out, "Blitz 9.9.9 is the latest release.")
	})
	t.Run("named release without v", func(t *testing.T) {
		version = "dev"
		out, err := runCLI(t, "update", "--version", "9.9.9", "--skip-signature")
		require.NoError(t, err, out)
		assert.Contains(t, out, "Updated to Blitz v9.9.9")
	})
	t.Run("named release missing", func(t *testing.T) {
		version = "0.1.0"
		_, err := runCLI(t, "update", "--version", "v1.2.3", "--skip-signature")
		assert.ErrorContains(t, err, "downloading blitz_1.2.3")
		assert.ErrorContains(t, err, "HTTP 404")
	})
	t.Run("executable unknown", func(t *testing.T) {
		old := executable
		t.Cleanup(func() { executable = old })
		executable = func() (string, error) { return "", errors.New("no executable") }
		_, err := runCLI(t, "update", "--version", "v9.9.9")
		assert.ErrorContains(t, err, "no executable")
	})
	t.Run("system package", func(t *testing.T) {
		old := executable
		t.Cleanup(func() { executable = old })
		executable = func() (string, error) { return "/usr/bin/blitz", nil }
		_, err := runCLI(t, "update", "--version", "v9.9.9")
		assert.Equal(t, exitUsage, exitCodeFor(err))
		assert.ErrorContains(t, err, "your system's package manager")
	})
	for name, body := range map[string]string{"no tag": `{}`, "not JSON": `<html>`} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			t.Cleanup(srv.Close)
			releasesAPI = srv.URL
			_, err := runCLI(t, "update", "--check")
			assert.ErrorContains(t, err, "no release tag in the answer")
		})
	}
	t.Run("API error", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(srv.Close)
		releasesAPI = srv.URL
		_, err := runCLI(t, "update", "--check")
		assert.ErrorContains(t, err, "finding the latest release: HTTP 404")
	})
	t.Run("API unreachable", func(t *testing.T) {
		releasesAPI = "http://127.0.0.1:1"
		_, err := runCLI(t, "update", "--check")
		assert.ErrorContains(t, err, "finding the latest release")
	})
	t.Run("bad URL", func(t *testing.T) {
		releasesAPI = "http://bad host"
		_, err := runCLI(t, "update", "--check")
		assert.ErrorContains(t, err, "finding the latest release")
	})
}

// An archive without one of the installed programs installs neither.
func TestUpdateArchiveWithoutTheService(t *testing.T) {
	isolate(t)
	fakeRelease(t, false)
	dir := installed(t)
	version = "0.1.0"
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "d/", Mode: 0o755, Typeflag: tar.TypeDir})
	tw.WriteHeader(&tar.Header{Name: "d/blitz", Mode: 0o755, Size: 3, Typeflag: tar.TypeReg})
	tw.Write([]byte("new"))
	tw.Close()
	gz.Close()
	serveArchive(t, archive.Bytes())
	// The programs are replaced in map order: try it both ways round.
	for range 8 {
		_, err := runCLI(t, "update", "--skip-signature")
		assert.ErrorContains(t, err, "the archive has no blitzd")
		for _, p := range []string{"blitz", "blitzd"} {
			data, _ := os.ReadFile(filepath.Join(dir, p))
			assert.Equal(t, "old "+p, string(data), "neither program is replaced")
		}
	}
}

// checkSum needs the file's line in checksums.txt.
func TestCheckSum(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.tar.gz")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	sums := filepath.Join(dir, "checksums.txt")
	require.NoError(t, os.WriteFile(sums, []byte("abc  other.tar.gz\nmalformed\n"), 0o644))
	assert.ErrorContains(t, checkSum(sums, file), "a.tar.gz isn't in checksums.txt")
	assert.Error(t, checkSum(filepath.Join(dir, "missing.txt"), file), "no checksums file")
	require.NoError(t, os.WriteFile(sums, []byte("abc  missing.tar.gz\n"), 0o644))
	assert.Error(t, checkSum(sums, filepath.Join(dir, "missing.tar.gz")), "no archive")
}

// extractPrograms reads Windows' zip archives as well as tarballs, and
// refuses what isn't an archive.
func TestExtractPrograms(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "blitz.zip")
	f, err := os.Create(archive)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	_, err = zw.Create("blitz_x/")
	require.NoError(t, err)
	for _, p := range []string{"blitz_x/" + programName("blitz"), "blitz_x/" + programName("blitzd"), "blitz_x/README.md"} {
		w, err := zw.Create(p)
		require.NoError(t, err)
		fmt.Fprint(w, "new "+filepath.Base(p))
	}
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
	out := t.TempDir()
	files, err := extractPrograms(archive, out)
	require.NoError(t, err)
	assert.Len(t, files, 2)
	data, err := os.ReadFile(files["blitzd"])
	require.NoError(t, err)
	assert.Equal(t, "new "+programName("blitzd"), string(data))

	for name, content := range map[string]string{"bad.zip": "not a zip", "bad.tar.gz": "not gzip"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(dir, name)
			require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
			_, err := extractPrograms(p, out)
			assert.Error(t, err)
		})
	}
	t.Run("missing", func(t *testing.T) {
		_, err := extractPrograms(filepath.Join(dir, "none.tar.gz"), out)
		assert.Error(t, err)
	})
	t.Run("truncated tar", func(t *testing.T) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		gz.Write([]byte("this is not a tar header but long enough to be read as one, almost"))
		gz.Close()
		p := filepath.Join(dir, "trunc.tar.gz")
		require.NoError(t, os.WriteFile(p, buf.Bytes(), 0o644))
		_, err := extractPrograms(p, out)
		assert.Error(t, err)
	})
	t.Run("unwritable", func(t *testing.T) {
		_, err := extractPrograms(archive, filepath.Join(dir, "no", "such"))
		assert.Error(t, err)
	})
}

// replaceFiles fails cleanly when it can't read or place a new file, and
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
			assert.Error(t, replaceFiles(tc.pairs(dir)))
			data, err := os.ReadFile(filepath.Join(dir, "blitzd"))
			require.NoError(t, err)
			assert.Equal(t, "old blitzd", string(data), "the first program is put back")
			for _, left := range []string{"blitzd.new", "blitzd.old", "busy.new", "blitz.new"} {
				assert.NoFileExists(t, filepath.Join(dir, left))
			}
		})
	}
}

// replaceFiles puts every new file in place, and leaves nothing beside.
func TestReplaceFilesReplacesAll(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"blitz", "blitzd"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f+".src"), []byte("new "+f), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte("old "+f), 0o755))
	}
	require.NoError(t, replaceFiles([][2]string{
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

// serveArchive serves archive as v9.9.9's for this machine, with its
// checksum.
func serveArchive(t *testing.T, archive []byte) {
	t.Helper()
	name := fmt.Sprintf("blitz_9.9.9_%s_%s.tar.gz", goruntime.GOOS, goruntime.GOARCH)
	sum := sha256.Sum256(archive)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case name:
			w.Write(archive)
		case "checksums.txt":
			fmt.Fprintf(w, "%s *%s\n", hex.EncodeToString(sum[:]), name)
		default:
			fmt.Fprint(w, "{}")
		}
	}))
	t.Cleanup(srv.Close)
	releasesDownload = srv.URL
}

// What fails while installing leaves the installation as it was: an
// archive that isn't one, a download that doesn't connect, a folder that
// can't be written.
func TestUpdateInstallFailures(t *testing.T) {
	isolate(t)
	fakeRelease(t, false)
	dir := installed(t)
	version = "0.1.0"
	working := releasesDownload

	serveArchive(t, []byte("not an archive"))
	_, err := runCLI(t, "update", "--skip-signature")
	assert.ErrorContains(t, err, "gzip")

	releasesDownload = "http://127.0.0.1:1"
	_, err = runCLI(t, "update", "--skip-signature")
	assert.ErrorContains(t, err, "downloading ")

	releasesDownload = working
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	_, err = runCLI(t, "update", "--skip-signature")
	assert.ErrorContains(t, err, "replacing ")
	data, _ := os.ReadFile(filepath.Join(dir, "blitz"))
	assert.Equal(t, "old blitz", string(data))
}
