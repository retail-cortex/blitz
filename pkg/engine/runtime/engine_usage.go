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
	"log/slog"
	"slices"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"google.golang.org/genai"
)

// Usage returns the token usage and estimated cost recorded for a session.
func (e *Engine) Usage(sessionID string) api.Usage {
	e.restoreUsage(sessionID)
	return e.usage.Session(sessionID)
}

// restoreUsage seeds the tracker with sessionID's saved usage, once.
func (e *Engine) restoreUsage(sessionID string) {
	if e.usageStore == nil || e.usage.Has(sessionID) {
		return
	}
	if u, ok := e.usageStore.Usage(sessionID); ok {
		e.usage.Seed(sessionID, u)
	}
}

// RecordSearch adds a web search's billed queries, and their cost, to
// sessionID's usage, and saves it (BL-WEB-01).
func (e *Engine) RecordSearch(ctx context.Context, sessionID string, queries int, costUSD float64) {
	e.restoreUsage(sessionID)
	e.keepCall(ctx, sessionID, e.usage.RecordSearch(sessionID, queries, costUSD))
	e.saveUsage(ctx, sessionID)
}

// RecordSpeech adds a speech model's usage (generate_audio) to
// sessionID's usage, priced by [pricing] like any model's, and saves it.
// A provider that reports no usage leaves the cost unknown.
func (e *Engine) RecordSpeech(ctx context.Context, sessionID, model string, usage *genai.GenerateContentResponseUsageMetadata) {
	e.restoreUsage(sessionID)
	e.keepCall(ctx, sessionID, e.usage.RecordSpeech(ctx, sessionID, settingsName(model), usage))
	e.saveUsage(ctx, sessionID)
}

// RecordImage adds an image model's usage (generate_image) to
// sessionID's usage, as RecordSpeech does a speech model's.
func (e *Engine) RecordImage(ctx context.Context, sessionID, model string, usage *genai.GenerateContentResponseUsageMetadata) {
	e.restoreUsage(sessionID)
	e.keepCall(ctx, sessionID, e.usage.RecordMedia(ctx, sessionID, api.PurposeImage, settingsName(model), usage))
	e.saveUsage(ctx, sessionID)
}

// charge records a model call in its session's totals and ledger, and
// saves the totals.
func (e *Engine) charge(ctx context.Context, c Charge) {
	e.restoreUsage(c.Session)
	e.keepCall(ctx, c.Session, e.usage.Charge(ctx, c))
	e.saveUsage(ctx, c.Session)
}

// keepCall adds c to sessionID's ledger, when the store keeps ledgers.
func (e *Engine) keepCall(ctx context.Context, sessionID string, c api.Call) {
	ls, ok := e.usageStore.(LedgerStore)
	if !ok {
		return
	}
	if err := ls.AppendCall(sessionID, c); err != nil {
		slog.WarnContext(ctx, "could not keep the call in the session's ledger", "session", sessionID, "error", err)
	}
}

// HasPrice reports whether model ("provider/" prefix optional) has a
// price, so its calls count toward cost limits.
func (e *Engine) HasPrice(model string) bool {
	return e.usage.HasPrice(settingsName(model))
}

// PriceOf is model's price as the ledger charges it: the price table,
// then the organization's catalog (spec_056 CA-123).
func (e *Engine) PriceOf(model string) (config.ModelPrice, bool) {
	return e.usage.priceFor(settingsName(model))
}

// UnpricedModels are the models sessionID called that have no price.
func (e *Engine) UnpricedModels(sessionID string) []string {
	return e.usage.UnpricedModels(sessionID)
}

// Ledger is sessionID's calls, oldest first, with what was spent before
// its ledger was kept as one call at the head (spec_056 CA-126), and their
// totals by model and purpose (CA-121).
func (e *Engine) Ledger(sessionID string) ([]api.Call, api.Totals, error) {
	total := e.Usage(sessionID)
	var calls []api.Call
	if ls, ok := e.usageStore.(LedgerStore); ok {
		var err error
		if calls, err = ls.Calls(sessionID); err != nil {
			return nil, api.Totals{}, err
		}
	}
	if before, ok := api.Before(total, calls); ok {
		calls = append([]api.Call{before}, calls...)
	}
	return calls, api.Sum(calls), nil
}

// EstimateCostUSD is what a model call outside a session cost (an eval's
// judge), by the configured prices; 0 for a model without one.
func (e *Engine) EstimateCostUSD(model string, usage *genai.GenerateContentResponseUsageMetadata) float64 {
	if usage == nil {
		return 0
	}
	return e.usage.Estimate(settingsName(model), usage, 0).CostUSD
}

