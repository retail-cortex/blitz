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

// fakeReply is an answer that sends events before its result.
type fakeReply struct {
	events []map[string]any
	result any
}

// fakeDevTools is a DevTools endpoint answering from a table, and sending
// an event on request (Test.emit): Test.silent is never answered and
// Test.hangUp closes the connection.
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
			switch m.Method {
			case "Test.emit": // the event in params, or Test.event
				ev := map[string]any{"method": "Test.event", "params": map[string]any{"n": 1}}
				var e struct {
					Method string          `json:"method"`
					Params json.RawMessage `json:"params"`
				}
				if json.Unmarshal(m.Params, &e) == nil && e.Method != "" {
					ev = map[string]any{"method": e.Method, "params": e.Params}
				}
				ws.WriteJSON(ev)
			case "Test.silent": // never answered
				continue
			case "Test.hangUp": // the browser goes away
				return
			}
			result, errText := answer(m.Method, m.Params)
			if errText != "" {
				ws.WriteJSON(map[string]any{"id": m.ID, "error": map[string]any{"code": -32000, "message": errText}})
				continue
			}
			if fr, ok := result.(fakeReply); ok {
				for _, ev := range fr.events {
					ws.WriteJSON(ev)
				}
				result = fr.result
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

// A call fails when its params can't be sent, when its context ends before
// the reply, and when the browser goes away while it waits.
func TestConnFailures(t *testing.T) {
	ws := fakeDevTools(t, func(string, json.RawMessage) (any, string) { return map[string]any{}, "" })
	ctx := context.Background()
	c, err := dial(ctx, ws)
	require.NoError(t, err)
	defer c.close()

	bad, err := dial(ctx, ws)
	require.NoError(t, err)
	defer bad.close()
	assert.Error(t, bad.call(ctx, "Echo", map[string]any{"x": make(chan int)}, nil), "params that aren't JSON")

	tctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, c.call(tctx, "Test.silent", nil, nil), context.DeadlineExceeded)

	waiting := make(chan error, 1)
	go func() { waiting <- c.call(ctx, "Test.silent", nil, nil) }()
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.pending) == 1
	}, 5*time.Second, 5*time.Millisecond)
	assert.ErrorIs(t, c.call(ctx, "Test.hangUp", nil, nil), errClosed)
	select {
	case err := <-waiting:
		assert.ErrorIs(t, err, errClosed, "a call waiting when the browser left")
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting call never ended")
	}
}

// evalResult is Runtime.evaluate's answer for a value.
func evalResult(v any) map[string]any {
	typ := "string"
	switch v.(type) {
	case bool:
		typ = "boolean"
	case float64:
		typ = "number"
	case nil:
		typ = "object"
	}
	return map[string]any{"result": map[string]any{"type": typ, "value": v}}
}

// attachFake attaches to a fakeDevTools answering what answer doesn't
// (a nil result and no error) with an empty result.
func attachFake(t *testing.T, opts Options, answer func(method string, params json.RawMessage) (any, string)) *Browser {
	t.Helper()
	ws := fakeDevTools(t, func(method string, params json.RawMessage) (any, string) {
		if r, e := answer(method, params); r != nil || e != "" {
			return r, e
		}
		return map[string]any{}, ""
	})
	b, err := Attach(context.Background(), ws, opts)
	require.NoError(t, err)
	t.Cleanup(func() { b.Close() })
	return b
}

// expression is Runtime.evaluate's script.
func expression(params json.RawMessage) string {
	var p struct {
		Expression string `json:"expression"`
	}
	json.Unmarshal(params, &p)
	return p.Expression
}

// Attach fails when any step of setting up the page does.
func TestAttachSetupFails(t *testing.T) {
	for _, method := range []string{"Page.enable", "Page.getFrameTree", "Emulation.setDeviceMetricsOverride", "Fetch.enable"} {
		t.Run(method, func(t *testing.T) {
			ws := fakeDevTools(t, func(m string, _ json.RawMessage) (any, string) {
				if m == method {
					return nil, "boom"
				}
				return map[string]any{}, ""
			})
			_, err := Attach(context.Background(), ws, Options{})
			assert.ErrorContains(t, err, method+": boom")
		})
	}
}

