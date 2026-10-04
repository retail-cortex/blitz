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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/retail-cortex/blitz/pkg/shellpath"
)

// CLIStatus says where the blitz command is: the one that comes with this
// app, and the one a new terminal would run.
type CLIStatus struct {
	// CLI is the blitz beside this app ("" if none was found).
	CLI string `json:"cli"`
	// OnPath is the blitz on the user's PATH, as their shell sets it (""
	// if none), and Installed whether it's this app's.
	OnPath    string `json:"on_path"`
	Installed bool   `json:"installed"`
}

// CLIInstall is what InstallCLI did: the link it made, and the shell
// startup file it added the link's directory to PATH in ("" if the
// directory was on PATH already).
type CLIInstall struct {
	Link    string `json:"link"`
	Profile string `json:"profile"`
	// Removed lists the dead blitz links a reinstall took off PATH.
	Removed []string `json:"removed,omitempty"`
}

// CLIStatus reports where this app's blitz is and whether a terminal
// finds it (it asks the login shell, since the app's own PATH isn't the
// terminal's, on macOS especially).
func (a *App) CLIStatus() CLIStatus {
	cli, _ := findCLI()
	return cliStatus(cli, userPath(loginShell()))
}

// InstallCLI puts this app's blitz on the user's PATH: a link in a
// directory on it, else in ~/.local/bin, added to PATH in the shell's
// startup file. It does nothing when a blitz is on PATH already.
func (a *App) InstallCLI() (CLIInstall, error) {
	cli, err := findCLI()
	if err != nil {
		return CLIInstall{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return CLIInstall{}, err
	}
	shell := loginShell()
	return installCLI(cli, home, shell, goruntime.GOOS, userPath(shell))
}

// ReinstallCLI links this app's blitz again, for when the one on PATH is
// another build's or a dead link (a developer's old bazel-bin, an app
// since moved): it removes the dead blitz links on PATH, points the blitz
// link the shell finds at this app's, and links it as InstallCLI does when
// there's none. A blitz on PATH that's a file, not a link, is the user's
// and is left alone.
func (a *App) ReinstallCLI() (CLIInstall, error) {
	cli, err := findCLI()
	if err != nil {
		return CLIInstall{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return CLIInstall{}, err
	}
	shell := loginShell()
	return reinstallCLI(cli, home, shell, goruntime.GOOS, userPath(shell))
}

// reinstallCLI is ReinstallCLI's work, for cli, home, shell, goos and the
// shell's path.
func reinstallCLI(cli, home, shell, goos string, path []string) (CLIInstall, error) {
	var removed []string
	for _, d := range path {
		if d == "" {
			continue
		}
		p := filepath.Join(d, "blitz")
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			continue // a live link: the shell may find it, below
		}
		if err := os.Remove(p); err != nil {
			return CLIInstall{Removed: removed}, fmt.Errorf("%s is a dead link and couldn't be removed: %w", p, err)
		}
		removed = append(removed, p)
	}
	found := lookPathIn(path, "blitz")
	if found == "" {
		out, err := installCLI(cli, home, shell, goos, path)
		out.Removed = removed
		return out, err
	}
	out := CLIInstall{Link: found, Removed: removed}
	if info, err := os.Lstat(found); err != nil || info.Mode()&os.ModeSymlink == 0 {
		return out, fmt.Errorf("%s is a program, not a link: remove it to use this app's blitz", found)
	}
	if target, err := filepath.EvalSymlinks(found); err == nil && target == cli {
		return out, nil
	}
	// A new link beside the old, renamed over it: never no blitz at all.
	tmp := found + ".blitz-new"
	os.Remove(tmp)
	if err := os.Symlink(cli, tmp); err != nil {
		return out, err
	}
	if err := os.Rename(tmp, found); err != nil {
		os.Remove(tmp)
		return out, err
	}
	return out, nil
}

func cliStatus(cli string, path []string) CLIStatus {
	s := CLIStatus{CLI: cli, OnPath: lookPathIn(path, "blitz")}
	if s.OnPath != "" && cli != "" {
		resolved, err := filepath.EvalSymlinks(s.OnPath)
		s.Installed = err == nil && resolved == cli
	}
	return s
}

// findCLI finds the blitz that goes with this app, where findService
// finds blitzd, but not on PATH: that's what it's for.
func findCLI() (string, error) {
	var dirs []string
	if d := loginitem.Beside(); d != "" {
		dirs = append(dirs, d)
	}
	for _, root := range runfilesRoots() {
		dirs = append(dirs, filepath.Join(root, "_main", "apps", "cli"))
	}
	for _, d := range dirs {
		p := filepath.Join(d, "blitz")
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return filepath.EvalSymlinks(p)
		}
	}
	return "", errors.New("the blitz command isn't installed beside the app: reinstall Blitz")
}

