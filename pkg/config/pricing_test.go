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

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// The introductory Gemini 3.8 Flash price ends on 2026-12-31; a build made
// before then must price calls at the new rate afterwards.
func TestDefaultPricingAtAppliesPriceChanges(t *testing.T) {
	before := DefaultPricingAt(time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC))["gemini-3.8-flash"]
	assert.Equal(t, 0.75, before.InputPerMTok, "before the change: %+v", before)
	assert.Equal(t, 3.75, before.OutputPerMTok, "before the change: %+v", before)
	assert.Equal(t, 0.075, before.CachedInputPerMTok, "before the change: %+v", before)
	after := DefaultPricingAt(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))["gemini-3.8-flash"]
	assert.Equal(t, 1.50, after.InputPerMTok, "from 2027-01-01: %+v", after)
	assert.Equal(t, 7.50, after.OutputPerMTok, "from 2027-01-01: %+v", after)
	assert.Equal(t, 0.15, after.CachedInputPerMTok, "from 2027-01-01: %+v", after)
	// Other models, and the base table itself, are unchanged.
	got := DefaultPricingAt(time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC))["claude-opus-5"]
	assert.Equal(t, DefaultPricing["claude-opus-5"], got, "claude-opus-5 changed: %+v", got)
	assert.Equal(t, 0.75, DefaultPricing["gemini-3.8-flash"].InputPerMTok, "DefaultPricingAt modified DefaultPricing")
	// The default model is priced, so costs show and --max-cost-usd holds,
	// and its price doubles on 2026-12-01.
	_, ok := DefaultPricing[DefaultConfig().LLM.Gemini.Model]
	assert.True(t, ok, "the default Gemini model has a price")
	argon := DefaultPricingAt(time.Date(2026, 11, 30, 23, 59, 59, 0, time.UTC))["gemini-4-argon"]
	assert.Equal(t, ModelPrice{InputPerMTok: 2, OutputPerMTok: 10, CachedInputPerMTok: 0.10}, argon)
	argon = DefaultPricingAt(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))["gemini-4-argon"]
	assert.Equal(t, ModelPrice{InputPerMTok: 4, OutputPerMTok: 20, CachedInputPerMTok: 0.20}, argon)
}

func TestDefaultConfigUsesPricesInEffect(t *testing.T) {
	want := DefaultPricingAt(time.Now())["gemini-3.8-flash"]
	got := DefaultConfig().Pricing["gemini-3.8-flash"]
	assert.Equal(t, want, got, "DefaultConfig price %+v, want %+v", got, want)
}

// Gemini 4's prices (spec_015_models MDL-97a): each of its four models
// is priced, a dated version by its model's price; Argon's and Carbon's
// double on 2026-12-01, Neon's and Hydrogen's don't change.
func TestGemini4Pricing(t *testing.T) {
	nov30 := time.Date(2026, 11, 30, 23, 59, 59, 0, time.UTC)
	dec1 := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	intro := ModelPrice{InputPerMTok: 2, OutputPerMTok: 10, CachedInputPerMTok: 0.10}
	full := ModelPrice{InputPerMTok: 4, OutputPerMTok: 20, CachedInputPerMTok: 0.20}
	neon := ModelPrice{InputPerMTok: 3, OutputPerMTok: 15, CachedInputPerMTok: 0.15}
	hydrogen := ModelPrice{InputPerMTok: 1.50, OutputPerMTok: 7.50, CachedInputPerMTok: 0.075}
	for _, tc := range []struct {
		model         string
		before, after ModelPrice
	}{
		{"gemini-4-argon", intro, full},
		{"gemini-4-carbon", intro, full},
		{"gemini-4-neon", neon, neon},
		{"gemini-4-hydrogen", hydrogen, hydrogen},
		{"gemini-4-carbon-001", intro, full}, // a dated version: its model's price
		{"gemini-4-neon-preview-11-2026", neon, neon},
	} {
		t.Run(tc.model, func(t *testing.T) {
			got, ok := PriceFor(DefaultPricingAt(nov30), tc.model)
			assert.True(t, ok, "priced")
			assert.Equal(t, tc.before, got, "through 2026-11-30")
			got, ok = PriceFor(DefaultPricingAt(dec1), tc.model)
			assert.True(t, ok, "priced")
			assert.Equal(t, tc.after, got, "from 2026-12-01")
		})
	}
	_, ok := PriceFor(DefaultPricingAt(dec1), "gemini-4-xenon")
	assert.False(t, ok, "a Gemini 4 model nobody priced has no price, not a sibling's")
}
