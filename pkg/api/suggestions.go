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

// SuggestionKind is what a welcome tile does.
type SuggestionKind string

const (
	// SuggestSetup: the workspace has no agent instructions; run /setup.
	SuggestSetup SuggestionKind = "setup"
	// SuggestContinue: pick up the latest conversation (SessionID, Title).
	SuggestContinue SuggestionKind = "continue"
	// SuggestChanges: review the Count uncommitted changes.
	SuggestChanges SuggestionKind = "changes"
	// SuggestWorkerFailed: the Worker's latest run failed (Detail: why).
	SuggestWorkerFailed SuggestionKind = "worker_failed"
	// SuggestIdea: a model's idea from the recent conversations (Title on
	// the tile, Prompt sent).
	SuggestIdea SuggestionKind = "idea"
)

// Suggestion is one welcome tile. State kinds carry data for a front end
// to word; an idea carries its text.
type Suggestion struct {
	Kind      SuggestionKind
	Title     string
	Prompt    string
	SessionID string
	Worker    string
	Count     int
	Detail    string
}

// Suggestions are a workspace's welcome tiles.
type Suggestions struct {
	// Tiles: state tiles first, then ideas.
	Tiles []Suggestion
	// HarnessMissing: no .agents/AGENT.md or root instruction file.
	HarnessMissing bool
	// Pending: ideas are being written from newer conversations.
	Pending bool
}
