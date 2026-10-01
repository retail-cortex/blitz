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

// Command notices writes THIRD_PARTY_NOTICES: the license of every Go
// module linked into Blitz's programs and of every npm package bundled
// into the desktop app's page, from the copies Bazel fetched. It fails
// when a component has no license file, or one outside the licenses
// Blitz accepts. tools/third_party_notices.sh finds its inputs and runs
// it (spec_release_readiness_030 RR-04).
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// component is one piece of third-party software and its license files.
type component struct {
	name, version string
	license       string            // SPDX identifier, from the text
	texts         map[string]string // file name → text
}

func main() {
	var (
		repos    = flag.String("go-repos", "", "file listing the directories of the linked Go modules, one per line")
		gomod    = flag.String("go-mod", "go.mod", "the repository's go.mod, for module versions")
		goroot   = flag.String("goroot", "", "the Go SDK, for the standard library's license")
		apache   = flag.String("apache", "LICENSE", "the Apache License 2.0 text, printed once at the end")
		lock     = flag.String("npm-lock", "", "the pnpm-lock.yaml (the page's packages are what ships)")
		store    = flag.String("npm-store", "", "rules_js's package store (node_modules/.aspect_rules_js)")
		out      = flag.String("out", "THIRD_PARTY_NOTICES", "the file to write")
		checkOld = flag.Bool("check", false, "fail if the file differs from what would be written, instead of writing it")
	)
	flag.Parse()
	if err := run(*repos, *gomod, *goroot, *apache, *lock, *store, *out, *checkOld); err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		os.Exit(1)
	}
}

func run(reposFile, gomod, goroot, apachePath, lock, store, out string, check bool) error {
	versions, err := moduleVersions(gomod)
	if err != nil {
		return err
	}
	var problems []string
	goComps, errs := goModules(reposFile, versions)
	problems = append(problems, errs...)
	if goroot != "" {
		c, err := readComponent("Go standard library", strings.TrimPrefix(goVersion(goroot), "go"), goroot)
		if err != nil {
			problems = append(problems, err.Error())
		} else {
			goComps = append([]component{c}, goComps...)
		}
	}
	npmComps, errs := npmPackages(lock, store)
	problems = append(problems, errs...)
	if len(problems) > 0 {
		sort.Strings(problems)
		return errors.New("third-party licenses:\n    " + strings.Join(problems, "\n    "))
	}
	apacheText, err := os.ReadFile(apachePath)
	if err != nil {
		return err
	}
	text := render(goComps, npmComps, string(apacheText))
	if check {
		old, _ := os.ReadFile(out)
		if !bytes.Equal(old, []byte(text)) {
			return fmt.Errorf("%s is out of date: run tools/third_party_notices.sh", out)
		}
		fmt.Printf("✓ %s is current (%d Go modules, %d npm packages)\n", out, len(goComps), len(npmComps))
		return nil
	}
	if err := os.WriteFile(out, []byte(text), 0o644); err != nil {
		return err
	}
	fmt.Printf("✓ wrote %s (%d Go modules, %d npm packages)\n", out, len(goComps), len(npmComps))
	return nil
}

// moduleVersions reads each required module's version from go.mod.
func moduleVersions(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	versions := map[string]string{}
	re := regexp.MustCompile(`^\s*(?:require\s+)?([^\s()]+)\s+(v\S+)`)
	for line := range strings.SplitSeq(string(data), "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			versions[m[1]] = m[2]
		}
	}
	return versions, nil
}

// repoName is Gazelle's repository name for a module path:
// "github.com/pkg/errors" → "com_github_pkg_errors".
func repoName(path string) string {
	parts := strings.Split(path, "/")
	host := strings.Split(parts[0], ".")
	for i, j := 0, len(host)-1; i < j; i, j = i+1, j-1 {
		host[i], host[j] = host[j], host[i]
	}
	name := strings.Join(append(host, parts[1:]...), "_")
	return strings.NewReplacer(".", "_", "-", "_", "~", "_").Replace(name)
}

func goVersion(goroot string) string {
	data, _ := os.ReadFile(filepath.Join(goroot, "VERSION"))
	return strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
}

// goModules reads the listed module directories.
func goModules(reposFile string, versions map[string]string) ([]component, []string) {
	data, err := os.ReadFile(reposFile)
	if err != nil {
		return nil, []string{err.Error()}
	}
	var comps []component
	var problems []string
	for dir := range strings.FieldsSeq(string(data)) {
		name := ""
		if mod, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
			if m := regexp.MustCompile(`(?m)^module\s+(\S+)`).FindSubmatch(mod); m != nil {
				name = strings.Trim(string(m[1]), `"`)
			}
		}
		if name == "" {
			// An older module without go.mod: match Gazelle's name for it.
			repo := filepath.Base(dir)
			repo = repo[strings.LastIndex(repo, "+")+1:]
			for path := range versions {
				if repoName(path) == repo {
					name = path
				}
			}
		}
		if name == "" {
			problems = append(problems, fmt.Sprintf("%s: can't tell which module it is", filepath.Base(dir)))
			continue
		}
		c, err := readComponent(name, versions[name], dir)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		comps = append(comps, c)
	}
	sortComponents(comps)
	return comps, problems
}

