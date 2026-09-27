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

// Command specs checks the specs against the repository
// (spec_release_readiness_030 RR-21): every spec is in the specs index,
// and every repository path a spec names in backquotes exists. Paths
// under the old layout (internal/, cmd/, web/, go/, bazel/) are flagged
// too, since none of them exist any more. A path introduced as an example
// ("e.g.", "for example") or followed by "(planned)" isn't checked.
//
//	bazel run //tools/specs [-- --dir=.agents/specs]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// roots are the top-level directories whose paths a spec may name: the
// repository's, and the old layout's.
var roots = []string{"apps/", "pkg/", "proto/", "build/", "tools/", "third_party/", "release/", "docs/", ".github/",
	"internal/", "cmd/", "web/", "go/", "bazel/", "configs/"}

// rootFiles are files at the repository's root a spec may name. (Not
// AGENTS.md or .agents/: those are also conventions in the workspaces
// Blitz opens.)
var rootFiles = map[string]bool{"MODULE.bazel": true, "BUILD.bazel": true, ".bazelrc": true, ".bazelversion": true, ".bazelignore": true,
	"go.mod": true, "go.sum": true, "buf.yaml": true, "LICENSE": true, "NOTICE": true, "THIRD_PARTY_NOTICES": true, "README.md": true,
	"MODULE.bazel.lock": true, "OWNERS.txt": true}

func main() {
	dir := flag.String("dir", ".agents/specs", "the specs, relative to the repository")
	flag.Parse()
	if ws := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); ws != "" {
		if err := os.Chdir(ws); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	problems, n, err := check(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		os.Exit(1)
	}
	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "✗ %d problems in the specs:\n", len(problems))
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "    "+p)
		}
		os.Exit(1)
	}
	fmt.Printf("✓ %d specs: indexed, and every path they name exists\n", n)
}

// check returns the specs' problems and how many specs there are.
func check(dir string) ([]string, int, error) {
	specs, err := filepath.Glob(filepath.Join(dir, "spec_*.md"))
	if err != nil {
		return nil, 0, err
	}
	index, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		return nil, 0, err
	}
	var problems []string
	for _, s := range specs {
		name := filepath.Base(s)
		if !strings.Contains(string(index), "("+name+")") {
			problems = append(problems, name+": not in the specs index (README.md)")
		}
		text, err := os.ReadFile(s)
		if err != nil {
			return nil, 0, err
		}
		for _, p := range missingPaths(string(text)) {
			problems = append(problems, name+": "+p)
		}
	}
	sort.Strings(problems)
	return problems, len(specs), nil
}

var quoted = regexp.MustCompile("`([^`\\s]+)`")

// missingPaths lists the repository paths text names that don't exist.
func missingPaths(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, loc := range quoted.FindAllStringSubmatchIndex(text, -1) {
		p := strings.TrimRight(text[loc[2]:loc[3]], ".,;:)")
		if seen[p] || !repoPath(p) || example(text[:loc[0]]) || strings.HasPrefix(text[loc[1]:], " (planned)") {
			continue
		}
		seen[p] = true
		if !exists(p) {
			out = append(out, fmt.Sprintf("`%s` doesn't exist", p))
		}
	}
	return out
}

// example: the text just before a path introduces it as an example.
func example(before string) bool {
	tail := strings.ToLower(before[max(0, len(before)-24):])
	return strings.Contains(tail, "e.g.") || strings.Contains(tail, "example")
}

// repoPath: p names something in the repository (not a label, a URL, a
// home path or a pattern of names).
func repoPath(p string) bool {
	if rootFiles[p] {
		return true
	}
	if strings.ContainsAny(p, "<>$~@…") || strings.Contains(p, "//") || strings.Contains(p, ":") {
		return false
	}
	for _, r := range roots {
		if strings.HasPrefix(p, r) {
			return true
		}
	}
	return false
}

// exists: the path, a glob of it, or each alternative of {a,b} exists.
func exists(p string) bool {
	if i := strings.Index(p, "{"); i >= 0 {
		if j := strings.Index(p[i:], "}"); j > 0 {
			for _, alt := range strings.Split(p[i+1:i+j], ",") {
				if !exists(p[:i] + alt + p[i+j+1:]) {
					return false
				}
			}
			return true
		}
	}
	if strings.ContainsAny(p, "*?[") {
		m, _ := filepath.Glob(p)
		return len(m) > 0
	}
	_, err := os.Stat(strings.TrimSuffix(p, "/"))
	return err == nil
}
