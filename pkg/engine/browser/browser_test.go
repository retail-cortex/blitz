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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicAddr(t *testing.T) {
	for _, tt := range []struct {
		addr string
		want bool
	}{
		{"8.8.8.8", true}, {"2606:4700::1111", true},
		{"127.0.0.1", false}, {"::1", false}, {"10.1.2.3", false}, {"192.168.0.1", false},
		{"169.254.169.254", false}, {"100.64.0.1", false}, {"0.0.0.0", false}, {"224.0.0.1", false},
		{"::ffff:127.0.0.1", false},
	} {
		t.Run(tt.addr, func(t *testing.T) {
			assert.Equal(t, tt.want, PublicAddr(netip.MustParseAddr(tt.addr)))
		})
	}
}

// proxyClient is an HTTP client going through p.
func proxyClient(p *proxy) *http.Client {
	u, _ := url.Parse("http://" + p.addr())
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}, Timeout: 5 * time.Second}
}

func TestProxy(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "hello") }))
	defer site.Close()
	tlsSite := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "secure") }))
	defer tlsSite.Close()

	tests := []struct {
		name       string
		allowLocal bool
		deny       string
		wantBody   string
		wantStatus int
		refused    string
	}{
		{name: "local allowed", allowLocal: true, wantBody: "hello"},
		{name: "local refused as dialled", wantStatus: http.StatusBadGateway, refused: "not a public address"},
		{name: "denied host", allowLocal: true, deny: "127.0.0.1", wantStatus: http.StatusForbidden, refused: "denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var refused []string
			p, err := startProxy(
				func(a netip.Addr) bool { return tt.allowLocal || PublicAddr(a) },
				func(h string) error {
					if h == tt.deny {
						return errors.New("denied")
					}
					return nil
				},
				func(h string, err error) { mu.Lock(); refused = append(refused, h+": "+err.Error()); mu.Unlock() })
			require.NoError(t, err)
			defer p.close()
			c := proxyClient(p)

			resp, err := c.Get(site.URL)
			require.NoError(t, err)
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if tt.wantBody != "" {
				assert.Equal(t, tt.wantBody, string(body))
				// And a tunnel (https).
				tc := proxyClient(p)
				tc.Transport.(*http.Transport).TLSClientConfig = tlsSite.Client().Transport.(*http.Transport).TLSClientConfig
				resp, err := tc.Get(tlsSite.URL)
				require.NoError(t, err)
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				assert.Equal(t, "secure", string(body))
				return
			}
			assert.Equal(t, tt.wantStatus, resp.StatusCode)
			mu.Lock()
			first := append([]string{}, refused...)
			mu.Unlock()
			require.NotEmpty(t, first)
			assert.Contains(t, first[0], tt.refused)

			// Tunnels are refused the same way, at once.
			raw, err := net.Dial("tcp", p.addr())
			require.NoError(t, err)
			defer raw.Close()
			raw.SetDeadline(time.Now().Add(5 * time.Second))
			target := strings.TrimPrefix(tlsSite.URL, "https://")
			fmt.Fprintf(raw, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
			line, err := bufio.NewReader(raw).ReadString('\n')
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf("HTTP/1.1 %d %s\r\n", tt.wantStatus, http.StatusText(tt.wantStatus)), line)
		})
	}
}

