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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"
)

// Options configure a browser.
type Options struct {
	// Path is the browser to run; empty finds Chrome, Chromium, Edge or
	// Brave.
	Path string
	// Visible shows its window; by default it runs headless.
	Visible bool
	// Width and Height are the viewport (default 1280×800).
	Width, Height int
	// AllowLocal lets it reach loopback and private addresses (the user's
	// own app); link-local ones (cloud metadata) never.
	AllowLocal bool
	// Host decides each host it would reach (deny_domains); nil allows all.
	Host func(host string) error
	// Navigation decides each top-level page it would load, other than
	// those Navigate is asked for (links, redirects, forms): nil allows
	// all.
	Navigation func(u *url.URL) error
	// Timeout bounds a page load (default 30s).
	Timeout time.Duration
}

// ErrNoBrowser: no Chromium-family browser was found.
var ErrNoBrowser = errors.New("no Chrome, Chromium, Edge or Brave found: install one or set browser.path")

// Find is the browser to run: path when set, else the first one installed.
func Find(path string) (string, error) {
	if path != "" {
		if p, err := exec.LookPath(path); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("browser.path %q: not found", path)
	}
	var candidates []string
	switch goruntime.GOOS {
	case "darwin":
		for _, app := range []string{"Google Chrome", "Chromium", "Microsoft Edge", "Brave Browser", "Google Chrome Canary"} {
			candidates = append(candidates, filepath.Join("/Applications", app+".app", "Contents", "MacOS", app))
		}
	case "windows":
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			if base := os.Getenv(env); base != "" {
				candidates = append(candidates, filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"),
					filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
			}
		}
	}
	candidates = append(candidates, "google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "brave-browser")
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", ErrNoBrowser
}

// ConsoleLine is one message of the page's console, or an uncaught error.
type ConsoleLine struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

// Browser is a running browser with one page.
type Browser struct {
	opts    Options
	cmd     *exec.Cmd
	profile string
	proxy   *proxy
	conn    *conn

	mu        sync.Mutex
	mainFrame string
	loads     chan struct{} // a load event, for waiting
	committed chan struct{} // the main frame committed a navigation
	navigated chan struct{} // a navigation started
	console   []ConsoleLine
	refused   []string        // what was refused since the last Refused call
	allowNext map[string]bool // hosts Navigate was asked for, once each
	stderr    tailBuffer
}

// tailBuffer keeps the end of what the browser writes to stderr, to say
// why it failed.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	return len(p), nil
}

func (t *tailBuffer) tail() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(string(t.buf)), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, " | ")
}

const maxConsole = 200

// Launch starts a browser with a new, empty profile.
func Launch(ctx context.Context, opts Options) (*Browser, error) {
	path, err := Find(opts.Path)
	if err != nil {
		return nil, err
	}
	b := newBrowser(opts)
	opts = b.opts
	allowAddr := func(a netip.Addr) bool {
		a = a.Unmap()
		if PublicAddr(a) {
			return true
		}
		return opts.AllowLocal && (a.IsLoopback() || a.IsPrivate())
	}
	host := func(h string) error {
		if opts.Host != nil {
			return opts.Host(normalHost(h))
		}
		return nil
	}
	if b.proxy, err = startProxy(allowAddr, host, b.noteRefused); err != nil {
		return nil, err
	}
	if b.profile, err = os.MkdirTemp("", "blitz-browser-"); err != nil {
		b.proxy.close()
		return nil, err
	}
	args := []string{
		"--remote-debugging-port=0", "--user-data-dir=" + b.profile,
		"--proxy-server=http://" + b.proxy.addr(), "--proxy-bypass-list=<-loopback>",
		"--no-first-run", "--no-default-browser-check", "--disable-extensions", "--disable-sync",
		"--disable-background-networking", "--disable-component-update", "--disable-default-apps",
		"--disable-features=Translate,OptimizationHints,MediaRouter", "--password-store=basic", "--use-mock-keychain",
		fmt.Sprintf("--window-size=%d,%d", opts.Width, opts.Height),
	}
	if !opts.Visible {
		args = append(args, "--headless=new", "--hide-scrollbars", "--mute-audio")
	}
	if goruntime.GOOS == "linux" && os.Geteuid() == 0 {
		args = append(args, "--no-sandbox") // Chrome refuses to run as root otherwise
	}
	args = append(args, "about:blank")
	b.cmd = exec.Command(path, args...)
	b.cmd.Stderr = &b.stderr
	if err := b.cmd.Start(); err != nil {
		b.cleanup()
		return nil, fmt.Errorf("starting %s: %w", path, err)
	}
	if err := b.connect(ctx); err != nil {
		b.Close()
		if tail := b.stderr.tail(); tail != "" {
			err = fmt.Errorf("%w (the browser said: %s)", err, tail)
		}
		return nil, err
	}
	return b, nil
}

