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

package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestViableLinks(t *testing.T) {
	in := []tools.SearchResult{
		{URL: "https://a.example/doc#intro"},
		{URL: "https://A.example/doc"}, // same page
		{URL: "https://b.example/paper.PDF"},
		{URL: "ftp://c.example/"},
		{URL: "https://vertexaisearch.cloud.google.com/grounding-api-redirect/x"},
		{URL: "https://d.example/"},
		{URL: "https://e.example/"},
		{URL: "https://f.example/"},
		{URL: "https://g.example/"},
		{URL: "https://h.example/"},
	}
	got := viableLinks(in, 5)
	var urls []string
	for _, r := range got {
		urls = append(urls, r.URL)
	}
	want := "https://a.example/doc#intro https://d.example/ https://e.example/ https://f.example/ https://g.example/"
	require.Equal(t, want, strings.Join(urls, " "), "got %v", urls)
}

func TestUsageContextAndSessionSearch(t *testing.T) {
	w, llm := openTestWith(t, nil, text("pineapple noted"))
	llm.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 10}
	_, err := w.SessionUsage()
	assert.ErrorIs(t, err, api.ErrNoActiveSession, "usage without a session: %v", err)
	_, err = w.Context(false)
	assert.ErrorIs(t, err, api.ErrNoActiveSession, "context without a session: %v", err)
	sid := newSession(t, w).ID
	_, err = w.Run(context.Background(), sid, api.Turn{Text: "remember pineapple"}, ignore)
	require.NoError(t, err)
	u, err := w.SessionUsage()
	assert.NoError(t, err, "usage %+v", u)
	assert.Equal(t, 1, u.Calls, "usage %+v %v", u, err)
	assert.Equal(t, int64(100), u.Input, "usage %+v %v", u, err)
	c, err := w.Context(true)
	assert.NoError(t, err, "context %+v", c)
	assert.Equal(t, int64(100), c.Tokens, "context %+v %v", c, err)
	var sum int64
	names := map[string]bool{}
	for _, p := range c.Parts {
		sum += p.Tokens
		names[p.Name] = true
	}
	assert.InDelta(t, 100, sum, float64(len(c.Parts)), "the parts add up to the total: %+v", c.Parts)
	for _, want := range []string{"system_prompt", "tool_declarations", "user_messages", "replies"} {
		assert.True(t, names[want], "no %s in %+v", want, c.Parts)
	}
	found, prompt := w.SearchSession("pineapple")
	assert.NotEqual(t, 0, found, "session search: %d %q", found, prompt)
	assert.Contains(t, prompt, "pineapple", "session search: %d %q", found, prompt)
	found, _ = w.SearchSession("mango")
	assert.Equal(t, 0, found, "found %d passages for mango", found)
}

func TestMemoryAndLocale(t *testing.T) {
	defer i18n.SetCurrent(nil)
	w := openTest(t)
	ctx := context.Background()
	paths, err := w.ReloadMemory(ctx)
	require.NoError(t, err, "memory before: %v", paths)
	require.Len(t, paths, 0, "memory before: %v %v", paths, err)
	p, err := w.AddMemory(ctx, "always run go vet")
	require.NoError(t, err, "add: %q", p)
	require.Equal(t, "BLITZ.md", filepath.Base(p), "add: %q %v", p, err)
	paths, _ = w.ReloadMemory(ctx)
	assert.Len(t, paths, 1, "memory after: %v", paths)

	_, localeErr := w.SetLocale(ctx, "zz-top-9")
	assert.ErrorIs(t, localeErr, api.ErrUnknownLocale)
	res, err := w.SetLocale(ctx, "japanese")
	require.NoError(t, err, "ja: %+v", res)
	require.Equal(t, "ja", res.Tag, "ja: %+v %v", res, err)
	require.False(t, res.HasCatalog, "ja: %+v %v", res, err)
	require.NoError(t, res.Saved.Err, "ja: %+v %v", res, err)
	require.Equal(t, "ja", w.Settings().Locale, "ja: %+v %v", res, err)
	// The interface language is the client's; the workspace leaves it alone.
	got := i18n.Current().Tag().String()
	assert.Equal(t, "en-US", got, "interface language changed to %s", got)
	got = savedConfig(t).UI.Locale
	assert.Equal(t, "ja", got, "saved locale %q", got)
	list, _ := w.AvailableLocales()
	assert.GreaterOrEqual(t, len(list), 3, "locales %v", list)
}

// Search queries count in the session's usage, apart from the tokens, and
// are kept with it (BL-WEB-01).
func TestSearchQueriesInUsage(t *testing.T) {
	w, _ := openTestWith(t, nil)
	sid := newSession(t, w).ID
	w.engine.RecordSearch(context.Background(), sid, 2, 0.028)
	w.engine.RecordSearch(context.Background(), sid, 1, 0.014)
	u, err := w.SessionUsage()
	require.NoError(t, err)
	assert.Equal(t, 3, u.SearchQueries)
	assert.InDelta(t, 0.042, u.SearchCostUSD, 1e-9)
	assert.Zero(t, u.CostUSD, "search cost mixed into the token cost")
	saved, ok := w.storage.Usage(sid)
	require.True(t, ok)
	assert.Equal(t, 3, saved.SearchQueries, "not saved with the session")
}
