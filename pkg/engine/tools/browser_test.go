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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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

// TestBrowserScriptApproval checks a page's scripts aren't approved by
// tools.auto_approve_commands, and web rules on the page's host apply to
// them, browser.allow_scripts or not (N26 engine-tools-1#21).
func TestBrowserScriptApproval(t *testing.T) {
	for _, tc := range []struct {
		name         string
		host         string
		allowScripts bool
		autoCommands bool
		wantErr      string
	}{
		{name: "auto_approve_commands doesn't cover scripts", host: "ok.example", autoCommands: true, wantErr: "no interactive approver"},
		{name: "deny rule on the page", host: "evil.example", allowScripts: true, wantErr: "denied by the permission rule deny web(evil.example)"},
		{name: "ask rule on the page", host: "ask.example", allowScripts: true, wantErr: "must be asked about"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hooks := NewHooks(Policy{AutoApproveCommands: tc.autoCommands})
			bt := testBrowserTool(t, hooks, func(c *config.BrowserConfig, w *WebFetchConfig) {
				c.AllowScripts = tc.allowScripts
				rules, err := NewPermissionRules(config.PermissionsConfig{Deny: []string{"web(evil.example)"}, Ask: []string{"web(ask.example)"}}, "test")
				require.NoError(t, err)
				w.Rules = rules
			})
			bt.host = tc.host
			out := bt.run(context.Background(), BrowserInput{Action: "script", Script: "1+1"})
			assert.Contains(t, out.Error, tc.wantErr)
			assert.Nil(t, bt.b, "no browser started")
		})
	}
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

// With the workspace's browser profile (BP-03, BP-04) the browser keeps it
// (ProfileDir), the profile's own sites go without asking and others ask
// even when web.allow_domains has them; a change of profile is a new
// browser.
func TestBrowserToolUsesTheWorkspacesProfile(t *testing.T) {
	page := browsertest.New(t)
	hooks, reqs := decisionHooks(api.DecisionOnce)
	bt := testBrowserTool(t, hooks, nil)
	bt.web.AllowDomains = []string{"shop.example"}
	var launches []browser.Options
	bt.launch = func(ctx context.Context, o browser.Options) (*browser.Browser, error) {
		launches = append(launches, o)
		return browser.Attach(ctx, page.URL, o)
	}
	dir, allow := "/profiles/work", []string{"mail.example"}
	bt.r.SetBrowserProfiles(func() BrowserProfiles {
		if dir == "" {
			return BrowserProfiles{}
		}
		p := BrowserProfile{Name: "work", Dir: dir, Allow: allow}
		return BrowserProfiles{Current: p, All: []BrowserProfile{p}}
	})
	ctx := context.Background()

	out := bt.run(ctx, BrowserInput{Action: "navigate", URL: "https://mail.example/"})
	require.Empty(t, out.Error)
	require.Len(t, launches, 1)
	assert.Equal(t, "/profiles/work", launches[0].ProfileDir)
	assert.Empty(t, *reqs, "the profile's own site isn't asked about")
	out = bt.run(ctx, BrowserInput{Action: "navigate", URL: "https://shop.example/"})
	require.Empty(t, out.Error)
	require.Len(t, *reqs, 1, "web.allow_domains doesn't cover a profile's browsing")

	dir = ""
	out = bt.run(ctx, BrowserInput{Action: "navigate", URL: "https://shop.example/"})
	require.Empty(t, out.Error)
	require.Len(t, launches, 2, "no profile now: a new browser")
	assert.Empty(t, launches[1].ProfileDir)
	assert.Len(t, *reqs, 1, "and web.allow_domains applies again")
	bt.mu.Lock()
	bt.closeLocked()
	bt.mu.Unlock()
}

