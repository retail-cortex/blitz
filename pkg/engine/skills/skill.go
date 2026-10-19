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

// Package skills loads the skills a workspace offers: a SKILL.md with
// frontmatter and instructions, optionally scripts, from the built-in set,
// ~/.blitz/skills and (when trusted) the project; and the policy that caps
// what their scripts may do (spec_skills_013).
package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// SkillMetadata defines metadata stored in SKILL.md frontmatter: the Agent
// Skills spec fields, plus Castor's skill definition fields (see
// definition.go). Unknown keys are ignored, so skills written for other
// tools still load.
type SkillMetadata struct {
	// Agent Skills spec.
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	License       string            `yaml:"license,omitempty"`
	Compatibility string            `yaml:"compatibility,omitempty"`
	AllowedTools  string            `yaml:"allowed-tools,omitempty"`
	Metadata      map[string]string `yaml:"metadata,omitempty"`

	// Castor (castor.skills.v1.SkillDefinition).
	Tags               []string           `yaml:"tags"`
	Version            string             `yaml:"version,omitempty"`
	Author             string             `yaml:"author,omitempty"`
	Authors            []AuthorDetails    `yaml:"authors,omitempty"`
	AllowedToolsLegacy string             `yaml:"allowed_tools,omitempty"`
	ToolRequirements   []ToolRequirement  `yaml:"tool_requirements,omitempty"`
	Category           string             `yaml:"category,omitempty"`
	TriggerPhrases     []string           `yaml:"trigger_phrases,omitempty"`
	ExecutionHints     *ExecutionHints    `yaml:"execution_hints,omitempty"`
	CompiledReference  *CompiledReference `yaml:"compiled_reference,omitempty"`
	Scripts            []ScriptDefinition `yaml:"scripts,omitempty"`
	// ResourceRequirements are Castor's resources: context assets the
	// skill uses, listed by activate_skill.
	ResourceRequirements []ResourceRequirement `yaml:"resources,omitempty"`
	SkillID              string                `yaml:"skill_id,omitempty"`
	URI                  string                `yaml:"uri,omitempty"`
	SourceURI            string                `yaml:"source_uri,omitempty"`

	// Agents are the agents the skill is offered to (none: every one);
	// Members the skills a meta skill combines (spec_skills_013 SK-99).
	Agents  []string `yaml:"agents,omitempty"`
	Members []string `yaml:"skills,omitempty"`
}

// Skill represents an Agent Skill specification.
type Skill struct {
	SkillMetadata
	Content   string   `json:"content"`
	Path      string   `json:"path"`
	Resources []string `json:"resources"`
	// References are the files under references/, as references/<path>
	// (also in Resources).
	References []string `json:"references,omitempty"`
	// PackageFiles are the package files its folder has (requirements.txt,
	// pyproject.toml, package.json; SK-107), as paths in it.
	PackageFiles []string `json:"package_files,omitempty"`
	// Problems are definition errors (see SkillMetadata.Validate).
	Problems []string `json:"problems,omitempty"`

	fsys    fs.FS  // the skill's files, for ContentHash
	root    string // the skill's directory within fsys
	hostDir string // the skill's directory on disk; "" for built-in skills
}

// ParseSkillMD parses a SKILL.md document with YAML frontmatter.
func ParseSkillMD(content []byte, path string) (*Skill, error) {
	trimmed := bytes.TrimSpace(content)
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return nil, fmt.Errorf("SKILL.md missing starting frontmatter delimiter '---'")
	}

	rest := trimmed[3:]
	idx := bytes.Index(rest, []byte("---"))
	if idx == -1 {
		return nil, fmt.Errorf("SKILL.md missing closing frontmatter delimiter '---'")
	}

	frontmatterBytes := rest[:idx]
	instructionBytes := bytes.TrimSpace(rest[idx+3:])

	var meta SkillMetadata
	if err := yaml.Unmarshal(frontmatterBytes, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse SKILL.md frontmatter: %w", err)
	}

	if meta.Name == "" {
		return nil, fmt.Errorf("SKILL.md must specify a name")
	}

	return &Skill{
		SkillMetadata: meta,
		Content:       string(instructionBytes),
		Path:          path,
		Resources:     []string{},
		Problems:      meta.Validate(),
	}, nil
}

// Matches returns true if the query matches the skill's name, description, or tags.
func (s *Skill) Matches(query string) bool {
	q := strings.ToLower(query)
	if strings.Contains(strings.ToLower(s.Name), q) {
		return true
	}
	if strings.Contains(strings.ToLower(s.Description), q) {
		return true
	}
	for _, tag := range s.Tags {
		if strings.Contains(strings.ToLower(tag), q) {
			return true
		}
	}
	for _, p := range s.TriggerPhrases {
		if TriggerMatches(q, p) {
			return true
		}
	}
	return false
}

