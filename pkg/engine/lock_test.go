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

package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
)

// shortLockWaits makes the waits for a leaving holder short.
func shortLockWaits(t *testing.T, grace time.Duration) {
	t.Helper()
	oldGrace, oldPoll := lockGrace, lockPoll
	lockGrace, lockPoll = grace, 5*time.Millisecond
	t.Cleanup(func() { lockGrace, lockPoll = oldGrace, oldPoll })
}

// A workspace open and staying open is busy at once.
func TestLockBusyAtOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	shortLockWaits(t, 5*time.Second)
	dir := t.TempDir()
	first, err := lockWorkspace(dir)
	require.NoError(t, err)
	defer first.release()
	start := time.Now()
	_, err = lockWorkspace(dir)
	require.ErrorIs(t, err, api.ErrWorkspaceBusy)
	assert.Less(t, time.Since(start), time.Second, "no wait for a holder staying")
}

// One opening a workspace its holder is closing waits for it, and gets it
// once it's released: a restarted service and the one before it.
func TestLockWaitsForAClosingHolder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	shortLockWaits(t, 5*time.Second)
	dir := t.TempDir()
	first, err := lockWorkspace(dir)
	require.NoError(t, err)
	first.markClosing()
	go func() {
		time.Sleep(50 * time.Millisecond)
		first.release()
	}()
	second, err := lockWorkspace(dir)
	require.NoError(t, err, "waited for the closing holder")
	second.release()
}

// A holder that never lets go is busy once the wait is over.
func TestLockGivesUpAfterTheWait(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	shortLockWaits(t, 50*time.Millisecond)
	dir := t.TempDir()
	first, err := lockWorkspace(dir)
	require.NoError(t, err)
	defer first.release()
	first.markClosing()
	_, err = lockWorkspace(dir)
	assert.ErrorIs(t, err, api.ErrWorkspaceBusy)
}

func TestHolderLeaving(t *testing.T) {
	dir := t.TempDir()
	owner := filepath.Join(dir, "x.owner")
	write := func(pid int, state string) {
		require.NoError(t, os.WriteFile(owner, []byte(fmt.Sprintf("pid %d\nstate %s\n", pid, state)), 0o600))
	}
	assert.False(t, holderLeaving(owner), "no owner file: an older holder, staying")
	write(os.Getpid(), "open")
	assert.False(t, holderLeaving(owner), "this process, open")
	write(os.Getpid(), "closing")
	assert.True(t, holderLeaving(owner), "closing")
	cmd := exec.Command(os.Args[0], "-test.run=^$") // a process that ends
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "exit")
	}
	require.NoError(t, cmd.Run())
	write(cmd.Process.Pid, "open")
	assert.True(t, holderLeaving(owner), "its process is gone")
}

// Closing a workspace says so in its owner file, before the long part.
func TestCloseMarksTheLockClosing(t *testing.T) {
	w, _ := openTestWith(t, nil)
	require.NotNil(t, w.lock)
	owner := w.lock.owner
	b, err := os.ReadFile(owner)
	require.NoError(t, err)
	assert.Contains(t, string(b), "state open")
	require.NoError(t, w.Close())
	b, err = os.ReadFile(owner)
	require.NoError(t, err)
	assert.Contains(t, string(b), "state closing")
}
