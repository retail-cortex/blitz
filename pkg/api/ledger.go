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
	"cmp"
	"slices"
	"time"
)

// What a call in a conversation's ledger was for (spec_056 CA-120).
const (
	PurposeTurn       = "turn"       // the conversation's own turns
	PurposeCompaction = "compaction" // summarising it to fit
	PurposeReview     = "review"     // the review model checking a plan or a change
	PurposeTask       = "task"       // a background task it started
	PurposeSpeech     = "speech"     // generate_audio's speech model
	PurposeImage      = "image"      // generate_image's image model
	PurposeSearch     = "search"     // a search provider's billed queries
	PurposeBefore     = "before"     // what was spent before the ledger was kept (CA-126)
)

// Price is what a model charged per million tokens when a call was made.
type Price struct {
	Input       float64 `json:"input"`
	CachedInput float64 `json:"cached_input,omitempty"`
	CacheWrite  float64 `json:"cache_write,omitempty"`
	Output      float64 `json:"output"`
}

// Call is one entry of a conversation's ledger: a model call, a speech
// call or a search's billed queries, with what it was for, who answered
// and what it cost at the time (spec_056 CA-120).
type Call struct {
	Time time.Time `json:"time"`
	// API, Provider and Model are who answered: the wire API, the
	// provider as configured, and the model.
	API      string `json:"api,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Purpose  string `json:"purpose"`
	// Task is the background task a task's call ran for.
	Task string `json:"task,omitempty"`
	// Usage is the call's tokens and cost (Calls is 1, or 0 for a search).
	Usage Usage `json:"usage"`
	// Price is what it was charged at; nil when its model had no price.
	Price *Price `json:"price,omitempty"`
}

// Group is the usage of one model, or one purpose, in a ledger.
type Group struct {
	Name  string
	Usage Usage
}

// Totals is a ledger summed: the whole, by model and by purpose, each
// sorted by cost, highest first (spec_056 CA-121).
type Totals struct {
	All       Usage
	ByModel   []Group
	ByPurpose []Group
}

// Sum adds up calls. A call with no price makes only its own cost
// unknown: the totals count it in Unpriced.
func Sum(calls []Call) Totals {
	all := Usage{Priced: true}
	byModel, byPurpose := map[string]*Usage{}, map[string]*Usage{}
	add := func(m map[string]*Usage, k string, u Usage) {
		if m[k] == nil {
			m[k] = &Usage{Priced: true}
		}
		m[k].Add(u)
	}
	for _, c := range calls {
		all.Add(c.Usage)
		// A search's queries, and what was spent before the ledger, have
		// no model: they're grouped by what they were.
		add(byModel, cmp.Or(c.Model, c.Purpose, "unknown"), c.Usage)
		add(byPurpose, cmp.Or(c.Purpose, PurposeTurn), c.Usage)
	}
	return Totals{All: all, ByModel: groups(byModel), ByPurpose: groups(byPurpose)}
}

func groups(m map[string]*Usage) []Group {
	out := make([]Group, 0, len(m))
	for k, u := range m {
		out = append(out, Group{Name: k, Usage: *u})
	}
	slices.SortFunc(out, func(a, b Group) int {
		if c := cmp.Compare(b.Usage.CostUSD+b.Usage.SearchCostUSD, a.Usage.CostUSD+a.Usage.SearchCostUSD); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}

// Before is the call that stands for what total holds and calls don't:
// what a session spent before its ledger was kept (spec_056 CA-126). ok is
// false when calls account for all of it.
func Before(total Usage, calls []Call) (Call, bool) {
	have := Sum(calls).All
	if total.Calls <= have.Calls && total.SearchQueries <= have.SearchQueries {
		return Call{}, false
	}
	u := Usage{
		Calls:         max(total.Calls-have.Calls, 0),
		Input:         max(total.Input-have.Input, 0),
		Cached:        max(total.Cached-have.Cached, 0),
		CacheWrite:    max(total.CacheWrite-have.CacheWrite, 0),
		Output:        max(total.Output-have.Output, 0),
		Reasoning:     max(total.Reasoning-have.Reasoning, 0),
		CostUSD:       max(total.CostUSD-have.CostUSD, 0),
		Priced:        total.Priced,
		Unpriced:      max(total.Unpriced-have.Unpriced, 0),
		SearchQueries: max(total.SearchQueries-have.SearchQueries, 0),
		SearchCostUSD: max(total.SearchCostUSD-have.SearchCostUSD, 0),
	}
	return Call{Purpose: PurposeBefore, Usage: u}, true
}
