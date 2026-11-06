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
	"errors"
)

// ContextInfo is the size of the model's context and when it is compacted.
type ContextInfo struct {
	Tokens int64 // the latest prompt's size
	// AutoCompact means the context is compacted when it passes Threshold
	// tokens, keeping the last Keep events.
	AutoCompact bool
	Threshold   int
	Keep        int
	// Parts estimate what the context is made of, by category, scaled to
	// Tokens (/context).
	Parts []ContextPart
}

// ContextPart is one category of what a prompt carries. Name is a key:
// system_prompt, instructions, tool_declarations, user_messages, replies,
// tool_calls, tool_results, images or summary.
type ContextPart struct {
	Name   string
	Tokens int64
}

// ReloadResult is what Reload loaded: how many agents and skills from
// files (built-in ones aside), the instruction files and rules, and the
// files that didn't load.
type ReloadResult struct {
	Agents   int
	Skills   int
	Files    []string
	Problems []string
}

// CompactResult is what Compact did.
type CompactResult struct {
	EventsCompacted int
	SummaryChars    int
	Before, After   Usage
}

// LocaleInfo names an interface language with a catalog.
type LocaleInfo struct{ Tag, Name string }

// ErrUnknownLocale reports input that isn't a language.
var ErrUnknownLocale = errors.New("not a language")

// LocaleChange describes the interface language after SetLocale.
type LocaleChange struct {
	Tag          string
	NativeName   string // e.g. "Español"
	LanguageName string // in the new language's terms, e.g. "español"
	// HasCatalog means the interface is translated; otherwise only the model's
	// replies are in the language.
	HasCatalog bool
	Saved      Saved
}

// ErrNoFetch reports that the agent can't fetch web pages, so a web search
// would give it nothing to read.
var ErrNoFetch = errors.New("web fetching is disabled")

// Link is a web search result the agent is asked to read.
type Link struct{ Title, URL string }

// WebSearch is a search whose results are handed to the agent: run Prompt
// as a turn with FetchGrants set to the links' URLs, so the agent may read
// exactly those pages without asking.
type WebSearch struct {
	Links  []Link // none: nothing worth handing over
	Prompt string
}

// URLs returns the links' URLs, for Turn.FetchGrants.
func (s WebSearch) URLs() []string {
	out := make([]string, len(s.Links))
	for i, l := range s.Links {
		out[i] = l.URL
	}
	return out
}

// ErrNothingToCompact is returned when a session is too short to compact.
var ErrNothingToCompact = errors.New("nothing to compact yet")

// ErrOutputLimit means the model's reply stopped at its output limit
// (max_tokens) before it was done, so the turn ended without it: a tool
// call cut short is dropped, which looks like nothing happened.
var ErrOutputLimit = errors.New("the model's reply hit its output limit (max_tokens) and was cut off: raise max_tokens for this model (Run settings › Generation, or max_tokens in [model_settings]) and ask again")

// ErrRefused means the model declined the request: its words are the
// turn's last reply (spec_056_conversation_adapters CA-31).
var ErrRefused = errors.New("the model declined this request")

// ErrBlocked means the provider's safety filters stopped the model's reply.
var ErrBlocked = errors.New("the provider's safety filters stopped the model's reply")

// ErrRecitation means the provider stopped the model's reply because it
// repeated material it may not reproduce (Gemini's RECITATION).
var ErrRecitation = errors.New("the provider stopped the model's reply because it repeated protected material: ask for it in other words")

// ErrMalformedToolCall means the model wrote a tool call that couldn't be
// read, again after being told so.
var ErrMalformedToolCall = errors.New("the model wrote a tool call that couldn't be read, twice: ask again, or try another model")

// ErrContextLength means the conversation is longer than the model takes,
// even summarised (CA-111).
var ErrContextLength = errors.New("the conversation is longer than the model can read, even summarised: start a new conversation, or switch to a model with a larger context window")

// ErrNoSearch is returned by Registry.WebSearch when no search provider is
// configured (web.search_provider), or it can't be used.
var ErrNoSearch = errors.New("web search is not set up")
