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

package loginitem

import (
	"html"
	"os"
	goruntime "runtime"
	"strings"
)

// entryProgram is the program a login entry runs: an XDG autostart
// entry's Exec, a systemd unit's ExecStart or a launchd plist's first
// ProgramArguments string ("" when there's none).
func entryProgram(data string) string {
	if i := strings.Index(data, "<key>ProgramArguments</key>"); i >= 0 {
		rest := data[i:]
		a, b := strings.Index(rest, "<string>"), strings.Index(rest, "</string>")
		if a < 0 || b < a {
			return ""
		}
		return html.UnescapeString(rest[a+len("<string>") : b])
	}
	for _, line := range strings.Split(data, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || k != "Exec" && k != "ExecStart" {
			continue
		}
		return unquoteExec(v)
	}
	return ""
}

// unquoteExec is the program in an Exec or ExecStart value: the first
// word, or the quoted string it starts with (both backslash-escaped, as
// trayDesktopEntry and strconv.Quote write them).
func unquoteExec(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, `"`) {
		w, _, _ := strings.Cut(v, " ")
		return w
	}
	var b strings.Builder
	for i := 1; i < len(v); i++ {
		switch c := v[i]; c {
		case '\\':
			if i+1 < len(v) {
				i++
				b.WriteByte(v[i])
			}
		case '"':
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// TrayProgram is the program the tray's start-at-login entry runs (""
// when there's no entry, or it can't be read).
func TrayProgram() string {
	if goruntime.GOOS == "windows" {
		return runProgram()
	}
	p, err := TrayPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return entryProgram(string(data))
}

// ServiceProgram is the program the service's login item runs (""
// when there's none).
func ServiceProgram() string {
	p, err := Path()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return entryProgram(string(data))
}

// missing reports whether a login entry's program is gone (an install
// that moved, as the desktop app's did in v0.7.0).
func missing(program string) bool {
	if program == "" {
		return false
	}
	_, err := os.Stat(program)
	return err != nil
}

// RepairTray rewrites the tray's start-at-login entry when the program it
// names is gone, to run bin instead, and reports whether it did. The
// caller starts the tray (on macOS launchd has).
func RepairTray(bin string) (bool, error) {
	if bin == "" || !TrayInstalled() || !missing(TrayProgram()) {
		return false, nil
	}
	return true, InstallTray(bin)
}

// RepairService rewrites the service's login item when the program it
// names is gone, to run blitzd instead (and starts it), and reports
// whether it did.
func RepairService(blitzd string) (bool, error) {
	if blitzd == "" || !Installed() || !missing(ServiceProgram()) {
		return false, nil
	}
	return true, Install(blitzd)
}
