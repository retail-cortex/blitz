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
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// cmdReasoning is /reasoning [turn|turns] [--full]: a turn's reasoning
// graph as a tree (spec_067_cli_parity CLP-47, spec_066_reasoning).
func cmdReasoning(app *App, args []string) {
	active, ok := app.Workspace.ActiveSession()
	if !ok {
		fmt.Printf("%s✗ %s%s\n", Red, i18n.T("session.none_active"), Reset)
		return
	}
	turn, full, list := -1, false, false
	for _, a := range args {
		switch {
		case a == "--full":
			full = true
		case a == "turns":
			list = true
		default:
			n, err := strconv.Atoi(a)
			if err != nil || n < 1 {
				fmt.Printf("%s%s%s\n", Yellow, i18n.T("reasoning.usage"), Reset)
				return
			}
			turn = n - 1
		}
	}
	r, err := app.Workspace.Reasoning(active.ID, turn)
	if err != nil {
		fmt.Printf("%s✗ %s%s\n", Red, safe(err.Error()), Reset)
		return
	}
	if list {
		PrintReasoningTurns(os.Stdout, r)
		return
	}
	PrintReasoning(os.Stdout, r, full)
}

// PrintReasoningTurns lists a session's turns: each one's number, prompt,
// calls, steps of reasoning and cost.
func PrintReasoningTurns(out io.Writer, r api.Reasoning) {
	if len(r.Turns) == 0 {
		fmt.Fprintf(out, "%s%s%s\n", Dim, i18n.T("reasoning.none"), Reset)
		return
	}
	fmt.Fprintln(out)
	for _, t := range r.Turns {
		fmt.Fprintf(out, "  %s%3d%s  %s  %s%s%s\n", Bold, t.Index+1, Reset, safe(clipLine(t.Prompt, 70)), Dim, turnFacts(t.Calls, t.Thoughts, t.Usage), Reset)
	}
	fmt.Fprintf(out, "\n  %s%s%s\n\n", Dim, i18n.T("reasoning.pick"), Reset)
}

// PrintReasoning draws one turn's graph as a tree: its calls with their
// model, tokens, time and cost; under each, its steps of reasoning, its
// tools (✓ or ✗) and sub-agents (their calls nested), and its words. full
// prints each node's text too.
func PrintReasoning(out io.Writer, r api.Reasoning, full bool) {
	if len(r.Turns) == 0 || len(r.Nodes) == 0 {
		fmt.Fprintf(out, "%s%s%s\n", Dim, i18n.T("reasoning.none"), Reset)
		return
	}
	idx := min(max(r.Turn, 0), len(r.Turns)-1)
	t := r.Turns[idx]
	fmt.Fprintf(out, "\n%s%s%s  %s\n", Bold, i18n.T("reasoning.turn", "n", strconv.Itoa(idx+1), "of", strconv.Itoa(len(r.Turns))), Reset, safe(clipLine(t.Prompt, 80)))
	fmt.Fprintf(out, "%s%s%s\n", Dim, turnFacts(t.Calls, t.Thoughts, t.Usage), Reset)
	children := map[string][]api.ReasoningNode{}
	var root string
	for _, n := range r.Nodes {
		if n.Kind == "turn" && root == "" {
			root = n.ID
			continue
		}
		children[n.Parent] = append(children[n.Parent], n)
	}
	var draw func(parent, indent string)
	draw = func(parent, indent string) {
		kids := children[parent]
		for i, n := range kids {
			last := i == len(kids)-1
			branch, next := "├─ ", "│  "
			if last {
				branch, next = "└─ ", "   "
			}
			fmt.Fprintf(out, "%s%s%s\n", indent, branch, nodeLine(n))
			if full && n.Text != "" && n.Kind != "call" {
				for _, l := range strings.Split(strings.TrimSpace(n.Text), "\n") {
					fmt.Fprintf(out, "%s%s  %s%s%s\n", indent, next, Dim, safe(l), Reset)
				}
			}
			draw(n.ID, indent+next)
		}
	}
	draw(root, "")
	if r.Cut {
		fmt.Fprintf(out, "%s%s%s\n", Yellow, i18n.T("reasoning.cut"), Reset)
	}
	hint := i18n.T("reasoning.more")
	if full {
		hint = i18n.T("reasoning.less")
	}
	fmt.Fprintf(out, "\n%s%s%s\n\n", Dim, hint, Reset)
}

// nodeLine is a node as one line of the tree.
func nodeLine(n api.ReasoningNode) string {
	switch n.Kind {
	case "call":
		model := n.Model
		if n.Author != "" && n.Author != "blitz" {
			model = n.Author + " · " + model
		}
		facts := []string{}
		if n.Usage != nil {
			facts = append(facts, humanTokens(n.Usage.Input)+" in", humanTokens(n.Usage.Output)+" out")
			if n.Usage.Priced && n.Usage.CostUSD > 0 {
				facts = append(facts, fmt.Sprintf("$%.4f", n.Usage.CostUSD))
			}
		}
		if n.Duration > 0 {
			facts = append(facts, shortDuration(n.Duration))
		}
		hidden := ""
		if n.Hidden {
			hidden = Dim + " · " + i18n.T("reasoning.hidden") + Reset
		}
		line := Bold + safe(model) + Reset
		if len(facts) > 0 {
			line += " " + Dim + strings.Join(facts, " · ") + Reset
		}
		return line + hidden
	case "thought":
		return Magenta + "◆ " + Reset + safe(clipLine(n.Title, 90))
	case "tool":
		mark, color := "✓", Green
		switch n.Status {
		case api.NodeFailed:
			mark, color = "✗", Red
		case api.NodeRunning:
			mark, color = "…", Yellow
		}
		args := ""
		if len(n.Args) > 0 {
			args = " " + Dim + safe(clipLine(argLine(n.Args), 70)) + Reset
		}
		d := ""
		if n.Duration > 0 {
			d = Dim + " · " + shortDuration(n.Duration) + Reset
		}
		return fmt.Sprintf("%s%s%s %s%s%s", color, mark, Reset, safe(n.Tool), args, d)
	case "agent":
		return Cyan + "▸ " + Reset + Bold + safe(n.Title) + Reset
	case "text":
		return Dim + "“" + Reset + safe(clipLine(n.Text, 90)) + Dim + "”" + Reset
	}
	return safe(clipLine(n.Title, 90))
}

// argLine is a tool's arguments in a line: key=value, sorted.
func argLine(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+clipLine(fmt.Sprint(args[k]), 40))
	}
	return strings.Join(parts, " ")
}

// turnFacts is a turn's calls, steps of reasoning and cost, in words.
func turnFacts(calls, thoughts int, u api.Usage) string {
	facts := []string{i18n.N("reasoning.calls", calls), i18n.N("reasoning.steps", thoughts)}
	if u.Priced && u.CostUSD > 0 {
		facts = append(facts, fmt.Sprintf("$%.4f", u.CostUSD))
	}
	return strings.Join(facts, " · ")
}

// clipLine is s on one line, at most n runes.
func clipLine(s string, n int) string { return clip(oneLine(s), n) }

// shortDuration is d in a word: 850ms, 2.1s, 3m4s.
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
	case d < time.Minute:
		return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + "s"
	}
	return d.Round(time.Second).String()
}
