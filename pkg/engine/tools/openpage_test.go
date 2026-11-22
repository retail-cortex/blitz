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
	"net/netip"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// systemOpens records what open_page gave the system's browser.
func systemOpens(t *testing.T, fail error) *[]string {
	t.Helper()
	var opened []string
	old := openSystemBrowser
	openSystemBrowser = func(u string) error {
		opened = append(opened, u)
		return fail
	}
	t.Cleanup(func() { openSystemBrowser = old })
	return &opened
}

// open_page takes web addresses only, never a denied host.
func TestOpenPageURL(t *testing.T) {
	rules, err := NewPermissionRules(config.PermissionsConfig{Deny: []string{"web(evil.example)"}}, "test")
	require.NoError(t, err)
	r := &Registry{rules: rules, web: WebFetchConfig{DenyDomains: []string{"*.blocked.example"}}}
	tests := []struct {
		name, in, want, err string
	}{
		{name: "a bare host", in: " github.com ", want: "https://github.com"},
		{name: "http", in: "http://localhost:3000/x?y=1", want: "http://localhost:3000/x?y=1"},
		{name: "empty", in: "", err: "url is required"},
		{name: "another scheme", in: "file:///etc/passwd", err: "only http and https"},
		{name: "no host", in: "https://", err: "not a web address"},
		{name: "deny_domains", in: "https://a.blocked.example/", err: "web.deny_domains"},
		{name: "a deny rule", in: "evil.example", err: "deny web(evil.example)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.pageURL(tt.in)
			if tt.err != "" {
				assert.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// With the desktop app open, a page opens where browser.open says, or
// for "ask" where the chat answers (asked once, the embedded browser
// first); without the app, or when the window can't, the system's
// browser; a job's run opens nothing.
func TestOpenPage(t *testing.T) {
	tests := []struct {
		name       string
		open       string
		answer     string
		noWindow   bool
		windowFail bool
		unattended bool
		want       string // where it opened
		window     []string
		system     int
		asked      int
		err        string
	}{
		{name: "embedded", open: "embedded", want: PageEmbedded, window: []string{"embedded", "embedded"}},
		{name: "system", open: "system", want: PageSystem, window: []string{"system", "system"}},
		{name: "ask, embedded", open: "ask", answer: "Embedded browser", want: PageEmbedded, window: []string{"embedded", "embedded"}, asked: 1},
		{name: "ask, system", open: "ask", answer: "System browser", want: PageSystem, window: []string{"system", "system"}, asked: 1},
		{name: "ask, no one to answer", open: "ask", want: PageEmbedded, window: []string{"embedded", "embedded"}},
		{name: "no window", open: "embedded", noWindow: true, want: PageSystem, system: 2},
		{name: "the window can't", open: "system", windowFail: true, want: PageSystem, system: 2},
		{name: "the window can't, ask", open: "ask", answer: "Embedded browser", windowFail: true, want: PageSystem, system: 2, asked: 1},
		// Asked for the app's tab: never the service machine's browser
		// instead (N26 engine-tools-1#15).
		{name: "the window can't, embedded", open: "embedded", windowFail: true, err: "browser.open = \"embedded\""},
		{name: "a job's run", open: "embedded", unattended: true, err: "nobody is watching"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			system := systemOpens(t, nil)
			hooks, _ := decisionHooks(api.DecisionOnce)
			asked := 0
			if tt.answer != "" {
				hooks.SetUserPrompter(func(_ context.Context, q string, options []string) (string, error) {
					asked++
					assert.Equal(t, "Open github.com in Blitz's embedded browser or your system browser?", q)
					assert.Equal(t, []string{"Embedded browser", "System browser"}, options)
					return tt.answer, nil
				})
			}
			r := &Registry{hooks: hooks}
			r.page.open = tt.open
			var window []string
			if !tt.noWindow {
				r.SetPageOpener(func(_ context.Context, u, where, _ string) error {
					assert.Equal(t, "https://github.com", u)
					if tt.windowFail {
						return errors.New("no desktop window")
					}
					window = append(window, where)
					return nil
				})
			}
			ctx := WithOwnerSession(context.Background(), "s1")
			if tt.unattended {
				ctx = Unattended(ctx, func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionDeny, nil })
			}
			for range 2 { // the same chat, twice
				got, err := r.openPage(ctx, "https://github.com", "")
				if tt.err != "" {
					assert.ErrorContains(t, err, tt.err)
					continue
				}
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
			assert.Equal(t, tt.window, window)
			assert.Len(t, *system, tt.system)
			assert.Equal(t, tt.asked, asked, "asked once a chat")
		})
	}
}

// The tool says where it opened, or why it didn't: an address it
// won't open, or a system browser that won't start.
func TestOpenPageTool(t *testing.T) {
	r := &Registry{hooks: &Hooks{}}
	r.SetPageOpener(func(context.Context, string, string, string) error { return nil })
	r.page.open = PageEmbedded
	open := toolOf(t)(NewOpenPageTool(r))
	out := runTool(t, open, map[string]any{"url": "github.com"})
	assert.Equal(t, map[string]any{"url": "https://github.com", "where": "embedded"}, out)
	assert.Contains(t, errOf(runTool(t, open, map[string]any{"url": "ftp://x"})), "only http and https")

	r.SetPageOpener(nil)
	systemOpens(t, errors.New("no opener"))
	out = runTool(t, open, map[string]any{"url": "github.com"})
	assert.Contains(t, errOf(out), "couldn't open the system browser")
	assert.Equal(t, "https://github.com", out["url"])
}

// An ask web(…) rule puts open_page to the person, and a deleted
// session's answers are forgotten (N26 engine-tools-1#13, #34).
func TestOpenPageAskRuleAndSessionGone(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		decision  api.Decision
		asked     int
		wantErr   string
	}{
		{name: "ask rule, declined", url: "ask.example", decision: api.DecisionDeny, asked: 1, wantErr: "declined"},
		{name: "ask rule, approved", url: "ask.example/x", decision: api.DecisionOnce, asked: 1},
		{name: "no rule", url: "ok.example", decision: api.DecisionDeny},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hooks, reqs := decisionHooks(tc.decision)
			r := &Registry{hooks: hooks, rules: rulesOf(t, nil, []string{"web(ask.example)"}, nil)}
			r.SetPageOpener(func(context.Context, string, string, string) error { return nil })
			r.page.open = PageEmbedded
			out := runTool(t, toolOf(t)(NewOpenPageTool(r)), map[string]any{"url": tc.url})
			if tc.wantErr != "" {
				assert.Contains(t, errOf(out), tc.wantErr)
			} else {
				assert.Empty(t, errOf(out))
			}
			require.Len(t, *reqs, tc.asked)
			if tc.asked > 0 {
				assert.True(t, (*reqs)[0].MustAsk)
			}
		})
	}

	r := &Registry{hooks: &Hooks{}, browser: &browserTool{shows: map[string]string{"s1": "watch", "s2": "silent"}}}
	r.page.where = map[string]string{"s1": PageSystem, "s2": PageEmbedded}
	r.SessionGone("s1")
	assert.Equal(t, map[string]string{"s2": PageEmbedded}, r.page.where)
	assert.Equal(t, map[string]string{"s2": "silent"}, r.browser.shows)
}

