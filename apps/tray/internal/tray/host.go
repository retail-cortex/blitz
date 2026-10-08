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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	goruntime "runtime"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/fileview"
	"github.com/retail-cortex/blitz/pkg/legal"
	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/retail-cortex/blitz/pkg/pageserver"
	"github.com/retail-cortex/blitz/pkg/socket"
)

// Host answers the page the tray serves (pageserver.HostPrefix) with what
// the desktop app's bindings do and a browser can't: the service's
// status and controls, starting at login, showing files, the licenses.
// The page calls POST /host/<Method> with its arguments as a JSON array
// and gets the result as JSON, or {"error": "…"} with status 500, the
// method names being the desktop app's (window.go.main.App); GET
// /host/info lists them.
type Host struct {
	Actions Actions
	// Version is the tray's, which the page shows as the app's.
	Version string
	// Socket is the service's.
	Socket string
	// Tray is the tray's own program, which starts at login.
	Tray string
}

// ServiceStatus is the desktop app's ServiceStatus, as the page reads it.
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
	return map[string]hostMethod{
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
				return nil, loginitem.InstallTray(h.Tray)
			}
			return nil, loginitem.UninstallTray()
		},
		"StopService":    func([]json.RawMessage) (any, error) { return nil, h.Actions.Stop() },
		"RestartService": func([]json.RawMessage) (any, error) { return nil, h.Actions.Restart() },
		"ProgramExists": func(args []json.RawMessage) (any, error) {
			var p string
			if err := arg(args, 0, &p); err != nil {
				return nil, err
			}
			return isFile(p), nil
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
			return license(which)
		},
	}
}

// status is the service's status as the page shows it. On Windows the
// service starts at login with the tray, which starts it.
func (h Host) status() ServiceStatus {
	bin, _ := loginitem.FindService(h.Actions.Beside)
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
		if h.Actions.Status().Running {
			return nil
		}
		return h.Actions.Start()
	}
	bin, err := loginitem.FindService(h.Actions.Beside)
	if err != nil {
		return err
	}
	return loginitem.Install(bin)
}

// license is one of the license texts the desktop app shows.
func license(which string) (string, error) {
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
