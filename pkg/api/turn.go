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
	"time"

	"github.com/retail-cortex/blitz/pkg/images"
)

// Turn is one prompt for the agent.
type Turn struct {
	// Text is what the user typed. prompt_submit hooks see it and the
	// transcript records it.
	Text string
	// Prompt, if set, is sent to the agent instead of Text (e.g. /search
	// sends the fetched results, and the transcript records the command).
	Prompt string
	// Plan: Text is a goal to plan for. Tools that change anything are
	// refused for the whole turn, and the transcript
	// records "/plan <Text>".
	Plan bool
	// ReadOnly names a mode that refuses the same tools as plan mode.
	ReadOnly string
	// Aside: a /btw question, answered in a throwaway copy of the session
	// and recorded nowhere. It takes no images, starts no checkpoint and
	// leaves queued steer messages alone.
	Aside bool
	// Accepted: Text already passed prompt_submit hooks and was recorded (a
	// steer message that arrived too late to be read mid-turn).
	Accepted bool
	// Command: Text is a custom slash command ("/name args"), expanded into
	// its prompt with its agent, model, tools and plan mode; the
	// transcript records Text.
	Command bool
	// Images are sent with the prompt.
	Images []*images.Image
	// MaxTurns limits the model calls in the turn (0: unlimited).
	MaxTurns int
	// MaxCostUSD stops the turn once it has cost more than this (0:
	// unlimited); it can only be enforced when the model has a price.
	MaxCostUSD float64
	// Timeout stops the turn after this long (0: unlimited).
	Timeout time.Duration
	// FetchGrants are URLs the agent may fetch in this turn without asking
	// (the pages a web search handed it).
	FetchGrants []string
	// OnAccepted, if set, runs once the prompt has passed prompt_submit
	// hooks and been recorded, just before it is sent.
	OnAccepted func()
	// OnFinished, if set, runs when the agent has stopped, before unread
	// steer messages are collected: a front end still taking a steer message
	// finishes here, so the message ends up in TurnResult.Leftover.
	OnFinished func()
}

// TurnResult describes a finished turn.
type TurnResult struct {
	// Output is the model's final text (partial and thought text excluded).
	Output string
	// Before and After are the session's usage around the turn.
	Before, After Usage
	// Leftover are steer messages sent after the model's last tool call, so
	// never read. Front ends send them as the next turn (with Accepted set)
	// or, if the turn was interrupted, drop them.
	Leftover []string
}

// Limits a turn can stop at. They wrap the limit, e.g. "the turn reached
// its cost limit ($0.50)"; ErrMaxTurns is the third.
var (
	ErrCostLimit = errors.New("the turn reached its cost limit")
	ErrTimeLimit = errors.New("the turn reached its time limit")
)

// IsLimit reports whether err is a turn stopping at one of its limits.
func IsLimit(err error) bool {
	return errors.Is(err, ErrMaxTurns) || errors.Is(err, ErrCostLimit) || errors.Is(err, ErrTimeLimit)
}

// BlockedError reports a prompt refused by a prompt_submit hook. Nothing
// was recorded or sent.
type BlockedError struct{ Reason string }

// Error says a prompt_submit hook blocked the prompt, and why.
func (e *BlockedError) Error() string { return "prompt blocked by hook: " + e.Reason }

// Event is something that happened during a turn. Exactly one of Text,
// ToolCall and ToolResult is set. Events arrive in order.
type Event struct {
	// Author is the agent that produced the event (a sub-agent's name
	// when one is working).
	Author     string
	Text       *Text
	ToolCall   *ToolCall
	ToolResult *ToolResult
	// Tasks is the agent's task list, whole, each time it changes (the todo
	// tool).
	Tasks []Task
}

// Task is an item of the agent's task list.
type Task struct {
	Content string
	Status  string // pending, in_progress or done
}

// Text is model output. With streaming, partial chunks arrive first and a
// final event then repeats their text in full (Repeat); without streaming
// only final events arrive. To show text once, show partial chunks and
// final text that isn't a Repeat. The transcript keeps final text only.
type Text struct {
	Text    string
	Partial bool
	// Repeat marks final text whose partial chunks were already delivered.
	Repeat bool
	// Thought is the model's reasoning rather than its answer; front ends
	// may show it or not, and the transcript leaves it out.
	Thought bool
}

// ToolCall is the agent calling a tool.
type ToolCall struct {
	ID   string // matches the ToolResult
	Name string
	Args map[string]any
	// Partial: the call arrived in a streamed chunk, and the final event
	// repeats it.
	Partial bool
}

// ToolResult is what a tool returned.
type ToolResult struct {
	ID     string
	Name   string
	Result map[string]any
}

// ErrMaxTurns is returned when a run exceeds its model-call budget.
var ErrMaxTurns = errors.New("maximum turns reached")

// Usage accumulates token counts and estimated cost.
type Usage struct {
	Calls      int
	Input      int64 // prompt tokens, including cached
	Cached     int64
	CacheWrite int64 // prompt tokens written to the cache (part of Input)
	Output     int64 // candidates + thinking tokens
	LastPrompt int64 // prompt size of the latest call: the current context size
	CostUSD    float64
	Priced     bool // false if any call's model had no price
}

// Add adds o's calls, tokens and cost to u; u stays priced only if both are.
func (u *Usage) Add(o Usage) {
	u.Calls += o.Calls
	u.Input += o.Input
	u.Cached += o.Cached
	u.CacheWrite += o.CacheWrite
	u.Output += o.Output
	u.CostUSD += o.CostUSD
	u.Priced = u.Priced && o.Priced
	if o.LastPrompt > 0 {
		u.LastPrompt = o.LastPrompt
	}
}

// InitPrompt asks the agent to write or update BLITZ.md, the project's
// instructions for future sessions (/init, blitz init).
func InitPrompt() string {
	return "Study this repository and write BLITZ.md at the workspace root: instructions for an AI coding agent starting a new session here.\n\n" +
		"Include, briefly and only what you verified in the files:\n" +
		"1. What the project is, in one or two sentences\n" +
		"2. How to build, test (all tests and a single test), lint and run it, with the exact commands from the Makefile, package.json, go.mod, pyproject.toml, CI workflows and the like\n" +
		"3. The layout: the main directories and what lives where\n" +
		"4. Conventions that aren't obvious from the code: style, naming, error handling, commit and review rules, generated files not to edit\n" +
		"5. Gotchas: required services or environment, slow or flaky tests, things that look wrong but are deliberate\n\n" +
		"If BLITZ.md exists, update it instead of replacing it, keeping what is still true. If AGENTS.md or CLAUDE.md already covers something, don't repeat it; refer to it. " +
		"Don't invent commands, don't include secrets or credentials, and don't run the full test suite. Keep it under 150 lines of Markdown."
}

// ErrImagesDisabled is returned when [images] enabled = false (or the image
// store could not be created).
var ErrImagesDisabled = errors.New("image support is disabled")
