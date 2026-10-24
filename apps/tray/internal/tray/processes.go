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
	"path/filepath"

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

// ProcessesTitle is the submenu's own item: how many are running.
func ProcessesTitle(n int) string {
	if n == 0 {
		return i18n.T("tray.processes")
	}
	return i18n.N("tray.processes.count", n)
}

// ProcessEntries are the Processes submenu: each running process, its
// command, under it its workspace (named as spaces name it) and Stop;
// none, a line saying so.
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
		out = append(out, Entry{Title: textutil.Ellipsize(p.Command, processTitle), Icon: DotRunning, Children: []Entry{
			{Title: i18n.T("tray.process.in", "workspace", name, "id", p.Id), Icon: IconFolder, Disabled: true},
			{Title: i18n.T("tray.stop_run"), Icon: IconStop, Action: Action{Kind: StopProcess, Workspace: wp.Workspace, ID: int(p.Id)}},
		}})
	}
	return out
}
