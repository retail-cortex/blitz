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
	"path/filepath"
	"slices"
	"sync"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/socket"
)

// The service's socket is guarded only by its 0600 mode: anything running
// as the user may connect, and a sandboxed command runs as the user. Over
// it, RunShell runs a command outside the sandbox and Approve answers an
// approval, so the sandbox keeps sandboxed commands away from it (spec_007
// SH-23a): they can't connect to the service's sockets or anything in
// Blitz's run folder (~/.blitz/run), or write there, whatever the network
// setting. Other Unix sockets (an SSH agent, Docker, a database) still work
// when the network is off.
var (
	guardMu      sync.Mutex
	guardedSocks []string
)

// GuardSocket keeps sandboxed commands from connecting to the Unix socket
// at path from then on, beside $BLITZ_SOCKET or the default
// ~/.blitz/run/blitz.sock, which are always guarded. The service calls it
// with the socket it listens on.
func GuardSocket(path string) {
	if path == "" {
		return
	}
	guardMu.Lock()
	defer guardMu.Unlock()
	if !slices.Contains(guardedSocks, path) {
		guardedSocks = append(guardedSocks, path)
	}
}

// serviceSockets returns Blitz's run folder and the sockets sandboxed
// commands may not reach, each as given and as its real path. Replaced in
// tests.
var serviceSockets = func() (runDirs, socks []string) {
	guardMu.Lock()
	given := append([]string{socket.DefaultSocket()}, guardedSocks...)
	guardMu.Unlock()
	return realAndGiven([]string{config.ExpandHome("~/.blitz/run")}), realAndGiven(given)
}

// realAndGiven returns each path made absolute and, when links lead
// elsewhere, its real location too: the sandboxes match the path a
// process names and the one it resolves to.
func realAndGiven(paths []string) []string {
	var out []string
	add := func(p string) {
		if p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	for _, p := range paths {
		abs, err := filepath.Abs(config.ExpandHome(p))
		if err != nil {
			continue
		}
		add(abs)
		if real, err := resolveExistingPrefix(abs); err == nil {
			add(real)
		}
	}
	return out
}
