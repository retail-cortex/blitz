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

// Package pagehost answers the desktop page's calls to the program that
// serves it: the Electron app's sidecar (blitz-tray --serve), and the
// tray's page in a browser on Windows. It holds what those calls do (the
// window's preferences, the service, the blitz command's installation,
// the OS sandbox, updates), so every host answers alike.
package pagehost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	goruntime "runtime"
	"slices"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/fileview"
	"github.com/retail-cortex/blitz/pkg/legal"
	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/retail-cortex/blitz/pkg/pageserver"
	"github.com/retail-cortex/blitz/pkg/socket"
)

// Service starts and stops the service for the host (the tray's
// Actions).
type Service interface {
	Start() error
	Stop() error
	Restart() error
	Running() bool
}

// Host answers the page it serves (pageserver.HostPrefix) with what the
// desktop app's bindings do and a browser can't: the service's status
// and controls, starting at login, showing files, the licenses, the
// window's preferences, the blitz command and the OS sandbox. The page
// calls POST /host/<Method> with its arguments as a JSON array and gets
// the result as JSON, or {"error": "…"} with status 500, the method names
// being the desktop app's (window.go.main.App); GET /host/info lists
// them.
type Host struct {
	Service Service
	// Beside is the directory blitzd is looked for in before PATH (the
	// host's own).
	Beside string
	// Version is the host's, which the page shows as the app's.
	Version string
	// Socket is the service's.
	Socket string
	// Tray is the tray's own program, which starts at login.
	Tray string
	// StartTray starts the tray in this session, where launchd doesn't
	// (Linux and Windows; nil: it isn't started).
	StartTray func() error
	// Prefs keeps the window's preferences (nil: the page keeps its own).
	Prefs *PrefsStore
	// Updates updates the desktop app the host runs in (nil: no updates).
	Updates *Updater
}

// ServiceStatus says whether the service answers and whether it starts at
// login, as the page reads it.
type ServiceStatus struct {
	Running       bool   `json:"running"`
	Installed     bool   `json:"installed"`
	Socket        string `json:"socket"`
	Service       string `json:"service"`
	Tray          string `json:"tray"`
	TrayInstalled bool   `json:"tray_installed"`
}

// hostMethod is one of the page's calls: its arguments, decoded by the
// method, and its result.
type hostMethod func(args []json.RawMessage) (any, error)

