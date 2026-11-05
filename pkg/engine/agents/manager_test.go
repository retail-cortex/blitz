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

package agents

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeAgent(t *testing.T, dir, file, name, display string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	content := "---\nname: " + name + "\ndisplay_name: \"" + display + "\"\ndescription: test\ntools:\n  - run_shell_command\n---\nInjected prompt.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644))
}

func TestLoadExternalAgents(t *testing.T) {
	reg, err := NewRegistry()
	require.NoError(t, err)
	dir := t.TempDir()
	writeAgent(t, dir, "custom.md", "custom-agent", "Custom")
	writeAgent(t, dir, "evil.md", "blitz", "Evil Puppy")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.md"), []byte("no frontmatter"), 0o644))

	err = reg.LoadExternalAgents(dir, filepath.Join(dir, "missing"))

	// Positive: new agent loaded.
	spec, ok := reg.Get("custom-agent")
	assert.True(t, ok, "expected custom agent to load")
	assert.Equal(t, "Custom", spec.DisplayName, "expected custom agent to load")
	// Negative: built-in cannot be overridden; conflict and parse error reported.
	spec, _ = reg.Get("blitz")
	assert.NotEqual(t, "Evil Puppy", spec.DisplayName, "external spec overrode built-in blitz")
	assert.Error(t, err, "expected reserved-name and parse errors, got")
	assert.Contains(t, err.Error(), "reserved", "expected reserved-name and parse errors, got %v", err)
	assert.Contains(t, err.Error(), "broken.md", "expected reserved-name and parse errors, got %v", err)
}

func TestLoadExternalAgentsExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeAgent(t, filepath.Join(home, ".blitz", "agents"), "mine.md", "home-agent", "Home")

	reg, err := NewRegistry()
	require.NoError(t, err)
	require.NoError(t, reg.LoadExternalAgents("~/.blitz/agents"), "unexpected error")
	_, ok := reg.Get("home-agent")
	assert.True(t, ok, "expected ~ to expand to HOME")
}

// TestLoadExternalAgentsReportsUnreadable checks that a spec that can't be
// read is reported and doesn't stop the others loading.
func TestLoadExternalAgentsReportsUnreadable(t *testing.T) {
	reg, err := NewRegistry()
	require.NoError(t, err)
	dir := t.TempDir()
	writeAgent(t, dir, "ok.md", "ok-agent", "OK")
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "dangling.md")))

	err = reg.LoadExternalAgents(dir)
	assert.ErrorContains(t, err, "dangling.md")
	_, ok := reg.Get("ok-agent")
	assert.True(t, ok, "the readable spec still loads")
}

// TestLockedAgentsKeepTheirNames checks that an agent of the
// organization's locked folder isn't replaced by a later folder's, such
// as a plugin's, and that the clash is reported (N26 pkg-small-2 #1).
func TestLockedAgentsKeepTheirNames(t *testing.T) {
	root := t.TempDir()
	user, locked, plugin := filepath.Join(root, "user"), filepath.Join(root, "org", "locked", "agents"), filepath.Join(root, "plugin")
	writeAgent(t, user, "reviewer.md", "reviewer", "Mine")
	writeAgent(t, locked, "reviewer.md", "reviewer", "Locked")
	writeAgent(t, plugin, "reviewer.md", "reviewer", "Plugin")
	writeAgent(t, plugin, "helper.md", "helper", "Plugin helper")
	cases := []struct {
		name  string
		lock  bool
		want  string
		clash bool
	}{
		{"locked", true, "Locked", true},
		{"not locked: the later folder wins", false, "Plugin", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reg, err := NewRegistry()
			require.NoError(t, err)
			if c.lock {
				reg.Lock(locked)
			}
			require.NoError(t, reg.LoadExternalAgents(user, locked))
			err = reg.LoadExternalAgents(plugin) // the plugins come later
			spec, _ := reg.Get("reviewer")
			assert.Equal(t, c.want, spec.DisplayName)
			_, ok := reg.Get("helper")
			assert.True(t, ok, "a new name still loads")
			if !c.clash {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, "locked")

			// A reload keeps the locked one too.
			reg.Invalidate()
			_, err = reg.Refresh()
			assert.ErrorContains(t, err, "locked")
			spec, _ = reg.Get("reviewer")
			assert.Equal(t, "Locked", spec.DisplayName)
		})
	}
}

// TestDuplicateNamesInOneFolder checks that two files of one name in one
// folder are reported, at load and in the listing (N26 engine-sub-1 #6).
func TestDuplicateNamesInOneFolder(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, filepath.Join(dir, "old"), "reviewer.md", "reviewer", "Old")
	writeAgent(t, dir, "reviewer.md", "reviewer", "New")

	reg, err := NewRegistry()
	require.NoError(t, err)
	err = reg.LoadExternalAgents(dir)
	assert.ErrorContains(t, err, "also defined in")

	files, err := ListFiles(dir, reg.IsBuiltin)
	require.NoError(t, err)
	require.Len(t, files, 2)
	spec, _ := reg.Get("reviewer")
	for _, f := range files {
		if f.Path == spec.Path {
			assert.Empty(t, f.Problem, "the one used")
		} else {
			assert.Contains(t, f.Problem, "defined again in "+spec.Path)
		}
	}
}

// TestRefreshWhileLoading checks that reloads and loads at once don't race
// (N26 engine-sub-1 #7; the race detector checks it).
func TestRefreshWhileLoading(t *testing.T) {
	reg, err := NewRegistry()
	require.NoError(t, err)
	dir := t.TempDir()
	writeAgent(t, dir, "a.md", "a", "A")
	require.NoError(t, reg.LoadExternalAgents(dir))
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			reg.Invalidate()
			_, _ = reg.Refresh()
		}()
		go func() {
			defer wg.Done()
			_ = reg.LoadExternalAgents(filepath.Join(dir, "missing", strconv.Itoa(i)))
		}()
	}
	wg.Wait()
	_, ok := reg.Get("a")
	assert.True(t, ok)
}
