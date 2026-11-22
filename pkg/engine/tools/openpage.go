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
	"net"
	"net/netip"
	"net/url"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/browser"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// Where open_page shows a page (spec_050_agent_browser AB-10).
const (
	// PageEmbedded is a web tab of the desktop app's.
	PageEmbedded = "embedded"
	// PageSystem is the system's default browser.
	PageSystem = "system"
)

// PageOpener has the desktop window open url for the user, in a web tab
// (PageEmbedded) or the system's browser (PageSystem), a web tab signed
// in with browser profile ("": the app's own pages' session, BP-08); an
// error when no window is watching the workspace.
type PageOpener func(ctx context.Context, url, where, profile string) error

// openSystemBrowser opens u in the system's browser on this machine
// (replaced in tests).
var openSystemBrowser = func(u string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	}
	return exec.Command("xdg-open", u).Start()
}

// pageTool is open_page's state: browser.open, and each chat's answer to
// the question.
type pageTool struct {
	open string // browser.open: ask, embedded or system
	mu   sync.Mutex
	// opener is the desktop window's (nil: none); where is each chat's
	// answer.
	opener PageOpener
	where  map[string]string
}

// SetPageOpener supplies the desktop window's web tabs (AB-10).
func (r *Registry) SetPageOpener(o PageOpener) {
	r.page.mu.Lock()
	defer r.page.mu.Unlock()
	r.page.opener = o
}

// OpenPageInput is what the open_page tool takes.
type OpenPageInput struct {
	URL     string `json:"url" jsonschema:"The http(s) address to show the user (a bare host gets https://)"`
	Profile string `json:"profile,omitempty" jsonschema:"show it signed in with one of the user's browser profiles, by name (the user is asked first, unless it's this workspace's own); anonymous for signed out; empty: the profile this chat browses with"`
}

// OpenPageOutput is what it returns.
type OpenPageOutput struct {
	URL     string `json:"url,omitempty"`
	Where   string `json:"where,omitempty"`   // embedded or system
	Profile string `json:"profile,omitempty"` // the browser profile it's signed in with (embedded only)
	Note    string `json:"note,omitempty"`
	Error   string `json:"error,omitempty"`
}

// NewOpenPageTool is open_page: a page opened for the user to look at
// (AB-10), in the app's embedded browser or the system's.
func NewOpenPageTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{Name: "open_page", Description: "Open a web page for the user to look at, when they ask to open, show or go to a site: in Blitz's embedded browser (a tab in the app) or their system browser, as they choose. Use this, never the shell's open, start or xdg-open. In the app's browser it can be signed in with one of the user's browser profiles (profile: its name). It doesn't read the page for you: for that, web_fetch or browser."},
		func(ctx agent.Context, in OpenPageInput) (OpenPageOutput, error) {
			u, err := r.pageURL(in.URL)
			if err != nil {
				return OpenPageOutput{Error: err.Error()}, nil
			}
			if err := r.askRulePage(ctx, u); err != nil {
				return OpenPageOutput{URL: u, Error: err.Error()}, nil
			}
			profile, err := r.pageProfile(ctx, in.Profile, u)
			if err != nil {
				return OpenPageOutput{URL: u, Error: err.Error()}, nil
			}
			where, err := r.openPage(ctx, u, profile)
			if err != nil {
				return OpenPageOutput{URL: u, Error: err.Error()}, nil
			}
			out := OpenPageOutput{URL: u, Where: where}
			switch {
			case where == PageEmbedded:
				out.Profile = profile
			case profile != "":
				out.Note = fmt.Sprintf("Opened in the system's browser, which has its own sign-ins: the browser profile %q is only for the app's browser (browser.open).", profile)
			}
			return out, nil
		})
}

