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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captured struct {
	method, path, query, auth, token string
	body                             map[string]any
}

func searchServer(t *testing.T, reply string, status int, got *captured) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.query = r.Method, r.URL.Path, r.URL.RawQuery
		got.auth, got.token = r.Header.Get("Authorization"), r.Header.Get("X-Subscription-Token")
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			json.Unmarshal(b, &got.body)
		}
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func searcher(t *testing.T, cfg WebSearchConfig) *webSearcher {
	t.Helper()
	cfg.AllowNetwork = true
	s, err := newWebSearcher(cfg)
	require.NoError(t, err)
	return s
}

func TestWebSearchProviders(t *testing.T) {
	ctx := context.Background()

	var b captured
	braveURL := searchServer(t, `{"web":{"results":[
		{"title":"Go","url":"https://go.dev/","description":"The <strong>Go</strong> language"},
		{"title":"Blocked","url":"https://ads.evil.test/x","description":"no"},
		{"title":"FTP","url":"ftp://files.example.com/","description":"no"}]}}`, 200, &b)
	out := searcher(t, WebSearchConfig{Provider: "brave", APIKey: "brv-key", BaseURL: braveURL, DenyDomains: []string{"*.evil.test"}}).
		search(ctx, allowAll(), WebSearchInput{Query: "golang", MaxResults: 3})
	assert.Equal(t, "", out.Error, "brave results %+v", out)
	assert.Len(t, out.Results, 1, "brave results %+v", out)
	assert.Equal(t, "https://go.dev/", out.Results[0].URL, "brave results %+v", out)
	assert.Equal(t, "The Go language", out.Results[0].Snippet, "brave results %+v", out)
	assert.Equal(t, "brv-key", b.token, "brave request %+v", b)
	assert.Contains(t, b.query, "q=golang", "brave request %+v", b)
	assert.Contains(t, b.query, "count=3", "brave request %+v", b)

	var tv captured
	tavilyURL := searchServer(t, `{"results":[{"title":"A","url":"https://a.example/","content":"alpha"}]}`, 200, &tv)
	out = searcher(t, WebSearchConfig{Provider: "tavily", APIKey: "tvly-key", BaseURL: tavilyURL}).search(ctx, allowAll(), WebSearchInput{Query: "alpha"})
	assert.Len(t, out.Results, 1, "tavily results %+v", out)
	assert.Equal(t, "alpha", out.Results[0].Snippet, "tavily results %+v", out)
	assert.Equal(t, "POST", tv.method, "tavily request %+v", tv)
	assert.Equal(t, "Bearer tvly-key", tv.auth, "tavily request %+v", tv)
	assert.Equal(t, "alpha", tv.body["query"], "tavily request %+v", tv)
	assert.Equal(t, float64(5), tv.body["max_results"], "tavily request %+v", tv)

	var sx captured
	sxURL := searchServer(t, `{"results":[{"title":"S","url":"https://s.example/","content":"searx"}]}`, 200, &sx)
	out = searcher(t, WebSearchConfig{Provider: "searxng", BaseURL: sxURL + "/"}).search(ctx, allowAll(), WebSearchInput{Query: "q"})
	assert.Len(t, out.Results, 1, "searxng %+v / %+v", out, sx)
	assert.Equal(t, "/search", sx.path, "searxng %+v / %+v", out, sx)
	assert.Contains(t, sx.query, "format=json", "searxng %+v / %+v", out, sx)
}

func TestWebSearchApprovalAndErrors(t *testing.T) {
	ctx := context.Background()
	var c captured
	u := searchServer(t, `{"web":{"results":[]}}`, 200, &c)
	s := searcher(t, WebSearchConfig{Provider: "brave", APIKey: "k", BaseURL: u})

	h, reqs := approverHooks(false)
	out := s.search(ctx, h, WebSearchInput{Query: "secret project name"})
	assert.Contains(t, out.Error, "not approved", "expected denial: %+v", out)
	assert.Len(t, *reqs, 1, "approval request %+v", *reqs)
	assert.Equal(t, "search:brave", (*reqs)[0].Key, "approval request %+v", *reqs)
	assert.Contains(t, (*reqs)[0].Detail, "secret project name", "approval request %+v", *reqs)
	assert.Equal(t, "", c.method, "a denied search reached the provider")
	out = s.search(ctx, allowAll(), WebSearchInput{Query: "  "})
	assert.NotEqual(t, "", out.Error, "empty query should fail")
	off := searcher(t, WebSearchConfig{Provider: "brave", APIKey: "k", BaseURL: u})
	off.cfg.AllowNetwork = false
	out = off.search(ctx, allowAll(), WebSearchInput{Query: "x"})
	assert.Contains(t, out.Error, "disabled", "network off: %+v", out)

	for status, want := range map[int]string{401: "authentication failed", 429: "rate limited", 500: "HTTP 500"} {
		t.Run(want, func(t *testing.T) {
			var e captured
			bad := searcher(t, WebSearchConfig{Provider: "brave", APIKey: "k", BaseURL: searchServer(t, `{"error":"x"}`, status, &e)})
			out := bad.search(ctx, allowAll(), WebSearchInput{Query: "x"})
			assert.Contains(t, out.Error, want, "status %d: %q", status, out.Error)
		})
	}
	var g captured
	garbage := searcher(t, WebSearchConfig{Provider: "brave", APIKey: "k", BaseURL: searchServer(t, `<html>`, 200, &g)})
	out = garbage.search(ctx, allowAll(), WebSearchInput{Query: "x"})
	assert.Contains(t, out.Error, "unexpected response", "garbage: %q", out.Error)
}

func TestWebSearchConfigValidation(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "from-env")
	for name, cfg := range map[string]WebSearchConfig{
		"unknown":        {Provider: "bing"},
		"brave no key":   {Provider: "brave"},
		"searxng no url": {Provider: "searxng"},
		"bad url":        {Provider: "searxng", BaseURL: "file:///etc"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newWebSearcher(cfg)
			assert.Error(t, err, "%s: expected error", name)
		})
	}
	s, err := newWebSearcher(WebSearchConfig{Provider: "Tavily"})
	assert.NoError(t, err, "env key / default endpoint: %+v", s)
	assert.Equal(t, "from-env", s.cfg.APIKey, "env key / default endpoint: %+v %v", s, err)
	assert.Equal(t, searchEndpoints["tavily"], s.cfg.BaseURL, "env key / default endpoint: %+v %v", s, err)

	// Registry: registered only when a provider is configured; bad config fails startup.
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	reg, _ := NewRegistry(cfg, nil, nil)
	assert.Len(t, reg.GetToolsForAgent([]string{"web_search"}), 0, "web_search registered without a provider")
	reg.Close()
	cfg.Web.SearchProvider, cfg.Web.SearchAPIKey = "brave", "k"
	reg, err = NewRegistry(cfg, nil, nil)
	assert.NoError(t, err, "web_search not registered")
	assert.Len(t, reg.GetToolsForAgent([]string{"web_search"}), 1, "web_search not registered: %v", err)
	reg.Close()
	cfg.Web.SearchAPIKey = ""
	_, err = NewRegistry(cfg, nil, nil)
	assert.Error(t, err, "missing key should fail startup clearly")
	assert.Contains(t, err.Error(), "BRAVE_API_KEY", "missing key should fail startup clearly: %v", err)
}
