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

package tools

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialEnv names the socket the test binary, started again under the
// sandbox, connects to (maybeDial).
const dialEnv = "BLITZ_TOOLS_TEST_DIAL"

// maybeDial connects to the socket $BLITZ_TOOLS_TEST_DIAL names, prints
// "connected" or the error, and exits; it does nothing when that's unset.
func maybeDial() {
	path := os.Getenv(dialEnv)
	if path == "" {
		return
	}
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		fmt.Println("refused:", err)
		os.Exit(0)
	}
	c.Close()
	fmt.Println("connected")
	os.Exit(0)
}

// guardSockets makes runDir and socks the guarded ones for the test.
func guardSockets(t *testing.T, runDir string, socks ...string) {
	old := serviceSockets
	serviceSockets = func() ([]string, []string) { return realAndGiven([]string{runDir}), realAndGiven(socks) }
	t.Cleanup(func() { serviceSockets = old })
}

// shortDir is a folder whose sockets fit in sun_path (104 bytes on macOS):
// the test temp dirs are too deep.
func shortDir(t *testing.T) string {
	dir, err := os.MkdirTemp("/tmp", "bzs")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	real, err := canonicalDir(dir)
	require.NoError(t, err)
	return real
}

func listenUnix(t *testing.T, path string) {
	l, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { l.Close() })
}

func TestGuardSocket(t *testing.T) {
	guardMu.Lock()
	old := guardedSocks
	guardedSocks = nil
	guardMu.Unlock()
	t.Cleanup(func() { guardMu.Lock(); guardedSocks = old; guardMu.Unlock() })
	t.Setenv("BLITZ_SOCKET", "/srv/blitz/default.sock")

	GuardSocket("")
	GuardSocket("/srv/blitz/other.sock")
	GuardSocket("/srv/blitz/other.sock")
	dirs, socks := serviceSockets()
	home, _ := os.UserHomeDir()
	assert.Contains(t, dirs, filepath.Join(home, ".blitz", "run"))
	assert.Equal(t, []string{"/srv/blitz/default.sock", "/srv/blitz/other.sock"}, socks)
}

// The profile denies connecting to the service's sockets, and writing
// there, after the Unix sockets it allows, with the network off or on.
func TestSeatbeltProfileDeniesServiceSockets(t *testing.T) {
	guardSockets(t, "/u/.blitz/run", "/srv/blitz.sock")
	for _, network := range []bool{false, true} {
		t.Run(fmt.Sprintf("network %v", network), func(t *testing.T) {
			p := seatbeltProfile(OSSandboxSpec{AllowNetwork: network})
			deny := strings.Index(p, "(deny network-outbound file-write*\n  (subpath \"/u/.blitz/run\")\n  (literal \"/srv/blitz.sock\")\n)")
			require.GreaterOrEqual(t, deny, 0, p)
			assert.Greater(t, deny, strings.LastIndex(p, "(allow"), "the deny must come after every allow:\n%s", p)
		})
	}
}

func TestBwrapHidesServiceSockets(t *testing.T) {
	base := t.TempDir()
	run := filepath.Join(base, "run")
	creds := filepath.Join(run, "credentials")
	require.NoError(t, os.MkdirAll(creds, 0o700))
	inRun := filepath.Join(run, "blitz.sock")
	elsewhere := filepath.Join(base, "custom.sock")
	require.NoError(t, os.WriteFile(elsewhere, nil, 0o600)) // stands for the socket
	guardSockets(t, run, inRun, elsewhere, filepath.Join(base, "missing.sock"))
	oldCreds := credentialsDir
	credentialsDir = func() (string, error) { return creds, nil }
	t.Cleanup(func() { credentialsDir = oldCreds })

	joined := strings.Join(bwrapArgs("bwrap", OSSandboxSpec{AllowNetwork: true}, nil, nil), " ")
	for _, want := range []string{
		"--tmpfs " + run + " --ro-bind " + creds + " " + creds + " --remount-ro " + run,
		"--ro-bind /dev/null " + elsewhere,
	} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, joined, want)
		})
	}
	assert.NotContains(t, joined, "/dev/null "+inRun, "the run folder's tmpfs hides it already")
	assert.NotContains(t, joined, "missing.sock", "bwrap fails on a missing mount point")
}

// Under the real macOS sandbox a command can't connect to the service's
// socket, nor one in the run folder, while another Unix socket still
// works, with the network off and on.
func TestSeatbeltRefusesServiceSocket(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("no sandbox-exec")
	}
	dir := shortDir(t)
	run := filepath.Join(dir, "run")
	require.NoError(t, os.Mkdir(run, 0o700))
	service := filepath.Join(dir, "svc.sock") // a $BLITZ_SOCKET outside the run folder
	inRun := filepath.Join(run, "b.sock")
	other := filepath.Join(dir, "ok.sock")
	for _, s := range []string{service, inRun, other} {
		listenUnix(t, s)
	}
	guardSockets(t, run, service)
	self, err := os.Executable()
	require.NoError(t, err)

	tests := []struct {
		name, path, want string
	}{
		{"the service's socket", service, "refused"},
		{"a socket in the run folder", inRun, "refused"},
		{"another socket", other, "connected"},
	}
	for _, network := range []bool{false, true} {
		osb, err := NewOSSandbox(OSSandboxSpec{Mode: SandboxRequired, AllowNetwork: network})
		if err != nil {
			t.Skipf("no usable OS sandbox: %v", err)
		}
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%s, network %v", tt.name, network), func(t *testing.T) {
				argv, err := osb.wrapCtx(context.Background(), []string{self})
				require.NoError(t, err)
				cmd := exec.Command(argv[0], argv[1:]...)
				cmd.Env = []string{"HOME=" + os.Getenv("HOME"), dialEnv + "=" + tt.path} // no test runner's files to touch
				out, _ := cmd.CombinedOutput()
				assert.Contains(t, string(out), tt.want, "dial %s", tt.path)
			})
		}
	}
}
