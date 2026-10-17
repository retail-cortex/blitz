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
	"io/fs"
	"path"
	"slices"
	"strings"
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
var scriptLanguages = map[string]ScriptLanguage{".py": LanguagePython, ".ts": LanguageTypeScript}

// ScriptLanguageOf is the language of a script file, by its extension
// (.py, .ts); unspecified for anything else.
func ScriptLanguageOf(name string) ScriptLanguage { return scriptLanguages[path.Ext(name)] }

// scanBundle fills in what the skill's folder holds (SK-97, SK-98): its
// resources (the files beside SKILL.md, and everything under
// references/), its references, and, when the front matter declares no
// scripts, those in scripts/ (.py and .ts), named by their file name. A
// skill that declares its scripts keeps exactly those: its other files
// there may be helpers they import.
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

	if len(s.Scripts) > 0 {
		return
	}
	scripts, _ := fs.ReadDir(s.fsys, path.Join(s.root, ScriptsDir))
	for _, e := range scripts {
		lang := ScriptLanguageOf(e.Name())
		if !e.Type().IsRegular() || lang == LanguageUnspecified {
			continue
		}
		name := strings.TrimSuffix(e.Name(), path.Ext(e.Name()))
		s.Scripts = append(s.Scripts, ScriptDefinition{Name: name, Language: lang, RelativePath: ScriptsDir + "/" + e.Name(), Found: true})
	}
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