// EstimateUsage is a model call outside a session's usage (a rule's
// judge, a workflow's extract step), priced by the configured prices; a
// call that reported no tokens is unpriced.
func (e *Engine) EstimateUsage(model string, usage *genai.GenerateContentResponseUsageMetadata) api.Usage {
	return e.usage.Estimate(settingsName(model), usage, 0) // no usage: unpriced
}

// saveUsage keeps sessionID's usage so far in the store.
func (e *Engine) saveUsage(ctx context.Context, sessionID string) {
	if e.usageStore == nil {
		return
	}
	if err := e.usageStore.SetUsage(sessionID, e.usage.Session(sessionID)); err != nil {
		slog.WarnContext(ctx, "could not save the session's usage", "session", sessionID, "error", err)
	}
}

// settingsName is the key a model's settings are kept under: its name as
// the provider reports it, without a "provider/" prefix.
func settingsName(ref string) string {
	_, name := ParseModelRef(ref, "")
	return name
}

// ModelSettings returns the generation settings for model ("provider/"
// prefix optional); the zero value when it has none.
func (e *Engine) ModelSettings(model string) config.ModelSettings {
	e.settingsMu.RLock()
	defer e.settingsMu.RUnlock()
	return e.settings[settingsName(model)]
}

// AllModelSettings returns every model's settings, by model name.
func (e *Engine) AllModelSettings() map[string]config.ModelSettings {
	e.settingsMu.RLock()
	defer e.settingsMu.RUnlock()
	out := make(map[string]config.ModelSettings, len(e.settings))
	for k, v := range e.settings {
		out[k] = v
	}
	return out
}

// SetModelSettings replaces model's settings (the zero value removes
// them). They apply from the next model call, also to calls already running.
func (e *Engine) SetModelSettings(model string, s config.ModelSettings) {
	name := settingsName(model)
	e.settingsMu.Lock()
	defer e.settingsMu.Unlock()
	if s.IsZero() {
		delete(e.settings, name)
	} else {
		e.settings[name] = s
	}
}

// providerToolPolicy is what the settings and the organization's policy
// say about provider tools (spec_056 CA-81): all off with [tools]
// provider = false, and those the organization's tools.off names
// (provider:<kind>, provider:*).
func (e *Engine) providerToolPolicy() ProviderToolPolicy {
	all, off := ParseProviderToolsOff(e.cfg.Org.Policy.Tools.Off, "")
	if off == nil {
		off = map[string]bool{}
	}
	// What switches the client tool off switches the provider's off: the
	// organization naming it, or a deny rule for it.
	for _, kind := range []string{ptWebSearch, ptWebFetch, ptCodeExecution} {
		gate := gateTool(kind)
		if slices.Contains(e.cfg.Org.Policy.Tools.Off, gate) || e.toolReg != nil && e.toolReg.Rules().ToolDenied(gate, nil) != nil {
			off[kind] = true
		}
	}
	return ProviderToolPolicy{Off: all || !e.cfg.Tools.ProviderToolsOn(), Disabled: off}
}

func (e *Engine) lookupSettings(name string) (config.ModelSettings, bool) {
	e.settingsMu.RLock()
	defer e.settingsMu.RUnlock()
	s, ok := e.settings[name]
	if e.effort != "" { // the session's effort wins over the model's own
		effort := e.effort
		s.ReasoningEffort, ok = &effort, true
	}
	return s, ok
}

// SetEffort sets the reasoning effort for every model call from now on
// ("" returns each model to its own reasoning_effort, or the default).
func (e *Engine) SetEffort(effort string) {
	e.settingsMu.Lock()
	defer e.settingsMu.Unlock()
	e.effort = effort
}

// SetAgency sets the agency level the root agent's prompt names, from the
// next rebuild (the settings it was built with aren't written once open).
func (e *Engine) SetAgency(level string) {
	e.settingsMu.Lock()
	defer e.settingsMu.Unlock()
	e.agency = level
}

// agencyLevel is the agency level now.
func (e *Engine) agencyLevel() string {
	e.settingsMu.RLock()
	defer e.settingsMu.RUnlock()
	return e.agency
}

// Effort returns the session's reasoning effort ("" when unset).
func (e *Engine) Effort() string {
	e.settingsMu.RLock()
	defer e.settingsMu.RUnlock()
	return e.effort
}

func (e *Engine) generateConfig() *genai.GenerateContentConfig {
	gc := &genai.GenerateContentConfig{}
	if e.cfg.Blitz.Temperature > 0 {
		gc.Temperature = genai.Ptr(float32(e.cfg.Blitz.Temperature))
	}
	if e.cfg.Blitz.MaxTokens > 0 {
		gc.MaxOutputTokens = int32(e.cfg.Blitz.MaxTokens)
	}
	return gc
}
