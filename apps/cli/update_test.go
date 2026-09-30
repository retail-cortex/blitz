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
