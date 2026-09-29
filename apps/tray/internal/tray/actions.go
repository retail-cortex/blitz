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
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"syscall"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/loginitem"
)

// Actions are what the menu's items do. Each has what it needs to find
// and run the system's programs; tests replace them.
type Actions struct {
	// Run starts a program and doesn't wait for it.
	Run func(name string, args ...string) error
	// Beside is the tray's own directory (blitzd and the app are there
	// when installed together).
	Beside string
	// Status is the service's state now.
	Status func() Status
	// Wait polls until the service is (or isn't) running, for up to d.
	Wait func(running bool, d time.Duration) bool
	// LogDir is where the running service says its log is ("" when it
	// doesn't say); nil asks nobody. The settings file answers otherwise.
	LogDir func() string
}

// Start starts the service: through its login item when it has one, else
// blitzd from beside the tray or PATH, on its own.
func (a Actions) Start() error {
	if loginitem.Installed() {
		if err := loginitem.Start(); err != nil {
			return err
		}
	} else {
		bin, err := loginitem.FindService(a.Beside)
		if err != nil {
			return err
		}
		if err := a.Run(bin); err != nil {
			return err
		}
	}
	if !a.Wait(true, 15*time.Second) {
		return errors.New("the service didn't start in 15 s: see Open the logs")
	}
	return nil
}

// Stop stops the service: its login item's, else the process that
// answered (SIGTERM, so turns in progress get a few seconds).
func (a Actions) Stop() error {
	if loginitem.Installed() {
		_ = loginitem.Stop() // not loaded is fine: it may run on its own
		if a.Wait(false, 5*time.Second) {
			return nil
		}
	}
	if pid := a.Status().PID; pid > 0 {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Signal(syscall.SIGTERM)
		}
	}
	if !a.Wait(false, 15*time.Second) {
		return errors.New("the service is still running after 15 s")
	}
	return nil
}

// Restart restarts the service (Stop then Start without a login item).
func (a Actions) Restart() error {
	if loginitem.Installed() {
		if err := loginitem.Restart(); err != nil {
			return err
		}
		if !a.Wait(true, 15*time.Second) {
			return errors.New("the service didn't come back in 15 s: see Open the logs")
		}
		return nil
	}
	if err := a.Stop(); err != nil {
		return err
	}
	return a.Start()
}

// OpenApp opens the desktop app: the bundle the tray is in (macOS),
// blitz-desktop beside it or on PATH (Linux).
func (a Actions) OpenApp() error {
	if goruntime.GOOS == "darwin" {
		// Blitz.app/Contents/MacOS/blitz-tray
		if app := filepath.Dir(filepath.Dir(a.Beside)); strings.HasSuffix(app, ".app") {
			return a.Run("open", app)
		}
		return a.Run("open", "-a", "Blitz")
	}
	name := "blitz-desktop"
	if p := filepath.Join(a.Beside, name); isFile(p) {
		return a.Run(p)
	}
	if p, err := exec.LookPath(name); err == nil {
		return a.Run(p)
	}
	return fmt.Errorf("%s isn't installed beside the tray or on PATH", name)
}

// OpenLogs shows the logs folder in the file manager: the running
// service's, which may not be the settings file's (another --config, or
// log.dir changed since it started).
func (a Actions) OpenLogs() error {
	dir := ""
	if a.LogDir != nil {
		dir = a.LogDir()
	}
	if dir == "" {
		dir = LogDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if goruntime.GOOS == "darwin" {
		return a.Run("open", dir)
	}
	return a.Run("xdg-open", dir)
}

// LogDir is where the service's logs go by the settings file: log.dir of
// the global settings.
func LogDir() string {
	if cfg, err := config.Load(""); err == nil && cfg.Log.Dir != "" {
		return config.ExpandHome(cfg.Log.Dir)
	}
	return config.ExpandHome("~/.blitz/logs")
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// Detached starts a program in its own session, with no terminal and its
// output discarded, and reaps it when it exits.
func Detached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
