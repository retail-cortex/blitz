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

// ManagedURL is the organization's service the machine-wide settings set
// ("" when they set none, or there are none).
func ManagedURL() string { return loadManaged().Enterprise.URL }

func loadManaged() managedFile {
	var m managedFile
	data, err := os.ReadFile(ManagedSettingsPath())
	if err != nil {
		return m
	}
	if _, err := toml.Decode(string(data), &m); err != nil {
		return managedFile{}
	}
	return m
}

// applyManaged puts the machine-wide settings over the person's: device
// management's organization, and where its workspaces go.
func applyManaged(cfg *Config) {
	m := loadManaged()
	if m.Enterprise.URL == "" {
		return
	}
	cfg.Enterprise.URL, cfg.Enterprise.Managed = m.Enterprise.URL, true
	if m.Enterprise.WorkspacesDir != "" {
		cfg.Enterprise.WorkspacesDir = m.Enterprise.WorkspacesDir
	}
}