// TriggerMatches reports whether a request is near a trigger phrase
// (SK-110): one holds the other, or it has most of the phrase's words
// (two thirds of those that aren't stop words, however short: "pdf"
// counts, "the" doesn't; all of them when every one is a stop word), a
// word matching one that starts the same (plurals, other endings).
// Phrases are examples, so no two people need write them alike.
func TriggerMatches(request, phrase string) bool {
	r, p := strings.ToLower(strings.TrimSpace(request)), strings.ToLower(strings.TrimSpace(phrase))
	if r == "" || p == "" {
		return false
	}
	if strings.Contains(r, p) || strings.Contains(p, r) {
		return true
	}
	words := strings.FieldsFunc(r, notWordRune)
	pws := strings.FieldsFunc(p, notWordRune)
	if slices.ContainsFunc(pws, func(w string) bool { return !stopWords[w] }) {
		pws = slices.DeleteFunc(pws, func(w string) bool { return stopWords[w] })
	}
	var want, have int
	for _, pw := range pws {
		want++
		if slices.ContainsFunc(words, func(w string) bool { return sameWord(w, pw) }) {
			have++
		}
	}
	return want > 0 && have*3 >= want*2
}

// stopWords are English words that say little on their own: a trigger
// phrase's other words decide whether a request is near it.
var stopWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`a an the and or but nor so yet if then than as of to in on at by for from with
		without into onto about over under up down out off is are was were be been being am do does did done
		have has had having can could would should will shall may might must it its it's this that these those
		there here i me my mine we us our ours you your yours he him his she her hers they them their theirs
		what which who whom whose when where why how all any some each every no not just also very too more most
		please let lets let's want need like get got make some something`) {
		m[w] = true
	}
	return m
}()

func notWordRune(c rune) bool { return !unicode.IsLetter(c) && !unicode.IsDigit(c) }

// sameWord: equal, or alike enough to be the same word written another
// way: they open with four letters or more alike, three quarters of the
// shorter ("release" and "releases", "summary" and "summarize"; not
// "contract" and "control").
func sameWord(a, b string) bool {
	if a == b {
		return true
	}
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n >= 4 && n*4 >= min(len(a), len(b))*3
}

// maxHashedBytes bounds how much of a skill's directory ContentHash reads.
const maxHashedBytes = 64 << 20

// ContentHash is the SHA-256 of every regular file in the skill's
// directory (SKILL.md, scripts, resources), in path order, each as its
// relative path, its size and its content. It identifies exactly what was
// reviewed, so a config can pin a skill (skills.policy.trusted_hashes).
// Symbolic links are skipped. The format is "sha256:<hex>".
func (s *Skill) ContentHash() (string, error) {
	if s.fsys == nil {
		return "", errors.New("skill has no files to hash")
	}
	h := sha256.New()
	var total int64
	err := fs.WalkDir(s.fsys, s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil // directories, and symbolic links, which could point anywhere
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if total += info.Size(); total > maxHashedBytes {
			return fmt.Errorf("skill directory is larger than %d MB", maxHashedBytes>>20)
		}
		f, err := s.fsys.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		rel := strings.TrimPrefix(strings.TrimPrefix(p, s.root), "/")
		fmt.Fprintf(h, "%s\x00%d\x00", rel, info.Size())
		_, err = io.Copy(h, f)
		return err
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// HostDir is the skill's directory on disk, or "" for built-in skills.
func (s *Skill) HostDir() string { return s.hostDir }

// ScriptPath returns the host path of a script shipped in the skill's
// directory (relative_path), checked through os.Root so neither ".." nor a
// symbolic link can lead outside the directory. Built-in skills have no
// host directory; use CopyTo.
func (s *Skill) ScriptPath(rel string) (string, error) {
	if s.hostDir == "" {
		return "", errors.New("built-in skill: its files aren't on disk")
	}
	if !inBundle(rel) {
		return "", fmt.Errorf("%q is outside the skill's directory", rel)
	}
	root, err := os.OpenRoot(s.hostDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Stat(rel)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q isn't a regular file", rel)
	}
	return filepath.Join(s.hostDir, filepath.FromSlash(rel)), nil
}

// ReadScript returns a script's source shipped in the skill (relative_path):
// from its directory, or from the embedded files of a built-in skill.
func (s *Skill) ReadScript(rel string) ([]byte, error) {
	if s.hostDir != "" {
		p, err := s.ScriptPath(rel)
		if err != nil {
			return nil, err
		}
		return os.ReadFile(p)
	}
	if s.fsys == nil || !inBundle(rel) {
		return nil, fmt.Errorf("%q isn't in the skill", rel)
	}
	return fs.ReadFile(s.fsys, path.Join(s.root, rel))
}

// CopyTo writes the skill's files into dir (for built-in skills, whose
// files are embedded). Symbolic links are skipped.
func (s *Skill) CopyTo(dir string) error {
	if s.fsys == nil {
		return errors.New("skill has no files")
	}
	return fs.WalkDir(s.fsys, s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, s.root), "/")
		target := filepath.Join(dir, filepath.FromSlash(rel))
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o700)
		case !d.Type().IsRegular():
			return nil
		}
		b, err := fs.ReadFile(s.fsys, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o600)
	})
}
