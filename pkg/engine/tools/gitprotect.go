// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// gitControlPattern is, relative to a writable root, the files git runs
// commands from in the root's repository and its submodules'
// (.git/modules/…): its config (core.fsmonitor, core.hooksPath, filter
// and diff drivers, aliases), hooks and attributes. The agent never
// writes them (spec_007 SH-23b): the person's own terminal, git GUI or
// shell prompt runs git there outside the sandbox (security review
// terminal#1). A repository the agent makes further down (git clone, git
// init) is its own and isn't covered.
const gitControlPattern = `\.git/(modules/(.+/)?)?(config|config\.worktree|hooks(/.*)?|info/attributes)$`

var gitControl = func() *regexp.Regexp {
	if caseInsensitiveFS {
		return regexp.MustCompile("(?i)^" + gitControlPattern)
	}
	return regexp.MustCompile("^" + gitControlPattern)
}()

// ErrGitControl means a write reached a file git runs commands from.
var ErrGitControl = fmt.Errorf("%w: git's config, hooks and attributes aren't the agent's to change (they run commands outside the sandbox); the person changes them", ErrReadOnlyPath)

// isGitControl reports whether rel (relative to a writable root, slash
// or OS separators) is one of the root's repository's files git runs
// commands from.
func isGitControl(rel string) bool {
	return gitControl.MatchString(filepath.ToSlash(rel))
}

// gitControlSBPL is the macOS profile's rule keeping commands from
// writing those files under the writable dirs: last among the write
// rules, so it wins over them. Seatbelt matches its regexes without case
// on a case-insensitive disk.
func gitControlSBPL(writable []string) string {
	if len(writable) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("(deny file-write*\n")
	for _, d := range writable {
		fmt.Fprintf(&sb, "  (regex #\"^%s/%s\")\n", regexp.QuoteMeta(filepath.ToSlash(d)), gitControlPattern)
	}
	sb.WriteString(")\n")
	return sb.String()
}

// gitControlFiles are the files and folders bubblewrap binds read-only
// under the writable dirs: each repository's at the top of one (and its
// submodules'), those that exist (a bind needs its target).
func gitControlFiles(writable []string) []string {
	var out []string
	for _, d := range writable {
		gitDir := filepath.Join(d, ".git")
		if info, err := os.Stat(gitDir); err != nil || !info.IsDir() {
			continue
		}
		dirs := []string{gitDir}
		if mods, _ := filepath.Glob(filepath.Join(gitDir, "modules", "*")); len(mods) > 0 {
			dirs = append(dirs, mods...)
		}
		for _, g := range dirs {
			for _, rel := range []string{"config", "config.worktree", "hooks", filepath.Join("info", "attributes")} {
				p := filepath.Join(g, rel)
				if _, err := os.Lstat(p); err == nil {
					out = append(out, p)
				}
			}
		}
	}
	return out
}
