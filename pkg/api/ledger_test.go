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

package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A ledger sums to the conversation's totals, by model and by purpose; a
// call with no price makes only its own cost unknown (spec_056 CA-121).
func TestSum(t *testing.T) {
	calls := []Call{
		{Model: "gemini-3-pro", Purpose: PurposeTurn, Usage: Usage{Calls: 1, Input: 1000, Output: 100, CostUSD: 0.01, Priced: true}},
		{Model: "gemini-3-pro", Purpose: PurposeCompaction, Usage: Usage{Calls: 1, Input: 5000, Output: 400, CostUSD: 0.03, Priced: true}},
		{Model: "llama-local", Purpose: PurposeTurn, Usage: Usage{Calls: 1, Input: 800, Output: 50, Unpriced: 1}},
		{Purpose: PurposeSearch, Usage: Usage{SearchQueries: 2, SearchCostUSD: 0.07, Priced: true}},
	}
	got := Sum(calls)
	assert.Equal(t, 3, got.All.Calls)
	assert.InDelta(t, 0.04, got.All.CostUSD, 1e-9)
	assert.InDelta(t, 0.07, got.All.SearchCostUSD, 1e-9)
	assert.Equal(t, 1, got.All.Unpriced, "only the unpriced call is unknown")
	assert.False(t, got.All.Priced)

	require.Len(t, got.ByModel, 3)
	assert.Equal(t, "search", got.ByModel[0].Name, "costliest first")
	assert.Equal(t, "gemini-3-pro", got.ByModel[1].Name)
	assert.Equal(t, 2, got.ByModel[1].Usage.Calls)
	assert.Equal(t, "llama-local", got.ByModel[2].Name)
	assert.Equal(t, 1, got.ByModel[2].Usage.Unpriced)

	purposes := map[string]Usage{}
	for _, g := range got.ByPurpose {
		purposes[g.Name] = g.Usage
	}
	assert.Equal(t, 2, purposes[PurposeTurn].Calls)
	assert.InDelta(t, 0.03, purposes[PurposeCompaction].CostUSD, 1e-9)
	assert.Equal(t, 2, purposes[PurposeSearch].SearchQueries)

	empty := Sum(nil)
	assert.True(t, empty.All.Priced)
	assert.Empty(t, empty.ByModel)
	assert.Equal(t, "unknown", Sum([]Call{{Usage: Usage{Calls: 1}}}).ByModel[0].Name, "a call with nothing to name it")
}

// What a session spent before its ledger was kept is one call at its head
// (CA-126).
func TestBefore(t *testing.T) {
	total := Usage{Calls: 5, Input: 9000, Output: 900, CostUSD: 0.5, Priced: true, SearchQueries: 1, SearchCostUSD: 0.035}
	kept := []Call{{Model: "m", Usage: Usage{Calls: 2, Input: 3000, Output: 300, CostUSD: 0.2, Priced: true}}}
	before, ok := Before(total, kept)
	require.True(t, ok)
	assert.Equal(t, PurposeBefore, before.Purpose)
	assert.Equal(t, Usage{Calls: 3, Input: 6000, Output: 600, CostUSD: 0.3, Priced: true, SearchQueries: 1, SearchCostUSD: 0.035}, roundCost(before.Usage))

	_, ok = Before(Sum(kept).All, kept)
	assert.False(t, ok, "a ledger that holds it all")
	_, ok = Before(Usage{}, nil)
	assert.False(t, ok, "a new session")

	old, ok := Before(Usage{Calls: 4, CostUSD: 0.1}, nil)
	require.True(t, ok)
	assert.False(t, old.Usage.Priced, "an old session's unpriced total stays unpriced")
}

func roundCost(u Usage) Usage {
	u.CostUSD = float64(int(u.CostUSD*1e6+0.5)) / 1e6
	return u
}
