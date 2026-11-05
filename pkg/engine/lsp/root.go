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

package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// rootMarkers are the files that make a folder a language's project root
// (FS-61a), in tiers: the nearest folder holding one of a tier's files,
// from the file's folder up to the workspace's, is the root, and a later
// tier is looked for only when no earlier one is found. Go's go.work comes
// first, since a module in a workspace is served from the workspace. A
// language without markers, or a file with none above it, is served from
// the workspace's root.
var rootMarkers = map[string][][]string{
	"go":         {{"go.work"}, {"go.mod"}},
	"typescript": {{"tsconfig.json", "jsconfig.json", "package.json"}},
	"python":     {{"pyrightconfig.json", "pyproject.toml", "setup.py", "setup.cfg", ".venv"}},
	"rust":       {{"Cargo.toml"}},
}

// projectRoot is the folder language's server for path runs in: the
// nearest project root at or above path's folder inside the workspace,
// else the workspace's root (and for a path outside the workspace, such as
// a code block's scratch folder).
func (m *Manager) projectRoot(language, path string) string {
	dir := filepath.Dir(filepath.Clean(path))
	if !within(m.root, dir) {
		return m.root
	}
	for _, tier := range rootMarkers[language] {
		for d := dir; ; d = filepath.Dir(d) {
			for _, f := range tier {
				if _, err := os.Stat(filepath.Join(d, f)); err == nil {
					return d
				}
			}
			if d == m.root || filepath.Dir(d) == d {
				break
			}
		}
	}
	return m.root
}

// within reports whether dir is root or inside it.
func within(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// shown is dir as messages give it: relative to the workspace.
func (m *Manager) shown(dir string) string {
	if rel, err := filepath.Rel(m.root, dir); err == nil {
		return filepath.ToSlash(rel)
	}
	return dir
}

// tsLookPath finds tsc on PATH (a variable, for tests).
var tsLookPath = exec.LookPath

// tsserverLib is the TypeScript the typescript server is given for root
// (its initializationOptions' tsserver.path): the project's own, in
// node_modules at root or a folder above it in the workspace; else the
// one installed beside tsc on PATH, or beside the server's command (a
// global npm install of both). "" for none: the server looks for itself,
// and says when it can't find one.
func (m *Manager) tsserverLib(root, command string) string {
	for d := root; within(m.root, d); d = filepath.Dir(d) {
		if lib := filepath.Join(d, "node_modules", "typescript", "lib"); isTSLib(lib) {
			return lib
		}
		if d == m.root || filepath.Dir(d) == d {
			break
		}
	}
	// A global install: <prefix>/bin/tsc links to
	// <prefix>/lib/node_modules/typescript/bin/tsc, and the server's
	// command to its own package beside typescript's.
	if tsc, err := tsLookPath("tsc"); err == nil {
		if real, err := filepath.EvalSymlinks(tsc); err == nil {
			if lib := filepath.Join(filepath.Dir(filepath.Dir(real)), "lib"); isTSLib(lib) {
				return lib
			}
		}
	}
	if bin, err := tsLookPath(command); err == nil {
		if real, err := filepath.EvalSymlinks(bin); err == nil {
			for d := filepath.Dir(real); filepath.Dir(d) != d; d = filepath.Dir(d) {
				for _, lib := range []string{filepath.Join(d, "typescript", "lib"), filepath.Join(d, "node_modules", "typescript", "lib")} {
					if isTSLib(lib) {
						return lib
					}
				}
			}
		}
	}
	return ""
}

// isTSLib reports whether lib is a TypeScript install's lib folder.
func isTSLib(lib string) bool {
	st, err := os.Stat(filepath.Join(lib, "tsserver.js"))
	return err == nil && !st.IsDir()
}
