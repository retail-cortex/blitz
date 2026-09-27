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
)

// The introductory Gemini 3.8 Flash price ends on 2026-12-31; a build made
// before then must price calls at the new rate afterwards.
func TestDefaultPricingAtAppliesPriceChanges(t *testing.T) {
	before := DefaultPricingAt(time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC))["gemini-3.8-flash"]
	if before.InputPerMTok != 0.75 || before.OutputPerMTok != 3.75 || before.CachedInputPerMTok != 0.075 {
		t.Errorf("before the change: %+v", before)
	}
	after := DefaultPricingAt(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))["gemini-3.8-flash"]
	if after.InputPerMTok != 1.50 || after.OutputPerMTok != 7.50 || after.CachedInputPerMTok != 0.15 {
		t.Errorf("from 2027-01-01: %+v", after)
	}
	// Other models, and the base table itself, are unchanged.
	if got := DefaultPricingAt(time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC))["claude-opus-5"]; got != DefaultPricing["claude-opus-5"] {
		t.Errorf("claude-opus-5 changed: %+v", got)
	}
	if DefaultPricing["gemini-3.8-flash"].InputPerMTok != 0.75 {
		t.Error("DefaultPricingAt modified DefaultPricing")
	}
}

func TestDefaultConfigUsesPricesInEffect(t *testing.T) {
	want := DefaultPricingAt(time.Now())["gemini-3.8-flash"]
	if got := DefaultConfig().Pricing["gemini-3.8-flash"]; got != want {
		t.Errorf("DefaultConfig price %+v, want %+v", got, want)
	}
}
