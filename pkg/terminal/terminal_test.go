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

package terminal

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wait is how long a test waits for a shell.
const wait = 10 * time.Second

func open(t *testing.T, m *Manager, args ...string) *Terminal {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no PTYs here yet (TRM-50)")
	}
	term, err := m.Open(Options{Dir: t.TempDir(), Shell: "/bin/sh", Args: args, Cols: 80, Rows: 24, Version: "test"})
	require.NoError(t, err)
	return term
}

// watch collects term's output from from until it exits or wait passes,
// with how much was skipped.
func watch(t *testing.T, term *Terminal, from int64) (out string, last Chunk, skipped int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), wait)
	defer cancel()
	var b strings.Builder
	err := term.Watch(ctx, from, func(c Chunk) error {
		b.Write(c.Data)
		skipped += c.Skipped
		last = c
		return nil
	})
	require.NoError(t, err)
	return b.String(), last, skipped
}

// until reads term's output until it holds want.
func until(t *testing.T, term *Terminal, want string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), wait)
	defer cancel()
	var b strings.Builder
	errFound := context.Canceled
	err := term.Watch(ctx, 0, func(c Chunk) error {
		b.Write(c.Data)
		if strings.Contains(b.String(), want) {
			return errFound
		}
		return nil
	})
	require.ErrorIs(t, err, errFound, "waited for %q, got %q", want, b.String())
	return b.String()
}

func TestTerminalRunsAShell(t *testing.T) {
	m := NewManager(0, 0)
	t.Cleanup(m.CloseAll)
	for _, tc := range []struct {
		name   string
		script string
		out    string
		code   int
	}{
		{"its output and exit code", "printf 'hello'; exit 3", "hello", 3},
		{"its environment", `printf '%s %s %s' "$TERM" "$COLORTERM" "$TERM_PROGRAM"`, "xterm-256color truecolor Blitz", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term := open(t, m, "-c", tc.script)
			out, last, _ := watch(t, term, 0)
			assert.Contains(t, out, tc.out)
			assert.True(t, last.Exited)
			assert.Equal(t, tc.code, last.ExitCode)
			assert.Equal(t, int64(len(out)), last.Offset, "offsets count every byte")
			info := term.Info()
			assert.True(t, info.Exited)
			assert.Equal(t, tc.code, info.ExitCode)
			assert.Equal(t, "/bin/sh", info.Shell)
			<-term.Done()
		})
	}
}

func TestTerminalInputAndResize(t *testing.T) {
	m := NewManager(0, 0)
	t.Cleanup(m.CloseAll)
	term := open(t, m) // an interactive sh reading the PTY
	require.NoError(t, term.Resize(100, 40))
	require.NoError(t, term.Write([]byte("stty size; exit 0\n")))
	out, last, _ := watch(t, term, 0)
	assert.Contains(t, out, "40 100", "the shell sees the new size")
	assert.True(t, last.Exited)

	for _, size := range [][2]uint16{{0, 10}, {10, 0}, {1001, 10}} {
		assert.Error(t, term.Resize(size[0], size[1]), "%v", size)
	}
	assert.Error(t, term.Write(make([]byte, MaxWrite+1)), "too much at once")
}

func TestTerminalScrollback(t *testing.T) {
	m := NewManager(0, 64)
	t.Cleanup(m.CloseAll)
	term := open(t, m, "-c", "i=0; while [ $i -lt 20 ]; do printf '0123456789'; i=$((i+1)); done")
	<-term.Done()
	out, last, skipped := watch(t, term, 0)
	assert.Len(t, out, 64, "what's kept is replayed")
	assert.Equal(t, int64(200), last.Offset, "the offset counts what was cut too")
	assert.Equal(t, int64(136), skipped, "and says how much was cut")
	assert.True(t, strings.HasSuffix(out, "0123456789"))

	again, last, _ := watch(t, term, 190)
	assert.Equal(t, "0123456789", again, "a watcher picks up where it had read")
	assert.True(t, last.Exited)
}

