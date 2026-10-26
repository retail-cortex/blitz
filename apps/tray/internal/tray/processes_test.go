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
	"strings"
	"testing"

	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Processes submenu (EL-112): each running process with its
// workspace and Stop, named as the app names the workspace; a line when
// there are none; its title counts them.
func TestProcessEntries(t *testing.T) {
	none := ProcessEntries(nil, nil)
	require.Len(t, none, 1)
	assert.Equal(t, "No processes running", none[0].Title)
	assert.True(t, none[0].Disabled)

	long := "python -m http.server " + strings.Repeat("x", 80)
	procs := []*pb.WorkspaceProcess{
		{Workspace: "/w/shop", Process: &pb.Process{Id: 3, Command: "npm run dev"}},
		{Workspace: "/w/blog", Process: &pb.Process{Id: 1, Command: long, Measured: true, CpuPercent: 4.2, MemoryBytes: 120 << 20}},
		{Workspace: "/w/shop", Process: &pb.Process{Id: 4, Command: "node spin.js", Measured: true, CpuPercent: 99.5, MemoryBytes: 3 << 30, Flags: []string{"cpu", "left"}}},
		{Workspace: "/w/gone"},
	}
	got := ProcessEntries(procs, []Space{{Dir: "/w/shop", Name: "My shop"}})
	require.Len(t, got, 3, "one without a process is skipped")
	assert.Equal(t, Entry{Title: "npm run dev", Icon: DotRunning, Children: []Entry{
		{Title: "In My shop (#3)", Icon: IconFolder, Disabled: true},
		{Title: "Show in Blitz", Icon: IconEye, Action: Action{Kind: ShowProcess, Workspace: "/w/shop", ID: 3}},
		{Title: "Stop", Icon: IconStop, Action: Action{Kind: StopProcess, Workspace: "/w/shop", ID: 3}},
	}}, got[0], "not measured: nothing to say of its use")
	assert.LessOrEqual(t, len([]rune(got[1].Title)), processTitle, "cut to fit")
	assert.Equal(t, "In blog (#1)", got[1].Children[0].Title, "a workspace the app hasn't open: its folder's name")
	assert.Equal(t, Entry{Title: "4% CPU · 120 MB", Disabled: true}, got[1].Children[1], "what it uses")
	assert.Equal(t, DotFailed, got[2].Icon, "flagged")
	assert.Equal(t, "Busy, Left running · 100% CPU · 3.0 GB", got[2].Children[1].Title, "why")

	tests := map[string]struct {
		procs []*pb.WorkspaceProcess
		want  string
	}{
		"none":         {want: "Processes"},
		"one":          {procs: procs[:1], want: "Processes (1 running)"},
		"two":          {procs: procs[:2], want: "Processes (2 running)"},
		"some flagged": {procs: procs[:3], want: "Processes (3 running, 1 flagged)"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) { assert.Equal(t, tt.want, ProcessesTitle(tt.procs)) })
	}
	assert.Equal(t, "blitz://processes?dir=%2Fw%2Fshop&id=3", ProcessesLink("/w/shop", 3))
	assert.Equal(t, "blitz://processes?dir=%2Fw%2Fshop", ProcessesLink("/w/shop", 0))
}
