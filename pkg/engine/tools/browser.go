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
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/browser"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/textutil"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// browserIdle closes a browser nobody has used for this long; tests
// shorten it.
var browserIdle = 15 * time.Minute

// BrowserInput is what the browser tool takes.
type BrowserInput struct {
	Action   string `json:"action" jsonschema:"navigate, click, type, select, read (the page's text), html, screenshot, console, script, back, record (save a screenshot after each step, as a walkthrough), stop_recording, or close"`
	URL      string `json:"url,omitempty" jsonschema:"navigate: the http(s) address"`
	Selector string `json:"selector,omitempty" jsonschema:"click, type, select, read, html: a CSS selector, or text=<visible text> for the element showing it"`
	Text     string `json:"text,omitempty" jsonschema:"type: the text to enter"`
	Append   bool   `json:"append,omitempty" jsonschema:"type: add to what the field holds instead of replacing it"`
	Submit   bool   `json:"submit,omitempty" jsonschema:"type: press Enter afterwards"`
	Value    string `json:"value,omitempty" jsonschema:"select: the option's value or text"`
	Script   string `json:"script,omitempty" jsonschema:"script: a JavaScript expression run in the page; its value (awaited) is returned"`
	FullPage bool   `json:"full_page,omitempty" jsonschema:"screenshot: the whole page, not just the viewport"`
	Profile  string `json:"profile,omitempty" jsonschema:"browse signed in with one of the user's browser profiles, by name (the user is asked first, unless it's this workspace's own); anonymous to browse signed out; empty: as before in this chat"`
}

// BrowserOutput is what it returns.
type BrowserOutput struct {
	URL       string                `json:"url,omitempty"`
	Title     string                `json:"title,omitempty"`
	Text      string                `json:"text,omitempty"`
	Result    string                `json:"result,omitempty"`
	Truncated bool                  `json:"truncated,omitempty"`
	Console   []browser.ConsoleLine `json:"console,omitempty"`
	ImageURI  string                `json:"image_uri,omitempty"`
	MIME      string                `json:"mime_type,omitempty"`
	Refused   []string              `json:"refused,omitempty"`
	Recording string                `json:"recording,omitempty"`
	Frames    []string              `json:"frames,omitempty"`
	Note      string                `json:"note,omitempty"`
	Error     string                `json:"error,omitempty"`
}

// browserTool is the browser tool's state: one browser for the workspace,
// started on first use and closed when idle, on close, or with the
// registry.
type browserTool struct {
	r       *Registry
	cfg     config.BrowserConfig
	web     WebFetchConfig
	timeout time.Duration
	// dir is where walkthroughs are saved.
	dir string

	mu   sync.Mutex // one action at a time
	b    *browser.Browser
	idle *time.Timer
	// idleGen is the current idle timer's: one that fired as an action
	// began (too late to stop) sees a newer one and leaves the browser.
	idleGen int
	rec     string // the walkthrough being recorded, or ""
	frame   int
	missed  int // the walkthrough's steps whose frame wasn't saved

	// launch starts a browser (browser.Launch; replaced in tests).
	launch func(context.Context, browser.Options) (*browser.Browser, error)

	actMu sync.Mutex
	act   context.Context // the running action's, to ask about navigations
	host  string          // the current page's

	// profile is the running browser's kept profile ("": a throwaway
	// one), profileName its name, and allow its sites browsed without
	// asking (BP-04); actMu guards them, for the page's navigations.
	profile     string
	profileName string
	allow       []string
	// shows are how each chat chose to see the agent browse in the app
	// ("watch" or "silent", AB-02; actMu guards it, forgotten when the
	// session is deleted), and inApp says the running browser is the
	// app's view, shown so.
	shows map[string]string
	inApp string
	// chosen are the profiles each chat asked to browse with (BP-03b; ""
	// for none), noted the notes already given each chat (BP-03c); actMu
	// guards them.
	chosen map[string]string
	noted  map[string]bool
}

