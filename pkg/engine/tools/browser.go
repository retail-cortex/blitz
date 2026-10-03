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
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/browser"
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
}

func newBrowserTool(r *Registry, cfg config.BrowserConfig, web WebFetchConfig, timeout time.Duration, dir string) *browserTool {
	return &browserTool{r: r, cfg: cfg, web: web, timeout: timeout, dir: dir, launch: browser.Launch}
}

// NewBrowserTool is browser: the agent drives a real browser (spec_parity_027
// PAR-TOOL-10..12).
func NewBrowserTool(bt *browserTool) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name: "browser",
			Description: "Drive a web browser (Chromium, its own profile): navigate to a page, click, type, select, read its text or HTML, take a screenshot (shown to you), read the console, run a script, and record a walkthrough. " +
				"Use it for pages that need JavaScript or interaction, and to check a web app the user is building; for a plain page, web_fetch is faster. New sites need the user's approval.",
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
	if action == "script" && !bt.cfg.AllowScripts {
		if err := bt.r.hooks.Approve(ctx, api.ApprovalRequest{
			Tool: "browser", Kind: api.ActionCommand, Detail: "run in the page: " + in.Script,
			Key: "browser:script", KeyLabel: "scripts in the browser",
		}); err != nil {
			return fail(err)
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
				out.Result, out.Truncated = out.Result[:webMaxOutputChars], true
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
	return u, approveWeb(ctx, bt.r.hooks, bt.web, "browser", "open "+u.String(), u)
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
	if host == current || matchDomain(bt.web.AllowDomains, host) {
		return nil
	}
	if ctx == nil {
		return fmt.Errorf("the page went to %s by itself; navigate there to ask", host)
	}
	return approveWeb(ctx, bt.r.hooks, bt.web, "browser", "open "+u.String()+" (the page went there)", u)
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
	b, err := bt.launch(ctx, browser.Options{
		Path: bt.cfg.Path, Visible: bt.cfg.Visible, Width: bt.cfg.Width, Height: bt.cfg.Height,
		AllowLocal: bt.cfg.AllowLocal, Timeout: bt.timeout,
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
