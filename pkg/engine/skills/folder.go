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

package skills

import (
	"encoding/json"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// The four kinds of skill, told from what its folder holds rather than a
// field (spec_skills_013 SK-97).
const (
	// KindSimple is SKILL.md alone.
	KindSimple = "simple"
	// KindReferences has a references/ folder the model can read.
	KindReferences = "references"
	// KindScripts has scripts (declared, or in scripts/), and maybe
	// references too.
	KindScripts = "scripts"
	// KindMeta combines other skills (front matter skills:).
	KindMeta = "meta"
)

// The folders a skill's files are kept in, beside SKILL.md.
const (
	ReferencesDir = "references"
	ScriptsDir    = "scripts"
)

// scriptLanguages are the languages of scripts found in scripts/, by
// extension.
var scriptLanguages = map[string]ScriptLanguage{
	".py": LanguagePython, ".ts": LanguageTypeScript, ".js": LanguageJavaScript, ".mjs": LanguageJavaScript,
	".sh": LanguageShell, ".cmd": LanguageCmd, ".bat": LanguageCmd, ".ps1": LanguagePowerShell,
}

// ScriptLanguageOf is the language of a script file, by its extension
// (.py, .ts, .js, .mjs, .sh, .cmd, .bat, .ps1); unspecified for anything
// else.
func ScriptLanguageOf(name string) ScriptLanguage { return scriptLanguages[path.Ext(name)] }

// scanBundle fills in what the skill's folder holds (SK-97, SK-98): its
// resources (the files beside SKILL.md, and everything under
// references/), its references, and, when the front matter declares no
// scripts, those in scripts/ (by extension: ScriptLanguageOf), named by
// their file name (with its extension when two share one: test.py and
// test.ts). A skill that declares its scripts keeps exactly those: its
// other files there may be helpers they import. Then a Python script that
// names no packages takes requirements.txt's, and a Node one
// package.json's dependencies (SK-107).
func (s *Skill) scanBundle() {
	s.Resources, s.References = []string{}, nil
	if s.fsys == nil {
		return
	}
	entries, _ := fs.ReadDir(s.fsys, s.root)
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "SKILL.md" {
			s.Resources = append(s.Resources, e.Name())
		}
	}
	refs := path.Join(s.root, ReferencesDir)
	_ = fs.WalkDir(s.fsys, refs, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			s.References = append(s.References, ReferencesDir+"/"+strings.TrimPrefix(p, refs+"/"))
		}
		return nil
	})
	s.Resources = append(s.Resources, s.References...)

	defer s.addPackages()
	if len(s.Scripts) > 0 {
		return
	}
	scripts, _ := fs.ReadDir(s.fsys, path.Join(s.root, ScriptsDir))
	var found []fs.DirEntry
	bases := map[string]int{}
	for _, e := range scripts {
		if e.Type().IsRegular() && ScriptLanguageOf(e.Name()) != LanguageUnspecified {
			found = append(found, e)
			bases[strings.TrimSuffix(e.Name(), path.Ext(e.Name()))]++
		}
	}
	for _, e := range found {
		name := strings.TrimSuffix(e.Name(), path.Ext(e.Name()))
		if bases[name] > 1 {
			name = e.Name() // test.py and test.ts: each by its whole name
		}
		s.Scripts = append(s.Scripts, ScriptDefinition{Name: name, Language: ScriptLanguageOf(e.Name()), RelativePath: ScriptsDir + "/" + e.Name(), Found: true})
	}
}

// PackageFiles are where a skill's scripts' packages may be listed, by
// kind, the first found winning: requirements.txt, then pyproject.toml
// ([project] dependencies) for Python, package.json for Node; in scripts/
// first, then beside SKILL.md.
var PackageFiles = map[string][]string{
	"python": {"scripts/requirements.txt", "requirements.txt", "scripts/pyproject.toml", "pyproject.toml"},
	"node":   {"scripts/package.json", "package.json"},
}

// IsPackageFile reports whether rel names one of PackageFiles.
func IsPackageFile(rel string) bool {
	for _, list := range PackageFiles {
		if slices.Contains(list, rel) {
			return true
		}
	}
	return false
}

// findPackageFiles notes which of PackageFiles the skill's folder has.
func (s *Skill) findPackageFiles() {
	s.PackageFiles = nil
	for _, kind := range []string{"python", "node"} {
		for _, rel := range PackageFiles[kind] {
			if _, err := fs.Stat(s.fsys, path.Join(s.root, rel)); err == nil {
				s.PackageFiles = append(s.PackageFiles, rel)
			}
		}
	}
}

// addPackages gives each script that names no packages those its
// skill's requirements.txt (Python) or package.json (Node) lists, so it
// runs in an isolated environment with them (SK-107).
func (s *Skill) addPackages() {
	s.findPackageFiles()
	var py, npm []string
	loaded := false
	for i := range s.Scripts {
		sc := &s.Scripts[i]
		if len(sc.Dependencies) > 0 || (sc.Language != LanguagePython && !sc.Language.Node()) {
			continue
		}
		if !loaded {
			py, npm, loaded = s.readPackageFiles(), s.readPackageJSON(), true
		}
		if sc.Language == LanguagePython {
			sc.Dependencies = py
		} else {
			sc.Dependencies = npm
		}
	}
}

// readPackageFiles is the first Python package file's requirements:
// requirements.txt's lines, without comments and blank lines, or
// pyproject.toml's [project] dependencies.
func (s *Skill) readPackageFiles() []string {
	for _, rel := range PackageFiles["python"] {
		data, err := fs.ReadFile(s.fsys, path.Join(s.root, rel))
		if err != nil {
			continue
		}
		if strings.HasSuffix(rel, ".toml") {
			var py struct {
				Project struct {
					Dependencies []string `toml:"dependencies"`
				} `toml:"project"`
			}
			if _, err := toml.Decode(string(data), &py); err != nil {
				return []string{rel + " isn't valid TOML"} // refused by the policy, with the reason shown
			}
			return py.Project.Dependencies
		}
		var out []string
		for _, line := range strings.Split(string(data), "\n") {
			if i := strings.Index(line, "#"); i >= 0 {
				line = line[:i]
			}
			if line = strings.TrimSpace(line); line != "" {
				out = append(out, line)
			}
		}
		return out
	}
	return nil
}

// readPackageJSON is package.json's dependencies, as name@version.
func (s *Skill) readPackageJSON() []string {
	for _, rel := range PackageFiles["node"] {
		data, err := fs.ReadFile(s.fsys, path.Join(s.root, rel))
		if err != nil {
			continue
		}
		var pkg struct {
			Dependencies map[string]string `json:"dependencies"`
		}
		if json.Unmarshal(data, &pkg) != nil {
			return []string{rel + " isn't valid JSON"} // refused by the policy, with the reason shown
		}
		var out []string
		for _, name := range slices.Sorted(maps.Keys(pkg.Dependencies)) {
			out = append(out, name+"@"+pkg.Dependencies[name])
		}
		return out
	}
	return nil
}

// Kind is which of the four kinds the skill is (SK-97): meta when it
// names other skills, else scripts, references or simple by what its
// folder holds.
func (s *Skill) Kind() string {
	switch {
	case len(s.Members) > 0:
		return KindMeta
	case len(s.Scripts) > 0:
		return KindScripts
	case len(s.References) > 0:
		return KindReferences
	}
	return KindSimple
}

// OfferedTo reports whether the skill is offered to the named agent
// (front matter agents:; none means every agent).
func (s *Skill) OfferedTo(agent string) bool {
	return len(s.Agents) == 0 || slices.Contains(s.Agents, agent)
}
