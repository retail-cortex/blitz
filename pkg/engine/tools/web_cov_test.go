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
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Redirects to another host, or away from http(s), aren't followed.
func TestWebFetchRefusedRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-other-host":
			u, _ := url.Parse(srv.URL)
			http.Redirect(w, r, "http://localhost:"+u.Port()+"/", http.StatusFound)
		case "/to-ftp":
			http.Redirect(w, r, "ftp://files.example/", http.StatusFound)
		}
	}))
	defer srv.Close()
	f := testFetcher(WebFetchConfig{AllowPrivate: true})
	for path, want := range map[string]string{
		"/to-other-host": "redirected from 127.0.0.1 to localhost",
		"/to-ftp":        "only http and https",
	} {
		t.Run(path, func(t *testing.T) {
			out := f.fetch(context.Background(), allowAll(), srv.URL+path)
			assert.Contains(t, out.Error, want)
		})
	}
}

// web_fetch, through its tool: an unparsable URL is refused, and a long
// text page is cut to the output limit.
func TestWebFetchToolLimits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(strings.Repeat("a", webMaxOutputChars+10)))
	}))
	defer srv.Close()
	tl := toolOf(t)(NewWebFetchTool(WebFetchConfig{AllowNetwork: true, AllowPrivate: true, MaxBytes: 2 * webMaxOutputChars}, allowAll()))

	out := runTool(t, tl, map[string]any{"url": "http://[::1"})
	assert.Contains(t, errOf(out), "invalid URL")

	out = runTool(t, tl, map[string]any{"url": srv.URL})
	require.Empty(t, errOf(out))
	assert.Equal(t, true, out["truncated"])
	assert.Len(t, out["content"], webMaxOutputChars)
}

// Through a proxy, a host that doesn't resolve isn't fetched.
func TestWebFetchProxyLookupFails(t *testing.T) {
	oldProxy, oldLookup := proxyFromEnv, lookupHost
	t.Cleanup(func() { proxyFromEnv, lookupHost = oldProxy, oldLookup })
	proxyFromEnv = func(*http.Request) (*url.URL, error) { return url.Parse("http://127.0.0.1:1") }
	lookupHost = func(context.Context, string) ([]netip.Addr, error) { return nil, errors.New("no such host") }
	f := newWebFetcher(WebFetchConfig{AllowNetwork: true, AllowDomains: []string{"*"}})
	out := f.fetch(context.Background(), allowAll(), "http://nowhere.example/")
	assert.Contains(t, out.Error, "no such host")
}

// The real resolver answers for a literal address without the network.
func TestLookupHostLiteral(t *testing.T) {
	addrs, err := lookupHost(context.Background(), "127.0.0.1")
	require.NoError(t, err)
	require.Len(t, addrs, 1)
	assert.Equal(t, netip.MustParseAddr("127.0.0.1"), addrs[0].Unmap())
}

// Without Go's own transport there are no TLS settings to copy.
func TestDefaultTLSOtherTransport(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = http.NewFileTransport(http.Dir(t.TempDir()))
	assert.Nil(t, defaultTLS())
}

// Empty globs match nothing; runs of blank lines become one.
func TestWebHelpersEdges(t *testing.T) {
	assert.False(t, matchDomain([]string{" ", ""}, "example.com"))
	assert.Equal(t, "a\n\nb", collapseBlankLines("a\n\n\n\nb"))
}
