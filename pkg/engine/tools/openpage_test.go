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
				r.SetPageOpener(func(_ context.Context, u, where string) error {
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
				got, err := r.openPage(ctx, "https://github.com")
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
	r.SetPageOpener(func(context.Context, string, string) error { return nil })
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
			r.SetPageOpener(func(context.Context, string, string) error { return nil })
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
