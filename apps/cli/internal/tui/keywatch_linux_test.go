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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// openPTY opens a pseudo-terminal: the terminal's side (master) and the
// program's (the tty).
func openPTY(t *testing.T) (master, tty *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminals here: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	require.NoError(t, unix.IoctlSetPointerInt(int(m.Fd()), unix.TIOCSPTLCK, 0))
	n, err := unix.IoctlGetInt(int(m.Fd()), unix.TIOCGPTN)
	require.NoError(t, err)
	s, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return m, s
}

// ttyKeys switches a real terminal to key-at-a-time input and back, and
// reads keys from it.
func TestTTYKeys(t *testing.T) {
	master, tty := openPTY(t)
	fd := int(tty.Fd())
	lflag := func() uint32 {
		tio, err := unix.IoctlGetTermios(fd, getTermios)
		require.NoError(t, err)
		return tio.Lflag
	}
	require.NotZero(t, lflag()&unix.ICANON, "a new terminal is line-buffered")

	for name, c := range map[string]struct {
		k      keyTerm
		signal bool
	}{"steering": {newTTYKeys(fd), true}, "picker": {newPickerKeys(fd), false}} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, c.k.enter())
			assert.Zero(t, lflag()&(unix.ICANON|unix.ECHO), "still line-buffered or echoing")
			assert.Equal(t, c.signal, lflag()&unix.ISIG != 0, "Ctrl+C as a signal")

			ok, err := c.k.ready(10 * time.Millisecond)
			require.NoError(t, err)
			assert.False(t, ok, "ready with nothing typed")
			_, err = master.Write([]byte("x"))
			require.NoError(t, err)
			ok, err = c.k.ready(time.Second)
			require.NoError(t, err)
			require.True(t, ok, "not ready after a key")
			buf := make([]byte, 8)
			n, err := c.k.read(buf)
			require.NoError(t, err)
			assert.Equal(t, "x", string(buf[:n]))

			require.NoError(t, c.k.leave())
			assert.NotZero(t, lflag()&unix.ICANON, "the mode wasn't restored")
			assert.NoError(t, c.k.leave(), "leaving twice")
		})
	}

	// A pipe isn't a terminal.
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()
	assert.Error(t, newTTYKeys(int(r.Fd())).enter())
}

// Page prints short text on a terminal and pages long text through
// $PAGER, printing it if the pager fails.
func TestPageOnATerminal(t *testing.T) {
	master, tty := openPTY(t)
	require.NoError(t, unix.IoctlSetWinsize(int(tty.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 5, Col: 80}))
	screen := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			sb.Write(buf[:n])
			if err != nil || strings.Contains(sb.String(), "END") {
				screen <- sb.String()
				return
			}
		}
	}()
	long := strings.Repeat("line\n", 10)
	require.NoError(t, Page(tty, "short\n"))
	t.Setenv("PAGER", "echo paged; cat")
	require.NoError(t, Page(tty, long))
	t.Setenv("PAGER", "exit 3")
	require.NoError(t, Page(tty, long+"END\n"))
	select {
	case got := <-screen:
		assert.Contains(t, got, "short")
		assert.Contains(t, got, "paged")
		assert.Equal(t, 20, strings.Count(got, "line"), "paged once, printed once after the failed pager")
	case <-time.After(5 * time.Second):
		t.Fatal("nothing reached the terminal")
	}
	assert.NoError(t, Page(io.Discard, "not a file"))
}
