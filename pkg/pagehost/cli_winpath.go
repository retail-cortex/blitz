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
	"errors"
	"os"
	"strings"
)

// The blitz command on Windows: no links (they need an administrator, or
// Developer Mode) and no shell startup file, so its folder goes on the
// user's PATH (HKEY_CURRENT_USER\Environment), and Windows is told, so
// terminals opened from then on find it. The registry is reached through
// these, which the tests replace.
var (
	readUserPath   = registryUserPath
	readSystemPath = registrySystemPath
	writeUserPath  = registrySetUserPath
	pathChanged    = announcePathChange
)

// userPathKey is where the user's PATH is, as the install result names it.
const userPathKey = `HKEY_CURRENT_USER\Environment`

// errNotWindows is the registry's answer elsewhere.
var errNotWindows = errors.New("the user's PATH is in the registry on Windows only")

// windowsCLIStatus is where a terminal finds blitz.exe: the first folder
// of the system's PATH, then the user's, that has it.
func windowsCLIStatus(cli string) CLIStatus {
	s := CLIStatus{CLI: cli, Method: "path"}
	system, _ := readSystemPath()
	user, _ := readUserPath()
	for _, d := range append(splitWinPath(system), splitWinPath(user)...) {
		if hasBlitzExe(expandWin(d)) {
			s.OnPath = joinWin(expandWin(d), "blitz.exe")
			break
		}
	}
	s.Installed = s.OnPath != "" && cli != "" && sameWinPath(s.OnPath, cli)
	return s
}

// windowsInstallCLI puts cli's folder first on the user's PATH, and tells
// Windows. Installing does nothing when a blitz is on PATH already; again
// (Reinstall) also takes other copies of Blitz's folders off the user's
// PATH, those still there and those since gone, so a terminal finds this
// app's.
func windowsInstallCLI(cli string, again bool) (CLIInstall, error) {
	if st := windowsCLIStatus(cli); !again && st.OnPath != "" {
		return CLIInstall{Link: st.OnPath}, nil
	}
	dir := dirWin(cli)
	user, err := readUserPath()
	if err != nil {
		return CLIInstall{}, err
	}
	entries := splitWinPath(user)
	next := []string{dir}
	var removed []string
	for _, e := range entries {
		switch d := expandWin(e); {
		case sameWinPath(d, dir):
			// put first, above
		case again && (hasBlitzExe(d) || blitzBinGone(d)):
			removed = append(removed, e)
		default:
			next = append(next, e)
		}
	}
	out := CLIInstall{Link: cli, Profile: userPathKey, Removed: removed}
	if strings.Join(next, ";") == strings.Join(entries, ";") {
		return out, nil
	}
	if err := writeUserPath(strings.Join(next, ";")); err != nil {
		return CLIInstall{}, err
	}
	pathChanged()
	return out, nil
}

// RemoveFromUserPath takes dir out of the user's PATH on Windows: the
// uninstaller's (apps/electron/winsetup), for the folder Install put
// there (WIN-14). Nothing is written when it isn't there.
func RemoveFromUserPath(dir string) error {
	user, err := readUserPath()
	if err != nil {
		return err
	}
	entries := splitWinPath(user)
	var next []string
	for _, e := range entries {
		if !sameWinPath(expandWin(e), dir) {
			next = append(next, e)
		}
	}
	if len(next) == len(entries) {
		return nil
	}
	if err := writeUserPath(strings.Join(next, ";")); err != nil {
		return err
	}
	pathChanged()
	return nil
}

// splitWinPath is a PATH's folders, the empty ones left out.
func splitWinPath(v string) []string {
	var out []string
	for _, d := range strings.Split(v, ";") {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// expandWin expands %NAME% in a PATH entry (REG_EXPAND_SZ), leaving
// unknown names as they are.
func expandWin(p string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(p, '%')
		if i < 0 {
			break
		}
		j := strings.IndexByte(p[i+1:], '%')
		if j < 0 {
			break
		}
		name := p[i+1 : i+1+j]
		if v, ok := os.LookupEnv(name); ok && name != "" {
			b.WriteString(p[:i] + v)
		} else {
			b.WriteString(p[:i+2+j])
		}
		p = p[i+2+j:]
	}
	return b.String() + p
}

// sameWinPath reports whether two Windows paths name the same place:
// case and a trailing separator aside.
func sameWinPath(a, b string) bool {
	norm := func(p string) string { return strings.TrimRight(strings.ReplaceAll(p, "/", `\`), `\`) }
	return strings.EqualFold(norm(a), norm(b))
}

// dirWin is a Windows path's folder.
func dirWin(p string) string {
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		return p[:i]
	}
	return p
}

// joinWin is a folder and a name in it.
func joinWin(dir, name string) string { return strings.TrimRight(dir, `\/`) + `\` + name }

// hasBlitzExe reports whether dir has blitz.exe.
var hasBlitzExe = func(dir string) bool {
	info, err := os.Stat(joinWin(dir, "blitz.exe"))
	return err == nil && !info.IsDir()
}

// blitzBinGone reports whether dir was a copy of Blitz's (the Electron
// app's resources\app\bin), since moved or deleted.
func blitzBinGone(dir string) bool {
	if !strings.HasSuffix(strings.ToLower(strings.TrimRight(dir, `\/`)), `\resources\app\bin`) {
		return false
	}
	_, err := os.Stat(dir)
	return errors.Is(err, os.ErrNotExist)
}
