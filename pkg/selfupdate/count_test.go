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
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The counted check answers first and sends only its five parameters;
// whatever goes wrong with it, GitHub answers (spec_update_checks_054
// UC-01, UC-02).
func TestCountedLatest(t *testing.T) {
	saved := CheckWait
	CheckWait = 200 * time.Millisecond
	t.Cleanup(func() { CheckWait = saved })
	for _, tc := range []struct {
		name       string
		answer     func(w http.ResponseWriter)
		want       string
		fromGitHub bool
	}{
		{"answered", func(w http.ResponseWriter) { fmt.Fprint(w, `{"tag": "v2.0.0", "notes": "…"}`) }, "v2.0.0", false},
		{"down", func(w http.ResponseWriter) { w.WriteHeader(http.StatusServiceUnavailable) }, "v1.9.0", true},
		{"no tag", func(w http.ResponseWriter) { fmt.Fprint(w, `{"tag": ""}`) }, "v1.9.0", true},
		{"not JSON", func(w http.ResponseWriter) { fmt.Fprint(w, `<html>`) }, "v1.9.0", true},
		{"too slow", func(w http.ResponseWriter) { time.Sleep(500 * time.Millisecond) }, "v1.9.0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var asked url.Values
			counted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				asked = r.URL.Query()
				mu.Unlock()
				assert.Empty(t, r.Header.Get("Cookie"))
				tc.answer(w)
			}))
			t.Cleanup(counted.Close)
			var github atomic.Bool
			gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				github.Store(true)
				assert.Empty(t, r.URL.RawQuery, "GitHub gets none of the check's parameters")
				fmt.Fprint(w, `{"tag_name": "v1.9.0"}`)
			}))
			t.Cleanup(gh.Close)
			src := Source{API: gh.URL, Count: &Count{URL: counted.URL, Install: "0123456789abcdef0123456789abcdef", Version: "v1.8.0", App: "cli", GOOS: "linux", GOARCH: "arm64"}}
			got, err := src.Latest(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.fromGitHub, github.Load())
			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, url.Values{
				"install": {"0123456789abcdef0123456789abcdef"}, "version": {"v1.8.0"},
				"os": {"linux"}, "arch": {"arm64"}, "app": {"cli"},
			}, asked)
		})
	}
}

// Counting is on only when the settings say so and no local release
// replaces GitHub's (UC-05, UC-20).
func TestCounting(t *testing.T) {
	for _, tc := range []struct {
		name   string
		on     bool
		local  string
		counts bool
	}{
		{"on", true, "", true},
		{"off", false, "", false},
		{"a local release", true, "http://127.0.0.1:8000", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BLITZ_UPDATE_URL", tc.local)
			dir := t.TempDir()
			src := GitHub().Counting(tc.on, dir, "desktop", "v1.0.0")
			_, statErr := os.Stat(filepath.Join(dir, "install-id"))
			if !tc.counts {
				assert.Nil(t, src.Count)
				assert.ErrorIs(t, statErr, os.ErrNotExist, "no ID is made when nothing is counted")
				return
			}
			require.NotNil(t, src.Count)
			assert.Equal(t, Count{URL: CheckURL, Install: src.Count.Install, Version: "v1.0.0", App: "desktop", GOOS: goruntime.GOOS, GOARCH: goruntime.GOARCH}, *src.Count)
			assert.Len(t, src.Count.Install, 32)
		})
	}
}

// The ID is made once, private, and kept; a file that isn't one is
// replaced (UC-10).
func TestInstallID(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".blitz")
	id, err := InstallID(dir)
	require.NoError(t, err)
	assert.Regexp(t, `^[0-9a-f]{32}$`, id)
	info, err := os.Stat(filepath.Join(dir, "install-id"))
	require.NoError(t, err)
	if goruntime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	again, err := InstallID(dir)
	require.NoError(t, err)
	assert.Equal(t, id, again)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "install-id"), []byte("not an id"), 0o600))
	other, err := InstallID(dir)
	require.NoError(t, err)
	assert.Regexp(t, `^[0-9a-f]{32}$`, other)
	assert.NotEqual(t, id, other)

	_, err = InstallID(filepath.Join(dir, "install-id", "below a file"))
	assert.Error(t, err)
}

// When the ID can't be kept, nothing is counted, and a check with an
// address that isn't one fails before anything is sent (UC-01, UC-10).
func TestCountingWithoutAnID(t *testing.T) {
	t.Setenv("BLITZ_UPDATE_URL", "")
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "install-id"), 0o700))
	_, err := InstallID(dir)
	assert.Error(t, err, "install-id is a folder: no ID can be written")
	assert.Nil(t, GitHub().Counting(true, dir, "cli", "v1.0.0").Count, "no ID: not counted")

	c := &Count{URL: "http://bad\x7f.example", Install: "0123456789abcdef0123456789abcdef", Version: "v1.0.0", App: "cli", GOOS: "linux", GOARCH: "amd64"}
	_, err = c.latest(context.Background())
	assert.Error(t, err)
}
