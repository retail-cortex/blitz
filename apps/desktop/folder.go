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
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"
)

// openCommand opens a folder in the file manager, or a document in its
// viewer.
var openCommand = func(dir string) *exec.Cmd {
	switch goruntime.GOOS {
	case "darwin":
		return exec.Command("open", dir)
	case "windows":
		return exec.Command("explorer", dir)
	}
	return exec.Command("xdg-open", dir)
}

// OpenFolder shows a folder (the logs', for one) in the file manager. Only
// an existing directory opens: the page can't make it run a file.
func (a *App) OpenFolder(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("not opening %s: not a folder", dir)
	}
	cmd := openCommand(dir)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opening %s: %w", dir, err)
	}
	go func() { _ = cmd.Wait() }() // reap it
	return nil
}

// viewable are the documents OpenDocument opens: images, PDFs, sound and
// video, which the system shows or plays in a viewer and never runs.
var viewable = map[string]bool{".pdf": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true, ".ico": true, ".svg": true,
	".heic": true, ".heif": true,
	".mp3": true, ".wav": true, ".m4a": true, ".aac": true, ".ogg": true, ".oga": true, ".opus": true, ".flac": true, ".weba": true,
	".mp4": true, ".m4v": true, ".mov": true, ".webm": true, ".avi": true, ".wmv": true, ".flv": true, ".mpeg": true, ".mpg": true, ".3gp": true}

// OpenDocument shows an image, PDF, sound or video in the system's viewer (where the
// window's own preview falls short). Other files are refused: the page
// can't make it run one.
func (a *App) OpenDocument(path string) error {
	if !viewable[strings.ToLower(filepath.Ext(path))] {
		return fmt.Errorf("not opening %s: only images, PDFs, sound and video open", path)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("not opening %s: not a file", path)
	}
	cmd := openCommand(path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// revealCommand shows path selected in its folder, in the file manager of
// goos. Linux has none that all file managers answer, so it asks over
// D-Bus (org.freedesktop.FileManager1, which Dolphin, Files, Nemo, Caja
// and Thunar implement) and waits for the answer.
func revealCommand(ctx context.Context, goos, path string) *exec.Cmd {
	switch goos {
	case "darwin":
		return exec.CommandContext(ctx, "open", "-R", path)
	case "windows":
		return exec.CommandContext(ctx, "explorer", "/select,", path)
	}
	// dbus-send splits arrays at commas.
	uri := strings.ReplaceAll((&url.URL{Scheme: "file", Path: path}).String(), ",", "%2C")
	return exec.CommandContext(ctx, "dbus-send", "--session", "--print-reply", "--dest=org.freedesktop.FileManager1",
		"/org/freedesktop/FileManager1", "org.freedesktop.FileManager1.ShowItems", "array:string:"+uri, "string:")
}

// reveal runs a reveal command: macOS's and Windows's in the background,
// Linux's to its answer (it fails without a file manager on the bus).
var reveal = func(path string) error {
	if goruntime.GOOS != "linux" {
		cmd := revealCommand(context.Background(), goruntime.GOOS, path)
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return revealCommand(ctx, "linux", path).Run()
}

// RevealPath shows a file or folder selected in the system's file manager
// (Finder, Explorer, Dolphin, …); where that can't be asked, it opens the
// folder the path is in. Nothing is run: the path must exist, and it is
// only shown.
func (a *App) RevealPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("not showing %s: not an absolute path", path)
	}
	path = filepath.Clean(path)
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	if err := reveal(path); err == nil {
		return nil
	}
	return a.OpenFolder(filepath.Dir(path))
}

// fileManagers names Linux file managers by their desktop file.
var fileManagers = map[string]string{
	"org.kde.dolphin":     "Dolphin",
	"dolphin":             "Dolphin",
	"org.gnome.nautilus":  "Files",
	"nautilus":            "Files",
	"nemo":                "Nemo",
	"caja":                "Caja",
	"org.xfce.thunar":     "Thunar",
	"thunar":              "Thunar",
	"pcmanfm":             "PCManFM",
	"pcmanfm-qt":          "PCManFM",
	"io.elementary.files": "Files",
}

// fileManagerName is what the file manager of goos is called, given the
// desktop file Linux opens folders with (xdg-mime's answer); "" when it
// isn't one we know (the page says "file manager").
func fileManagerName(goos, desktopFile string) string {
	switch goos {
	case "darwin":
		return "Finder"
	case "windows":
		return "File Explorer"
	}
	id := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(desktopFile), ".desktop"))
	return fileManagers[id]
}

// folderHandler asks which desktop file opens folders (Linux).
var folderHandler = func() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xdg-mime", "query", "default", "inode/directory").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

var fileManager = sync.OnceValue(func() string {
	if goruntime.GOOS != "linux" {
		return fileManagerName(goruntime.GOOS, "")
	}
	return fileManagerName("linux", folderHandler())
})

// FileManager is the name of the system's file manager, for "Show in …"
// ("" when it has none we know by name).
func (a *App) FileManager() string {
	return fileManager()
}