// open_page shows a page signed in with a browser profile (BP-08): the one
// named, asked first unless it's the workspace's own, else the one the
// chat browses with; the system's browser has its own sign-ins.
func TestOpenPageProfiles(t *testing.T) {
	work := BrowserProfile{Name: "work", Dir: "/profiles/work"}
	home := BrowserProfile{Name: "home", Dir: "/profiles/home", Allow: []string{"mail.example"}}
	tests := []struct {
		name    string
		where   string
		profile string
		want    string // the profile the window got
		asked   []string
		note    string
		err     string
	}{
		// mail.example isn't among work's sites: its sign-ins go only once allowed (BP-08a).
		{name: "the chat's, the workspace's own", where: PageEmbedded, want: "work", asked: []string{"open_page:profile:work:mail.example"}},
		{name: "the workspace's own, named", where: PageEmbedded, profile: "work", want: "work", asked: []string{"open_page:profile:work:mail.example"}},
		// It is among home's: only the profile is asked about.
		{name: "another, asked", where: PageEmbedded, profile: "home", want: "home", asked: []string{"browser:profile:home"}},
		{name: "none", where: PageEmbedded, profile: "none", want: ""},
		{name: "no such profile", where: PageEmbedded, profile: "play", err: "no browser profile \"play\""},
		{name: "the system's browser", where: PageSystem, profile: "work", want: "", note: "the browser profile \"work\" is only for the app's browser"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hooks, reqs := decisionHooks(api.DecisionOnce)
			r := &Registry{hooks: hooks}
			r.browser = &browserTool{r: r}
			r.SetBrowserProfiles(func() BrowserProfiles { return BrowserProfiles{Current: work, All: []BrowserProfile{work, home}} })
			r.page.open = tt.where
			var got []string
			r.SetPageOpener(func(_ context.Context, _, _, profile string) error { got = append(got, profile); return nil })
			args := map[string]any{"url": "mail.example"}
			if tt.profile != "" {
				args["profile"] = tt.profile
			}
			out := runTool(t, toolOf(t)(NewOpenPageTool(r)), args)
			if tt.err != "" {
				assert.Contains(t, errOf(out), tt.err)
				assert.Empty(t, got, "nothing opened")
				return
			}
			require.Empty(t, errOf(out))
			assert.Equal(t, []string{tt.want}, got)
			var asked []string
			for _, q := range *reqs {
				asked = append(asked, q.Key)
			}
			assert.Equal(t, tt.asked, asked)
			if tt.note != "" {
				assert.Contains(t, out["note"], tt.note)
			}
		})
	}
}

