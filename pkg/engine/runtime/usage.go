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
	"cmp"
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/observability"
	"google.golang.org/genai"
)

// UsageTracker records usage per session: its totals in memory, and each
// call for its ledger (spec_056_conversation_adapters CA-120).
type UsageTracker struct {
	mu       sync.Mutex
	pricing  map[string]config.ModelPrice
	sessions map[string]*api.Usage
	// catalog is the organization's price for a model of its catalog
	// (spec_053), for models the price table doesn't have; nil for none.
	catalog func(model string) (config.ModelPrice, bool)
	now     func() time.Time
	// unpriced is, per session, the models it called that have no price
	// (not those of calls that reported no usage, whose model has one).
	unpriced map[string]map[string]bool
}

// NewUsageTracker creates a tracker with the given price table.
func NewUsageTracker(pricing map[string]config.ModelPrice) *UsageTracker {
	return &UsageTracker{pricing: pricing, sessions: map[string]*api.Usage{}, now: time.Now, unpriced: map[string]map[string]bool{}}
}

// UnpricedModels are the models session called that have no price, so
// cost limits can't count their calls (spec_056 CA-124).
func (t *UsageTracker) UnpricedModels(session string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Sorted(maps.Keys(t.unpriced[session]))
}

// price finds the price for model (priceFor).
func (t *UsageTracker) price(model string) (config.ModelPrice, bool) {
	return t.priceFor(model)
}

// priceFor is the one place a model's price is read (CA-123): the price
// table, exact or by the longest prefix ("gemini-3.8-flash-001" uses
// "gemini-3.8-flash"), with the settings' [pricing] already in it; else
// the organization's catalog, for its models.
func (t *UsageTracker) priceFor(model string) (config.ModelPrice, bool) {
	if p, ok := config.PriceFor(t.pricing, model); ok {
		return p, true
	}
	if t.catalog != nil && model != "" {
		return t.catalog(model)
	}
	return config.ModelPrice{}, false
}

// SetCatalogPrices has the tracker price the organization's models by f
// where its price table has no price.
func (t *UsageTracker) SetCatalogPrices(f func(model string) (config.ModelPrice, bool)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.catalog = f
}

// Charge is a model call to record: whose session it's charged to, what
// it was for, who answered and what it used.
type Charge struct {
	Session string
	// Task is the background task it ran for, if any; its own total is
	// kept too, for its cost cap.
	Task    string
	Purpose string
	// Model is the model the call is priced as (the one that answered).
	Model       string
	Origin      Origin
	Usage       *genai.GenerateContentResponseUsageMetadata
	CacheWrites int64
}

// Charge records one model call: in the session's totals (and its task's)
// and as the ledger entry it returns, for the session's store to keep.
func (t *UsageTracker) Charge(ctx context.Context, c Charge) api.Call {
	call := t.bill(ctx, c)
	t.add(c.Session, call.Usage)
	if c.Task != "" {
		t.add(c.Task, call.Usage)
	}
	if _, priced := t.priceFor(c.Model); !priced && c.Model != "" {
		t.mu.Lock()
		if t.unpriced[c.Session] == nil {
			t.unpriced[c.Session] = map[string]bool{}
		}
		t.unpriced[c.Session][c.Model] = true
		t.mu.Unlock()
	}
	return call
}

// bill prices c as a ledger entry, without adding it to any total.
func (t *UsageTracker) bill(ctx context.Context, c Charge) api.Call {
	u := t.Estimate(c.Model, c.Usage, c.CacheWrites)
	observability.RecordTokens(ctx, c.Model, u.Input, u.Output, u.Cached, u.CostUSD, u.Priced)
	call := api.Call{
		Time: t.now().UTC(), API: c.Origin.API, Provider: c.Origin.Provider,
		Model: cmp.Or(c.Model, c.Origin.Model), Purpose: cmp.Or(c.Purpose, api.PurposeTurn), Task: c.Task, Usage: u,
	}
	if p, ok := t.priceFor(c.Model); ok && c.Usage != nil {
		call.Price = &api.Price{Input: p.InputPerMTok, CachedInput: p.CachedInputPerMTok, CacheWrite: cmp.Or(p.CacheWritePerMTok, p.InputPerMTok), Output: p.OutputPerMTok}
	}
	return call
}

