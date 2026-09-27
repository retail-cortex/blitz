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
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// ANSI color codes
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
	BgBlack = "\033[40m"
	BgBlue  = "\033[44m"
)

// PrintBanner renders the Blitz ASCII splash banner.
// Product is the product's name, which isn't translated.
const Product = "Blitz"

func PrintBanner(version, agent, model string) {
	fmt.Printf("%s%s%s %s  %s%s %s · %s %s%s\n", Bold, Product, Reset, version, Dim, i18n.T("banner.agent"), agent, i18n.T("banner.model"), model, Reset)
	fmt.Printf("%s%s%s\n\n", Dim, i18n.T("banner.hint", "help", "/help", "key", "Ctrl+C"), Reset)
}

// FormatDiff highlights diff additions in green and deletions in red.
func FormatDiff(diffText string) string {
	lines := strings.Split(diffText, "\n")
	var sb strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			sb.WriteString(Green + line + Reset + "\n")
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			sb.WriteString(Red + line + Reset + "\n")
		} else if strings.HasPrefix(line, "@@") {
			sb.WriteString(Cyan + line + Reset + "\n")
		} else {
			sb.WriteString(line + "\n")
		}
	}

	return sb.String()
}

// safe prepares untrusted text (model output, tool args/results) for the
// terminal by removing control sequences.
func safe(s string) string { return textutil.SanitizeTerminal(s) }

// PrintModelText writes streamed model text with control sequences removed.
func PrintModelText(text string) { fmt.Print(safe(text)) }

// FormatToolCall renders an invocation badge for a tool.
func FormatToolCall(toolName string, args map[string]any) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n%s%s:%s %s%s%s", Yellow, i18n.T("tool.call"), Reset, Bold, safe(toolName), Reset)
	if path, ok := args["path"].(string); ok && path != "" {
		fmt.Fprintf(&sb, " (%s%s%s)", Cyan, safe(textutil.Ellipsize(path, 120)), Reset)
	} else if cmd, ok := args["command"].(string); ok && cmd != "" {
		fmt.Fprintf(&sb, " (%s%s%s)", Dim, safe(textutil.Ellipsize(cmd, 60)), Reset)
	} else if q, ok := args["query"].(string); ok && q != "" {
		fmt.Fprintf(&sb, " (%s: %s%s%s)", i18n.T("tool.query"), Cyan, safe(textutil.Ellipsize(q, 80)), Reset)
	}
	sb.WriteByte('\n')
	return sb.String()
}

// PrintToolCall prints FormatToolCall.
func PrintToolCall(toolName string, args map[string]any) {
	fmt.Print(FormatToolCall(toolName, args))
}

// FormatToolResult renders a completed tool badge.
func FormatToolResult(toolName string, success bool, summary string) string {
	icon := "✓"
	color := Green
	if !success {
		icon = "✗"
		color = Red
	}
	toolName = safe(toolName)
	if summary != "" {
		// Collapse to one line so multi-line output cannot spoof other UI.
		summary = strings.Join(strings.Fields(safe(summary)), " ")
		return fmt.Sprintf("%s%s [%s]:%s %s\n", color, icon, toolName, Reset, textutil.Ellipsize(summary, 80))
	}
	return fmt.Sprintf("%s%s [%s] %s%s\n", color, icon, toolName, i18n.T("tool.done"), Reset)
}

// PrintToolResult prints FormatToolResult.
func PrintToolResult(toolName string, success bool, summary string) {
	fmt.Print(FormatToolResult(toolName, success, summary))
}

// SummarizeToolResponse picks a short summary from a function response and
// reports whether it represents success.
func SummarizeToolResponse(resp map[string]any) (summary string, success bool) {
	if resp == nil {
		return "", true
	}
	if errStr, ok := resp["error"].(string); ok && errStr != "" {
		return errStr, false
	}
	if resStr, ok := resp["result"].(string); ok && resStr != "" {
		return resStr, true
	}
	if cnt, ok := resp["content"].(string); ok && cnt != "" {
		return fmt.Sprintf("%d bytes read", len(cnt)), true
	}
	return "", true
}

// FormatTasks renders the agent's task list as a checklist.
func FormatTasks(tasks []api.Task) string {
	var sb strings.Builder
	for _, t := range tasks {
		switch t.Status {
		case "done":
			fmt.Fprintf(&sb, "  %s☒ %s%s\n", Dim, safe(t.Content), Reset)
		case "in_progress":
			fmt.Fprintf(&sb, "  %s☐ %s%s\n", Bold+Cyan, safe(t.Content), Reset)
		default:
			fmt.Fprintf(&sb, "  ☐ %s\n", safe(t.Content))
		}
	}
	return sb.String()
}
