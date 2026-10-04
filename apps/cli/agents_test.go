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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintRuns(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	running := api.BackgroundRun{ID: "bg-2", Workspace: "/w/app", Prompt: "fix the tests", State: "running", Started: now.Add(-90 * time.Second), CostUSD: 0.5}
	waiting := api.BackgroundRun{ID: "bg-3", Workspace: "/w/app", Prompt: "deploy", State: "waiting", Waiting: 2, Started: now.Add(-time.Minute)}
	ended := api.BackgroundRun{ID: "bg-1", Workspace: "/w/lib", Prompt: "\x1b[31mred", State: "done", Started: now.Add(-time.Hour), Ended: now.Add(-50 * time.Minute)}
	tests := []struct {
		name    string
		list    []api.BackgroundRun
		all     bool
		want    []string
		notWant []string
	}{
		{"going", []api.BackgroundRun{waiting, running, ended}, false, []string{"bg-2", "running", "1m30s", "$0.50", "bg-3", "waiting (2)"}, []string{"bg-1"}},
		{"all", []api.BackgroundRun{running, ended}, true, []string{"bg-1", "done", "10m0s", "[31mred"}, []string{"\x1b"}},
		{"none going", []api.BackgroundRun{ended}, false, []string{"No background runs are going", "blitz --bg"}, []string{"bg-1"}},
		{"none", nil, true, []string{"No background runs:"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			printRuns(&out, tt.list, tt.all, now)
			for _, w := range tt.want {
				assert.Contains(t, out.String(), w)
			}
			for _, w := range tt.notWant {
				assert.NotContains(t, out.String(), w)
			}
		})
	}
}

func TestHomeRel(t *testing.T) {
	home := isolate(t)
	assert.Equal(t, filepath.Join("~", "src", "app"), homeRel(filepath.Join(home, "src", "app")))
	assert.Equal(t, "/elsewhere", homeRel("/elsewhere"))
}

func TestBackgroundNeedsItsArguments(t *testing.T) {
	isolate(t)
	for _, args := range [][]string{
		{"--bg"},
		{"--bg", "-i", "hello"},
		{"--bg", "--local", "hello"},
		{"--bg", "--continue", "hello"},
		{"--bg", "--output-format", "json", "hello"},
	} {
		t.Run(args[len(args)-1], func(t *testing.T) {
			_, err := runCLI(t, args...)
			assert.Equal(t, exitUsage, exitCodeFor(err), "%v: %v", args, err)
		})
	}
	for _, args := range [][]string{{"--bg", "hello"}, {"agents"}, {"logs", "bg-1"}, {"stop", "bg-1"}, {"attach", "bg-1"}} {
		t.Run(args[0]+" without the service", func(t *testing.T) {
			_, err := runCLI(t, args...)
			assert.ErrorIs(t, err, errNeedsService)
		})
	}
}

// blitz --bg, agents, logs, stop and attach, with the service.
func TestBackgroundRunsWithTheService(t *testing.T) {
	fakeModel(t, isolate(t))
	dir, err := os.MkdirTemp("/tmp", "bg") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	t.Setenv("BLITZ_SOCKET", sock)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- servicetest.Run(ctx, sock) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("service didn't stop")
		}
	})
	require.Eventually(t, func() bool { return socket.Running(sock) }, 10*time.Second, 20*time.Millisecond)

	out, err := runCLI(t, "agents")
	require.NoError(t, err, out)
	assert.Contains(t, out, "No background runs are going")

	ws := t.TempDir()
	out, err = runCLI(t, "-d", ws, "--bg", "say", "hello")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Started bg-1")
	assert.Contains(t, out, "blitz attach bg-1")

	require.Eventually(t, func() bool {
		out, _ := runCLI(t, "agents", "--all")
		return bytes.Contains([]byte(out), []byte("bg-1  done"))
	}, 10*time.Second, 20*time.Millisecond, "the run didn't end")
	out, err = runCLI(t, "logs", "bg-1")
	require.NoError(t, err, out)
	assert.Contains(t, out, "done")
	out, err = runCLI(t, "stop", "bg-1")
	require.NoError(t, err, out)
	assert.Contains(t, out, "bg-1: done")

	for _, args := range [][]string{{"logs", "bg-9"}, {"stop", "bg-9"}, {"attach", "bg-9"}} {
		_, err = runCLI(t, args...)
		assert.Equal(t, exitUsage, exitCodeFor(err), "%v: %v", args, err)
		assert.ErrorIs(t, err, api.ErrUnknownRun)
	}
	_, err = runCLI(t, "--local", "attach", "bg-1")
	assert.Equal(t, exitUsage, exitCodeFor(err))
}
