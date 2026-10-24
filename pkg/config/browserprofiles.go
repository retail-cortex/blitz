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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"sync"
	"time"
)

// BrowserProfiles keeps the person's browser profiles and which one each
// workspace browses with (spec_browser_profiles_049): browser-profiles.json
// in the settings directory, and each profile's data (cookies, sign-ins,
// local storage) in browser/profiles/<name> beside it, owner-only. A
// profile's cookies are credentials, so it's the person's, never a
// project's: no project setting chooses one.
type BrowserProfiles struct {
	path string
	root string
	mu   sync.Mutex
}

// BrowserProfile is a named profile.
type BrowserProfile struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	// AllowDomains are the sites the agents may browse with it without
	// asking; any other site asks, whatever web.allow_domains says.
	AllowDomains []string `json:"allow_domains,omitempty"`
}

type browserProfilesFile struct {
	Version    int               `json:"version"`
	Profiles   []BrowserProfile  `json:"profiles"`
	Workspaces map[string]string `json:"workspaces"` // canonical folder → profile
}

// ErrNoBrowserProfile means no profile has the name.
var ErrNoBrowserProfile = errors.New("no browser profile by that name")

var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ValidBrowserProfileName reports whether name can name a profile:
// lowercase letters, digits, - and _, up to 32.
func ValidBrowserProfileName(name string) bool { return profileName.MatchString(name) }

// OpenBrowserProfiles is the store in the settings directory dir.
func OpenBrowserProfiles(dir string) *BrowserProfiles {
	return &BrowserProfiles{path: filepath.Join(dir, "browser-profiles.json"), root: filepath.Join(dir, "browser", "profiles")}
}

// Dir is where profile name keeps its data.
func (s *BrowserProfiles) Dir(name string) string { return filepath.Join(s.root, name) }

// List is the profiles, by name.
func (s *BrowserProfiles) List() []BrowserProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked().Profiles
}

// Get is profile name.
func (s *BrowserProfiles) Get(name string) (BrowserProfile, bool) {
	for _, p := range s.List() {
		if p.Name == name {
			return p, true
		}
	}
	return BrowserProfile{}, false
}

// Create makes profile name, empty; its folder is made owner-only.
func (s *BrowserProfiles) Create(name string, now time.Time) (BrowserProfile, error) {
	if !ValidBrowserProfileName(name) {
		return BrowserProfile{}, fmt.Errorf("profile name %q: lowercase letters, digits, - and _, up to 32", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.readLocked()
	if slices.ContainsFunc(f.Profiles, func(p BrowserProfile) bool { return p.Name == name }) {
		return BrowserProfile{}, fmt.Errorf("a browser profile named %q exists", name)
	}
	if err := os.MkdirAll(s.Dir(name), 0o700); err != nil {
		return BrowserProfile{}, err
	}
	p := BrowserProfile{Name: name, Created: now.UTC()}
	f.Profiles = append(f.Profiles, p)
	return p, s.writeLocked(f)
}

// Delete removes profile name, its data, and the workspaces' choice of it.
func (s *BrowserProfiles) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.readLocked()
	i := slices.IndexFunc(f.Profiles, func(p BrowserProfile) bool { return p.Name == name })
	if i < 0 {
		return fmt.Errorf("%w: %q", ErrNoBrowserProfile, name)
	}
	f.Profiles = slices.Delete(f.Profiles, i, i+1)
	for ws, p := range f.Workspaces {
		if p == name {
			delete(f.Workspaces, ws)
		}
	}
	if err := s.writeLocked(f); err != nil {
		return err
	}
	return os.RemoveAll(s.Dir(name))
}

// Clear signs profile name out of everything: its data is removed, the
// profile kept.
func (s *BrowserProfiles) Clear(name string) error {
	if _, ok := s.Get(name); !ok {
		return fmt.Errorf("%w: %q", ErrNoBrowserProfile, name)
	}
	if err := os.RemoveAll(s.Dir(name)); err != nil {
		return err
	}
	return os.MkdirAll(s.Dir(name), 0o700)
}

// SetAllowDomains is the sites profile name browses without asking.
func (s *BrowserProfiles) SetAllowDomains(name string, domains []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.readLocked()
	i := slices.IndexFunc(f.Profiles, func(p BrowserProfile) bool { return p.Name == name })
	if i < 0 {
		return fmt.Errorf("%w: %q", ErrNoBrowserProfile, name)
	}
	f.Profiles[i].AllowDomains = slices.Compact(slices.Sorted(slices.Values(domains)))
	return s.writeLocked(f)
}

// ForWorkspace is the profile workspace browses with ("": none, a
// throwaway one each time).
func (s *BrowserProfiles) ForWorkspace(workspace string) string {
	key, err := CanonicalWorkspace(workspace)
	if err != nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked().Workspaces[key]
}

// SetWorkspace makes workspace browse with profile name ("": none).
func (s *BrowserProfiles) SetWorkspace(workspace, name string) error {
	key, err := CanonicalWorkspace(workspace)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.readLocked()
	if name == "" {
		delete(f.Workspaces, key)
		return s.writeLocked(f)
	}
	if !slices.ContainsFunc(f.Profiles, func(p BrowserProfile) bool { return p.Name == name }) {
		return fmt.Errorf("%w: %q", ErrNoBrowserProfile, name)
	}
	f.Workspaces[key] = name
	return s.writeLocked(f)
}

// readLocked reads the file; a missing or damaged one has no profiles.
func (s *BrowserProfiles) readLocked() browserProfilesFile {
	var f browserProfilesFile
	if data, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(data, &f)
	}
	if f.Workspaces == nil {
		f.Workspaces = map[string]string{}
	}
	sort.Slice(f.Profiles, func(i, j int) bool { return f.Profiles[i].Name < f.Profiles[j].Name })
	return f
}

func (s *BrowserProfiles) writeLocked(f browserProfilesFile) error {
	f.Version = 1
	return writeJSONAtomic(s.path, f)
}