// The tests never ask DNS: names resolve from this table, others to a
// public address.
func init() {
	pageLookup = func(_ context.Context, host string) ([]netip.Addr, error) {
		fake := map[string][]string{
			"localhost":      {"127.0.0.1", "::1"},
			"router.lan":     {"192.168.1.1"},
			"rebind.example": {"93.184.216.34", "10.0.0.5"},
		}
		if host == "gone.example" {
			return nil, errors.New("no such host")
		}
		list, ok := fake[host]
		if !ok {
			list = []string{"93.184.216.34"}
		}
		var out []netip.Addr
		for _, a := range list {
			out = append(out, netip.MustParseAddr(a))
		}
		return out, nil
	}
}

// A page on this computer or its network is put to the person first,
// whatever the mode, as it opens with their own browser and sign-ins
// (AB-10a); a public one isn't.
func TestOpenPagePrivateHosts(t *testing.T) {
	for _, tc := range []struct {
		url   string
		asked bool
	}{
		{"https://example.com", false},
		{"http://localhost:8080/admin", true},
		{"http://127.0.0.1:3000", true},
		{"http://[::1]/", true},
		{"http://[::ffff:127.0.0.1]/", true},
		{"http://169.254.169.254/latest/meta-data", true},
		{"http://10.1.2.3", true},
		{"https://router.lan", true},
		{"https://rebind.example", true},
		{"https://gone.example", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			hooks, reqs := decisionHooks(api.DecisionDeny)
			r := &Registry{hooks: hooks}
			opened := 0
			r.SetPageOpener(func(context.Context, string, string, string) error { opened++; return nil })
			r.page.open = PageEmbedded
			out := runTool(t, toolOf(t)(NewOpenPageTool(r)), map[string]any{"url": tc.url})
			if !tc.asked {
				assert.Empty(t, *reqs)
				assert.Empty(t, errOf(out))
				assert.Equal(t, 1, opened)
				return
			}
			require.Len(t, *reqs, 1)
			assert.True(t, (*reqs)[0].MustAsk, "asked in every mode")
			assert.Contains(t, (*reqs)[0].Detail, "on this computer or its network")
			assert.Contains(t, errOf(out), "declined")
			assert.Zero(t, opened, "opened though declined")
		})
	}
}

// A page outside a profile's own sites isn't opened signed in with it
// when the person says no (BP-08a); the page's reach is the browser's:
// allow_local, the denied domains and the profile's own site names.
func TestOpenPageProfileSiteRefusedAndReach(t *testing.T) {
	work := BrowserProfile{Name: "work", Dir: "/profiles/work", Allow: []string{"*.corp.example", "jira.example", "10.1.2.3"}}
	hooks, reqs := decisionHooks(api.DecisionDeny)
	r := &Registry{hooks: hooks, web: WebFetchConfig{DenyDomains: []string{"evil.example"}}}
	r.browser = &browserTool{r: r}
	r.browser.cfg.AllowLocal = true
	r.SetBrowserProfiles(func() BrowserProfiles { return BrowserProfiles{Current: work, All: []BrowserProfile{work}} })
	r.page.open = PageEmbedded
	opened := 0
	r.SetPageOpener(func(context.Context, string, string, string) error { opened++; return nil })

	out := runTool(t, toolOf(t)(NewOpenPageTool(r)), map[string]any{"url": "https://attacker.example/csrf.html"})
	assert.NotEmpty(t, errOf(out))
	assert.Zero(t, opened, "not opened with the profile's sign-ins")
	require.Len(t, *reqs, 1)
	assert.Equal(t, "opening attacker.example signed in as work", (*reqs)[0].KeyLabel)

	out = runTool(t, toolOf(t)(NewOpenPageTool(r)), map[string]any{"url": "wiki.corp.example"})
	assert.Empty(t, errOf(out), "its own site opens without asking")
	assert.Equal(t, 1, opened)
	assert.Len(t, *reqs, 1)

	local, deny, sites := r.PageReach("work")
	assert.True(t, local)
	assert.Equal(t, []string{"evil.example"}, deny)
	assert.Equal(t, []string{"*.corp.example", "jira.example"}, sites, "names only")
	local, _, sites = (&Registry{}).PageReach("")
	assert.False(t, local)
	assert.Nil(t, sites)
	assert.NoError(t, r.askProfileSite(context.Background(), "https://x.example", ""), "no profile: nothing to ask")
	assert.NoError(t, r.askProfileSite(context.Background(), "https://x.example", "anonymous"))
}