// With the desktop app open, the agent browses in its view (AB-01): as
// browser.show says, or for "ask" as the chat answers, asked once (silent
// where no one can answer); no view, Chrome as before.
func TestBrowserToolInTheApp(t *testing.T) {
	tests := []struct {
		name     string
		show     string
		answer   string
		noWindow bool
		want     string // the view's show; "" for Chrome
		asked    int
	}{
		{name: "watch", show: "watch", want: "watch"},
		{name: "silent", show: "silent", want: "silent"},
		{name: "ask, watched", show: "ask", answer: "Watch it in a tab", want: "watch", asked: 1},
		{name: "ask, silent", show: "ask", answer: "Run silently", want: "silent", asked: 1},
		{name: "ask, no one to answer", show: "ask", want: "silent"},
		{name: "no window", show: "watch", noWindow: true, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := browsertest.New(t)
			hooks, _ := decisionHooks(api.DecisionOnce)
			asked := 0
			if tt.answer != "" {
				hooks.SetUserPrompter(func(_ context.Context, q string, options []string) (string, error) {
					asked++
					assert.Equal(t, []string{"Watch it in a tab", "Run silently"}, options)
					return tt.answer, nil
				})
			}
			bt := testBrowserTool(t, hooks, func(c *config.BrowserConfig, _ *WebFetchConfig) { c.Show = tt.show })
			launched := 0
			bt.launch = func(ctx context.Context, o browser.Options) (*browser.Browser, error) {
				launched++
				return browser.Attach(ctx, page.URL, o)
			}
			var opened []AgentBrowserOptions
			work := BrowserProfile{Name: "work", Dir: "/profiles/work"}
			bt.r.SetBrowserProfiles(func() BrowserProfiles { return BrowserProfiles{Current: work, All: []BrowserProfile{work}} })
			bt.r.SetAgentBrowserHost(func(ctx context.Context, o AgentBrowserOptions) (browser.Transport, error) {
				if tt.noWindow {
					return nil, errors.New("no desktop window")
				}
				opened = append(opened, o)
				ws, _, err := websocket.DefaultDialer.DialContext(ctx, page.URL, nil)
				return ws, err
			})
			ctx := context.Background()
			for range 2 {
				out := bt.run(ctx, BrowserInput{Action: "navigate", URL: "https://shop.example/"})
				require.Empty(t, out.Error)
				bt.mu.Lock()
				bt.closeLocked() // a new browser: the chat's choice is kept
				bt.mu.Unlock()
			}
			if tt.want == "" {
				assert.Equal(t, 2, launched, "Chrome")
				assert.Empty(t, opened)
				return
			}
			assert.Zero(t, launched)
			require.Len(t, opened, 2)
			assert.Equal(t, tt.want, opened[0].Show)
			assert.Equal(t, "work", opened[0].Profile, "the profile's partition")
			assert.Equal(t, asked, tt.asked, "asked once a chat")
		})
	}
}

// web_fetch's fallback reads a page's text through the browser, under the
// browser's checks (WEB-09).
func TestBrowserPageText(t *testing.T) {
	page := browsertest.New(t)
	hooks, _ := decisionHooks(api.DecisionOnce)
	bt := testBrowserTool(t, hooks, nil)
	bt.launch = func(ctx context.Context, o browser.Options) (*browser.Browser, error) {
		return browser.Attach(ctx, page.URL, o)
	}
	ctx := context.Background()

	out, err := bt.pageText(ctx, "https://shop.example/")
	require.NoError(t, err)
	assert.Equal(t, browsertest.Title, out.Title)
	assert.NotEmpty(t, out.Text)
	_, err = bt.pageText(ctx, "https://a.blocked.example/")
	assert.ErrorContains(t, err, "web.deny_domains")
}

// deadTransport is a desktop view that went before it answered.
type deadTransport struct{}

func (deadTransport) WriteJSON(any) error { return nil }
func (deadTransport) ReadJSON(any) error  { return io.EOF }
func (deadTransport) Close() error        { return nil }

// A desktop view that goes before it's set up is an error, not a page.
func TestBrowserToolViewThatWontConnect(t *testing.T) {
	hooks, _ := decisionHooks(api.DecisionOnce)
	bt := testBrowserTool(t, hooks, func(c *config.BrowserConfig, _ *WebFetchConfig) { c.Show = "silent" })
	bt.timeout = time.Second
	bt.launch = func(context.Context, browser.Options) (*browser.Browser, error) {
		t.Fatal("not Chrome: a window is watching")
		return nil, nil
	}
	bt.r.SetAgentBrowserHost(func(context.Context, AgentBrowserOptions) (browser.Transport, error) { return deadTransport{}, nil })
	out := bt.run(context.Background(), BrowserInput{Action: "navigate", URL: "https://shop.example/"})
	assert.NotEmpty(t, out.Error)
}

