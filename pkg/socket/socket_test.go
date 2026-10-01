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

package socket

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shortTemp is a temporary directory short enough for a Unix socket path.
func shortTemp(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sk") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// TestDefaultSocket checks $BLITZ_SOCKET wins and the home default otherwise.
func TestDefaultSocket(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BLITZ_SOCKET", "")
	assert.Equal(t, filepath.Join(home, ".blitz/run/blitz.sock"), DefaultSocket())
	t.Setenv("BLITZ_SOCKET", "~/x.sock")
	assert.Equal(t, filepath.Join(home, "x.sock"), DefaultSocket(), "the variable is expanded")
}

// TestListen checks the socket and its directory are private, a second
// listener is refused while the first answers, and a stale socket is replaced.
func TestListen(t *testing.T) {
	path := filepath.Join(shortTemp(t), "run", "b.sock")
	assert.False(t, Running(path), "nothing listens yet")

	l, err := Listen(path)
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
	assert.True(t, Running(path))

	_, err = Listen(path)
	require.ErrorIs(t, err, ErrRunning)

	// A socket left behind by a service that died is replaced.
	l.(interface{ SetUnlinkOnClose(bool) }).SetUnlinkOnClose(false)
	require.NoError(t, l.Close())
	_, err = os.Lstat(path)
	require.NoError(t, err, "the stale socket stays on disk")
	l2, err := Listen(path)
	require.NoError(t, err)
	defer l2.Close()
	assert.True(t, Running(path))
}

// TestListenErrors checks Listen reports a directory it cannot create and a
// stale socket it cannot remove.
func TestListenErrors(t *testing.T) {
	dir := shortTemp(t)
	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	_, err := Listen(filepath.Join(file, "b.sock"))
	assert.Error(t, err, "a file where the directory should be")

	// A non-empty directory where the socket should be: not running, and
	// it can't be removed.
	sock := filepath.Join(dir, "d.sock")
	require.NoError(t, os.MkdirAll(filepath.Join(sock, "x"), 0o700))
	_, err = Listen(sock)
	assert.ErrorContains(t, err, "removing stale socket")

	_, err = Listen(filepath.Join(dir, strings.Repeat("s", 200)+".sock"))
	assert.Error(t, err, "a socket path longer than the OS allows")
}

// TestClient checks the client reaches an HTTP server on the socket.
func TestClient(t *testing.T) {
	path := filepath.Join(shortTemp(t), "c.sock")
	l, err := Listen(path)
	require.NoError(t, err)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { srv.Close() })

	resp, err := Client(path).Get(BaseURL + "/x")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
}
