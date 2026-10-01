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

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/browser/browsertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goruntime "runtime"
)

// fakeChrome is an executable standing in for the browser: a shell script
// that finds its profile in dir and runs body.
func fakeChrome(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chrome")
	script := "#!/bin/sh\nfor a in \"$@\"; do case \"$a\" in --user-data-dir=*) dir=\"${a#--user-data-dir=}\";; esac; done\n" + body + "\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

// devToolsList serves /json/list: no page at first, then the targets.
func devToolsList(t *testing.T, targets ...map[string]string) string {
	t.Helper()
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/list" {
			http.NotFound(w, r)
			return
		}
		if asked.Add(1) == 1 {
			fmt.Fprint(w, "[]")
			return
		}
		json.NewEncoder(w).Encode(targets)
	}))
	t.Cleanup(srv.Close)
	return srv.URL[strings.LastIndex(srv.URL, ":")+1:]
}

// Launch starts the browser with its own profile and proxy, finds its page
// and drives it; Close kills a browser that doesn't exit.
func TestLaunch(t *testing.T) {
	page := browsertest.New(t)
	port := devToolsList(t, map[string]string{"type": "service_worker"}, map[string]string{"type": "page", "webSocketDebuggerUrl": page.URL})
	chrome := fakeChrome(t, "sleep 0.1\nprintf '%s\\n/devtools/browser/x\\n' "+port+" > \"$dir/DevToolsActivePort\"\nexec sleep 30")
	var hosts []string
	b, err := Launch(context.Background(), Options{Path: chrome, AllowLocal: true, Host: func(h string) error {
		hosts = append(hosts, h)
		if h == "denied.example" {
			return errors.New("denied")
		}
		return nil
	}})
	require.NoError(t, err)
	profile := b.profile
	assert.DirExists(t, profile)

	p, err := b.Navigate(context.Background(), "https://shop.example/")
	require.NoError(t, err)
	assert.Equal(t, browsertest.Title, p.Title)

	// The proxy applies the options.
	for _, tt := range []struct {
		addr string
		want bool
	}{{"8.8.8.8:443", true}, {"10.0.0.1:80", true}, {"169.254.169.254:80", false}} {
		t.Run(tt.addr, func(t *testing.T) {
			assert.Equal(t, tt.want, b.proxy.dialer.Control("tcp", tt.addr, nil) == nil)
		})
	}
	assert.ErrorContains(t, b.proxy.check("Denied.Example."), "denied")
	assert.Equal(t, []string{"denied.example"}, hosts, "the host as Options.Host sees it")
	assert.Contains(t, b.Refused(), "Denied.Example.: denied")

	start := time.Now()
	require.NoError(t, b.Close())
	assert.Less(t, time.Since(start), 10*time.Second)
	assert.NoDirExists(t, profile, "the profile is removed")
}

// Launch says why the browser couldn't be started or driven.
func TestLaunchFails(t *testing.T) {
	noPage := devToolsList(t)
	badPage := devToolsList(t, map[string]string{"type": "page", "webSocketDebuggerUrl": "ws://127.0.0.1:1/devtools/page/x"})
	writePort := func(port string) string { return "printf '%s\\n' " + port + " > \"$dir/DevToolsActivePort\"" }
	for _, tt := range []struct {
		name    string
		path    func(t *testing.T) string
		tmpdir  bool // TMPDIR doesn't exist
		wantErr string
	}{
		{name: "not found", path: func(*testing.T) string { return "/no/such/chrome" }, wantErr: "not found"},
		{name: "no profile", path: func(t *testing.T) string { return fakeChrome(t, "") }, tmpdir: true, wantErr: "no-such-dir"},
		{name: "not a program", path: func(t *testing.T) string {
			p := filepath.Join(t.TempDir(), "chrome")
			require.NoError(t, os.WriteFile(p, []byte("\x00\x01not a program"), 0o755))
			return p
		}, wantErr: "starting"},
		{name: "never starts", path: func(t *testing.T) string { return fakeChrome(t, "printf 'one\\ntwo\\nmissing libnss3\\n' >&2") },
			wantErr: "the browser didn't start in time (the browser said: one | two | missing libnss3)"},
		{name: "no page", path: func(t *testing.T) string { return fakeChrome(t, writePort(noPage)) }, wantErr: "the browser opened no page"},
		{name: "page unreachable", path: func(t *testing.T) string { return fakeChrome(t, writePort(badPage)) }, wantErr: "connect"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.path(t)
			if tt.tmpdir {
				t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "no-such-dir"))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			b, err := Launch(ctx, Options{Path: path})
			assert.Nil(t, b)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// Find looks for the browsers it knows on the PATH.
func TestFindOnPath(t *testing.T) {
	if goruntime.GOOS == "darwin" {
		t.Skip("macOS looks in /Applications first, where a browser may be installed")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "chromium"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", dir)
	got, err := Find("")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "chromium"), got)
}
