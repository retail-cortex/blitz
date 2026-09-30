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
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/textutil"
	"golang.org/x/net/html"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

const (
	webMaxRedirects   = 5
	webMaxOutputChars = 100 * 1024
)

// WebFetchConfig configures web_fetch.
type WebFetchConfig struct {
	AllowDomains []string // fetched without approval
	DenyDomains  []string // never fetched
	AllowPrivate bool     // permit loopback/private/link-local targets
	AllowNetwork bool     // mirrors sandbox.allow_network
	MaxBytes     int64
	Timeout      time.Duration
	// Rules are the permission rules: web(host) deny refuses, ask always
	// asks (even for allow_domains), allow skips the question.
	Rules *PermissionRules
}

// ErrBlockedAddress is returned when a request targets a non-public address.
var ErrBlockedAddress = errors.New("destination address is not allowed")

// cgnat is 100.64.0.0/10 (carrier-grade NAT), not covered by netip.IsPrivate.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// publicAddr reports whether addr is a routable public address. Loopback,
// private, link-local (including cloud metadata at 169.254.169.254),
// multicast, and unspecified addresses are rejected.
func publicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsValid() && !addr.IsLoopback() && !addr.IsPrivate() && !addr.IsLinkLocalUnicast() &&
		!addr.IsLinkLocalMulticast() && !addr.IsInterfaceLocalMulticast() && !addr.IsMulticast() &&
		!addr.IsUnspecified() && !cgnat.Contains(addr) && !(addr.Is4() && addr.As4()[0] == 0)
}

type webFetcher struct {
	cfg    WebFetchConfig
	client *http.Client
	// allowAddr decides whether a resolved address may be dialled.
	allowAddr func(netip.AddrPort) bool
}

func newWebFetcher(cfg WebFetchConfig) *webFetcher {
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 2 << 20
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	f := &webFetcher{cfg: cfg}
	f.allowAddr = func(ap netip.AddrPort) bool { return cfg.AllowPrivate || publicAddr(ap.Addr()) }

	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		// Control runs after DNS resolution with the actual IP being dialled,
		// so a hostname that resolves to an internal address (or rebinds to
		// one between lookups) is still refused, on every redirect hop too.
		Control: func(network, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("%w: %s", ErrBlockedAddress, address)
			}
			if !f.allowAddr(ap) {
				return fmt.Errorf("%w: %s is not a public address (set web.allow_private to permit)", ErrBlockedAddress, ap.Addr())
			}
			return nil
		},
	}
	transport := &http.Transport{
		Proxy:                 nil, // a proxy would bypass the address check
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: cfg.Timeout,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	f.client = &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= webMaxRedirects {
				return fmt.Errorf("stopped after %d redirects", webMaxRedirects)
			}
			if err := f.checkURL(req.URL); err != nil {
				return err
			}
			// A redirect to another host must be independently allowed;
			// otherwise the model should request it explicitly (and be approved).
			first := via[0].URL.Hostname()
			if !strings.EqualFold(req.URL.Hostname(), first) && !matchDomain(f.cfg.AllowDomains, req.URL.Hostname()) {
				return fmt.Errorf("redirected from %s to %s; fetch that URL directly if needed", first, req.URL.Hostname())
			}
			return nil
		},
	}
	return f
}

func (f *webFetcher) checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http and https URLs are supported, got %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return errors.New("URL has no host")
	}
	if u.User != nil {
		return errors.New("URLs with embedded credentials are not allowed")
	}
	if matchDomain(f.cfg.DenyDomains, u.Hostname()) {
		return fmt.Errorf("%s is blocked by web.deny_domains", u.Hostname())
	}
	return nil
}

// matchDomain reports whether host matches any glob; "*.example.com" also
// matches "example.com".
func matchDomain(globs []string, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, g := range globs {
		g = strings.ToLower(strings.TrimSpace(g))
		if g == "" {
			continue
		}
		if ok, _ := path.Match(g, host); ok {
			return true
		}
		if strings.HasPrefix(g, "*.") && host == g[2:] {
			return true
		}
	}
	return false
}

// WebFetchInput defines arguments for web_fetch.
type WebFetchInput struct {
	URL string `json:"url" jsonschema:"The http(s) URL to fetch"`
}

// WebFetchOutput holds the fetched page.
type WebFetchOutput struct {
	URL         string `json:"url"`
	FinalURL    string `json:"final_url,omitempty"`
	Status      int    `json:"status,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Content     string `json:"content,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	Error       string `json:"error,omitempty"`
}

// NewWebFetchTool creates web_fetch: it retrieves a public web page and
// returns readable text. Domains outside web.allow_domains need approval.
func NewWebFetchTool(cfg WebFetchConfig, hooks *Hooks) (tool.Tool, error) {
	f := newWebFetcher(cfg)
	return functiontool.New(
		functiontool.Config{
			Name:        "web_fetch",
			Description: "Fetch a public web page or text/JSON document over http(s) and return its readable text",
		},
		func(ctx agent.Context, input WebFetchInput) (WebFetchOutput, error) {
			return f.fetch(ctx, hooks, input.URL), nil
		},
	)
}

