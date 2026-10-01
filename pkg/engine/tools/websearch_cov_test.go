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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Billing is set only when there's a search provider to bill.
func TestSetSearchBillingWithoutProvider(t *testing.T) {
	r := &Registry{}
	assert.NotPanics(t, func() { r.SetSearchBilling(func(context.Context, string, string, int) {}) })
	assert.Nil(t, r.searcher)
}

// The web_search tool returns at most the results asked for.
func TestWebSearchToolLimitsResults(t *testing.T) {
	var c captured
	u := searchServer(t, `{"web":{"results":[
		{"title":"A","url":"https://a.example/"},
		{"title":"B","url":"https://b.example/"},
		{"title":"C","url":"https://c.example/"}]}}`, 200, &c)
	s := searcher(t, WebSearchConfig{Provider: "brave", APIKey: "k", BaseURL: u})
	tl := toolOf(t)(newWebSearchTool(s, allowAll()))
	out := runTool(t, tl, map[string]any{"query": "x", "max_results": 2})
	require.Empty(t, errOf(out))
	assert.Len(t, out["results"], 2)
}

// Each provider reports a failed request as the search's error.
func TestWebSearchProviderErrors(t *testing.T) {
	ctx := context.Background()
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	tests := []struct {
		name string
		cfg  WebSearchConfig
		want string
	}{
		{"unreachable", WebSearchConfig{Provider: "brave", APIKey: "k", BaseURL: closed.URL}, "brave search:"},
		{"tavily", WebSearchConfig{Provider: "tavily", APIKey: "k"}, "tavily search: HTTP 500"},
		{"searxng", WebSearchConfig{Provider: "searxng"}, "searxng search: HTTP 500"},
		{"google", WebSearchConfig{Provider: "google", APIKey: "k"}, "google search: HTTP 500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cfg.BaseURL == "" {
				var c captured
				tt.cfg.BaseURL = searchServer(t, "boom", 500, &c)
			}
			out := searcher(t, tt.cfg).search(ctx, allowAll(), WebSearchInput{Query: "q"})
			assert.Contains(t, out.Error, tt.want)
		})
	}
}

// Google search: an answer without candidates is an error; chunks without
// a link are dropped; redirect links that can't be resolved are kept.
func TestGoogleSearchEdges(t *testing.T) {
	ctx := context.Background()
	var c captured
	empty := searcher(t, WebSearchConfig{Provider: "google", APIKey: "k", BaseURL: searchServer(t, `{"candidates":[]}`, 200, &c)})
	assert.Contains(t, empty.search(ctx, allowAll(), WebSearchInput{Query: "q"}).Error, "no answer")

	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	reply := `{"candidates":[{"content":{"parts":[{"text":"thinking","thought":true},{"text":"Answer."}]},
		"groundingMetadata":{"groundingChunks":[
			{"web":{"uri":"","title":"none"}},
			{"web":{"uri":"` + gone.URL + `/grounding-api-redirect/abc","title":"Gone"}},
			{"web":{"uri":"https://bad.example/%zz","title":"Bad"}}]}}]}`
	s := searcher(t, WebSearchConfig{Provider: "google", APIKey: "k", BaseURL: searchServer(t, reply, 200, &c)})
	out := s.search(ctx, allowAll(), WebSearchInput{Query: "q"})
	require.Empty(t, out.Error)
	assert.Equal(t, "Answer.", out.Answer, "thoughts left out")
	require.Len(t, out.Results, 1, "the empty link and the unparsable one are dropped")
	assert.Equal(t, gone.URL+"/grounding-api-redirect/abc", out.Results[0].URL, "an unresolved redirect is kept")
}