// The agent may browse with any of the person's profiles (BP-03b): the
// workspace's own without asking, another once the person allows it, and
// none on asking; it's told once a chat what it could use or why the
// workspace's isn't used (BP-03c).
func TestBrowserToolProfiles(t *testing.T) {
	work := BrowserProfile{Name: "work", Dir: "/profiles/work", Allow: []string{"mail.example"}}
	home := BrowserProfile{Name: "home", Dir: "/profiles/home", Allow: []string{"mail.example"}}
	tests := []struct {
		name     string
		profiles BrowserProfiles
		refuse   bool     // the person refuses the profile question
		steps    []string // each a navigate to mail.example with this profile
		wantErr  string   // the last step's
		wantDirs []string // the browsers' profile folders, in launch order
		asked    []string // the profile questions
		note     string   // in the first step's note
	}{
		{name: "the workspace's own, unasked", profiles: BrowserProfiles{Current: work, All: []BrowserProfile{work, home}},
			steps: []string{"", "work"}, wantDirs: []string{"/profiles/work"}, note: "Chrome's copy of the browser profile \"work\""},
		{name: "another, asked once and kept for the chat", profiles: BrowserProfiles{Current: work, All: []BrowserProfile{work, home}},
			steps: []string{"home", ""}, wantDirs: []string{"/profiles/home"}, asked: []string{"browser:profile:home"}},
		{name: "another, refused", profiles: BrowserProfiles{Current: work, All: []BrowserProfile{work, home}}, refuse: true,
			steps: []string{"home"}, wantErr: "not approved", asked: []string{"browser:profile:home"}},
		{name: "none chosen, the user's listed", profiles: BrowserProfiles{All: []BrowserProfile{work, home}},
			steps: []string{"", ""}, wantDirs: []string{""}, note: "pass profile with one of their browser profiles (the user is asked first): work (mail.example), home (mail.example)"},
		{name: "signed out on asking", profiles: BrowserProfiles{Current: work, All: []BrowserProfile{work}},
			steps: []string{"none"}, wantDirs: []string{""}},
		{name: "no such profile", profiles: BrowserProfiles{All: []BrowserProfile{work}},
			steps: []string{"play"}, wantErr: "no browser profile \"play\"; the user's profiles: work (mail.example)"},
		{name: "no profiles at all", steps: []string{"play"}, wantErr: "the user has none"},
		{name: "the workspace's withheld until trusted", profiles: BrowserProfiles{All: []BrowserProfile{work}, Withheld: "work"},
			steps: []string{""}, wantDirs: []string{""}, note: "\"work\" isn't used until the user trusts the project's settings"},
		{name: "the withheld one, asked", profiles: BrowserProfiles{All: []BrowserProfile{work}, Withheld: "work"},
			steps: []string{"work"}, wantDirs: []string{"/profiles/work"}, asked: []string{"browser:profile:work"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := browsertest.New(t)
			hooks, reqs := decisionHooks(api.DecisionOnce)
			if tt.refuse {
				hooks, reqs = decisionHooks(api.DecisionDeny)
			}
			bt := testBrowserTool(t, hooks, nil)
			var dirs []string
			bt.launch = func(ctx context.Context, o browser.Options) (*browser.Browser, error) {
				dirs = append(dirs, o.ProfileDir)
				return browser.Attach(ctx, page.URL, o)
			}
			bt.r.SetBrowserProfiles(func() BrowserProfiles { return tt.profiles })
			t.Cleanup(func() {
				bt.mu.Lock()
				bt.closeLocked()
				bt.mu.Unlock()
			})
			var outs []BrowserOutput
			for _, p := range tt.steps {
				outs = append(outs, bt.run(context.Background(), BrowserInput{Action: "navigate", URL: "https://mail.example/", Profile: p}))
			}
			last := outs[len(outs)-1]
			if tt.wantErr != "" {
				assert.Contains(t, last.Error, tt.wantErr)
			} else {
				for _, o := range outs {
					require.Empty(t, o.Error)
				}
			}
			assert.Equal(t, tt.wantDirs, dirs)
			var asked []string
			for _, r := range *reqs {
				if strings.HasPrefix(r.Key, "browser:profile:") {
					asked = append(asked, r.Key)
				}
			}
			assert.Equal(t, tt.asked, asked)
			if tt.note != "" {
				assert.Contains(t, outs[0].Note, tt.note)
				if len(outs) > 1 {
					assert.NotContains(t, outs[1].Note, tt.note, "once a chat")
				}
			}
		})
	}
}

// web_fetch loads a page of a site the profile in use signs in to in the
// browser, signed in (BP-07); any other page, or one the browser can't
// load, is a plain request.
func TestWebFetchSignedIn(t *testing.T) {
	tests := []struct {
		name    string
		as      string
		pageErr error
		want    bool
	}{
		{name: "signed in", as: "work", want: true},
		{name: "no profile has the site", as: ""},
		{name: "the browser can't load it", as: "work", pageErr: errors.New("no browser")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hosts, loaded []string
			f := newWebFetcher(WebFetchConfig{
				AllowNetwork: true, MaxBytes: 1 << 20,
				SignedInAs: func(_ context.Context, host string) string { hosts = append(hosts, host); return tt.as },
				Page: func(_ context.Context, u string) (BrowserOutput, error) {
					loaded = append(loaded, u)
					return BrowserOutput{URL: u, Title: "Inbox", Text: "3 unread"}, tt.pageErr
				},
			})
			u, err := url.Parse("https://Mail.Example/inbox")
			require.NoError(t, err)
			out, ok := f.signedIn(context.Background(), u)
			assert.Equal(t, tt.want, ok)
			assert.Equal(t, []string{"mail.example"}, hosts)
			if !tt.want {
				return
			}
			assert.Equal(t, []string{"https://Mail.Example/inbox"}, loaded)
			assert.Equal(t, "browser", out.Via)
			assert.Equal(t, "Inbox\n\n3 unread", out.Content)
			assert.Contains(t, out.Note, `signed in with the user's browser profile "work"`)
		})
	}
}
