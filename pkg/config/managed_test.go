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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Device management's machine-wide settings (spec_053 EN-10): its
// organization and workspaces folder over the person's, which they can't
// change while it's set; a file that doesn't parse sets nothing; the
// machine's own place on each system.
func TestManagedSettings(t *testing.T) {
	keysEnv(t)
	managed := filepath.Join(t.TempDir(), "managed.toml")
	t.Setenv(ManagedEnv, managed)
	assert.Empty(t, ManagedURL(), "no file")
	_, err := SetEnterpriseURL("", "https://mine.example")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(managed, []byte("[enterprise]\nurl = \"https://blitz.acme.com\"\nworkspaces_dir = \"/Users/Shared/Acme\"\n"), 0o644))
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, "https://blitz.acme.com", cfg.Enterprise.URL)
	assert.Equal(t, "/Users/Shared/Acme", cfg.Enterprise.WorkspacesDir)
	assert.True(t, cfg.Enterprise.Managed)
	_, err = SetEnterpriseURL("", "")
	assert.ErrorIs(t, err, ErrManaged)

	require.NoError(t, os.WriteFile(managed, []byte("[enterprise\nurl ="), 0o644))
	cfg, err = Load("")
	require.NoError(t, err)
	assert.Equal(t, "https://mine.example", cfg.Enterprise.URL, "a broken file sets nothing")
	assert.False(t, cfg.Enterprise.Managed)

	t.Setenv(ManagedEnv, "")
	assert.Equal(t, filepath.Base(ManagedSettingsPath()), "managed.toml")
	assert.True(t, filepath.IsAbs(ManagedSettingsPath()))
}