func newBrowserTool(r *Registry, cfg config.BrowserConfig, web WebFetchConfig, timeout time.Duration, dir string) *browserTool {
	return &browserTool{r: r, cfg: cfg, web: web, timeout: timeout, dir: dir, launch: browser.Launch}
}

// NewBrowserTool is browser: the agent drives a real browser (spec_027_parity
// PAR-TOOL-10..12).
func NewBrowserTool(bt *browserTool) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name: "browser",
			Description: "Drive a web browser: navigate to a page, click, type, select, read its text or HTML, take a screenshot (shown to you), read the console, run a script, and record a walkthrough. " +
				"It can browse signed in to the user's sites with one of their browser profiles (profile: its name; a navigate's note lists them): use it for anything behind a sign-in, for pages that need JavaScript or interaction, and to check a web app the user is building; for a plain public page, web_fetch is faster. New sites need the user's approval.",
		},
		func(ctx agent.Context, in BrowserInput) (BrowserOutput, error) {
			return bt.run(ctx, in), nil
		})
}

func (bt *browserTool) run(ctx context.Context, in BrowserInput) (out BrowserOutput) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	fail := func(err error) BrowserOutput { out.Error = err.Error(); return out }
	if !bt.web.AllowNetwork {
		return fail(errors.New("network access is disabled (sandbox.allow_network = false)"))
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if !browserActions[action] {
		return fail(fmt.Errorf("unknown action %q", in.Action))
	}
	if action == "close" {
		bt.closeLocked()
		out.Note = "The browser is closed."
		return out
	}
	var site string // the page asked for, whose site may have a profile (BP-03d)
	if action == "navigate" {
		if u, err := url.Parse(strings.TrimSpace(in.URL)); err == nil {
			site = strings.ToLower(u.Hostname())
		}
	}
	p, all, err := bt.syncProfile(ctx, in.Profile, site)
	if err != nil {
		return fail(err)
	}
	if action == "navigate" {
		// Told once the browser is up, so the note says where it browses.
		defer func() {
			if note := bt.profileNote(sessionOf(ctx), p, all); note != "" {
				out.Note = strings.TrimSpace(out.Note + " " + note)
			}
		}()
	}
	if action == "stop_recording" {
		if bt.rec == "" {
			return fail(errors.New("not recording"))
		}
		out.Recording, out.Frames = bt.rec, bt.frames()
		if bt.missed > 0 {
			out.Note = fmt.Sprintf("The walkthrough is incomplete: screenshots of steps weren't saved (%d missing).", bt.missed)
		}
		bt.rec = ""
		return out
	}

	// Navigation is decided before the browser starts, so a refusal
	// doesn't launch one.
	var target *url.URL
	if action == "navigate" {
		u, err := bt.checkNavigate(ctx, in.URL)
		if err != nil {
			return fail(err)
		}
		target = u
	}
	if action == "script" {
		// The page's host is what web rules see: deny refuses its scripts
		// even with browser.allow_scripts, and ask puts them to the person.
		bt.actMu.Lock()
		host := bt.host
		bt.actMu.Unlock()
		var effect Effect
		if host != "" {
			var rule *PermissionRule
			if effect, rule = bt.web.Rules.Decide(RuleWeb, []string{host}); effect == EffectDeny {
				return fail(fmt.Errorf("scripts on %s are denied by the permission rule deny %s", host, rule))
			}
		}
		if !bt.cfg.AllowScripts || effect == EffectAsk {
			if err := bt.r.hooks.Approve(ctx, api.ApprovalRequest{
				Tool: "browser", Kind: api.ActionCommand, Detail: "run in the page: " + in.Script,
				Key: "browser:script", KeyLabel: "scripts in the browser", MustAsk: effect == EffectAsk,
			}); err != nil {
				return fail(err)
			}
		}
	}

	b, err := bt.browser(ctx)
	if err != nil {
		return fail(err)
	}
	bt.setAct(ctx)
	defer bt.clearAct()
	actx, cancel := context.WithTimeout(ctx, bt.timeout+30*time.Second)
	defer cancel()

	var page browser.Page
	switch action {
	case "navigate":
		page, err = b.Navigate(actx, target.String())
	case "back":
		page, err = b.Back(actx)
	case "click":
		if err = need(in.Selector, "selector"); err == nil {
			err = b.Click(actx, in.Selector)
		}
	case "type":
		if err = need(in.Selector, "selector"); err == nil {
			err = b.Type(actx, in.Selector, in.Text, in.Append, in.Submit)
		}
	case "select":
		if err = need(in.Selector, "selector"); err == nil {
			err = b.Select(actx, in.Selector, in.Value)
		}
	case "read":
		out.Text, out.Truncated, err = b.Text(actx, in.Selector, webMaxOutputChars)
	case "html":
		out.Text, out.Truncated, err = b.HTML(actx, in.Selector, webMaxOutputChars)
	case "script":
		if err = need(in.Script, "script"); err == nil {
			out.Result, err = b.Eval(actx, in.Script)
			if len(out.Result) > webMaxOutputChars {
				out.Result, out.Truncated = textutil.TruncateUTF8(out.Result, webMaxOutputChars), true
			}
		}
	case "screenshot":
		err = bt.screenshot(actx, b, in.FullPage, &out)
	case "console":
	case "record":
		if bt.rec != "" {
			out.Recording, out.Note = bt.rec, "Already recording."
			break
		}
		name := time.Now().Format("20060102-150405")
		bt.rec, bt.frame, bt.missed = filepath.Join(bt.dir, name), 0, 0
		if err = os.MkdirAll(bt.rec, 0o700); err != nil {
			bt.rec = ""
			break
		}
		out.Recording, out.Note = bt.rec, "Recording: a screenshot is saved after each step until stop_recording."
	}
	if err != nil {
		out.Error = err.Error()
	}
	if page.URL == "" {
		page, _ = b.Page(actx)
	}
	out.URL, out.Title = page.URL, page.Title
	if u, err := url.Parse(page.URL); err == nil {
		bt.actMu.Lock()
		bt.host = strings.ToLower(u.Hostname())
		bt.actMu.Unlock()
	}
	out.Console = b.Console()
	out.Refused = b.Refused()
	if len(out.Refused) > 0 {
		out.Note = strings.TrimSpace(out.Note + " Some requests were refused by the web rules; navigate to a site explicitly to ask for it.")
	}
	if bt.rec != "" && changes(action) && out.Error == "" {
		if err := bt.saveFrame(actx, b, action); err != nil {
			bt.missed++
			out.Note = strings.TrimSpace(out.Note + " This step's walkthrough screenshot wasn't saved: " + err.Error())
		}
	}
	return out
}

