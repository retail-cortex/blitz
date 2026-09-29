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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// openCommand opens a folder in the file manager, or a document in its
// viewer.
var openCommand = func(dir string) *exec.Cmd {
	if goruntime.GOOS == "darwin" {
		return exec.Command("open", dir)
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

// viewable are the documents OpenDocument opens: images and PDFs, which
// the system shows in a viewer and never runs.
var viewable = map[string]bool{".pdf": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true, ".ico": true, ".svg": true}

// OpenDocument shows an image or PDF in the system's viewer (where the
// window's own preview falls short). Other files are refused: the page
// can't make it run one.
func (a *App) OpenDocument(path string) error {
	if !viewable[strings.ToLower(filepath.Ext(path))] {
		return fmt.Errorf("not opening %s: only images and PDFs open", path)
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
