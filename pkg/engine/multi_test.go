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

package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// One process can hold several workspaces (the per-user service does):
// nothing may depend on the process's working directory, and per-workspace
// state stays separate.
func TestTwoWorkspacesInOneProcess(t *testing.T) {
	defer i18n.SetCurrent(nil)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	t.Chdir(t.TempDir()) // somewhere unrelated to either workspace

	open := func(agent string) *Workspace {
		t.Helper()
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, ".agents", "agents"), 0o700)
		os.WriteFile(filepath.Join(dir, ".agents", "agents", agent+".md"), []byte("---\nname: "+agent+"\ndisplay_name: "+agent+"\ndescription: d\ntools: []\n---\nprompt\n"), 0o600)
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Blitz.TrustWorkspace = true // so .agents/agents is read
		cfg.Session.StorageDir = t.TempDir()
		w, err := Open(context.Background(), cfg, Options{Model: runtime.NewMockLLM("m"), NewModel: mockModels})
		require.NoError(t, err)
		t.Cleanup(func() { w.Close() })
		return w
	}
	a, b := open("alpha"), open("beta")

	has := func(w *Workspace, name string) bool {
		return slices.ContainsFunc(w.ListAgents(), func(x api.AgentInfo) bool { return x.Name == name })
	}
	assert.True(t, has(a, "alpha"), "each workspace should read its own .agents/agents: a=%v b=%v", has(a, "alpha"), has(b, "beta"))
	assert.False(t, has(a, "beta"), "each workspace should read its own .agents/agents: a=%v b=%v", has(a, "alpha"), has(b, "beta"))
	assert.True(t, has(b, "beta"), "each workspace should read its own .agents/agents: a=%v b=%v", has(a, "alpha"), has(b, "beta"))
	assert.False(t, has(b, "alpha"), "each workspace should read its own .agents/agents: a=%v b=%v", has(a, "alpha"), has(b, "beta"))
	require.NotEqual(t, b.Dir(), a.Dir(), "same directory")

	// Reply languages are per workspace.
	_, err := a.SetLocale(context.Background(), "es")
	require.NoError(t, err)
	assert.Equal(t, "es", a.Settings().Locale, "locales a=%s b=%s", a.Settings().Locale, b.Settings().Locale)
	assert.Equal(t, "en-US", b.Settings().Locale, "locales a=%s b=%s", a.Settings().Locale, b.Settings().Locale)

}

// Only one Workspace, in any process, owns a workspace at a time.
func TestAWorkspaceHasOneOwner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(dir, link))
	open := func(d string) (*Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = d
		cfg.Session.StorageDir = t.TempDir()
		return Open(context.Background(), cfg, Options{Model: runtime.NewMockLLM("m")})
	}
	first, err := open(dir)
	require.NoError(t, err)
	_, secondErr := open(link)
	require.ErrorIs(t, secondErr, api.ErrWorkspaceBusy, "a second owner, through a symlink")
	first.Close()
	again, err := open(dir)
	require.NoError(t, err, "after Close")
	again.Close()
}