// pageText opens u in the browser and reads its text: web_fetch's
// fallback (WEB-09), under the browser's own checks and web rules.
func (bt *browserTool) pageText(ctx context.Context, u string) (BrowserOutput, error) {
	if out := bt.run(ctx, BrowserInput{Action: "navigate", URL: u}); out.Error != "" {
		return out, errors.New(out.Error)
	}
	out := bt.run(ctx, BrowserInput{Action: "read"})
	if out.Error != "" {
		return out, errors.New(out.Error)
	}
	return out, nil
}

var browserActions = map[string]bool{
	"navigate": true, "click": true, "type": true, "select": true, "read": true, "html": true, "screenshot": true,
	"console": true, "script": true, "back": true, "record": true, "stop_recording": true, "close": true,
}

// changes reports whether action may change the page (a walkthrough step).
func changes(action string) bool {
	switch action {
	case "navigate", "back", "click", "type", "select", "script":
		return true
	}
	return false
}

func need(v, name string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

// checkNavigate decides a navigation the agent asks for.
func (bt *browserTool) checkNavigate(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || raw == "" {
		return nil, fmt.Errorf("invalid URL %q", raw)
	}
	if err := bt.checkURL(u); err != nil {
		return nil, err
	}
	return u, approveWeb(ctx, bt.r.hooks, bt.webRules(), "browser", "open "+u.String(), u)
}

// checkURL is what any page must pass: http(s), no credentials, not a
// denied domain, and not a local address unless allowed.
func (bt *browserTool) checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http and https pages, not %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return errors.New("the URL has no host")
	}
	if u.User != nil {
		return errors.New("URLs with credentials are not allowed")
	}
	host := strings.ToLower(u.Hostname())
	if matchDomain(bt.web.DenyDomains, host) {
		return fmt.Errorf("%s is blocked by web.deny_domains", host)
	}
	if effect, rule := bt.web.Rules.Decide(RuleWeb, []string{host}); effect == EffectDeny {
		return fmt.Errorf("%s is denied by the permission rule deny %s", host, rule)
	}
	if a, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil && (a.IsLinkLocalUnicast() || a.IsUnspecified() || a.IsMulticast()) {
		return fmt.Errorf("%s is never reachable from the browser", host)
	}
	if !bt.cfg.AllowLocal && localHost(host) {
		return fmt.Errorf("%s is local: set browser.allow_local = true to test your own app", host)
	}
	return nil
}