// pageURL is raw as an address open_page opens, or why not: http or
// https only, and never a host web.deny_domains or a deny rule refuses.
func (r *Registry) pageURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("url is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err == nil && u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("only http and https pages open, not %s", u.Scheme)
	}
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("not a web address: %s", raw)
	}
	host := u.Hostname()
	if matchDomain(r.web.DenyDomains, host) {
		return "", fmt.Errorf("%s is blocked by web.deny_domains", host)
	}
	if r.rules != nil {
		if effect, rule := r.rules.Decide(RuleWeb, []string{host}); effect == EffectDeny {
			return "", fmt.Errorf("%s is denied by the permission rule deny %s", host, rule)
		}
	}
	return u.String(), nil
}

// askRulePage puts u to the person when an ask web(…) rule names its
// host, or when the host is (or resolves to) a private, loopback or
// link-local address (AB-10a): a public page needs no approval (AB-10),
// but a page on this computer or its network opens with the person's own
// browser and sign-ins, where a page the agent read could send it.
func (r *Registry) askRulePage(ctx context.Context, u string) error {
	parsed, err := url.Parse(u)
	if err != nil {
		return err
	}
	host := strings.ToLower(parsed.Hostname())
	effect, _ := r.rules.Decide(RuleWeb, []string{host})
	private := privateHost(ctx, host)
	if effect != EffectAsk && !private {
		return nil
	}
	detail := "open " + u + " in a browser"
	if private {
		detail += " (an address on this computer or its network)"
	}
	return r.hooks.Approve(ctx, api.ApprovalRequest{
		Tool: "open_page", Kind: api.ActionNetwork, Detail: detail,
		Targets: []string{host}, MustAsk: true,
	})
}

// pageLookup resolves a page's host (a variable for tests).
var pageLookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// privateHost reports whether host is an address that isn't public
// (browser.PublicAddr), or a name any of whose addresses isn't: a name
// that doesn't resolve isn't one (the page won't load either).
func privateHost(ctx context.Context, host string) bool {
	if a, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return !browser.PublicAddr(a.Unmap())
	}
	lctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := pageLookup(lctx, host)
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if !browser.PublicAddr(a.Unmap()) {
			return true
		}
	}
	return false
}

// pageProfile is the browser profile open_page shows a page signed in
// with (BP-08): the one named (asked first unless it's the workspace's
// own, as the browser's, BP-03b; "none": none), else the one this chat
// browses with, else the one whose sites include the page's, offered
// (BP-03d). "" without the browser tool.
func (r *Registry) pageProfile(ctx context.Context, name, page string) (string, error) {
	bt := r.browser
	if bt == nil {
		if n := strings.TrimSpace(name); n != "" && !strings.EqualFold(n, "none") && !strings.EqualFold(n, config.AnonymousBrowserProfile) {
			return "", errors.New("browser profiles need the browser ([browser] enabled)")
		}
		return "", nil
	}
	all := r.browserProfiles()
	session := sessionOf(ctx)
	if name = strings.TrimSpace(name); name != "" {
		if err := bt.choose(ctx, session, name, all); err != nil {
			return "", err
		}
	} else if u, err := url.Parse(page); err == nil {
		if err := bt.offerForSite(ctx, session, strings.ToLower(u.Hostname()), all); err != nil {
			return "", err
		}
	}
	return bt.effective(session, all).Name, nil
}

// SessionGone forgets what session's chat answered about pages and the
// browser, once the session is deleted.
func (r *Registry) SessionGone(session string) {
	r.page.mu.Lock()
	delete(r.page.where, session)
	r.page.mu.Unlock()
	if r.browser != nil {
		r.browser.actMu.Lock()
		delete(r.browser.shows, session)
		delete(r.browser.chosen, session)
		for k := range r.browser.noted {
			if strings.HasPrefix(k, session+"\x00") {
				delete(r.browser.noted, k)
			}
		}
		r.browser.actMu.Unlock()
	}
}