// fakeDevTools is a DevTools endpoint answering from a table, and sending
// an event on request.
func fakeDevTools(t *testing.T, answer func(method string, params json.RawMessage) (any, string)) string {
	t.Helper()
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var m struct {
				ID     int64           `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if ws.ReadJSON(&m) != nil {
				return
			}
			if m.Method == "Test.emit" {
				ws.WriteJSON(map[string]any{"method": "Test.event", "params": map[string]any{"n": 1}})
			}
			result, errText := answer(m.Method, m.Params)
			if errText != "" {
				ws.WriteJSON(map[string]any{"id": m.ID, "error": map[string]any{"code": -32000, "message": errText}})
				continue
			}
			ws.WriteJSON(map[string]any{"id": m.ID, "result": result})
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestConn(t *testing.T) {
	ws := fakeDevTools(t, func(method string, params json.RawMessage) (any, string) {
		switch method {
		case "Echo":
			return map[string]any{"got": json.RawMessage(params)}, ""
		case "Fail":
			return nil, "no such thing"
		}
		return map[string]any{}, ""
	})
	ctx := context.Background()
	c, err := dial(ctx, ws)
	require.NoError(t, err)

	var out struct {
		Got struct{ X int } `json:"got"`
	}
	require.NoError(t, c.call(ctx, "Echo", map[string]any{"x": 7}, &out))
	assert.Equal(t, 7, out.Got.X)
	assert.ErrorContains(t, c.call(ctx, "Fail", nil, nil), "Fail: no such thing")

	events := make(chan json.RawMessage, 1)
	c.handle("Test.event", func(p json.RawMessage) { events <- p })
	require.NoError(t, c.call(ctx, "Test.emit", nil, nil))
	select {
	case p := <-events:
		assert.JSONEq(t, `{"n":1}`, string(p))
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	assert.ErrorIs(t, c.call(cctx, "Echo", nil, nil), context.Canceled)

	require.NoError(t, c.close())
	assert.ErrorIs(t, c.call(ctx, "Echo", nil, nil), errClosed)
}

func TestFind(t *testing.T) {
	_, err := Find("/no/such/browser")
	assert.ErrorContains(t, err, "not found")
	t.Setenv("PATH", t.TempDir())
	if _, err := Find(""); err != nil {
		assert.ErrorIs(t, err, ErrNoBrowser)
	}
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 5))
	assert.Equal(t, "ab…", truncate("abcdef", 2))
	assert.Equal(t, "…", truncate("é", 1), "never splits a character")
	s, cut, _ := truncateFlag("abcdef", 3)
	assert.True(t, cut)
	assert.Equal(t, "abc…", s)
}

// TestChrome drives a real browser: set BLITZ_TEST_CHROME=1 (bazel test
// --test_env=BLITZ_TEST_CHROME=1) with Chrome or Chromium installed.
func TestChrome(t *testing.T) {
	if os.Getenv("BLITZ_TEST_CHROME") == "" {
		t.Skip("set BLITZ_TEST_CHROME=1 to drive a real browser")
	}
	var mu sync.Mutex
	var posted []string
	mux := http.NewServeMux()
	var other string
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><head><title>Home</title></head><body><h1>Welcome</h1>
<a href="/next">Next page</a> <a id="away" href="%s/elsewhere">Away</a>
<form action="/search"><input name="q" id="q"></form>
<select id="size"><option value="s">Small</option><option value="l">Large</option></select>
<script>console.log("ready", 42); document.getElementById("size").addEventListener("change", e => console.warn("size", e.target.value));</script>
</body></html>`, other)
	})
	mux.HandleFunc("/next", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>Next</title></head><body>Second page</body></html>`)
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		posted = append(posted, r.URL.Query().Get("q"))
		mu.Unlock()
		fmt.Fprintf(w, `<html><head><title>Results</title></head><body>Results for %s</body></html>`, r.URL.Query().Get("q"))
	})
	site := httptest.NewServer(mux)
	defer site.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(site.URL, "http://"))
	other = "http://localhost:" + port // another host name, same server

	ctx := context.Background()
	start := time.Now()
	step := func(name string) {
		t.Logf("%-10s %v", name, time.Since(start).Round(time.Millisecond))
		start = time.Now()
	}
	defer step("close")
	b, err := Launch(ctx, Options{
		AllowLocal: true,
		Navigation: func(u *url.URL) error {
			if u.Hostname() == "localhost" {
				return errors.New("not approved")
			}
			return nil
		},
	})
	require.NoError(t, err)
	defer b.Close()

	p, err := b.Navigate(ctx, site.URL)
	require.NoError(t, err)
	assert.Equal(t, "Home", p.Title)
	step("navigate")
	text, _, err := b.Text(ctx, "", 0)
	require.NoError(t, err)
	assert.Contains(t, text, "Welcome")
	assert.Contains(t, fmt.Sprint(b.Console()), "ready 42")

	require.NoError(t, b.Select(ctx, "#size", "Large"))
	assert.Contains(t, fmt.Sprint(b.Console()), "size l")
	assert.ErrorContains(t, b.Select(ctx, "#size", "Huge"), "no option")

	step("select")
	require.NoError(t, b.Click(ctx, "text=Next page"))
	p, _ = b.Page(ctx)
	assert.Equal(t, "Next", p.Title, "a link on the same site")
	step("click")
	p, err = b.Back(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Home", p.Title)

	step("back")
	require.NoError(t, b.Type(ctx, "#q", "gophers", false, true))
	p, _ = b.Page(ctx)
	assert.Equal(t, "Results", p.Title)
	mu.Lock()
	assert.Equal(t, []string{"gophers"}, posted)
	mu.Unlock()

	step("type")
	_, err = b.Navigate(ctx, site.URL)
	require.NoError(t, err)
	b.Refused()
	require.NoError(t, b.Click(ctx, "#away"))
	p, _ = b.Page(ctx)
	assert.NotEqual(t, "Home", p.Title, "Chrome shows its error page")
	assert.Contains(t, fmt.Sprint(b.Refused()), "localhost: not approved")
	_, err = b.Navigate(ctx, site.URL)
	require.NoError(t, err)

	step("refused")
	v, err := b.Eval(ctx, "document.querySelectorAll('a').length")
	require.NoError(t, err)
	assert.Equal(t, "2", v)
	_, err = b.Eval(ctx, "throw new Error('boom')")
	assert.ErrorContains(t, err, "boom")
	html, _, err := b.HTML(ctx, "h1", 0)
	require.NoError(t, err)
	assert.Equal(t, "<h1>Welcome</h1>", html)
	assert.ErrorContains(t, b.Click(ctx, "#missing"), "no element")

	step("eval")
	png, err := b.Screenshot(ctx, true)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(png, []byte("\x89PNG")))
}

// Without allow_local, the browser can't reach the machine's own services.
func TestChromeRefusesLocal(t *testing.T) {
	if os.Getenv("BLITZ_TEST_CHROME") == "" {
		t.Skip("set BLITZ_TEST_CHROME=1 to drive a real browser")
	}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<title>Secret</title>")
	}))
	defer site.Close()
	ctx := context.Background()
	b, err := Launch(ctx, Options{})
	require.NoError(t, err)
	defer b.Close()
	p, _ := b.Navigate(ctx, site.URL)
	assert.NotEqual(t, "Secret", p.Title)
	assert.Contains(t, fmt.Sprint(b.Refused()), "not a public address")
}