// localHost reports whether host is localhost or a literal non-public
// address.
func localHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	a, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil && !browser.PublicAddr(a)
}

// navigation decides a page load the page started (a link, a redirect, a
// form): the current site's pages go ahead; another site's is asked about
// while an action runs, and refused otherwise.
func (bt *browserTool) navigation(u *url.URL) error {
	if err := bt.checkURL(u); err != nil {
		return err
	}
	host := strings.ToLower(u.Hostname())
	bt.actMu.Lock()
	ctx, current := bt.act, bt.host
	bt.actMu.Unlock()
	if host == current || matchDomain(bt.webRules().AllowDomains, host) {
		return nil
	}
	if ctx == nil {
		return fmt.Errorf("the page went to %s by itself; navigate there to ask", host)
	}
	return approveWeb(ctx, bt.r.hooks, bt.webRules(), "browser", "open "+u.String()+" (the page went there)", u)
}

func (bt *browserTool) setAct(ctx context.Context) {
	bt.actMu.Lock()
	defer bt.actMu.Unlock()
	bt.act = ctx
}

func (bt *browserTool) clearAct() {
	bt.actMu.Lock()
	defer bt.actMu.Unlock()
	bt.act = nil
}

// webRules are the web rules the browser asks by: with a profile, its own
// sites in place of web.allow_domains, since its sign-ins go with every
// request (BP-04).
func (bt *browserTool) webRules() WebFetchConfig {
	w := bt.web
	bt.actMu.Lock()
	defer bt.actMu.Unlock()
	if bt.profile != "" {
		w.AllowDomains = bt.allow
	}
	return w
}

// syncProfile takes the browser profile this chat browses with, before an
// action is judged (BP-03): the one it asked for (name, BP-03b; "none"
// for none), asked about first unless it's the workspace's own, else the
// one it asked for before, else the workspace's; with none of those, the
// one profile whose sites include site, offered (BP-03d). A change of
// profile closes the browser, so the next one starts with it. It returns
// the profile and all of them, for the agent's note (BP-03c). bt.mu must
// be held.
func (bt *browserTool) syncProfile(ctx context.Context, name, site string) (BrowserProfile, BrowserProfiles, error) {
	all := bt.r.browserProfiles()
	session := sessionOf(ctx)
	if name = strings.TrimSpace(name); name != "" {
		if err := bt.choose(ctx, session, name, all); err != nil {
			return BrowserProfile{}, all, err
		}
	} else if err := bt.offerForSite(ctx, session, site, all); err != nil {
		return BrowserProfile{}, all, err
	}
	p := bt.effective(session, all)
	bt.actMu.Lock()
	changed := bt.profile != p.Dir
	bt.profile, bt.profileName, bt.allow = p.Dir, p.Name, p.Allow
	bt.actMu.Unlock()
	if changed && bt.b != nil {
		bt.b.Close()
		bt.b = nil
	}
	return p, all, nil
}

