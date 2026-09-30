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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/retail-cortex/blitz/pkg/images"
	"golang.org/x/term"
)

// Output formats for non-interactive runs.
const (
	formatText       = "text"
	formatJSON       = "json"        // one result object at the end
	formatStreamJSON = "stream-json" // one JSON object per event, then the result
)

type usageJSON struct {
	InputTokens  int64 `json:"input_tokens"`
	CachedTokens int64 `json:"cached_input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	ModelCalls   int   `json:"model_calls"`
}

type toolCallJSON struct {
	Name   string         `json:"name"`
	Args   map[string]any `json:"args,omitempty"`
	Result map[string]any `json:"result,omitempty"`
}

// runResult is the final JSON object for json and stream-json output.
type runResult struct {
	Type       string         `json:"type"`
	SessionID  string         `json:"session_id"`
	Result     string         `json:"result"`
	IsError    bool           `json:"is_error"`
	Error      string         `json:"error,omitempty"`
	ExitCode   int            `json:"exit_code"`
	DurationMs int64          `json:"duration_ms"`
	Usage      usageJSON      `json:"usage"`
	CostUSD    *float64       `json:"cost_usd,omitempty"`
	ToolCalls  []toolCallJSON `json:"tool_calls,omitempty"`
}

type oneShotOptions struct {
	prompt     string
	sessionID  string
	format     string
	maxTurns   int
	sendPrompt string        // sent instead of prompt, which is recorded
	maxCostUSD float64       // --max-cost-usd (0: unlimited)
	timeout    time.Duration // --timeout (0: unlimited)
	plan       bool          // --plan: read-only tools, answer with a plan
	input      tui.Input
	stdinTTY   bool
	stdout     io.Writer
	markdown   bool
	spinner    bool
	width      int
	usageLines bool
	images     []*images.Image
	theme      string // Markdown theme
}

// runOneShot executes a single prompt and exits. Background processes are
// never left behind: the user is asked (text mode on a terminal) or they are
// killed.
func runOneShot(ctx context.Context, w api.Backend, o oneShotOptions) error {
	start := time.Now()
	out := o.stdout
	sid := o.sessionID

	var handler func(api.Event)
	var printer *tui.Printer
	var calls []toolCallJSON
	var enc *json.Encoder
	switch o.format {
	case formatText:
		printer = tui.NewPrinter(tui.PrinterOptions{Out: out, Markdown: o.markdown, Theme: o.theme, Width: o.width, Spinner: o.spinner})
		handler = printer.Handle
	case formatStreamJSON:
		enc = json.NewEncoder(out)
		handler = streamJSONHandler(enc)
	default:
		handler = collectHandler(&calls)
	}
	turn, runErr := w.Run(ctx, sid, api.Turn{
		Text: o.prompt, Prompt: o.sendPrompt, Plan: o.plan, Images: o.images, MaxTurns: o.maxTurns, MaxCostUSD: o.maxCostUSD, Timeout: o.timeout,
		OnAccepted: func() {
			if printer != nil {
				printer.Begin()
			}
			if enc != nil {
				_ = enc.Encode(map[string]any{"type": "session", "session_id": sid, "model": w.Model().Name, "agent": w.ActiveAgent().Name})
			}
		},
	}, handler)
	var blocked *api.BlockedError
	if errors.As(runErr, &blocked) {
		runErr = withCode(exitBlocked, runErr)
	} else if printer != nil {
		printer.End()
		fmt.Fprintln(out)
	}
	if ctx.Err() != nil && runErr != nil && !api.IsLimit(runErr) {
		runErr = withCode(exitInterrupted, runErr)
	}

	usage, _ := w.SessionUsage() // sid is the active session
	if o.format == formatText {
		if o.usageLines {
			if line := tui.UsageLine(api.Usage{}, usage); line != "" {
				fmt.Fprintf(os.Stderr, "%s%s%s\n", tui.Dim, line, tui.Reset)
			}
		}
	} else {
		res := runResult{
			Type: "result", SessionID: sid, Result: turn.Output,
			DurationMs: time.Since(start).Milliseconds(),
			Usage:      usageJSON{InputTokens: usage.Input, CachedTokens: usage.Cached, OutputTokens: usage.Output, ModelCalls: usage.Calls},
			ToolCalls:  calls,
		}
		if usage.Priced && usage.Calls > 0 {
			cost := usage.CostUSD
			res.CostUSD = &cost
		}
		if runErr != nil {
			res.IsError, res.Error = true, runErr.Error()
		}
		res.ExitCode = exitCodeFor(runErr)
		_ = json.NewEncoder(out).Encode(res)
	}

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	tui.ConfirmExit(context.Background(), o.input, w.Processes(), interrupts, tui.ExitPrompt{
		CanPrompt: o.format == formatText && ctx.Err() == nil && o.stdinTTY && o.input != nil,
		Tasks:     w,
	})
	return runErr
}

// streamJSONHandler writes one JSON line per event.
func streamJSONHandler(enc *json.Encoder) func(api.Event) {
	return func(ev api.Event) {
		switch {
		case ev.Text != nil && !ev.Text.Thought:
			_ = enc.Encode(map[string]any{"type": "text", "text": ev.Text.Text, "partial": ev.Text.Partial, "author": ev.Author})
		case ev.ToolCall != nil:
			_ = enc.Encode(map[string]any{"type": "tool_call", "name": ev.ToolCall.Name, "args": ev.ToolCall.Args})
		case ev.ToolResult != nil:
			_ = enc.Encode(map[string]any{"type": "tool_result", "name": ev.ToolResult.Name, "result": ev.ToolResult.Result})
		}
	}
}

// collectHandler gathers tool calls for json output.
func collectHandler(calls *[]toolCallJSON) func(api.Event) {
	pending := map[string]int{}
	return func(ev api.Event) {
		switch {
		case ev.ToolCall != nil && !ev.ToolCall.Partial:
			pending[ev.ToolCall.ID+ev.ToolCall.Name] = len(*calls)
			*calls = append(*calls, toolCallJSON{Name: ev.ToolCall.Name, Args: ev.ToolCall.Args})
		case ev.ToolResult != nil:
			if i, ok := pending[ev.ToolResult.ID+ev.ToolResult.Name]; ok {
				(*calls)[i].Result = ev.ToolResult.Result
			}
		}
	}
}

func stdoutIsTerminal() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

func terminalWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		return w
	}
	return 100
}
