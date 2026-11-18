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

// The global settings are the settings directory's .env.toml and device
// management's file; a stamp of them changes when either's content does,
// or one appears or goes, and not when a file is only touched.
func TestSettingsStamp(t *testing.T) {
	dir := t.TempDir()
	managed := filepath.Join(t.TempDir(), "managed.toml")
	t.Setenv(ManagedEnv, managed)
	files := GlobalSettingsFiles(dir)
	assert.Equal(t, []string{filepath.Join(dir, ".env.toml"), managed}, files)

	settings := filepath.Join(dir, ".env.toml")
	write := func(p, text string) func() {
		return func() { require.NoError(t, os.WriteFile(p, []byte(text), 0o600)) }
	}
	tests := []struct {
		name    string
		change  func()
		changed bool
	}{
		{name: "nothing", change: func() {}},
		{name: "the settings appear", change: write(settings, "[web]\nenabled = true\n"), changed: true},
		{name: "written again, the same", change: write(settings, "[web]\nenabled = true\n")},
		{name: "a setting changes", change: write(settings, "[web]\nenabled = false\n"), changed: true},
		{name: "device management's appear", change: write(managed, "[enterprise]\nurl = \"https://blitz.example\"\n"), changed: true},
		{name: "the settings go", change: func() { require.NoError(t, os.Remove(settings)) }, changed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := SettingsStamp(files)
			tt.change()
			assert.Equal(t, tt.changed, SettingsStamp(files) != before)
		})
	}
}
