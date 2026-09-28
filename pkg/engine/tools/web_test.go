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

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicAddr(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "::1", "fe80::1",
		"0.0.0.0", "100.64.0.1", "224.0.0.1", "::ffff:127.0.0.1", "fc00::1", "0.1.2.3"} {
		assert.False(t, publicAddr(netip.MustParseAddr(s)), "%s should be blocked", s)
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		assert.True(t, publicAddr(netip.MustParseAddr(s)), "%s should be allowed", s)
	}
}

func TestMatchDomain(t *testing.T) {
	globs := []string{"*.go.dev", "example.com"}
	for host, want := range map[string]bool{
		"pkg.go.dev": true, "go.dev": true, "EXAMPLE.com": true, "example.com.": true,
		"evilgo.dev": false, "go.dev.evil.com": false, "sub.example.com": false,
	} {
		got := matchDomain(globs, host)
		assert.Equal(t, want, got, "matchDomain(%q) = %v, want %v", host, got, want)
	}
}

func testFetcher(cfg WebFetchConfig) *webFetcher {
	cfg.AllowNetwork = true
	return newWebFetcher(cfg)
}

func TestWebFetchBlocksPrivateByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("secret internal")) }))
	defer srv.Close()

	out := testFetcher(WebFetchConfig{}).fetch(context.Background(), allowAll(), srv.URL)
	assert.Contains(t, out.Error, "not a public address", "expected loopback to be blocked: %+v", out)
	assert.NotContains(t, out.Content, "secret", "expected loopback to be blocked: %+v", out)
	// "localhost" resolves to loopback and is blocked at dial time too.
	u, _ := url.Parse(srv.URL)
	out = testFetcher(WebFetchConfig{}).fetch(context.Background(), allowAll(), "http://localhost:"+u.Port())
	assert.NotEqual(t, "", out.Error, "localhost should be blocked")
}

