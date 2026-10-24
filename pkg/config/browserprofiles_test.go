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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserProfiles(t *testing.T) {
	dir := t.TempDir()
	s := OpenBrowserProfiles(dir)
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	assert.Empty(t, s.List())

	work, err := s.Create("work", now)
	require.NoError(t, err)
	assert.Equal(t, "work", work.Name)
	info, err := os.Stat(s.Dir("work"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "owner-only: its cookies are credentials")
	_, err = s.Create("personal", now)
	require.NoError(t, err)
	assert.Equal(t, []string{"personal", "work"}, names(s.List()), "by name")

	for _, bad := range []string{"Work", "", "a b", "../x", "-x", "far-too-long-for-a-browser-profile-name"} {
		_, err := s.Create(bad, now)
		assert.Error(t, err, "%q", bad)
	}
	_, err = s.Create("work", now)
	assert.ErrorContains(t, err, "exists")

	// A workspace's choice, by its folder (symbolic links resolved).
	ws := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(ws, link))
	assert.Empty(t, s.ForWorkspace(ws))
	require.NoError(t, s.SetWorkspace(link, "work"))
	assert.Equal(t, "work", s.ForWorkspace(ws))
	assert.ErrorIs(t, s.SetWorkspace(ws, "nope"), ErrNoBrowserProfile)

	require.NoError(t, s.SetAllowDomains("work", []string{"mail.example.com", "docs.example.com", "mail.example.com"}))
	p, ok := s.Get("work")
	require.True(t, ok)
	assert.Equal(t, []string{"docs.example.com", "mail.example.com"}, p.AllowDomains)

	// Clear signs out; Delete forgets it and the workspaces' choice.
	require.NoError(t, os.WriteFile(filepath.Join(s.Dir("work"), "Cookies"), []byte("x"), 0o600))
	require.NoError(t, s.Clear("work"))
	assert.NoFileExists(t, filepath.Join(s.Dir("work"), "Cookies"))
	assert.DirExists(t, s.Dir("work"))
	require.NoError(t, s.Delete("work"))
	assert.NoDirExists(t, s.Dir("work"))
	assert.Empty(t, s.ForWorkspace(ws))
	assert.ErrorIs(t, s.Delete("work"), ErrNoBrowserProfile)
	assert.ErrorIs(t, s.Clear("work"), ErrNoBrowserProfile)

	require.NoError(t, s.SetWorkspace(ws, "personal"))
	require.NoError(t, s.SetWorkspace(ws, ""))
	assert.Empty(t, s.ForWorkspace(ws), "none again")

	// Another process's store sees the same.
	assert.Equal(t, []string{"personal"}, names(OpenBrowserProfiles(dir).List()))
}

func names(ps []BrowserProfile) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

// A store that can't be written, or a workspace that isn't there, is an
// error, not a choice half made.
func TestBrowserProfilesFailures(t *testing.T) {
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	missing := filepath.Join(t.TempDir(), "gone")

	t.Run("a workspace that isn't there", func(t *testing.T) {
		s := OpenBrowserProfiles(t.TempDir())
		_, err := s.Create("work", now)
		require.NoError(t, err)
		assert.Error(t, s.SetWorkspace(missing, "work"))
		assert.Empty(t, s.ForWorkspace(missing))
	})
	t.Run("sites for no profile", func(t *testing.T) {
		assert.ErrorIs(t, OpenBrowserProfiles(t.TempDir()).SetAllowDomains("work", []string{"example.com"}), ErrNoBrowserProfile)
	})
	t.Run("no room for its folder", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "browser"), nil, 0o600))
		_, err := OpenBrowserProfiles(dir).Create("work", now)
		assert.Error(t, err)
	})
	t.Run("a settings folder it can't write", func(t *testing.T) {
		dir := t.TempDir()
		s := OpenBrowserProfiles(dir)
		_, err := s.Create("work", now)
		require.NoError(t, err)
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { os.Chmod(dir, 0o700) })
		assert.Error(t, s.Delete("work"))
		assert.DirExists(t, s.Dir("work"), "kept when it can't be forgotten")
	})
}