func TestTerminalWatchers(t *testing.T) {
	m := NewManager(0, 0)
	t.Cleanup(m.CloseAll)
	term := open(t, m)
	var wg sync.WaitGroup
	got := make([]string, 2)
	for i := range got {
		wg.Go(func() { got[i] = until(t, term, "both see it") })
	}
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, term.Write([]byte("echo both see it\n")))
	wg.Wait()
	for _, g := range got {
		assert.Contains(t, g, "both see it")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.ErrorIs(t, term.Watch(ctx, 0, func(Chunk) error { return nil }), context.Canceled, "a watcher that leaves")
}

func TestTerminalClose(t *testing.T) {
	old := killAfter
	killAfter = 100 * time.Millisecond
	t.Cleanup(func() { killAfter = old })
	m := NewManager(2, 0)
	t.Cleanup(m.CloseAll)

	for _, tc := range []struct {
		name   string
		script string
	}{
		{"hung up", "sleep 30"},
		{"killed when it ignores the hangup", "trap '' HUP; sleep 30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term := open(t, m, "-c", tc.script)
			id := term.Info().ID
			assert.Equal(t, []string{id}, ids(m.List()))
			started := time.Now()
			require.NoError(t, m.Close(id))
			assert.Less(t, time.Since(started), 5*time.Second)
			select {
			case <-term.Done():
			default:
				t.Fatal("closed but not done")
			}
			_, err := m.Get(id)
			assert.ErrorIs(t, err, ErrUnknown)
			assert.ErrorIs(t, m.Close(id), ErrUnknown)
		})
	}
}

func TestManagerLimits(t *testing.T) {
	m := NewManager(1, 0)
	term := open(t, m, "-c", "sleep 30")
	got, err := m.Get(term.Info().ID)
	require.NoError(t, err)
	assert.Same(t, term, got)
	_, err = m.Open(Options{Dir: t.TempDir(), Shell: "/bin/sh", Args: []string{"-c", "true"}})
	assert.ErrorIs(t, err, ErrTooMany)

	m.CloseAll()
	assert.Empty(t, m.List())
	_, err = m.Open(Options{Dir: t.TempDir(), Shell: "/bin/sh"})
	assert.ErrorIs(t, err, ErrClosed, "none opens once the workspace closed")

	m = NewManager(0, 0)
	t.Cleanup(m.CloseAll)
	_, err = m.Open(Options{Dir: t.TempDir(), Shell: "/no/such/shell"})
	assert.Error(t, err)
	assert.Empty(t, m.List(), "a shell that doesn't start isn't kept")
}

func TestEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  []string
		want []string
	}{
		{"added", []string{"PATH=/bin"}, []string{"PATH=/bin", "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=Blitz", "TERM_PROGRAM_VERSION=1.0", "LANG=en_US.UTF-8"}},
		{"replaced", []string{"TERM=dumb", "COLORTERM=no", "TERM_PROGRAM=Apple_Terminal", "LANG=fr_CA.UTF-8"}, []string{"LANG=fr_CA.UTF-8", "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=Blitz", "TERM_PROGRAM_VERSION=1.0"}},
		{"an empty LANG", []string{"LANG="}, []string{"LANG=", "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=Blitz", "TERM_PROGRAM_VERSION=1.0", "LANG=en_US.UTF-8"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Environment(tc.env, "1.0"))
		})
	}
	assert.NotEmpty(t, Environment(nil, ""), "the service's own")
	assert.Equal(t, "PATH", key("PATH=/bin"))
	assert.Equal(t, "ODD", key("ODD"))
}

func TestLoginShell(t *testing.T) {
	t.Setenv("SHELL", "/usr/local/bin/fish")
	assert.Equal(t, "/usr/local/bin/fish", LoginShell())
	t.Setenv("SHELL", "")
	assert.Contains(t, []string{"/bin/zsh", "/bin/bash"}, LoginShell())
}

func ids(infos []Info) []string {
	var out []string
	for _, i := range infos {
		out = append(out, i.ID)
	}
	return out
}