// Each action reports the browser's failure, or what it found instead of
// the element it wanted.
func TestBrowserActionErrors(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name    string
		fail    string // the method that fails
		eval    any    // what scripts evaluate to
		run     func(b *Browser) error
		wantErr string
	}{
		{"navigate", "Page.navigate", nil, func(b *Browser) error { _, err := b.Navigate(ctx, "https://a.example/"); return err }, "Page.navigate: boom"},
		{"back", "Runtime.evaluate", nil, func(b *Browser) error { _, err := b.Back(ctx); return err }, "Runtime.evaluate: boom"},
		{"click finding", "Runtime.evaluate", nil, func(b *Browser) error { return b.Click(ctx, "#a") }, "boom"},
		{"click pressing", "Input.dispatchMouseEvent", `{"x":1,"y":2}`, func(b *Browser) error { return b.Click(ctx, "#a") }, "Input.dispatchMouseEvent: boom"},
		{"type finding", "Runtime.evaluate", nil, func(b *Browser) error { return b.Type(ctx, "#a", "x", false, false) }, "boom"},
		{"type inserting", "Input.insertText", true, func(b *Browser) error { return b.Type(ctx, "#a", "x", false, false) }, "Input.insertText: boom"},
		{"type submitting", "Input.dispatchKeyEvent", true, func(b *Browser) error { return b.Type(ctx, "#a", "x", false, true) }, "Input.dispatchKeyEvent: boom"},
		{"select finding", "Runtime.evaluate", nil, func(b *Browser) error { return b.Select(ctx, "#a", "x") }, "boom"},
		{"select not a select", "", "not a select", func(b *Browser) error { return b.Select(ctx, "#a", "x") }, `"#a" is not a select`},
		{"text", "Runtime.evaluate", nil, func(b *Browser) error { _, _, err := b.Text(ctx, "#a", 0); return err }, "boom"},
		{"html", "Runtime.evaluate", nil, func(b *Browser) error { _, _, err := b.HTML(ctx, "", 0); return err }, "boom"},
		{"html nothing", "", nil, func(b *Browser) error { _, _, err := b.HTML(ctx, "#a", 0); return err }, `no element matches "#a"`},
		{"screenshot", "Page.captureScreenshot", nil, func(b *Browser) error { _, err := b.Screenshot(ctx, false); return err }, "Page.captureScreenshot: boom"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := attachFake(t, Options{}, func(method string, _ json.RawMessage) (any, string) {
				switch method {
				case tt.fail:
					return nil, "boom"
				case "Runtime.evaluate":
					return evalResult(tt.eval), ""
				}
				return nil, ""
			})
			assert.ErrorContains(t, tt.run(b), tt.wantErr)
		})
	}
}

// A page that fails to load is an error, with where the browser is.
func TestNavigateErrorText(t *testing.T) {
	b := attachFake(t, Options{}, func(method string, _ json.RawMessage) (any, string) {
		switch method {
		case "Page.navigate":
			return map[string]any{"errorText": "net::ERR_BLOCKED_BY_CLIENT"}, ""
		case "Runtime.evaluate":
			return evalResult(`{"url":"chrome-error://chromewebdata/","title":"blocked"}`), ""
		}
		return nil, ""
	})
	p, err := b.Navigate(context.Background(), "https://a.example/")
	assert.ErrorContains(t, err, "loading https://a.example/: net::ERR_BLOCKED_BY_CLIENT")
	assert.Equal(t, "blocked", p.Title)
}

