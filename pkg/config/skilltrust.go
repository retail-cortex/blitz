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

// SkillTrust keeps which skills' scripts the user reviewed and trusted,
// at which content (the skill's content hash): skill-trust.json in the
// settings directory, by the skill's folder with symbolic links
// resolved (spec_skills_013 SK-108). A skill whose content changes since
// (a script added or edited, an update from its registry) needs review
// again.
type SkillTrust struct {
	path string
	// key names the decisions in the file: skills, or connections.
	key string
	mu  sync.Mutex
}

type skillTrustEntry struct {
	Hash string    `json:"hash"`
	At   time.Time `json:"at"`
}

// OpenSkillTrust is the store in the settings directory dir.
func OpenSkillTrust(dir string) *SkillTrust {
	return &SkillTrust{path: filepath.Join(dir, "skill-trust.json"), key: "skills"}
}

// OpenConnectorTrust is the store of the workspace connection files the
// user trusted, by their content (spec_connectors_047):
// connector-trust.json in the settings directory dir, by the file's path.
func OpenConnectorTrust(dir string) *SkillTrust {
	return &SkillTrust{path: filepath.Join(dir, "connector-trust.json"), key: "connections"}
}

// Reviewed reports whether the skill in folder dir was trusted with
// content hash. It reads the file each time, so a decision made in
// another process counts at once.
func (s *SkillTrust) Reviewed(dir, hash string) bool {
	if hash == "" {
		return false
	}
	key, err := CanonicalWorkspace(dir)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()[key].Hash == hash
}

// Trust records that the skill in folder dir is trusted with content
// hash.
func (s *SkillTrust) Trust(dir, hash string) error {
	key, err := CanonicalWorkspace(dir)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.readLocked()
	if all[key].Hash == hash {
		return nil
	}
	all[key] = skillTrustEntry{Hash: hash, At: time.Now().UTC()}
	data, err := json.MarshalIndent(map[string]any{"version": 1, s.key: all}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".trust-*.json")
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

// readLocked reads the decisions; a missing or damaged file has none.
func (s *SkillTrust) readLocked() map[string]skillTrustEntry {
	var f map[string]json.RawMessage
	all := map[string]skillTrustEntry{}
	if data, err := os.ReadFile(s.path); err == nil && json.Unmarshal(data, &f) == nil {
		_ = json.Unmarshal(f[s.key], &all)
	}
	if all == nil {
		all = map[string]skillTrustEntry{}
	}
	return all
}
