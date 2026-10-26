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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// The agents' background processes in the REPL (spec_processes_052
// PR-40): /processes lists them with what they use and why one looks
// bad, shows one's output, and stops them, whichever session started
// them.

// processLogLines is how much of a process's output /processes log shows.
const processLogLines = 40

// processes is what /processes needs of the workspace: the service's
// view of the processes (pkg/client.Remote).
type processes interface {
	RunningProcesses(everywhere bool) ([]api.WorkspaceProcess, error)
	StopProcessIn(workspace string, id int) (api.ProcessInfo, error)
	ProcessOutputIn(workspace string, id int) (string, api.ProcessInfo, error)
}

// ProcessLine is a process on one line: "[3] pnpm dev · 4m12s · 98% CPU ·
// 310 MB — busy, left running (idle 2h0m)".
func ProcessLine(p api.ProcessInfo) string {
	parts := []string{
		fmt.Sprintf("[%d] %s", p.ID, textutil.Ellipsize(p.Command, 60)),
		(time.Duration(p.RuntimeMs) * time.Millisecond).Round(time.Second).String(),
	}
	if p.Measured {
		parts = append(parts, i18n.T("processes.cpu", "percent", fmt.Sprintf("%.0f", p.CPUPercent)), formatBytes(p.MemoryBytes))
	}
	line := strings.Join(parts, " · ")
	if len(p.Flags) == 0 {
		return line
	}
	why := make([]string, 0, len(p.Flags))
	for _, f := range p.Flags {
		why = append(why, i18n.T("processes.flag."+f))
	}
	if p.IdleMs > 0 && hasFlag(p, api.ProcessFlagLeft) {
		why[len(why)-1] += " " + i18n.T("processes.idle", "took", (time.Duration(p.IdleMs)*time.Millisecond).Round(time.Minute).String())
	}
	return line + " — " + strings.Join(why, ", ")
}

func hasFlag(p api.ProcessInfo, flag string) bool {
	for _, f := range p.Flags {
		if f == flag {
			return true
		}
	}
	return false
}

// formatBytes is n in MB, or GB from 1 GB.
func formatBytes(n int64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
	return fmt.Sprintf("%d MB", n>>20)
}

// cmdProcesses is /processes: this workspace's processes, every
// workspace's (all), one's output (log), or stopping some (stop: IDs,
// flagged or all, in this workspace).
func cmdProcesses(args []string, app *App) {
	pm, ok := app.Workspace.(processes)
	if !ok {
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("processes.unavailable"), Reset)
		return
	}
	here := app.Workspace.Dir()
	if real, err := filepath.EvalSymlinks(here); err == nil {
		here = real // as the service names it
	}
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch {
	case sub == "" || sub == "all":
		listProcesses(pm, here, sub == "all")
	case sub == "log" && len(args) == 2:
		id, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Printf("%s%s%s\n", Red, i18n.T("processes.usage"), Reset)
			return
		}
		out, p, err := pm.ProcessOutputIn(here, id)
		if err != nil {
			fmt.Printf("%s%s%s\n", Red, err, Reset)
			return
		}
		fmt.Printf("\n%s%s%s\n", Bold, safe(ProcessLine(p)), Reset)
		if out = strings.TrimRight(out, "\n"); out == "" {
			fmt.Printf("  %s%s%s\n\n", Dim, i18n.T("processes.no_output"), Reset)
			return
		}
		lines := strings.Split(out, "\n")
		lines = lines[max(0, len(lines)-processLogLines):]
		for _, l := range lines {
			fmt.Printf("  %s\n", safe(l))
		}
		fmt.Println()
	case sub == "stop" && len(args) >= 2:
		stopProcesses(pm, here, args[1:])
	default:
		fmt.Printf("%s%s%s\n", Red, i18n.T("processes.usage"), Reset)
	}
}

// listProcesses prints the processes running here, or everywhere (by
// workspace), and how many run elsewhere.
func listProcesses(pm processes, here string, everywhere bool) {
	procs, err := pm.RunningProcesses(true)
	if err != nil {
		fmt.Printf("%s%s%s\n", Red, err, Reset)
		return
	}
	var shown []api.WorkspaceProcess
	elsewhere := 0
	for _, wp := range procs {
		if everywhere || wp.Workspace == here {
			shown = append(shown, wp)
		} else {
			elsewhere++
		}
	}
	if len(shown) == 0 {
		fmt.Printf("%s%s%s\n", Dim, i18n.T("processes.none"), Reset)
	} else {
		fmt.Printf("\n%s%s%s\n", Bold, i18n.T("processes.title"), Reset)
		workspace := ""
		for _, wp := range shown {
			if everywhere && wp.Workspace != workspace {
				workspace = wp.Workspace
				fmt.Printf("  %s%s%s\n", Dim, safe(workspace), Reset)
			}
			color := ""
			if len(wp.Process.Flags) > 0 {
				color = Yellow
			}
			fmt.Printf("  %s%s%s\n", color, safe(ProcessLine(wp.Process)), Reset)
		}
		fmt.Println()
	}
	if elsewhere > 0 {
		fmt.Printf("%s%s%s\n", Dim, i18n.N("processes.elsewhere", elsewhere), Reset)
	}
}

// stopProcesses stops what targets name in workspace here: IDs, "flagged"
// or "all".
func stopProcesses(pm processes, here string, targets []string) {
	var ids []int
	if len(targets) == 1 && (targets[0] == "flagged" || targets[0] == "all") {
		procs, err := pm.RunningProcesses(false)
		if err != nil {
			fmt.Printf("%s%s%s\n", Red, err, Reset)
			return
		}
		for _, wp := range procs {
			if targets[0] == "all" || len(wp.Process.Flags) > 0 {
				ids = append(ids, wp.Process.ID)
			}
		}
		if len(ids) == 0 {
			fmt.Printf("%s%s%s\n", Dim, i18n.T("processes.none_to_stop"), Reset)
			return
		}
	} else {
		for _, t := range targets {
			id, err := strconv.Atoi(strings.TrimPrefix(t, "#"))
			if err != nil {
				fmt.Printf("%s%s%s\n", Red, i18n.T("processes.usage"), Reset)
				return
			}
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		p, err := pm.StopProcessIn(here, id)
		if err != nil {
			fmt.Printf("%s%s%s\n", Red, err, Reset)
			continue
		}
		fmt.Println(safe(i18n.T("processes.stopped", "id", p.ID, "command", textutil.Ellipsize(p.Command, 60))))
	}
}
