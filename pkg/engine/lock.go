package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/config"
)

// workspaceLock is an exclusive OS file lock held while a workspace is open.
type workspaceLock struct{ f *os.File }

// lockWorkspace locks dir (canonical) through a file under
// ~/.blitz/locks named after it.
func lockWorkspace(dir string) (*workspaceLock, error) {
	locks := config.ExpandHome("~/.blitz/locks")
	if err := os.MkdirAll(locks, 0o700); err != nil {
		return nil, fmt.Errorf("workspace lock: %w", err)
	}
	sum := sha256.Sum256([]byte(dir))
	f, err := os.OpenFile(filepath.Join(locks, hex.EncodeToString(sum[:12])+".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("workspace lock: %w", err)
	}
	if err := tryLock(f); err != nil {
		f.Close()
		if errors.Is(err, errLocked) {
			return nil, fmt.Errorf("%w: %s", api.ErrWorkspaceBusy, dir)
		}
		return nil, fmt.Errorf("workspace lock: %w", err)
	}
	// For people looking at the directory: which workspace, which process.
	f.Truncate(0)
	fmt.Fprintf(f, "%s\npid %d\n", dir, os.Getpid())
	return &workspaceLock{f: f}, nil
}

// release unlocks; the file stays for the next owner.
func (l *workspaceLock) release() {
	if l != nil && l.f != nil {
		unlock(l.f)
		l.f.Close()
		l.f = nil
	}
}
