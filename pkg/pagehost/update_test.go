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

package pagehost

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/selfupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// releaseServer serves latest as the latest release, with a Windows zip
// of the app.
func releaseServer(t *testing.T, latest string) selfupdate.Source {
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	w, err := zw.Create("Blitz/Blitz.exe")
	require.NoError(t, err)
	fmt.Fprint(w, "new")
	require.NoError(t, zw.Close())
	name := "Blitz_" + latest[1:] + "_windows_x64.zip"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			fmt.Fprintf(w, `{"tag_name": %q}`, latest)
		case "/download/" + latest + "/" + name:
			w.Write(zb.Bytes())
		case "/download/" + latest + "/" + name + ".sigstore.json":
			fmt.Fprint(w, "{}")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return selfupdate.Source{API: srv.URL + "/api", Download: srv.URL + "/download"}
}

// installed makes an installed Windows app in a folder, and returns its
// host's program.
func installedApp(t *testing.T) string {
	exe := filepath.Join(t.TempDir(), "Blitz", "resources", "app", "bin", "blitz-tray.exe")
	require.NoError(t, os.MkdirAll(filepath.Dir(exe), 0o755))
	require.NoError(t, os.WriteFile(exe, []byte("tray"), 0o755))
	return exe
}

func trustPackages(t *testing.T) {
	old := selfupdate.VerifyPackage
	t.Cleanup(func() { selfupdate.VerifyPackage = old })
	selfupdate.VerifyPackage = func(context.Context, io.Reader, []byte, string) error { return nil }
}

// Check, download in the background, then apply: the copy of the host is
// started to swap the app once it quits.
func TestUpdater(t *testing.T) {
	trustPackages(t)
	src := releaseServer(t, "v0.6.7")
	var spawned []string
	u := &Updater{
		Version: "0.6.6", HostExe: installedApp(t), Dir: t.TempDir(), CacheDir: t.TempDir(),
		Source: func() selfupdate.Source { return src }, GOOS: "windows", GOARCH: "amd64",
		Spawn: func(name string, args ...string) error { spawned = append([]string{name}, args...); return nil },
	}
	ctx := context.Background()
	s := u.Check(ctx, false)
	assert.Equal(t, UpdateStatus{Current: "0.6.6", Latest: "v0.6.7", Available: true, Can: true, Notes: "https://github.com/retail-cortex/blitz/releases/tag/v0.6.7"}, s)
	assert.ErrorIs(t, u.Apply(), ErrNotReady)

	assert.True(t, u.Download(ctx).Ready, "the download is waited for")
	assert.True(t, u.Status().Ready)
	assert.True(t, u.Download(ctx).Ready, "downloaded once")

	require.NoError(t, u.Apply())
	require.NotEmpty(t, spawned)
	assert.Equal(t, filepath.Join(u.Dir, "apply-blitz-tray.exe"), spawned[0], "a copy, outside the app")
	assert.Contains(t, spawned, "--apply-update")
	data, err := os.ReadFile(filepath.Join(u.Dir, "v0.6.7", "new", "Blitz", "Blitz.exe"))
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
}

// Where it can't update itself it says why, and a release that can't be
// had is an error.
func TestUpdaterRefusals(t *testing.T) {
	trustPackages(t)
	src := releaseServer(t, "v0.6.7")
	ctx := context.Background()
	for _, tc := range []struct {
		name, version, exe, goos, reason string
	}{
		{"a development build", "dev", installedApp(t), "windows", "development build"},
		{"not installed", "0.6.6", "/work/bazel-bin/apps/tray/blitz-tray", "darwin", "isn't an installed desktop app"},
		{"a package manager's", "0.6.6", "/usr/lib/blitz-desktop/resources/app/bin/blitz-tray", "linux", "your system's package manager"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := &Updater{Version: tc.version, HostExe: tc.exe, Dir: t.TempDir(), Source: func() selfupdate.Source { return src }, GOOS: tc.goos, GOARCH: "amd64"}
			s := u.Check(ctx, false)
			assert.False(t, s.Can)
			assert.Contains(t, s.Reason, tc.reason)
			assert.False(t, u.Download(ctx).Ready)
			assert.ErrorContains(t, u.Apply(), tc.reason)
		})
	}
	t.Run("the latest already", func(t *testing.T) {
		u := &Updater{Version: "0.6.7", HostExe: installedApp(t), Dir: t.TempDir(), Source: func() selfupdate.Source { return src }, GOOS: "windows", GOARCH: "amd64"}
		assert.False(t, u.Check(ctx, false).Available)
		assert.False(t, u.Download(ctx).Ready)
	})
	t.Run("no release to be had", func(t *testing.T) {
		u := &Updater{Version: "0.6.6", HostExe: installedApp(t), Dir: t.TempDir(), Source: func() selfupdate.Source { return selfupdate.Source{API: "http://127.0.0.1:1"} }, GOOS: "windows", GOARCH: "amd64"}
		assert.Contains(t, u.Check(ctx, false).Error, "finding the latest release")
	})
	t.Run("a download that fails says so", func(t *testing.T) {
		selfupdate.VerifyPackage = func(context.Context, io.Reader, []byte, string) error {
			return fmt.Errorf("the release's signature doesn't verify")
		}
		u := &Updater{Version: "0.6.6", HostExe: installedApp(t), Dir: t.TempDir(), Source: func() selfupdate.Source { return src }, GOOS: "windows", GOARCH: "amd64"}
		u.Check(ctx, false)
		assert.Contains(t, u.Download(ctx).Error, "doesn't verify")
	})
}

