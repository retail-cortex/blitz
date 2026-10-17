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

package tray

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/selfupdate"
)

// Update puts a downloaded desktop app in place of the running one
// (blitz-tray --apply-update, run by the app's host from outside the app,
// spec_electron_038 EL-90): it waits for the app to quit, stops the
// service if it runs from the app (Windows can't replace a program in
// use), swaps them, starts the service again and then the app, the new
// one or, if the swap failed, the old one.
type Update struct {
	Staged, Root, Relaunch string
	// WaitPID is the app's process, waited for (0: none).
	WaitPID int
	Actions Actions
	// ServiceExe is the running service's program ("" if none runs).
	ServiceExe func() string
	// Exited waits up to d for process pid to end.
	Exited func(pid int, d time.Duration) bool
	Log    io.Writer
}

// Run applies the update.
func (u Update) Run() error {
	logf := func(format string, args ...any) {
		fmt.Fprintf(u.Log, time.Now().Format(time.RFC3339)+" "+format+"\n", args...)
	}
	if u.WaitPID > 0 && !u.Exited(u.WaitPID, 2*time.Minute) {
		return errors.New("the app didn't quit, so it wasn't updated")
	}
	stopped := false
	if exe := u.ServiceExe(); exe != "" && within(exe, u.Root) {
		logf("stopping the service (%s)", exe)
		if err := u.Actions.Stop(); err != nil {
			return fmt.Errorf("stopping the service, so the app wasn't updated: %w", err)
		}
		stopped = true
	}
	err := selfupdate.Apply(u.Staged, u.Root)
	if err != nil {
		logf("not updated: %v", err)
	} else {
		logf("updated %s", u.Root)
	}
	if stopped {
		if serr := u.Actions.Start(); serr != nil {
			logf("starting the service again: %v", serr)
		}
	}
	if u.Relaunch != "" {
		if rerr := u.relaunch(); rerr != nil {
			logf("starting the app again: %v", rerr)
		}
	}
	return err
}

// relaunch starts the app again: the bundle on macOS, its program
// elsewhere.
func (u Update) relaunch() error {
	if goruntime.GOOS == "darwin" {
		return u.Actions.Run("open", u.Relaunch)
	}
	return u.Actions.Run(u.Relaunch)
}

// within reports whether path is inside dir.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
