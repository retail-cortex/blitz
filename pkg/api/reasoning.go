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

import "time"

// The kinds of node in a turn's reasoning graph (spec_066_reasoning).
const (
	// NodeTurn is the turn: its prompt, and its totals.
	NodeTurn = "turn"
	// NodeCall is one model call: who answered, its tokens and cost.
	NodeCall = "call"
	// NodeThought is a step of the model's reasoning, as it shared it.
	NodeThought = "thought"
	// NodeTool is a tool the model called, with its result.
	NodeTool = "tool"
	// NodeAgent is a sub-agent the model asked (invoke_agent): its own
	// calls are its children.
	NodeAgent = "agent"
	// NodeText is the model's words: its answer, or what it said between
	// tools.
	NodeText = "text"
)

// A reasoning node's status.
const (
	NodeOK      = "ok"
	NodeFailed  = "failed"
	NodeRunning = "running"
)

// ReasoningNode is one node of a turn's reasoning graph. Parent is the ID
// of the node it hangs from ("" for the turn).
type ReasoningNode struct {
	ID, Parent string
	Kind       string
	// Title is its short label; Text its whole text (a step of the
	// reasoning, an answer, the prompt), cut at MaxReasoningText.
	Title, Text string
	// Author is the agent whose call it is (a sub-agent's name inside one).
	Author string
	// Provider and Model answered a call.
	Provider, Model string
	// Tool, Args and Result are a tool's call and what it returned.
	Tool         string
	Args, Result map[string]any
	Status       string
	Start        time.Time
	Duration     time.Duration
	// Usage is a call's tokens and estimated cost, or a turn's or a
	// sub-agent's total.
	Usage *Usage
	// Hidden marks a call that reasoned (Usage.Reasoning) without sharing
	// its reasoning: the provider keeps it to itself.
	Hidden bool
}

// ReasoningTurn sums up one turn of a session, for the turn picker.
type ReasoningTurn struct {
	Index  int
	Prompt string
	Start  time.Time
	Calls  int
	// Thoughts counts the steps of reasoning the model shared.
	Thoughts int
	Usage    Usage
}

// Reasoning is a session's turns and the graph of one of them.
type Reasoning struct {
	Turns []ReasoningTurn
	// Turn is the index of the turn Nodes draw.
	Turn  int
	Nodes []ReasoningNode
	// Cut says the turn had more nodes than MaxReasoningNodes: the rest
	// aren't drawn.
	Cut bool
}

// MaxReasoningText bounds each node's text, and MaxReasoningNodes a
// turn's graph.
const (
	MaxReasoningText  = 16 << 10
	MaxReasoningNodes = 1500
)
