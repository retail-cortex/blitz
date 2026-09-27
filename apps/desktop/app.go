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

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is what the page can call in Go (bound by Wails): what the service
// can't do itself, namely report whether it runs, install it, and native
// dialogs.
type App struct {
	ctx    context.Context
	socket string
	prefs  *prefsStore

	notifyOnce sync.Once
	notifyOK   bool // notifications work and the user allowed them

	unsavedMu sync.Mutex
	unsaved   Unsaved // the editor's unsaved changes (unsaved.go)
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// A click on a notification brings the window back, on its workspace.
	if runtime.InitializeNotifications(ctx) == nil {
		runtime.OnNotificationResponse(ctx, func(r runtime.NotificationResult) {
			if r.Error != nil {
				return
			}
			runtime.WindowUnminimise(ctx)
			runtime.WindowShow(ctx)
			if dir, ok := r.Response.UserInfo["dir"].(string); ok {
				runtime.EventsEmit(ctx, "notification:open", dir)
			}
		})
	}
}

// Notify shows a system notification (the agent finished, or waits for
// the user). The first one asks the user's permission; without it, or
// where notifications don't work, it does nothing. dir is the workspace a
// click opens.
func (a *App) Notify(title, body, dir string) error {
	a.notifyOnce.Do(func() {
		if !runtime.IsNotificationAvailable(a.ctx) {
			return
		}
		ok, err := runtime.RequestNotificationAuthorization(a.ctx)
		a.notifyOK = ok && err == nil
	})
	if !a.notifyOK {
		return nil
	}
	return runtime.SendNotification(a.ctx, runtime.NotificationOptions{
		ID:    fmt.Sprintf("blitz-%d", time.Now().UnixNano()),
		Title: title,
		Body:  body,
		Data:  map[string]any{"dir": dir},
	})
}

// Version is the app's version: the release's tag, or "dev".
func (a *App) Version() string { return version }

// ServiceStatus says whether the service answers and whether it starts at
// login.
type ServiceStatus struct {
	Running   bool   `json:"running"`
	Installed bool   `json:"installed"`
	Socket    string `json:"socket"`
	// Service is the blitzd this app installs and restarts ("" if none
	// was found).
	Service string `json:"service"`
}

func (a *App) ServiceStatus() ServiceStatus {
	bin, _ := findService()
	return ServiceStatus{Running: socket.Running(a.socket), Installed: loginitem.Installed(), Socket: a.socket, Service: bin}
}

// InstallService starts the service now and at every login, with the
// blitzd that goes with this app, replacing a previous login item.
func (a *App) InstallService() error {
	bin, err := findService()
	if err != nil {
		return err
	}
	return loginitem.Install(bin)
}

// StopService stops the running service: through the login item when there
// is one, else (or if it's still answering) by signalling pid, the process
// GetServiceInfo reported (0 when unknown: a service too old to say).
func (a *App) StopService(pid int) error {
	if loginitem.Installed() {
		_ = loginitem.Stop() // not loaded is fine
		if a.waitStopped(5 * time.Second) {
			return nil
		}
	}
	if pid > 0 {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Signal(syscall.SIGTERM) // turns in progress get a few seconds
		}
		if a.waitStopped(15 * time.Second) {
			return nil
		}
	}
	if !socket.Running(a.socket) {
		return nil
	}
	return errors.New("the service is still running; stop it from a terminal (blitz service uninstall, or kill it)")
}

// RestartService stops the running service and starts the one that goes
// with this app, at login too.
func (a *App) RestartService(pid int) error {
	if err := a.StopService(pid); err != nil {
		return err
	}
	return a.InstallService()
}

func (a *App) waitStopped(limit time.Duration) bool {
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if !socket.Running(a.socket) {
			return true
		}
	}
	return false
}

// ChooseWorkspace asks for a directory to open ("" if cancelled). The
// page gives the dialog's title, in the window's language.
func (a *App) ChooseWorkspace(title string) (string, error) {
	start := time.Now()
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title, CanCreateDirectories: true})
	// A dialog that closes on its own shows here as an empty answer after
	// a moment (on the terminal the app runs from).
	log.Printf("open workspace dialog: %q, error %v, after %s", dir, err, time.Since(start).Round(time.Millisecond))
	return dir, err
}

// ProgramExists reports whether path is a file: the program a running
// service started from may have been removed since (DSK-51a).
func (a *App) ProgramExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// GetPrefs returns the window's settings (the defaults when none are
// saved). A damaged file is reported once and replaced by the defaults.
func (a *App) GetPrefs() (Prefs, error) { return a.prefs.load() }

// SavePrefs saves the window's settings and returns them as saved
// (normalized).
func (a *App) SavePrefs(p Prefs) (Prefs, error) { return a.prefs.save(p) }

// OpenURL opens a web or mail link in the system browser, never in the
// window. Other schemes are refused.
func (a *App) OpenURL(link string) error {
	if !safeURL(link) {
		return fmt.Errorf("not opening %q: only web and mail links open", link)
	}
	runtime.BrowserOpenURL(a.ctx, link)
	return nil
}

// findService finds the blitzd that goes with this app: beside its
// executable (Blitz.app, the .deb), in its Bazel runfiles (bazel run), or
// on PATH.
func findService() (string, error) {
	var dirs []string
	if d := loginitem.Beside(); d != "" {
		dirs = append(dirs, d)
	}
	for _, root := range runfilesRoots() {
		dirs = append(dirs, filepath.Join(root, "_main", "apps", "service"))
	}
	return loginitem.FindService(dirs...)
}

// runfilesRoots are where Bazel puts this program's runfiles, when it ran it.
func runfilesRoots() []string {
	var out []string
	if d := os.Getenv("RUNFILES_DIR"); d != "" {
		out = append(out, d)
	}
	if exe, err := os.Executable(); err == nil {
		out = append(out, exe+".runfiles")
	}
	return out
}