// openPage shows u where browser.open says, or for "ask" where the chat
// answers (asked once; the embedded browser if no one answers). No
// desktop window: the system's browser on this machine. A run no one
// watches (a job's) opens nothing.
func (r *Registry) openPage(ctx context.Context, u, profile string) (string, error) {
	if isUnattended(ctx) {
		return "", errors.New("nobody is watching this run to see the page")
	}
	p := &r.page
	p.mu.Lock()
	opener := p.opener
	p.mu.Unlock()
	if opener != nil {
		where := r.pageWhere(ctx, u)
		if where != PageEmbedded {
			profile = ""
		}
		if err := r.askProfileSite(ctx, u, profile); err != nil {
			return "", err
		}
		err := opener(ctx, u, where, profile)
		switch {
		case err == nil:
			return where, nil
		case p.open == PageEmbedded:
			// Asked for the app's web tab: never a browser on the
			// service's machine instead.
			return "", fmt.Errorf("couldn't open the page in the app (browser.open = \"embedded\"): %w", err)
		}
	}
	if err := openSystemBrowser(u); err != nil {
		return "", fmt.Errorf("couldn't open the system browser: %w", err)
	}
	return PageSystem, nil
}

// pageWhere is where the chat sees pages: browser.open, or for "ask" the
// chat's answer.
func (r *Registry) pageWhere(ctx context.Context, u string) string {
	p := &r.page
	if p.open == PageEmbedded || p.open == PageSystem {
		return p.open
	}
	session := sessionOf(ctx)
	p.mu.Lock()
	v, ok := p.where[session]
	p.mu.Unlock()
	if ok {
		return v
	}
	host := u
	if parsed, err := url.Parse(u); err == nil {
		host = parsed.Hostname()
	}
	v = PageEmbedded
	options := []string{i18n.T("tools.open_page.embedded"), i18n.T("tools.open_page.system")}
	if answer, err := r.hooks.askUser(ctx, i18n.T("tools.open_page.ask", "host", host), options); err == nil && answer == options[1] {
		v = PageSystem
	}
	p.mu.Lock()
	if p.where == nil {
		p.where = map[string]string{}
	}
	p.where[session] = v
	p.mu.Unlock()
	return v
}

// askProfileSite asks before a page outside profile's own sites opens
// signed in with it (BP-08a): its sign-ins go with the page's requests,
// as the browser asks for such a site (BP-04). Allowed for the chat, a
// site isn't asked about again. Its own sites, and no profile, open
// without asking.
func (r *Registry) askProfileSite(ctx context.Context, u, profile string) error {
	if profile == "" || profile == config.AnonymousBrowserProfile {
		return nil
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return err
	}
	host := strings.ToLower(parsed.Hostname())
	for _, p := range r.browserProfiles().All {
		if p.Name == profile && matchDomain(p.Allow, host) {
			return nil
		}
	}
	return r.hooks.Approve(ctx, api.ApprovalRequest{
		Tool: "open_page", Kind: api.ActionNetwork,
		Detail:  fmt.Sprintf("open %s signed in with your browser profile %s (a site outside its own)", u, profile),
		Targets: []string{host},
		Key:     "open_page:profile:" + profile + ":" + host, KeyLabel: fmt.Sprintf("opening %s signed in as %s", host, profile),
	})
}

// PageReach is what a page open_page shows may reach (BP-08a), held by
// the window's network filter as the agent's browser is (EL-17):
// private addresses only with browser.allow_local, never
// web.deny_domains, and profile's own sites (names) on the private
// network.
func (r *Registry) PageReach(profile string) (allowLocal bool, deny, sites []string) {
	if r.browser != nil {
		allowLocal = r.browser.cfg.AllowLocal
	}
	deny = slices.Clone(r.web.DenyDomains)
	if profile == "" {
		return allowLocal, deny, nil
	}
	for _, p := range r.browserProfiles().All {
		if p.Name == profile {
			sites = profileSiteNames(p.Allow)
		}
	}
	return allowLocal, deny, sites
}
