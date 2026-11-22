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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/config"
)

// workspaceLock is an exclusive OS file lock held while a workspace is
// open, and its owner file beside it: the holder's process and whether it
// is closing the workspace.
type workspaceLock struct {
	f     *os.File
	owner string
}

// How long an opener waits for a workspace whose holder is closing it or
// has gone, and how often it looks (variables for tests). A service that
// restarts is still closing its workspaces (stopping tasks, MCP servers,
// processes) when the new one is asked for them, and Windows releases a
// dead process's lock when it gets to it, not at once; a workspace open
// elsewhere and staying open is busy at once, as before.
var (
	lockGrace = 15 * time.Second
	lockPoll  = 100 * time.Millisecond
)

// lockWorkspace locks dir (canonical) through a file under
// ~/.blitz/locks named after it, waiting a while when its holder is on its
// way out (holderLeaving).
func lockWorkspace(dir string) (*workspaceLock, error) {
	locks := config.ExpandHome("~/.blitz/locks")
	if err := os.MkdirAll(locks, 0o700); err != nil {
		return nil, fmt.Errorf("workspace lock: %w", err)
	}
	sum := sha256.Sum256([]byte(dir))
	base := filepath.Join(locks, hex.EncodeToString(sum[:12]))
	f, err := os.OpenFile(base+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("workspace lock: %w", err)
	}
	l := &workspaceLock{f: f, owner: base + ".owner"}
	deadline := time.Now().Add(lockGrace)
	for {
		err := tryLock(f)
		if err == nil {
			break
		}
		if !errors.Is(err, errLocked) {
			f.Close()
			return nil, fmt.Errorf("workspace lock: %w", err)
		}
		if !holderLeaving(l.owner) || time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("%w: %s", api.ErrWorkspaceBusy, dir)
		}
		time.Sleep(lockPoll)
	}
	// For people looking at the directory: which workspace, which process.
	f.Truncate(0)
	fmt.Fprintf(f, "%s\npid %d\n", dir, os.Getpid())
	l.state("open")
	return l, nil
}

// state records the holder and what it's doing with the workspace in the
// owner file (unlocked, so a would-be opener can read it: Windows' locks
// keep other processes from reading the locked file).
func (l *workspaceLock) state(s string) {
	_ = os.WriteFile(l.owner, []byte(fmt.Sprintf("pid %d\nstate %s\n", os.Getpid(), s)), 0o600)
}

// markClosing says the holder is closing the workspace: an opener waits
// for the lock rather than calling it busy.
func (l *workspaceLock) markClosing() {
	if l != nil && l.f != nil {
		l.state("closing")
	}
}

// holderLeaving reports whether the lock's holder, as its owner file says,
// is closing the workspace or no longer runs. No owner file (a holder
// from before it was kept) is a holder staying.
func holderLeaving(owner string) bool {
	b, err := os.ReadFile(owner)
	if err != nil {
		return false
	}
	pid, state := 0, ""
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "pid "); ok {
			pid, _ = strconv.Atoi(strings.TrimSpace(v))
		}
		if v, ok := strings.CutPrefix(line, "state "); ok {
			state = strings.TrimSpace(v)
		}
	}
	if state == "closing" {
		return true
	}
	return pid > 0 && pid != os.Getpid() && !processAlive(pid)
}

// release unlocks; the files stay for the next owner, who writes its own.
func (l *workspaceLock) release() {
	if l != nil && l.f != nil {
		unlock(l.f)
		l.f.Close()
		l.f = nil
	}
}
