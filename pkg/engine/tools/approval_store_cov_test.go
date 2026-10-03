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

package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApprovalStoreReloadSeesOtherWriters checks Reload picks up rules
// another process saved, and that a nil store is harmless.
func TestApprovalStoreReloadSeesOtherWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approvals.json")
	a, err := OpenApprovalStore(path)
	require.NoError(t, err)
	b, err := OpenApprovalStore(path)
	require.NoError(t, err)
	require.NoError(t, b.Add("shell:ls", "ls"))

	assert.False(t, a.Has("shell:ls"), "before reloading")
	require.NoError(t, a.Reload())
	assert.True(t, a.Has("shell:ls"), "after reloading")
	assert.Equal(t, path, a.Path())

	var nilStore *ApprovalStore
	assert.NoError(t, nilStore.Reload())
	assert.Empty(t, nilStore.Path())
	assert.Nil(t, nilStore.Rules())
	assert.False(t, nilStore.Has("x"))
}

// TestApprovalStoreRemove checks Remove reports whether the rule existed and
// persists the removal.
func TestApprovalStoreRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approvals.json")
	s, err := OpenApprovalStore(path)
	require.NoError(t, err)
	require.NoError(t, s.Add("b", "B"))
	require.NoError(t, s.Add("a", "A"))
	rules := s.Rules()
	require.Len(t, rules, 2)
	assert.Equal(t, "a", rules[0].Key, "rules are sorted by key")

	ok, err := s.Remove("missing")
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = s.Remove("a")
	require.NoError(t, err)
	assert.True(t, ok)

	again, err := OpenApprovalStore(path)
	require.NoError(t, err)
	assert.False(t, again.Has("a"))
	assert.True(t, again.Has("b"))
}

// TestApprovalStoreErrors checks unreadable and corrupt files fail to open or
// reload, and that saving into an unwritable place fails.
func TestApprovalStoreErrors(t *testing.T) {
	dir := t.TempDir()
	t.Run("path is a directory", func(t *testing.T) {
		_, err := OpenApprovalStore(dir)
		assert.Error(t, err)
	})
	t.Run("reload of a corrupted file", func(t *testing.T) {
		path := filepath.Join(dir, "a.json")
		s, err := OpenApprovalStore(path)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, []byte("{"), 0o600))
		assert.Error(t, s.Reload())
	})
	t.Run("parent is a file", func(t *testing.T) {
		parent := filepath.Join(dir, "later-a-file")
		s, err := OpenApprovalStore(filepath.Join(parent, "a.json"))
		require.NoError(t, err, "a missing file opens empty")
		require.NoError(t, os.WriteFile(parent, nil, 0o600))
		assert.Error(t, s.Add("k", "l"))
	})
	t.Run("parent not writable", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores permissions")
		}
		ro := filepath.Join(dir, "ro")
		require.NoError(t, os.Mkdir(ro, 0o500))
		t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
		s, err := OpenApprovalStore(filepath.Join(ro, "a.json"))
		require.NoError(t, err)
		assert.Error(t, s.Add("k", "l"))
	})
}

// TestApprovalStoreSaveFails checks a rule that can't be saved is neither
// added nor removed in memory: the store says what the file says.
func TestApprovalStoreSaveFails(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenApprovalStore(filepath.Join(dir, "approvals.json"))
	require.NoError(t, err)
	require.NoError(t, s.Add("kept", "K"))
	require.NoError(t, os.Chmod(dir, 0o500)) // no temp file can be made
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	assert.Error(t, s.Add("new", "N"))
	assert.False(t, s.Has("new"), "a rule that wasn't saved")
	assert.Error(t, s.Add("kept", "changed"))
	require.Len(t, s.Rules(), 1)
	assert.Equal(t, "K", s.Rules()[0].Label, "a change that wasn't saved")
	ok, err := s.Remove("kept")
	assert.Error(t, err)
	assert.True(t, ok)
	assert.True(t, s.Has("kept"), "a removal that wasn't saved")
}
