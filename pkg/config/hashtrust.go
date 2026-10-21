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
	"sync"
	"time"
)

// HashTrust keeps what the person reviewed and trusted, by path (symbolic
// links resolved) and at which content (a hash): something changed since
// needs review again. It holds skills' scripts (skill-trust.json,
// spec_skills_013 SK-108) and workspace connection files
// (connector-trust.json, spec_connectors_047 CON-09).
type HashTrust struct {
	path string
	// key names the decisions in the file: skills, or connections.
	key string
	mu  sync.Mutex
}

type hashTrustEntry struct {
	Hash string    `json:"hash"`
	At   time.Time `json:"at"`
}

// OpenSkillTrust is the skills' store in the settings directory dir, by
// each skill's folder.
func OpenSkillTrust(dir string) *HashTrust {
	return &HashTrust{path: filepath.Join(dir, "skill-trust.json"), key: "skills"}
}

// OpenConnectorTrust is the store of the workspace connection files the
// person trusted, by file, in the settings directory dir.
func OpenConnectorTrust(dir string) *HashTrust {
	return &HashTrust{path: filepath.Join(dir, "connector-trust.json"), key: "connections"}
}

// Reviewed reports whether path was trusted with content hash. It reads
// the file each time, so a decision made in another process counts at
// once.
func (s *HashTrust) Reviewed(path, hash string) bool {
	if hash == "" {
		return false
	}
	key, err := CanonicalWorkspace(path)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()[key].Hash == hash
}

// Trust records that path is trusted with content hash.
func (s *HashTrust) Trust(path, hash string) error {
	key, err := CanonicalWorkspace(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.readLocked()
	if all[key].Hash == hash {
		return nil
	}
	all[key] = hashTrustEntry{Hash: hash, At: time.Now().UTC()}
	return s.writeLocked(all)
}

// Forget drops what was trusted at path (one deleted or moved): its path
// as given and as it resolved, since a removed file's links can't be.
func (s *HashTrust) Forget(path string) error {
	keys := []string{path}
	if abs, err := filepath.Abs(path); err == nil {
		keys = append(keys, abs)
		if dir, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
			keys = append(keys, filepath.Join(dir, filepath.Base(abs)))
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.readLocked()
	n := len(all)
	for _, k := range keys {
		delete(all, k)
	}
	if len(all) == n {
		return nil
	}
	return s.writeLocked(all)
}

// writeLocked writes the decisions in place of the file, atomically.
func (s *HashTrust) writeLocked(all map[string]hashTrustEntry) error {
	return writeJSONAtomic(s.path, map[string]any{"version": 1, s.key: all})
}

// readLocked reads the decisions; a missing or damaged file has none.
func (s *HashTrust) readLocked() map[string]hashTrustEntry {
	var f map[string]json.RawMessage
	all := map[string]hashTrustEntry{}
	if data, err := os.ReadFile(s.path); err == nil && json.Unmarshal(data, &f) == nil {
		_ = json.Unmarshal(f[s.key], &all)
	}
	if all == nil {
		all = map[string]hashTrustEntry{}
	}
	return all
}
