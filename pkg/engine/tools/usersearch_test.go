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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGemini answers generateContent with grounding metadata whose links
// are redirects served by the same server, like Google's.
func fakeGemini(t *testing.T, got *captured) string {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/grounding-api-redirect/go"):
			http.Redirect(w, r, "https://go.dev/doc/effective_go#names", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/grounding-api-redirect/ads"):
			http.Redirect(w, r, "https://ads.evil.test/x", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/grounding-api-redirect/"):
			http.NotFound(w, r) // can't be resolved: kept as it is
		default:
			got.method, got.path, got.token = r.Method, r.URL.Path, r.Header.Get("x-goog-api-key")
			b, _ := io.ReadAll(r.Body)
			json.Unmarshal(b, &got.body)
			io.WriteString(w, strings.ReplaceAll(`{"candidates":[{"content":{"parts":[
				{"text":"thinking about it","thought":true},
				{"text":"Go names use MixedCaps."}]},
			  "groundingMetadata":{
				"webSearchQueries":["go naming conventions"],
				"groundingChunks":[
				  {"web":{"uri":"SRV/grounding-api-redirect/go1","title":"go.dev"}},
				  {"web":{"uri":"SRV/grounding-api-redirect/ads1","title":"ads"}},
				  {"web":{"uri":"SRV/grounding-api-redirect/missing","title":"missing.example"}}],
				"groundingSupports":[
				  {"segment":{"text":"Go names use MixedCaps."},"groundingChunkIndices":[0,2]},
				  {"segment":{"text":"Not underscores."},"groundingChunkIndices":[0]}]}}]}`, "SRV", srv.URL))
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestGoogleSearchUsesGroundingAndResolvesLinks(t *testing.T) {
	var got captured
	base := fakeGemini(t, &got)
	s := searcher(t, WebSearchConfig{Provider: "google", APIKey: "gem-key", BaseURL: base, DenyDomains: []string{"*.evil.test"}})
	// The queries Gemini ran are billed, for the session searching.
	var billed []string
	r0 := &Registry{searcher: s}
	r0.SetSearchBilling(func(_ context.Context, session, provider string, queries int) {
		billed = append(billed, fmt.Sprintf("%s %s %d", session, provider, queries))
	})
	out := s.search(WithOwnerSession(context.Background(), "chat"), allowAll(), WebSearchInput{Query: "go naming conventions"})
	require.Equal(t, "", out.Error, out.Error)
	assert.Equal(t, []string{"chat google 1"}, billed)
	assert.Equal(t, "POST", got.method, "request %+v", got)
	assert.Equal(t, "/models/gemini-3.8-flash:generateContent", got.path, "request %+v", got)
	assert.Equal(t, "gem-key", got.token, "request %+v", got)
	tools, _ := got.body["tools"].([]any)
	assert.Len(t, tools, 1, "no google_search tool in %v", got.body)
	assert.NotNil(t, tools[0].(map[string]any)["google_search"], "no google_search tool in %v", got.body)
	assert.Equal(t, "Go names use MixedCaps.", out.Answer, "answer %q (thoughts must be left out)", out.Answer)
	// The redirect is resolved, the denied target dropped, and the
	// unresolvable link kept.
	require.Len(t, out.Results, 2, "results %+v", out.Results)
	r := out.Results[0]
	assert.Equal(t, "https://go.dev/doc/effective_go#names", r.URL, "first result %+v", r)
	assert.Equal(t, "go.dev", r.Title, "first result %+v", r)
	assert.Equal(t, "Go names use MixedCaps. … Not underscores.", r.Snippet, "first result %+v", r)
	r = out.Results[1]
	assert.True(t, strings.HasSuffix(r.URL, "/grounding-api-redirect/missing"), "second result %+v", r)
}

func TestGoogleSearchConfig(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	_, err := newWebSearcher(WebSearchConfig{Provider: "google"})
	require.Error(t, err, "missing key")
	require.Contains(t, err.Error(), "Gemini API key", "missing key: %v", err)
	t.Setenv("GEMINI_API_KEY", "from-env")
	s, err := newWebSearcher(WebSearchConfig{Provider: "Google", Model: "gemini-3.7-flash"})
	require.NoError(t, err, "%+v", s)
	require.Equal(t, "from-env", s.cfg.APIKey, "%+v %v", s, err)
	require.Equal(t, "gemini-3.7-flash", s.cfg.Model, "%+v %v", s, err)
	require.True(t, strings.HasPrefix(s.cfg.BaseURL, "https://generativelanguage.googleapis.com/"), "%+v %v", s, err)
}

// The user's own search runs without asking; with nothing configured it
// says so.
func TestRegistryWebSearch(t *testing.T) {
	r := &Registry{hooks: NewHooks(Policy{})} // no approver: an approval would fail
	_, err := r.WebSearch(context.Background(), "x", 5)
	require.ErrorIs(t, err, api.ErrNoSearch, "unconfigured: %v", err)
	var got captured
	r.searcher = searcher(t, WebSearchConfig{Provider: "searxng", BaseURL: searchServer(t, `{"results":[{"title":"S","url":"https://s.example/","content":"hit"}]}`, 200, &got)})
	out, err := r.WebSearch(context.Background(), "q", 5)
	require.NoError(t, err, "%+v", out)
	require.Len(t, out.Results, 1, "%+v %v", out, err)
	require.Equal(t, "searxng", r.SearchProvider(), "%+v %v", out, err)
}

func TestFetchGrantsSkipApprovalForExactlyThoseURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("page " + r.URL.Path)) }))
	defer srv.Close()
	f := testFetcher(WebFetchConfig{AllowPrivate: true})
	ctx := WithFetchGrants(context.Background(), []string{srv.URL + "/picked#section"})

	h, reqs := approverHooks(false)
	out := f.fetch(ctx, h, srv.URL+"/picked")
	require.Equal(t, "page /picked", out.Content, "granted URL: %+v, %d prompts", out, len(*reqs))
	require.Len(t, *reqs, 0, "granted URL: %+v, %d prompts", out, len(*reqs))
	// Same host, another page: still asks (and is refused here).
	out = f.fetch(ctx, h, srv.URL+"/other")
	require.Contains(t, out.Error, "not approved", "other URL: %+v, %d prompts", out, len(*reqs))
	require.Len(t, *reqs, 1, "other URL: %+v, %d prompts", out, len(*reqs))
	// Without the grant in the context, the picked URL asks too.
	out = f.fetch(context.Background(), h, srv.URL+"/picked")
	require.Contains(t, out.Error, "not approved", "no grant: %+v", out)
}
