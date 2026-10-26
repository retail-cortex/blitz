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

package tray

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/textutil"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// The tray's Processes submenu (spec_electron_038 EL-112): the background
// processes the agents started and that still run, in every open
// workspace, each with its workspace and Stop, so the person sees what's
// left running and can end it.

// processTitle is at most this long: a command is cut to fit the menu.
const processTitle = 60

// ProcessesTitle is the submenu's own item: how many are running, and
// how many of them look bad (spec_processes_052).
func ProcessesTitle(procs []*pb.WorkspaceProcess) string {
	flagged := 0
	for _, wp := range procs {
		if len(wp.GetProcess().GetFlags()) > 0 {
			flagged++
		}
	}
	switch {
	case len(procs) == 0:
		return i18n.T("tray.processes")
	case flagged > 0:
		return i18n.N("tray.processes.flagged", len(procs), "flagged", flagged)
	}
	return i18n.N("tray.processes.count", len(procs))
}

// processState is what a process uses and why it looks bad, on a line:
// "Busy, left running · 98% CPU · 310 MB"; "" when there's nothing to say.
func processState(p *pb.Process) string {
	var parts []string
	if len(p.Flags) > 0 {
		why := make([]string, 0, len(p.Flags))
		for _, f := range p.Flags {
			why = append(why, i18n.T("tray.process.flag."+f))
		}
		parts = append(parts, strings.Join(why, ", "))
	}
	if p.Measured {
		mem := fmt.Sprintf("%d MB", p.MemoryBytes>>20)
		if p.MemoryBytes >= 1<<30 {
			mem = fmt.Sprintf("%.1f GB", float64(p.MemoryBytes)/(1<<30))
		}
		parts = append(parts, i18n.T("tray.process.usage", "percent", fmt.Sprintf("%.0f", p.CpuPercent), "memory", mem))
	}
	return strings.Join(parts, " · ")
}

// ProcessesLink opens the desktop app's processes view on a workspace's
// process.
func ProcessesLink(workspace string, id int) string {
	q := url.Values{"dir": {workspace}}
	if id > 0 {
		q.Set("id", strconv.Itoa(id))
	}
	return "blitz://processes?" + q.Encode()
}

// ProcessEntries are the Processes submenu: each running process, its
// command (marked when it looks bad), under it its workspace (named as
// spaces name it), what it uses and why it looks bad, Show in Blitz and
// Stop; none, a line saying so.
func ProcessEntries(procs []*pb.WorkspaceProcess, spaces []Space) []Entry {
	if len(procs) == 0 {
		return []Entry{{Title: i18n.T("tray.processes.none"), Disabled: true}}
	}
	names := map[string]string{}
	for _, s := range spaces {
		names[s.Dir] = s.Name
	}
	out := make([]Entry, 0, len(procs))
	for _, wp := range procs {
		p := wp.Process
		if p == nil {
			continue
		}
		name := names[wp.Workspace]
		if name == "" {
			name = filepath.Base(wp.Workspace)
		}
		dot := DotRunning
		if len(p.Flags) > 0 {
			dot = DotFailed
		}
		children := []Entry{{Title: i18n.T("tray.process.in", "workspace", name, "id", p.Id), Icon: IconFolder, Disabled: true}}
		if state := processState(p); state != "" {
			children = append(children, Entry{Title: state, Disabled: true})
		}
		children = append(children,
			Entry{Title: i18n.T("tray.process.show"), Icon: IconEye, Action: Action{Kind: ShowProcess, Workspace: wp.Workspace, ID: int(p.Id)}},
			Entry{Title: i18n.T("tray.stop_run"), Icon: IconStop, Action: Action{Kind: StopProcess, Workspace: wp.Workspace, ID: int(p.Id)}},
		)
		out = append(out, Entry{Title: textutil.Ellipsize(p.Command, processTitle), Icon: dot, Children: children})
	}
	return out
}