// The host answers the updater's calls when it has one.
func TestHostUpdateCalls(t *testing.T) {
	trustPackages(t)
	src := releaseServer(t, "v0.6.7")
	u := &Updater{Version: "0.6.6", HostExe: installedApp(t), Dir: t.TempDir(), Source: func() selfupdate.Source { return src }, GOOS: "windows", GOARCH: "amd64"}
	h := Host{Updates: u}.Handler()
	code, body := call(t, h, "CheckUpdate")
	require.Equal(t, http.StatusOK, code, string(body))
	var s UpdateStatus
	require.NoError(t, json.Unmarshal(body, &s))
	assert.True(t, s.Available)
	code, body = call(t, h, "ApplyUpdate")
	assert.Equal(t, http.StatusInternalServerError, code, "not downloaded yet: %s", body)
	code, body = call(t, h, "DownloadUpdate")
	assert.Equal(t, http.StatusOK, code)
	require.NoError(t, json.Unmarshal(body, &s))
	assert.True(t, s.Ready, "the call returns when it's downloaded")
	code, body = call(t, h, "UpdateStatus")
	assert.Equal(t, http.StatusOK, code, string(body))
	code, _ = call(t, h, "CheckUpdate", true)
	assert.Equal(t, http.StatusOK, code)
	code, _ = call(t, h, "CheckUpdate", "yes")
	assert.Equal(t, http.StatusInternalServerError, code)
}

// The app asks for the latest release when it opens, but only five days
// after the last time: until then the last answer stands, unless Check
// now forces a new one.
func TestUpdaterChecksEveryFiveDays(t *testing.T) {
	latest := "v0.6.7"
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		fmt.Fprintf(w, `{"tag_name": %q}`, latest)
	}))
	t.Cleanup(srv.Close)
	now := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	newUpdater := func() *Updater {
		return &Updater{Version: "0.6.6", HostExe: installedApp(t), Dir: dir, GOOS: "windows", GOARCH: "amd64", Now: func() time.Time { return now },
			Source: func() selfupdate.Source { return selfupdate.Source{API: srv.URL} }}
	}
	ctx := context.Background()
	assert.Equal(t, "v0.6.7", newUpdater().Check(ctx, false).Latest)
	assert.Equal(t, 1, asked)

	latest = "v0.6.8"
	now = now.Add(4 * 24 * time.Hour)
	assert.Equal(t, "v0.6.7", newUpdater().Check(ctx, false).Latest, "four days on: the last answer, kept between runs")
	assert.Equal(t, 1, asked)
	assert.Equal(t, "v0.6.8", newUpdater().Check(ctx, true).Latest, "Check now")
	assert.Equal(t, 2, asked)

	latest = "v0.6.9"
	now = now.Add(5 * 24 * time.Hour)
	assert.Equal(t, "v0.6.9", newUpdater().Check(ctx, false).Latest, "five days after: asked again")
	assert.Equal(t, 3, asked)
}

// The updater's copy of the host must be made, and started.
func TestUpdaterApplyFailures(t *testing.T) {
	trustPackages(t)
	src := releaseServer(t, "v0.6.7")
	exe := installedApp(t)
	u := &Updater{Version: "0.6.6", HostExe: exe, Dir: t.TempDir(), Source: func() selfupdate.Source { return src }, GOOS: "windows", GOARCH: "amd64",
		Spawn: func(string, ...string) error { return fmt.Errorf("can't start it") }}
	ctx := context.Background()
	u.Check(ctx, false)
	require.True(t, u.Download(ctx).Ready)
	assert.ErrorContains(t, u.Apply(), "can't start it")
	require.NoError(t, os.Remove(exe))
	assert.ErrorContains(t, u.Apply(), "copying the updater")
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	assert.Error(t, copyProgram(file, filepath.Join(file, "under", "x")))
}

// The copy fails where it can't be written, or a folder is in its way.
func TestCopyProgramFailures(t *testing.T) {
	src := filepath.Join(t.TempDir(), "tray")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0o755))
	ro := filepath.Join(t.TempDir(), "ro")
	require.NoError(t, os.Mkdir(ro, 0o500))
	if os.Geteuid() != 0 {
		assert.Error(t, copyProgram(src, filepath.Join(ro, "x")), "a folder it can't write in")
	}
	busy := filepath.Join(t.TempDir(), "busy")
	require.NoError(t, os.MkdirAll(filepath.Join(busy, "child"), 0o755))
	assert.Error(t, copyProgram(src, busy), "a folder where the copy goes")
	assert.NoFileExists(t, busy+".new")
}

// Fixing the sandbox where there's none to fix says so.
func TestFixSandboxElsewhere(t *testing.T) {
	if goruntime.GOOS == "linux" {
		t.Skip("on Linux it would run pkexec")
	}
	fix := FixSandbox(context.Background())
	assert.NotEmpty(t, fix.Status.State)
	assert.NotEmpty(t, fix.Error)
}