// npmPackages walks the page's runtime dependencies (not its build
// tools) through the lockfile, and reads each one from the store.
func npmPackages(lock, store string) ([]component, []string) {
	if lock == "" {
		return nil, nil
	}
	data, err := os.ReadFile(lock)
	if err != nil {
		return nil, []string{err.Error()}
	}
	var lf struct {
		Importers map[string]struct {
			Dependencies map[string]struct{ Version string } `yaml:"dependencies"`
		} `yaml:"importers"`
		Snapshots map[string]struct {
			Dependencies map[string]string `yaml:"dependencies"`
		} `yaml:"snapshots"`
	}
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", lock, err)}
	}
	seen := map[string]bool{}
	var queue []string
	for name, d := range lf.Importers["apps/desktop/web"].Dependencies {
		queue = append(queue, name+"@"+d.Version)
	}
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		if seen[key] {
			continue
		}
		seen[key] = true
		for name, v := range lf.Snapshots[key].Dependencies {
			queue = append(queue, name+"@"+v)
		}
	}
	var comps []component
	var problems []string
	for key := range seen {
		name, version := splitNpmKey(key)
		dir := filepath.Join(store, findStoreDir(store, key), "node_modules", name)
		c, err := readNpm(name, version, dir)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		comps = append(comps, c)
	}
	sortComponents(comps)
	return comps, problems
}

// sortComponents orders components by name, then version, so the file
// comes out the same every time.
func sortComponents(comps []component) {
	sort.Slice(comps, func(i, j int) bool {
		if comps[i].name != comps[j].name {
			return comps[i].name < comps[j].name
		}
		return comps[i].version < comps[j].version
	})
}

// splitNpmKey splits "react-dom@19.3.0(react@19.3.0)" into its name and
// version (peers dropped).
func splitNpmKey(key string) (string, string) {
	base, _, _ := strings.Cut(key, "(")
	i := strings.LastIndex(base, "@")
	if i <= 0 {
		return base, ""
	}
	return base[:i], base[i+1:]
}

// storeDir is rules_js's directory for a lockfile key:
// "react-dom@19.3.0(react@19.3.0)" → "react-dom@19.3.0_react@19.3.0".
func storeDir(key string) string {
	r := strings.NewReplacer("/", "+", "(", "_", ")", "")
	return r.Replace(key)
}

// findStoreDir finds a package's directory in the store: its exact name,
// else (rules_js hashes long peer suffixes) the first with its name and
// version.
func findStoreDir(store, key string) string {
	exact := storeDir(key)
	if _, err := os.Stat(filepath.Join(store, exact)); err == nil {
		return exact
	}
	base, _, _ := strings.Cut(key, "(")
	prefix := storeDir(base) + "_"
	entries, _ := os.ReadDir(store)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			return e.Name()
		}
	}
	return exact
}

// readNpm reads an npm package's license files; a package that ships
// none is described by the license its package.json declares, with that
// license's standard terms and the author named there.
func readNpm(name, version, dir string) (component, error) {
	c, err := readComponent(name, version, dir)
	if err == nil || len(c.texts) > 0 {
		return c, err
	}
	var pkg struct {
		License string          `json:"license"`
		Author  json.RawMessage `json:"author"`
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "package.json"))
	if rerr != nil || json.Unmarshal(data, &pkg) != nil || pkg.License == "" {
		return c, err
	}
	ids := regexp.MustCompile(`[A-Za-z0-9.-]+`).FindAllString(pkg.License, -1)
	var parts []string
	for _, id := range ids {
		if id == "AND" || id == "OR" {
			continue
		}
		terms, ok := standardTerms[id]
		if !ok {
			return c, fmt.Errorf("%s %s: declares %s, which Blitz doesn't accept", name, version, pkg.License)
		}
		if terms != "" {
			parts = append(parts, terms)
		}
	}
	author := authorName(pkg.Author)
	head := "The package ships no license file; its package.json declares " + pkg.License + "."
	if author != "" {
		head += " Author: " + author + "."
	}
	c.license = pkg.License
	c.texts["package.json"] = strings.Join(append([]string{head}, parts...), "\n\n")
	return c, nil
}

// authorName reads package.json's author, a string or {name, …}.
func authorName(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct{ Name string }
	if json.Unmarshal(raw, &o) == nil {
		return o.Name
	}
	return ""
}