// choose makes the chat browse with profile name (anonymous, or none:
// signed out): the user is asked first unless it's the workspace's own,
// or signed out.
func (bt *browserTool) choose(ctx context.Context, session, name string, all BrowserProfiles) error {
	if strings.EqualFold(name, "none") || strings.EqualFold(name, config.AnonymousBrowserProfile) {
		bt.setChosen(session, "")
		return nil
	}
	i := slices.IndexFunc(all.All, func(p BrowserProfile) bool { return p.Name == name })
	if i < 0 {
		if len(all.All) == 0 {
			return fmt.Errorf("no browser profile %q: the user has none (they make one in Settings › Browser)", name)
		}
		return fmt.Errorf("no browser profile %q; the user's profiles: %s", name, profileList(all.All))
	}
	if name != all.Current.Name {
		p := all.All[i]
		detail := fmt.Sprintf("browse signed in with your browser profile %q", name)
		if len(p.Allow) > 0 {
			detail += " (its sites: " + strings.Join(p.Allow, ", ") + ")"
		}
		if err := bt.r.hooks.Approve(ctx, api.ApprovalRequest{
			Tool: "browser", Kind: api.ActionNetwork, Detail: detail,
			Key: "browser:profile:" + name, KeyLabel: "browsing with your browser profile " + name, Targets: p.Allow,
		}); err != nil {
			return err
		}
	}
	bt.setChosen(session, name)
	return nil
}

// forSite is the one profile of the person's whose sites include host,
// for a chat that browses with none it chose while the workspace has none
// of its own (BP-03d); "" when no profile, or more than one, has it.
func (bt *browserTool) forSite(session, host string, all BrowserProfiles) string {
	if host == "" || all.Current.Name != "" || all.Anonymous {
		return ""
	}
	bt.actMu.Lock()
	_, chose := bt.chosen[session]
	bt.actMu.Unlock()
	if chose {
		return ""
	}
	name := ""
	for _, p := range all.All {
		if matchDomain(p.Allow, host) {
			if name != "" {
				return ""
			}
			name = p.Name
		}
	}
	return name
}

// offerForSite asks to browse with the profile whose sites include host
// (forSite), as choose does; refused, the chat browses signed out and
// isn't asked again (BP-03d).
func (bt *browserTool) offerForSite(ctx context.Context, session, host string, all BrowserProfiles) error {
	name := bt.forSite(session, host, all)
	if name == "" {
		return nil
	}
	err := bt.choose(ctx, session, name, all)
	if errors.Is(err, ErrNotApproved) {
		bt.setChosen(session, "")
		return nil
	}
	return err
}

func (bt *browserTool) setChosen(session, name string) {
	bt.actMu.Lock()
	defer bt.actMu.Unlock()
	if bt.chosen == nil {
		bt.chosen = map[string]string{}
	}
	bt.chosen[session] = name
}

// effective is the profile session browses with: the one it chose (if
// it still exists), else the workspace's.
func (bt *browserTool) effective(session string, all BrowserProfiles) BrowserProfile {
	bt.actMu.Lock()
	name, ok := bt.chosen[session]
	bt.actMu.Unlock()
	if !ok {
		return all.Current
	}
	if i := slices.IndexFunc(all.All, func(p BrowserProfile) bool { return p.Name == name }); name != "" && i >= 0 {
		return all.All[i]
	}
	return BrowserProfile{}
}

// profileNote is what the agent is told once a chat about its profile
// (BP-03c): browsing signed out while the user has profiles, which ones;
// the workspace's withheld, why; in Chrome's copy of one, that the app's
// sign-ins aren't in it. bt.mu must be held.
func (bt *browserTool) profileNote(session string, p BrowserProfile, all BrowserProfiles) string {
	var key, note string
	bt.actMu.Lock()
	_, chose := bt.chosen[session]
	inApp := bt.inApp != ""
	bt.actMu.Unlock()
	switch {
	case p.Name == "" && all.Withheld != "" && !chose:
		key = "withheld"
		note = fmt.Sprintf("Browsing signed out: this workspace's browser profile %q isn't used until the user trusts the project's settings.", all.Withheld)
		if len(all.All) > 0 {
			note += " Pass profile to browse with one anyway (the user is asked); the user's profiles: " + profileList(all.All) + "."
		}
	case p.Name == "" && len(all.All) > 0 && !chose:
		key = "signed-out"
		note = "Browsing signed out. To browse signed in to the user's sites, pass profile with one of their browser profiles (the user is asked first): " + profileList(all.All) + "."
	case p.Name != "" && bt.b != nil && !inApp:
		key = "chrome:" + p.Name
		note = fmt.Sprintf("Browsing with Chrome's copy of the browser profile %q: sites the user signed in to in the app (Settings › Browser › Sign in…) aren't signed in here; Sign in with Chrome… signs in to this copy.", p.Name)
	default:
		return ""
	}
	bt.actMu.Lock()
	defer bt.actMu.Unlock()
	if bt.noted == nil {
		bt.noted = map[string]bool{}
	}
	if bt.noted[session+"\x00"+key] {
		return ""
	}
	bt.noted[session+"\x00"+key] = true
	return note
}

