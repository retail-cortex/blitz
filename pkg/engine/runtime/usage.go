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

package runtime

import (
	"context"
	"sync"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/observability"
	"google.golang.org/genai"
)

// UsageTracker records usage per session.
type UsageTracker struct {
	mu       sync.Mutex
	pricing  map[string]config.ModelPrice
	sessions map[string]*api.Usage
}

// NewUsageTracker creates a tracker with the given price table.
func NewUsageTracker(pricing map[string]config.ModelPrice) *UsageTracker {
	return &UsageTracker{pricing: pricing, sessions: map[string]*api.Usage{}}
}

// price finds the price for model: exact, else the longest configured prefix
// ("gemini-3.8-flash-001" uses "gemini-3.8-flash").
func (t *UsageTracker) price(model string) (config.ModelPrice, bool) {
	return config.PriceFor(t.pricing, model)
}

// CacheWriteTokensKey is the LLMResponse.CustomMetadata key a model uses to
// report prompt tokens written to the cache (genai usage has no such field).
const CacheWriteTokensKey = "cache_creation_input_tokens"

// HasPrice reports whether a price is configured for model.
func (t *UsageTracker) HasPrice(model string) bool {
	_, ok := t.price(model)
	return ok
}

// Estimate converts token counts to USD for model. cacheWrites are prompt
// tokens (already counted in the prompt total) written to the cache.
func (t *UsageTracker) Estimate(model string, m *genai.GenerateContentResponseUsageMetadata, cacheWrites int64) api.Usage {
	u := api.Usage{Calls: 1, Priced: true}
	if m == nil {
		return u
	}
	u.Input = int64(m.PromptTokenCount) + int64(m.ToolUsePromptTokenCount)
	u.Cached = int64(m.CachedContentTokenCount)
	u.CacheWrite = min(max(cacheWrites, 0), max(u.Input-u.Cached, 0))
	u.Output = int64(m.CandidatesTokenCount) + int64(m.ThoughtsTokenCount)
	u.LastPrompt = u.Input
	p, ok := t.price(model)
	if !ok {
		u.Priced = false
		return u
	}
	writeRate := p.CacheWritePerMTok
	if writeRate == 0 {
		writeRate = p.InputPerMTok
	}
	uncached := max(u.Input-u.Cached-u.CacheWrite, 0)
	u.CostUSD = (float64(uncached)*p.InputPerMTok + float64(u.Cached)*p.CachedInputPerMTok +
		float64(u.CacheWrite)*writeRate + float64(u.Output)*p.OutputPerMTok) / 1e6
	return u
}

// Record adds one model call's usage to session.
func (t *UsageTracker) Record(session, model string, m *genai.GenerateContentResponseUsageMetadata) api.Usage {
	return t.RecordWrites(session, model, m, 0)
}

// RecordWrites is Record with a count of cache-write tokens.
func (t *UsageTracker) RecordWrites(session, model string, m *genai.GenerateContentResponseUsageMetadata, cacheWrites int64) api.Usage {
	u := t.Estimate(model, m, cacheWrites)
	observability.RecordTokens(context.Background(), model, u.Input, u.Output, u.Cached, u.CostUSD, u.Priced)
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.sessions[session]
	if s == nil {
		s = &api.Usage{Priced: true}
		t.sessions[session] = s
	}
	s.Add(u)
	return u
}

// RecordSpeech adds a speech model's call (generate_audio) to session:
// its tokens and cost, but not as a prompt, so the context's size stays
// the conversation's. Without usage (a provider that reports none) the
// cost is unknown.
func (t *UsageTracker) RecordSpeech(session, model string, m *genai.GenerateContentResponseUsageMetadata) {
	u := t.Estimate(model, m, 0)
	u.LastPrompt = 0
	if m == nil {
		u.Priced = false
	}
	observability.RecordTokens(context.Background(), model, u.Input, u.Output, u.Cached, u.CostUSD, u.Priced)
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.sessions[session]
	if s == nil {
		s = &api.Usage{Priced: true}
		t.sessions[session] = s
	}
	s.Add(u)
}

// RecordSearch adds web search queries, and what they cost, to session.
func (t *UsageTracker) RecordSearch(session string, queries int, costUSD float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.sessions[session]
	if s == nil {
		s = &api.Usage{Priced: true}
		t.sessions[session] = s
	}
	s.SearchQueries += queries
	s.SearchCostUSD += costUSD
}

// Seed sets session's usage to u when it has none yet (usage saved before
// a restart); it reports whether it did.
func (t *UsageTracker) Seed(session string, u api.Usage) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sessions[session] != nil {
		return false
	}
	t.sessions[session] = &u
	return true
}

// Has reports whether session has usage in the tracker.
func (t *UsageTracker) Has(session string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessions[session] != nil
}

// Session returns the usage recorded for session.
func (t *UsageTracker) Session(session string) api.Usage {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s := t.sessions[session]; s != nil {
		return *s
	}
	return api.Usage{Priced: true}
}