func (f *webFetcher) fetch(ctx context.Context, hooks *Hooks, raw string) WebFetchOutput {
	out := WebFetchOutput{URL: raw}
	fail := func(err error) WebFetchOutput { out.Error = err.Error(); return out }

	if !f.cfg.AllowNetwork {
		return fail(errors.New("network access is disabled (sandbox.allow_network = false)"))
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fail(fmt.Errorf("invalid URL: %w", err))
	}
	if err := f.checkURL(u); err != nil {
		return fail(err)
	}
	if err := approveWeb(ctx, hooks, f.cfg, "web_fetch", "GET "+u.String(), u); err != nil {
		return fail(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fail(err)
	}
	req.Header.Set("User-Agent", "blitz/2 (+web_fetch)")
	req.Header.Set("Accept", "text/html,text/plain,application/json,application/xml;q=0.9,*/*;q=0.1")
	resp, err := f.client.Do(req)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()

	out.Status = resp.StatusCode
	out.FinalURL = resp.Request.URL.String()
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	out.ContentType = mediaType

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.cfg.MaxBytes+1))
	if err != nil {
		return fail(fmt.Errorf("read body: %w", err))
	}
	if int64(len(body)) > f.cfg.MaxBytes {
		body = body[:f.cfg.MaxBytes]
		out.Truncated = true
	}

	var text string
	switch {
	case mediaType == "text/html" || mediaType == "application/xhtml+xml":
		text = htmlToText(string(body))
	case strings.HasPrefix(mediaType, "text/"), mediaType == "application/json", strings.HasSuffix(mediaType, "+json"),
		mediaType == "application/xml", strings.HasSuffix(mediaType, "+xml"), mediaType == "application/javascript", mediaType == "":
		text = string(body)
	default:
		return fail(fmt.Errorf("unsupported content type %q", mediaType))
	}
	if len(text) > webMaxOutputChars {
		text = textutil.TruncateUTF8(text, webMaxOutputChars)
		out.Truncated = true
	}
	out.Content = strings.ToValidUTF8(text, "�")
	if resp.StatusCode >= 400 {
		out.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return out
}

// approveWeb decides a request to u's host by the web rules: a deny rule
// refuses, an ask rule always asks, web.allow_domains and URLs the user
// chose go ahead, and anything else is approved per host (the key web:host,
// shared by web_fetch and the browser).
func approveWeb(ctx context.Context, hooks *Hooks, cfg WebFetchConfig, toolName, detail string, u *url.URL) error {
	host := strings.ToLower(u.Hostname())
	effect, rule := cfg.Rules.Decide(RuleWeb, []string{host})
	if effect == EffectDeny {
		return fmt.Errorf("%s is denied by the permission rule deny %s", host, rule)
	}
	req := api.ApprovalRequest{
		Tool: toolName, Kind: api.ActionNetwork, Detail: detail,
		Key: "web:" + host, KeyLabel: "requests to " + host, Targets: []string{host},
	}
	switch {
	case effect == EffectAsk:
		req.MustAsk = true
		return hooks.Approve(ctx, req)
	case matchDomain(cfg.AllowDomains, host):
		return nil
	case fetchGranted(ctx, u):
		hooks.Audit().Log(audit.Entry{Kind: audit.KindApproval, Tool: toolName, Detail: detail, Decision: "user-selected"})
		return nil
	}
	return hooks.Approve(ctx, req)
}

// htmlToText extracts readable text from HTML: scripts and styles are
// dropped, block elements become line breaks, headings and list items keep
// light Markdown markers, and links keep their absolute targets.
func htmlToText(src string) string {
	z := html.NewTokenizer(strings.NewReader(src))
	var sb strings.Builder
	skip := 0
	var href string
	newline := func() {
		s := sb.String()
		if len(s) > 0 && !strings.HasSuffix(s, "\n") {
			sb.WriteString("\n")
		}
	}
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return collapseBlankLines(sb.String())
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			switch tag {
			case "script", "style", "noscript", "svg", "template", "iframe":
				if tt == html.StartTagToken {
					skip++
				}
			case "br", "p", "div", "section", "article", "tr", "table", "pre", "blockquote", "hr":
				newline()
			case "h1", "h2", "h3", "h4", "h5", "h6":
				newline()
				sb.WriteString(strings.Repeat("#", int(tag[1]-'0')) + " ")
			case "li":
				newline()
				sb.WriteString("- ")
			case "a":
				href = ""
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					if string(k) == "href" && (strings.HasPrefix(string(v), "http://") || strings.HasPrefix(string(v), "https://")) {
						href = string(v)
					}
				}
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch tag := string(name); tag {
			case "script", "style", "noscript", "svg", "template", "iframe":
				if skip > 0 {
					skip--
				}
			case "p", "div", "section", "article", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr", "pre", "blockquote":
				newline()
			case "a":
				if href != "" {
					sb.WriteString(" (" + href + ")")
					href = ""
				}
			}
		case html.TextToken:
			if skip > 0 {
				continue
			}
			text := strings.Join(strings.Fields(string(z.Text())), " ")
			if text == "" {
				continue
			}
			if s := sb.String(); len(s) > 0 && !strings.HasSuffix(s, "\n") && !strings.HasSuffix(s, " ") {
				sb.WriteString(" ")
			}
			sb.WriteString(text)
		}
	}
}

func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " ")
		if l == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