func TestWebFetchContent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><title>T</title><style>.x{}</style><script>alert(1)</script></head>
<body><h1>Heading</h1><p>Hello <b>world</b>.</p><ul><li>one</li><li>two</li></ul>
<a href="https://go.dev/doc">docs</a><a href="/rel">relative</a></body></html>`))
	})
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(strings.Repeat("a", 5000)))
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte{0, 1, 2})
	})
	mux.HandleFunc("/404", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", 404) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	f := testFetcher(WebFetchConfig{AllowPrivate: true, MaxBytes: 1000})
	ctx := context.Background()

	out := f.fetch(ctx, allowAll(), srv.URL+"/page")
	require.Equal(t, "", out.Error, out.Error)
	for _, want := range []string{"# Heading", "Hello world .", "- one", "- two", "docs (https://go.dev/doc)"} {
		assert.Contains(t, out.Content, want, "html text missing %q:\n%s", want, out.Content)
	}
	assert.NotContains(t, out.Content, "alert", "script/style leaked: %s", out.Content)
	assert.NotContains(t, out.Content, ".x{}", "script/style leaked: %s", out.Content)
	out = f.fetch(ctx, allowAll(), srv.URL+"/json")
	assert.Equal(t, `{"ok":true}`, out.Content, "json: %+v", out)
	out = f.fetch(ctx, allowAll(), srv.URL+"/big")
	assert.True(t, out.Truncated, "size cap: truncated=%v len=%d", out.Truncated, len(out.Content))
	assert.Len(t, out.Content, 1000, "size cap: truncated=%v len=%d", out.Truncated, len(out.Content))
	out = f.fetch(ctx, allowAll(), srv.URL+"/bin")
	assert.Contains(t, out.Error, "unsupported content type", "binary: %+v", out)
	out = f.fetch(ctx, allowAll(), srv.URL+"/404")
	assert.Equal(t, 404, out.Status, "404: %+v", out)
	assert.Equal(t, "HTTP 404", out.Error, "404: %+v", out)
}

func TestWebFetchValidationAndApproval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer srv.Close()
	ctx := context.Background()
	f := testFetcher(WebFetchConfig{AllowPrivate: true, DenyDomains: []string{"*.evil.test"}})

	for _, bad := range []string{"file:///etc/passwd", "ftp://x.org/f", "http://", "http://user:pw@example.com/", "https://a.evil.test/x"} {
		out := f.fetch(ctx, allowAll(), bad)
		assert.NotEqual(t, "", out.Error, "%q should be rejected", bad)
	}

	// Non-allow-listed hosts need approval, keyed per host.
	h, reqs := approverHooks(false)
	out := f.fetch(ctx, h, srv.URL)
	assert.Contains(t, out.Error, "not approved", "expected approval denial: %+v", out)
	assert.Len(t, *reqs, 1, "unexpected approval request %+v", *reqs)
	assert.Equal(t, "web:127.0.0.1", (*reqs)[0].Key, "unexpected approval request %+v", *reqs)
	assert.Equal(t, api.ActionNetwork, (*reqs)[0].Kind, "unexpected approval request %+v", *reqs)
	// Allow-listed hosts don't prompt.
	allowed := testFetcher(WebFetchConfig{AllowPrivate: true, AllowDomains: []string{"127.0.0.1"}})
	h, reqs = approverHooks(false)
	out = allowed.fetch(ctx, h, srv.URL)
	assert.Equal(t, "ok", out.Content, "allow-listed fetch: %+v prompts=%d", out, len(*reqs))
	assert.Len(t, *reqs, 0, "allow-listed fetch: %+v prompts=%d", out, len(*reqs))

	// Network disabled by sandbox.
	off := newWebFetcher(WebFetchConfig{AllowPrivate: true})
	out = off.fetch(ctx, allowAll(), srv.URL)
	assert.Contains(t, out.Error, "disabled", "expected network disabled: %+v", out)
}

func TestWebFetchRedirects(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("metadata-secret")) }))
	defer internal.Close()
	internalPort := netip.MustParseAddrPort(strings.TrimPrefix(internal.URL, "http://")).Port()

	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-internal":
			http.Redirect(w, r, internal.URL+"/", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/to-self":
			http.Redirect(w, r, "/final", http.StatusFound)
		default:
			w.Write([]byte("final page"))
		}
	}))
	defer public.Close()

	// Treat the "internal" server's port as a private destination.
	f := testFetcher(WebFetchConfig{AllowPrivate: true})
	f.allowAddr = func(ap netip.AddrPort) bool { return ap.Port() != internalPort }

	out := f.fetch(context.Background(), allowAll(), public.URL+"/to-internal")
	assert.NotEqual(t, "", out.Error, "redirect to internal address followed: %+v", out)
	assert.NotContains(t, out.Content, "metadata-secret", "redirect to internal address followed: %+v", out)
	assert.False(t, !errors.Is(errors.New(out.Error), ErrBlockedAddress) && !strings.Contains(out.Error, "not a public address") && !strings.Contains(out.Error, "redirected"), "unexpected error: %s", out.Error)
	out = f.fetch(context.Background(), allowAll(), public.URL+"/loop")
	assert.Contains(t, out.Error, "redirects", "redirect loop: %+v", out)
	out = f.fetch(context.Background(), allowAll(), public.URL+"/to-self")
	assert.Equal(t, "final page", out.Content, "same-host redirect: %+v", out)
	assert.True(t, strings.HasSuffix(out.FinalURL, "/final"), "same-host redirect: %+v", out)
}

func TestHTMLToText(t *testing.T) {
	got := htmlToText("<div>a</div><div>b</div><p></p><p></p><p>c</p><noscript>x</noscript>")
	assert.Equal(t, "a\nb\nc", got, "htmlToText = %q", got)
	got = htmlToText("not html at all")
	assert.Equal(t, "not html at all", got, "plain text = %q", got)
}
