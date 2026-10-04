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

// Package shellpath finds programs as the user's terminal would: on the
// PATH an interactive login shell sets, which a GUI app or a login item
// doesn't inherit (Homebrew, the Google Cloud SDK, ~/.local/bin).
package shellpath

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// LoginShell is the user's shell ($SHELL, else the system's default).
func LoginShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

// pathMarker starts the line UserPath reads, among whatever else the
// shell's startup files print.
const pathMarker = "BLITZ_PATH="

// UserPath is PATH as a new terminal has it: what an interactive login
// shell prints, else (it fails, or takes over 5 s) this process's PATH.
func UserPath(shell string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// fish joins a quoted $PATH with colons too.
	out, err := exec.CommandContext(ctx, shell, "-i", "-l", "-c", `printf '`+pathMarker+`%s\n' "$PATH"`).Output()
	if err == nil {
		for _, l := range strings.Split(string(out), "\n") {
			if v, ok := strings.CutPrefix(l, pathMarker); ok {
				return filepath.SplitList(v)
			}
		}
	}
	return filepath.SplitList(os.Getenv("PATH"))
}

// LookPathIn is the first executable file named name in dirs ("" if
// none), as the shell would find it.
func LookPathIn(dirs []string, name string) string {
	for _, d := range dirs {
		if d == "" {
			continue
		}
		p := filepath.Join(d, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// Find is the program named name on this process's PATH, else on the
// user's terminal's ("" if on neither).
func Find(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return LookPathIn(UserPath(LoginShell()), name)
}
