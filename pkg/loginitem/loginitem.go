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

// Package loginitem starts the Blitz service (blitzd) at login and
// controls it: a launchd agent on macOS, a systemd user unit on Linux.
// The CLI (`blitz service …`) and the desktop app both use it, so the app
// needs no CLI to install, restart or stop the service.
package loginitem

import (
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
)

const (
	// Label is the launchd agent's label (macOS).
	Label = "dev.blitz.service"
	// Unit is the systemd user unit (Linux).
	Unit = "blitz.service"
)

// ErrUnsupported: login items exist only on macOS and Linux.
var ErrUnsupported = fmt.Errorf("starting the service at login isn't supported on %s: run blitzd yourself", goruntime.GOOS)

// RunSystem runs a system command (launchctl, systemctl); tests replace it.
var RunSystem = func(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Path is where the login item is written.
func Path() (string, error) {
	switch goruntime.GOOS {
	case "darwin":
		return config.ExpandHome("~/Library/LaunchAgents/" + Label + ".plist"), nil
	case "linux":
		return config.ExpandHome("~/.config/systemd/user/" + Unit), nil
	}
	return "", ErrUnsupported
}

// Installed reports whether the login item exists.
func Installed() bool {
	p, err := Path()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// LogFile is where the service's output goes on macOS (systemd keeps
// it in the journal on Linux).
func LogFile() string { return config.ExpandHome("~/.blitz/logs/service.log") }

func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// Install writes the login item for blitzd (an absolute path) and starts
// it now, replacing a previous item and stopping the service it ran.
func Install(blitzd string) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	switch goruntime.GOOS {
	case "darwin":
		if err := os.MkdirAll(filepath.Dir(LogFile()), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(launchdPlist(blitzd, LogFile())), 0o644); err != nil {
			return err
		}
		_ = RunSystem("launchctl", "bootout", domain()+"/"+Label) // a previous install
		return RunSystem("launchctl", "bootstrap", domain(), path)
	default: // linux
		if err := os.WriteFile(path, []byte(systemdUnitFile(blitzd)), 0o644); err != nil {
			return err
		}
		if err := RunSystem("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		if err := RunSystem("systemctl", "--user", "enable", Unit); err != nil {
			return err
		}
		// restart, not start: a running unit keeps its old program otherwise.
		return RunSystem("systemctl", "--user", "restart", Unit)
	}
}

// Uninstall stops the service the login item runs and removes the item.
func Uninstall() error {
	path, err := Path()
	if err != nil {
		return err
	}
	switch goruntime.GOOS {
	case "darwin":
		_ = RunSystem("launchctl", "bootout", domain()+"/"+Label)
	case "linux":
		_ = RunSystem("systemctl", "--user", "disable", "--now", Unit)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if goruntime.GOOS == "linux" {
		_ = RunSystem("systemctl", "--user", "daemon-reload")
	}
	return nil
}

// Stop stops the service the login item runs, until the next login (or
// Start, or Install).
func Stop() error {
	switch goruntime.GOOS {
	case "darwin":
		return RunSystem("launchctl", "bootout", domain()+"/"+Label)
	case "linux":
		return RunSystem("systemctl", "--user", "stop", Unit)
	}
	return ErrUnsupported
}

// launchdPlist is the launchd agent that runs bin (blitzd) and restarts it
// if it exits with an error.
func launchdPlist(bin, logFile string) string {
	x := html.EscapeString
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + x(bin) + `</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>StandardOutPath</key>
	<string>` + x(logFile) + `</string>
	<key>StandardErrorPath</key>
	<string>` + x(logFile) + `</string>
</dict>
</plist>
`
}

// systemdUnitFile is the user unit that runs bin (blitzd).
func systemdUnitFile(bin string) string {
	return `[Unit]
Description=Blitz service (workspaces and scheduled workers)

[Service]
ExecStart=` + strconv.Quote(bin) + `
Restart=on-failure

[Install]
WantedBy=default.target
`
}

// FindService finds blitzd: in dirs (beside the program asking, as
// released and bundled), else on PATH. The result has symlinks resolved,
// so a login item keeps working when the link moves.
func FindService(dirs ...string) (string, error) {
	name := "blitzd"
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return filepath.EvalSymlinks(p)
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return filepath.EvalSymlinks(p)
	}
	return "", errors.New("blitzd, the Blitz service, isn't installed beside this program or on PATH")
}

// Beside is the directory of the running program (symlinks resolved), for
// FindService.
func Beside() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return ""
	}
	return filepath.Dir(exe)
}
