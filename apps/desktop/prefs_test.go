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

package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrefsDefaultsAndRoundTrip(t *testing.T) {
	s := &prefsStore{path: filepath.Join(t.TempDir(), "blitz", "desktop.json")}
	p, err := s.load()
	require.NoError(t, err, "defaults %+v", p)
	require.Equal(t, "system", p.Theme, "defaults %+v %v", p, err)
	require.Equal(t, "comfortable", p.Density, "defaults %+v %v", p, err)
	require.Equal(t, "full", p.Width, "defaults %+v %v", p, err)
	require.Equal(t, "on", p.Notifications, "defaults %+v %v", p, err)
	require.Len(t, p.Workspaces, 0, "defaults %+v %v", p, err)
	a, b := t.TempDir(), t.TempDir()
	p.Theme = "dark"
	p.Workspaces = []WorkspacePrefs{
		{Dir: a + "/", Name: "  Shop  ", Color: "teal", Open: true},
		{Dir: b, Description: "old", Open: false},
		{Dir: a, Open: true},              // the same directory again
		{Dir: "relative/dir", Open: true}, // not a directory the service can open
	}
	p.Active = b // closed: can't be active
	p.Files, p.ShowHidden, p.ChatWidth, p.FilesWidth, p.RunSettingsWidth = true, true, -3, 99999, 480
	saved, err := s.save(p)
	require.NoError(t, err)
	require.Len(t, saved.Workspaces, 2, "normalized %+v", saved)
	require.Equal(t, a, saved.Workspaces[0].Dir, "normalized %+v", saved)
	require.Equal(t, "Shop", saved.Workspaces[0].Name, "normalized %+v", saved)
	require.Equal(t, a, saved.Active, "normalized %+v", saved)
	require.True(t, saved.Files, "normalized %+v", saved)
	require.True(t, saved.ShowHidden, "normalized %+v", saved)
	require.Equal(t, 0, saved.ChatWidth, "normalized %+v", saved)
	require.Equal(t, 0, saved.FilesWidth, "normalized %+v", saved)
	require.Equal(t, 480, saved.RunSettingsWidth, "normalized %+v", saved)
	info, err := os.Stat(s.path)
	require.NoError(t, err, "file mode %v", info)
	require.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "file mode %v %v", info, err)
	got, err := s.load()
	require.NoError(t, err, "loaded %+v", got)
	require.Equal(t, "dark", got.Theme, "loaded %+v %v", got, err)
	require.Len(t, got.Workspaces, 2, "loaded %+v %v", got, err)
	require.Equal(t, "old", got.Workspaces[1].Description, "loaded %+v %v", got, err)
	require.False(t, got.Workspaces[1].Open, "loaded %+v %v", got, err)
}

// The page reads workspaces as a list: with none it must be [], not null
// (a first start crashed the page when the service came up).
func TestPrefsSendAnEmptyListNotNull(t *testing.T) {
	s := &prefsStore{path: filepath.Join(t.TempDir(), "desktop.json")}
	p, err := s.load()
	require.NoError(t, err)
	data, _ := json.Marshal(p)
	require.Contains(t, string(data), `"workspaces":[]`, "defaults encode as %s", data)
	saved, _ := s.save(Prefs{})
	data, _ = json.Marshal(saved)
	require.Contains(t, string(data), `"workspaces":[]`, "saved encode as %s", data)
}

func TestPrefsKeepOpenWorkspacesFirstAndBoundRecent(t *testing.T) {
	var p Prefs
	for i := range maxRecent + 5 {
		p.Workspaces = append(p.Workspaces, WorkspacePrefs{Dir: filepath.Join("/w", string(rune('a'+i)))})
	}
	p.Workspaces = append(p.Workspaces, WorkspacePrefs{Dir: "/open", Open: true})
	p.Theme = "sepia"
	p.normalize()
	require.Equal(t, "/open", p.Workspaces[0].Dir, "%d workspaces, first %s, theme %s, active %s", len(p.Workspaces), p.Workspaces[0].Dir, p.Theme, p.Active)
	require.Len(t, p.Workspaces, maxRecent+1, "%d workspaces, first %s, theme %s, active %s", len(p.Workspaces), p.Workspaces[0].Dir, p.Theme, p.Active)
	require.Equal(t, "system", p.Theme, "%d workspaces, first %s, theme %s, active %s", len(p.Workspaces), p.Workspaces[0].Dir, p.Theme, p.Active)
	require.Equal(t, "/open", p.Active, "%d workspaces, first %s, theme %s, active %s", len(p.Workspaces), p.Workspaces[0].Dir, p.Theme, p.Active)
}

// Each theme's Markdown CSS is kept as written, up to the cap, and never
// cut inside a character.
func TestPrefsMarkdownCSS(t *testing.T) {
	cases := []struct {
		name, in string
		want     int
	}{
		{"empty", "", 0},
		{"short", ".markdown { --doc-accent: teal; }", 33},
		{"at the cap", strings.Repeat("a", maxMarkdownCSS), maxMarkdownCSS},
		{"over the cap", strings.Repeat("a", maxMarkdownCSS+10), maxMarkdownCSS},
		{"a character across the cap", strings.Repeat("a", maxMarkdownCSS-1) + "é", maxMarkdownCSS - 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Prefs{MarkdownCSSLight: c.in, MarkdownCSSDark: c.in}
			p.normalize()
			assert.Len(t, p.MarkdownCSSLight, c.want)
			assert.Len(t, p.MarkdownCSSDark, c.want)
			assert.True(t, utf8.ValidString(p.MarkdownCSSLight))
		})
	}
}

func TestDamagedPrefsAreSetAside(t *testing.T) {
	dir := t.TempDir()
	s := &prefsStore{path: filepath.Join(dir, "desktop.json")}
	os.WriteFile(s.path, []byte("{nope"), 0o600)
	p, err := s.load()
	require.Error(t, err, "%+v", p)
	require.Contains(t, err.Error(), "set aside", "%+v %v", p, err)
	require.Equal(t, "system", p.Theme, "%+v %v", p, err)
	_, err = os.Stat(s.path + ".damaged")
	assert.NoError(t, err, "the damaged file wasn't kept")
	_, err = s.load()
	assert.NoError(t, err, "after setting it aside")
}

func TestSafeURL(t *testing.T) {
	for link, want := range map[string]bool{
		"https://example.com/a?b":   true,
		"http://localhost:8080":     true,
		"mailto:dev@example.com":    true,
		"HTTPS://EXAMPLE.COM":       true,
		"file:///etc/passwd":        false,
		"javascript:alert(1)":       false,
		"vscode://open?x":           false,
		"https://":                  false,
		"/relative/path":            false,
		"data:text/html,<script>":   false,
		" https://example.com/ok  ": true,
		"http://[::1":               false,
	} {
		t.Run(link, func(t *testing.T) {
			got := safeURL(link)
			assert.Equal(t, want, got, "%q: %v", link, got)
		})
	}
}
