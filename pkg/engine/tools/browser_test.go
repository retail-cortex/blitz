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
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/browser"
	"github.com/retail-cortex/blitz/pkg/engine/browser/browsertest"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testBrowserTool(t *testing.T, hooks *Hooks, mutate func(*config.BrowserConfig, *WebFetchConfig)) *browserTool {
	t.Helper()
	rules, err := NewPermissionRules(config.PermissionsConfig{Deny: []string{"web(evil.example)"}}, "test")
	require.NoError(t, err)
	cfg := config.BrowserConfig{Enabled: true}
	web := WebFetchConfig{AllowNetwork: true, DenyDomains: []string{"*.blocked.example"}, AllowDomains: []string{"docs.example"}, Rules: rules}
	if mutate != nil {
		mutate(&cfg, &web)
	}
	bt := newBrowserTool(&Registry{hooks: hooks}, cfg, web, 30*time.Second, t.TempDir())
	t.Cleanup(bt.close)
	return bt
}

func TestBrowserToolRefusesWithoutLaunching(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.BrowserConfig, *WebFetchConfig)
		deny    bool
		in      BrowserInput
		wantErr string
	}{
		{name: "unknown action", in: BrowserInput{Action: "fly"}, wantErr: `unknown action "fly"`},
		{name: "no network", mutate: func(_ *config.BrowserConfig, w *WebFetchConfig) { w.AllowNetwork = false }, in: BrowserInput{Action: "read"}, wantErr: "network access is disabled"},
		{name: "not a web page", in: BrowserInput{Action: "navigate", URL: "file:///etc/passwd"}, wantErr: "only http and https"},
		{name: "credentials", in: BrowserInput{Action: "navigate", URL: "https://me:pw@example.com"}, wantErr: "credentials"},
		{name: "denied domain", in: BrowserInput{Action: "navigate", URL: "https://a.blocked.example/"}, wantErr: "web.deny_domains"},
		{name: "deny rule", in: BrowserInput{Action: "navigate", URL: "https://evil.example/"}, wantErr: "permission rule deny"},
		{name: "localhost", in: BrowserInput{Action: "navigate", URL: "http://localhost:3000"}, wantErr: "browser.allow_local"},
		{name: "private address", in: BrowserInput{Action: "navigate", URL: "http://10.0.0.8/"}, wantErr: "browser.allow_local"},
		{name: "metadata even when local is allowed", mutate: func(c *config.BrowserConfig, _ *WebFetchConfig) { c.AllowLocal = true },
			in: BrowserInput{Action: "navigate", URL: "http://169.254.169.254/"}, wantErr: "never reachable"},
		{name: "the user says no", in: BrowserInput{Action: "navigate", URL: "https://example.com/"}, deny: true, wantErr: "not approved"},
		{name: "script refused", in: BrowserInput{Action: "script", Script: "1+1"}, deny: true, wantErr: "not approved"},
		{name: "not recording", in: BrowserInput{Action: "stop_recording"}, wantErr: "not recording"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := api.DecisionOnce
			if tt.deny {
				d = api.DecisionDeny
			}
			hooks, _ := decisionHooks(d)
			bt := testBrowserTool(t, hooks, tt.mutate)
			out := bt.run(context.Background(), tt.in)
			assert.Contains(t, out.Error, tt.wantErr)
			assert.Nil(t, bt.b, "no browser started")
		})
	}
}

func TestBrowserNavigationDecisions(t *testing.T) {
	hooks, reqs := decisionHooks(api.DecisionOnce)
	bt := testBrowserTool(t, hooks, nil)
	bt.host = "shop.example"
	parse := func(s string) *url.URL { u, _ := url.Parse(s); return u }

	assert.NoError(t, bt.navigation(parse("https://shop.example/cart")), "the same site")
	assert.NoError(t, bt.navigation(parse("https://docs.example/")), "web.allow_domains")
	assert.ErrorContains(t, bt.navigation(parse("https://x.blocked.example/")), "deny_domains")
	assert.ErrorContains(t, bt.navigation(parse("https://other.example/")), "by itself", "no action running to ask in")
	assert.Empty(t, *reqs)

	bt.setAct(context.Background())
	assert.NoError(t, bt.navigation(parse("https://other.example/")), "asked while an action runs")
	require.Len(t, *reqs, 1)
	assert.Equal(t, "web:other.example", (*reqs)[0].Key, "shares web_fetch's approval")
	bt.clearAct()
}

func TestBrowserToolCloseAndChanges(t *testing.T) {
	hooks, _ := decisionHooks(api.DecisionOnce)
	bt := testBrowserTool(t, hooks, nil)
	out := bt.run(context.Background(), BrowserInput{Action: "close"})
	assert.Equal(t, "The browser is closed.", out.Note)
	for _, a := range []string{"navigate", "click", "type", "select", "back", "script"} {
		assert.True(t, changes(a), a)
	}
	for _, a := range []string{"read", "html", "screenshot", "console"} {
		assert.False(t, changes(a), a)
	}
	assert.ErrorContains(t, need(" ", "selector"), "selector is required")
}