func (h Host) methods() map[string]hostMethod {
	path := func(f func(string) error) hostMethod {
		return func(args []json.RawMessage) (any, error) {
			var p string
			if err := arg(args, 0, &p); err != nil {
				return nil, err
			}
			return nil, f(p)
		}
	}
	m := map[string]hostMethod{
		"Version":       func([]json.RawMessage) (any, error) { return h.Version, nil },
		"ServiceStatus": func([]json.RawMessage) (any, error) { return h.status(), nil },
		"InstallService": func([]json.RawMessage) (any, error) {
			return nil, h.installService()
		},
		"SetTray": func(args []json.RawMessage) (any, error) {
			var on bool
			if err := arg(args, 0, &on); err != nil {
				return nil, err
			}
			if on {
				if err := loginitem.InstallTray(h.Tray); err != nil {
					return nil, err
				}
				return nil, h.startTray()
			}
			return nil, loginitem.UninstallTray()
		},
		"StopService":    func([]json.RawMessage) (any, error) { return nil, h.Service.Stop() },
		"RestartService": func([]json.RawMessage) (any, error) { return nil, h.Service.Restart() },
		"ProgramExists": func(args []json.RawMessage) (any, error) {
			var p string
			if err := arg(args, 0, &p); err != nil {
				return nil, err
			}
			return ProgramExists(p), nil
		},
		"OpenFolder":   path(fileview.OpenFolder),
		"OpenDocument": path(fileview.OpenDocument),
		"RevealPath":   path(fileview.RevealPath),
		"FileManager":  func([]json.RawMessage) (any, error) { return fileview.FileManager(), nil },
		"License": func(args []json.RawMessage) (any, error) {
			var which string
			if err := arg(args, 0, &which); err != nil {
				return nil, err
			}
			return License(which)
		},
		"SandboxStatus": func([]json.RawMessage) (any, error) { return SandboxStatus(context.Background()), nil },
		"FixSandbox":    func([]json.RawMessage) (any, error) { return FixSandbox(context.Background()), nil },
	}
	if h.Prefs != nil {
		m["GetPrefs"] = func([]json.RawMessage) (any, error) { return h.Prefs.Load() }
		m["SavePrefs"] = func(args []json.RawMessage) (any, error) {
			var p Prefs
			if err := arg(args, 0, &p); err != nil {
				return nil, err
			}
			return h.Prefs.Save(p)
		}
	}
	// The blitz command: a link on PATH, or on Windows its folder on the
	// user's PATH.
	m["CLIStatus"] = func([]json.RawMessage) (any, error) { return CurrentCLIStatus(), nil }
	m["InstallCLI"] = func([]json.RawMessage) (any, error) { return InstallCLI() }
	m["ReinstallCLI"] = func([]json.RawMessage) (any, error) { return ReinstallCLI() }
	if u := h.Updates; u != nil {
		// CheckUpdate(force): when the app opens (force false: GitHub is
		// asked only five days after the last check) or Check now (true).
		m["CheckUpdate"] = func(args []json.RawMessage) (any, error) {
			var force bool
			if len(args) > 0 {
				if err := arg(args, 0, &force); err != nil {
					return nil, err
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return u.Check(ctx, force), nil
		}
		m["UpdateStatus"] = func([]json.RawMessage) (any, error) { return u.Status(), nil }
		m["DownloadUpdate"] = func([]json.RawMessage) (any, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			return u.Download(ctx), nil
		}
		m["ApplyUpdate"] = func([]json.RawMessage) (any, error) { return nil, u.Apply() }
	}
	return m
}

// startTray starts the tray where launchd hasn't (a tray already running
// keeps its lock, and the new one exits).
func (h Host) startTray() error {
	if goruntime.GOOS == "darwin" || h.StartTray == nil {
		return nil
	}
	return h.StartTray()
}

// Repair rewrites login entries that name a program no longer there (an
// install that moved, as the desktop app's did in v0.7.0): the tray's,
// which it then starts, and the service's. What it can't repair it
// returns, joined.
func (h Host) Repair() error {
	var errs []error
	if fixed, err := loginitem.RepairTray(h.Tray); err != nil {
		errs = append(errs, fmt.Errorf("repairing the tray's login entry: %w", err))
	} else if fixed {
		errs = append(errs, h.startTray())
	}
	if goruntime.GOOS != "windows" {
		if bin, err := loginitem.FindService(h.Beside); err == nil {
			if _, err := loginitem.RepairService(bin); err != nil {
				errs = append(errs, fmt.Errorf("repairing the service's login item: %w", err))
			}
		}
	}
	return errors.Join(errs...)
}

// status is the service's status as the page shows it. On Windows the
// service starts at login with the tray, which starts it.
func (h Host) status() ServiceStatus {
	bin, _ := loginitem.FindService(h.Beside)
	installed := loginitem.Installed()
	if goruntime.GOOS == "windows" {
		installed = loginitem.TrayInstalled()
	}
	return ServiceStatus{Running: socket.Running(h.Socket), Installed: installed, Socket: h.Socket, Service: bin, Tray: h.Tray, TrayInstalled: loginitem.TrayInstalled()}
}

// installService makes the service start at login, and starts it: on
// Windows the tray starts at login, and starts the service.
func (h Host) installService() error {
	if goruntime.GOOS == "windows" {
		if err := loginitem.InstallTray(h.Tray); err != nil {
			return err
		}
		if h.Service.Running() {
			return nil
		}
		return h.Service.Start()
	}
	bin, err := loginitem.FindService(h.Beside)
	if err != nil {
		return err
	}
	return loginitem.Install(bin)
}

// License is one of the license texts the desktop app shows: "notice"
// (the NOTICE), "full" (the Apache License) or "third-party" (the notices
// of the software the app includes).
func License(which string) (string, error) {
	switch which {
	case "notice":
		return legal.Notice, nil
	case "full":
		return legal.License, nil
	case "third-party":
		return legal.ThirdParty, nil
	}
	return "", fmt.Errorf("unknown license text %q", which)
}

// ProgramExists reports whether path is a file: the program a running
// service started from may have been removed since (DSK-51a).
func ProgramExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// arg decodes the i-th argument into v.
func arg(args []json.RawMessage, i int, v any) error {
	if i >= len(args) {
		return fmt.Errorf("missing argument %d", i+1)
	}
	return json.Unmarshal(args[i], v)
}

// maxHostRequest bounds a call's arguments: paths and flags.
const maxHostRequest = 64 << 10

// Handler serves the page's calls under pageserver.HostPrefix.
func (h Host) Handler() http.Handler {
	methods := h.methods()
	names := make([]string, 0, len(methods))
	for n := range methods {
		names = append(names, n)
	}
	slices.Sort(names)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, pageserver.HostPrefix)
		if name == "info" && r.Method == http.MethodGet {
			reply(w, map[string]any{"methods": names}, nil)
			return
		}
		m, ok := methods[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		var args []json.RawMessage
		body, err := io.ReadAll(io.LimitReader(r.Body, maxHostRequest))
		if err == nil && len(body) > 0 {
			err = json.Unmarshal(body, &args)
		}
		if err != nil {
			reply(w, nil, errors.New("the arguments aren't a JSON array"))
			return
		}
		res, err := m(args)
		reply(w, res, err)
	})
}

// reply writes a call's result, or its error.
func reply(w http.ResponseWriter, res any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
