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

// Package socket is where the per-user Blitz service listens and how
// clients reach it: a Unix socket that only its user can open. The
// service runs shell commands, so it is never on a network port by
// default.
package socket

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
)

// BaseURL is the URL clients use over the socket (the host is ignored).
const BaseURL = "http://blitz"

// ErrRunning reports a service already answering on the socket.
var ErrRunning = errors.New("a Blitz service is already running")

// DefaultSocket is where the per-user service listens:
// $BLITZ_SOCKET, or ~/.blitz/run/blitz.sock.
func DefaultSocket() string {
	if p := os.Getenv("BLITZ_SOCKET"); p != "" {
		return config.ExpandHome(p)
	}
	return config.ExpandHome(defaultSocket)
}

const defaultSocket = "~/.blitz/run/blitz.sock"

// Listen opens the Unix socket at path for this user only. It refuses when
// a service already answers there, and replaces a socket left by one that
// died, never another kind of file. It makes the folder private (0700)
// when it creates it, and always ~/.blitz/run; a folder of the user's
// own, such as their home for $BLITZ_SOCKET=~/blitz.sock, keeps its mode.
//
// The 0600 mode (and the folder's 0700) is the only gate: there is no peer
// check, so anything running as the user may connect. The model's commands
// run as the user too; the OS sandbox keeps sandboxed processes away from
// the socket (tools.GuardSocket, spec_007 SH-23a).
//
// On Windows, permission bits mean nothing (Chmod only sets read-only);
// there the socket is as private as the folder it's in, whose access
// control list the user's profile gives it: ~/.blitz/run is only the
// user's (and the system's, and administrators').
func Listen(path string) (net.Listener, error) {
	dir := filepath.Dir(path)
	_, err := os.Stat(dir)
	created := errors.Is(err, fs.ErrNotExist)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// The directory, not only the socket, keeps other users out: there is
	// no window between creating the socket and restricting it.
	if created || dir == filepath.Dir(config.ExpandHome(defaultSocket)) {
		if err := restrict(dir, 0o700); err != nil {
			return nil, err
		}
	}
	if info, err := os.Lstat(path); err == nil {
		if Running(path) {
			return nil, fmt.Errorf("%w (%s)", ErrRunning, path)
		}
		if !isSocket(info.Mode()) {
			return nil, fmt.Errorf("%s is there and isn't a socket: it's left as it is (choose another $BLITZ_SOCKET)", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("removing stale socket: %w", err)
		}
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := restrict(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// isSocket says whether a file is a socket (on Windows, Go may report an
// AF_UNIX socket's reparse point as irregular).
func isSocket(m fs.FileMode) bool {
	return m&fs.ModeSocket != 0 || (goruntime.GOOS == "windows" && m&fs.ModeIrregular != 0)
}

// restrict sets path's permissions (not on Windows, which has none).
func restrict(path string, mode os.FileMode) error {
	if goruntime.GOOS == "windows" {
		return nil
	}
	return os.Chmod(path, mode)
}

// Running reports whether a service answers on the socket.
func Running(path string) bool {
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// Client returns an HTTP client that reaches the service on the socket;
// use it with BaseURL.
func Client(path string) *http.Client {
	var d net.Dialer
	return &http.Client{Transport: named{&http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return d.DialContext(ctx, "unix", path)
		},
	}}}
}

// ClientHeader names the front end a request to the service comes from
// (spec_054_active_installs AI-02).
const ClientHeader = "Blitz-Client"

// Front is the front end this program is (activity.CLI, activity.Desktop),
// set once in its main: each request Client makes carries it as
// ClientHeader, unless the request names another (a page's, proxied).
var Front string

// named puts Front on each request.
type named struct{ base http.RoundTripper }

func (n named) RoundTrip(r *http.Request) (*http.Response, error) {
	if Front != "" && r.Header.Get(ClientHeader) == "" {
		r = r.Clone(r.Context())
		r.Header.Set(ClientHeader, Front)
	}
	return n.base.RoundTrip(r)
}
