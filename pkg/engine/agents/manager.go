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

// Package agents loads the agents a workspace offers: the built-in ones and
// Markdown files with YAML frontmatter from ~/.blitz/agents (and the
// project's, when it's trusted), each a name, a description, a system
// prompt, the tools it may use and optionally its own model
// (spec_agents_014).
package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents/builtin"
)

// Registry manages discovered and built-in agents.
type Registry struct {
	mu      sync.RWMutex
	agents  map[string]*AgentSpec
	builtin map[string]bool
	// dirs are the external folders loaded, in order; stamp is what they
	// held then (see Refresh).
	dirs  []string
	stamp string
}

// NewRegistry creates a new Registry and loads built-in embedded agent specs.
func NewRegistry() (*Registry, error) {
	r := &Registry{
		agents:  make(map[string]*AgentSpec),
		builtin: make(map[string]bool),
	}

	if err := r.loadEmbeddedAgents(); err != nil {
		return nil, fmt.Errorf("failed to load embedded agents: %w", err)
	}

	return r, nil
}

func (r *Registry) loadEmbeddedAgents() error {
	entries, err := builtin.FS.ReadDir(".")
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		data, err := builtin.FS.ReadFile(entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read embedded agent %s: %w", entry.Name(), err)
		}

		spec, err := ParseMarkdownSpec(data)
		if err != nil {
			return fmt.Errorf("failed to parse embedded agent %s: %w", entry.Name(), err)
		}

		r.agents[spec.Name] = spec
		r.builtin[spec.Name] = true
	}

	return nil
}

// LoadExternalAgents scans directories for user-defined .md agent specifications.
// A leading "~" is expanded. External specs may add new agents but may not
// replace built-in ones, since that would let a directory silently swap the
// system prompt and tool list of a trusted persona. Rejected or unparsable
// specs are reported in the returned (joined) error; valid ones are still
// loaded. The directories are remembered for Refresh.
func (r *Registry) LoadExternalAgents(dirs ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, dir := range dirs {
		r.dirs = append(r.dirs, config.ExpandHome(dir))
	}
	r.stamp = stamp(r.dirs)
	return errors.Join(r.load(r.agents, dirs)...)
}

// Refresh loads the external agents again if a file in their directories
// was added, changed or removed since they were last loaded, and reports
// whether it did. An agent whose file is gone is gone.
func (r *Registry) Refresh() (bool, error) {
	r.mu.RLock()
	dirs, was := r.dirs, r.stamp
	r.mu.RUnlock()
	now := stamp(dirs)
	if now == was {
		return false, nil
	}
	agents := make(map[string]*AgentSpec, len(r.agents))
	r.mu.RLock()
	for name, spec := range r.agents {
		if r.builtin[name] {
			agents[name] = spec
		}
	}
	r.mu.RUnlock()
	errs := r.load(agents, dirs)
	r.mu.Lock()
	r.agents, r.stamp = agents, now
	r.mu.Unlock()
	return true, errors.Join(errs...)
}

// IsBuiltin reports whether name is a built-in agent's.
func (r *Registry) IsBuiltin(name string) bool { return r.builtin[name] }

// load adds the agents in dirs to into (later directories win), returning
// what couldn't be loaded. r.builtin is fixed once the registry is made.
func (r *Registry) load(into map[string]*AgentSpec, dirs []string) []error {
	var errs []error
	for _, dir := range dirs {
		dir = config.ExpandHome(dir)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		// The callback never fails, so neither does the walk.
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
				return nil
			}

			spec, err := ParseMarkdownSpec(data)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
				return nil
			}
			if r.builtin[spec.Name] {
				errs = append(errs, fmt.Errorf("%s: agent name %q is reserved by a built-in agent", path, spec.Name))
				return nil
			}
			spec.Path = path
			into[spec.Name] = spec
			return nil
		})
	}
	return errs
}

// stamp sums up the agent files in dirs (their paths, sizes and times), so
// a change to any of them changes it.
func stamp(dirs []string) string {
	h := sha256.New()
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			if info, err := os.Stat(path); err == nil {
				fmt.Fprintf(h, "%s\x00%d\x00%d\n", path, info.Size(), info.ModTime().UnixNano())
			} else {
				fmt.Fprintf(h, "%s\x00?\n", path)
			}
			return nil
		})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Get retrieves an agent spec by name.
func (r *Registry) Get(name string) (*AgentSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	spec, ok := r.agents[name]
	return spec, ok
}

// List returns all registered agents sorted by name.
func (r *Registry) List() []*AgentSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]*AgentSpec, 0, len(r.agents))
	for _, spec := range r.agents {
		list = append(list, spec)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})

	return list
}