func newBrowser(opts Options) *Browser {
	if opts.Width <= 0 || opts.Height <= 0 {
		opts.Width, opts.Height = 1280, 800
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	return &Browser{opts: opts, loads: make(chan struct{}, 8), committed: make(chan struct{}, 8), navigated: make(chan struct{}, 8), allowNext: map[string]bool{}}
}

// Attach drives the page at a DevTools WebSocket address, in a browser
// someone else started: its network isn't filtered (Options.Host and
// AllowLocal don't apply), but page loads still go through Navigation.
func Attach(ctx context.Context, wsURL string, opts Options) (*Browser, error) {
	b := newBrowser(opts)
	c, err := dial(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	b.conn = c
	if err := b.setup(ctx); err != nil {
		b.Close()
		return nil, err
	}
	return b, nil
}

// connect finds the page the browser opened and attaches to it.
func (b *Browser) connect(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var port string
	for port == "" {
		if data, err := os.ReadFile(filepath.Join(b.profile, "DevToolsActivePort")); err == nil {
			if line, _, _ := strings.Cut(string(data), "\n"); line != "" {
				port = strings.TrimSpace(line)
				break
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("the browser didn't start in time")
		case <-time.After(50 * time.Millisecond):
		}
	}
	var targets []struct {
		Type string `json:"type"`
		WS   string `json:"webSocketDebuggerUrl"`
	}
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/json/list", nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			json.NewDecoder(resp.Body).Decode(&targets)
			resp.Body.Close()
		}
		ws := ""
		for _, t := range targets {
			if t.Type == "page" {
				ws = t.WS
				break
			}
		}
		if ws != "" {
			c, err := dial(ctx, ws)
			if err != nil {
				return err
			}
			b.conn = c
			return b.setup(ctx)
		}
		select {
		case <-ctx.Done():
			return errors.New("the browser opened no page")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (b *Browser) setup(ctx context.Context) error {
	c := b.conn
	c.handle("Page.loadEventFired", func(json.RawMessage) { signal(b.loads) })
	c.handle("Page.frameNavigated", func(p json.RawMessage) {
		var e struct {
			Frame struct {
				ParentID string `json:"parentId"`
			} `json:"frame"`
		}
		if json.Unmarshal(p, &e) == nil && e.Frame.ParentID == "" {
			signal(b.committed)
		}
	})
	c.handle("Page.frameStartedNavigating", func(json.RawMessage) { signal(b.navigated) })
	c.handle("Page.frameRequestedNavigation", func(json.RawMessage) { signal(b.navigated) })
	c.handle("Runtime.consoleAPICalled", func(p json.RawMessage) {
		var e struct {
			Type string `json:"type"`
			Args []struct {
				Value       any    `json:"value"`
				Description string `json:"description"`
			} `json:"args"`
		}
		if json.Unmarshal(p, &e) != nil {
			return
		}
		var parts []string
		for _, a := range e.Args {
			switch v := a.Value.(type) {
			case nil:
				parts = append(parts, a.Description)
			case string:
				parts = append(parts, v)
			default:
				j, _ := json.Marshal(v)
				parts = append(parts, string(j))
			}
		}
		b.addConsole(e.Type, strings.Join(parts, " "))
	})
	c.handle("Runtime.exceptionThrown", func(p json.RawMessage) {
		var e struct {
			Details struct {
				Text      string `json:"text"`
				Exception struct {
					Description string `json:"description"`
				} `json:"exception"`
			} `json:"exceptionDetails"`
		}
		if json.Unmarshal(p, &e) == nil {
			b.addConsole("error", strings.TrimSpace(e.Details.Text+" "+e.Details.Exception.Description))
		}
	})
	c.handle("Log.entryAdded", func(p json.RawMessage) {
		var e struct {
			Entry struct {
				Level string `json:"level"`
				Text  string `json:"text"`
				URL   string `json:"url"`
			} `json:"entry"`
		}
		if json.Unmarshal(p, &e) == nil {
			b.addConsole(e.Entry.Level, strings.TrimSpace(e.Entry.Text+" "+e.Entry.URL))
		}
	})
	c.handle("Fetch.requestPaused", func(p json.RawMessage) {
		var e struct {
			RequestID string `json:"requestId"`
			FrameID   string `json:"frameId"`
			Request   struct {
				URL string `json:"url"`
			} `json:"request"`
		}
		if json.Unmarshal(p, &e) != nil {
			return
		}
		go b.decideNavigation(e.RequestID, e.FrameID, e.Request.URL)
	})
	for _, m := range []string{"Page.enable", "Runtime.enable", "Log.enable"} {
		if err := c.call(ctx, m, nil, nil); err != nil {
			return err
		}
	}
	var tree struct {
		FrameTree struct {
			Frame struct {
				ID string `json:"id"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := c.call(ctx, "Page.getFrameTree", nil, &tree); err != nil {
		return err
	}
	b.mainFrame = tree.FrameTree.Frame.ID
	if err := c.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": b.opts.Width, "height": b.opts.Height, "deviceScaleFactor": 1, "mobile": false,
	}, nil); err != nil {
		return err
	}
	return c.call(ctx, "Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*", "resourceType": "Document"}}}, nil)
}

// decideNavigation lets a page load go on, unless it's the main frame's
// and Navigation refuses it.
func (b *Browser) decideNavigation(id, frame, raw string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u, err := url.Parse(raw)
	if err == nil && frame == b.mainFrame && b.opts.Navigation != nil && !b.takeAllowed(u.Hostname()) {
		if u.Scheme == "http" || u.Scheme == "https" {
			err = b.opts.Navigation(u)
		}
	}
	if err != nil {
		b.noteRefused(u.Hostname(), err)
		b.conn.call(ctx, "Fetch.failRequest", map[string]any{"requestId": id, "errorReason": "BlockedByClient"}, nil)
		return
	}
	b.conn.call(ctx, "Fetch.continueRequest", map[string]any{"requestId": id}, nil)
}

func (b *Browser) takeAllowed(host string) bool {
	host = normalHost(host)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.allowNext[host] {
		delete(b.allowNext, host)
		return true
	}
	return false
}

func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func drain(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func (b *Browser) addConsole(level, text string) {
	if text == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.console = append(b.console, ConsoleLine{Level: level, Text: truncate(text, 2000)})
	if len(b.console) > maxConsole {
		b.console = b.console[len(b.console)-maxConsole:]
	}
}

func (b *Browser) noteRefused(host string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	msg := host + ": " + err.Error()
	for _, r := range b.refused {
		if r == msg {
			return
		}
	}
	if len(b.refused) < 20 {
		b.refused = append(b.refused, msg)
	}
}

// Refused is what the browser was stopped from reaching since the last
// call.
func (b *Browser) Refused() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.refused
	b.refused = nil
	return r
}

// Console is the console's messages since the last call.
func (b *Browser) Console() []ConsoleLine {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.console
	b.console = nil
	return c
}

// Page is where the browser is.
type Page struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// Navigate loads u (already decided on) and waits for it.
func (b *Browser) Navigate(ctx context.Context, u string) (Page, error) {
	if pu, err := url.Parse(u); err == nil {
		b.mu.Lock()
		clear(b.allowNext)                            // an earlier one never used
		b.allowNext[normalHost(pu.Hostname())] = true // the caller decided on it
		b.mu.Unlock()
	}
	b.fresh()
	var res struct {
		ErrorText string `json:"errorText"`
	}
	if err := b.conn.call(ctx, "Page.navigate", map[string]any{"url": u}, &res); err != nil {
		return Page{}, err
	}
	if res.ErrorText != "" {
		p, _ := b.Page(ctx)
		return p, fmt.Errorf("loading %s: %s", u, res.ErrorText)
	}
	b.waitLoad(ctx, b.opts.Timeout)
	b.fresh()
	return b.Page(ctx)
}

// Back goes back a page.
func (b *Browser) Back(ctx context.Context) (Page, error) {
	b.fresh()
	if _, err := b.eval(ctx, "history.back(), true"); err != nil {
		return Page{}, err
	}
	b.settle(ctx)
	return b.Page(ctx)
}

// fresh forgets navigation signals from before an action, so settle sees
// only those the action caused.
func (b *Browser) fresh() {
	drain(b.navigated)
	drain(b.loads)
	drain(b.committed)
}

// waitLoad waits for the page to load: its load event, or, once the new
// document is committed, its being complete (a page restored from the
// back/forward cache fires no load event).
func (b *Browser) waitLoad(ctx context.Context, d time.Duration) {
	deadline := time.After(d)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	committed := false
	for {
		select {
		case <-b.loads:
			return
		case <-b.committed:
			committed = true
		case <-tick.C:
			if !committed {
				continue
			}
			pctx, cancel := context.WithTimeout(ctx, time.Second)
			state, err := b.eval(pctx, "document.readyState")
			cancel()
			if err == nil && state == "complete" {
				return
			}
		case <-deadline:
			return
		case <-ctx.Done():
			return
		}
	}
}

// settle waits for a navigation an action may have started to load.
func (b *Browser) settle(ctx context.Context) {
	select {
	case <-b.navigated:
		b.waitLoad(ctx, b.opts.Timeout)
		b.fresh()
	case <-time.After(400 * time.Millisecond):
	case <-ctx.Done():
	}
}

// Page is the current page.
func (b *Browser) Page(ctx context.Context) (Page, error) {
	v, err := b.eval(ctx, "JSON.stringify({url: location.href, title: document.title})")
	if err != nil {
		return Page{}, err
	}
	var p Page
	json.Unmarshal([]byte(v), &p)
	return p, nil
}

// eval runs expr in the page and returns its value as text.
func (b *Browser) eval(ctx context.Context, expr string) (string, error) {
	var res struct {
		Result struct {
			Type        string          `json:"type"`
			Value       json.RawMessage `json:"value"`
			Description string          `json:"description"`
		} `json:"result"`
		Exception *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := b.conn.call(ctx, "Runtime.evaluate", map[string]any{
		"expression": expr, "returnByValue": true, "awaitPromise": true, "userGesture": true,
	}, &res); err != nil {
		return "", err
	}
	if res.Exception != nil {
		msg := res.Exception.Exception.Description
		if msg == "" {
			msg = res.Exception.Text
		}
		return "", errors.New(msg)
	}
	switch res.Result.Type {
	case "undefined":
		return "", nil
	case "string":
		var s string
		json.Unmarshal(res.Result.Value, &s)
		return s, nil
	}
	if len(res.Result.Value) > 0 {
		return string(res.Result.Value), nil
	}
	return res.Result.Description, nil
}

// Eval runs a script the model wrote in the page and returns its value
// (awaited, as JSON unless a string).
func (b *Browser) Eval(ctx context.Context, script string) (string, error) {
	b.fresh()
	out, err := b.eval(ctx, script)
	b.settle(ctx)
	return out, err
}

// find is JavaScript giving the element selector names: a CSS selector,
// or text=… for the first visible element whose text is that.
func find(selector string) string {
	q, _ := json.Marshal(selector)
	return `(() => {
  const s = ` + string(q) + `;
  if (s.startsWith("text=")) {
    const want = s.slice(5).trim().toLowerCase();
    const all = [...document.querySelectorAll("a, button, input[type=submit], input[type=button], [role=button], [role=link], label, summary, option, li, td, span, div, h1, h2, h3, p")];
    const visible = e => { const r = e.getBoundingClientRect(); return r.width > 0 && r.height > 0; };
    return all.find(e => visible(e) && (e.innerText || e.value || "").trim().toLowerCase() === want)
      || all.find(e => visible(e) && (e.innerText || e.value || "").trim().toLowerCase().includes(want)) || null;
  }
  return document.querySelector(s);
})()`
}

// Click clicks the element's middle, as a mouse would.
func (b *Browser) Click(ctx context.Context, selector string) error {
	v, err := b.eval(ctx, `(() => { const e = `+find(selector)+`; if (!e) return "";
  e.scrollIntoView({block: "center", inline: "center"}); const r = e.getBoundingClientRect();
  return JSON.stringify({x: r.left + r.width / 2, y: r.top + r.height / 2}); })()`)
	if err != nil {
		return err
	}
	if v == "" {
		return fmt.Errorf("no element matches %q", selector)
	}
	var at struct{ X, Y float64 }
	json.Unmarshal([]byte(v), &at)
	b.fresh()
	for _, t := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
		params := map[string]any{"type": t, "x": at.X, "y": at.Y, "button": "left", "clickCount": 1}
		if t == "mouseMoved" {
			params["button"] = "none"
		}
		if err := b.conn.call(ctx, "Input.dispatchMouseEvent", params, nil); err != nil {
			return err
		}
	}
	b.settle(ctx)
	return nil
}

// Type puts text in the element (replacing what's there unless add), and
// presses Enter after when submit.
func (b *Browser) Type(ctx context.Context, selector, text string, add, submit bool) error {
	clear := "true"
	if add {
		clear = "false"
	}
	v, err := b.eval(ctx, `(() => { const e = `+find(selector)+`; if (!e) return false;
  e.scrollIntoView({block: "center"}); e.focus();
  if (`+clear+`) { if ("value" in e) { e.value = ""; e.dispatchEvent(new Event("input", {bubbles: true})); } else if (e.isContentEditable) { e.textContent = ""; } }
  return true; })()`)
	if err != nil {
		return err
	}
	if v != "true" {
		return fmt.Errorf("no element matches %q", selector)
	}
	if err := b.conn.call(ctx, "Input.insertText", map[string]any{"text": text}, nil); err != nil {
		return err
	}
	if !submit {
		return nil
	}
	b.fresh()
	for _, t := range []string{"keyDown", "keyUp"} {
		params := map[string]any{"type": t, "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13, "nativeVirtualKeyCode": 13}
		if t == "keyDown" {
			params["text"] = "\r"
		}
		if err := b.conn.call(ctx, "Input.dispatchKeyEvent", params, nil); err != nil {
			return err
		}
	}
	b.settle(ctx)
	return nil
}

// Select chooses the option of a <select> whose value or text is value.
func (b *Browser) Select(ctx context.Context, selector, value string) error {
	q, _ := json.Marshal(value)
	b.fresh()
	v, err := b.eval(ctx, `(() => { const e = `+find(selector)+`; if (!e) return "none";
  if (e.tagName !== "SELECT") return "not a select";
  const want = `+string(q)+`; const o = [...e.options].find(o => o.value === want || o.text.trim() === want);
  if (!o) return "no option"; e.value = o.value;
  e.dispatchEvent(new Event("input", {bubbles: true})); e.dispatchEvent(new Event("change", {bubbles: true}));
  return "ok"; })()`)
	if err != nil {
		return err
	}
	switch v {
	case "ok":
		b.settle(ctx)
		return nil
	case "none":
		return fmt.Errorf("no element matches %q", selector)
	case "no option":
		return fmt.Errorf("%q has no option %q", selector, value)
	}
	return fmt.Errorf("%q is %s", selector, v)
}

// Text is the page's readable text (or the element's), up to max bytes.
func (b *Browser) Text(ctx context.Context, selector string, max int) (string, bool, error) {
	target := "document.body"
	if selector != "" {
		target = find(selector)
	}
	v, err := b.eval(ctx, `(() => { const e = `+target+`; return e ? e.innerText : null; })()`)
	if err != nil {
		return "", false, err
	}
	if v == "null" {
		return "", false, fmt.Errorf("no element matches %q", selector)
	}
	return truncateFlag(v, max)
}

// HTML is the page's (or the element's) HTML, up to max bytes.
func (b *Browser) HTML(ctx context.Context, selector string, max int) (string, bool, error) {
	target := "document.documentElement"
	if selector != "" {
		target = find(selector)
	}
	v, err := b.eval(ctx, `(() => { const e = `+target+`; return e ? e.outerHTML : null; })()`)
	if err != nil {
		return "", false, err
	}
	if v == "null" {
		return "", false, fmt.Errorf("no element matches %q", selector)
	}
	return truncateFlag(v, max)
}

// Screenshot is a PNG of the viewport, or of the whole page.
func (b *Browser) Screenshot(ctx context.Context, full bool) ([]byte, error) {
	var res struct {
		Data string `json:"data"`
	}
	if err := b.conn.call(ctx, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": full}, &res); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(res.Data)
}

// Close stops the browser and removes its profile.
func (b *Browser) Close() error {
	if b.conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		b.conn.call(ctx, "Browser.close", nil, nil)
		cancel()
		b.conn.close()
	}
	if b.cmd != nil && b.cmd.Process != nil {
		done := make(chan struct{})
		go func() { b.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			b.cmd.Process.Kill()
			<-done
		}
	}
	b.cleanup()
	return nil
}

func (b *Browser) cleanup() {
	if b.proxy != nil {
		b.proxy.close()
	}
	if b.profile != "" {
		os.RemoveAll(b.profile)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func utf8Start(c byte) bool { return c&0xC0 != 0x80 }

func truncateFlag(s string, n int) (string, bool, error) {
	if n > 0 && len(s) > n {
		return truncate(s, n), true, nil
	}
	return s, false, nil
}
