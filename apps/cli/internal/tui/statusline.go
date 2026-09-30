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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/textutil"
	"golang.org/x/term"
)

// The status line (spec_parity_027 PAR-UI-08): a dim line shown above each
// prompt, from ui.status_line: "default" for the built-in one, or a
// command that reads the session's state as JSON on stdin and whose first
// line of output is shown. Above, not under, the prompt: the line editor
// clears what is below it as you type.

// statusLineTimeout bounds a status line command.
var statusLineTimeout = 2 * time.Second

// StatusState is what a status line command reads on stdin.
type StatusState struct {
	Version        string  `json:"version"`
	Workspace      string  `json:"workspace"`
	Attached       bool    `json:"attached"`
	SessionID      string  `json:"session_id,omitempty"`
	SessionName    string  `json:"session_name,omitempty"`
	Agent          string  `json:"agent"`
	Model          string  `json:"model"`
	Provider       string  `json:"provider,omitempty"`
	Mode           string  `json:"permission_mode"`
	Effort         string  `json:"effort,omitempty"`
	Style          string  `json:"style,omitempty"`
	ContextTokens  int64   `json:"context_tokens"`
	ContextPercent float64 `json:"context_percent"` // of the compaction threshold; 0 without one
	CostUSD        float64 `json:"cost_usd"`
	Priced         bool    `json:"priced"`
}

// statusState gathers the session's state.
func statusState(app *App) StatusState {
	st := app.Workspace.Settings()
	s := StatusState{
		Version: app.Version, Workspace: app.Workspace.Dir(), Attached: app.Attached,
		Agent: st.Agent, Model: st.Model.Name, Provider: st.Model.Provider, Mode: st.PermissionMode,
		Effort: st.Effort, Style: st.Style,
	}
	if a, ok := app.Workspace.ActiveSession(); ok {
		s.SessionID, s.SessionName = a.ID, a.Title
	}
	if c, err := app.Workspace.Context(false); err == nil {
		s.ContextTokens = c.Tokens
		if c.Threshold > 0 {
			s.ContextPercent = float64(c.Tokens) / float64(c.Threshold) * 100
		}
	}
	if u, err := app.Workspace.SessionUsage(); err == nil {
		s.CostUSD, s.Priced = u.CostUSD, u.Priced
	}
	return s
}

// defaultStatusLine is the built-in status line: model, mode, context and
// cost.
func defaultStatusLine(s StatusState) string {
	parts := []string{s.Model, s.Mode}
	if s.ContextTokens > 0 {
		ctx := i18n.T("statusline.context", "tokens", humanTokens(s.ContextTokens))
		if s.ContextPercent > 0 {
			ctx += fmt.Sprintf(" (%.0f%%)", s.ContextPercent)
		}
		parts = append(parts, ctx)
	}
	if s.Priced && s.CostUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", s.CostUSD))
	}
	return strings.Join(parts, " · ")
}

// statusLine is the line to show above the prompt ("" for none).
func statusLine(ctx context.Context, app *App) string {
	cmd := strings.TrimSpace(app.StatusLine)
	if cmd == "" {
		return ""
	}
	s := statusState(app)
	if cmd == "default" {
		return defaultStatusLine(s)
	}
	in, _ := json.Marshal(s)
	ctx, cancel := context.WithTimeout(ctx, statusLineTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	c.Dir = app.Workspace.Dir()
	c.Stdin = bytes.NewReader(in)
	c.WaitDelay = time.Second
	out, err := c.Output()
	if err != nil && len(out) == 0 {
		return i18n.T("statusline.failed", "error", err)
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return textutil.SanitizeTerminal(strings.TrimRight(line, "\r\n"))
}

// printStatusLine shows the status line, cut to the terminal's width.
func printStatusLine(ctx context.Context, app *App) {
	line := statusLine(ctx, app)
	if line == "" {
		return
	}
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 2 && len([]rune(line)) > w-1 {
		line = string([]rune(line)[:w-2]) + "…"
	}
	fmt.Printf("%s%s%s\n", Dim, line, Reset)
}