// TestBrowserToolWithChrome runs the tool end to end; set
// BLITZ_TEST_CHROME=1 with Chrome or Chromium installed.
func TestBrowserToolWithChrome(t *testing.T) {
	if os.Getenv("BLITZ_TEST_CHROME") == "" {
		t.Skip("set BLITZ_TEST_CHROME=1 to drive a real browser")
	}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<title>App</title><button onclick="document.title='Clicked'; console.error('oops')">Go</button>`)
	}))
	defer site.Close()

	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	cfg.Images.Dir = t.TempDir()
	cfg.Sandbox.Shell = "off"
	cfg.Browser.AllowLocal = true
	cfg.Web.AllowDomains = []string{"127.0.0.1"}
	r, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	defer r.Close()
	r.browser.dir = t.TempDir()
	ctx := context.Background()

	out := r.browser.run(ctx, BrowserInput{Action: "record"})
	require.Empty(t, out.Error)
	out = r.browser.run(ctx, BrowserInput{Action: "navigate", URL: site.URL})
	require.Empty(t, out.Error)
	assert.Equal(t, "App", out.Title)
	out = r.browser.run(ctx, BrowserInput{Action: "click", Selector: "text=Go"})
	require.Empty(t, out.Error)
	assert.Equal(t, "Clicked", out.Title)
	assert.Contains(t, fmt.Sprint(out.Console), "oops")

	out = r.browser.run(ctx, BrowserInput{Action: "screenshot"})
	require.Empty(t, out.Error)
	assert.True(t, strings.HasPrefix(out.ImageURI, "blitz-image:"), out.ImageURI)
	assert.Contains(t, out.Note, "screenshot follows")

	out = r.browser.run(ctx, BrowserInput{Action: "stop_recording"})
	require.Empty(t, out.Error)
	require.Len(t, out.Frames, 2, "a screenshot after each step")
	assert.Contains(t, out.Frames[0], "001-navigate.png")
}

// The tool's actions, against a fake DevTools page.
func TestBrowserToolWithFakePage(t *testing.T) {
	page := browsertest.New(t)
	hooks, reqs := decisionHooks(api.DecisionOnce)
	bt := testBrowserTool(t, hooks, nil)
	store, err := images.OpenStore(t.TempDir())
	require.NoError(t, err)
	bt.r.images = store
	var launched browser.Options
	bt.launch = func(ctx context.Context, o browser.Options) (*browser.Browser, error) {
		launched = o
		return browser.Attach(ctx, page.URL, o)
	}
	ctx := context.Background()
	run := func(in BrowserInput) BrowserOutput { t.Helper(); return bt.run(ctx, in) }

	out := run(BrowserInput{Action: "record"})
	require.Empty(t, out.Error)
	assert.Contains(t, out.Note, "Recording")
	assert.Equal(t, "Already recording.", run(BrowserInput{Action: "record"}).Note)

	out = run(BrowserInput{Action: "navigate", URL: "https://shop.example/"})
	require.Empty(t, out.Error)
	assert.Equal(t, browsertest.Title, out.Title)
	assert.Equal(t, "https://shop.example/", out.URL)
	assert.Contains(t, fmt.Sprint(out.Console), "loaded")
	require.Len(t, *reqs, 1)
	assert.Equal(t, "web:shop.example", (*reqs)[0].Key)
	assert.ErrorContains(t, launched.Host("x.blocked.example"), "deny_domains")
	assert.ErrorContains(t, launched.Host("evil.example"), "permission rule")
	assert.NoError(t, launched.Host("shop.example"))

	// The click leaves for another site: asked about, as an action runs.
	out = run(BrowserInput{Action: "click", Selector: "text=Away"})
	require.Empty(t, out.Error)
	require.Eventually(t, func() bool { return page.Decided()["https://away.example/"] == "continue" }, 5*time.Second, 10*time.Millisecond)
	require.Len(t, *reqs, 2)
	assert.Equal(t, "web:away.example", (*reqs)[1].Key)

	for _, tt := range []struct {
		in      BrowserInput
		want    func(BrowserOutput)
		wantErr string
	}{
		{in: BrowserInput{Action: "click"}, wantErr: "selector is required"},
		{in: BrowserInput{Action: "type", Selector: "#q", Text: "socks", Submit: true}},
		{in: BrowserInput{Action: "type", Text: "x"}, wantErr: "selector is required"},
		{in: BrowserInput{Action: "select", Selector: "#size", Value: "Large"}},
		{in: BrowserInput{Action: "select", Value: "x"}, wantErr: "selector is required"},
		{in: BrowserInput{Action: "read"}, want: func(o BrowserOutput) { assert.Contains(t, o.Text, "Hello from the fake page") }},
		{in: BrowserInput{Action: "html", Selector: "h1"}, want: func(o BrowserOutput) { assert.Equal(t, "<h1>Fake</h1>", o.Text) }},
		{in: BrowserInput{Action: "script", Script: "1+1"}, want: func(o BrowserOutput) { assert.Equal(t, "2", o.Result) }},
		{in: BrowserInput{Action: "script"}, wantErr: "script is required"},
		{in: BrowserInput{Action: "console"}},
		{in: BrowserInput{Action: "back"}, want: func(o BrowserOutput) { assert.Equal(t, "https://back.example/", o.URL) }},
		{in: BrowserInput{Action: "screenshot"}, wantErr: "the screenshot can't be shown"},
	} {
		t.Run(tt.in.Action, func(t *testing.T) {
			out := run(tt.in)
			if tt.wantErr != "" {
				assert.Contains(t, out.Error, tt.wantErr)
				return
			}
			assert.Empty(t, out.Error)
			if tt.want != nil {
				tt.want(out)
			}
		})
	}
	out = run(BrowserInput{Action: "stop_recording"})
	require.Empty(t, out.Error)
	assert.Len(t, out.Frames, 6, "navigate, click, type, select, script and back: the steps that worked")
	out = run(BrowserInput{Action: "close"})
	assert.Nil(t, bt.b)
}
