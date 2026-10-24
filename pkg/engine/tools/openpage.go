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
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// Where open_page shows a page (spec_agent_browser_050 AB-10).
const (
	// PageEmbedded is a web tab of the desktop app's.
	PageEmbedded = "embedded"
	// PageSystem is the system's default browser.
	PageSystem = "system"
)

// PageOpener has the desktop window open url for the user, in a web tab
// (PageEmbedded) or the system's browser (PageSystem); an error when no
// window is watching the workspace.
type PageOpener func(ctx context.Context, url, where string) error

// openAsk is the question browser.open "ask" puts, and its answers.
const (
	openAsk      = "Open %s in Blitz's embedded browser or your system browser?"
	openEmbedded = "Embedded browser"
	openSystem   = "System browser"
)

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
	URL string `json:"url" jsonschema:"The http(s) address to show the user (a bare host gets https://)"`
}

// OpenPageOutput is what it returns.
type OpenPageOutput struct {
	URL   string `json:"url,omitempty"`
	Where string `json:"where,omitempty"` // embedded or system
	Error string `json:"error,omitempty"`
}

// NewOpenPageTool is open_page: a page opened for the user to look at
// (AB-10), in the app's embedded browser or the system's.
func NewOpenPageTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{Name: "open_page", Description: "Open a web page for the user to look at, when they ask to open, show or go to a site: in Blitz's embedded browser (a tab in the app) or their system browser, as they choose. Use this, never the shell's open, start or xdg-open. It doesn't read the page for you: for that, web_fetch or browser."},
		func(ctx agent.Context, in OpenPageInput) (OpenPageOutput, error) {
			u, err := r.pageURL(in.URL)
			if err != nil {
				return OpenPageOutput{Error: err.Error()}, nil
			}
			where, err := r.openPage(ctx, u)
			if err != nil {
				return OpenPageOutput{URL: u, Error: err.Error()}, nil
			}
			return OpenPageOutput{URL: u, Where: where}, nil
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

// openPage shows u where browser.open says, or for "ask" where the chat
// answers (asked once; the embedded browser if no one answers). No
// desktop window: the system's browser on this machine. A run no one
// watches (a job's) opens nothing.
func (r *Registry) openPage(ctx context.Context, u string) (string, error) {
	if isUnattended(ctx) {
		return "", errors.New("nobody is watching this run to see the page")
	}
	p := &r.page
	p.mu.Lock()
	opener := p.opener
	p.mu.Unlock()
	if opener != nil {
		where := r.pageWhere(ctx, u)
		if err := opener(ctx, u, where); err == nil {
			return where, nil
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
	if answer, err := r.hooks.askUser(ctx, fmt.Sprintf(openAsk, host), []string{openEmbedded, openSystem}); err == nil && answer == openSystem {
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
