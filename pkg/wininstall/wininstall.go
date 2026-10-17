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

// Package wininstall installs the desktop app on Windows for one user,
// without an administrator (spec_electron_038 EL-81): the app's zip,
// carried at the end of the setup program (Payload), unpacked in
// %LOCALAPPDATA%\Programs\Blitz (Unpack, Place), and its entry in Add or
// remove programs (Entry, in the registry on Windows). The setup program
// (apps/electron/winsetup) and the tray's updates use it; what isn't the
// registry works, and is tested, everywhere.
package wininstall

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// UninstallKey is the app's entry in Add or remove programs, under
	// HKEY_CURRENT_USER.
	UninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Blitz`
	// Uninstaller is the setup program's name in the installed app, where
	// it uninstalls (it's in the zip, so an update keeps it).
	Uninstaller = "Uninstall Blitz.exe"
	// App is the app's program in its folder.
	App = "Blitz.exe"
	// magic ends a setup program that carries the zip: then the zip's
	// length (8 bytes, little-endian) and magic.
	magic = "BLITZZIP"
	// trailer is the length of what follows the zip.
	trailer = 8 + len(magic)
)

// ErrNoPayload means the program carries no zip: it's the uninstaller.
var ErrNoPayload = errors.New("no app in this program")

// DefaultRoot is where the app goes: %LOCALAPPDATA%\Programs\Blitz, as
// other per-user installs do.
func DefaultRoot(localAppData string) string {
	return filepath.Join(localAppData, "Programs", "Blitz")
}

// AppendPayload writes zip after what's already been written to w (the
// setup program), followed by its length and magic.
func AppendPayload(w io.Writer, zipData []byte) error {
	if _, err := w.Write(zipData); err != nil {
		return err
	}
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(len(zipData)))
	if _, err := w.Write(n[:]); err != nil {
		return err
	}
	_, err := io.WriteString(w, magic)
	return err
}

// Payload is the zip at the end of a program of size bytes, or
// ErrNoPayload.
func Payload(exe io.ReaderAt, size int64) (*zip.Reader, error) {
	if size < int64(trailer) {
		return nil, ErrNoPayload
	}
	t := make([]byte, trailer)
	if _, err := exe.ReadAt(t, size-int64(trailer)); err != nil {
		return nil, err
	}
	if string(t[8:]) != magic {
		return nil, ErrNoPayload
	}
	n := int64(binary.LittleEndian.Uint64(t[:8]))
	if n <= 0 || n > size-int64(trailer) {
		return nil, fmt.Errorf("the app in this program is damaged")
	}
	return zip.NewReader(io.NewSectionReader(exe, size-int64(trailer)-n, n), n)
}

// Unpack writes the zip's files under dest, without the folder they're in
// (the zip's Blitz\). A file that would land outside dest is refused.
func Unpack(zr *zip.Reader, dest, strip string) error {
	for _, f := range zr.File {
		name := strings.TrimPrefix(filepath.ToSlash(f.Name), strip+"/")
		if name == "" || name == strip {
			continue
		}
		p := filepath.Join(dest, filepath.FromSlash(name))
		if rel, err := filepath.Rel(dest, p); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s would be outside the app's folder", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := unpackFile(f, p); err != nil {
			return err
		}
	}
	return nil
}

func unpackFile(f *zip.File, p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	mode := os.FileMode(0o644)
	if f.Mode()&0o111 != 0 {
		mode = 0o755
	}
	w, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// Place puts the unpacked app (staged) at root: an app already there is
// moved aside first and removed after, so a failure leaves one in place.
func Place(staged, root string) error {
	old := ""
	if _, err := os.Stat(root); err == nil {
		old = root + ".old-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		if err := os.Rename(root, old); err != nil {
			return fmt.Errorf("moving the installed app aside (is Blitz still running?): %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return err
	}
	if err := os.Rename(staged, root); err != nil {
		if old != "" {
			_ = os.Rename(old, root)
		}
		return err
	}
	if old != "" {
		_ = os.RemoveAll(old) // a file still open stays behind; nothing uses it
	}
	return nil
}

// Size is how much the files under dir take, in KB (Add or remove
// programs shows it).
func Size(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return (n + 1023) / 1024
}

// Entry is the app's entry in Add or remove programs.
type Entry struct {
	Root    string
	Version string
	// SizeKB is what the app takes.
	SizeKB int64
}

// Strings are the entry's text values, by name.
func (e Entry) Strings() map[string]string {
	un := `"` + filepath.Join(e.Root, Uninstaller) + `"`
	return map[string]string{
		"DisplayName":          "Blitz",
		"DisplayVersion":       e.Version,
		"Publisher":            "Retail Cortex",
		"DisplayIcon":          filepath.Join(e.Root, App),
		"InstallLocation":      e.Root,
		"UninstallString":      un,
		"QuietUninstallString": un + " /S",
		"URLInfoAbout":         "https://retail-cortex.github.io/blitz/",
	}
}

// Numbers are the entry's number values: no Modify or Repair, and its size.
func (e Entry) Numbers() map[string]uint32 {
	return map[string]uint32{"NoModify": 1, "NoRepair": 1, "EstimatedSize": uint32(e.SizeKB)}
}

// TrayVersion is the version in blitz-tray --version's line ("" when it
// isn't one).
func TrayVersion(out string) string {
	f := strings.Fields(out)
	if len(f) != 2 || f[0] != "blitz-tray" {
		return ""
	}
	return f[1]
}
