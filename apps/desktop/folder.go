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
	goruntime "runtime"
)

// openCommand opens a folder in the system's file manager.
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
