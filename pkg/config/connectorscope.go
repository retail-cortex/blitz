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

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// ConnectorScope keeps which connections the person turned off in each
// workspace (spec_connectors_047 CON-17): connector-scope.json in the
// settings directory, by the workspace's folder with symbolic links
// resolved, each workspace's connections off by their key
// (<scope>/<category>/<name>). It's the person's, never the project's,
// so a repository can't turn a global connection on or off.
type ConnectorScope struct {
	path string
	mu   sync.Mutex
}

type connectorScopeEntry struct {
	Off []string `json:"off"`
}

// OpenConnectorScope is the store in the settings directory dir.
func OpenConnectorScope(dir string) *ConnectorScope {
	return &ConnectorScope{path: filepath.Join(dir, "connector-scope.json")}
}

// Off is the keys of the connections turned off in workspace. It reads
// the file each time, so a change made in another process counts at
// once.
func (s *ConnectorScope) Off(workspace string) map[string]bool {
	key, err := CanonicalWorkspace(workspace)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	off := map[string]bool{}
	for _, k := range s.readLocked()[key].Off {
		off[k] = true
	}
	return off
}

// Set turns the connection with key conn on or off in workspace.
func (s *ConnectorScope) Set(workspace, conn string, on bool) error {
	return s.change(workspace, func(off []string) []string {
		off = slices.DeleteFunc(off, func(k string) bool { return k == conn })
		if !on {
			off = append(off, conn)
		}
		return off
	})
}

// Rename keeps a renamed connection's choice (from, to: keys) in
// workspace, or in every workspace when it's "" (a global connection's
// is in each).
func (s *ConnectorScope) Rename(workspace, from, to string) error {
	if from == to {
		return nil
	}
	return s.each(workspace, func(off []string) []string {
		if !slices.Contains(off, from) {
			return off
		}
		off = slices.DeleteFunc(off, func(k string) bool { return k == from || k == to })
		return append(off, to)
	})
}

// Forget drops a deleted connection (its key) from workspace, or from
// every workspace when it's "".
func (s *ConnectorScope) Forget(workspace, conn string) error {
	return s.each(workspace, func(off []string) []string {
		return slices.DeleteFunc(off, func(k string) bool { return k == conn })
	})
}

// each applies f to workspace's list ("": every workspace's) and writes
// what changed.
func (s *ConnectorScope) each(workspace string, f func([]string) []string) error {
	if workspace != "" {
		return s.change(workspace, f)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.readLocked()
	changed := false
	for ws, e := range all {
		after := f(slices.Clone(e.Off))
		slices.Sort(after)
		if slices.Equal(slices.Sorted(slices.Values(e.Off)), after) {
			continue
		}
		changed = true
		if len(after) == 0 {
			delete(all, ws)
		} else {
			all[ws] = connectorScopeEntry{Off: after}
		}
	}
	if !changed {
		return nil
	}
	return s.writeLocked(all)
}

// change applies f to workspace's list and writes it when it changed.
func (s *ConnectorScope) change(workspace string, f func([]string) []string) error {
	key, err := CanonicalWorkspace(workspace)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.readLocked()
	before := all[key].Off
	after := f(slices.Clone(before))
	slices.Sort(after)
	if slices.Equal(slices.Sorted(slices.Values(before)), after) {
		return nil
	}
	if len(after) == 0 {
		delete(all, key)
	} else {
		all[key] = connectorScopeEntry{Off: after}
	}
	return s.writeLocked(all)
}

// readLocked reads the choices; a missing or damaged file has none.
func (s *ConnectorScope) readLocked() map[string]connectorScopeEntry {
	var f struct {
		Workspaces map[string]connectorScopeEntry `json:"workspaces"`
	}
	if data, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(data, &f)
	}
	if f.Workspaces == nil {
		f.Workspaces = map[string]connectorScopeEntry{}
	}
	return f.Workspaces
}

// writeLocked writes the choices in place of the file, atomically.
func (s *ConnectorScope) writeLocked(all map[string]connectorScopeEntry) error {
	data, err := json.MarshalIndent(map[string]any{"version": 1, "workspaces": all}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".scope-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
