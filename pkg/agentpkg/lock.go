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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// LockFile is a workspace's lockfile, beside its assets (PK-12):
// .agents/blitz.lock.
const LockFile = "blitz.lock"

// Lock is what a workspace added from elsewhere (PK-12): each asset by
// "kind/name", where it came from, the version it resolved to and its
// files' digest, so `blitz pkg verify` tells a change. A superset of
// Castor's .manifest.lock (its skills' URI and SHA-256).
type Lock struct {
	Version string               `json:"version"`
	Assets  map[string]LockEntry `json:"assets"`
}

// LockEntry is one asset a lock holds.
type LockEntry struct {
	Kind    string    `json:"kind"`
	Name    string    `json:"name"`
	URI     string    `json:"uri"`
	Version string    `json:"version,omitempty"`
	SHA256  string    `json:"sha256"`
	Added   time.Time `json:"added"`
}

// ReadLock reads the lockfile at path: an empty lock when there's none.
func ReadLock(path string) (Lock, error) {
	l := Lock{Version: "1", Assets: map[string]LockEntry{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return l, fmt.Errorf("%s: %w", path, err)
	}
	if l.Assets == nil {
		l.Assets = map[string]LockEntry{}
	}
	return l, nil
}

// Write writes l at path.
func (l Lock) Write(path string) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Keys are the lock's assets' keys, in order.
func (l Lock) Keys() []string {
	out := make([]string, 0, len(l.Assets))
	for k := range l.Assets {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// ManifestFile is a compiled manifest's name (PK-13), beside the assets.
const ManifestFile = "agents_manifest.json"

// Manifest is a workspace's assets compiled into one file (PK-13), for
// loaders that read it rather than the folders: each asset by
// "kind/name", with its version, description, files' digest, where it
// came from, its dependencies and its files.
type Manifest struct {
	Schema int                      `json:"schema"`
	Assets map[string]ManifestAsset `json:"assets"`
}

// ManifestAsset is one asset in a manifest.
type ManifestAsset struct {
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	Version     string            `json:"version,omitempty"`
	Description string            `json:"description,omitempty"`
	SHA256      string            `json:"sha256"`
	SourceURI   string            `json:"source_uri"`
	Deps        []Dep             `json:"deps,omitempty"`
	Files       map[string]string `json:"files"`
}
