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

package tui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/retail-cortex/blitz/pkg/api"
)

func sampleReasoning() api.Reasoning {
	u := &api.Usage{Input: 12_400, Output: 830, CostUSD: 0.0031, Priced: true}
	return api.Reasoning{
		Turns: []api.ReasoningTurn{{Index: 0, Prompt: "fix the failing test", Calls: 2, Thoughts: 2, Usage: api.Usage{CostUSD: 0.004, Priced: true}}},
		Turn:  0,
		Nodes: []api.ReasoningNode{
			{ID: "t", Kind: "turn", Title: "fix the failing test"},
			{ID: "c1", Parent: "t", Kind: "call", Model: "gemini-flash", Author: "blitz", Usage: u, Duration: 2100 * time.Millisecond},
			{ID: "th1", Parent: "c1", Kind: "thought", Title: "Finding the test", Text: "It's in pkg/x.\nThe assertion is wrong."},
			{ID: "tool1", Parent: "c1", Kind: "tool", Tool: "run_shell_command", Args: map[string]any{"command": "go test ./..."}, Status: api.NodeFailed, Duration: 1300 * time.Millisecond},
			{ID: "a1", Parent: "c1", Kind: "agent", Title: "reviewer"},
			{ID: "c2", Parent: "a1", Kind: "call", Model: "claude", Author: "reviewer", Hidden: true},
			{ID: "x", Parent: "c2", Kind: "text", Text: "Looks right now."},
		},
	}
}

// /reasoning draws a turn as a tree (CLP-47).
func TestPrintReasoning(t *testing.T) {
	var b bytes.Buffer
	PrintReasoning(&b, sampleReasoning(), false)
	out := stripANSI(b.String())
	for _, want := range []string{
		"Turn 1 of 1  fix the failing test", "2 calls · 2 steps · $0.0040",
		"└─ gemini-flash 12.4k in · 830 out · $0.0031 · 2.1s",
		"   ├─ ◆ Finding the test", "✗ run_shell_command command=go test ./... · 1.3s",
		"└─ ▸ reviewer", "reviewer · claude · reasoned, kept by the provider", "“Looks right now.”", "--full shows",
	} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "The assertion is wrong", "the text only with --full")
	b.Reset()
	PrintReasoning(&b, sampleReasoning(), true)
	assert.Contains(t, stripANSI(b.String()), "The assertion is wrong")
	b.Reset()
	PrintReasoning(&b, api.Reasoning{}, false)
	assert.Contains(t, b.String(), "No turn to show")
	b.Reset()
	PrintReasoningTurns(&b, sampleReasoning())
	assert.Contains(t, stripANSI(b.String()), "  1  fix the failing test  2 calls")
}

func TestReasoningCommand(t *testing.T) {
	app := newTestApp(t, nil)
	_, _ = app.Workspace.NewSession()
	out := stripANSI(captureStdout(t, func() { HandleCommand(context.Background(), "/reasoning", app) }))
	assert.Contains(t, out, "No turn to show")
	out = stripANSI(captureStdout(t, func() { HandleCommand(context.Background(), "/reasoning zero", app) }))
	assert.Contains(t, out, "/reasoning [<turn>] [--full]")
	assert.True(t, strings.Contains(shortDuration(850*time.Millisecond), "850ms"))
	assert.Equal(t, "3m4s", shortDuration(184*time.Second))
}

// /reasoning turns lists them; a turn by number and --full come back from
// the workspace.
func TestReasoningCommandArgs(t *testing.T) {
	app, s := stubApp(t, "")
	_, _ = app.Workspace.NewSession()
	var asked []int
	s.reasoning = func(_ string, turn int) (api.Reasoning, error) {
		asked = append(asked, turn)
		return sampleReasoning(), nil
	}
	out := stripANSI(captureStdout(t, func() { HandleCommand(context.Background(), "/reasoning turns", app) }))
	assert.Contains(t, out, "fix the failing test")
	out = stripANSI(captureStdout(t, func() { HandleCommand(context.Background(), "/reasoning 1 --full", app) }))
	assert.Contains(t, out, "The assertion is wrong")
	assert.Equal(t, []int{-1, 0}, asked)
	s.reasoning = func(string, int) (api.Reasoning, error) { return api.Reasoning{}, errors.New("no log") }
	assert.Contains(t, stripANSI(captureStdout(t, func() { HandleCommand(context.Background(), "/reasoning", app) })), "no log")
	app2, _ := stubApp(t, "")
	assert.Contains(t, stripANSI(captureStdout(t, func() { cmdReasoning(app2, nil) })), "✗")
}