// installCLI links cli into a directory on path, else into
// home/.local/bin, which it adds to PATH in shell's startup file.
func installCLI(cli, home, shell, goos string, path []string) (CLIInstall, error) {
	if p := lookPathIn(path, "blitz"); p != "" {
		return CLIInstall{Link: p}, nil
	}
	dir, onPath := linkDir(home, path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return CLIInstall{}, err
	}
	link := filepath.Join(dir, "blitz")
	// Not on PATH, but there: a link left by an app since moved, which
	// is replaced; anything else is the user's.
	if info, err := os.Lstat(link); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return CLIInstall{}, fmt.Errorf("%s is there already and isn't a link: remove it, or put its directory on PATH", link)
		}
		if err := os.Remove(link); err != nil {
			return CLIInstall{}, err
		}
	}
	if err := os.Symlink(cli, link); err != nil {
		return CLIInstall{}, err
	}
	out := CLIInstall{Link: link}
	if !onPath {
		profile, err := addToPath(home, shell, goos, dir)
		if err != nil {
			return out, fmt.Errorf("linked %s, but couldn't add %s to PATH: %w", link, dir, err)
		}
		out.Profile = profile
	}
	return out, nil
}

// linkDir is where the link goes: the first of ~/.local/bin, ~/bin and
// /usr/local/bin (if the user can write there) that's on path, else
// ~/.local/bin, which isn't.
func linkDir(home string, path []string) (string, bool) {
	local := filepath.Join(home, ".local", "bin")
	for _, d := range []string{local, filepath.Join(home, "bin"), "/usr/local/bin"} {
		if !slices.Contains(path, d) {
			continue
		}
		if strings.HasPrefix(d, home+string(filepath.Separator)) || writable(d) {
			return d, true
		}
	}
	return local, false
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".blitz-")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// addToPath adds dir to PATH in shell's startup file (the one a new
// terminal reads, for zsh, bash and fish; ~/.profile for others), unless
// the line is there already. It returns the file.
func addToPath(home, shell, goos, dir string) (string, error) {
	file, line := profileFor(home, shell, goos, dir)
	old, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if strings.Contains(string(old), line) {
		return file, nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	prefix := ""
	if len(old) > 0 && !strings.HasSuffix(string(old), "\n") {
		prefix = "\n"
	}
	_, err = fmt.Fprintf(f, "%s\n# Added by Blitz, for its blitz command.\n%s\n", prefix, line)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return file, err
}

// profileFor is shell's startup file and the line in it that puts dir on
// PATH. Terminals on macOS start login shells (bash reads
// ~/.bash_profile), on Linux interactive ones (~/.bashrc).
func profileFor(home, shell, goos, dir string) (string, string) {
	rel := dir
	if r, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(r, "..") {
		rel = "$HOME/" + r
	}
	switch filepath.Base(shell) {
	case "zsh":
		zdot := os.Getenv("ZDOTDIR")
		if zdot == "" {
			zdot = home
		}
		return filepath.Join(zdot, ".zshrc"), `export PATH="` + rel + `:$PATH"`
	case "bash":
		if goos == "darwin" {
			return filepath.Join(home, ".bash_profile"), `export PATH="` + rel + `:$PATH"`
		}
		return filepath.Join(home, ".bashrc"), `export PATH="` + rel + `:$PATH"`
	case "fish":
		return filepath.Join(home, ".config", "fish", "conf.d", "blitz.fish"), "fish_add_path --path " + rel
	}
	return filepath.Join(home, ".profile"), `export PATH="` + rel + `:$PATH"`
}

// loginShell, userPath and lookPathIn find programs as a terminal would
// (pkg/shellpath).
var (
	loginShell = shellpath.LoginShell
	userPath   = shellpath.UserPath
	lookPathIn = shellpath.LookPathIn
)