// profileList is the profiles by name, each with its sites.
func profileList(ps []BrowserProfile) string {
	parts := make([]string, 0, len(ps))
	for _, p := range ps {
		if len(p.Allow) > 0 {
			parts = append(parts, p.Name+" ("+strings.Join(p.Allow, ", ")+")")
		} else {
			parts = append(parts, p.Name)
		}
	}
	return strings.Join(parts, ", ")
}

// signedInAs is the profile session browses with when its sites include
// host, else the one profile whose sites do that it would be offered
// (BP-03d); "": none, for web_fetch (BP-07).
func (bt *browserTool) signedInAs(ctx context.Context, host string) string {
	session, all := sessionOf(ctx), bt.r.browserProfiles()
	p := bt.effective(session, all)
	if p.Name != "" && matchDomain(p.Allow, host) {
		return p.Name
	}
	if p.Name == "" {
		return bt.forSite(session, host, all)
	}
	return ""
}

// show is how the chat sees the agent browse in the app (AB-02):
// browser.show, or for "ask" the chat's answer, asked the first time
// (silent where no one can answer, as a job's run).
func (bt *browserTool) show(ctx context.Context) string {
	switch bt.cfg.Show {
	case "watch", "silent":
		return bt.cfg.Show
	}
	session := sessionOf(ctx)
	bt.actMu.Lock()
	v, ok := bt.shows[session]
	bt.actMu.Unlock()
	if ok {
		return v
	}
	v = "silent"
	options := []string{i18n.T("tools.browser.watch"), i18n.T("tools.browser.silent")}
	if answer, err := bt.r.hooks.askUser(ctx, i18n.T("tools.browser.show_ask"), options); err == nil && answer == options[0] {
		v = "watch"
	}
	bt.actMu.Lock()
	defer bt.actMu.Unlock()
	if bt.shows == nil {
		bt.shows = map[string]string{}
	}
	bt.shows[session] = v
	return v
}

