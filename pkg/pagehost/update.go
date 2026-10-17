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

package pagehost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/selfupdate"
)

// UpdateStatus is the desktop app's update, as the page shows it
// (spec_electron_038 EL-90).
type UpdateStatus struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	// Can is whether the app updates itself here; Reason says why not (a
	// package manager's copy, a development build).
	Can    bool   `json:"can"`
	Reason string `json:"reason,omitempty"`
	// Downloading, then Ready: downloaded, checked and unpacked.
	Downloading bool   `json:"downloading"`
	Ready       bool   `json:"ready"`
	Error       string `json:"error,omitempty"`
	// Notes is the release's page.
	Notes string `json:"notes,omitempty"`
}

// Updater updates the desktop app the host runs in: it checks the
// releases (the owner's wish: when the app opens, and then only five days
// after the last check, unless asked), downloads and checks the new one,
// and puts it in place through a copy of the host run from outside the app
// (blitz-tray --apply-update), once the app has quit.
type Updater struct {
	Version  string // the app's (the host's)
	HostExe  string // the host's program, in the app (…/resources/app/bin)
	Dir      string // where downloads go (~/.blitz/updates)
	CacheDir string // Sigstore's trust root's cache (~/.blitz)
	Source   func() selfupdate.Source
	// Spawn starts a program that outlives the host.
	Spawn        func(name string, args ...string) error
	GOOS, GOARCH string

	// Now is the time (tests replace it).
	Now func() time.Time

	mu          sync.Mutex
	latest      string
	staged      string
	downloading bool
	err         string
}

// install is the app being updated, or why it can't be.
func (u *Updater) install() (selfupdate.Install, string) {
	if u.Version == "" || u.Version == "dev" {
		return selfupdate.Install{}, "This is a development build: it doesn't update itself."
	}
	in, err := selfupdate.FindInstall(u.HostExe, u.GOOS)
	switch {
	case err != nil:
		return in, err.Error()
	case in.Manager != "":
		return in, fmt.Sprintf("Installed by %s: update it there.", in.Manager)
	}
	return in, ""
}

// CheckEvery is how long a check's answer stands: the app asks for the
// latest release when it opens only if the last check is older.
const CheckEvery = 5 * 24 * time.Hour

// lastCheck is the last check's time and answer, kept between runs
// (Dir/last-check.json).
type lastCheck struct {
	Time   time.Time `json:"time"`
	Latest string    `json:"latest"`
}

// Check says where the update is: from the last check while it's less
// than CheckEvery old, unless force (Check now), else from the latest
// release, asked for.
func (u *Updater) Check(ctx context.Context, force bool) UpdateStatus {
	_, reason := u.install()
	now := time.Now()
	if u.Now != nil {
		now = u.Now()
	}
	last := filepath.Join(u.Dir, "last-check.json")
	var prev lastCheck
	if data, err := os.ReadFile(last); err == nil {
		_ = json.Unmarshal(data, &prev)
	}
	latest, err := prev.Latest, error(nil)
	if force || prev.Latest == "" || now.Sub(prev.Time) >= CheckEvery {
		latest, err = u.Source().Latest(ctx)
		if err == nil {
			if data, merr := json.Marshal(lastCheck{Time: now, Latest: latest}); merr == nil {
				_ = os.MkdirAll(u.Dir, 0o700)
				_ = os.WriteFile(last, data, 0o600)
			}
		}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if err == nil {
		u.latest = latest
	}
	s := u.statusLocked(reason)
	if err != nil {
		s.Error = "finding the latest release: " + err.Error()
	}
	return s
}

// Status is where the update is, without asking for the latest release
// (as Check last found it): for following a download.
func (u *Updater) Status() UpdateStatus {
	_, reason := u.install()
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.statusLocked(reason)
}

// statusLocked is the status now (u.mu held).
func (u *Updater) statusLocked(reason string) UpdateStatus {
	s := UpdateStatus{Current: u.Version, Latest: u.latest, Can: reason == "", Reason: reason, Downloading: u.downloading, Error: u.err}
	if u.latest != "" {
		s.Available = u.Version == "dev" || selfupdate.Compare(u.latest, u.Version) > 0
		s.Notes = "https://github.com/retail-cortex/blitz/releases/tag/" + u.latest
	}
	s.Ready = s.Available && u.staged != "" && filepath.Base(filepath.Dir(filepath.Dir(u.staged))) == u.latest
	return s
}

// Download downloads, checks and unpacks the latest release (Check
// first), unless it's downloading or ready already, and says where the
// update is when it's done: the page waits for it, rather than asking
// again and again.
func (u *Updater) Download(ctx context.Context) UpdateStatus {
	_, reason := u.install()
	u.mu.Lock()
	s := u.statusLocked(reason)
	if !s.Can || !s.Available || s.Downloading || s.Ready {
		u.mu.Unlock()
		return s
	}
	tag := u.latest
	u.downloading, u.err = true, ""
	u.mu.Unlock()
	staged, err := selfupdate.Stage(ctx, u.Source(), tag, u.GOOS, u.GOARCH, filepath.Join(u.Dir, tag), u.CacheDir)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.downloading = false
	if err != nil {
		u.err = err.Error()
	} else {
		u.staged = staged
	}
	return u.statusLocked(reason)
}

// ErrNotReady is applying an update that hasn't been downloaded.
var ErrNotReady = errors.New("no update has been downloaded")

// Apply starts the swap (a copy of the host, from outside the app), which
// waits for the app (the host's parent) to quit; the page then quits it.
func (u *Updater) Apply() error {
	in, reason := u.install()
	if reason != "" {
		return errors.New(reason)
	}
	u.mu.Lock()
	staged := u.staged
	u.mu.Unlock()
	if staged == "" {
		return ErrNotReady
	}
	name := "apply-" + filepath.Base(u.HostExe)
	runner := filepath.Join(u.Dir, name)
	if err := copyProgram(u.HostExe, runner); err != nil {
		return fmt.Errorf("copying the updater: %w", err)
	}
	return u.Spawn(runner, "--apply-update", "--staged", staged, "--root", in.Root, "--relaunch", in.Program, "--wait-pid", strconv.Itoa(os.Getppid()))
}

// copyProgram copies an executable.
func copyProgram(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		// A previous updater still running on Windows keeps its file: it's
		// a copy of the host too, with the same flags, so it's used as it is.
		if strings.Contains(err.Error(), "used by another process") {
			return nil
		}
		return err
	}
	return nil
}
