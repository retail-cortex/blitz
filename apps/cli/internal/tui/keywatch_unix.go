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

//go:build darwin || linux

package tui

import (
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

// ttyKeys is the keyTerm for a real terminal on fd.
type ttyKeys struct {
	fd    int
	saved *unix.Termios
	// noSignals makes Ctrl+C a key rather than SIGINT (pickers).
	noSignals bool
}

func newTTYKeys(fd int) keyTerm { return &ttyKeys{fd: fd} }

// newPickerKeys is the keyTerm for a picker, which reads Ctrl+C itself.
func newPickerKeys(fd int) keyTerm { return &ttyKeys{fd: fd, noSignals: true} }

// enter turns off line buffering and echo but keeps signal keys, so Ctrl+C
// still interrupts the turn exactly as before. On macOS Ctrl+T is the
// STATUS key (SIGINFO); it is disabled so it reaches us.
func (t *ttyKeys) enter() error {
	cur, err := unix.IoctlGetTermios(t.fd, getTermios)
	if err != nil {
		return err
	}
	saved := *cur
	cur.Lflag &^= unix.ICANON | unix.ECHO
	if t.noSignals {
		cur.Lflag &^= unix.ISIG
	}
	cur.Cc[unix.VMIN] = 1
	cur.Cc[unix.VTIME] = 0
	disableStatusKey(cur)
	if err := unix.IoctlSetTermios(t.fd, setTermios, cur); err != nil {
		return err
	}
	t.saved = &saved
	return nil
}

func (t *ttyKeys) leave() error {
	if t.saved == nil {
		return nil
	}
	err := unix.IoctlSetTermios(t.fd, setTermios, t.saved)
	t.saved = nil
	return err
}

// ready uses select(2) rather than poll(2), which doesn't work on
// terminals on macOS.
func (t *ttyKeys) ready(timeout time.Duration) (bool, error) {
	var fds unix.FdSet
	fds.Set(t.fd)
	tv := unix.NsecToTimeval(timeout.Nanoseconds())
	n, err := unix.Select(t.fd+1, &fds, nil, nil, &tv)
	if errors.Is(err, unix.EINTR) { // e.g. SIGWINCH on resize
		return false, nil
	}
	return n > 0, err
}

func (t *ttyKeys) read(p []byte) (int, error) {
	n, err := unix.Read(t.fd, p)
	if errors.Is(err, unix.EINTR) {
		return 0, nil
	}
	return n, err
}
