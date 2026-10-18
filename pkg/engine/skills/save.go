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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Writing a skill from Studio's form (spec_skills_013 SK-100): SKILL.md's
// front matter keeps every field the form doesn't edit, in its order, and
// the files under references/ and scripts/ are written one at a time,
// never outside the skill's folder.

// ErrChanged means SKILL.md changed since it was read (its hash differs).
var ErrChanged = errors.New("the skill changed since it was opened")

// ErrUnknownSkill means there's no skill folder of that name.
var ErrUnknownSkill = errors.New("no such skill")

// Spec is what the form edits: the skill's name (its folder's), what
// it's for, the tools it may use (allowed-tools, as written), the agents
// it's offered to, the skills it combines and its instructions.
type Spec struct {
	Name         string
	Description  string
	Tools        string
	Agents       []string
	Members      []string
	Instructions string
}

// nameRE is a skill's name, as the Agent Skills format has it: lowercase
// letters, digits and single hyphens, not at either end.
var nameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidName reports whether name may name a skill (and its folder).
func ValidName(name string) bool { return len(name) <= 64 && nameRE.MatchString(name) }

// FileHash identifies SKILL.md's content, to tell an edit made elsewhere.
func FileHash(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

// Check returns what keeps spec from being saved: a name that isn't one,
// a missing description, a member that isn't a skill name or is itself.
// reserved says a name a built-in skill has.
func Check(spec Spec, reserved func(string) bool) []string {
	var probs []string
	switch {
	case !ValidName(spec.Name):
		probs = append(probs, fmt.Sprintf("name %q: lowercase letters, digits and single hyphens, up to 64", spec.Name))
	case reserved != nil && reserved(spec.Name):
		probs = append(probs, fmt.Sprintf("name %q is a built-in skill's", spec.Name))
	}
	if strings.TrimSpace(spec.Description) == "" {
		probs = append(probs, "description: say what the skill is for and when to use it")
	}
	for _, m := range spec.Members {
		switch {
		case m == spec.Name:
			probs = append(probs, "skills: a skill can't combine itself")
		case !ValidName(m):
			probs = append(probs, fmt.Sprintf("skills: %q isn't a skill's name", m))
		}
	}
	return probs
}

// Save writes dir/<name>/SKILL.md from spec and returns its path, or the
// problems that keep it from being saved (nothing written). previous is
// the skill being edited ("" for a new one); a different name renames its
// folder. hash, when given, must be its SKILL.md's current FileHash.
func Save(dir string, spec Spec, previous, hash string, reserved func(string) bool) (string, []string, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	if probs := Check(spec, reserved); len(probs) > 0 {
		return "", probs, nil
	}
	target := filepath.Join(dir, spec.Name)
	var old []byte
	if previous != "" {
		if !ValidName(previous) {
			return "", nil, fmt.Errorf("%w: %q", ErrUnknownSkill, previous)
		}
		data, err := os.ReadFile(filepath.Join(dir, previous, "SKILL.md"))
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil, fmt.Errorf("%w: %q", ErrUnknownSkill, previous)
		} else if err != nil {
			return "", nil, err
		}
		if hash != "" && FileHash(data) != hash {
			return "", nil, ErrChanged
		}
		old = data
	}
	if previous != spec.Name {
		if _, err := os.Stat(target); err == nil {
			return "", []string{fmt.Sprintf("name %q: the workspace has a skill by that name", spec.Name)}, nil
		}
	}
	data, err := Render(old, spec)
	if err != nil {
		return "", nil, err
	}
	if previous != "" && previous != spec.Name {
		if err := os.Rename(filepath.Join(dir, previous), target); err != nil {
			return "", nil, err
		}
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", nil, err
	}
	p := filepath.Join(target, "SKILL.md")
	return p, nil, writeAtomic(p, data)
}

