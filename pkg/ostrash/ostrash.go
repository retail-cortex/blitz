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

// Package ostrash moves files to the system's trash, where the person can
// see, restore or empty them as any other deleted file (spec_029_files
// FIL-37a), and puts them back. On macOS it asks the system
// (NSFileManager's trashItemAtURL, through osascript, since Blitz is
// built without cgo), so Finder's Put Back works; on Linux and the BSDs
// it follows the freedesktop.org Trash specification. Elsewhere, and for a
// file the trash can't take, it says so (ErrUnavailable), and the caller
// keeps its own.
package ostrash

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ErrUnavailable is a trash this system doesn't have, or one that can't
// take the file (another disk than the home's, on Linux).
var ErrUnavailable = errors.New("the system's trash isn't available here")

// osascript runs the macOS script; a variable for tests.
var osascript = "/usr/bin/osascript"

// macTimeout is how long macOS has to move a file to the Trash.
const macTimeout = 30 * time.Second

// macScript trashes argv[0] and prints where it went (JavaScript for
// Automation, through the Objective-C bridge). The out-parameters are
// $(), nil objects the call fills; Ref()'s crash osascript reading them.
const macScript = `ObjC.import("Foundation");
function run(argv) {
  const out = $(), err = $();
  const ok = $.NSFileManager.defaultManager.trashItemAtURLResultingItemURLError($.NSURL.fileURLWithPath(argv[0]), out, err);
  if (!ok) throw new Error(err.localizedDescription.js);
  return out.path.js;
}`

// fake is the trash a test set (Fake): a folder standing in for the
// system's.
var fake struct {
	sync.Mutex
	dir string
	n   int
}

// Fake makes Trash move files into dir, as the system's trash would, until
// the returned func is called: for tests, which otherwise get no system
// trash at all (ErrUnavailable), so they never fill the person's.
func Fake(dir string) (undo func()) {
	fake.Lock()
	fake.dir, fake.n = dir, 0
	fake.Unlock()
	return func() {
		fake.Lock()
		fake.dir = ""
		fake.Unlock()
	}
}

// fakeDir is the fake trash, or "" for the system's.
func fakeDir() string {
	fake.Lock()
	defer fake.Unlock()
	return fake.dir
}

// inTest reports whether a test runs without asking for the real trash
// (BLITZ_TEST_SYSTEM_TRASH=1).
func inTest() bool { return testing.Testing() && os.Getenv("BLITZ_TEST_SYSTEM_TRASH") != "1" }

// Available reports whether this system has a trash Trash uses.
func Available() bool {
	if fakeDir() != "" {
		return true
	}
	if inTest() {
		return false
	}
	switch runtime.GOOS {
	case "darwin":
		_, err := os.Stat(osascript)
		return err == nil
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly":
		return homeTrash() != ""
	}
	return false
}

// Trash moves path (a file or a folder) to the system's trash and returns
// where it is there, for Restore.
func Trash(ctx context.Context, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); err != nil {
		return "", err
	}
	if dir := fakeDir(); dir != "" {
		fake.Lock()
		fake.n++
		where := filepath.Join(dir, strconv.Itoa(fake.n), filepath.Base(abs))
		fake.Unlock()
		if err := os.MkdirAll(filepath.Dir(where), 0o700); err != nil {
			return "", err
		}
		return where, os.Rename(abs, where)
	}
	if inTest() {
		return "", ErrUnavailable
	}
	switch runtime.GOOS {
	case "darwin":
		return macTrash(ctx, abs)
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly":
		if dir := homeTrash(); dir != "" {
			return freedesktopTrash(abs, dir, time.Now())
		}
	}
	return "", ErrUnavailable
}

// Restore puts what Trash moved to where back at to, which must not
// exist; the freedesktop trash's record of it goes too.
func Restore(where, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("%s is there already", to)
	}
	if err := os.Rename(where, to); err != nil {
		return err
	}
	// A freedesktop trash's record: Trash/info/<name>.trashinfo.
	files := filepath.Dir(where)
	if filepath.Base(files) == "files" {
		_ = os.Remove(filepath.Join(filepath.Dir(files), "info", filepath.Base(where)+".trashinfo"))
	}
	return nil
}

func macTrash(ctx context.Context, abs string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, macTimeout)
	defer cancel()
	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, osascript, "-l", "JavaScript", "-e", macScript, abs)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		// "execution error: Error: Error: … (-2700)": the system's reason.
		if i := strings.LastIndex(msg, "Error: "); i >= 0 {
			msg = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(msg[i+len("Error: "):]), "(-2700)"))
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("moving %s to the Trash: %s", filepath.Base(abs), msg)
	}
	where := strings.TrimSpace(string(out))
	if where == "" {
		return "", fmt.Errorf("moving %s to the Trash: the system didn't say where it went", filepath.Base(abs))
	}
	return where, nil
}

// homeTrash is the home trash: $XDG_DATA_HOME/Trash, else
// ~/.local/share/Trash; "" without a home.
func homeTrash() string {
	if d := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, "Trash")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "Trash")
}

// freedesktopTrash moves abs into the trash at dir as the specification
// says: its record (info/<name>.trashinfo, made first, so the name is
// claimed), then the file (files/<name>). A file on another device than
// the trash is ErrUnavailable (the specification's per-disk trashes aren't
// used).
func freedesktopTrash(abs, dir string, now time.Time) (string, error) {
	files, info := filepath.Join(dir, "files"), filepath.Join(dir, "info")
	for _, d := range []string{files, info} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return "", err
		}
	}
	if !sameDevice(abs, dir) {
		return "", ErrUnavailable
	}
	base := filepath.Base(abs)
	record := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", escapePath(abs), now.Format("2006-01-02T15:04:05"))
	for n := 1; n < 10000; n++ {
		name := base
		if n > 1 {
			ext := filepath.Ext(base)
			name = strings.TrimSuffix(base, ext) + "." + strconv.Itoa(n) + ext
		}
		rec := filepath.Join(info, name+".trashinfo")
		f, err := os.OpenFile(rec, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.WriteString(record)
		cerr := f.Close()
		if err := errors.Join(werr, cerr); err != nil {
			_ = os.Remove(rec)
			return "", err
		}
		where := filepath.Join(files, name)
		if _, err := os.Lstat(where); err == nil {
			_ = os.Remove(rec)
			continue
		}
		if err := os.Rename(abs, where); err != nil {
			_ = os.Remove(rec)
			if errors.Is(err, syscall.EXDEV) {
				return "", ErrUnavailable
			}
			return "", err
		}
		return where, nil
	}
	return "", fmt.Errorf("the trash has too many files named %s", base)
}

// escapePath is p as a trashinfo's Path: each part percent-escaped, the
// slashes kept.
func escapePath(p string) string {
	parts := strings.Split(filepath.ToSlash(p), "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}