// standardTerms are the accepted licenses' terms, for packages that ship
// no text ("" for Apache-2.0: printed once at the end).
var standardTerms = map[string]string{
	"Apache-2.0": "",
	"MIT": `Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.`,
	"ISC": `Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY
AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.`,
	"BSD-3-Clause": `Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.
2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.
3. Neither the name of the copyright holder nor the names of its contributors
   may be used to endorse or promote products derived from this software
   without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.`,
}

var licenseFile = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice)([.-].*)?$`)

// readComponent reads dir's license files and identifies the license.
func readComponent(name, version, dir string) (component, error) {
	c := component{name: name, version: version, texts: map[string]string{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return c, fmt.Errorf("%s: %v", name, err)
	}
	for _, e := range entries {
		if e.IsDir() || !licenseFile.MatchString(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return c, fmt.Errorf("%s: %v", name, err)
		}
		c.texts[e.Name()] = strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n"))
	}
	if len(c.texts) == 0 {
		return c, fmt.Errorf("%s %s: no license file", name, version)
	}
	files := make([]string, 0, len(c.texts))
	for f := range c.texts {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, file := range files {
		if strings.HasPrefix(strings.ToUpper(file), "NOTICE") {
			continue
		}
		if id := identify(c.texts[file]); id != "" {
			c.license = id
			break
		}
	}
	if c.license == "" {
		return c, fmt.Errorf("%s %s: a license Blitz doesn't accept, or one it can't recognize", name, version)
	}
	return c, nil
}

// identify names an accepted license from its text ("" for others).
func identify(text string) string {
	t := strings.Join(strings.Fields(text), " ")
	switch {
	case strings.Contains(t, "Pictogrammers Free License"):
		// Material Design Icons: free for any use, the icons under Apache 2.0.
		return "Pictogrammers Free License (icons Apache-2.0)"
	case strings.Contains(t, "Apache License") && strings.Contains(t, "Version 2.0"):
		return "Apache-2.0"
	case strings.Contains(t, "Mozilla Public License Version 2.0") || strings.Contains(t, "Mozilla Public License, version 2.0"):
		return "MPL-2.0"
	case strings.Contains(t, "Permission is hereby granted, free of charge"):
		return "MIT"
	case strings.Contains(t, "Permission to use, copy, modify, and/or distribute this software for any purpose"),
		strings.Contains(t, "Permission to use, copy, modify, and distribute this software for any purpose"):
		return "ISC"
	case strings.Contains(t, "This is free and unencumbered software released into the public domain"):
		return "Unlicense"
	case strings.Contains(t, "Redistribution and use in source and binary forms"):
		if strings.Contains(t, "Neither the name") || strings.Contains(t, "names of its contributors") {
			return "BSD-3-Clause"
		}
		return "BSD-2-Clause"
	}
	return ""
}

func render(goComps, npmComps []component, apache string) string {
	var b strings.Builder
	b.WriteString(`THIRD-PARTY NOTICES

Blitz (the blitz CLI, the blitzd service and the Blitz desktop app)
includes the third-party software below, under the licenses shown. The
Apache License 2.0 is printed once, at the end; every other license is
printed with its component, as its copyright notice requires.

This file is generated by tools/third_party_notices.sh from the modules
and packages the programs link; don't edit it by hand.

`)
	section := func(title string, comps []component) {
		fmt.Fprintf(&b, "%s\n%s\n\n", title, strings.Repeat("=", len(title)))
		for _, c := range comps {
			fmt.Fprintf(&b, "  %-62s %s\n", strings.TrimSpace(c.name+" "+c.version), c.license)
		}
		b.WriteString("\n")
		for _, c := range comps {
			head := strings.TrimSpace(c.name + " " + c.version)
			fmt.Fprintf(&b, "%s\n%s\n\n", head, strings.Repeat("-", len(head)))
			files := make([]string, 0, len(c.texts))
			for f := range c.texts {
				files = append(files, f)
			}
			sort.Strings(files)
			for _, f := range files {
				text := c.texts[f]
				if identify(text) == "Apache-2.0" && !strings.HasPrefix(strings.ToUpper(f), "NOTICE") {
					fmt.Fprintf(&b, "Licensed under the Apache License 2.0 (printed at the end).\n\n")
					continue
				}
				fmt.Fprintf(&b, "%s\n\n", text)
			}
		}
	}
	section("Go modules linked into blitz, blitzd and the desktop app", goComps)
	section("npm packages bundled into the desktop app's window", npmComps)
	title := "Apache License 2.0"
	fmt.Fprintf(&b, "%s\n%s\n\n%s\n", title, strings.Repeat("=", len(title)), strings.TrimSpace(apache))
	return b.String()
}