// Navigate waits for the load event, or for the committed page to be
// complete, or for its timeout or context to end; and collects the
// console on the way.
func TestNavigateWaits(t *testing.T) {
	page := evalResult(`{"url":"https://a.example/","title":"A"}`)
	console := []map[string]any{
		{"method": "Runtime.consoleAPICalled", "params": map[string]any{"type": "log", "args": []any{map[string]any{"type": "object", "description": "HTMLDivElement"}, map[string]any{"type": "object", "value": map[string]any{"k": 1}}}}},
		{"method": "Runtime.consoleAPICalled", "params": "not an event"},
		{"method": "Runtime.exceptionThrown", "params": map[string]any{"exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "TypeError: x"}}}},
		{"method": "Log.entryAdded", "params": map[string]any{"entry": map[string]any{"level": "warning", "text": "blocked", "url": "https://a.example/x.js"}}},
		{"method": "Fetch.requestPaused", "params": "not an event"},
		{"method": "Page.frameNavigated", "params": map[string]any{"frame": map[string]any{"id": "sub", "parentId": "main"}}},
	}
	committed := map[string]any{"method": "Page.frameNavigated", "params": map[string]any{"frame": map[string]any{"id": "main"}}}
	for _, tt := range []struct {
		name     string
		events   []map[string]any
		ready    string
		timeout  time.Duration
		ctx      time.Duration
		wantErr  error
		wantLogs []ConsoleLine
	}{
		{name: "committed and complete", events: append(append([]map[string]any{}, console...), committed), ready: "complete", timeout: 5 * time.Second,
			wantLogs: []ConsoleLine{{"log", `HTMLDivElement {"k":1}`}, {"error", "Uncaught TypeError: x"}, {"warning", "blocked https://a.example/x.js"}}},
		{name: "nothing comes", timeout: 150 * time.Millisecond},
		{name: "still loading", events: []map[string]any{committed}, ready: "loading", timeout: 300 * time.Millisecond},
		{name: "context ends", timeout: 5 * time.Second, ctx: 150 * time.Millisecond, wantErr: context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := attachFake(t, Options{Timeout: tt.timeout}, func(method string, params json.RawMessage) (any, string) {
				switch method {
				case "Page.navigate":
					return fakeReply{events: tt.events, result: map[string]any{}}, ""
				case "Runtime.evaluate":
					if strings.Contains(expression(params), "readyState") {
						return evalResult(tt.ready), ""
					}
					return page, ""
				}
				return nil, ""
			})
			ctx := context.Background()
			if tt.ctx > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.ctx)
				defer cancel()
			}
			p, err := b.Navigate(ctx, "https://a.example/")
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "A", p.Title)
			assert.Equal(t, tt.wantLogs, b.Console())
		})
	}
}

// What a script evaluates to, as text: an exception's text when it has no
// description, an object's description when it has no value.
func TestEvalResults(t *testing.T) {
	for _, tt := range []struct {
		name    string
		answer  map[string]any
		want    string
		wantErr string
	}{
		{"description", map[string]any{"result": map[string]any{"type": "function", "description": "function f() {}"}}, "function f() {}", ""},
		{"exception text", map[string]any{"result": map[string]any{"type": "object"}, "exceptionDetails": map[string]any{"text": "Uncaught SyntaxError"}}, "", "Uncaught SyntaxError"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := attachFake(t, Options{}, func(method string, _ json.RawMessage) (any, string) {
				if method == "Runtime.evaluate" {
					return tt.answer, ""
				}
				return nil, ""
			})
			v, err := b.Eval(context.Background(), "f")
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, v)
		})
	}
}

// A cancelled script isn't sent, and doesn't wait for a navigation.
func TestEvalCancelled(t *testing.T) {
	b := attachFake(t, Options{}, func(string, json.RawMessage) (any, string) { return nil, "" })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.Eval(ctx, "1")
	assert.ErrorIs(t, err, context.Canceled)
}

// A main-frame load that isn't http or https isn't put to Navigation.
func TestNavigationOtherSchemes(t *testing.T) {
	decided := make(chan string, 1)
	asked := make(chan string, 1)
	b := attachFake(t, Options{Navigation: func(u *url.URL) error { asked <- u.String(); return errors.New("no") }},
		func(method string, params json.RawMessage) (any, string) {
			if method == "Fetch.continueRequest" || method == "Fetch.failRequest" {
				decided <- method
			}
			return nil, ""
		})
	require.NoError(t, b.conn.call(context.Background(), "Test.emit", map[string]any{"method": "Fetch.requestPaused",
		"params": map[string]any{"requestId": "r1", "frameId": "", "request": map[string]any{"url": "data:text/html,hi"}}}, nil))
	select {
	case m := <-decided:
		assert.Equal(t, "Fetch.continueRequest", m)
	case <-time.After(5 * time.Second):
		t.Fatal("the load wasn't decided")
	}
	assert.Empty(t, asked)
}

