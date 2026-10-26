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
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessLine(t *testing.T) {
	tests := map[string]struct {
		p    api.ProcessInfo
		want string
	}{
		"not measured": {
			p:    api.ProcessInfo{ID: 2, Command: "npm run dev", RuntimeMs: 61_400},
			want: "[2] npm run dev · 1m1s",
		},
		"measured": {
			p:    api.ProcessInfo{ID: 3, Command: "vite", RuntimeMs: 5000, Measured: true, CPUPercent: 12.4, MemoryBytes: 310 << 20},
			want: "[3] vite · 5s · 12% CPU · 310 MB",
		},
		"flagged, with gigabytes": {
			p: api.ProcessInfo{
				ID: 4, Command: "node leak.js", RuntimeMs: 1000, Measured: true, CPUPercent: 99.6, MemoryBytes: 3 << 30,
				Flags: []string{api.ProcessFlagCPU, api.ProcessFlagMemory, api.ProcessFlagLeft}, IdleMs: (2 * time.Hour).Milliseconds(),
			},
			want: "[4] node leak.js · 1s · 100% CPU · 3.0 GB — busy, memory, left running (idle 2h0m0s)",
		},
		"left, its session deleted": {
			p:    api.ProcessInfo{ID: 5, Command: "sleep 99", Flags: []string{api.ProcessFlagLeft}},
			want: "[5] sleep 99 · 0s — left running",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, ProcessLine(tt.p))
		})
	}
}

// procRig is an app whose workspace has two processes running (one
// flagged) and another workspace one.
func procRig(t *testing.T) (*App, *stubBackend, *[]string) {
	t.Helper()
	app, s := stubApp(t, "")
	here := app.Workspace.Dir()
	if real, err := filepath.EvalSymlinks(here); err == nil {
		here = real
	}
	procs := []api.WorkspaceProcess{
		{Workspace: here, Process: api.ProcessInfo{ID: 1, Command: "pnpm dev", Running: true}},
		{Workspace: here, Process: api.ProcessInfo{ID: 2, Command: "node spin.js", Running: true, Flags: []string{api.ProcessFlagCPU}}},
		{Workspace: "/elsewhere", Process: api.ProcessInfo{ID: 7, Command: "uv run api", Running: true}},
	}
	s.runningProcs = func(everywhere bool) ([]api.WorkspaceProcess, error) {
		if everywhere {
			return procs, nil
		}
		return procs[:2], nil
	}
	var stopped []string
	s.stopProcIn = func(workspace string, id int) (api.ProcessInfo, error) {
		if id == 99 {
			return api.ProcessInfo{}, api.ErrUnknownProcess
		}
		stopped = append(stopped, filepath.Base(workspace)+"#"+string(rune('0'+id)))
		return api.ProcessInfo{ID: id, Command: "cmd"}, nil
	}
	s.procOutputIn = func(_ string, id int) (string, api.ProcessInfo, error) {
		if id == 99 {
			return "", api.ProcessInfo{}, api.ErrUnknownProcess
		}
		if id == 2 {
			return "", api.ProcessInfo{ID: 2, Command: "node spin.js"}, nil
		}
		var b strings.Builder
		for i := range 50 {
			b.WriteString("line ")
			b.WriteRune(rune('A' + i%26))
			b.WriteString("\n")
		}
		return b.String(), api.ProcessInfo{ID: id, Command: "pnpm dev"}, nil
	}
	return app, s, &stopped
}

func TestProcessesCommand(t *testing.T) {
	tests := map[string]struct {
		line    string
		want    []string
		notWant []string
		stopped []string
	}{
		"this workspace's": {
			line:    "/processes",
			want:    []string{"Background processes:", "[1] pnpm dev", "[2] node spin.js", "busy", "1 more in another workspace: /processes all"},
			notWant: []string{"uv run api"},
		},
		"the alias": {line: "/ps", want: []string{"[1] pnpm dev"}},
		"every workspace's": {
			line:    "/processes all",
			want:    []string{"/elsewhere", "[7] uv run api", "[1] pnpm dev"},
			notWant: []string{"more in another"},
		},
		"a log's last lines":   {line: "/processes log 1", want: []string{"[1] pnpm dev", "  line K\n", "  line X\n\n"}},
		"a log with no output": {line: "/processes log 2", want: []string{"(no output yet)"}},
		"an unknown log":       {line: "/processes log 99", want: []string{api.ErrUnknownProcess.Error()}},
		"a log needs a number": {line: "/processes log x", want: []string{"Use /processes"}},
		"stop by ID":           {line: "/processes stop 1 #2", want: []string{"Stopped [1]", "Stopped [2]"}, stopped: []string{"1", "2"}},
		"stop flagged":         {line: "/processes stop flagged", want: []string{"Stopped [2]"}, stopped: []string{"2"}},
		"stop all here":        {line: "/processes stop all", stopped: []string{"1", "2"}},
		"stop an unknown one":  {line: "/processes stop 99", want: []string{api.ErrUnknownProcess.Error()}},
		"stop needs numbers":   {line: "/processes stop one", want: []string{"Use /processes"}},
		"something else":       {line: "/processes frob", want: []string{"Use /processes"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			app, _, stopped := procRig(t)
			out := runCmd(t, app, tt.line)
			for _, w := range tt.want {
				assert.Contains(t, out, w)
			}
			for _, w := range tt.notWant {
				assert.NotContains(t, out, w)
			}
			var ids []string
			for _, s := range *stopped {
				ids = append(ids, s[strings.Index(s, "#")+1:])
			}
			assert.Equal(t, tt.stopped, ids)
			if name == "a log's last lines" {
				assert.Equal(t, processLogLines, strings.Count(out, "  line "), "the last lines only")
			}
			for _, s := range *stopped {
				assert.NotContains(t, s, "elsewhere", "only this workspace's are stopped")
			}
		})
	}
}

func TestProcessesCommandEdges(t *testing.T) {
	t.Run("none running", func(t *testing.T) {
		app, s := stubApp(t, "")
		s.runningProcs = func(bool) ([]api.WorkspaceProcess, error) { return nil, nil }
		assert.Contains(t, runCmd(t, app, "/processes"), "No background processes running here.")
		assert.Contains(t, runCmd(t, app, "/processes stop flagged"), "Nothing to stop.")
	})
	t.Run("the service fails", func(t *testing.T) {
		app, s := stubApp(t, "")
		s.runningProcs = func(bool) ([]api.WorkspaceProcess, error) { return nil, errBoom }
		assert.Contains(t, runCmd(t, app, "/processes"), "boom")
		assert.Contains(t, runCmd(t, app, "/processes stop all"), "boom")
	})
	t.Run("a workspace without the service's processes", func(t *testing.T) {
		app, s := stubApp(t, "")
		app.Workspace = plainBackend{s}
		assert.Contains(t, runCmd(t, app, "/processes"), "aren't available here")
	})
	t.Run("in /help", func(t *testing.T) {
		app, _ := stubApp(t, "")
		out := runCmd(t, app, "/help")
		require.Contains(t, out, "/processes")
		assert.True(t, slices.ContainsFunc(strings.Split(out, "\n"), func(l string) bool {
			return strings.Contains(l, "/processes") && strings.Contains(l, "background processes")
		}))
	})
}

// plainBackend is a workspace with only api.Backend's methods.
type plainBackend struct{ api.Backend }