// browser is the running browser, started when needed.
func (bt *browserTool) browser(ctx context.Context) (*browser.Browser, error) {
	if bt.idle != nil {
		bt.idle.Stop()
	}
	bt.idleGen++
	gen := bt.idleGen
	bt.idle = time.AfterFunc(browserIdle, func() {
		bt.mu.Lock()
		defer bt.mu.Unlock()
		if bt.idleGen == gen {
			bt.closeLocked()
		}
	})
	if bt.b != nil {
		return bt.b, nil
	}
	bt.actMu.Lock()
	profile, profileName := bt.profile, bt.profileName
	// The profile's own sites may be on the user's private network (an
	// intranet over VPN or split-horizon DNS): its sign-ins are for them
	// (BP-04a). A change of profile closes the browser, so these last.
	var sites []string
	if profile != "" {
		sites = slices.Clone(bt.allow)
	}
	bt.actMu.Unlock()
	privateHost := profileSiteHost(sites)
	// The desktop app's own view when a window is watching (AB-01),
	// watched or silent as the chat chose; else Chrome, headless.
	if host := bt.r.agentBrowserHost(); host != nil {
		show := bt.show(ctx)
		if t, err := host(ctx, AgentBrowserOptions{Show: show, Profile: profileName, AllowLocal: bt.cfg.AllowLocal, DenyDomains: bt.web.DenyDomains, PrivateSites: profileSiteNames(sites)}); err == nil {
			b, err := browser.AttachTransport(ctx, t, browser.Options{Width: bt.cfg.Width, Height: bt.cfg.Height, Timeout: bt.timeout, KeepViewport: show == "watch", Navigation: bt.navigation})
			if err != nil {
				return nil, err
			}
			bt.b, bt.inApp = b, show
			return b, nil
		}
	}
	bt.inApp = ""
	b, err := bt.launch(ctx, browser.Options{
		Path: bt.cfg.Path, Visible: bt.cfg.Visible, Width: bt.cfg.Width, Height: bt.cfg.Height,
		AllowLocal: bt.cfg.AllowLocal, PrivateHost: privateHost, Timeout: bt.timeout, ProfileDir: profile,
		Host: func(host string) error {
			if matchDomain(bt.web.DenyDomains, host) {
				return fmt.Errorf("blocked by web.deny_domains")
			}
			if effect, rule := bt.web.Rules.Decide(RuleWeb, []string{host}); effect == EffectDeny {
				return fmt.Errorf("denied by the permission rule deny %s", rule)
			}
			return nil
		},
		Navigation: bt.navigation,
	})
	if err != nil {
		return nil, err
	}
	bt.b = b
	return b, nil
}

func (bt *browserTool) screenshot(ctx context.Context, b *browser.Browser, full bool, out *BrowserOutput) error {
	data, err := b.Screenshot(ctx, full)
	if err != nil {
		return err
	}
	img, err := bt.r.AddImage("screenshot.png", data)
	if err != nil {
		return fmt.Errorf("the screenshot can't be shown: %w", err)
	}
	out.ImageURI, out.MIME = img.URI(), img.MIME
	out.Note = strings.TrimSpace(out.Note + " The screenshot follows this result.")
	return nil
}

var frameName = regexp.MustCompile(`[^a-z]+`)

func (bt *browserTool) saveFrame(ctx context.Context, b *browser.Browser, action string) error {
	data, err := b.Screenshot(ctx, false)
	if err != nil {
		return err
	}
	bt.frame++
	name := fmt.Sprintf("%03d-%s.png", bt.frame, frameName.ReplaceAllString(action, "-"))
	return os.WriteFile(filepath.Join(bt.rec, name), data, 0o600)
}

func (bt *browserTool) frames() []string {
	entries, _ := os.ReadDir(bt.rec)
	var out []string
	for _, e := range entries {
		out = append(out, filepath.Join(bt.rec, e.Name()))
	}
	return out
}

func (bt *browserTool) closeLocked() {
	if bt.idle != nil {
		bt.idle.Stop()
		bt.idle = nil
	}
	if bt.b != nil {
		bt.b.Close()
		bt.b = nil
	}
	bt.rec = ""
	bt.actMu.Lock()
	bt.host = ""
	bt.actMu.Unlock()
}

// close stops the browser, with the registry.
func (bt *browserTool) close() {
	if bt == nil {
		return
	}
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.closeLocked()
}

// profileSiteHost reports whether a host is one of a profile's own sites,
// which may be on the private network (BP-04a): a name, never a literal
// address, and only by a site that names a domain (security review
// electron#1: a site of "*" or "10.*" opened the whole private network to
// every page).
func profileSiteHost(sites []string) func(string) bool {
	named := profileSiteNames(sites)
	return func(host string) bool {
		if _, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
			return false
		}
		return matchDomain(named, host)
	}
}

// profileSiteNames are the sites that name a domain: not a literal
// address, not a pattern matching any host.
func profileSiteNames(sites []string) []string {
	var named []string
	for _, s := range sites {
		bare := strings.TrimPrefix(strings.TrimSpace(s), "*.")
		if strings.ContainsAny(bare, "*?[") || !strings.Contains(bare, ".") {
			continue
		}
		if _, err := netip.ParseAddr(strings.Trim(bare, "[]")); err == nil {
			continue
		}
		named = append(named, s)
	}
	return named
}