// A load whose URL Go can't parse (an invalid escape Chrome keeps) is
// refused, not a crash.
func TestNavigationUnparsableURL(t *testing.T) {
	decided := make(chan string, 1)
	b := attachFake(t, Options{Navigation: func(*url.URL) error { return nil }},
		func(method string, params json.RawMessage) (any, string) {
			if method == "Fetch.continueRequest" || method == "Fetch.failRequest" {
				decided <- method
			}
			return nil, ""
		})
	require.NoError(t, b.conn.call(context.Background(), "Test.emit", map[string]any{"method": "Fetch.requestPaused",
		"params": map[string]any{"requestId": "r1", "frameId": "", "request": map[string]any{"url": "http://x.example/a%zz"}}}, nil))
	select {
	case m := <-decided:
		assert.Equal(t, "Fetch.failRequest", m)
	case <-time.After(5 * time.Second):
		t.Fatal("the load wasn't decided")
	}
	refused := b.Refused()
	require.Len(t, refused, 1)
	assert.Contains(t, refused[0], "http://x.example/a%zz")
}

// The console keeps its last messages, skipping empty ones; what was
// refused is said once and only so many times; signals don't block.
func TestBrowserBookkeeping(t *testing.T) {
	b := newBrowser(Options{})
	b.addConsole("log", "")
	for i := range maxConsole + 5 {
		b.addConsole("log", fmt.Sprint(i))
	}
	c := b.Console()
	require.Len(t, c, maxConsole)
	assert.Equal(t, "5", c[0].Text, "the oldest are dropped")

	for i := range 30 {
		b.noteRefused(fmt.Sprintf("h%d.example", i), errors.New("no"))
		b.noteRefused(fmt.Sprintf("h%d.example", i), errors.New("no"))
	}
	r := b.Refused()
	assert.Len(t, r, 20)
	assert.Equal(t, "h0.example: no", r[0])

	for range cap(b.loads) + 2 {
		signal(b.loads)
	}
	assert.Len(t, b.loads, cap(b.loads))
	b.fresh()
	assert.Empty(t, b.loads)
}

// The tail of what the browser said: its last few lines, of the last few
// kilobytes.
func TestTailBuffer(t *testing.T) {
	var tb tailBuffer
	n, err := tb.Write([]byte(strings.Repeat("x", 5000)))
	require.NoError(t, err)
	assert.Equal(t, 5000, n)
	assert.Len(t, tb.buf, 4096)
	tb.Write([]byte("\none\ntwo\nthree\nfour\n"))
	assert.Equal(t, "two | three | four", tb.tail())
}

// The proxy refuses what isn't a proxied http request or a CONNECT to a
// host and port, and a tunnel carries what the client sent with its
// CONNECT.
func TestProxyRequests(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()
	p, err := startProxy(func(netip.Addr) bool { return true }, func(string) error { return nil }, func(string, error) {})
	require.NoError(t, err)
	defer p.close()

	for _, tt := range []struct {
		name, request, want string
	}{
		{"not proxied", "GET /x HTTP/1.1\r\nHost: a.example\r\n\r\n", "HTTP/1.1 400 Bad Request\r\n"},
		{"connect without a port", "CONNECT a.example HTTP/1.1\r\nHost: a.example\r\n\r\n", "HTTP/1.1 400 Bad Request\r\n"},
		{"tunnel", "CONNECT " + echo.Addr().String() + " HTTP/1.1\r\nHost: " + echo.Addr().String() + "\r\n\r\nping", "HTTP/1.1 200 Connection Established\r\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := net.Dial("tcp", p.addr())
			require.NoError(t, err)
			defer raw.Close()
			raw.SetDeadline(time.Now().Add(5 * time.Second))
			_, err = io.WriteString(raw, tt.request)
			require.NoError(t, err)
			r := bufio.NewReader(raw)
			line, err := r.ReadString('\n')
			require.NoError(t, err)
			assert.Equal(t, tt.want, line)
			if tt.name != "tunnel" {
				return
			}
			_, err = r.ReadString('\n') // the blank line ending the reply
			require.NoError(t, err)
			got := make([]byte, 4)
			_, err = io.ReadFull(r, got)
			require.NoError(t, err)
			assert.Equal(t, "ping", string(got), "sent with the CONNECT, echoed back")
		})
	}
}
