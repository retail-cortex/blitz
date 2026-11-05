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
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

// ManagedEnv names a managed settings file in place of the machine's, for
// tests and for trying a file before device management sends it.
const ManagedEnv = "BLITZ_MANAGED_SETTINGS"

// ErrManaged is the refusal to change what device management sets.
var ErrManaged = errors.New("your organization's device management sets this computer's organization: ask your IT team to change it")

// managedFile is the machine-wide settings a company's device management
// installs (spec_053 EN-10): what they set, the person can't change.
type managedFile struct {
	Enterprise struct {
		URL           string `toml:"url"`
		WorkspacesDir string `toml:"workspaces_dir"`
	} `toml:"enterprise"`
}

// ManagedSettingsPath is where device management puts the machine-wide
// settings: /Library/Application Support/Blitz/managed.toml on macOS,
// %ProgramData%\Blitz\managed.toml on Windows, /etc/blitz/managed.toml
// elsewhere; BLITZ_MANAGED_SETTINGS names another.
func ManagedSettingsPath() string {
	if p := os.Getenv(ManagedEnv); p != "" {
		return p
	}
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/Blitz/managed.toml"
	case "windows":
		return filepath.Join(cmp.Or(os.Getenv("ProgramData"), `C:\ProgramData`), "Blitz", "managed.toml")
	}
	return "/etc/blitz/managed.toml"
}

// Managed reports whether device management manages this computer's
// organization: its machine-wide settings set a service, or are there but
// can't be read (a damaged file doesn't hand the choice back to the
// person). While it does, the person can't change it (ErrManaged).
func Managed() bool {
	m, err := loadManaged()
	return err != nil || m.Enterprise.URL != ""
}

// loadManaged reads the machine-wide settings: none when there's no file,
// an error when there's one that can't be read or decoded.
func loadManaged() (managedFile, error) {
	var m managedFile
	path := ManagedSettingsPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if _, err := toml.Decode(string(data), &m); err != nil {
		return managedFile{}, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// applyManaged puts the machine-wide settings over the person's: device
// management's organization, and where its workspaces go. A file that
// can't be read keeps the organization managed (the person's own URL, if
// any, stays, but they can't change it) and says so.
func applyManaged(cfg *Config) {
	m, err := loadManaged()
	if err != nil {
		cfg.Enterprise.Managed = true
		cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("your device management's settings can't be read (%v): ask your IT team", err))
		return
	}
	if m.Enterprise.URL == "" {
		return
	}
	cfg.Enterprise.URL, cfg.Enterprise.Managed = m.Enterprise.URL, true
	if m.Enterprise.WorkspacesDir != "" {
		cfg.Enterprise.WorkspacesDir = m.Enterprise.WorkspacesDir
	}
}
