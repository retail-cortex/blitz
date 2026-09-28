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
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveUILocale(t *testing.T) {
	cases := []struct {
		name, before string
		want         []string // substrings of the result
	}{
		{"missing file", "", []string{"[ui]\nlocale = \"es\"\n"}},
		{"no ui table", "# top comment\n[llm]\nprovider = \"gemini\"\n",
			[]string{"# top comment\n[llm]\nprovider = \"gemini\"\n\n[ui]\nlocale = \"es\"\n"}},
		{"replace keeps comment", "[ui]\nmarkdown = true\nlocale    = \"en-US\"   # interface language\n\n[memory]\nenabled = true\n",
			[]string{"locale = \"es\"   # interface language\n", "markdown = true", "[memory]\nenabled = true"}},
		{"insert at end of table", "[ui]\nmarkdown = true\n# spinner = false\n\n[memory]\nenabled = true\n",
			[]string{"# spinner = false\nlocale = \"es\"\n\n[memory]"}},
		{"ignores other tables' locale", "[other]\nlocale = \"x\"\n[ui]\nspinner = true\n",
			[]string{"[other]\nlocale = \"x\"\n[ui]\nspinner = true\nlocale = \"es\""}},
		{"ignores sub-tables", "[ui.theme]\nlocale = \"x\"\n",
			[]string{"[ui.theme]\nlocale = \"x\"\n\n[ui]\nlocale = \"es\"\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".env.toml")
			if tc.before != "" {
				os.WriteFile(path, []byte(tc.before), 0o640)
			}
			got, err := SaveUILocale(dir, "es")
			require.NoError(t, err, "SaveUILocale = %q,", got)
			require.Equal(t, path, got, "SaveUILocale = %q, %v", got, err)
			data, _ := os.ReadFile(path)
			for _, w := range tc.want {
				assert.Contains(t, string(data), w, "result lacks %q:\n%s", w, data)
			}
			var cfg struct {
				UI struct{ Locale string } `toml:"ui"`
			}
			_, err = toml.Decode(string(data), &cfg)
			assert.NoError(t, err, "decoded locale %q,", cfg.UI.Locale)
			assert.Equal(t, "es", cfg.UI.Locale, "decoded locale %q, %v", cfg.UI.Locale, err)
			info, _ := os.Stat(path)
			wantPerm := os.FileMode(0o600)
			if tc.before != "" {
				wantPerm = 0o640 // existing permissions are kept
			}
			assert.Equal(t, wantPerm, info.Mode().Perm(), "mode = %v, want %v", info.Mode().Perm(), wantPerm)
		})
	}
}

func TestSaveUILocaleRefusesToBreakConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.toml")
	bad := "[ui\nlocale = \n"
	os.WriteFile(path, []byte(bad), 0o600)
	_, err := SaveUILocale(dir, "es")
	require.Error(t, err, "expected an error for an unparseable file")
	data, _ := os.ReadFile(path)
	assert.Equal(t, bad, string(data), "file must be left untouched on failure")
	_, err = SaveUILocale("", "es")
	assert.Error(t, err, "empty dir should fail")
}

func TestDefaultLocale(t *testing.T) {
	c := DefaultConfig()
	assert.Equal(t, "en-US", c.UI.Locale, "defaults: %q %q", c.UI.Locale, c.UI.LocalesDir)
	assert.True(t, strings.HasSuffix(c.UI.LocalesDir, "locales"), "defaults: %q %q", c.UI.Locale, c.UI.LocalesDir)
}
