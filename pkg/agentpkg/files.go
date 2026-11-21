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
package agentpkg

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Folders are where each kind of asset lives under an assets folder (a
// workspace's .agents, or ~/.blitz): agents/<name>.md,
// skills/<name>/SKILL.md and its other files, workflows/<name>.flow.yaml,
// rules/<name>.md, guardrails/<name>.md, evals/<name>.md (PK-11).
var Folders = map[string]string{"agent": "agents", "skill": "skills", "workflow": "workflows", "rule": "rules", "guardrail": "guardrails", "eval": "evals"}

// MainFile is an asset's main file under its assets folder.
func MainFile(kind, name string) string {
	switch kind {
	case "skill":
		return "skills/" + name + "/SKILL.md"
	case "workflow":
		return "workflows/" + name + ".flow.yaml"
	}
	return Folders[kind] + "/" + name + ".md"
}

// Owns reports whether path (under the assets folder) is one of the
// kind's asset name's files: its main file, or, for a skill, one in its
// folder.
func Owns(kind, name, path string) bool {
	if strings.Contains(path, "..") || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") {
		return false
	}
	if kind == "skill" {
		return strings.HasPrefix(path, "skills/"+name+"/") && len(path) > len("skills/"+name+"/")
	}
	return path == MainFile(kind, name)
}

// The most of an asset that's read (an organization's service's limits).
const (
	MaxFile  = 1 << 20
	MaxTotal = 5 << 20
	MaxFiles = 200
)

// ReadAssets are the assets of kinds under root, by "kind/name", each its
// files by path under root; skipped are those that can't be read as a
// package, and why: a name Blitz doesn't give, a file that isn't text or
// is over 1 MB, an asset over 5 MB or 200 files. Hidden files are left
// out, and a linked skill folder isn't read.
func ReadAssets(root string, kinds ...string) (map[string]map[string]string, map[string]string) {
	out, skipped := map[string]map[string]string{}, map[string]string{}
	for _, kind := range kinds {
		dir := Folders[kind]
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			name, ok := assetName(kind, e)
			if !ok {
				continue
			}
			key := kind + "/" + name
			if !ValidName(name) {
				skipped[key] = "its name isn't lowercase letters, digits and single hyphens or underscores"
				continue
			}
			files, why := readAsset(root, kind, name)
			if why != "" {
				skipped[key] = why
				continue
			}
			out[key] = files
		}
	}
	return out, skipped
}

// ReadAsset is one asset's files under root, or why it can't be read.
func ReadAsset(root, kind, name string) (map[string]string, string) {
	return readAsset(root, kind, name)
}

// assetName is the asset e is in a kind's folder, if it is one.
func assetName(kind string, e fs.DirEntry) (string, bool) {
	n := e.Name()
	if strings.HasPrefix(n, ".") {
		return "", false
	}
	switch kind {
	case "skill":
		return n, e.IsDir()
	case "workflow":
		name, ok := strings.CutSuffix(n, ".flow.yaml")
		return name, ok && !e.IsDir()
	}
	name, ok := strings.CutSuffix(n, ".md")
	return name, ok && !e.IsDir()
}

func readAsset(root, kind, name string) (map[string]string, string) {
	files := map[string]string{}
	add := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		switch {
		case err != nil:
			return err.Error()
		case len(b) > MaxFile:
			return rel + " is over 1 MB"
		case !utf8.Valid(b):
			return rel + " isn't text"
		}
		files[rel] = string(b)
		return ""
	}
	if kind != "skill" {
		if why := add(MainFile(kind, name)); why != "" {
			return nil, why
		}
		return files, ""
	}
	base := filepath.Join(root, "skills", name)
	var why string
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && p != base {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if why = add(filepath.ToSlash(rel)); why != "" {
			return fs.SkipAll
		}
		return nil
	})
	switch {
	case err != nil:
		return nil, err.Error()
	case why != "":
		return nil, why
	case files[MainFile(kind, name)] == "":
		return nil, "it has no SKILL.md"
	}
	total := 0
	for _, t := range files {
		total += len(t)
	}
	if total > MaxTotal || len(files) > MaxFiles {
		return nil, "it's over 5 MB or 200 files"
	}
	return files, ""
}

// WriteAsset writes an asset's files under root, replacing what it had;
// every path must be its own (Owns).
func WriteAsset(root, kind, name string, files map[string]string) error {
	// The name is a folder or file under root: one an organization's
	// service sends ("../..") never reaches outside it (packages#1).
	if !ValidName(name) {
		return &fs.PathError{Op: "write", Path: name, Err: fs.ErrInvalid}
	}
	for p := range files {
		if !Owns(kind, name, p) {
			return &fs.PathError{Op: "write", Path: p, Err: fs.ErrInvalid}
		}
	}
	if err := RemoveAsset(root, kind, name); err != nil {
		return err
	}
	for p, text := range files {
		path := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// RemoveAsset removes an asset's files under root; a name Blitz doesn't
// give (ValidName) is refused, so nothing outside root is removed.
func RemoveAsset(root, kind, name string) error {
	if !ValidName(name) {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrInvalid}
	}
	var err error
	if kind == "skill" {
		err = os.RemoveAll(filepath.Join(root, "skills", name))
	} else {
		err = os.Remove(filepath.Join(root, filepath.FromSlash(MainFile(kind, name))))
	}
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
