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

package pageserver

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeService answers every request on a socket with the Cookie and
// Origin headers it got, so a test sees what the proxy forwards.
func fakeService(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "psv") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")
	l, err := socket.Listen(path)
	require.NoError(t, err)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "cookie="+r.Header.Get("Cookie")+" origin="+r.Header.Get("Origin"))
	})}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return path
}

// start serves a page, the fake service and a host that says "host".
func start(t *testing.T, host http.Handler) *Server {
	t.Helper()
	s, err := Start(Options{
		Page:   fstest.MapFS{"index.html": {Data: []byte("<p>blitz</p>")}, "app.js": {Data: []byte("1")}},
		Socket: fakeService(t),
		Host:   host,
	})
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

// client is a browser that doesn't follow redirects, so a test sees them.
var client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// get sends a request with headers and returns the response and its body.
func get(t *testing.T, method, url string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res, string(body)
}

// The link lets a browser in once, with a cookie, and goes to the page.
func TestOpenLink(t *testing.T) {
	s := start(t, nil)
	link := s.OpenURL()
	require.True(t, strings.HasPrefix(link, s.Origin()+"/open?t="), link)

	res, _ := get(t, "GET", link, nil)
	require.Equal(t, http.StatusSeeOther, res.StatusCode)
	assert.Equal(t, "/", res.Header.Get("Location"))
	cookies := res.Cookies()
	require.Len(t, cookies, 1)
	c := cookies[0]
	assert.Equal(t, s.cookie, c.Name)
	assert.True(t, c.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite)

	res, _ = get(t, "GET", link, nil)
	assert.Equal(t, http.StatusForbidden, res.StatusCode, "a used link, without the cookie")
	res, _ = get(t, "GET", link, map[string]string{"Cookie": c.Name + "=" + c.Value})
	assert.Equal(t, http.StatusSeeOther, res.StatusCode, "a used link, already in: a reload")

	res, _ = get(t, "GET", s.Origin()+"/open?t=nonsense", nil)
	assert.Equal(t, http.StatusForbidden, res.StatusCode, "a made-up link")

	now := time.Now()
	s.now = func() time.Time { return now }
	late := s.OpenURL()
	s.now = func() time.Time { return now.Add(TicketTTL + time.Second) }
	res, _ = get(t, "GET", late, nil)
	assert.Equal(t, http.StatusForbidden, res.StatusCode, "an expired link")
	s.mu.Lock()
	assert.Empty(t, s.tickets, "used and expired links are forgotten")
	s.mu.Unlock()
}

// Only a request with the cookie, for the server's own host and from the
// page (or from nowhere) gets anything.
func TestRequestsNeedTheCookieHostAndOrigin(t *testing.T) {
	s := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "host "+r.URL.Path) }))
	res, _ := get(t, "GET", s.OpenURL(), nil)
	cookie := res.Cookies()[0]
	in := cookie.Name + "=" + cookie.Value
	for _, tc := range []struct {
		name, method, path string
		headers            map[string]string
		code               int
		body               string
	}{
		{"the page", "GET", "/", map[string]string{"Cookie": in}, http.StatusOK, "<p>blitz</p>"},
		{"its files", "GET", "/app.js", map[string]string{"Cookie": in}, http.StatusOK, "1"},
		{"the API, without the cookie or origin", "POST", "/blitz.v1.WorkspaceService/GetModel", map[string]string{"Cookie": in, "Origin": s.Origin()}, http.StatusOK, "cookie= origin="},
		{"the host", "POST", "/host/Version", map[string]string{"Cookie": in}, http.StatusOK, "host /host/Version"},
		{"no cookie", "GET", "/", nil, http.StatusForbidden, ""},
		{"no cookie, the API", "POST", "/blitz.v1.WorkspaceService/GetModel", nil, http.StatusForbidden, ""},
		{"no cookie, the host", "POST", "/host/Version", nil, http.StatusForbidden, ""},
		{"a wrong cookie", "GET", "/", map[string]string{"Cookie": cookie.Name + "=nope"}, http.StatusForbidden, ""},
		{"another host (DNS rebinding)", "GET", "/", map[string]string{"Cookie": in, "Host": "evil.example:80"}, http.StatusForbidden, ""},
		{"another origin", "POST", "/blitz.v1.WorkspaceService/GetModel", map[string]string{"Cookie": in, "Origin": "http://127.0.0.1:1"}, http.StatusForbidden, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, body := get(t, tc.method, s.Origin()+tc.path, tc.headers)
			assert.Equal(t, tc.code, res.StatusCode, body)
			if tc.body != "" {
				assert.Equal(t, tc.body, body)
			}
			assert.Equal(t, "DENY", res.Header.Get("X-Frame-Options"))
		})
	}
	res, _ = get(t, "GET", s.Origin()+"/", map[string]string{"Cookie": in})
	assert.Equal(t, "no-store", res.Header.Get("Cache-Control"), "index.html is never cached")
}

// Without a host, its calls aren't found.
func TestNoHost(t *testing.T) {
	s := start(t, nil)
	res, _ := get(t, "GET", s.OpenURL(), nil)
	c := res.Cookies()[0]
	res, _ = get(t, "POST", s.Origin()+"/host/Version", map[string]string{"Cookie": c.Name + "=" + c.Value})
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

// A Server needs a page, and listens only on a loopback address.
func TestStartRefuses(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    Options
	}{
		{"no page", Options{}},
		{"not loopback", Options{Page: fstest.MapFS{}, Addr: "0.0.0.0:0"}},
		{"a bad address", Options{Page: fstest.MapFS{}, Addr: "nowhere"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Start(tc.o)
			if s != nil {
				s.Close()
			}
			assert.Error(t, err)
		})
	}
}
