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

// Command codeowners writes .github/CODEOWNERS from OWNERS.txt, so GitHub
// requests reviews from the maintainers who own the paths a change touches
// (spec_release_readiness_030 RR-42). With --check it only reports whether
// the file is current, for CI.
//
//	bazel run //tools/codeowners [-- --check]
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

const (
	ownersFile     = "OWNERS.txt"
	codeownersFile = ".github/CODEOWNERS"
)

func main() {
	check := flag.Bool("check", false, "only check that "+codeownersFile+" is current")
	flag.Parse()
	if ws := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); ws != "" {
		if err := os.Chdir(ws); err != nil {
			fail(err)
		}
	}
	owners, err := os.ReadFile(ownersFile)
	if err != nil {
		fail(err)
	}
	want, err := generate(string(owners))
	if err != nil {
		fail(fmt.Errorf("%s: %w", ownersFile, err))
	}
	if *check {
		got, err := os.ReadFile(codeownersFile)
		if err != nil || !bytes.Equal(got, want) {
			fail(fmt.Errorf("%s is out of date (run: bazel run //tools/codeowners)", codeownersFile))
		}
		fmt.Printf("✓ %s is current\n", codeownersFile)
		return
	}
	if err := os.WriteFile(codeownersFile, want, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("✓ wrote %s\n", codeownersFile)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "✗", err)
	os.Exit(1)
}

// generate turns OWNERS.txt into CODEOWNERS: a line per path, in the order
// the paths first appear, listing every handle that owns it.
func generate(owners string) ([]byte, error) {
	var paths []string
	handles := map[string][]string{}
	for n, line := range strings.Split(owners, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		at := -1
		for i, f := range fields {
			if strings.HasPrefix(f, "@") {
				at = i
				break
			}
		}
		switch {
		case at < 0:
			return nil, fmt.Errorf("line %d: no GitHub handle (@name)", n+1)
		case at == 0:
			return nil, fmt.Errorf("line %d: no name before %s", n+1, fields[0])
		case at == len(fields)-1:
			return nil, fmt.Errorf("line %d: %s owns no paths", n+1, fields[at])
		}
		for _, p := range fields[at+1:] {
			if strings.Contains(p, "@") {
				return nil, fmt.Errorf("line %d: one handle per line (%s)", n+1, p)
			}
			if _, ok := handles[p]; !ok {
				paths = append(paths, p)
			}
			handles[p] = append(handles[p], fields[at])
		}
	}
	if len(paths) == 0 {
		return nil, errors.New("no maintainers")
	}
	var b bytes.Buffer
	b.WriteString("# Generated from OWNERS.txt by //tools/codeowners. Don't edit: change\n# OWNERS.txt and run bazel run //tools/codeowners.\n\n")
	for _, p := range paths {
		fmt.Fprintf(&b, "%s %s\n", p, strings.Join(handles[p], " "))
	}
	return b.Bytes(), nil
}
