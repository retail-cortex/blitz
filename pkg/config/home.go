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

package config

import (
	"os"
	"path/filepath"
)

// HomeWorkspace is the folder the user's global workers run in
// (~/.blitz/home, spec_workers_023 WK-60): a workspace of its own, for
// work that belongs to no project.
func HomeWorkspace(prefixDir string) string {
	return filepath.Join(ConfigDir(prefixDir), "home")
}

// EnsureHomeWorkspace makes the home workspace when it's missing and
// returns it, symlinks resolved (as workspaces are keyed).
func EnsureHomeWorkspace(prefixDir string) (string, error) {
	dir := HomeWorkspace(prefixDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(dir)
}

// UserWorkersDir holds the user's global workers,
// ~/.blitz/workers/<name>/WORKER.md.
func UserWorkersDir(prefixDir string) string {
	return filepath.Join(ConfigDir(prefixDir), "workers")
}

// IsHomeWorkspace reports whether dir is the home workspace, symlinks
// resolved.
func IsHomeWorkspace(prefixDir, dir string) bool {
	home := HomeWorkspace(prefixDir)
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return dir != "" && filepath.Clean(dir) == filepath.Clean(home)
}
