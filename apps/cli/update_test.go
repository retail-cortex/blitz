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
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/retail-cortex/blitz/pkg/selfupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// src is the release source the tests serve.
var src selfupdate.Source

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
	oldReleases, oldExe, oldVerify, oldCosign, oldVersion := releases, executable, verifySignature, lookCosign, version
	t.Cleanup(func() {
		releases, executable, verifySignature, lookCosign, version = oldReleases, oldExe, oldVerify, oldCosign, oldVersion
	})
	src = selfupdate.Source{API: srv.URL + "/api", Download: srv.URL + "/download"}
	releases = func() selfupdate.Source { return src }
	// A release before v0.6.6: its signature only cosign checks, and
	// there's none.
	verifySignature = func(context.Context, io.Reader, []byte, string) error { return selfupdate.ErrOldBundle }
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
	assert.ErrorContains(t, err, "can only be checked with cosign, which isn't installed")

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

// The checksums' signature decides, checked here (Sigstore's bundle): a
// good one installs, a bad one installs nothing.
func TestUpdateChecksTheSignature(t *testing.T) {
	isolate(t)
	fakeRelease(t, false)
	dir := installed(t)
	version = "0.1.0"

	verifySignature = func(context.Context, io.Reader, []byte, string) error {
		return errors.New("the release's signature doesn't verify: wrong identity")
	}
	_, err := runCLI(t, "update")
	assert.ErrorContains(t, err, "wrong identity: nothing was installed")
	data, _ := os.ReadFile(filepath.Join(dir, "blitz"))
	assert.Equal(t, "old blitz", string(data), "nothing was installed")

	verifySignature = func(_ context.Context, artifact io.Reader, _ []byte, _ string) error {
		data, _ := io.ReadAll(artifact)
		assert.Contains(t, string(data), "blitz_9.9.9", "the checksums are what's checked")
		return nil
	}
	out, err := runCLI(t, "update")
	require.NoError(t, err, out)
	assert.Contains(t, out, "The checksums are signed by Blitz's release workflow.")
}

// An older release's signature (cosign's own bundle) is checked with
// cosign: a good one installs, a bad one installs nothing.
func TestUpdateChecksAnOldSignatureWithCosign(t *testing.T) {
	isolate(t)
	fakeRelease(t, false)
	dir := installed(t)
	version = "0.1.0"

	cosignScript(t, 1)
	_, err := runCLI(t, "update")
	assert.ErrorContains(t, err, "signature doesn't verify, so nothing was installed")
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
			src.API = srv.URL
			_, err := runCLI(t, "update", "--check")
			assert.ErrorContains(t, err, "no release tag in the answer")
		})
	}
	t.Run("API error", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(srv.Close)
		src.API = srv.URL
		_, err := runCLI(t, "update", "--check")
		assert.ErrorContains(t, err, "finding the latest release: HTTP 404")
	})
	t.Run("API unreachable", func(t *testing.T) {
		src.API = "http://127.0.0.1:1"
		_, err := runCLI(t, "update", "--check")
		assert.ErrorContains(t, err, "finding the latest release")
	})
	t.Run("bad URL", func(t *testing.T) {
		src.API = "http://bad host"
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
	src.Download = srv.URL
}

// What fails while installing leaves the installation as it was: an
// archive that isn't one, a download that doesn't connect, a folder that
// can't be written.
func TestUpdateInstallFailures(t *testing.T) {
	isolate(t)
	fakeRelease(t, false)
	dir := installed(t)
	version = "0.1.0"
	working := src.Download

	serveArchive(t, []byte("not an archive"))
	_, err := runCLI(t, "update", "--skip-signature")
	assert.ErrorContains(t, err, "gzip")

	src.Download = "http://127.0.0.1:1"
	_, err = runCLI(t, "update", "--skip-signature")
	assert.ErrorContains(t, err, "downloading ")

	src.Download = working
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	_, err = runCLI(t, "update", "--skip-signature")
	assert.ErrorContains(t, err, "replacing ")
	data, _ := os.ReadFile(filepath.Join(dir, "blitz"))
	assert.Equal(t, "old blitz", string(data))
}

// The signature check reads the checksums and the bundle it downloaded.
func TestCheckSignatureReadsItsFiles(t *testing.T) {
	dir := t.TempDir()
	sums := filepath.Join(dir, "checksums.txt")
	require.NoError(t, os.WriteFile(sums, []byte("x"), 0o600))
	assert.Error(t, checkSignature(context.Background(), filepath.Join(dir, "missing.txt"), filepath.Join(dir, "b.json")))
	assert.Error(t, checkSignature(context.Background(), sums, filepath.Join(dir, "missing.json")))
}

// cosign is looked for on PATH.
func TestLookCosign(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := lookCosign()
	assert.Error(t, err)
}
