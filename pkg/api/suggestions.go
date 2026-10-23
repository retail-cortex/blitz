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
	// SuggestSetup suggests /setup: the workspace has no agent instructions.
	SuggestSetup SuggestionKind = "setup"
	// SuggestHarnessUpdate suggests updating the skills and agents; Detail
	// says why: HarnessUpdateSpec, the specification (.agents/specs/)
	// changed after them (/skills update, then /agents update); "skills"
	// or "agents", the runs since they changed hold patterns about them
	// (/skills update or /agents update).
	SuggestHarnessUpdate SuggestionKind = "harness_update"
	// SuggestReflect suggests /reflect: the evidence since the last
	// reflection holds Count patterns, at least ReflectAt.
	SuggestReflect SuggestionKind = "reflect"
	// SuggestReflectReview suggests /reflect to review the Count changes a
	// scheduled reflection proposed.
	SuggestReflectReview SuggestionKind = "reflect_review"
	// SuggestContinue suggests picking up the latest conversation (SessionID,
	// Title).
	SuggestContinue SuggestionKind = "continue"
	// SuggestChanges suggests reviewing the Count uncommitted changes.
	SuggestChanges SuggestionKind = "changes"
	// SuggestWorkerFailed says the Worker's latest run failed (Detail: why).
	SuggestWorkerFailed SuggestionKind = "worker_failed"
	// SuggestIdea is a model's idea from the recent conversations (Title on
	// the tile, Prompt sent).
	SuggestIdea SuggestionKind = "idea"
)

// HarnessUpdateSpec is SuggestHarnessUpdate's Detail when the
// specification changed.
const HarnessUpdateSpec = "spec"

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
	// Tiles are the state tiles first, then ideas.
	Tiles []Suggestion
	// HarnessMissing means there's no .agents/AGENTS.md or root instruction
	// file.
	HarnessMissing bool
	// Pending means ideas are being written from newer conversations.
	Pending bool
}

// ReflectAt is how many patterns since the last reflection make the
// welcome screen suggest /reflect (spec_harness_learning_048 HE-05).
const ReflectAt = 5

// Reflection is what a workspace's harness has to learn, as the welcome
// screens say it.
type Reflection struct {
	// Patterns is how many items of the evidence since the last
	// reflection are patterns.
	Patterns int
	// Proposals is how many changes a scheduled reflection proposed,
	// waiting for /reflect.
	Proposals int
}

// Suggested is the welcome tile it makes, if any: the proposals to
// review, else /reflect once there are ReflectAt patterns.
func (r Reflection) Suggested() (Suggestion, bool) {
	switch {
	case r.Proposals > 0:
		return Suggestion{Kind: SuggestReflectReview, Count: r.Proposals}, true
	case r.Patterns >= ReflectAt:
		return Suggestion{Kind: SuggestReflect, Count: r.Patterns}, true
	}
	return Suggestion{}, false
}
