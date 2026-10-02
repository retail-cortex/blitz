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

package agents

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
)

// The agent files of one folder, as the editors show them: listed with
// why one doesn't load, written from a form, deleted.

// File is one agent file in a folder.
type File struct {
	Path string
	// Spec is what it defines; nil when it doesn't parse.
	Spec *AgentSpec
	// Problem is why it doesn't load ("" when it does).
	Problem string
}

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidName reports whether name can name an agent the editors write:
// lowercase letters, digits, - and _, starting with a letter or digit.
func ValidName(name string) bool { return validName.MatchString(name) }

// ListFiles returns the agent files in dir (a leading "~" is expanded) and
// below it, by path; a folder that doesn't exist has none. builtin names
// the built-in agents, which a file can't redefine.
func ListFiles(dir string, builtin func(string) bool) ([]File, error) {
	dir = config.ExpandHome(dir)
	var out []File
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir && errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		f := File{Path: path}
		data, err := os.ReadFile(path)
		if err == nil {
			f.Spec, err = ParseMarkdownSpec(data)
		}
		switch {
		case err != nil:
			f.Problem = err.Error()
		case builtin(f.Spec.Name):
			f.Problem = fmt.Sprintf("agent name %q is reserved by a built-in agent", f.Spec.Name)
		}
		if f.Spec != nil {
			f.Spec.Path = path
		}
		out = append(out, f)
		return nil
	})
	return out, err
}

// Save writes spec to its file in dir: in place of previous's file when
// previous names one (a rename, or "" for a new agent), else dir/<name>.md.
// What's wrong with spec comes back as problems, with nothing written.
func Save(dir string, spec *AgentSpec, previous string, builtin func(string) bool) (path string, problems []string, err error) {
	dir = config.ExpandHome(dir)
	files, err := ListFiles(dir, builtin)
	if err != nil {
		return "", nil, err
	}
	m := spec.AgentMetadata
	switch {
	case !ValidName(m.Name):
		problems = append(problems, fmt.Sprintf("name %q: use lowercase letters, digits, - and _", m.Name))
	case builtin(m.Name):
		problems = append(problems, fmt.Sprintf("name %q is a built-in agent's", m.Name))
	}
	if strings.TrimSpace(m.Description) == "" {
		problems = append(problems, "description: say what the agent is for")
	}
	if m.AgencyLevel != "" && !slices.Contains(AgencyLevels, m.AgencyLevel) {
		problems = append(problems, fmt.Sprintf("agency_level: %q (one of %s)", m.AgencyLevel, strings.Join(AgencyLevels, ", ")))
	}
	if err := m.Check(); err != nil {
		problems = append(problems, err.Error())
	}
	var old string // previous's file
	for _, f := range files {
		if f.Spec == nil {
			continue
		}
		if previous != "" && f.Spec.Name == previous {
			old = f.Path
		} else if f.Spec.Name == m.Name {
			problems = append(problems, fmt.Sprintf("an agent named %q is already defined in %s", m.Name, f.Path))
		}
	}
	if len(problems) > 0 {
		return "", problems, nil
	}

	data, err := spec.Marshal()
	if err != nil {
		return "", nil, err
	}
	path = filepath.Join(dir, m.Name+".md")
	if old != "" && previous == m.Name {
		path = old // the same agent keeps its file, whatever it's called
	}
	if old != path {
		if _, err := os.Stat(path); err == nil {
			return "", []string{fmt.Sprintf("%s already exists", path)}, nil
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	if err := writeAtomic(path, data); err != nil {
		return "", nil, err
	}
	if old != "" && old != path {
		if err := os.Remove(old); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return path, nil, fmt.Errorf("saved %s, but %s is still there: %w", path, old, err)
		}
	}
	return path, nil, nil
}

// Delete removes the file in dir that defines name.
func Delete(dir, name string) error {
	files, err := ListFiles(dir, func(string) bool { return false })
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.Spec != nil && f.Spec.Name == name {
			return os.Remove(f.Path)
		}
	}
	return &api.UnknownAgentError{Name: name}
}

// DeleteFile removes path, an agent file in dir (or below it), whether or
// not it parses.
func DeleteFile(dir, path string) error {
	dir = config.ExpandHome(dir)
	rel, err := filepath.Rel(dir, path)
	if err != nil || !filepath.IsAbs(path) || rel == "." || strings.HasPrefix(rel, "..") || !strings.HasSuffix(path, ".md") {
		return fmt.Errorf("%s isn't an agent file in %s", path, dir)
	}
	return os.Remove(path)
}

// writeAtomic replaces path with data through a temporary file beside it,
// so a reload never reads half a file.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agent-*.md.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
