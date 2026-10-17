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

// Command winsetup installs Blitz on Windows for the person running it,
// without an administrator, and uninstalls it (spec_electron_038 EL-81).
// Released as Blitz_<version>_windows_x64_setup.exe: this program with
// the app's zip at its end (pack). Without a zip it's the uninstaller,
// "Uninstall Blitz.exe" in the installed app, which Add or remove
// programs runs.
//
//	Blitz_…_setup.exe [/S]           install (/S: silently)
//	"Uninstall Blitz.exe" [/S]       uninstall
package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/wininstall"
)

// version is the release's, stamped at build time.
var version = "dev"

// system is what the program asks of Windows; tests fake it.
type system interface {
	// Self is this program's path.
	Self() (string, error)
	// LocalAppData is %LOCALAPPDATA%.
	LocalAppData() string
	// TempDir is where the uninstaller copies itself to remove the app's folder.
	TempDir() string
	// StopApp stops the programs running from root: the app, the tray, the service.
	StopApp(root string) error
	// StartMenu and Desktop are the folders for the app's shortcuts.
	StartMenu() string
	Desktop() string
	// Shortcut makes link (a .lnk) open target.
	Shortcut(link, target string) error
	WriteEntry(wininstall.Entry) error
	DeleteEntry() error
	// RemoveRunValue stops the tray starting at login.
	RemoveRunValue() error
	// RemoveFromPath takes dir off the user's PATH.
	RemoveFromPath(dir string) error
	// Start runs a program and doesn't wait for it.
	Start(exe string, args ...string) error
	// Ask asks yes or no; Tell says how it went.
	Ask(text string) bool
	Tell(text string, failed bool)
	// DeleteLater removes path once this program has exited.
	DeleteLater(path string)
}

// options are the command line's.
type options struct {
	silent    bool
	uninstall bool
	// root is the app's folder, for the uninstaller's copy in %TEMP%.
	root string
}

func parse(args []string) options {
	var o options
	for i := 0; i < len(args); i++ {
		switch a := strings.ToLower(args[i]); a {
		case "/s", "-s", "--silent", "/silent":
			o.silent = true
		case "--uninstall", "/uninstall":
			o.uninstall = true
		case "--root":
			if i+1 < len(args) {
				o.root = args[i+1]
				i++
			}
		}
	}
	return o
}

func main() { os.Exit(run(os.Args[1:], newSystem())) }

// run installs when this program carries the app (and isn't asked to
// uninstall), else uninstalls; it returns the exit status.
func run(args []string, sys system) int {
	o := parse(args)
	self, err := sys.Self()
	if err != nil {
		return fail(sys, o, err)
	}
	f, err := os.Open(self)
	if err != nil {
		return fail(sys, o, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fail(sys, o, err)
	}
	zr, err := wininstall.Payload(f, info.Size())
	switch {
	case o.uninstall || errors.Is(err, wininstall.ErrNoPayload):
		return uninstall(sys, o, self)
	case err != nil:
		return fail(sys, o, err)
	}
	return install(sys, o, zr)
}

// fail says err (unless silent) and returns the failure's status.
func fail(sys system, o options, err error) int {
	if !o.silent {
		sys.Tell(err.Error(), true)
	}
	fmt.Fprintln(os.Stderr, "Blitz setup:", err)
	return 1
}

// install puts the app in %LOCALAPPDATA%\Programs\Blitz (an older one
// replaced), its shortcuts and its entry in Add or remove programs, and
// starts it.
func install(sys system, o options, zr *zip.Reader) int {
	root := wininstall.DefaultRoot(sys.LocalAppData())
	if !o.silent && !sys.Ask(fmt.Sprintf("Install Blitz %s for you?\n\nIt goes in %s. No administrator is needed, and Blitz keeps itself up to date.", version, root)) {
		return 1
	}
	if err := sys.StopApp(root); err != nil {
		return fail(sys, o, fmt.Errorf("stopping Blitz: %w", err))
	}
	staged := root + ".new"
	_ = os.RemoveAll(staged)
	if err := wininstall.Unpack(zr, staged, "Blitz"); err != nil {
		_ = os.RemoveAll(staged)
		return fail(sys, o, fmt.Errorf("unpacking Blitz: %w", err))
	}
	if err := wininstall.Place(staged, root); err != nil {
		_ = os.RemoveAll(staged)
		return fail(sys, o, err)
	}
	app := filepath.Join(root, wininstall.App)
	for _, link := range []string{filepath.Join(sys.StartMenu(), "Blitz.lnk"), filepath.Join(sys.Desktop(), "Blitz.lnk")} {
		if err := sys.Shortcut(link, app); err != nil {
			return fail(sys, o, fmt.Errorf("making the shortcut %s: %w", link, err))
		}
	}
	if err := sys.WriteEntry(wininstall.Entry{Root: root, Version: version, SizeKB: wininstall.Size(root)}); err != nil {
		return fail(sys, o, fmt.Errorf("adding Blitz to the installed programs: %w", err))
	}
	if !o.silent {
		if err := sys.Start(app); err != nil {
			return fail(sys, o, err)
		}
	}
	return 0
}

// uninstall removes what install made: the app's folder, its shortcuts,
// its entry, the tray's start at login and the blitz command's PATH
// entry. Settings and conversations (~/.blitz) stay. Run from inside the
// app's folder, it copies itself to %TEMP% and runs from there, so the
// folder can go.
func uninstall(sys system, o options, self string) int {
	root := o.root
	if root == "" {
		root = filepath.Dir(self)
		if !strings.EqualFold(filepath.Base(self), wininstall.Uninstaller) {
			root = wininstall.DefaultRoot(sys.LocalAppData())
		}
	}
	if o.root == "" && within(self, root) {
		tmp := filepath.Join(sys.TempDir(), fmt.Sprintf("blitz-uninstall-%d.exe", time.Now().UnixNano()))
		if err := copyFile(self, tmp); err != nil {
			return fail(sys, o, err)
		}
		args := []string{"--uninstall", "--root", root}
		if o.silent {
			args = append(args, "/S")
		}
		if err := sys.Start(tmp, args...); err != nil {
			return fail(sys, o, err)
		}
		return 0
	}
	if o.root != "" {
		defer sys.DeleteLater(self)
	}
	if !o.silent && !sys.Ask("Uninstall Blitz?\n\nYour settings and conversations (in .blitz in your home folder) are kept.") {
		return 1
	}
	if err := sys.StopApp(root); err != nil {
		return fail(sys, o, fmt.Errorf("stopping Blitz: %w", err))
	}
	var errs []error
	for _, link := range []string{filepath.Join(sys.StartMenu(), "Blitz.lnk"), filepath.Join(sys.Desktop(), "Blitz.lnk")} {
		if err := os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	errs = append(errs, sys.DeleteEntry(), sys.RemoveRunValue(), sys.RemoveFromPath(filepath.Join(root, "resources", "app", "bin")))
	errs = append(errs, removeAll(root))
	if err := errors.Join(errs...); err != nil {
		return fail(sys, o, err)
	}
	if !o.silent {
		sys.Tell("Blitz is uninstalled.", false)
	}
	return 0
}

// removeAll removes dir, trying again for a few seconds while the
// programs that ran from it let go of their files.
func removeAll(dir string) error {
	var err error
	for range 10 {
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
		time.Sleep(retryEvery)
	}
	return fmt.Errorf("removing %s: %w", dir, err)
}

// retryEvery is how long removeAll waits between tries (tests shorten it).
var retryEvery = 500 * time.Millisecond

// within reports whether path is inside dir.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func copyFile(from, to string) error {
	r, err := os.Open(from)
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}
