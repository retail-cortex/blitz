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
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/textutil"
	"github.com/stretchr/testify/assert"
)

func TestNeedsBrowser(t *testing.T) {
	scripts := `<html><head><script src="/app.js"></script><script>boot()</script></head><body><div id="root"></div></body></html>`
	article := "<html><body><h1>News</h1><p>" + strings.Repeat("A long article about the economy. ", 20) + "</p><script>x()</script><script>y()</script></body></html>"
	tests := []struct {
		name, mediaType, body string
		status                int
		want                  string
	}{
		{"a bot check (Cloudflare)", "text/html", `<title>Just a moment...</title><div id="cf-chl-widget"></div>`, 403, "bot check"},
		{"rate limited with a captcha", "text/html", `<p>Please complete the captcha</p>`, 429, "bot check"},
		{"a single-page app", "text/html", scripts, 200, "needs JavaScript"},
		{"a page that says to enable JavaScript", "text/html", `<noscript>Please enable JavaScript to continue.</noscript>`, 200, "needs JavaScript"},
		{"an article with scripts", "text/html", article, 200, ""},
		{"a plain 403", "text/html", `<h1>Forbidden</h1>`, 403, ""},
		{"a 404", "text/html", `<h1>Not found</h1>`, 404, ""},
		{"JSON", "application/json", `{"captcha": true}`, 403, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := textOf(tt.mediaType, tt.body)
			got := needsBrowser(tt.status, tt.mediaType, []byte(tt.body), text)
			if tt.want == "" {
				assert.Empty(t, got)
			} else {
				assert.Contains(t, got, tt.want)
			}
		})
	}
}

// textOf is the text web_fetch makes of body.
func textOf(mediaType, body string) string {
	if mediaType == "text/html" {
		return textutil.HTMLToText(body)
	}
	return body
}

// web_fetch loads a page that needs a browser in the browser, the URL
// already approved (so the user isn't asked twice), and says so; a plain
// page, or no browser, is the plain answer.
func TestWebFetchFallsBackToTheBrowser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/app":
			w.Write([]byte(`<html><script src="a.js"></script><script>boot()</script><div id="root"></div></html>`))
		case "/check":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`<title>Just a moment...</title>`))
		default:
			w.Write([]byte("<p>" + strings.Repeat("Plain words. ", 30) + "</p>"))
		}
	}))
	defer srv.Close()

	type call struct {
		url     string
		granted bool
	}
	tests := []struct {
		name     string
		path     string
		browser  bool
		fail     error
		via      string
		content  string
		note     string
		browsed  bool
		errorMsg string
	}{
		{name: "a single-page app", path: "/app", browser: true, via: "browser", content: "The app\n\nRendered by scripts", note: "needs JavaScript", browsed: true},
		{name: "a bot check", path: "/check", browser: true, via: "browser", content: "The app", note: "bot check", browsed: true},
		{name: "a plain page", path: "/plain", browser: true, content: "Plain words."},
		{name: "no browser", path: "/app", browser: false},
		{name: "the browser fails too", path: "/check", browser: true, fail: errors.New("chrome not found"), note: "couldn't load it either: chrome not found", browsed: true, errorMsg: "HTTP 403"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []call
			cfg := WebFetchConfig{AllowNetwork: true, AllowPrivate: true, MaxBytes: 1 << 20}
			if tt.browser {
				cfg.Browser = func(ctx context.Context, u string) (BrowserOutput, error) {
					parsed, _ := url.Parse(u)
					calls = append(calls, call{u, fetchGranted(ctx, parsed)})
					if tt.fail != nil {
						return BrowserOutput{}, tt.fail
					}
					return BrowserOutput{URL: u, Title: "The app", Text: "Rendered by scripts"}, nil
				}
			}
			out := runTool(t, toolOf(t)(NewWebFetchTool(cfg, allowAll())), map[string]any{"url": srv.URL + tt.path})
			assert.Equal(t, tt.browsed, len(calls) == 1, "calls %v", calls)
			if len(calls) == 1 {
				assert.True(t, calls[0].granted, "the browser is told the URL was approved")
			}
			if tt.via != "" {
				assert.Equal(t, tt.via, out["via"])
			} else {
				assert.Nil(t, out["via"])
			}
			if tt.content != "" {
				assert.Contains(t, out["content"], tt.content)
			}
			if tt.note != "" {
				assert.Contains(t, out["note"], tt.note)
			}
			if tt.errorMsg != "" {
				assert.Equal(t, tt.errorMsg, errOf(out))
			}
		})
	}
}