func (t *UsageTracker) add(session string, u api.Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.sessions[session]
	if s == nil {
		s = &api.Usage{Priced: true}
		t.sessions[session] = s
	}
	s.Add(u)
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
// tokens (already counted in the prompt total) written to the cache. A
// call that reported no usage is one call of unknown cost (unpriced).
func (t *UsageTracker) Estimate(model string, m *genai.GenerateContentResponseUsageMetadata, cacheWrites int64) api.Usage {
	if m == nil {
		return api.Usage{Calls: 1, Unpriced: 1}
	}
	u := api.Usage{Calls: 1, Priced: true}
	u.Input = int64(m.PromptTokenCount) + int64(m.ToolUsePromptTokenCount)
	u.Cached = int64(m.CachedContentTokenCount)
	u.CacheWrite = min(max(cacheWrites, 0), max(u.Input-u.Cached, 0))
	// Usage is kept as Gemini counts it, thoughts apart from candidates;
	// the adapters normalise to that (CA-122, stampResponse).
	u.Output = int64(m.CandidatesTokenCount) + int64(m.ThoughtsTokenCount)
	u.Reasoning = int64(m.ThoughtsTokenCount)
	u.LastPrompt = u.Input
	p, ok := t.price(model)
	if !ok {
		u.Priced, u.Unpriced = false, 1
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
	return t.RecordCall(context.Background(), session, model, m, cacheWrites)
}

// RecordCall is RecordWrites for a call made under ctx, whose trace the
// token metrics keep.
func (t *UsageTracker) RecordCall(ctx context.Context, session, model string, m *genai.GenerateContentResponseUsageMetadata, cacheWrites int64) api.Usage {
	u := t.Estimate(model, m, cacheWrites)
	observability.RecordTokens(ctx, model, u.Input, u.Output, u.Cached, u.CostUSD, u.Priced)
	t.add(session, u)
	return u
}

// RecordSpeech adds a speech model's call (generate_audio) to session:
// its tokens and cost, but not as a prompt, so the context's size stays
// the conversation's. Without usage (a provider that reports none) the
// cost is unknown. It returns the call for the session's ledger.
func (t *UsageTracker) RecordSpeech(ctx context.Context, session, model string, m *genai.GenerateContentResponseUsageMetadata) api.Call {
	return t.RecordMedia(ctx, session, api.PurposeSpeech, model, m)
}

// RecordMedia adds a call made for purpose by a model that makes media
// (speech, pictures) to session, as RecordSpeech describes.
func (t *UsageTracker) RecordMedia(ctx context.Context, session, purpose, model string, m *genai.GenerateContentResponseUsageMetadata) api.Call {
	c := t.bill(ctx, Charge{Session: session, Purpose: purpose, Model: model, Usage: m})
	c.Usage.LastPrompt = 0
	t.add(session, c.Usage)
	return c
}

// RecordSearch adds web search queries, and what they cost, to session,
// and returns them as a call for its ledger.
func (t *UsageTracker) RecordSearch(session string, queries int, costUSD float64) api.Call {
	u := api.Usage{Priced: true, SearchQueries: queries, SearchCostUSD: costUSD}
	t.add(session, u)
	return api.Call{Time: t.now().UTC(), Purpose: api.PurposeSearch, Usage: u}
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

// Forget drops sessions' (or tasks') usage: deleted, or gone for good.
func (t *UsageTracker) Forget(ids ...string) {
	if len(ids) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, id := range ids {
		delete(t.sessions, id)
	}
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