// Render is SKILL.md for spec: old's front matter (nil: none) with the
// fields the form edits set, or removed when empty, and the instructions
// as its body.
func Render(old []byte, spec Spec) ([]byte, error) {
	var doc yaml.Node
	if front, _, ok := splitFrontMatter(old); ok {
		if err := yaml.Unmarshal(front, &doc); err != nil {
			return nil, fmt.Errorf("SKILL.md's front matter: %w", err)
		}
	}
	if len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		return nil, errors.New("SKILL.md's front matter isn't a mapping")
	}
	setScalar(m, "name", spec.Name)
	setScalar(m, "description", strings.TrimSpace(spec.Description))
	setScalar(m, "allowed-tools", strings.Join(strings.Fields(spec.Tools), " "))
	setList(m, "agents", spec.Agents)
	setList(m, "skills", spec.Members)
	front, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(front)
	b.WriteString("---\n\n")
	if body := strings.TrimSpace(spec.Instructions); body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.Bytes(), nil
}

// splitFrontMatter is SKILL.md's front matter and body.
func splitFrontMatter(data []byte) (front, body []byte, ok bool) {
	t := bytes.TrimSpace(data)
	if !bytes.HasPrefix(t, []byte("---")) {
		return nil, t, false
	}
	rest := t[3:]
	i := bytes.Index(rest, []byte("\n---"))
	if i < 0 {
		return nil, t, false
	}
	return rest[:i], bytes.TrimSpace(rest[i+4:]), true
}

// index is where key's value is in the mapping m, or -1.
func index(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i + 1
		}
	}
	return -1
}

// set puts v as key's value in m (added at the end when new), or removes
// key when v is nil.
func set(m *yaml.Node, key string, v *yaml.Node) {
	i := index(m, key)
	switch {
	case v == nil && i >= 0:
		m.Content = append(m.Content[:i-1], m.Content[i+1:]...)
	case v == nil:
	case i >= 0:
		m.Content[i] = v
	default:
		m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, v)
	}
}

func setScalar(m *yaml.Node, key, value string) {
	if value == "" {
		set(m, key, nil)
		return
	}
	set(m, key, &yaml.Node{Kind: yaml.ScalarNode, Value: value})
}

func setList(m *yaml.Node, key string, values []string) {
	if len(values) == 0 {
		set(m, key, nil)
		return
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, v := range values {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: v})
	}
	set(m, key, seq)
}

// Delete removes dir/<name>: SKILL.md and everything beside it.
func Delete(dir, name string) error {
	if !ValidName(name) {
		return fmt.Errorf("%w: %q", ErrUnknownSkill, name)
	}
	p := filepath.Join(dir, name)
	if _, err := os.Stat(filepath.Join(p, "SKILL.md")); err != nil {
		return fmt.Errorf("%w: %q", ErrUnknownSkill, name)
	}
	return os.RemoveAll(p)
}

// fileRel checks a path for one of a skill's own files: under
// references/ or scripts/, or a package file beside SKILL.md, inside the
// folder.
func fileRel(rel string) (string, error) {
	rel = path.Clean(strings.TrimSpace(rel))
	if !inBundle(rel) {
		return "", fmt.Errorf("%q is outside the skill's folder", rel)
	}
	if IsPackageFile(rel) {
		return rel, nil // requirements.txt, pyproject.toml, package.json, beside SKILL.md too
	}
	top, rest, _ := strings.Cut(rel, "/")
	if (top != ReferencesDir && top != ScriptsDir) || rest == "" {
		return "", fmt.Errorf("%q: a skill's files go in references/ or scripts/", rel)
	}
	return rel, nil
}

// WriteFile writes one of dir/<name>'s files (references/… or
// scripts/…), making its folders, through os.Root so nothing outside the
// skill's folder is touched.
func WriteFile(dir, name, rel string, data []byte) error {
	root, rel, err := openSkill(dir, name, rel)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(path.Dir(rel), 0o755); err != nil {
		return err
	}
	return root.WriteFile(rel, data, 0o644)
}

// DeleteFile removes one of dir/<name>'s files.
func DeleteFile(dir, name, rel string) error {
	root, rel, err := openSkill(dir, name, rel)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Remove(rel)
}

func openSkill(dir, name, rel string) (*os.Root, string, error) {
	if !ValidName(name) {
		return nil, "", fmt.Errorf("%w: %q", ErrUnknownSkill, name)
	}
	rel, err := fileRel(rel)
	if err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(filepath.Join(dir, name))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %q", ErrUnknownSkill, name)
	}
	return root, rel, nil
}

// writeAtomic writes data to p through a temporary file beside it.
func writeAtomic(p string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".SKILL-*.md")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}
