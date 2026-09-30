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
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// What a person decided about a workspace's project settings, for the
// content they saw (Project.Hash).
const (
	TrustNone     = "none"     // nothing to trust: no tier B settings
	TrustNew      = "new"      // not asked yet
	TrustChanged  = "changed"  // trusted or declined, but the content has changed since
	TrustTrusted  = "trusted"  // trusted, as it is now
	TrustDeclined = "declined" // declined, as it is now
)

// TrustStore keeps the decisions in trust.json in the settings
// directory, by canonical workspace directory. Writes are atomic.
type TrustStore struct {
	path string
	mu   sync.Mutex
}

// trustEntry is one workspace's decision.
type trustEntry struct {
	Hash    string    `json:"hash"`
	Trusted bool      `json:"trusted"`
	At      time.Time `json:"at"`
}

// OpenTrustStore is the store in settings directory dir (as ConfigDir
// returns).
func OpenTrustStore(dir string) *TrustStore {
	return &TrustStore{path: filepath.Join(dir, "trust.json")}
}

// CanonicalWorkspace is the key a workspace's trust is kept under: its
// absolute path with symbolic links resolved, and nothing read from the
// workspace itself.
func CanonicalWorkspace(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// State is the decision for workspace dir's content hash ("" when there
// is nothing to trust).
func (s *TrustStore) State(dir, hash string) string {
	if hash == "" {
		return TrustNone
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.readLocked()[dir]
	switch {
	case !ok:
		return TrustNew
	case e.Hash != hash:
		return TrustChanged
	case e.Trusted:
		return TrustTrusted
	}
	return TrustDeclined
}

// Set records the decision for workspace dir's content hash.
func (s *TrustStore) Set(dir, hash string, trusted bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.readLocked()
	all[dir] = trustEntry{Hash: hash, Trusted: trusted, At: time.Now().UTC()}
	return s.writeLocked(all)
}

// Forget removes the decision for workspace dir, so it's asked again.
func (s *TrustStore) Forget(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.readLocked()
	if _, ok := all[dir]; !ok {
		return nil
	}
	delete(all, dir)
	return s.writeLocked(all)
}

// readLocked reads the decisions; a missing or damaged file has none.
func (s *TrustStore) readLocked() map[string]trustEntry {
	var f struct {
		Workspaces map[string]trustEntry `json:"workspaces"`
	}
	if data, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(data, &f)
	}
	if f.Workspaces == nil {
		f.Workspaces = map[string]trustEntry{}
	}
	return f.Workspaces
}

func (s *TrustStore) writeLocked(all map[string]trustEntry) error {
	data, err := json.MarshalIndent(struct {
		Version    int                   `json:"version"`
		Workspaces map[string]trustEntry `json:"workspaces"`
	}{1, all}, "", "  ")
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
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return errors.Join(errors.New("saving the trust decision"), err)
	}
	return nil
}
