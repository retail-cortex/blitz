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
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// memLedger keeps sessions' usage and ledgers in memory.
type memLedger struct {
	mu    sync.Mutex
	usage map[string]api.Usage
	calls map[string][]api.Call
}

func newMemLedger() *memLedger {
	return &memLedger{usage: map[string]api.Usage{}, calls: map[string][]api.Call{}}
}

func (m *memLedger) Usage(id string) (api.Usage, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.usage[id]
	return u, ok
}

func (m *memLedger) SetUsage(id string, u api.Usage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage[id] = u
	return nil
}

func (m *memLedger) AppendCall(id string, c api.Call) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls[id] = append(m.calls[id], c)
	return nil
}

func (m *memLedger) Calls(id string) ([]api.Call, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]api.Call(nil), m.calls[id]...), nil
}

var _ LedgerStore = (*memLedger)(nil)

// A mixed conversation, priced by hand, against the ledger (spec_056
// CA-152): a Gemini turn with cached input and thinking, a Claude turn
// writing the cache, an OpenAI turn whose output includes its reasoning,
// a fallback with no price, a speech call and a search's queries. The
// ledger sums to the session's total, and the unpriced call is the only
// unknown.
func TestLedgerPricedByHand(t *testing.T) {
	tr := NewUsageTracker(map[string]config.ModelPrice{
		"gemini-3-pro":    {InputPerMTok: 2, CachedInputPerMTok: 0.5, OutputPerMTok: 12},
		"claude-opus-5-5": {InputPerMTok: 5, CachedInputPerMTok: 0.5, CacheWritePerMTok: 6.25, OutputPerMTok: 25},
		"gpt-5":           {InputPerMTok: 1.25, CachedInputPerMTok: 0.125, OutputPerMTok: 10},
		"tts":             {InputPerMTok: 0.5, OutputPerMTok: 10},
	})
	ctx := context.Background()
	usage := func(prompt, cached, candidates, thoughts int32) *genai.GenerateContentResponseUsageMetadata {
		return &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: prompt, CachedContentTokenCount: cached, CandidatesTokenCount: candidates, ThoughtsTokenCount: thoughts}
	}
	// OpenAI's 900 output tokens include its 300 of reasoning; the adapter
	// keeps usage as Gemini counts it (CA-122).
	openai := &model.LLMResponse{UsageMetadata: usage(8000, 2000, 900, 300)}
	stampResponse(fromGPT, openai)

	var calls []api.Call
	charge := func(c Charge) { calls = append(calls, tr.Charge(ctx, c)) }
	charge(Charge{Session: "s", Model: "gemini-3-pro", Origin: fromGemini, Usage: usage(10000, 4000, 500, 200)})
	charge(Charge{Session: "s", Model: "claude-opus-5-5", Origin: fromClaude, Usage: usage(20000, 5000, 1000, 0), CacheWrites: 3000})
	charge(Charge{Session: "s", Model: "gpt-5", Origin: fromGPT, Usage: openai.UsageMetadata})
	charge(Charge{Session: "s", Model: "llama-local", Origin: Origin{API: "openai", Provider: "ollama", Model: "llama-local"}, Usage: usage(800, 0, 50, 0)})
	calls = append(calls, tr.RecordSpeech(ctx, "s", "tts", usage(100, 0, 2000, 0)))
	calls = append(calls, tr.RecordSearch("s", 3, 0.105))

	want := []float64{
		(6000*2 + 4000*0.5 + 700*12) / 1e6,               // Gemini: uncached, cached, output with thinking
		(12000*5 + 5000*0.5 + 3000*6.25 + 1000*25) / 1e6, // Claude: uncached, read, written, output
		(6000*1.25 + 2000*0.125 + 900*10) / 1e6,          // OpenAI: reasoning counted once
		0,                                                // no price
		(100*0.5 + 2000*10) / 1e6,                        // speech
		0,                                                // search: apart from the tokens
	}
	require.Len(t, calls, len(want))
	for i, c := range calls {
		assert.InDelta(t, want[i], c.Usage.CostUSD, 1e-12, "call %d (%s)", i, c.Model)
	}
	assert.Equal(t, int64(900), calls[2].Usage.Output)
	assert.Equal(t, int64(300), calls[2].Usage.Reasoning)
	assert.Equal(t, 1, calls[3].Usage.Unpriced)
	assert.Nil(t, calls[3].Price, "no price to record")
	require.NotNil(t, calls[1].Price)
	assert.Equal(t, 6.25, calls[1].Price.CacheWrite)
	assert.Equal(t, api.PurposeSpeech, calls[4].Purpose)
	assert.Zero(t, calls[4].Usage.LastPrompt, "speech isn't the conversation's prompt")
	assert.Equal(t, api.PurposeSearch, calls[5].Purpose)

	ledger := api.Sum(calls).All
	total := tr.Session("s")
	assert.Equal(t, total.Calls, ledger.Calls)
	assert.Equal(t, 5, ledger.Calls)
	assert.InDelta(t, total.CostUSD, ledger.CostUSD, 1e-12)
	assert.InDelta(t, want[0]+want[1]+want[2]+want[4], ledger.CostUSD, 1e-12)
	assert.InDelta(t, 0.105, ledger.SearchCostUSD, 1e-12)
	assert.Equal(t, 1, ledger.Unpriced, "only the fallback's call is unknown")
	assert.Equal(t, total.Unpriced, ledger.Unpriced)
	assert.Equal(t, int64(500), ledger.Reasoning)
	assert.Equal(t, int64(3000), ledger.CacheWrite)
	assert.Equal(t, []string{"llama-local"}, tr.UnpricedModels("s"), "the models cost limits can't count")
	assert.Empty(t, tr.UnpricedModels("other"))
}

