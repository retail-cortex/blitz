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
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Bazel workspaces (FS-61c): MODULE.bazel (or a WORKSPACE file) says
// which rules the workspace builds with, and Bazel, not go.mod or
// node_modules beside the sources, has what the servers need: rules_go's
// package driver gives gopls the packages of sources with no go.mod above
// them, Bazel's output has the TypeScript rules_js installs and the
// packages rules_python's pip installs.

// bazelWorkspace is what the workspace's MODULE.bazel or WORKSPACE says.
type bazelWorkspace struct {
	ok bool // the workspace builds with Bazel
	// rulesGo is rules_go's repository name ("rules_go", or the
	// repo_name it's given), "" when the workspace doesn't use it.
	rulesGo string
	// rulesPython reports whether it uses rules_python.
	rulesPython bool
}

var (
	bazelDep = regexp.MustCompile(`(?s)bazel_dep\s*\((.*?)\)`)
	bazelArg = regexp.MustCompile(`(\w+)\s*=\s*"([^"]*)"`)
)

// readBazel reads root's MODULE.bazel, else its WORKSPACE file.
func readBazel(root string) bazelWorkspace {
	if data, err := os.ReadFile(filepath.Join(root, "MODULE.bazel")); err == nil {
		b := bazelWorkspace{ok: true}
		for _, dep := range bazelDep.FindAllStringSubmatch(string(data), -1) {
			args := map[string]string{}
			for _, a := range bazelArg.FindAllStringSubmatch(dep[1], -1) {
				args[a[1]] = a[2]
			}
			repo := args["name"]
			if r := args["repo_name"]; r != "" {
				repo = r
			}
			switch args["name"] {
			case "rules_go":
				b.rulesGo = repo
			case "rules_python":
				b.rulesPython = true
			}
		}
		return b
	}
	for _, name := range []string{"WORKSPACE.bazel", "WORKSPACE"} {
		if data, err := os.ReadFile(filepath.Join(root, name)); err == nil {
			b := bazelWorkspace{ok: true, rulesPython: strings.Contains(string(data), "rules_python")}
			if strings.Contains(string(data), "io_bazel_rules_go") {
				b.rulesGo = "io_bazel_rules_go"
			}
			return b
		}
	}
	return bazelWorkspace{}
}

// bazel is the workspace's Bazel setup, read once.
func (m *Manager) bazel() bazelWorkspace {
	m.bazelOnce.Do(func() { m.bazelWS = readBazel(m.root) })
	return m.bazelWS
}

// bazelOutputs are the convenience links Bazel makes in the workspace
// (bazel-bin, bazel-<name>…): its output, which no server should index.
func (m *Manager) bazelOutputs() []string {
	return []string{"bazel-bin", "bazel-out", "bazel-testlogs", "bazel-" + filepath.Base(m.root)}
}

// cacheDir is where Blitz keeps the language servers' files (a variable
// for tests): the user's cache, which commands in the OS sandbox may write.
var cacheDir = func() string {
	d, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "blitz")
	}
	return filepath.Join(d, "blitz")
}

// goDriverScript runs rules_go's package driver with the flags the
// environment gives (GOPACKAGESDRIVER_BAZEL_FLAGS: Bazel's own output
// base, so the language server never starts or shares the person's Bazel
// server from inside the sandbox).
const goDriverScript = `#!/bin/sh
# Blitz's gopls package driver for Bazel workspaces (rules_go).
exec "${GOPACKAGESDRIVER_BAZEL:-bazel}" $GOPACKAGESDRIVER_BAZEL_FLAGS run --tool_tag=gopackagesdriver --ui_event_filters=-info,-stderr --noshow_progress -- "@${BLITZ_RULES_GO:-rules_go}//go/tools/gopackagesdriver" "$@"
`

// goDriverIdle is how long the driver's Bazel server waits idle before
// it exits.
const goDriverIdle = "1800"

// goDriverEnv is the environment gopls gets for root: rules_go's package
// driver when the workspace builds Go with Bazel and root has no go.mod
// or go.work (gopls finds no packages for such sources without it); nil
// otherwise. The workspace's own driver (tools/gopackagesdriver.sh, as
// rules_go's documentation names it) is used as it is.
func (m *Manager) goDriverEnv(root string) []string {
	b := m.bazel()
	if b.rulesGo == "" || runtime.GOOS == "windows" || hasAny(root, rootMarkers["go"]) {
		return nil
	}
	if own := filepath.Join(m.root, "tools", "gopackagesdriver.sh"); isFile(own) {
		return []string{"GOPACKAGESDRIVER=" + own}
	}
	dir := filepath.Join(cacheDir(), "lsp")
	script := filepath.Join(dir, "gopackagesdriver.sh")
	if data, err := os.ReadFile(script); err != nil || string(data) != goDriverScript {
		if os.MkdirAll(dir, 0o700) != nil || os.WriteFile(script, []byte(goDriverScript), 0o700) != nil {
			return nil
		}
	}
	sum := sha256.Sum256([]byte(m.root))
	base := filepath.Join(cacheDir(), "bazel", hex.EncodeToString(sum[:6]))
	return []string{
		"GOPACKAGESDRIVER=" + script,
		"GOPACKAGESDRIVER_BAZEL_FLAGS=--output_base=" + base + " --max_idle_secs=" + goDriverIdle,
		"BLITZ_RULES_GO=" + b.rulesGo,
	}
}

// hasAny reports whether dir holds one of tiers' files.
func hasAny(dir string, tiers [][]string) bool {
	for _, tier := range tiers {
		for _, f := range tier {
			if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
				return true
			}
		}
	}
	return false
}

// pythonBazelMarkers are a Bazel workspace's Python project roots: its
// imports start at the workspace's root (rules_python), so a requirements
// lock in a folder isn't a root; a pyproject.toml or pyright's own file
// is.
var pythonBazelMarkers = [][]string{{"pyrightconfig.json", "pyproject.toml"}}

// sitePackages are the packages rules_python's pip installed, in Bazel's
// external repositories (bazel-<name>/external/<hub>_<package>/site-packages),
// for pyright to resolve imports from; nil for none.
func (m *Manager) sitePackages() []string {
	if !m.bazel().rulesPython {
		return nil
	}
	found, _ := filepath.Glob(filepath.Join(m.root, "bazel-"+filepath.Base(m.root), "external", "*", "site-packages"))
	return found
}

// bazelTSLib is TypeScript as rules_js installed it for root: in Bazel's
// output (bazel-bin/<root>/node_modules), at root or a folder above it in
// the workspace; "" for none.
func (m *Manager) bazelTSLib(root string) string {
	if !m.bazel().ok {
		return ""
	}
	for d := root; within(m.root, d); d = filepath.Dir(d) {
		rel, _ := filepath.Rel(m.root, d)
		if lib := filepath.Join(m.root, "bazel-bin", rel, "node_modules", "typescript", "lib"); isTSLib(lib) {
			return lib
		}
		if d == m.root || filepath.Dir(d) == d {
			break
		}
	}
	return ""
}
