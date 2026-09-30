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

package tools

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A watched process reports matching lines and its exit.
func TestWatchedProcess(t *testing.T) {
	pm := NewProcessManager(0, 0)
	pm.exec = &ExecEnv{}
	t.Cleanup(pm.Shutdown)
	var mu sync.Mutex
	var got []string
	pm.Notice = func(session, text string) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, session+": "+text)
	}
	_, err := pm.StartWatched("s1", `printf 'ok\nERROR one\nfine\nERROR two'; exit 3`, t.TempDir(), &Watch{Exit: true, Lines: regexp.MustCompile("ERROR")})
	require.NoError(t, err)
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 3 }, 10*time.Second, 20*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, got[0], "s1: Background process 1 (`printf")
	assert.Contains(t, got[0], "printed: ERROR one")
	assert.Contains(t, got[1], "printed: ERROR two", "the last line, without a newline")
	assert.Contains(t, got[2], "exited with code 3")
	assert.Contains(t, got[2], "ERROR two")
}

func TestLineWatcherCap(t *testing.T) {
	var lines []string
	w := &lineWatcher{re: regexp.MustCompile("x"), emit: func(l string) { lines = append(lines, l) }}
	w.Write([]byte(strings.Repeat("x\r\n", maxWatchedLines+5)))
	assert.Len(t, lines, maxWatchedLines)
	assert.Equal(t, "x", lines[0], "no carriage return")
	long := &lineWatcher{re: regexp.MustCompile("y"), emit: func(l string) { lines = append(lines, l) }}
	long.Write([]byte(strings.Repeat("y", 70<<10)))
	assert.Len(t, lines, maxWatchedLines+1, "a very long line is checked")
}

func TestShellNotifyPattern(t *testing.T) {
	ws, _ := newTestWorkspace(t)
	cfg := shellCfg(ws)
	t.Cleanup(cfg.Processes.Shutdown)
	ctx := context.Background()
	out := runShellCommand(ctx, cfg, RunShellCommandInput{Command: "true", Background: true, NotifyPattern: "("})
	assert.Contains(t, out.Error, "notify_pattern")
	out = runShellCommand(ctx, cfg, RunShellCommandInput{Command: "true", Background: true, Notify: true})
	assert.Contains(t, out.Output, "you'll be told when it exits")
}