// A call with no usage reported is one call of unknown cost.
func TestChargeWithoutUsage(t *testing.T) {
	tr := NewUsageTracker(map[string]config.ModelPrice{"m": {InputPerMTok: 1}})
	c := tr.Charge(context.Background(), Charge{Session: "s", Task: "task-1", Purpose: api.PurposeTask, Model: "m"})
	assert.Equal(t, api.Usage{Calls: 1, Unpriced: 1}, c.Usage)
	assert.Nil(t, c.Price)
	assert.Equal(t, "task-1", c.Task)
	assert.Equal(t, 1, tr.Session("task-1").Calls, "a task's own total, for its cost cap")
	assert.Equal(t, api.PurposeTurn, tr.Charge(context.Background(), Charge{Session: "s", Model: "m"}).Purpose, "a turn's, by default")
}

// The organization's catalog prices its models the price table doesn't.
func TestCatalogPrices(t *testing.T) {
	tr := NewUsageTracker(map[string]config.ModelPrice{"gemini-3-pro": {InputPerMTok: 2}})
	assert.False(t, tr.HasPrice("org-model"))
	tr.SetCatalogPrices(func(m string) (config.ModelPrice, bool) {
		if m == "org-model" || m == "gemini-3-pro" {
			return config.ModelPrice{InputPerMTok: 9, OutputPerMTok: 9}, true
		}
		return config.ModelPrice{}, false
	})
	p, ok := tr.priceFor("org-model")
	require.True(t, ok)
	assert.Equal(t, 9.0, p.InputPerMTok)
	p, _ = tr.priceFor("gemini-3-pro")
	assert.Equal(t, 2.0, p.InputPerMTok, "the price table first")
	assert.False(t, tr.HasPrice(""))
}

func ledgerEngine(t *testing.T, cfg *config.Config, llm model.LLM, store UsageStore) *Engine {
	t.Helper()
	agentReg, err := agents.NewRegistry()
	require.NoError(t, err)
	skillProv, err := skills.NewProvider()
	require.NoError(t, err)
	toolReg, err := tools.NewRegistry(cfg, agentReg, skillProv)
	require.NoError(t, err)
	t.Cleanup(func() { toolReg.Close() })
	eng, err := NewEngine(context.Background(), cfg, agentReg, skillProv, toolReg, llm, WithUsageStore(store))
	require.NoError(t, err)
	return eng
}

// Each call a session's turn and its compaction make is kept in its
// ledger, as the model that answered: a summary a fallback wrote is the
// fallback's, never the primary's (CA-92, CA-120).
func TestLedgerKeepsEachCall(t *testing.T) {
	cfg := conformanceConfig(t)
	cfg.Pricing = map[string]config.ModelPrice{"claude-opus-5-5": {InputPerMTok: 5, OutputPerMTok: 25}}
	chain := newFallbackModel([]model.LLM{
		&settingsModel{inner: failing{name: "claude-opus-5-5"}, provider: "anthropic"},
		&settingsModel{inner: servedBy{name: "llama-local-7b"}, provider: "openai", configured: "ollama"},
	})
	store := newMemLedger()
	eng := ledgerEngine(t, cfg, chain, store)
	for _, prompt := range []string{"hello", "again", "once more"} {
		_, err := collect(t, eng, "s", prompt)
		require.NoError(t, err)
	}
	_, err := eng.Compact(context.Background(), "s", "", 1)
	require.NoError(t, err)

	calls, totals, err := eng.Ledger("s")
	require.NoError(t, err)
	require.Len(t, calls, 4)
	for _, c := range calls[:3] {
		assert.Equal(t, api.PurposeTurn, c.Purpose)
		assert.Equal(t, "llama-local-7b", c.Model)
		assert.Equal(t, "ollama", c.Provider)
		assert.Equal(t, 1, c.Usage.Unpriced, "the fallback has no price")
	}
	assert.Equal(t, api.PurposeCompaction, calls[3].Purpose)
	assert.Equal(t, "llama-local-7b", calls[3].Model, "the summary is the fallback's")
	assert.Equal(t, 4, totals.All.Calls)
	assert.Equal(t, 4, totals.All.Unpriced)
	assert.Zero(t, totals.All.CostUSD, "nothing charged at the primary's price")
	saved, _ := store.Usage("s")
	assert.Equal(t, totals.All.Calls, saved.Calls, "the saved total agrees")
}

// A session from before the ledger shows what it spent as one call at its
// head (CA-126).
func TestLedgerOfAnOldSession(t *testing.T) {
	store := newMemLedger()
	require.NoError(t, store.SetUsage("old", api.Usage{Calls: 7, Input: 50000, Output: 4000, CostUSD: 0.42, Priced: true}))
	eng := ledgerEngine(t, conformanceConfig(t), servedBy{name: "m"}, store)
	calls, totals, err := eng.Ledger("old")
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, api.PurposeBefore, calls[0].Purpose)
	assert.Equal(t, 7, totals.All.Calls)
	assert.InDelta(t, 0.42, totals.All.CostUSD, 1e-12)

	calls, totals, err = eng.Ledger("new")
	require.NoError(t, err)
	assert.Empty(t, calls)
	assert.Zero(t, totals.All.Calls)
}
